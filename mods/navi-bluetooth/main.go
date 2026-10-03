// navi-bluetooth — the navi Bluetooth manager for navi 2 "eiri".
//
// Replaces blueman-applet with a keyboard-first nightshadeNeon TUI over
// BlueZ's bluetoothctl(1): pair, trust, connect, disconnect, and forget
// devices; adapter power, discoverability, and scan toggles; headset
// battery from `bluetoothctl info` with a upower fallback.
//
// Audio routing stays in navi-audio — this mod owns the radio link, not
// the sound.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rav3ndust/navi-theme"
)

// frameWidth is the family-standard mod window width. Views assume it.
var frameWidth = 62

// Fixed-frame geometry: every screen renders exactly btBodyRows body
// lines, so the rounded frame never jumps. The device list shows at most
// btMaxDevices devices (windowed around the cursor); headers add at most
// 3×2 lines, so the list region is exactly btListLines.
const (
	btBodyRows   = 20
	btListLines  = 11
	btMaxDevices = 5
)

// ── screens ─────────────────────────────────────────────────────────

type screen int

const (
	screenMain screen = iota
	screenConfirmRemove
	screenPairing
	screenPIN
	screenTrustAsk
)

// ── messages ────────────────────────────────────────────────────────

type startMsg struct{}

type snapshotMsg struct {
	snap snapshot
	err  error
}

type opMsg struct {
	err  error
	note string
}

type pairEventMsg struct{ ev pairEvent }

type pairTimeoutMsg struct{}

type pollTickMsg struct{}

type scanErrMsg struct{ err string }

// ── model ───────────────────────────────────────────────────────────

type row struct {
	header string // non-empty for section headers (not selectable)
	dev    int    // index into m.devs; -1 for headers
}

type model struct {
	tx           theme.Transmission
	screen       screen
	devs         []device
	rows         []row
	cursor       int // index into selectable rows
	adapter      adapterState
	scanning     bool // user scan toggle; default on
	status       string
	statusErr    bool
	working      bool
	loading      bool
	width        int
	scanFrame    int // animation frame for the scanning radar
	scanErr      string // last scan error, shown in UI
	pairSess     *pairSession
	pairDev      device
	pairPrompt   pairEvent
	pinBuf       string
	removeDev    device
	trustDev     device
	pairTimedOut bool
}

func initialModel() model {
	return model{
		tx:       theme.Transmission{},
		screen:   screenMain,
		scanning: true,
		loading:  true,
		width:    80,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.tx.Init(),
		func() tea.Msg { return startMsg{} },
		pollTickCmd(),
		scanAnimCmd(),
	)
}

// scanAnimCmd ticks the scanning radar animation.
func scanAnimCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return scanAnimMsg{} })
}

type scanAnimMsg struct{}

func pollTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return pollTickMsg{} })
}

func snapshotCmd() tea.Cmd {
	return func() tea.Msg {
		snap, err := listSnapshot()
		return snapshotMsg{snap: snap, err: err}
	}
}

func opCmd(note string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		err := fn()
		return opMsg{err: err, note: note}
	}
}

// ── row building ────────────────────────────────────────────────────

func (m *model) rebuildRows() {
	m.rows = nil
	sections := []struct {
		title string
		want  func(device) bool
	}{
		{"CONNECTED", func(d device) bool { return d.connected }},
		{"PAIRED", func(d device) bool { return d.paired && !d.connected }},
		{"AVAILABLE", func(d device) bool { return !d.paired && !d.connected }},
	}
	for _, s := range sections {
		var idx []int
		for i, d := range m.devs {
			if s.want(d) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			continue
		}
		m.rows = append(m.rows, row{header: s.title, dev: -1})
		for _, i := range idx {
			m.rows = append(m.rows, row{dev: i})
		}
	}
	// Clamp cursor to selectable rows.
	n := m.selectableCount()
	if m.cursor >= n {
		m.cursor = max(n-1, 0)
	}
}

