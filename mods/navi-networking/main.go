package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"
	theme "github.com/rav3ndust/navi-theme"
)

// ─────────────────────────────────────────────────────────────────────────
// Styles
// ─────────────────────────────────────────────────────────────────────────

// frameWidth is the family-standard mod window width. Views assume it;
// do not let status strings wrap inside it.
var frameWidth = 62

// okStyle is the one local style: "good news" green, composed from theme
// tokens. Everything else comes from the theme package directly.
var okStyle = theme.DotOn.Copy().Bold(true)

// ─────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────

type Network struct {
	SSID     string
	Signal   int
	Security string
	InUse    bool
	BSSID    string
}

type ConnectionInfo struct {
	SSID     string
	Profile  string
	UUID     string
	Device   string
	Signal   int
	Security string
	BSSID    string
	IPv4     string
	Gateway  string
	DNSv4    string
	DNSv6    string
}

type DNSPreset struct {
	Name      string
	IPv4      []string
	IPv6      []string
	Automatic bool
}

var dnsPresets = []DNSPreset{
	{Name: "ISP / Automatic", Automatic: true},
	{Name: "Cloudflare", IPv4: []string{"1.1.1.1", "1.0.0.1"}, IPv6: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}},
	{Name: "Google", IPv4: []string{"8.8.8.8", "8.8.4.4"}, IPv6: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"}},
	{Name: "Custom"},
}

type screen int

const (
	screenNetworks screen = iota
	screenPassword
	screenOpenConfirm
	screenDashboard
	screenDNS
	screenCustomDNS
	screenDetails
	screenShare
	screenForgetConfirm
	screenSpeedTest
)

// ─────────────────────────────────────────────────────────────────────────
// Messages
// ─────────────────────────────────────────────────────────────────────────

type startMsg struct{}
type networkMsg struct {
	networks []Network
	err      error
}
type connectedMsg struct{ err error }
type disconnectedMsg struct{ err error }
type dnsMsg struct {
	err    error
	preset string
}
type infoMsg struct {
	info ConnectionInfo
	err  error
}
type shareMsg struct {
	payload string
	err     error
}
type forgetMsg struct{ err error }
type speedTestDoneMsg speedTestResult
type speedTickMsg struct{}

// transTickMsg drives the static-dissolve screen transition.
type transTickMsg struct{}

// idleTickMsg drives the breathing idle glow when there is no link.
type idleTickMsg struct{}

const transFrames = 4

func transTickCmd() tea.Cmd {
	return tea.Tick(45*time.Millisecond, func(time.Time) tea.Msg { return transTickMsg{} })
}

func idleTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return idleTickMsg{} })
}

// ─────────────────────────────────────────────────────────────────────────
// ─────────────────────────────────────────────────────────────────────────
// Model
// ─────────────────────────────────────────────────────────────────────────

type model struct {
	networks      []Network
	cursor        int
	screen        screen
	password      string
	selected      Network
	info          ConnectionInfo
	dnsCursor     int
	customDNS     [4]string
	customCursor  int
	width, height int
	loading       bool
	message       string
	err           error
	qr            string

	// Theme chrome: the shared nightshadeNeon spinner frame counter and
	// the ambient Serial Experiments Lain transmission ticker.
	spinFrame int
	spinTag   int
	tx        theme.Transmission

	// transition counts down a brief static-dissolve when changing
	// screens; place() glitches the frame while it is nonzero.
	transition int

	// samples is a ring of recent throughput readings (Mbps) taken on
	// each speed-test tick, rendered as a live sparkline.
	samples   []float64
	lastBytes int64

	// idleTick tracks whether the breathing-idle ticker is in flight.
	idleTick bool

	// cancel, when non-nil, aborts whatever background operation is
	// currently in flight (scan, connect, speed test, ...). Pressing esc
	// while loading calls this instead of quitting the program outright.
	cancel context.CancelFunc

	speedProgress *speedProgress
	speedResult   speedTestResult
	speedRunning  bool
}

func initialModel() model {
	return model{loading: true}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(theme.SpinnerTick(m.spinTag, 80*time.Millisecond), m.tx.Init(), func() tea.Msg { return startMsg{} })
}

// ─────────────────────────────────────────────────────────────────────────
// NetworkManager (nmcli) plumbing
// ─────────────────────────────────────────────────────────────────────────

func runNmcli(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "nmcli", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}

// nmcli terse mode escapes separators in fields. In particular, SSIDs may
// legitimately contain ':' characters, so a plain strings.Split is unsafe.
func splitTerse(line string) []string {
	var fields []string
	var b strings.Builder
	escaped := false

	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ':' {
			fields = append(fields, b.String())
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteByte('\\')
	}
	fields = append(fields, b.String())
	return fields
}

func scanNetworks(ctx context.Context) ([]Network, error) {
	output, err := runNmcli(ctx, "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY,BSSID", "device", "wifi", "list", "--rescan", "yes")
	if err != nil {
		return nil, fmt.Errorf("network scan failed: %w", err)
	}
	var networks []Network
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := splitTerse(line)
		if len(fields) < 5 {
			continue
		}
		inUse := fields[0] == "*"
		ssid := fields[1]
		signal, _ := strconv.Atoi(fields[2])
		security := fields[3]
		bssid := strings.Join(fields[4:], ":")
		if ssid == "" {
			ssid = "<hidden>"
		}
		if existing, ok := findNetwork(networks, ssid); ok {
			if signal > networks[existing].Signal {
				networks[existing].Signal, networks[existing].BSSID = signal, bssid
			}
			if inUse {
				networks[existing].InUse = true
			}
			continue
		}
		networks = append(networks, Network{SSID: ssid, Signal: signal, Security: security, InUse: inUse, BSSID: bssid})
	}
	for i := range networks {
		for j := i + 1; j < len(networks); j++ {
			if networks[j].Signal > networks[i].Signal {
				networks[i], networks[j] = networks[j], networks[i]
			}
		}
	}
	return networks, nil
}

