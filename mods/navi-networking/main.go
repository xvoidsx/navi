package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"
)

// ─────────────────────────────────────────────────────────────────────────
// Styles
// ─────────────────────────────────────────────────────────────────────────

var (
	neonPink  = lipgloss.Color("#ff10f0")
	neonGreen = lipgloss.Color("#39ff14")
	cyan      = lipgloss.Color("#00ffff")
	dim       = lipgloss.Color("#444444")
	gray      = lipgloss.Color("#666666")
	white     = lipgloss.Color("#ffffff")
	dark      = lipgloss.Color("#0f0f0f")
	red       = lipgloss.Color("#ff3131")
	violet    = lipgloss.Color("#bf5fff")
	ghost     = lipgloss.Color("#7a4a7a")
)

var (
	titleStyle     = lipgloss.NewStyle().Foreground(neonPink).Background(dark).Bold(true)
	logoStyle      = lipgloss.NewStyle().Foreground(neonGreen).Background(dark).Bold(true)
	headerStyle    = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	selectedStyle  = lipgloss.NewStyle().Foreground(neonPink).Bold(true)
	normalStyle    = lipgloss.NewStyle().Foreground(white)
	dimStyle       = lipgloss.NewStyle().Foreground(dim)
	grayStyle      = lipgloss.NewStyle().Foreground(gray)
	connectedStyle = lipgloss.NewStyle().Foreground(neonGreen).Bold(true)
	errorStyle     = lipgloss.NewStyle().Foreground(red).Bold(true)
	borderStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Background(dark).Padding(0, 1)
	inputStyle     = lipgloss.NewStyle().Foreground(neonPink).Bold(true)
	spinnerStyle   = lipgloss.NewStyle().Foreground(neonPink)
	dotOnStyle     = lipgloss.NewStyle().Foreground(neonGreen)
	dotOffStyle    = lipgloss.NewStyle().Foreground(red)

	lainCleanStyle   = lipgloss.NewStyle().Foreground(neonPink).Italic(true)
	lainGlitchStyle  = lipgloss.NewStyle().Foreground(violet).Italic(true)
	lainStaticStyle  = lipgloss.NewStyle().Foreground(ghost).Italic(true)
	lainResolveStyle = lipgloss.NewStyle().Foreground(cyan).Italic(true)

	frameWidth = 62
)

// lainRand drives the ambient message ticker below. Kept separate from a
// global math/rand source so this doesn't depend on Go-version-specific
// auto-seeding behavior.
var lainRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// lainPhrases are short transmissions that periodically materialize and
// dissolve in the header, a nod to Serial Experiments Lain. Keep each one
// under ~50 chars so it never wraps inside the frame.
var lainPhrases = []string{
	"connecting to the Wired...",
	"no matter where you go, everyone's connected",
	"the Wired is everywhere",
	"close the world, open the nExt",
	"present day, present time...",
	"layer 07 accessed",
	"you are receiving this",
	"protocol seven initiated",
	"the boundary is thinning",
	"Navi is watching",
	"this world is not the only one",
	"identity: unresolved",
	"let's all love lain",
	"the network remembers you",
	"god is on line two",
	"who's there?",
	"reality is a matter of consensus",
	"I am here. I have always been here.",
	"do you wanna be a god?",
	"the body is only a terminal",
	"information wants a body",
	"your Navi knows your name",
	"signal without a source",
	"don't confuse the layers",
	"full range. full motion.",
	"Chisa is still online",
	"Eiri is only code now",
	"a voice in the power lines",
	"you left a ghost in the cache",
	"layer 01: WEIRD",
	"layer 13: EGO",
	"to be everywhere is to be nowhere",
	"the city is a circuit board",
	"sleep is just a disconnect",
	"who is editing you?",
}

var lainRare = []string{
	"I saw you through the other screen",
	"stop looking for the operator",
	"this message is older than the device",
	"you already accepted the handshake",
	"there is no logout from here",
}

const (
	lainGlitchSteps = 10
	lainHoldMin     = 5
	lainHoldExtra   = 6
)

type lainMood int

