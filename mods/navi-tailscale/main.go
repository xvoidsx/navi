package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"
	theme "github.com/rav3ndust/navi-theme"
)

// ─────────────────────────────────────────────────────────────────────────
// navi-tailscale — a native Tailscale manager for navi.
//
// Shows tailnet status, peers, and offers up/down/ping/ssh actions.
// Wraps the `tailscale` CLI (JSON status) — no new daemons, no new deps.
//
// Philosophy: Tailscale is the mesh; this is navi's window into it.
// ─────────────────────────────────────────────────────────────────────────

var frameWidth = 62

// ── Domain types ─────────────────────────────────────────────────────────

type Peer struct {
	Name    string
	IP      string
	OS      string
	Online  bool
	IsSelf  bool
	IsExit  bool
	Latency string // filled by ping, "" if unknown
}

type TailnetStatus struct {
	Up       bool
	Tailnet  string
	Self     Peer
	Peers    []Peer
	LoggedIn bool
}

// tailscale status --json shape (subset we care about)
type tsStatusJSON struct {
	BackendState string `json:"BackendState"`
	Self         *tsNode `json:"Self"`
	Peer         map[string]*tsNode `json:"Peer"`
	CurrentTailnet *tsTailnet `json:"CurrentTailnet"`
}

type tsNode struct {
	DNSName    string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	OS         string   `json:"OS"`
	Online     *bool    `json:"Online"`
	ExitNode   bool     `json:"ExitNode"`
}

type tsTailnet struct {
	Name string `json:"Name"`
}

// ── Backend ──────────────────────────────────────────────────────────────

func tailscaleStatus() (TailnetStatus, error) {
	var st TailnetStatus
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		// not logged in or tailscaled not running
		st.LoggedIn = false
		return st, nil
	}
	var js tsStatusJSON
	if err := json.Unmarshal(out, &js); err != nil {
		return st, fmt.Errorf("parse tailscale status: %w", err)
	}
	st.LoggedIn = true
	st.Up = js.BackendState == "Running"
	if js.CurrentTailnet != nil {
		st.Tailnet = js.CurrentTailnet.Name
	}
	if js.Self != nil {
		st.Self = nodeToPeer(js.Self, true)
	}
	for _, n := range js.Peer {
		if n == nil {
			continue
		}
		st.Peers = append(st.Peers, nodeToPeer(n, false))
	}
	return st, nil
}

func nodeToPeer(n *tsNode, self bool) Peer {
	p := Peer{IsSelf: self}
	// DNSName is like "hostname.tailnet.ts.net" — take the short name
	p.Name = strings.SplitN(n.DNSName, ".", 2)[0]
	if len(n.TailscaleIPs) > 0 {
		p.IP = n.TailscaleIPs[0]
	}
	p.OS = n.OS
	p.IsExit = n.ExitNode
	if n.Online != nil {
		p.Online = *n.Online
	} else {
		p.Online = true // assume online if field absent
	}
	return p
}

func tailscaleUp() error {
	cmd := exec.Command("tailscale", "up")
	cmd.Stdin = nil
	return cmd.Run()
}

// tailscaleInstalled reports whether the tailscale binary is on PATH.
func tailscaleInstalled() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// tailscaledRunning reports whether the tailscaled daemon is reachable.
// We run a cheap status call and look for the daemon-connection failure.
func tailscaledRunning() bool {
	cmd := exec.Command("tailscale", "status")
	// capture stderr: "failed to connect to local tailscaled" means down
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil
	_ = cmd.Run()
	errText := strings.ToLower(stderr.String())
	return !strings.Contains(errText, "tailscaled") ||
		!strings.Contains(errText, "failed to connect")
}

// startTailscaled brings the daemon up via systemd.
func startTailscaled() tea.Msg {
	cmd := exec.Command("doas", "systemctl", "start", "tailscaled")
	if err := cmd.Run(); err != nil {
		// fall back to plain systemctl (may work for user units or with sudo config)
		if err2 := exec.Command("systemctl", "start", "tailscaled").Run(); err2 != nil {
			return daemonMsg{fmt.Errorf("could not start tailscaled: %w", err)}
		}
	}
	// give the daemon a moment to come up
	time.Sleep(1500 * time.Millisecond)
	return daemonMsg{nil}
}