func findNetwork(networks []Network, ssid string) (int, bool) {
	for i, n := range networks {
		if n.SSID == ssid {
			return i, true
		}
	}
	return -1, false
}
func isOpen(n Network) bool {
	return n.Security == "" || n.Security == "--" || strings.EqualFold(n.Security, "OPEN")
}

func connectNetwork(ctx context.Context, network Network, password string) error {
	args := []string{"device", "wifi", "connect", network.SSID}
	if password != "" {
		args = append(args, "password", password)
	}
	_, err := runNmcli(ctx, args...)
	return err
}
func disconnectNetwork(ctx context.Context, info ConnectionInfo) error {
	if info.Device == "" {
		return fmt.Errorf("no active Wi-Fi device")
	}
	_, err := runNmcli(ctx, "device", "disconnect", info.Device)
	return err
}

func activeConnection(ctx context.Context) (ConnectionInfo, error) {
	out, err := runNmcli(ctx, "-t", "-f", "ACTIVE,SSID,DEVICE,SIGNAL,SECURITY,BSSID", "device", "wifi")
	if err != nil {
		return ConnectionInfo{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := splitTerse(line)
		if len(f) < 6 || f[0] != "yes" {
			continue
		}
		signal, _ := strconv.Atoi(f[3])
		info := ConnectionInfo{
			SSID: f[1], Device: f[2], Signal: signal, Security: f[4], BSSID: f[5],
		}
		if info.Security == "" || info.Security == "--" {
			info.Security = "OPEN"
		}

		// Resolve the active profile by device, rather than assuming the
		// profile name is the SSID. This matters for renamed profiles and
		// SSIDs containing characters meaningful to nmcli.
		profiles, e := runNmcli(ctx, "-t", "-f", "NAME,UUID,TYPE,DEVICE", "connection", "show", "--active")
		if e == nil {
			for _, pl := range strings.Split(strings.TrimSpace(string(profiles)), "\n") {
				pf := splitTerse(pl)
				if len(pf) >= 4 && pf[2] == "802-11-wireless" && pf[3] == info.Device {
					info.Profile = pf[0]
					info.UUID = pf[1]
					break
				}
			}
		}

		details, e := runNmcli(ctx, "-t", "-f", "IP4.ADDRESS,IP4.GATEWAY,IP4.DNS,IP6.DNS", "device", "show", info.Device)
		if e == nil {
			vals := strings.Split(strings.TrimSpace(string(details)), "\n")
			if len(vals) > 0 {
				info.IPv4 = vals[0]
			}
			if len(vals) > 1 {
				info.Gateway = vals[1]
			}
			if len(vals) > 2 {
				info.DNSv4 = vals[2]
			}
			if len(vals) > 3 {
				info.DNSv6 = vals[3]
			}
		}
		return info, nil
	}
	return ConnectionInfo{}, fmt.Errorf("no active Wi-Fi connection")
}

func applyDNS(ctx context.Context, info ConnectionInfo, preset DNSPreset) error {
	target := info.UUID
	if target == "" {
		target = info.Profile
	}
	if target == "" {
		return fmt.Errorf("active connection profile unavailable")
	}
	args := []string{"connection", "modify", target}
	if preset.Automatic {
		args = append(args, "ipv4.ignore-auto-dns", "no", "ipv4.dns", "", "ipv6.ignore-auto-dns", "no", "ipv6.dns", "")
	} else {
		args = append(args, "ipv4.ignore-auto-dns", "yes", "ipv4.dns", strings.Join(preset.IPv4, " "), "ipv6.ignore-auto-dns", "yes", "ipv6.dns", strings.Join(preset.IPv6, " "))
	}
	if _, err := runNmcli(ctx, args...); err != nil {
		return fmt.Errorf("could not update DNS: %w", err)
	}
	if _, err := runNmcli(ctx, "connection", "up", target); err != nil {
		return fmt.Errorf("DNS saved, but connection could not be restarted: %w", err)
	}
	return nil
}

func applyCustomDNS(ctx context.Context, info ConnectionInfo, ipv4, ipv4b, ipv6, ipv6b string) error {
	v4 := []string{}
	if strings.TrimSpace(ipv4) != "" {
		v4 = append(v4, strings.TrimSpace(ipv4))
	}
	if strings.TrimSpace(ipv4b) != "" {
		v4 = append(v4, strings.TrimSpace(ipv4b))
	}
	v6 := []string{}
	if strings.TrimSpace(ipv6) != "" {
		v6 = append(v6, strings.TrimSpace(ipv6))
	}
	if strings.TrimSpace(ipv6b) != "" {
		v6 = append(v6, strings.TrimSpace(ipv6b))
	}
	if len(v4) == 0 && len(v6) == 0 {
		return fmt.Errorf("enter at least one DNS server")
	}
	target := info.UUID
	if target == "" {
		target = info.Profile
	}
	if target == "" {
		return fmt.Errorf("active connection profile unavailable")
	}
	args := []string{"connection", "modify", target, "ipv4.ignore-auto-dns", "yes", "ipv4.dns", strings.Join(v4, " "), "ipv6.ignore-auto-dns", "yes", "ipv6.dns", strings.Join(v6, " ")}
	if _, err := runNmcli(ctx, args...); err != nil {
		return fmt.Errorf("could not update DNS: %w", err)
	}
	if _, err := runNmcli(ctx, "connection", "up", target); err != nil {
		return fmt.Errorf("DNS saved, but connection could not be restarted: %w", err)
	}
	return nil
}

func wifiEscape(s string) string {
	r := strings.ReplaceAll(s, "\\", "\\\\")
	r = strings.ReplaceAll(r, ";", "\\;")
	r = strings.ReplaceAll(r, ",", "\\,")
	r = strings.ReplaceAll(r, ":", "\\:")
	return r
}
func wifiQR(ssid, security, password string) string {
	typ := "nopass"
	if !isOpen(Network{Security: security}) {
		typ = "WPA"
	}
	if typ == "nopass" {
		return fmt.Sprintf("WIFI:T:nopass;S:%s;;", wifiEscape(ssid))
	}
	return fmt.Sprintf("WIFI:T:WPA;S:%s;P:%s;;", wifiEscape(ssid), wifiEscape(password))
}
func getPassword(ctx context.Context, info ConnectionInfo) (string, error) {
	target := info.UUID
	if target == "" {
		target = info.Profile
	}
	if target == "" {
		return "", fmt.Errorf("saved connection profile unavailable")
	}
	out, err := runNmcli(ctx, "--show-secrets", "-g", "802-11-wireless-security.psk", "connection", "show", target)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ─────────────────────────────────────────────────────────────────────────
// Speed test
// ─────────────────────────────────────────────────────────────────────────

const (
	stageLatency int32 = iota
	stageDownload
	stageUpload
)

const (
	speedTestDownloadURL   = "https://speed.cloudflare.com/__down?bytes=%d"
	speedTestUploadURL     = "https://speed.cloudflare.com/__up"
	speedTestDownloadBytes = 50_000_000 // 50MB
	speedTestUploadBytes   = 20_000_000 // 20MB
)

type speedTestResult struct {
	latencyMs    float64
	downloadMbps float64
	uploadMbps   float64
	err          error
}

// speedProgress is shared, atomically-updated state: the test itself runs
// in one goroutine while a ticking tea.Cmd polls it for the progress bar,
// so nothing here ever touches the bubbletea model directly.
type speedProgress struct {
	stage      atomic.Int32
	bytesDone  atomic.Int64
	totalBytes atomic.Int64
}

func newSpeedProgress() *speedProgress {
	sp := &speedProgress{}
	sp.stage.Store(stageLatency)
	return sp
}

func measureLatency(ctx context.Context) (float64, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	const rounds = 4
	var total time.Duration
	for i := 0; i < rounds; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://speed.cloudflare.com/__down?bytes=0", nil)
		if err != nil {
			return 0, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		total += time.Since(start)
	}
	avg := total / rounds
	return float64(avg.Microseconds()) / 1000.0, nil
}

type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n.Add(int64(n))
	}
	return n, err
}

