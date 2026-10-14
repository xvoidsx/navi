package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rav3ndust/navi-theme"
)

const tuiWidth = 62

// Fixed-frame geometry: 18 body lines. Device/file lists windowed.
// Frame is 22 rows total (theme.Frame adds txRow line).
const wdBodyRows = 18
const wdMaxDevices = 8

type tuiScreen int

const (
	screenDevices tuiScreen = iota
	screenFiles
	screenConfirm
	screenSending
	screenHistory
)

type peerTickMsg struct{}
type sendProgressMsg struct {
	name        string
	sent, total int64
}
type sendDoneMsg struct{ err error }

type sendJob struct {
	peer  *Peer
	files []string
}

// tuiModel is the wiredrop TUI: nearby devices, file picking, the TOFU
// ceremony, send progress, and transfer history — all nightshadeNeon.
type tuiModel struct {
	prog    *tea.Program
	cfg     *Config
	paths   *Paths
	self    DeviceInfo
	cert    *tls.Certificate // presented to receivers (mutual auth)
	peers   *PeerCache
	known   *KnownHosts
	history *History
	tx      theme.Transmission

	screen   tuiScreen
	devList  []*Peer
	cursor   int
	files    []string
	input    textinput.Model
	bar      progress.Model
	job      *sendJob
	progress map[string][2]int64 // name -> sent,total
	sendErr  error
	sendDone bool
	notice   string
	width    int
}

func newTUIModel(cfg *Config, paths *Paths, self DeviceInfo, peers *PeerCache, known *KnownHosts) *tuiModel {
	ti := textinput.New()
	ti.Placeholder = "/path/to/file …  (enter adds, backspace on empty removes)"
	ti.Prompt = "› "
	ti.CharLimit = 512
	ti.TextStyle = theme.Input
	bar := progress.New(progress.WithDefaultGradient())
	bar.Width = tuiWidth - 10
	return &tuiModel{
		cfg:      cfg,
		paths:    paths,
		self:     self,
		peers:    peers,
		known:    known,
		history:  NewHistory(),
		tx:       theme.Transmission{},
		input:    ti,
		bar:      bar,
		progress: map[string][2]int64{},
		width:    tuiWidth,
	}
}

func (m *tuiModel) Init() tea.Cmd {
	return tea.Batch(m.tx.Init(), peerTickCmd())
}

func peerTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return peerTickMsg{} })
}

func (m *tuiModel) refreshPeers() {
	m.devList = m.peers.List()
	if m.cursor >= len(m.devList) {
		m.cursor = max(0, len(m.devList)-1)
	}
}

// --- Update ---------------------------------------------------------------

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Ambient row always gets its messages.
	var txCmd tea.Cmd
	m.tx, txCmd = m.tx.Update(msg)
	cmds = append(cmds, txCmd)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Width < 200 {
			m.width = min(msg.Width-4, tuiWidth)
			if m.width < 40 {
				m.width = 40
			}
		}
	case peerTickMsg:
		m.refreshPeers()
		cmds = append(cmds, peerTickCmd())
	case sendProgressMsg:
		m.progress[msg.name] = [2]int64{msg.sent, msg.total}
	case sendDoneMsg:
		m.sendDone = true
		m.sendErr = msg.err
		if msg.err == nil && m.job != nil {
			var total int64
			for _, f := range m.job.files {
				if st, err := os.Stat(f); err == nil {
					total += st.Size()
				}
			}
			names := make([]string, 0, len(m.job.files))
			for _, f := range m.job.files {
				names = append(names, filepath.Base(f))
			}
			m.history.Append(TransferRecord{
				Time: time.Now(), Direction: "sent",
				Peer: m.job.peer.Alias, Files: names, Bytes: total,
				Fingerprint: m.job.peer.Fingerprint,
			})
		}
	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	// Text input only lives on the files screen.
	if m.screen == screenFiles {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *tuiModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	m.notice = ""

	switch m.screen {
	case screenDevices:
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.devList)-1 {
				m.cursor++
			}
		case "r":
			m.refreshPeers()
			m.notice = "refreshed"
		case "h":
			m.screen = screenHistory
		case "enter":
			if len(m.devList) == 0 {
				m.notice = "no devices yet — is the daemon running?"
				return m, nil
			}
			m.job = &sendJob{peer: m.devList[m.cursor]}
			m.files = nil
			m.input.SetValue("")
			m.input.Focus()
			m.screen = screenFiles
		}

	case screenFiles:
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.screen = screenDevices
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				// Empty submit: backspace-equivalent removes last file.
				if len(m.files) > 0 {
					m.files = m.files[:len(m.files)-1]
				}
				return m, nil
			}
			if st, err := os.Stat(val); err != nil {
				m.notice = "no such file: " + val
			} else if st.IsDir() {
				m.notice = "directories aren't supported in v1 — pick files"
			} else {
				m.files = append(m.files, val)
				m.input.SetValue("")
			}
		case "ctrl+s":
			if len(m.files) == 0 {
				m.notice = "add at least one file first"
				return m, nil
			}
			m.screen = screenConfirm
		}

	case screenConfirm:
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.screen = screenFiles
		case "y", "Y", "enter":
			peer := m.job.peer
			switch m.known.Check(peer.Alias, peer.Fingerprint) {
			case TrustChanged:
				m.notice = "refused: fingerprint changed — resolve in the terminal"
				return m, nil
			case TrustUnknown:
				if err := m.known.Confirm(peer.Alias, peer.Fingerprint); err != nil {
					m.notice = err.Error()
					return m, nil
				}
			}
			m.startSend()
		case "n", "N":
			m.screen = screenFiles
		}

	case screenSending:
		switch key {
		case "ctrl+c":
			return m, tea.Quit // the HTTP client aborts; receiver times out the session
		case "enter", "esc":
			if m.sendDone {
				m.screen = screenDevices
				m.refreshPeers()
			}
		}

	case screenHistory:
		switch key {
		case "ctrl+c", "q", "esc", "h":
			m.screen = screenDevices
		}
	}
	return m, nil
}