func (m *model) selectableCount() int {
	n := 0
	for _, r := range m.rows {
		if r.dev >= 0 {
			n++
		}
	}
	return n
}

// cursorRow maps the cursor (over selectable rows) to a row index.
func (m *model) cursorRow() int {
	seen := -1
	for i, r := range m.rows {
		if r.dev < 0 {
			continue
		}
		seen++
		if seen == m.cursor {
			return i
		}
	}
	return -1
}

func (m *model) cursorDevice() (device, bool) {
	ri := m.cursorRow()
	if ri < 0 {
		return device{}, false
	}
	return m.devs[m.rows[ri].dev], true
}

func (m *model) moveCursor(d int) {
	n := m.selectableCount()
	if n == 0 {
		return
	}
	m.cursor = (m.cursor + d + n) % n
}

// ── update ──────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case startMsg:
		m.loading = true
		return m, snapshotCmd()

	case pollTickMsg:
		cmds := []tea.Cmd{pollTickCmd()}
		// Don't stomp a pairing ceremony with a refresh.
		if m.screen == screenMain && m.pairSess == nil {
			cmds = append(cmds, snapshotCmd())
			// If we want to be scanning but BlueZ stopped discovery,
			// restart it. Discovery times out on its own.
			if m.scanning && !m.adapter.discovering && m.adapter.powered {
				cmds = append(cmds, func() tea.Msg {
					if err := setScan(true); err != nil {
						return scanErrMsg{err: err.Error()}
					}
					return scanErrMsg{}
				})
			}
		}
		return m, tea.Batch(cmds...)

	case scanErrMsg:
		if msg.err != "" {
			m.scanErr = msg.err
		} else {
			m.scanErr = ""
		}
		return m, nil

	case scanAnimMsg:
		m.scanFrame++
		return m, scanAnimCmd()

	case snapshotMsg:
		m.loading = false
		m.working = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.statusErr = true
		} else {
			// Preserve cursor on the same MAC across refreshes.
			var curMAC string
			if d, ok := m.cursorDevice(); ok {
				curMAC = d.mac
			}
			m.adapter = msg.snap.adapter
			m.devs = msg.snap.devs
			m.rebuildRows()
			if curMAC != "" {
				seen := -1
				for _, r := range m.rows {
					if r.dev < 0 {
						continue
					}
					seen++
					if m.devs[r.dev].mac == curMAC {
						m.cursor = seen
						break
					}
				}
			}
		}
		return m, nil

	case opMsg:
		m.working = false
		if msg.err != nil {
			m.status = "× " + msg.err.Error()
			m.statusErr = true
		} else {
			m.status = msg.note
			m.statusErr = false
		}
		return m, snapshotCmd()

	case pairEventMsg:
		return m.handlePairEvent(msg.ev)

	case pairTimeoutMsg:
		if m.screen == screenPairing || m.screen == screenPIN {
			m.endPair()
			m.status = "× pairing timed out"
			m.statusErr = true
			m.screen = screenMain
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handlePairEvent(ev pairEvent) (tea.Model, tea.Cmd) {
	// Re-arm the pump: a ceremony can carry several prompts
	// (passkey, then service authorization) before its verdict.
	keepListening := func() tea.Cmd {
		if m.pairSess != nil {
			return pairPumpCmd(m.pairSess)
		}
		return nil
	}
	switch ev.kind {
	case pairPromptPasskey:
		m.pairPrompt = ev
		m.screen = screenPairing
		return m, keepListening()
	case pairPromptPIN:
		m.pairPrompt = ev
		m.pinBuf = ""
		m.screen = screenPIN
		return m, keepListening()
	case pairPromptAuthorize:
		m.pairPrompt = ev
		m.screen = screenPairing // yes/no screen, like passkey
		return m, keepListening()
	case pairDone:
		dev := m.pairDev
		m.endPair()
		// Apple HID devices: auto-trust so the HID profile establishes.
		// Everything else asks (existing behavior).
		if isAppleDevice(dev.name) {
			m.screen = screenMain
			m.working = true
			m.status = "paired — trusting " + dev.name + " for auto-connect…"
			m.statusErr = false
			return m, opCmd("trusted "+dev.name+" — it will auto-connect",
				func() error { return trustDevice(dev.mac, true) })
		}
		m.screen = screenTrustAsk
		m.trustDev = dev
		m.status = "paired with " + dev.name
		m.statusErr = false
		return m, snapshotCmd()
	case pairFailed:
		m.endPair()
		m.status = "× pairing failed: " + ev.text
		m.statusErr = true
		m.screen = screenMain
		return m, snapshotCmd()
	}
	return m, nil
}

func (m *model) endPair() {
	if m.pairSess != nil {
		m.pairSess.Close()
		m.pairSess = nil
	}
	m.pinBuf = ""
	m.pairTimedOut = false
}

// ── key handling ────────────────────────────────────────────────────

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// Ctrl+C always quits.
	if k == "ctrl+c" {
		m.endPair()
		return m, tea.Quit
	}

	switch m.screen {
	case screenConfirmRemove:
		switch k {
		case "y", "Y", "enter":
			dev := m.removeDev
			m.screen = screenMain
			m.working = true
			return m, opCmd("forgot "+dev.name, func() error { return removeDevice(dev.mac) })
		case "n", "N", "esc":
			m.screen = screenMain
			return m, nil
		}
		return m, nil

	case screenPairing:
		switch k {
		case "y", "Y", "enter":
			m.answerPair("yes")
			m.status = "confirming…"
			m.statusErr = false
			return m, nil
		case "n", "N":
			m.answerPair("no")
			m.endPair()
			m.status = "pairing rejected by you"
			m.statusErr = true
			m.screen = screenMain
			return m, snapshotCmd()
		case "esc":
			m.endPair()
			m.status = "pairing cancelled"
			m.statusErr = true
			m.screen = screenMain
			return m, snapshotCmd()
		}
		return m, nil

	case screenPIN:
		switch k {
		case "enter":
			m.answerPair(m.pinBuf)
			m.status = "sending PIN…"
			m.statusErr = false
			return m, nil
		case "backspace":
			if len(m.pinBuf) > 0 {
				m.pinBuf = m.pinBuf[:len(m.pinBuf)-1]
			}
			return m, nil
		case "esc":
			m.endPair()
			m.status = "pairing cancelled"
			m.statusErr = true
			m.screen = screenMain
			return m, snapshotCmd()
		default:
			if len(k) == 1 && k[0] >= '0' && k[0] <= '9' && len(m.pinBuf) < 16 {
				m.pinBuf += k
			}
			return m, nil
		}
	case screenTrustAsk:
		switch k {
		case "y", "Y", "enter":
			dev := m.trustDev
			m.screen = screenMain
			m.working = true
			return m, opCmd("trusted "+dev.name+" — it will auto-connect",
				func() error { return trustDevice(dev.mac, true) })
		case "n", "N", "esc":
			m.screen = screenMain
			m.status = "not trusted — connect manually each time"
			m.statusErr = false
			return m, snapshotCmd()
		}
		return m, nil
	}

	// screenMain
	switch k {
	case "q", "esc":
		m.endPair()
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "r":
		m.loading = true
		return m, snapshotCmd()
	case "enter":
		return m.smartAction()
	case "p":
		if d, ok := m.cursorDevice(); ok && !d.paired {
			return m.beginPair(d)
		}
	case "t":
		if d, ok := m.cursorDevice(); ok && d.paired {
			m.working = true
			return m, opCmd("trust toggled for "+d.name,
				func() error { return trustDevice(d.mac, true) })
		}
	case "c":
		if d, ok := m.cursorDevice(); ok && d.paired && !d.connected {
			m.working = true
			return m, opCmd("connecting to "+d.name+"…",
				func() error { return connectDevice(d.mac) })
		}
	case "d":
		if d, ok := m.cursorDevice(); ok && d.connected {
			m.working = true
			return m, opCmd("disconnecting "+d.name+"…",
				func() error { return disconnectDevice(d.mac) })
		}
	case "x":
		if d, ok := m.cursorDevice(); ok && d.paired {
			m.removeDev = d
			m.screen = screenConfirmRemove
		}
	case "s":
		m.scanning = !m.scanning
		on := m.scanning
		m.working = true
		note := "scan off"
		if on {
			note = "scanning for devices…"
		}
		return m, opCmd(note, func() error { return setScan(on) })
	case "D":
		want := !m.adapter.discoverable
		m.working = true
		note := "discoverable off"
		if want {
			note = "◉ discoverable — nearby devices can see this machine"
		}
		return m, opCmd(note, func() error { return setDiscoverable(want) })
	case "P":
		want := !m.adapter.powered
		m.working = true
		note := "adapter off"
		if want {
			note = "adapter on"
		}
		return m, opCmd(note, func() error { return setPower(want) })
	}
	return m, nil
}