func measureDownload(ctx context.Context, progress *speedProgress) (float64, error) {
	progress.stage.Store(stageDownload)
	progress.bytesDone.Store(0)
	progress.totalBytes.Store(speedTestDownloadBytes)

	url := fmt.Sprintf(speedTestDownloadURL, speedTestDownloadBytes)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	cr := &countingReader{r: resp.Body, n: &progress.bytesDone}
	written, err := io.Copy(io.Discard, cr)
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, fmt.Errorf("download finished too fast to measure")
	}
	return (float64(written) * 8 / 1_000_000) / elapsed, nil
}

func measureUpload(ctx context.Context, progress *speedProgress) (float64, error) {
	progress.stage.Store(stageUpload)
	progress.bytesDone.Store(0)
	progress.totalBytes.Store(speedTestUploadBytes)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		buf := make([]byte, 64*1024)
		remaining := int64(speedTestUploadBytes)
		for remaining > 0 {
			n := len(buf)
			if int64(n) > remaining {
				n = int(remaining)
			}
			written, err := pw.Write(buf[:n])
			if err != nil {
				return
			}
			remaining -= int64(written)
			progress.bytesDone.Add(int64(written))
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, speedTestUploadURL, pr)
	if err != nil {
		return 0, err
	}
	req.ContentLength = speedTestUploadBytes
	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, fmt.Errorf("upload finished too fast to measure")
	}
	return (float64(speedTestUploadBytes) * 8 / 1_000_000) / elapsed, nil
}

func runSpeedTest(ctx context.Context, progress *speedProgress) speedTestResult {
	latency, err := measureLatency(ctx)
	if err != nil {
		return speedTestResult{err: fmt.Errorf("latency test failed: %w", err)}
	}
	download, err := measureDownload(ctx, progress)
	if err != nil {
		return speedTestResult{latencyMs: latency, err: fmt.Errorf("download test failed: %w", err)}
	}
	upload, err := measureUpload(ctx, progress)
	if err != nil {
		return speedTestResult{latencyMs: latency, downloadMbps: download, err: fmt.Errorf("upload test failed: %w", err)}
	}
	return speedTestResult{latencyMs: latency, downloadMbps: download, uploadMbps: upload}
}

// ─────────────────────────────────────────────────────────────────────────
// Commands
// ─────────────────────────────────────────────────────────────────────────

func scanCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg { n, e := scanNetworks(ctx); return networkMsg{n, e} }
}
func connectCmd(ctx context.Context, n Network, p string) tea.Cmd {
	return func() tea.Msg { return connectedMsg{connectNetwork(ctx, n, p)} }
}
func dnsCmd(ctx context.Context, info ConnectionInfo, p DNSPreset) tea.Cmd {
	return func() tea.Msg { return dnsMsg{applyDNS(ctx, info, p), p.Name} }
}
func customDNSCmd(ctx context.Context, info ConnectionInfo, v4, v4b, v6, v6b string) tea.Cmd {
	return func() tea.Msg { return dnsMsg{applyCustomDNS(ctx, info, v4, v4b, v6, v6b), "Custom"} }
}
func infoCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg { i, e := activeConnection(ctx); return infoMsg{i, e} }
}
func shareCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		i, e := activeConnection(ctx)
		if e != nil {
			return shareMsg{"", e}
		}
		p, e := getPassword(ctx, i)
		if e != nil && !isOpen(Network{Security: i.Security}) {
			return shareMsg{"", e}
		}
		return shareMsg{wifiQR(i.SSID, i.Security, p), nil}
	}
}
func disconnectNetworkCmd(ctx context.Context, info ConnectionInfo) tea.Cmd {
	return func() tea.Msg { return disconnectedMsg{disconnectNetwork(ctx, info)} }
}
func forgetCmd(ctx context.Context, info ConnectionInfo) tea.Cmd {
	return func() tea.Msg {
		target := info.UUID
		if target == "" {
			target = info.Profile
		}
		if target == "" {
			return forgetMsg{fmt.Errorf("saved connection profile unavailable")}
		}
		_, e := runNmcli(ctx, "connection", "delete", target)
		return forgetMsg{e}
	}
}
func speedTestCmd(ctx context.Context, progress *speedProgress) tea.Cmd {
	return func() tea.Msg { return speedTestDoneMsg(runSpeedTest(ctx, progress)) }
}
func speedTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return speedTickMsg{} })
}

// startOp creates a fresh cancellable context, stashes its cancel func on
// the model so a subsequent esc can abort the operation, and returns the
// context for use in the accompanying command.
func (m *model) startOp() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	// Bump the spinner tag so ticks from a previous operation die.
	m.spinTag++
	return ctx
}

// syncIdle arms the breathing-idle ticker when the mod is truly idle —
// not loading, not testing, no link — and disarms it otherwise.
func (m *model) syncIdle(cmd tea.Cmd) tea.Cmd {
	want := !m.loading && !m.speedRunning && m.info.SSID == ""
	if want && !m.idleTick {
		m.idleTick = true
		return tea.Batch(cmd, idleTickCmd())
	}
	if !want {
		m.idleTick = false
	}
	return cmd
}

// goScreen switches screens through a brief static dissolve.
func (m *model) goScreen(s screen) tea.Cmd {
	if m.screen == s {
		return nil
	}
	m.screen = s
	m.transition = transFrames
	return transTickCmd()
}

// ─────────────────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case theme.SpinnerTickMsg:
		if msg.Tag == m.spinTag {
			m.spinFrame++
			if m.loading || m.speedRunning {
				return m, theme.SpinnerTick(m.spinTag, 80*time.Millisecond)
			}
		}
		return m, nil

	case startMsg:
		ctx := m.startOp()
		return m, tea.Batch(scanCmd(ctx), infoCmd(ctx))

	case networkMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		m.networks = msg.networks
		m.err = msg.err
		if m.cursor >= len(m.networks) {
			m.cursor = len(m.networks) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}

	case connectedMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if msg.err != nil {
			m.cancel = nil
			m.err = msg.err
			return m, nil
		}
		m.message = fmt.Sprintf("connected to %s", m.selected.SSID)
		m.password = ""
		trans := m.goScreen(screenNetworks)
		m.loading = true
		ctx := m.startOp()
		return m, tea.Batch(scanCmd(ctx), infoCmd(ctx), trans)

	case disconnectedMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.info = ConnectionInfo{}
		m.message = "disconnected"
		trans := m.goScreen(screenNetworks)
		m.err = nil
		ctx := m.startOp()
		return m, tea.Batch(scanCmd(ctx), trans)

	case dnsMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.message = "DNS set to " + msg.preset
		trans := m.goScreen(screenDashboard)
		ctx := m.startOp()
		return m, tea.Batch(infoCmd(ctx), trans)

	case infoMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		if msg.err == nil {
			m.info = msg.info
			if m.screen == screenNetworks && m.selected.SSID == "" {
				m.screen = screenDashboard
			}
		} else if m.screen == screenDashboard || m.screen == screenDetails || m.screen == screenShare {
			m.err = msg.err
		}

	case shareMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		m.err = msg.err
		if msg.err == nil {
			m.qr = msg.payload
			m.screen = screenShare
			m.transition = transFrames
			return m, transTickCmd()
		}

	case forgetMsg:
		m.loading = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.message = "forgot " + m.selected.SSID
		trans := m.goScreen(screenNetworks)
		ctx := m.startOp()
		return m, tea.Batch(scanCmd(ctx), trans)

	case speedTestDoneMsg:
		m.speedRunning = false
		res := speedTestResult(msg)
		if errors.Is(res.err, context.Canceled) {
			return m, nil
		}
		m.cancel = nil
		m.speedResult = res
		m.err = nil

	case speedTickMsg:
		if m.speedRunning && m.speedProgress != nil {
			// Sample throughput once per tick for the live sparkline.
			// Ticks are 120ms apart, so the delta converts directly.
			if m.speedProgress.stage.Load() != stageLatency {
				done := m.speedProgress.bytesDone.Load()
				mbps := float64(done-m.lastBytes) / 0.12 * 8 / 1_000_000
				m.lastBytes = done
				m.samples = append(m.samples, mbps)
				if len(m.samples) > 48 {
					m.samples = m.samples[len(m.samples)-48:]
				}
			}
			return m, speedTickCmd()
		}

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case transTickMsg:
		if m.transition > 0 {
			m.transition--
			if m.transition > 0 {
				return m, transTickCmd()
			}
		}
		return m, nil

	case idleTickMsg:
		m.idleTick = false
		return m, m.syncIdle(nil)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if (m.loading || m.speedRunning) && m.cancel != nil {
				m.cancel()
				m.cancel = nil
				m.loading = false
				m.speedRunning = false
				m.message = "cancelled"
				m.err = nil
				return m, m.syncIdle(nil)
			}
		}
		prev := m.screen
		var cmd tea.Cmd
		var nm tea.Model
		switch m.screen {
		case screenNetworks:
			nm, cmd = m.updateNetworks(msg)
		case screenPassword:
			nm, cmd = m.updatePassword(msg)
		case screenOpenConfirm:
			nm, cmd = m.updateOpenConfirm(msg)
		case screenDashboard:
			nm, cmd = m.updateDashboard(msg)
		case screenDNS:
			nm, cmd = m.updateDNS(msg)
		case screenCustomDNS:
			nm, cmd = m.updateCustomDNS(msg)
		case screenDetails:
			nm, cmd = m.updateDetails(msg)
		case screenShare:
			nm = m
			if msg.String() == "esc" || msg.String() == "q" {
				m.screen = screenDashboard
				m.qr = ""
				nm = m
			}
		case screenForgetConfirm:
			nm, cmd = m.updateForget(msg)
		case screenSpeedTest:
			nm, cmd = m.updateSpeedTest(msg)
		}
		if mm, ok := nm.(model); ok {
			m = mm
		}
		if m.screen != prev {
			// Screen changed: dissolve through static.
			m.transition = transFrames
			cmd = tea.Batch(cmd, transTickCmd())
		}
		return m, m.syncIdle(cmd)
	}
	return m, m.syncIdle(nil)
}