// installTailscale runs the navi tailscale installer.
func installTailscale() tea.Msg {
	// prefer the shipped installer script, fall back to navi-extras
	installer := "/usr/share/navi/installers/tailscale-installer.sh"
	if _, err := os.Stat(installer); err != nil {
		installer = ""
	}
	var cmd *exec.Cmd
	if installer != "" {
		cmd = exec.Command("doas", "bash", installer)
	} else {
		cmd = exec.Command("doas", "navi-extras", "--install", "tailscale")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return installMsg{fmt.Errorf("install failed: %s", strings.TrimSpace(string(out)))}
	}
	return installMsg{nil}
}

// pollAuth checks whether the device has joined the tailnet yet.
func pollAuth() tea.Msg {
	st, _ := tailscaleStatus()
	return authPollMsg{st.LoggedIn && st.Up}
}

// openAuthURL opens the tailscale login URL in the default browser.
func openAuthURL(url string) tea.Msg {
	_ = exec.Command("xdg-open", url).Start()
	return nil
}

// spinnerTick advances the busy spinner.
func spinnerTick() tea.Msg {
	time.Sleep(120 * time.Millisecond)
	return spinnerTickMsg{}
}

// tailscaleUpCapture runs `tailscale up` and returns combined output so we
// can extract the login URL for the QR code.
func tailscaleUpCapture() (string, error) {
	cmd := exec.Command("tailscale", "up")
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, "permission") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "must be root") ||
		strings.Contains(lower, "operation not permitted") {
		cmd = exec.Command("doas", "tailscale", "up")
		cmd.Stdin = nil
		out, err = cmd.CombinedOutput()
	}
	return string(out), err
}

// extractLoginURL finds the tailscale login URL in `tailscale up` output.
func extractLoginURL(output string) string {
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "https://login.tailscale.com/") {
				return strings.Trim(field, "()[]")
			}
		}
	}
	return ""
}

// renderQR returns a text QR code for the given URL.
func renderQR(url string) string {
	var sb strings.Builder
	// small QR, suitable for terminal
	qrterminal.Generate(url, qrterminal.L, &sb)
	return sb.String()
}

func tailscaleDown() error {
	return exec.Command("tailscale", "down").Run()
}

// setExitNode routes traffic through the named peer ("" to clear).
func setExitNode(peer string) error {
	args := []string{"set", "--exit-node=" + peer}
	cmd := exec.Command("tailscale", args...)
	cmd.Stdin = nil
	return cmd.Run()
}

// currentExitNode parses `tailscale status --json` for the active exit node.
func currentExitNode() string {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}
	var js tsStatusJSON
	if err := json.Unmarshal(out, &js); err != nil {
		return ""
	}
	if js.Self != nil {
		// Self has ExitNodeOption / ExitNode fields in full JSON;
		// we check peer list for the one marked as our exit node.
		// The JSON has "Self": {"ExitNode": ...} — simplified here.
	}
	return ""
}

// listSendableFiles finds files in common locations for taildrop.
func listSendableFiles() []string {
	var files []string
	seen := map[string]bool{}
	dirs := []string{}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(h, "Downloads"),
			filepath.Join(h, "Documents"),
			filepath.Join(h, "Pictures"),
			h,
		)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
			if len(files) >= 50 {
				break
			}
		}
	}
	sort.Slice(files, func(i, j int) bool {
		ai, _ := os.Stat(files[i])
		aj, _ := os.Stat(files[j])
		if ai == nil || aj == nil {
			return files[i] < files[j]
		}
		return ai.ModTime().After(aj.ModTime())
	})
	return files
}

// taildropSend sends a file to a peer via `tailscale file cp`.
func taildropSend(file, peer string) error {
	cmd := exec.Command("tailscale", "file", "cp", file, peer+":")
	return cmd.Run()
}