// startSend launches the transfer in a goroutine; progress arrives as
// messages so the UI never blocks. No resume in v1 — the confirm screen
// says so and errors repeat it.
func (m *tuiModel) startSend() {
	m.screen = screenSending
	m.progress = map[string][2]int64{}
	m.sendDone = false
	m.sendErr = nil
	prog := m.prog
	job := m.job
	cfg := m.cfg
	self := m.self
	cert := m.cert
	go func() {
		err := sendFiles(cfg, self, job.peer, job.files, cfg.PIN, true, cert,
			func(name string, sent, total int64) {
				prog.Send(sendProgressMsg{name: name, sent: sent, total: total})
			})
		prog.Send(sendDoneMsg{err: err})
	}()
}

// --- View -----------------------------------------------------------------

func (m *tuiModel) View() string {
	var body string
	switch m.screen {
	case screenDevices:
		body = m.viewDevices()
	case screenFiles:
		body = m.viewFiles()
	case screenConfirm:
		body = m.viewConfirm()
	case screenSending:
		body = m.viewSending()
	case screenHistory:
		body = m.viewHistory()
	}
	footer := m.footer()
	body = theme.PadLines(body+"\n"+footer, wdBodyRows)
	view := theme.FrameFixed(tuiWidth, "wiredrop", len(m.devList) > 0, m.tx.View(m.width), body, wdBodyRows)
	return lipgloss.NewStyle().Width(m.width).Render(view)
}

func (m *tuiModel) footer() string {
	switch m.screen {
	case screenDevices:
		return theme.Footer(true,
			[2]string{"↑↓", "select"}, [2]string{"enter", "send"}, [2]string{"r", "refresh"},
			[2]string{"h", "history"}, [2]string{"q", "quit"})
	case screenFiles:
		return theme.Footer(true,
			[2]string{"enter", "add file"}, [2]string{"ctrl+s", "continue"}, [2]string{"esc", "back"})
	case screenConfirm:
		return theme.Footer(true,
			[2]string{"y", "confirm + send"}, [2]string{"n", "back"}, [2]string{"esc", "back"})
	case screenSending:
		if m.sendDone {
			return theme.Footer(true, [2]string{"enter", "devices"})
		}
		return theme.Footer(true, [2]string{"ctrl+c", "abort (restarts from zero — no resume in v1)"})
	default:
		return theme.Footer(true, [2]string{"esc", "back"}, [2]string{"q", "quit"})
	}
}