func (m model) updateNetworks(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.networks)-1 {
			m.cursor++
		}
	case "r":
		m.loading = true
		m.message = ""
		m.err = nil
		ctx := m.startOp()
		return m, scanCmd(ctx)
	case "enter":
		if len(m.networks) == 0 {
			return m, nil
		}
		m.selected = m.networks[m.cursor]
		m.err = nil
		m.message = ""
		if m.selected.InUse {
			m.loading = true
			m.screen = screenDashboard
			ctx := m.startOp()
			return m, infoCmd(ctx)
		}
		if isOpen(m.selected) {
			m.screen = screenOpenConfirm
		} else {
			m.password = ""
			m.screen = screenPassword
		}
	case "i":
		if m.info.SSID == "" {
			return m, nil
		}
		m.loading = true
		m.err = nil
		m.screen = screenDetails
		ctx := m.startOp()
		return m, infoCmd(ctx)
	case "d":
		if m.info.SSID == "" {
			return m, nil
		}
		m.dnsCursor = 0
		m.err = nil
		m.screen = screenDNS
	case "s":
		if m.info.SSID == "" {
			return m, nil
		}
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, shareCmd(ctx)
	case "x":
		if m.info.SSID == "" {
			return m, nil
		}
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, disconnectNetworkCmd(ctx, m.info)
	case "f":
		if m.info.SSID == "" {
			return m, nil
		}
		m.selected = Network{SSID: m.info.SSID}
		m.screen = screenForgetConfirm
	case "t":
		return m.startSpeedTest()
	}
	return m, nil
}
func (m model) updatePassword(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenNetworks
		m.password = ""
		return m, nil
	case "enter":
		if m.password == "" {
			m.err = fmt.Errorf("password cannot be empty")
			return m, nil
		}
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, connectCmd(ctx, m.selected, m.password)
	case "backspace":
		if len(m.password) > 0 {
			m.password = m.password[:len(m.password)-1]
		}
	default:
		if len(msg.Runes) > 0 {
			m.password += string(msg.Runes)
		}
	}
	return m, nil
}
func (m model) updateOpenConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.screen = screenNetworks
	case "enter", "y":
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, connectCmd(ctx, m.selected, "")
	}
	return m, nil
}
func (m model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.screen = screenNetworks
	case "d":
		m.dnsCursor = 0
		m.screen = screenDNS
		m.err = nil
	case "i":
		m.loading = true
		m.err = nil
		m.screen = screenDetails
		ctx := m.startOp()
		return m, infoCmd(ctx)
	case "s":
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, shareCmd(ctx)
	case "x":
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, disconnectNetworkCmd(ctx, m.info)
	case "f":
		m.screen = screenForgetConfirm
	case "t":
		return m.startSpeedTest()
	case "r":
		m.screen = screenNetworks
		m.loading = true
		ctx := m.startOp()
		return m, scanCmd(ctx)
	}
	return m, nil
}
func (m model) updateDNS(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenDashboard
	case "up", "k":
		if m.dnsCursor > 0 {
			m.dnsCursor--
		}
	case "down", "j":
		if m.dnsCursor < len(dnsPresets)-1 {
			m.dnsCursor++
		}
	case "enter":
		p := dnsPresets[m.dnsCursor]
		if p.Name == "Custom" {
			m.customDNS = [4]string{}
			m.customCursor = 0
			m.screen = screenCustomDNS
			return m, nil
		}
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, dnsCmd(ctx, m.info, p)
	}
	return m, nil
}
func (m model) updateCustomDNS(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenDNS
	case "up", "k":
		if m.customCursor > 0 {
			m.customCursor--
		}
	case "down", "j":
		if m.customCursor < 3 {
			m.customCursor++
		}
	case "tab":
		m.customCursor = (m.customCursor + 1) % 4
	case "enter":
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, customDNSCmd(ctx, m.info, m.customDNS[0], m.customDNS[1], m.customDNS[2], m.customDNS[3])
	case "backspace":
		if len(m.customDNS[m.customCursor]) > 0 {
			m.customDNS[m.customCursor] = m.customDNS[m.customCursor][:len(m.customDNS[m.customCursor])-1]
		}
	default:
		if len(msg.Runes) > 0 {
			m.customDNS[m.customCursor] += string(msg.Runes)
		}
	}
	return m, nil
}
func (m model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" || msg.String() == "q" {
		m.screen = screenDashboard
	}
	return m, nil
}
func (m model) updateForget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.screen = screenDashboard
	case "enter", "y":
		m.loading = true
		m.err = nil
		ctx := m.startOp()
		return m, forgetCmd(ctx, m.info)
	}
	return m, nil
}
func (m model) updateSpeedTest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		if m.speedRunning {
			// esc while running is handled globally (cancels the test);
			// a second esc lands here to leave the screen.
			return m, nil
		}
		m.screen = screenDashboard
	case "t":
		if !m.speedRunning {
			return m.startSpeedTest()
		}
	}
	return m, nil
}