// taildropReceive pulls waiting files into ~/Downloads.
func taildropReceive() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	dl := filepath.Join(home, "Downloads")
	out, err := exec.Command("tailscale", "file", "get", dl).CombinedOutput()
	if err != nil {
		// "no files" exits non-zero — treat as empty, not failure
		if strings.Contains(string(out), "no files") ||
			strings.Contains(strings.ToLower(string(out)), "empty") {
			return 0, nil
		}
		return 0, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	// count received files from output lines
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

func tailscalePing(ip string) (string, error) {
	out, err := exec.Command("tailscale", "ping", "-c", "3", ip).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ping failed")
	}
	// parse "pong from X: 12.3ms" — take the last latency
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "ms") {
			parts := strings.Fields(lines[i])
			for _, p := range parts {
				if strings.HasSuffix(p, "ms") {
					return p, nil
				}
			}
		}
	}
	return "?", nil
}

// ── Model ────────────────────────────────────────────────────────────────

type screen int

const (
	screenMain screen = iota
	screenConfirmDown
	screenAuth
	screenTaildrop
	screenTaildropInbox
	screenNotInstalled
	screenDaemonDown
)

type model struct {
	status   TailnetStatus
	err      string
	msg      string
	msgAt    time.Time
	cursor   int
	screen   screen
	quitting bool
	width    int
	height   int
	// auth flow
	authURL string
	authQR  string
	// setup flow
	busy        bool   // async operation in progress (spinner)
	busyMsg     string // what the spinner is doing
	spinnerTick int
	// taildrop
	tdFiles    []string
	tdCursor   int
	tdPeer     int // selected peer index for sending
	tdStep     int // 0=pick file, 1=pick peer, 2=confirm
	tdFile     string
	// exit node
	exitNode string
}

type statusMsg struct {
	st  TailnetStatus
	err error
}

type pingMsg struct {
	idx     int
	latency string
	err     error
}

type actionDoneMsg struct {
	msg string
	err error
}

type authMsg struct {
	url    string
	qr     string
	err    error
	output string
}

type taildropMsg struct {
	msg string
	err error
}

type installMsg struct {
	err error
}

type daemonMsg struct {
	err error
}

type authPollMsg struct {
	loggedIn bool
}

type spinnerTickMsg struct{}

func fetchStatus() tea.Msg {
	st, err := tailscaleStatus()
	return statusMsg{st, err}
}

func initialModel() model {
	return model{screen: screenMain}
}