func (m model) answerPair(ans string) {
	if m.pairSess != nil {
		select {
		case m.pairSess.Answers <- ans:
		default:
		}
	}
}

// smartAction is Enter: available → pair, paired → connect,
// connected → disconnect. Apple HID devices get trust+connect.
func (m model) smartAction() (tea.Model, tea.Cmd) {
	d, ok := m.cursorDevice()
	if !ok {
		return m, nil
	}
	switch {
	case d.connected:
		m.working = true
		return m, opCmd("disconnecting "+d.name+"…",
			func() error { return disconnectDevice(d.mac) })
	case d.paired:
		m.working = true
		if isAppleDevice(d.name) {
			return m, opCmd("trusting + connecting "+d.name+"…",
				func() error { return connectAppleDevice(d.mac) })
		}
		return m, opCmd("connecting to "+d.name+"…",
			func() error { return connectDevice(d.mac) })
	default:
		return m.beginPair(d)
	}
}

func (m model) beginPair(d device) (tea.Model, tea.Cmd) {
	sess, err := startPair(d.mac)
	if err != nil {
		m.status = "× could not start pairing: " + err.Error()
		m.statusErr = true
		return m, nil
	}
	m.pairSess = sess
	m.pairDev = d
	m.screen = screenPairing
	m.pairPrompt = pairEvent{} // waiting for the first prompt
	m.status = "pairing with " + d.name + "…"
	m.statusErr = false
	return m, tea.Batch(
		pairPumpCmd(sess),
		tea.Tick(90*time.Second, func(time.Time) tea.Msg { return pairTimeoutMsg{} }),
	)
}