func (m model) startSpeedTest() (tea.Model, tea.Cmd) {
	m.speedProgress = newSpeedProgress()
	m.speedResult = speedTestResult{}
	m.speedRunning = true
	m.samples = nil
	m.lastBytes = 0
	m.screen = screenSpeedTest
	m.err = nil
	ctx := m.startOp()
	return m, tea.Batch(speedTestCmd(ctx, m.speedProgress), speedTickCmd())
}

// ─────────────────────────────────────────────────────────────────────────
// View
// ─────────────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.screen {
	case screenPassword:
		return m.passwordView()
	case screenOpenConfirm:
		return m.openConfirmView()
	case screenDashboard:
		return m.dashboardView()
	case screenDNS:
		return m.dnsView()
	case screenCustomDNS:
		return m.customDNSView()
	case screenDetails:
		return m.detailsView()
	case screenShare:
		return m.shareView()
	case screenForgetConfirm:
		return m.forgetView()
	case screenSpeedTest:
		return m.speedTestView()
	default:
		return m.networkView()
	}
}

func (m model) frame(content string) string {
	return theme.Frame(frameWidth, "navi networking", m.info.SSID != "", m.tx.View(frameWidth), content)
}

func (m model) place(content string) string {
	if m.width == 0 {
		return content
	}
	out := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.frame(content))
	if m.transition > 0 {
		// Static dissolve between screens: glitch the composed frame,
		// fading as the transition counts down. ANSI-aware so colors
		// survive the noise.
		intensity := 0.12 + 0.55*float64(m.transition)/float64(transFrames)
		out = theme.GlitchANSI(out, intensity)
	}
	return out
}

// networksFooter is the full, persistent hotkey bar shared by the network
// list and the dashboard, so the available commands never shift between
// the two screens.
func (m model) networksFooter() string {
	active := m.info.SSID != ""
	line1 := theme.Footer(true,
		[2]string{"↑↓", "navigate"},
		[2]string{"enter", "connect"},
		[2]string{"r", "rescan"},
	)
	line2 := theme.Footer(active,
		[2]string{"d", "dns"},
		[2]string{"i", "details"},
		[2]string{"s", "share"},
		[2]string{"x", "disconnect"},
	)
	line3 := theme.Footer(active,
		[2]string{"f", "forget"},
		[2]string{"t", "speed"},
	)
	return line1 + "\n" + line2 + "\n" + line3 + "\n" + theme.Dimmed.Render("q quit")
}