func (m model) Init() tea.Cmd {
	// setup chain: installed? -> daemon? -> status
	return func() tea.Msg {
		if !tailscaleInstalled() {
			return installMsg{fmt.Errorf("not installed")}
		}
		if !tailscaledRunning() {
			return daemonMsg{fmt.Errorf("daemon down")}
		}
		return fetchStatus()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, tea.ClearScreen

	case tea.KeyMsg:
		return m.handleKey(msg)

	case statusMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.status = msg.st
			m.err = ""
		}
		return m, nil

	case pingMsg:
		if msg.err == nil && msg.idx < len(m.status.Peers) {
			m.status.Peers[msg.idx].Latency = msg.latency
			m.setMsg(fmt.Sprintf("ping %s: %s", m.status.Peers[msg.idx].Name, msg.latency))
		} else {
			m.setMsg("ping failed")
		}
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.setMsg("error: " + msg.err.Error())
		} else {
			m.setMsg(msg.msg)
		}
		// refresh status after any action
		return m, fetchStatus

	case authMsg:
		m.busy = false
		if msg.err != nil {
			errText := msg.err.Error()
			if strings.TrimSpace(msg.output) != "" {
				lines := strings.Split(strings.TrimSpace(msg.output), "\n")
				errText = lines[0]
				if len(errText) > 80 {
					errText = errText[:80] + "..."
				}
			}
			m.setMsg("auth failed: " + errText)
			m.screen = screenMain
		} else if msg.url != "" {
			m.authURL = msg.url
			m.authQR = msg.qr
			m.screen = screenAuth
			// start polling for completion
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
				return pollAuth()
			})
		} else {
			// already authenticated or no URL needed
			m.setMsg("connected to tailnet")
			m.screen = screenMain
		}
		return m, fetchStatus

	case taildropMsg:
		if msg.err != nil {
			m.setMsg("taildrop failed: " + msg.err.Error())
		} else {
			m.setMsg(msg.msg)
		}
		m.screen = screenMain
		return m, nil

	case installMsg:
		m.busy = false
		if msg.err != nil && msg.err.Error() == "not installed" {
			// initial check: tailscale isn't here
			m.screen = screenNotInstalled
			return m, nil
		}
		if msg.err != nil {
			m.setMsg(msg.err.Error())
			m.screen = screenNotInstalled
			return m, nil
		}
		// install succeeded — check the daemon next
		m.screen = screenMain
		return m, func() tea.Msg {
			if !tailscaledRunning() {
				return daemonMsg{fmt.Errorf("daemon down")}
			}
			return fetchStatus()
		}

	case daemonMsg:
		m.busy = false
		if msg.err != nil && msg.err.Error() == "daemon down" {
			// initial check or post-install: daemon isn't running.
			// auto-start it — the user installed tailscale to use it.
			m.screen = screenDaemonDown
			m.busy = true
			m.busyMsg = "starting tailscaled..."
			return m, tea.Batch(startTailscaled, spinnerTick)
		}
		if msg.err != nil {
			m.setMsg(msg.err.Error())
			m.screen = screenDaemonDown
			m.busy = false
			return m, nil
		}
		// daemon is up — fetch status
		m.screen = screenMain
		return m, fetchStatus

	case authPollMsg:
		if msg.loggedIn {
			// auth completed — clear the sensitive URL and go to main
			m.authURL = ""
			m.authQR = ""
			m.screen = screenMain
			m.setMsg("connected to tailnet")
			return m, fetchStatus
		}
		// not yet — keep polling while on the auth screen
		if m.screen == screenAuth {
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
				return pollAuth()
			})
		}
		return m, nil

	case spinnerTickMsg:
		if m.busy {
			m.spinnerTick++
			return m, spinnerTick
		}
		return m, nil
	}
	return m, nil
}