func (m *tuiModel) viewDevices() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("NEARBY DEVICES") + "\n")
	m.refreshPeers()
	if len(m.devList) == 0 {
		b.WriteString(theme.Dimmed.Render("  no devices seen yet\n"))
		b.WriteString(theme.Dimmed.Render("  the daemon announces every 30s — phones running\n"))
		b.WriteString(theme.Dimmed.Render("  LocalSend appear here too."))
		// Pad to wdMaxDevices lines.
		for i := 0; i < wdMaxDevices-3; i++ {
			b.WriteString("\n")
		}
	} else {
		start, end := theme.ListWindow(len(m.devList), m.cursor, wdMaxDevices)
		for i := start; i < end; i++ {
			p := m.devList[i]
			cursor := "  "
			style := theme.Normal
			if i == m.cursor {
				cursor = theme.Selected.Render("› ")
				style = theme.Selected
			}
			dot := theme.DotOn.Render("●")
			age := time.Since(p.LastSeen)
			seen := "now"
			if age > 90*time.Second {
				seen = fmt.Sprintf("%dm ago", int(age.Minutes()))
				dot = theme.DotOff.Render("○")
			}
			trust := theme.Dimmed.Render("new")
			if m.known.Check(p.Alias, p.Fingerprint) == TrustKnown {
				trust = theme.DotOn.Render("✓")
			}
			via := theme.Dimmed.Render("[" + p.Via + "]")
			line := fmt.Sprintf("%s%s %s %s %s %s", cursor, dot, style.Render(p.Alias), via, theme.Dimmed.Render(seen), trust)
			b.WriteString(line + "\n")
		}
		// Pad short lists.
		for i := end - start; i < wdMaxDevices; i++ {
			b.WriteString("\n")
		}
		b.WriteString(theme.ScrollHint(len(m.devList), start, end))
	}
	if m.notice != "" {
		b.WriteString("\n" + theme.Error.Render(m.notice))
	} else {
		b.WriteString("\n" + theme.Dimmed.Render(fmt.Sprintf("you are %q", m.cfg.Alias)))
	}
	return b.String()
}

func (m *tuiModel) viewFiles() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render(fmt.Sprintf("SEND TO %s", strings.ToUpper(m.job.peer.Alias))) + "\n")
	if len(m.files) == 0 {
		b.WriteString(theme.Dimmed.Render("  no files yet — type a path and hit enter\n"))
	} else {
		var total int64
		for _, f := range m.files {
			sz := int64(0)
			if st, err := os.Stat(f); err == nil {
				sz = st.Size()
			}
			total += sz
			b.WriteString(theme.Normal.Render(fmt.Sprintf("  • %s  %s", filepath.Base(f), theme.Dimmed.Render(humanSize(sz)))) + "\n")
		}
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  %d file(s), %s total", len(m.files), humanSize(total))) + "\n")
	}
	b.WriteString("\n" + m.input.View())
	if m.notice != "" {
		b.WriteString("\n" + theme.Error.Render(m.notice))
	}
	return b.String()
}

func (m *tuiModel) viewConfirm() string {
	var b strings.Builder
	peer := m.job.peer
	b.WriteString(theme.Header.Render("CONFIRM") + "\n")
	var total int64
	for _, f := range m.job.files {
		if st, err := os.Stat(f); err == nil {
			total += st.Size()
		}
	}
	b.WriteString(fmt.Sprintf("  %d file(s), %s → %s\n\n",
		len(m.job.files), humanSize(total), theme.Selected.Render(peer.Alias)))

	switch m.known.Check(peer.Alias, peer.Fingerprint) {
	case TrustKnown:
		b.WriteString(theme.DotOn.Render("  ✓ trusted device") + "\n")
	case TrustChanged:
		b.WriteString(theme.Error.Render("  ⚠ REFUSED — this device changed fingerprints.") + "\n")
		b.WriteString(theme.Dimmed.Render("  could be a reinstall, could be a MITM.\n"))
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  run `wiredrop forget %s` after verifying,\n", peer.Alias)))
		b.WriteString(theme.Dimmed.Render("  then try again."))
	default:
		b.WriteString(theme.Header.Render("  FIRST SIGHT — confirm this device:") + "\n")
		b.WriteString(theme.Normal.Render("  🔑 "+fingerprintWords(peer.Fingerprint)) + "\n")
		b.WriteString(theme.Dimmed.Render("  "+peer.Fingerprint) + "\n\n")
		b.WriteString(theme.Dimmed.Render("  compare the words with the receiver's screen.\n"))
		b.WriteString(theme.Dimmed.Render("  'y' pins this fingerprint to the alias."))
	}
	b.WriteString("\n" + theme.Dimmed.Render("  no resume in v1 — an interrupted send restarts."))
	if m.notice != "" {
		b.WriteString("\n" + theme.Error.Render(m.notice))
	}
	return b.String()
}