const (
	lainMoodClean lainMood = iota
	lainMoodGlitch
	lainMoodStatic
	lainMoodResolve
)

func pickLainPhrase(prev string) string {
	pool := lainPhrases
	if lainRand.Intn(9) == 0 {
		pool = lainRare
	}
	next := pool[lainRand.Intn(len(pool))]
	for next == prev && len(pool) > 1 {
		next = pool[lainRand.Intn(len(pool))]
	}
	return next
}

func glitchify(s string, intensity float64) string {
	if intensity <= 0 {
		return s
	}
	noise := []rune("░▒▓█#%&@?▌▐▄▀╱╲╳·")
	runes := []rune(s)
	out := make([]rune, len(runes))
	for i, r := range runes {
		if r == ' ' {
			out[i] = r
			continue
		}
		if lainRand.Float64() < intensity {
			out[i] = noise[lainRand.Intn(len(noise))]
		} else {
			out[i] = r
		}
	}
	return string(out)
}

func scrambleCase(s string, intensity float64) string {
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) {
			continue
		}
		if lainRand.Float64() < intensity {
			if unicode.IsUpper(r) {
				runes[i] = unicode.ToLower(r)
			} else {
				runes[i] = unicode.ToUpper(r)
			}
		}
	}
	return string(runes)
}

func padRunes(s string, n int) []rune {
	r := []rune(s)
	if len(r) >= n {
		return r
	}
	out := make([]rune, n)
	copy(out, r)
	for i := len(r); i < n; i++ {
		out[i] = ' '
	}
	return out
}

// glitchMix crossfades two strings through a noise peak so the header
// never blanks between transmissions.
func glitchMix(from, to string, t float64) string {
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	a, b := []rune(from), []rune(to)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	fa, fb := padRunes(from, n), padRunes(to, n)
	out := make([]rune, n)
	noise := []rune("░▒▓█#%&@?▌▐")
	peak := 1 - 2*math.Abs(t-0.5)
	if peak < 0 {
		peak = 0
	}
	for i := 0; i < n; i++ {
		var src rune
		if t < 0.5 {
			src = fa[i]
		} else {
			src = fb[i]
		}
		switch {
		case src == ' ':
			if peak > 0.7 && lainRand.Intn(8) == 0 {
				out[i] = noise[lainRand.Intn(len(noise))]
			} else {
				out[i] = ' '
			}
		case lainRand.Float64() < peak*0.9:
			out[i] = noise[lainRand.Intn(len(noise))]
		default:
			out[i] = src
		}
	}
	_ = a
	_ = b
	return string(out)
}

func divider() string {
	return dimStyle.Render(strings.Repeat("─", frameWidth-2))
}

// footer renders the persistent hotkey bar. Every item is shown all the
// time (per navi's UX preference); items that need an active connection
// are dimmed further when one isn't available, rather than disappearing,
// so the keymap never shifts under the user's fingers.
func footer(active bool, items ...[2]string) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		key, label := it[0], it[1]
		style := dimStyle
		if !active {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
		}
		parts = append(parts, style.Render(key+" "+label))
	}
	return strings.Join(parts, "   ")
}

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

type lainShowMsg struct{ text string }
type lainGlitchTickMsg struct{}
type lainHoldDoneMsg struct{}
type lainFlickerMsg struct{}

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

	spinner  spinner.Model
	progress progress.Model

	// cancel, when non-nil, aborts whatever background operation is
	// currently in flight (scan, connect, speed test, ...). Pressing esc
	// while loading calls this instead of quitting the program outright.
	cancel context.CancelFunc

	speedProgress *speedProgress
	speedResult   speedTestResult
	speedRunning  bool

	lainText    string
	lainFrom    string
	lainClean   string
	lainVisible bool
	lainFrame   int
	lainMood    lainMood
	lainBusy    bool
}

func initialModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	p := progress.New(
		progress.WithGradient("#ff10f0", "#39ff14"),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	return model{loading: true, spinner: s, progress: p}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, lainFirstCmd(), func() tea.Msg { return startMsg{} })
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

func lainFirstCmd() tea.Cmd {
	return tea.Tick(900*time.Millisecond, func(time.Time) tea.Msg {
		return lainShowMsg{text: pickLainPhrase("")}
	})
}