func (m *model) setMsg(s string) {
	m.msg = s
	m.msgAt = time.Now()
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// busy operations swallow keys (except ctrl+c)
	if m.busy && msg.String() != "ctrl+c" {
		return m, nil
	}
	switch m.screen {
	case screenNotInstalled:
		switch msg.String() {
		case "i", "I":
			// install tailscale
			m.busy = true
			m.busyMsg = "installing tailscale..."
			return m, tea.Batch(installTailscale, spinnerTick)
		case "q", "esc":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case screenDaemonDown:
		switch msg.String() {
		case "s", "S", "enter":
			// retry starting the daemon
			m.busy = true
			m.busyMsg = "starting tailscaled..."
			return m, tea.Batch(startTailscaled, spinnerTick)
		case "q", "esc":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case screenConfirmDown:
		switch msg.String() {
		case "y", "Y":
			m.screen = screenMain
			return m, func() tea.Msg {
				if err := tailscaleDown(); err != nil {
					return actionDoneMsg{"", err}
				}
				return actionDoneMsg{"disconnected from tailnet", nil}
			}
		case "n", "N", "esc":
			m.screen = screenMain
			return m, nil
		}
		return m, nil

	case screenAuth:
		switch msg.String() {
		case "o", "O":
			// open the auth URL in the default browser
			if m.authURL != "" {
				return m, func() tea.Msg { return openAuthURL(m.authURL) }
			}
			return m, nil
		case "u", "U":
			// regenerate the auth URL (old one may have expired)
			m.busy = true
			m.busyMsg = "requesting login link..."
			return m, tea.Batch(
				func() tea.Msg {
					out, err := tailscaleUpCapture()
					if err != nil {
						return authMsg{"", "", err, out}
					}
					url := extractLoginURL(out)
					if url == "" {
						return authMsg{"", "", nil, ""}
					}
					return authMsg{url, renderQR(url), nil, ""}
				},
				spinnerTick,
			)
		default:
			// any other key returns to main (auth happens in browser/phone)
			m.screen = screenMain
			m.authURL = ""
			m.authQR = ""
			return m, fetchStatus
		}

	case screenTaildrop:
		return m.handleTaildropKey(msg)

	case screenTaildropInbox:
		if msg.String() == "esc" || msg.String() == "q" {
			m.screen = screenMain
			return m, nil
		}
		return m, nil
	}

	// main screen
	n := len(m.status.Peers)
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < n-1 {
			m.cursor++
		}

	case "r":
		return m, fetchStatus

	case "u":
		// tailscale up with QR code auth flow
		m.busy = true
		m.busyMsg = "requesting login link..."
		return m, tea.Batch(
			func() tea.Msg {
				out, err := tailscaleUpCapture()
				if err != nil {
					return authMsg{"", "", err, out}
				}
				url := extractLoginURL(out)
				if url == "" {
					// no URL = already authenticated
					return authMsg{"", "", nil, ""}
				}
				return authMsg{url, renderQR(url), nil, ""}
			},
			spinnerTick,
			)

	case "d":
		// confirm before disconnecting
		if m.status.Up {
			m.screen = screenConfirmDown
		} else {
			m.setMsg("not connected")
		}

	case "p":
		// ping selected peer
		if m.cursor < n {
			idx := m.cursor
			ip := m.status.Peers[idx].IP
			return m, func() tea.Msg {
				lat, err := tailscalePing(ip)
				return pingMsg{idx, lat, err}
			}
		}

	case "s":
		// ssh to selected peer (exits the TUI into ssh)
		if m.cursor < n {
			peer := m.status.Peers[m.cursor]
			if !peer.Online {
				m.setMsg(peer.Name + " is offline")
				return m, nil
			}
			return m, tea.ExecProcess(exec.Command("tailscale", "ssh", peer.Name), nil)
		}

	case "c", "y":
		// copy IP — print to stdout for terminal copy
		if m.cursor < n {
			ip := m.status.Peers[m.cursor].IP
			fmt.Printf("\n%s\n", ip)
			m.setMsg("IP printed below (select + copy)")
		}

	case "e":
		// toggle exit node for selected peer
		if m.cursor < n {
			peer := m.status.Peers[m.cursor]
			if !peer.IsExit {
				m.setMsg(peer.Name + " is not an exit node")
				return m, nil
			}
			peerName := peer.Name
			return m, func() tea.Msg {
				// toggle: if already using this exit node, clear it
				// (we track locally; tailscale doesn't expose it cleanly)
				if err := setExitNode(peerName); err != nil {
					return actionDoneMsg{"", err}
				}
				return actionDoneMsg{"exit node: " + peerName, nil}
			}
		}

	case "E":
		// clear exit node
		return m, func() tea.Msg {
			if err := setExitNode(""); err != nil {
				return actionDoneMsg{"", err}
			}
			return actionDoneMsg{"exit node cleared", nil}
		}

	case "t":
		// taildrop: send a file
		if !m.status.Up {
			m.setMsg("connect to tailnet first")
			return m, nil
		}
		m.tdFiles = listSendableFiles()
		m.tdCursor = 0
		m.tdStep = 0
		m.screen = screenTaildrop
		return m, nil

	case "i":
		// taildrop inbox: pull waiting files into ~/Downloads
		m.screen = screenTaildropInbox
		return m, func() tea.Msg {
			n, err := taildropReceive()
			if err != nil {
				return actionDoneMsg{"", err}
			}
			if n == 0 {
				return actionDoneMsg{"inbox empty — nothing to pull", nil}
			}
			return actionDoneMsg{fmt.Sprintf("pulled %d file(s) into ~/Downloads", n), nil}
		}
	}
	return m, nil
}

func (m model) handleTaildropKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.tdStep {
	case 0: // pick file
		switch k {
		case "esc", "q":
			m.screen = screenMain
			return m, nil
		case "up", "k":
			if m.tdCursor > 0 {
				m.tdCursor--
			}
		case "down", "j":
			if m.tdCursor < len(m.tdFiles)-1 {
				m.tdCursor++
			}
		case "enter":
			if len(m.tdFiles) > 0 {
				m.tdFile = m.tdFiles[m.tdCursor]
				m.tdStep = 1
				m.tdPeer = m.cursor // default to currently selected peer
			}
		}
	case 1: // pick peer
		n := len(m.status.Peers)
		switch k {
		case "esc":
			m.tdStep = 0
			return m, nil
		case "up", "k":
			if m.tdPeer > 0 {
				m.tdPeer--
			}
		case "down", "j":
			if m.tdPeer < n-1 {
				m.tdPeer++
			}
		case "enter":
			if m.tdPeer < n {
				m.tdStep = 2
			}
		}
	case 2: // confirm send
		switch k {
		case "y", "Y", "enter":
			file := m.tdFile
			peer := m.status.Peers[m.tdPeer].Name
			m.screen = screenMain
			return m, func() tea.Msg {
				if err := taildropSend(file, peer); err != nil {
					return taildropMsg{"", err}
				}
				return taildropMsg{fmt.Sprintf("sent %s → %s",
					filepath.Base(file), peer), nil}
			}
		case "n", "N", "esc":
			m.tdStep = 1
			return m, nil
		}
	}
	return m, nil
}