func (m *tuiModel) viewSending() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render(fmt.Sprintf("SENDING → %s", strings.ToUpper(m.job.peer.Alias))) + "\n")
	if m.sendDone {
		if m.sendErr != nil {
			b.WriteString(theme.Error.Render("  ✗ "+m.sendErr.Error()) + "\n")
		} else {
			b.WriteString(theme.DotOn.Render("  ✓ all files delivered") + "\n")
		}
		return b.String()
	}
	for _, f := range m.job.files {
		name := filepath.Base(f)
		st, _ := m.progress[name]
		var frac float64
		if st[1] > 0 {
			frac = float64(st[0]) / float64(st[1])
		}
		b.WriteString("  " + theme.Normal.Render(name) + "\n")
		b.WriteString("  " + m.bar.ViewAs(frac) + " " +
			theme.Dimmed.Render(fmt.Sprintf("%s / %s", humanSize(st[0]), humanSize(st[1]))) + "\n")
	}
	return b.String()
}

func (m *tuiModel) viewHistory() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("TRANSFER HISTORY") + "\n")
	recs := m.history.Recent(20)
	if len(recs) == 0 {
		b.WriteString(theme.Dimmed.Render("  nothing yet"))
		return b.String()
	}
	for _, r := range recs {
		arrow := "⤴"
		if r.Direction == "received" {
			arrow = "⤵"
		}
		when := r.Time.Format("02 Jan 15:04")
		files := strings.Join(r.Files, ", ")
		if len(files) > 44 {
			files = files[:41] + "…"
		}
		arrowStyle := lipgloss.NewStyle().Foreground(theme.Cyan)
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", arrowStyle.Render(arrow),
			theme.Normal.Render(files), theme.Dimmed.Render(fmt.Sprintf("%s · %s · %s", r.Peer, humanSize(r.Bytes), when))))
	}
	return b.String()
}

// runTUI launches the interface. The daemon must be running (it owns
// discovery and the peer cache); if it isn't, say so plainly.
func runTUI(cfg *Config, paths *Paths) error {
	peers := NewPeerCache(filepath.Join(paths.RuntimeDir, "peers.json"))
	known, err := LoadKnownHosts(paths.KnownHosts)
	if err != nil {
		return err
	}
	cert, err := loadOrCreateCert(paths.CertDir)
	if err != nil {
		return err
	}
	x509Cert, err := parseCert(cert)
	if err != nil {
		return err
	}
	model := deviceModel()
	m := newTUIModel(cfg, paths, DeviceInfo{
		Alias: cfg.Alias, Version: ProtoVersion,
		DeviceModel: &model, DeviceType: DeviceTypeDesktop,
		Fingerprint: fingerprintOf(x509Cert),
		Port:        cfg.Port, Protocol: "https",
	}, peers, known)
	m.cert = &cert
	m.refreshPeers()
	prog := tea.NewProgram(m, tea.WithAltScreen())
	m.prog = prog
	_, err = prog.Run()
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- headless --dump for visual checks ---

func dumpSample() {
	peers := NewPeerCache("")
	m := &tuiModel{
		cfg:     &Config{Alias: "test-machine"},
		peers:   peers,
		known:   &KnownHosts{},
		screen:  screenDevices,
		devList: []*Peer{},
		width:   tuiWidth,
	}
	m.tx = theme.Transmission{}
	fmt.Println("=== DEVICES (empty) ===")
	fmt.Println(m.View())
	// Sample with devices: add directly to devList, bypass refresh.
	m.devList = []*Peer{
		{Alias: "phone", Via: "mdns", LastSeen: time.Now()},
		{Alias: "laptop", Via: "mdns", LastSeen: time.Now()},
	}
	// Temporarily disable refresh by setting peers to return our list.
	fmt.Println("=== DEVICES (populated) ===")
	// viewDevices calls refreshPeers which overwrites devList; for the
	// sample we render directly.
	body := "NEARBY DEVICES sample:\n  ● phone [mdns] now\n  ● laptop [mdns] now"
	fmt.Println(theme.FrameFixed(tuiWidth, "wiredrop", true, "", theme.PadLines(body, wdBodyRows), wdBodyRows))
}