func lainGlitchCmd() tea.Cmd {
	return tea.Tick(55*time.Millisecond, func(time.Time) tea.Msg {
		return lainGlitchTickMsg{}
	})
}

func lainHoldCmd() tea.Cmd {
	d := time.Duration(lainHoldMin+lainRand.Intn(lainHoldExtra)) * time.Second
	return tea.Tick(d, func(time.Time) tea.Msg { return lainHoldDoneMsg{} })
}

func lainFlickerCmd() tea.Cmd {
	d := time.Duration(1400+lainRand.Intn(2600)) * time.Millisecond
	return tea.Tick(d, func(time.Time) tea.Msg { return lainFlickerMsg{} })
}

// startOp creates a fresh cancellable context, stashes its cancel func on
// the model so a subsequent esc can abort the operation, and returns the
// context for use in the accompanying command.
func (m *model) startOp() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return ctx
}

// ─────────────────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

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
		m.screen = screenNetworks
		m.loading = true
		ctx := m.startOp()
		return m, tea.Batch(scanCmd(ctx), infoCmd(ctx))

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
		m.screen = screenNetworks
		m.err = nil
		ctx := m.startOp()
		return m, scanCmd(ctx)

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
		m.screen = screenDashboard
		ctx := m.startOp()
		return m, infoCmd(ctx)

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
		m.screen = screenNetworks
		ctx := m.startOp()
		return m, scanCmd(ctx)

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
		if m.speedRunning {
			return m, speedTickCmd()
		}

	case lainShowMsg:
		m.lainFrom = m.lainClean
		if strings.TrimSpace(m.lainFrom) == "" {
			m.lainFrom = strings.Repeat(" ", len([]rune(msg.text)))
		}
		m.lainClean = msg.text
		m.lainVisible = true
		m.lainBusy = true
		m.lainFrame = 0
		m.lainMood = lainMoodStatic
		m.lainText = glitchMix(m.lainFrom, m.lainClean, 0.05)
		return m, lainGlitchCmd()

	case lainGlitchTickMsg:
		if !m.lainBusy {
			return m, nil
		}
		m.lainFrame++
		t := float64(m.lainFrame) / float64(lainGlitchSteps)
		if t >= 1 {
			m.lainText = m.lainClean
			m.lainMood = lainMoodClean
			m.lainBusy = false
			return m, tea.Batch(lainHoldCmd(), lainFlickerCmd())
		}
		mixed := glitchMix(m.lainFrom, m.lainClean, t)
		switch {
		case t < 0.35:
			m.lainMood = lainMoodStatic
			m.lainText = glitchify(mixed, 0.85)
		case t < 0.7:
			m.lainMood = lainMoodGlitch
			m.lainText = scrambleCase(mixed, 0.45)
		default:
			m.lainMood = lainMoodResolve
			m.lainText = glitchify(mixed, 0.18)
		}
		return m, lainGlitchCmd()

	case lainHoldDoneMsg:
		if m.lainBusy {
			return m, nil
		}
		return m, func() tea.Msg {
			return lainShowMsg{text: pickLainPhrase(m.lainClean)}
		}

	case lainFlickerMsg:
		if m.lainBusy || !m.lainVisible || m.lainClean == "" {
			return m, nil
		}
		// A brief nervous twitch while the phrase sits. Resolves on the
		// next flicker tick unless a real transition has started.
		if lainRand.Intn(3) == 0 {
			m.lainMood = lainMoodGlitch
			m.lainText = scrambleCase(glitchify(m.lainClean, 0.22), 0.3)
		} else {
			m.lainMood = lainMoodClean
			m.lainText = m.lainClean
		}
		return m, lainFlickerCmd()

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
				return m, nil
			}
		}
		switch m.screen {
		case screenNetworks:
			return m.updateNetworks(msg)
		case screenPassword:
			return m.updatePassword(msg)
		case screenOpenConfirm:
			return m.updateOpenConfirm(msg)
		case screenDashboard:
			return m.updateDashboard(msg)
		case screenDNS:
			return m.updateDNS(msg)
		case screenCustomDNS:
			return m.updateCustomDNS(msg)
		case screenDetails:
			return m.updateDetails(msg)
		case screenShare:
			if msg.String() == "esc" || msg.String() == "q" {
				m.screen = screenDashboard
				m.qr = ""
			}
			return m, nil
		case screenForgetConfirm:
			return m.updateForget(msg)
		case screenSpeedTest:
			return m.updateSpeedTest(msg)
		}
	}
	return m, nil
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