func (m model) networkView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("NETWORKS"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("scanning for networks..."))
		b.WriteString("\n")
	} else if len(m.networks) == 0 {
		b.WriteString(theme.Dimmed.Render("  no Wi-Fi networks found"))
		b.WriteString("\n")
	} else {
		for i, n := range m.networks {
			cursor := "  "
			if i == m.cursor {
				cursor = theme.Selected.Render("› ")
			}
			name := n.SSID
			if n.InUse {
				name = okStyle.Render(name)
			} else if i == m.cursor {
				name = theme.Selected.Render(name)
			}
			sec := n.Security
			if sec == "" {
				sec = "OPEN"
			}
			b.WriteString(fmt.Sprintf("%s%-28s %s  %s", cursor, name, signalBars(n.Signal), theme.Dimmed.Render(sec)))
			if n.InUse {
				b.WriteString("  " + okStyle.Render("●"))
			}
			b.WriteString("\n")
		}
	}
	if m.message != "" {
		b.WriteString("\n" + okStyle.Render("✓ "+m.message) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + theme.Error.Render("× "+m.err.Error()) + "\n")
	}
	if !m.loading && m.info.SSID == "" && m.err == nil && m.message == "" {
		// No link, nothing happening: the mod breathes, waiting.
		b.WriteString("\n" + theme.Glow("  ○ no link — listening for the Wired", time.Now()) + "\n")
	}
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(m.networksFooter())
	return m.place(b.String())
}
func (m model) passwordView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("CONNECT"))
	b.WriteString("\n\n")
	b.WriteString(theme.Normal.Render("  Network  ") + theme.Selected.Render(m.selected.SSID) + "\n")
	b.WriteString(theme.Dimmed.Render("  Security  ") + theme.Normal.Render(m.selected.Security) + "\n\n")
	b.WriteString(theme.Normal.Render("  Password") + "\n\n")
	b.WriteString(theme.Input.Render("  > " + strings.Repeat("•", len([]rune(m.password)))))
	if m.loading {
		b.WriteString("\n\n" + theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("connecting..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + theme.Error.Render("  × "+m.err.Error()))
	}
	b.WriteString("\n\n" + theme.Dimmed.Render("  enter connect   esc cancel"))
	return m.place(b.String())
}
func (m model) openConfirmView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("CONNECT"))
	b.WriteString("\n\n")
	b.WriteString(theme.Selected.Render("  "+m.selected.SSID) + "\n\n")
	b.WriteString(theme.Error.Render("  OPEN NETWORK") + "\n\n")
	b.WriteString(theme.Normal.Render("  This network has no password."))
	b.WriteString("\n")
	b.WriteString(theme.Dimmed.Render("  navi will never join an open network without"))
	b.WriteString("\n")
	b.WriteString(theme.Dimmed.Render("  your explicit confirmation."))
	b.WriteString("\n\n")
	b.WriteString(theme.Normal.Render("  Signal  ") + signalBars(m.selected.Signal) + "\n")
	b.WriteString(theme.Normal.Render("  BSSID   ") + theme.Grayed.Render(m.selected.BSSID) + "\n\n")
	if m.loading {
		b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("connecting...") + "\n\n")
	}
	b.WriteString(theme.Selected.Render("  [ enter ] connect"))
	b.WriteString("   " + theme.Dimmed.Render("[ esc ] cancel"))
	return m.place(b.String())
}
func (m model) dashboardView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("CONNECTION"))
	b.WriteString("\n\n")
	if m.info.SSID == "" {
		b.WriteString(theme.Dimmed.Render("  no active Wi-Fi connection"))
	} else {
		b.WriteString(okStyle.Render("  ● "+m.info.SSID) + "\n\n")
		b.WriteString(theme.Normal.Render("  Signal    ") + signalBars(m.info.Signal) + "\n")
		b.WriteString(theme.Normal.Render("  Security  ") + m.info.Security + "\n")
		b.WriteString(theme.Normal.Render("  Device    ") + m.info.Device + "\n")
		b.WriteString(theme.Normal.Render("  BSSID     ") + theme.Grayed.Render(m.info.BSSID) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + theme.Error.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(m.networksFooter())
	return m.place(b.String())
}
func (m model) dnsView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("DNS"))
	b.WriteString("\n\n")
	b.WriteString(theme.Normal.Render("  Connection  ") + theme.Selected.Render(m.info.SSID) + "\n\n")
	for i, p := range dnsPresets {
		cursor := "  "
		if i == m.dnsCursor {
			cursor = theme.Selected.Render("› ")
		}
		b.WriteString(cursor + p.Name + "\n")
	}
	if m.loading {
		b.WriteString("\n" + theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("applying..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + theme.Error.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + theme.Dimmed.Render("  enter select   ↑↓ navigate   esc back"))
	return m.place(b.String())
}
func (m model) customDNSView() string {
	labels := []string{"IPv4 primary", "IPv4 secondary", "IPv6 primary", "IPv6 secondary"}
	var b strings.Builder
	b.WriteString(theme.Header.Render("CUSTOM DNS"))
	b.WriteString("\n\n")
	for i, l := range labels {
		cursor := "  "
		if i == m.customCursor {
			cursor = theme.Selected.Render("› ")
		}
		val := m.customDNS[i]
		if val == "" {
			val = theme.Dimmed.Render("enter address")
		}
		b.WriteString(cursor + theme.Normal.Render(l+"  ") + val + "\n")
	}
	if m.loading {
		b.WriteString("\n" + theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("applying DNS..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + theme.Error.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + theme.Dimmed.Render("  ↑↓ select   tab next   enter apply   esc cancel"))
	return m.place(b.String())
}
func (m model) detailsView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("CONNECTION DETAILS"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("loading...") + "\n")
	}
	rows := [][2]string{{"SSID", m.info.SSID}, {"Device", m.info.Device}, {"BSSID", m.info.BSSID}, {"Signal", fmt.Sprintf("%d%%", m.info.Signal)}, {"Security", m.info.Security}, {"IPv4", m.info.IPv4}, {"Gateway", m.info.Gateway}, {"DNS IPv4", m.info.DNSv4}, {"DNS IPv6", m.info.DNSv6}}
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-12s %s\n", r[0], theme.Normal.Render(r[1])))
	}
	if m.err != nil {
		b.WriteString("\n" + theme.Error.Render("× "+m.err.Error()))
	}
	b.WriteString("\n" + theme.Dimmed.Render("esc back"))
	return m.place(b.String())
}
func (m model) shareView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("SHARE NETWORK"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("generating QR code..."))
		return m.place(b.String())
	}
	b.WriteString(okStyle.Render("  "+m.info.SSID) + "\n")
	b.WriteString(theme.Dimmed.Render("  scan this QR code with your phone") + "\n\n")
	if m.qr != "" {
		var buf bytes.Buffer
		cfg := qrterminal.Config{Level: qrterminal.M, Writer: &buf, HalfBlocks: true, QuietZone: 1}
		qrterminal.GenerateWithConfig(m.qr, cfg)
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + theme.Dimmed.Render("esc back"))
	if m.err != nil {
		b.WriteString("\n\n" + theme.Error.Render("× "+m.err.Error()))
	}
	return m.place(b.String())
}
func (m model) forgetView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("FORGET NETWORK"))
	b.WriteString("\n\n")
	b.WriteString(theme.Normal.Render("  Forget ") + theme.Selected.Render(m.selected.SSID) + theme.Normal.Render("?\n\n  This removes its saved NetworkManager profile.\n\n"))
	if m.loading {
		b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("forgetting...") + "\n\n")
	}
	b.WriteString(theme.Selected.Render("  [ enter ] forget"))
	b.WriteString("   " + theme.Dimmed.Render("[ esc ] cancel"))
	return m.place(b.String())
}