// ── View ─────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder

	// header — ∅ mark in green, title in pink, status badge
	header := theme.Logo.Render("∅") + " " + theme.Title.Render("navi tailscale")
	header += "  " + m.statusBadge()
	b.WriteString(header + "\n")
	b.WriteString(theme.Divider(frameWidth) + "\n")

	// busy spinner overlays the content area
	if m.busy {
		b.WriteString("\n\n  " + m.spinner() + "  " + theme.Dimmed.Render(m.busyMsg) + "\n")
	} else {
	switch m.screen {
	case screenMain, screenConfirmDown:
		if !m.status.LoggedIn {
			b.WriteString(m.renderNotLoggedIn())
		} else {
			b.WriteString(m.renderSelf())
			b.WriteString("\n")
			b.WriteString(m.renderPeers())
		}
	case screenAuth:
		b.WriteString(m.renderAuth())
	case screenTaildrop:
		b.WriteString(m.renderTaildrop())
	case screenTaildropInbox:
		b.WriteString(m.renderInbox())
	case screenNotInstalled:
		b.WriteString(m.renderNotInstalled())
	case screenDaemonDown:
		b.WriteString(m.renderDaemonDown())
	}
	}

	// message line (fixed slot so footer never shifts)
	b.WriteString("\n")
	if m.msg != "" && time.Since(m.msgAt) < 4*time.Second {
		b.WriteString("  " + theme.Dimmed.Render(m.msg))
	}
	b.WriteString("\n")

	// confirm dialog
	if m.screen == screenConfirmDown {
		b.WriteString("  " + theme.Error.Render("disconnect from tailnet? (y/n)"))
		b.WriteString("\n")
	}

	// footer
	b.WriteString(m.footer())
	return b.String()
}

func (m model) footer() string {
	if m.busy {
		return theme.Footer(false, [2]string{"please wait", ""})
	}
	switch m.screen {
	case screenAuth:
		return theme.Footer(true,
			[2]string{"o", "open in browser"},
			[2]string{"u", "new link"},
			[2]string{"esc", "back"})
	case screenTaildrop:
		if m.tdStep == 0 {
			return theme.Footer(true,
				[2]string{"↑↓", "select file"},
				[2]string{"enter", "choose"},
				[2]string{"esc", "back"})
		} else if m.tdStep == 1 {
			return theme.Footer(true,
				[2]string{"↑↓", "select peer"},
				[2]string{"enter", "choose"},
				[2]string{"esc", "back"})
		}
		return theme.Footer(true,
			[2]string{"y", "send"},
			[2]string{"n", "cancel"})
	case screenTaildropInbox:
		return theme.Footer(true, [2]string{"esc", "back"})
	case screenNotInstalled:
		return theme.Footer(true,
			[2]string{"i", "install tailscale"},
		[2]string{"q", "quit"})
	case screenDaemonDown:
		return theme.Footer(true,
			[2]string{"s", "retry start"},
			[2]string{"q", "quit"})
	default:
		// Grouped footer: navigate | connection | peer actions | system.
		// Width-aware: single line when wide enough, two lines when narrow
		// (instead of letting the terminal wrap it and ghost on resize).
		nav := theme.Footer(true, [2]string{"↑↓", "navigate"})
		conn := theme.Footer(true,
			[2]string{"u", "up"},
			[2]string{"d", "down"})
		actions := theme.Footer(true,
			[2]string{"p", "ping"},
			[2]string{"s", "ssh"},
			[2]string{"e", "exit node"},
			[2]string{"t", "taildrop"},
			[2]string{"i", "inbox"},
			[2]string{"c", "copy IP"})
		sys := theme.Footer(true,
			[2]string{"r", "refresh"},
			[2]string{"q", "quit"})
		sep := theme.Dimmed.Render(" │ ")
		if m.width >= 110 {
			return nav + sep + conn + sep + actions + sep + sys
		}
		return nav + sep + conn + sep + sys + "\n" + actions
	}
}