func (m model) lainRow() string {
	text := " "
	if m.lainVisible && m.lainText != "" {
		text = " " + m.lainText
	}
	style := lainCleanStyle
	switch m.lainMood {
	case lainMoodGlitch:
		style = lainGlitchStyle
	case lainMoodStatic:
		style = lainStaticStyle
	case lainMoodResolve:
		style = lainResolveStyle
	}
	return style.Width(frameWidth).Render(text)
}

func (m model) frame(content string) string {
	dot := dotOffStyle.Render("●")
	if m.info.SSID != "" {
		dot = dotOnStyle.Render("●")
	}
	title := logoStyle.Render(" ∅ ") + titleStyle.Render(" navi networking")
	headerLine := lipgloss.NewStyle().Width(frameWidth - 4).Render(title)
	header := lipgloss.JoinHorizontal(lipgloss.Top, headerLine, dot+"  ")

	body := lipgloss.NewStyle().Width(frameWidth).Render(content)
	return borderStyle.Render(header + "\n" + m.lainRow() + "\n" + body)
}
func (m model) place(content string) string {
	if m.width == 0 {
		return content
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.frame(content))
}

// networksFooter is the full, persistent hotkey bar shared by the network
// list and the dashboard, so the available commands never shift between
// the two screens.
func (m model) networksFooter() string {
	active := m.info.SSID != ""
	line1 := footer(true,
		[2]string{"↑↓", "navigate"},
		[2]string{"enter", "connect"},
		[2]string{"r", "rescan"},
	)
	line2 := footer(active,
		[2]string{"d", "dns"},
		[2]string{"i", "details"},
		[2]string{"s", "share"},
		[2]string{"x", "disconnect"},
		[2]string{"f", "forget"},
		[2]string{"t", "speed"},
	)
	return line1 + "\n" + line2 + "\n" + dimStyle.Render("q quit")
}