func (m model) speedTestView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("SPEED TEST"))
	b.WriteString("\n\n")

	switch {
	case m.speedRunning:
		stage := stageLatency
		if m.speedProgress != nil {
			stage = m.speedProgress.stage.Load()
		}
		switch stage {
		case stageLatency:
			b.WriteString(theme.Spinner(m.spinFrame) + " " + theme.Dimmed.Render("measuring latency..."))
		case stageDownload, stageUpload:
			label := "↓ download"
			if stage == stageUpload {
				label = "↑ upload"
			}
			done := m.speedProgress.bytesDone.Load()
			total := m.speedProgress.totalBytes.Load()
			frac := 0.0
			if total > 0 {
				frac = float64(done) / float64(total)
				if frac > 1 {
					frac = 1
				}
			}
			b.WriteString(theme.Normal.Render("  " + label))
			b.WriteString("\n\n  " + theme.Meter(40, frac, theme.Pink, theme.Faint))
			b.WriteString("\n  " + theme.Dimmed.Render(fmt.Sprintf("%.1f / %.1f MB", float64(done)/1_000_000, float64(total)/1_000_000)))
			if len(m.samples) > 1 {
				b.WriteString("\n\n  " + theme.Sparkline(m.samples, 40, theme.Cyan))
				b.WriteString("\n  " + theme.Grayed.Render(fmt.Sprintf("%.1f Mbps live", m.samples[len(m.samples)-1])))
			}
		}
		b.WriteString("\n\n" + theme.Dimmed.Render("  esc cancel"))

	case m.speedResult.err != nil:
		b.WriteString(theme.Error.Render("× " + m.speedResult.err.Error()))
		b.WriteString("\n\n" + theme.Dimmed.Render("  t retry   esc back"))

	case m.speedResult.downloadMbps > 0:
		b.WriteString(theme.Normal.Render("  Latency   ") + fmt.Sprintf("%.0f ms", m.speedResult.latencyMs) + "\n")
		b.WriteString(theme.Normal.Render("  Download  ") + okStyle.Render(fmt.Sprintf("%.1f Mbps", m.speedResult.downloadMbps)) + "\n")
		b.WriteString(theme.Normal.Render("  Upload    ") + okStyle.Render(fmt.Sprintf("%.1f Mbps", m.speedResult.uploadMbps)) + "\n")
		b.WriteString("\n" + theme.Dimmed.Render("  t run again   esc back"))

	default:
		b.WriteString(theme.Dimmed.Render("  press t to run a speed test"))
	}
	return m.place(b.String())
}

func signalBars(signal int) string {
	switch {
	case signal >= 80:
		return okStyle.Render("▰▰▰▰")
	case signal >= 60:
		return theme.DotOn.Render("▰▰▰▱")
	case signal >= 40:
		return theme.Header.Render("▰▰▱▱")
	case signal >= 20:
		return theme.Dimmed.Render("▰▱▱▱")
	default:
		return theme.Dimmed.Render("▱▱▱▱")
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dump" {
		dumpSample()
		return
	}
	if _, err := exec.LookPath("nmcli"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-networking: nmcli was not found.")
		fmt.Fprintln(os.Stderr, "Install NetworkManager first.")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-networking: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(50 * time.Millisecond)
}

// dumpSample renders the key screens with fake data to stdout, for
// headless visual checks without nmcli or a TTY.
func dumpSample() {
	m := initialModel()
	m.width, m.height = 80, 24
	m.loading = false
	m.networks = []Network{
		{SSID: "wired-uplink", Signal: 92, Security: "WPA2", InUse: true},
		{SSID: "ghost-ap", Signal: 61, Security: "WPA2"},
		{SSID: "open-cafe", Signal: 34, Security: ""},
	}
	m.info = ConnectionInfo{SSID: "wired-uplink", Signal: 92, Security: "WPA2", Device: "wlan0"}
	m.tx = theme.Transmission{Visible: true, Clean: "present day, present time...", Text: "present day, present time..."}
	fmt.Println(m.View())

	m.screen = screenSpeedTest
	m.speedRunning = true
	m.speedProgress = newSpeedProgress()
	m.speedProgress.stage.Store(stageDownload)
	m.speedProgress.bytesDone.Store(25_000_000)
	m.speedProgress.totalBytes.Store(speedTestDownloadBytes)
	m.samples = []float64{12, 18, 24, 31, 28, 35, 42, 38, 45, 51, 47, 55}
	fmt.Println(m.View())
}