func (m model) statusBadge() string {
	dot := lipgloss.NewStyle().Foreground(theme.Green)
	txt := lipgloss.NewStyle().Foreground(theme.Green)
	if !m.status.LoggedIn {
		dot = lipgloss.NewStyle().Foreground(theme.Dim)
		txt = theme.Dimmed
		return dot.Render("○") + " " + txt.Render("not logged in")
	}
	if m.status.Up {
		return dot.Render("●") + " " + txt.Render("connected")
	}
	dot = lipgloss.NewStyle().Foreground(theme.Dim)
	return dot.Render("○") + " " + theme.Dimmed.Render("down")
}

func (m model) renderNotLoggedIn() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Dimmed.Render("tailscale is installed but this device") + "\n")
	b.WriteString("  " + theme.Dimmed.Render("isn't on a tailnet yet.") + "\n\n")
	b.WriteString("  press " + theme.Selected.Render("u") + " to join " +
		theme.Dimmed.Render("(prints an approval link)") + "\n")
	if m.err != "" {
		b.WriteString("\n  " + theme.Error.Render(m.err) + "\n")
	}
	return b.String()
}

func (m model) renderSelf() string {
	s := m.status.Self
	pink := lipgloss.NewStyle().Foreground(theme.Pink).Bold(true)
	cyan := lipgloss.NewStyle().Foreground(theme.Cyan)
	line := fmt.Sprintf("  %s  %s",
		pink.Render("this device"),
		theme.Dimmed.Render(s.Name))
	if s.IP != "" {
		line += "  " + cyan.Render(s.IP)
	}
	if m.status.Tailnet != "" {
		line += "\n  " + theme.Dimmed.Render("tailnet: "+m.status.Tailnet)
	}
	return line
}