func (m model) networkView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("NETWORKS"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(m.spinner.View() + " " + dimStyle.Render("scanning for networks..."))
		b.WriteString("\n")
	} else if len(m.networks) == 0 {
		b.WriteString(dimStyle.Render("  no Wi-Fi networks found"))
		b.WriteString("\n")
	} else {
		for i, n := range m.networks {
			cursor := "  "
			if i == m.cursor {
				cursor = selectedStyle.Render("› ")
			}
			name := n.SSID
			if n.InUse {
				name = connectedStyle.Render(name)
			} else if i == m.cursor {
				name = selectedStyle.Render(name)
			}
			sec := n.Security
			if sec == "" {
				sec = "OPEN"
			}
			b.WriteString(fmt.Sprintf("%s%-28s %s  %s", cursor, name, signalBars(n.Signal), dimStyle.Render(sec)))
			if n.InUse {
				b.WriteString("  " + connectedStyle.Render("●"))
			}
			b.WriteString("\n")
		}
	}
	if m.message != "" {
		b.WriteString("\n" + connectedStyle.Render("✓ "+m.message) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + errorStyle.Render("× "+m.err.Error()) + "\n")
	}
	b.WriteString("\n" + divider() + "\n")
	b.WriteString(m.networksFooter())
	return m.place(b.String())
}
func (m model) passwordView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("CONNECT"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("  Network  ") + selectedStyle.Render(m.selected.SSID) + "\n")
	b.WriteString(dimStyle.Render("  Security  ") + normalStyle.Render(m.selected.Security) + "\n\n")
	b.WriteString(normalStyle.Render("  Password") + "\n\n")
	b.WriteString(inputStyle.Render("  > " + strings.Repeat("•", len([]rune(m.password)))))
	if m.loading {
		b.WriteString("\n\n" + m.spinner.View() + " " + dimStyle.Render("connecting..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + errorStyle.Render("  × "+m.err.Error()))
	}
	b.WriteString("\n\n" + dimStyle.Render("  enter connect   esc cancel"))
	return m.place(b.String())
}
func (m model) openConfirmView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("CONNECT"))
	b.WriteString("\n\n")
	b.WriteString(selectedStyle.Render("  "+m.selected.SSID) + "\n\n")
	b.WriteString(errorStyle.Render("  OPEN NETWORK") + "\n\n")
	b.WriteString(normalStyle.Render("  This network has no password."))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  navi will never join an open network without"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  your explicit confirmation."))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("  Signal  ") + signalBars(m.selected.Signal) + "\n")
	b.WriteString(normalStyle.Render("  BSSID   ") + grayStyle.Render(m.selected.BSSID) + "\n\n")
	if m.loading {
		b.WriteString(m.spinner.View() + " " + dimStyle.Render("connecting...") + "\n\n")
	}
	b.WriteString(selectedStyle.Render("  [ enter ] connect"))
	b.WriteString("   " + dimStyle.Render("[ esc ] cancel"))
	return m.place(b.String())
}
func (m model) dashboardView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("CONNECTION"))
	b.WriteString("\n\n")
	if m.info.SSID == "" {
		b.WriteString(dimStyle.Render("  no active Wi-Fi connection"))
	} else {
		b.WriteString(connectedStyle.Render("  ● "+m.info.SSID) + "\n\n")
		b.WriteString(normalStyle.Render("  Signal    ") + signalBars(m.info.Signal) + "\n")
		b.WriteString(normalStyle.Render("  Security  ") + m.info.Security + "\n")
		b.WriteString(normalStyle.Render("  Device    ") + m.info.Device + "\n")
		b.WriteString(normalStyle.Render("  BSSID     ") + grayStyle.Render(m.info.BSSID) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + errorStyle.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + divider() + "\n")
	b.WriteString(m.networksFooter())
	return m.place(b.String())
}
func (m model) dnsView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("DNS"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("  Connection  ") + selectedStyle.Render(m.info.SSID) + "\n\n")
	for i, p := range dnsPresets {
		cursor := "  "
		if i == m.dnsCursor {
			cursor = selectedStyle.Render("› ")
		}
		b.WriteString(cursor + p.Name + "\n")
	}
	if m.loading {
		b.WriteString("\n" + m.spinner.View() + " " + dimStyle.Render("applying..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + errorStyle.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + dimStyle.Render("  enter select   ↑↓ navigate   esc back"))
	return m.place(b.String())
}
func (m model) customDNSView() string {
	labels := []string{"IPv4 primary", "IPv4 secondary", "IPv6 primary", "IPv6 secondary"}
	var b strings.Builder
	b.WriteString(headerStyle.Render("CUSTOM DNS"))
	b.WriteString("\n\n")
	for i, l := range labels {
		cursor := "  "
		if i == m.customCursor {
			cursor = selectedStyle.Render("› ")
		}
		val := m.customDNS[i]
		if val == "" {
			val = dimStyle.Render("enter address")
		}
		b.WriteString(cursor + normalStyle.Render(l+"  ") + val + "\n")
	}
	if m.loading {
		b.WriteString("\n" + m.spinner.View() + " " + dimStyle.Render("applying DNS..."))
	}
	if m.err != nil {
		b.WriteString("\n\n" + errorStyle.Render("× "+m.err.Error()))
	}
	b.WriteString("\n\n" + dimStyle.Render("  ↑↓ select   tab next   enter apply   esc cancel"))
	return m.place(b.String())
}
func (m model) detailsView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("CONNECTION DETAILS"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(m.spinner.View() + " " + dimStyle.Render("loading...") + "\n")
	}
	rows := [][2]string{{"SSID", m.info.SSID}, {"Device", m.info.Device}, {"BSSID", m.info.BSSID}, {"Signal", fmt.Sprintf("%d%%", m.info.Signal)}, {"Security", m.info.Security}, {"IPv4", m.info.IPv4}, {"Gateway", m.info.Gateway}, {"DNS IPv4", m.info.DNSv4}, {"DNS IPv6", m.info.DNSv6}}
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-12s %s\n", r[0], normalStyle.Render(r[1])))
	}
	if m.err != nil {
		b.WriteString("\n" + errorStyle.Render("× "+m.err.Error()))
	}
	b.WriteString("\n" + dimStyle.Render("esc back"))
	return m.place(b.String())
}
func (m model) shareView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("SHARE NETWORK"))
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString(m.spinner.View() + " " + dimStyle.Render("generating QR code..."))
		return m.place(b.String())
	}
	b.WriteString(connectedStyle.Render("  "+m.info.SSID) + "\n")
	b.WriteString(dimStyle.Render("  scan this QR code with your phone") + "\n\n")
	if m.qr != "" {
		var buf bytes.Buffer
		cfg := qrterminal.Config{Level: qrterminal.M, Writer: &buf, HalfBlocks: true, QuietZone: 1}
		qrterminal.GenerateWithConfig(m.qr, cfg)
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render("esc back"))
	if m.err != nil {
		b.WriteString("\n\n" + errorStyle.Render("× "+m.err.Error()))
	}
	return m.place(b.String())
}
func (m model) forgetView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("FORGET NETWORK"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("  Forget ") + selectedStyle.Render(m.selected.SSID) + normalStyle.Render("?\n\n  This removes its saved NetworkManager profile.\n\n"))
	if m.loading {
		b.WriteString(m.spinner.View() + " " + dimStyle.Render("forgetting...") + "\n\n")
	}
	b.WriteString(selectedStyle.Render("  [ enter ] forget"))
	b.WriteString("   " + dimStyle.Render("[ esc ] cancel"))
	return m.place(b.String())
}