// pairPumpCmd forwards session events into the program.
func pairPumpCmd(sess *pairSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-sess.Events:
			if !ok {
				return pairEventMsg{ev: pairEvent{kind: pairFailed, text: "session closed"}}
			}
			return pairEventMsg{ev: ev}
		case <-sess.done:
			return pairEventMsg{ev: pairEvent{kind: pairFailed, text: "session closed"}}
		}
	}
}

// scanRadar renders a little radar sweep animation for the scanning state.
func scanRadar(frame int) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	return theme.Spinner(frame) + " " + frames[frame%len(frames)]
}

// ── view ────────────────────────────────────────────────────────────

func (m model) View() string {
	var body string
	switch m.screen {
	case screenMain:
		body = m.viewMain()
	case screenConfirmRemove:
		body = m.viewConfirmRemove()
	case screenPairing:
		body = m.viewPairing()
	case screenPIN:
		body = m.viewPIN()
	case screenTrustAsk:
		body = m.viewTrustAsk()
	}
	return theme.FrameFixed(frameWidth, "navi bluetooth", m.adapter.powered, m.tx.View(frameWidth), body, btBodyRows)
}

func (m model) viewMain() string {
	var b strings.Builder

	// Adapter line: always exactly one line so the frame never shifts.
	switch {
	case !m.adapter.powered:
		b.WriteString(theme.Error.Render("  ○ adapter is off — press P to power on") + "\n")
	case m.adapter.discoverable:
		b.WriteString(theme.Dimmed.Render("  ◉ discoverable — nearby devices can see this machine") + "\n")
	case m.scanning && m.scanErr != "":
		b.WriteString(theme.Error.Render("  × scan failed: "+m.scanErr) + "\n")
	case m.scanning:
		b.WriteString("  " + scanRadar(m.scanFrame) + theme.Dimmed.Render(" scanning for devices…") + "\n")
	default:
		b.WriteString("\n")
	}

	// Device list region: exactly btListLines, windowed around the cursor.
	entries := m.devEntries()
	start, end := theme.ListWindow(len(entries), m.cursor, btMaxDevices)
	b.WriteString(theme.PadLines(m.listRegion(entries, start, end), btListLines) + "\n")

	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")

	// Status line: always one line.
	if m.status != "" {
		st := theme.Dimmed.Render("  " + m.status)
		if m.statusErr {
			st = theme.Error.Render("  " + m.status)
		}
		b.WriteString(st + "\n")
	} else {
		b.WriteString("\n")
	}

	// Scroll/working line: always one line.
	if hint := theme.ScrollHint(len(entries), start, end); hint != "" {
		b.WriteString("  " + hint + "\n")
	} else if m.working {
		b.WriteString("  " + theme.Spinner(0) + "\n")
	} else {
		b.WriteString("\n")
	}

	b.WriteString(theme.Dimmed.Render("  audio output switching lives in navi-audio →") + "\n")
	b.WriteString(m.viewFooter() + "\n")
	return b.String()
}