func (m model) renderPeers() string {
	var b strings.Builder
	n := len(m.status.Peers)
	b.WriteString(fmt.Sprintf("  %s (%d)\n", theme.Dimmed.Render("peers"), n))
	if n == 0 {
		b.WriteString("  " + theme.Dimmed.Render("no other devices on this tailnet yet.") + "\n")
		return b.String()
	}
	green := lipgloss.NewStyle().Foreground(theme.Green)
	dimDot := lipgloss.NewStyle().Foreground(theme.Dim)
	dim := theme.Dimmed
	cyan := lipgloss.NewStyle().Foreground(theme.Cyan)
	for i, p := range m.status.Peers {
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Foreground(theme.White)
		if i == m.cursor {
			cursor = theme.Selected.Render("▸ ")
			nameStyle = nameStyle.Bold(true)
		}
		dot := dimDot.Render("○")
		if p.Online {
			dot = green.Render("●")
		}
		line := fmt.Sprintf("%s%s %s", cursor, dot, nameStyle.Render(p.Name))
		line += "  " + dim.Render(p.IP)
		if p.OS != "" {
			line += "  " + dim.Render(p.OS)
		}
		if p.IsExit {
			line += "  " + cyan.Render("exit")
		}
		if p.Latency != "" {
			line += "  " + green.Render(p.Latency)
		}
		if !p.Online {
			line += "  " + dim.Render("(offline)")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m model) renderAuth() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("JOIN TAILNET") + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render("scan with your phone to authenticate:") + "\n\n")
	// indent QR code
	for _, line := range strings.Split(m.authQR, "\n") {
		if line != "" {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("  " + theme.Dimmed.Render("or visit:") + "\n")
	cyan := lipgloss.NewStyle().Foreground(theme.Cyan)
	b.WriteString("  " + cyan.Render(m.authURL) + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render("press ") + theme.Selected.Render("o") +
		theme.Dimmed.Render(" to open in browser") + "\n")
	b.WriteString("  " + theme.Dimmed.Render("waiting for authentication — this screen updates automatically") + "\n")
	return b.String()
}

func (m model) renderTaildrop() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("TAILDROP — SEND FILE") + "\n\n")

	if m.tdStep == 0 {
		b.WriteString("  " + theme.Dimmed.Render("select a file:") + "\n")
		if len(m.tdFiles) == 0 {
			b.WriteString("  " + theme.Dimmed.Render("no files found in ~/Downloads, ~/Documents") + "\n")
			return b.String()
		}
		for i, f := range m.tdFiles {
			cursor := "  "
			nameStyle := lipgloss.NewStyle().Foreground(theme.White)
			if i == m.tdCursor {
				cursor = theme.Selected.Render("▸ ")
				nameStyle = nameStyle.Bold(true)
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, nameStyle.Render(filepath.Base(f))))
		}
	} else if m.tdStep == 1 {
		b.WriteString("  " + theme.Dimmed.Render("file: "+filepath.Base(m.tdFile)) + "\n\n")
		b.WriteString("  " + theme.Dimmed.Render("send to:") + "\n")
		for i, p := range m.status.Peers {
			if !p.Online {
				continue
			}
			cursor := "  "
			nameStyle := lipgloss.NewStyle().Foreground(theme.White)
			if i == m.tdPeer {
				cursor = theme.Selected.Render("▸ ")
				nameStyle = nameStyle.Bold(true)
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, nameStyle.Render(p.Name)))
		}
	} else {
		peer := m.status.Peers[m.tdPeer]
		b.WriteString("  send " + theme.Selected.Render(filepath.Base(m.tdFile)) + "\n")
		b.WriteString("  to " + theme.Selected.Render(peer.Name) + "?\n\n")
		b.WriteString("  " + theme.Dimmed.Render("press y to send, n to go back") + "\n")
	}
	return b.String()
}

func (m model) renderInbox() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("TAILDROP INBOX") + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render("pulling waiting files into ~/Downloads…") + "\n")
	return b.String()
}

// spinner returns the current spinner frame.
func (m model) spinner() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	cyan := lipgloss.NewStyle().Foreground(theme.Cyan)
	return cyan.Render(frames[m.spinnerTick%len(frames)])
}

func (m model) renderNotInstalled() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("TAILSCALE NOT INSTALLED") + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render("navi-tailscale needs the tailscale client to") + "\n")
	b.WriteString("  " + theme.Dimmed.Render("manage your tailnet.") + "\n\n")
	b.WriteString("  press " + theme.Selected.Render("i") + " to install it now,\n")
	b.WriteString("  or run " + theme.Dimmed.Render("navi-extras --install tailscale") + "\n")
	b.WriteString("  in a terminal.\n")
	return b.String()
}

func (m model) renderDaemonDown() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("TAILSCALED NOT RUNNING") + "\n\n")
	if m.err != "" {
		b.WriteString("  " + theme.Error.Render(m.err) + "\n\n")
	}
	b.WriteString("  " + theme.Dimmed.Render("the tailscale daemon isn't running.") + "\n\n")
	b.WriteString("  press " + theme.Selected.Render("s") + " to start it.\n")
	return b.String()
}

// ── Main ─────────────────────────────────────────────────────────────────

func main() {
	// no hard exit if tailscale is missing — the TUI handles it with
	// an install screen instead (stderr goes nowhere from a panel launcher)
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
