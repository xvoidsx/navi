package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func tailscaleDown() error {
	return exec.Command("tailscale", "down").Run()
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

func fetchStatus() tea.Msg {
	st, err := tailscaleStatus()
	return statusMsg{st, err}
}

func initialModel() model {
	return model{screen: screenMain}
}

func (m model) Init() tea.Cmd {
	return fetchStatus
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

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
	}
	return m, nil
}

func (m *model) setMsg(s string) {
	m.msg = s
	m.msgAt = time.Now()
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
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
		// tailscale up
		return m, func() tea.Msg {
			if err := tailscaleUp(); err != nil {
				return actionDoneMsg{"", err}
			}
			return actionDoneMsg{"connecting to tailnet…", nil}
		}

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

	if !m.status.LoggedIn {
		b.WriteString(m.renderNotLoggedIn())
	} else {
		b.WriteString(m.renderSelf())
		b.WriteString("\n")
		b.WriteString(m.renderPeers())
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
	b.WriteString(theme.Footer(true,
		[2]string{"↑↓", "navigate"},
		[2]string{"u", "up"},
		[2]string{"d", "down"},
		[2]string{"p", "ping"},
		[2]string{"s", "ssh"},
		[2]string{"c", "copy IP"},
		[2]string{"r", "refresh"},
		[2]string{"q", "quit"},
	))
	return b.String()
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

// ── Main ─────────────────────────────────────────────────────────────────

func main() {
	// needs tailscale on PATH
	if _, err := exec.LookPath("tailscale"); err != nil {
		fmt.Fprintln(os.Stderr, "tailscale not found — install it with: navi-extras --install tailscale")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