// devEntry is a selectable device with its section header.
type devEntry struct {
	section string
	dev     int
}

func (m model) devEntries() []devEntry {
	var out []devEntry
	cur := ""
	for _, r := range m.rows {
		if r.dev < 0 {
			cur = r.header
			continue
		}
		out = append(out, devEntry{section: cur, dev: r.dev})
	}
	return out
}

// listRegion renders the windowed device list with section headers.
// Headers cost 2 lines, devices 1 — at most btMaxDevices devices and 3
// headers, so the region never exceeds btListLines.
func (m model) listRegion(entries []devEntry, start, end int) string {
	var b strings.Builder
	if m.loading && len(entries) == 0 {
		b.WriteString("\n" + theme.Spinner(0) + " " + theme.Dimmed.Render("listening to the radio…") + "\n")
		return b.String()
	}
	if len(entries) == 0 {
		b.WriteString("\n" + theme.Dimmed.Render("  no devices yet — press s to scan") + "\n")
		return b.String()
	}
	lastSection := ""
	for _, e := range entries[start:end] {
		if e.section != lastSection {
			b.WriteString("\n" + theme.Header.Render("  "+e.section) + "\n")
			lastSection = e.section
		}
		b.WriteString("  " + m.viewDeviceRow(e.dev) + "\n")
	}
	return b.String()
}

func (m model) viewDeviceRow(devIdx int) string {
	d := m.devs[devIdx]
	focused := m.cursor == rowIndexOf(m.rows, devIdx)

	cursor := "  "
	name := theme.Normal.Render(truncateRunes(d.name, 30))
	if focused {
		cursor = theme.Selected.Render("› ")
		name = theme.Selected.Render(truncateRunes(d.name, 30))
	}
	dot := theme.DotOff.Render("○")
	if d.connected {
		dot = theme.DotOn.Render("●")
	}

	batt := theme.Dimmed.Render(" — ")
	if d.battery >= 0 {
		bs := fmt.Sprintf("%d%%", d.battery)
		if focused {
			batt = theme.Selected.Render(fmt.Sprintf("%3s", bs))
		} else {
			batt = theme.Normal.Render(fmt.Sprintf("%3s", bs))
		}
	}
	state := theme.Dimmed.Render("tap to pair")
	switch {
	case d.connected:
		state = theme.Dimmed.Render("connected")
	case d.paired:
		state = theme.Dimmed.Render("paired")
	}

	left := cursor + dot + " " + name
	// Pad the left column so battery/state right-align.
	leftWidth := 2 + 2 + 1 + 32
	return left + strings.Repeat(" ", max(leftWidth-lipgloss.Width(left), 1)) + batt + "  " + state
}