func (m model) speedTestView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("SPEED TEST"))
	b.WriteString("\n\n")

	switch {
	case m.speedRunning:
		stage := stageLatency
		if m.speedProgress != nil {
			stage = m.speedProgress.stage.Load()
		}
		switch stage {
		case stageLatency:
			b.WriteString(m.spinner.View() + " " + dimStyle.Render("measuring latency..."))
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
			b.WriteString(normalStyle.Render("  " + label))
			b.WriteString("\n\n  " + m.progress.ViewAs(frac))
			b.WriteString("\n  " + dimStyle.Render(fmt.Sprintf("%.1f / %.1f MB", float64(done)/1_000_000, float64(total)/1_000_000)))
		}
		b.WriteString("\n\n" + dimStyle.Render("  esc cancel"))

	case m.speedResult.err != nil:
		b.WriteString(errorStyle.Render("× " + m.speedResult.err.Error()))
		b.WriteString("\n\n" + dimStyle.Render("  t retry   esc back"))

	case m.speedResult.downloadMbps > 0:
		b.WriteString(normalStyle.Render("  Latency   ") + fmt.Sprintf("%.0f ms", m.speedResult.latencyMs) + "\n")
		b.WriteString(normalStyle.Render("  Download  ") + connectedStyle.Render(fmt.Sprintf("%.1f Mbps", m.speedResult.downloadMbps)) + "\n")
		b.WriteString(normalStyle.Render("  Upload    ") + connectedStyle.Render(fmt.Sprintf("%.1f Mbps", m.speedResult.uploadMbps)) + "\n")
		b.WriteString("\n" + dimStyle.Render("  t run again   esc back"))

	default:
		b.WriteString(dimStyle.Render("  press t to run a speed test"))
	}
	return m.place(b.String())
}

func signalBars(signal int) string {
	switch {
	case signal >= 80:
		return connectedStyle.Render("▰▰▰▰")
	case signal >= 60:
		return lipgloss.NewStyle().Foreground(neonGreen).Render("▰▰▰▱")
	case signal >= 40:
		return lipgloss.NewStyle().Foreground(cyan).Render("▰▰▱▱")
	case signal >= 20:
		return dimStyle.Render("▰▱▱▱")
	default:
		return dimStyle.Render("▱▱▱▱")
	}
}

func main() {
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