func rowIndexOf(rows []row, devIdx int) int {
	seen := -1
	for _, r := range rows {
		if r.dev < 0 {
			continue
		}
		seen++
		if r.dev == devIdx {
			return seen
		}
	}
	return -1
}

func (m model) viewFooter() string {
	d, hasDev := m.cursorDevice()
	enterLabel, enterOK := "pair", false
	pOK, tOK, cOK, dOK, xOK := false, false, false, false, false
	if hasDev {
		switch {
		case d.connected:
			enterLabel, enterOK = "disconnect", true
			dOK = true
			xOK = true
		case d.paired:
			enterLabel, enterOK = "connect", true
			cOK, tOK, xOK = true, true, true
		default:
			enterLabel, enterOK = "pair", true
			pOK = true
		}
	}
	scanLabel := "scan on"
	if m.scanning {
		scanLabel = "scan off"
	}
	discLabel := "discoverable on"
	if m.adapter.discoverable {
		discLabel = "discoverable off"
	}
	powerLabel := "power on"
	if m.adapter.powered {
		powerLabel = "power off"
	}
	row1 := theme.Footer(enterOK, [2]string{"enter", enterLabel}) + "   " +
		theme.Footer(pOK, [2]string{"p", "pair"}) + "   " +
		theme.Footer(tOK, [2]string{"t", "trust"}) + "   " +
		theme.Footer(xOK, [2]string{"x", "remove"})
	row2 := theme.Footer(cOK, [2]string{"c", "connect"}) + "   " +
		theme.Footer(dOK, [2]string{"d", "disconnect"}) + "   " +
		theme.Footer(true, [2]string{"s", scanLabel}) + "   " +
		theme.Footer(true, [2]string{"r", "refresh"})
	row3 := theme.Footer(true, [2]string{"D", discLabel}) + "   " +
		theme.Footer(true, [2]string{"P", powerLabel}) + "   " +
		theme.Footer(true, [2]string{"q", "quit"})
	return "  " + row1 + "\n  " + row2 + "\n  " + row3
}

func (m model) viewConfirmRemove() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(theme.Error.Render("  forget "+m.removeDev.name+"?") + "\n")
	b.WriteString(theme.Dimmed.Render("  it will disappear from Paired — re-pair to use it again.") + "\n")
	b.WriteString("\n")
	b.WriteString("  " + theme.Selected.Render("y") + theme.Dimmed.Render(" yes   ") +
		theme.Selected.Render("n") + theme.Dimmed.Render(" no") + "\n")
	return b.String()
}

func (m model) viewPairing() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(theme.Header.Render("  pairing with "+truncateRunes(m.pairDev.name, 40)) + "\n\n")
	if m.pairPrompt.kind == pairPromptPasskey {
		b.WriteString(theme.Dimmed.Render("  confirm the passkey matches on both devices:") + "\n\n")
		b.WriteString("  " + theme.Selected.Render(m.pairPrompt.text) + "\n\n")
		b.WriteString("  " + theme.Selected.Render("y") + theme.Dimmed.Render(" matches   ") +
			theme.Selected.Render("n") + theme.Dimmed.Render(" reject") + "\n")
	} else if m.pairPrompt.kind == pairPromptAuthorize {
		b.WriteString(theme.Dimmed.Render("  the device asks to authorize:") + "\n\n")
		b.WriteString("  " + theme.Normal.Render(truncateRunes(m.pairPrompt.text, 50)) + "\n\n")
		b.WriteString("  " + theme.Selected.Render("y") + theme.Dimmed.Render(" allow   ") +
			theme.Selected.Render("n") + theme.Dimmed.Render(" deny") + "\n")
	} else {
		b.WriteString("  " + theme.Spinner(0) + " " + theme.Dimmed.Render("waiting for the device…") + "\n")
		b.WriteString(theme.Dimmed.Render("  esc cancels") + "\n")
	}
	if m.status != "" && !m.statusErr {
		b.WriteString("\n" + theme.Dimmed.Render("  "+m.status) + "\n")
	}
	return b.String()
}

func (m model) viewPIN() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(theme.Header.Render("  enter PIN for "+truncateRunes(m.pairDev.name, 42)) + "\n\n")
	b.WriteString("  " + theme.Input.Render(strings.Repeat("•", len(m.pinBuf))+"▌") + "\n\n")
	b.WriteString(theme.Dimmed.Render("  enter submits · esc cancels") + "\n")
	return b.String()
}

func (m model) viewTrustAsk() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(theme.Normal.Render("  trust "+truncateRunes(m.trustDev.name, 44)+"?") + "\n")
	b.WriteString(theme.Dimmed.Render("  trusted devices auto-connect when in range.") + "\n\n")
	b.WriteString("  " + theme.Selected.Render("y") + theme.Dimmed.Render(" trust   ") +
		theme.Selected.Render("n") + theme.Dimmed.Render(" skip") + "\n")
	return b.String()
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── headless dump ───────────────────────────────────────────────────

func dumpSample() {
	m := initialModel()
	m.width = 80
	m.loading = false
	m.adapter = adapterState{present: true, name: "navi", powered: true, discoverable: true}
	m.devs = []device{
		{mac: "4C:87:5D:AA:BB:CC", name: "Sony WH-1000XM4", paired: true, connected: true, battery: 82},
		{mac: "00:1A:7D:11:22:33", name: "ThinkPad TrackPoint Keyboard II", paired: true, battery: -1},
		{mac: "A4:CF:12:9B:3D:E1", name: "JBL Flip 6", battery: -1},
	}
	m.rebuildRows()
	m.status = "scan found 1 new device"
	fmt.Println(m.View())
	fmt.Println("\n── pairing screen ──")
	m2 := m
	m2.screen = screenPairing
	m2.pairDev = m.devs[0]
	m2.pairPrompt = pairEvent{kind: pairPromptPasskey, text: "583920"}
	fmt.Println(m2.View())
	fmt.Println("\n── confirm remove ──")
	m3 := m
	m3.screen = screenConfirmRemove
	m3.removeDev = m.devs[1]
	fmt.Println(m3.View())
	fmt.Println("\n── PIN entry ──")
	m4 := m
	m4.screen = screenPIN
	m4.pairDev = m.devs[2]
	m4.pinBuf = "12"
	fmt.Println(m4.View())
	fmt.Println("\n── trust ask ──")
	m5 := m
	m5.screen = screenTrustAsk
	m5.trustDev = m.devs[1]
	fmt.Println(m5.View())
}

// ── main ────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--dump":
			dumpSample()
			return
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: navi-bluetooth [--dump]")
			os.Exit(0)
		}
	}
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-bluetooth: bluetoothctl was not found.")
		fmt.Fprintln(os.Stderr, "Install bluez first.")
		os.Exit(1)
	}
	// Ensure the adapter is powered — don't depend on blueman-applet
	// or any other tool having done it. Then start scanning.
	// Surface errors to stderr so they don't get swallowed.
	if err := setPower(true); err != nil {
		fmt.Fprintf(os.Stderr, "navi-bluetooth: power on failed: %v\n", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := setScan(true); err != nil {
		fmt.Fprintf(os.Stderr, "navi-bluetooth: scan on failed: %v\n", err)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-bluetooth: %v\n", err)
		os.Exit(1)
	}
	stopScanKeeper()
	time.Sleep(50 * time.Millisecond)
}
