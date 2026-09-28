// navi-display — the navi monitor-layout mod for navi 2 "eiri".
//
// Arrange, enable, rotate, scale, and set modes on every connected output —
// plus a night-light schedule — from one floating TUI. No GUI settings app.
//
// Backend is picked once at startup (swaymsg round-trip, else i3-msg):
// Wayland speaks `swaymsg -t get_outputs` / `swaymsg output …`, X11 speaks
// `xrandr --query` / `xrandr --output …`. Every change applies immediately.

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rav3ndust/navi-theme"
)

// frameWidth is the family-standard mod window width. Views assume it.
var frameWidth = 62

// ── tabs & screens ───────────────────────────────────────────────────

type tab int

const (
	tabOutputs tab = iota
	tabNight
)

const numTabs = 2

var tabTitles = []string{"OUTPUTS", "NIGHT LIGHT"}

type screen int

const (
	screenMain screen = iota
	screenDetail
)

// ── messages ─────────────────────────────────────────────────────────

type outputsMsg struct {
	outs []Output
	err  error
}

type appliedMsg struct {
	note string
	err  error
}

type refreshTickMsg struct{}
type animTickMsg struct{}

// ── model ────────────────────────────────────────────────────────────

type model struct {
	backend    Backend
	backendErr error
	outputs    []Output
	cursor     int
	tab        tab
	screen     screen

	// detail screen state
	detailName string
	modeCursor int

	// night-light state
	night     NightConfig
	nightRow  int
	editField string // "", "lat", "lon", "dawn", "dusk"
	editBuf   []rune

	message string
	err     error
	loading bool

	width  int
	height int

	tx       theme.Transmission
	quitting bool
}

var okStyle = theme.DotOn.Copy().Bold(true)

func initialModel() model {
	b, berr := detectBackend()
	m := model{
		backend:    b,
		backendErr: berr,
		loading:    berr == nil,
		night:      loadNightConfig(),
		tx:         theme.Transmission{},
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.tx.Init(),
		listOutputsCmd(m.backend),
		animTickCmd(),
		refreshTickCmd(),
	)
}

// ── commands ─────────────────────────────────────────────────────────

func listOutputsCmd(b Backend) tea.Cmd {
	return func() tea.Msg {
		outs, err := listOutputs(b)
		return outputsMsg{outs: outs, err: err}
	}
}

func animTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

func refreshTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func applyCmd(b Backend, fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		note, err := fn()
		return appliedMsg{note: note, err: err}
	}
}

// ── update ───────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, nil
	}
	switch msg := msg.(type) {
	case outputsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		// Preserve the cursor by output name across refreshes.
		name := ""
		if m.cursor < len(m.outputs) {
			name = m.outputs[m.cursor].Name
		}
		m.outputs = msg.outs
		m.cursor = 0
		for i, o := range m.outputs {
			if o.Name == name {
				m.cursor = i
				break
			}
		}
		if m.cursor >= len(m.outputs) && len(m.outputs) > 0 {
			m.cursor = len(m.outputs) - 1
		}
		// Re-resolve the detail output by name too.
		if m.screen == screenDetail {
			found := false
			for _, o := range m.outputs {
				if o.Name == m.detailName {
					found = true
					break
				}
			}
			if !found {
				m.screen = screenMain
			}
		}
		return m, nil

	case appliedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.message = msg.note
		} else {
			m.message = ""
		}
		// Re-list so the view reflects what the compositor actually did.
		return m, listOutputsCmd(m.backend)

	case refreshTickMsg:
		if m.backendErr == nil {
			return m, tea.Batch(listOutputsCmd(m.backend), refreshTickCmd())
		}
		return m, refreshTickCmd()

	case animTickMsg:
		return m, animTickCmd()

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Text-field editing swallows everything except enter/esc/backspace.
	if m.editField != "" {
		return m.updateEdit(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		return m.quit(), nil
	case "q":
		if m.screen == screenMain {
			return m.quit(), nil
		}
	case "esc":
		if m.screen == screenDetail {
			m.screen = screenMain
			return m, nil
		}
		return m.quit(), nil
	}

	// Tab switching is global on the main screen.
	if m.screen == screenMain {
		switch msg.String() {
		case "1":
			m.tab = tabOutputs
			return m, nil
		case "2":
			m.tab = tabNight
			return m, nil
		}
	}

	if m.screen == screenDetail {
		return m.updateDetailKey(msg)
	}
	if m.tab == tabNight {
		return m.updateNightKey(msg)
	}
	return m.updateOutputsKey(msg)
}

func (m model) quit() model {
	m.quitting = true
	return m
}

// ── outputs list keys ────────────────────────────────────────────────

func (m model) updateOutputsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.outputs)
	switch msg.String() {
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor < n-1 {
			m.cursor++
		}
	case "enter":
		if n > 0 {
			m.detailName = m.outputs[m.cursor].Name
			m.modeCursor = 0
			// Start the mode cursor on the current mode.
			if o := m.detailOutput(); o != nil {
				for i, md := range o.Modes {
					if md.Current {
						m.modeCursor = i
						break
					}
				}
			}
			m.screen = screenDetail
		}
	case "R":
		m.loading = true
		return m, listOutputsCmd(m.backend)
	case "h", "l", "k", "j", "m", "a":
		return m.layoutKey(msg.String())
	}
	return m, nil
}

func (m model) layoutKey(k string) (tea.Model, tea.Cmd) {
	if len(m.outputs) == 0 {
		return m, nil
	}
	var dir layoutDir
	var verb string
	switch k {
	case "h":
		dir, verb = dirLeft, "left of"
	case "l":
		dir, verb = dirRight, "right of"
	case "k":
		dir, verb = dirAbove, "above"
	case "j":
		dir, verb = dirBelow, "below"
	case "m":
		dir, verb = dirMirror, "mirroring"
	case "a":
		dir, verb = dirAuto, "auto-arranged"
	}
	b := m.backend
	old := m.outputs
	idx := m.cursor
	return m, applyCmd(b, func() (string, error) {
		next := arrange(old, idx, dir)
		for i := range next {
			if !next[i].Active {
				continue
			}
			if next[i].X != old[i].X || next[i].Y != old[i].Y {
				if err := applyPosition(b, next[i], next[i].X, next[i].Y); err != nil {
					return "", err
				}
			}
		}
		if dir == dirAuto {
			return "outputs auto-arranged", nil
		}
		prev := ""
		if idx > 0 {
			prev = " " + old[idx-1].Name
		}
		return fmt.Sprintf("%s %s%s", old[idx].Name, verb, prev), nil
	})
}

// ── detail screen keys ───────────────────────────────────────────────

func (m model) detailOutput() *Output {
	for i := range m.outputs {
		if m.outputs[i].Name == m.detailName {
			return &m.outputs[i]
		}
	}
	return nil
}

func (m model) updateDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	o := m.detailOutput()
	if o == nil {
		m.screen = screenMain
		return m, nil
	}
	b := m.backend
	switch msg.String() {
	case "up":
		if m.modeCursor > 0 {
			m.modeCursor--
		}
	case "down":
		if m.modeCursor < len(o.Modes)-1 {
			m.modeCursor++
		}
	case "enter":
		if m.modeCursor < len(o.Modes) {
			md := o.Modes[m.modeCursor]
			out := *o
			return m, applyCmd(b, func() (string, error) {
				if err := applyMode(b, out, md); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s → %s", out.Name, md.Label()), nil
			})
		}
	case "t":
		out := *o
		rot := nextRotation(out.Rotation)
		return m, applyCmd(b, func() (string, error) {
			if err := applyRotation(b, out, rot); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s rotated %s", out.Name, rot), nil
		})
	case "+", "=":
		if b == BackendX11 {
			m.err = fmt.Errorf("fractional scale is a Wayland thing")
			return m, nil
		}
		out := *o
		ns := out.Scale + 0.5
		if ns > 3 {
			ns = 3
		}
		return m, applyCmd(b, func() (string, error) {
			if err := applyScale(b, out, ns); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s scale %.1f", out.Name, ns), nil
		})
	case "-", "_":
		if b == BackendX11 {
			m.err = fmt.Errorf("fractional scale is a Wayland thing")
			return m, nil
		}
		out := *o
		ns := out.Scale - 0.5
		if ns < 0.5 {
			ns = 0.5
		}
		return m, applyCmd(b, func() (string, error) {
			if err := applyScale(b, out, ns); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s scale %.1f", out.Name, ns), nil
		})
	case "e":
		out := *o
		want := !out.Active
		verb := "disabled"
		if want {
			verb = "enabled"
		}
		return m, applyCmd(b, func() (string, error) {
			if err := applyPower(b, out, want); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %s", out.Name, verb), nil
		})
	}
	return m, nil
}

// ── night-light keys ─────────────────────────────────────────────────

var nightRows = []string{"day temp", "night temp", "schedule", "latitude", "longitude", "dawn", "dusk"}

func (m model) updateNightKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := m.backend
	switch msg.String() {
	case "up":
		if m.nightRow > 0 {
			m.nightRow--
		}
	case "down":
		if m.nightRow < len(nightRows)-1 {
			m.nightRow++
		}
	case "left", "right":
		delta := -100
		if msg.String() == "right" {
			delta = 100
		}
		m.nightAdjust(delta)
		return m, m.nightApplyCmd()
	case "n":
		m.night.Enabled = !m.night.Enabled
		return m, m.nightApplyCmd()
	case "enter":
		switch nightRows[m.nightRow] {
		case "schedule":
			if b == BackendX11 {
				m.err = fmt.Errorf("gammastep always uses location — manual times are wlsunset-only")
				return m, nil
			}
			m.night.Auto = !m.night.Auto
			return m, m.nightApplyCmd()
		case "latitude":
			m.editField, m.editBuf = "lat", []rune(m.night.Lat)
		case "longitude":
			m.editField, m.editBuf = "lon", []rune(m.night.Lon)
		case "dawn":
			m.editField, m.editBuf = "dawn", []rune(m.night.Dawn)
		case "dusk":
			m.editField, m.editBuf = "dusk", []rune(m.night.Dusk)
		}
	}
	return m, nil
}

func (m *model) nightAdjust(delta int) {
	switch nightRows[m.nightRow] {
	case "day temp":
		m.night.DayTemp = clampTemp(m.night.DayTemp + delta)
	case "night temp":
		m.night.NightTemp = clampTemp(m.night.NightTemp + delta)
	}
}

func clampTemp(k int) int {
	if k < 1000 {
		return 1000
	}
	if k > 10000 {
		return 10000
	}
	return k
}

func (m model) nightApplyCmd() tea.Cmd {
	b := m.backend
	cfg := m.night
	return applyCmd(b, func() (string, error) {
		if err := saveNightConfig(cfg); err != nil {
			return "", err
		}
		if err := applyNightLight(b, cfg); err != nil {
			return "", err
		}
		if cfg.Enabled {
			return fmt.Sprintf("night light on · %dK → %dK", cfg.DayTemp, cfg.NightTemp), nil
		}
		return "night light off", nil
	})
}

// ── inline text editing (lat/lon/dawn/dusk) ──────────────────────────

func (m model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(string(m.editBuf))
		switch m.editField {
		case "lat":
			m.night.Lat = val
		case "lon":
			m.night.Lon = val
		case "dawn":
			if !validClock(val) {
				m.err = fmt.Errorf("dawn wants HH:MM, 24-hour")
				return m, nil
			}
			m.night.Dawn = val
		case "dusk":
			if !validClock(val) {
				m.err = fmt.Errorf("dusk wants HH:MM, 24-hour")
				return m, nil
			}
			m.night.Dusk = val
		}
		m.editField = ""
		m.err = nil
		return m, m.nightApplyCmd()
	case "esc":
		m.editField = ""
		return m, nil
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
		}
	default:
		if len(msg.Runes) == 1 {
			m.editBuf = append(m.editBuf, msg.Runes[0])
		}
	}
	return m, nil
}

func validClock(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h, e1 := atoi2(s[:2])
	mi, e2 := atoi2(s[3:])
	return e1 == nil && e2 == nil && h >= 0 && h < 24 && mi >= 0 && mi < 60
}

func atoi2(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not digits")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ── view ─────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var body string
	switch {
	case m.backendErr != nil:
		body = m.errorView()
	case m.screen == screenDetail:
		body = m.detailView()
	case m.tab == tabNight:
		body = m.nightView()
	default:
		body = m.outputsView()
	}
	return m.place(m.frame(body))
}

func (m model) frame(content string) string {
	return theme.Frame(frameWidth, "navi display", m.backendErr == nil, m.tx.View(frameWidth), content)
}

func (m model) place(content string) string {
	if m.width == 0 {
		return content
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.frame(content))
}

func (m model) tabBar() string {
	parts := make([]string, 0, numTabs)
	for i, t := range tabTitles {
		label := fmt.Sprintf("%d %s", i+1, t)
		if tab(i) == m.tab {
			parts = append(parts, theme.Selected.Render("▸"+label))
		} else {
			parts = append(parts, theme.Dimmed.Render(" "+label))
		}
	}
	return strings.Join(parts, " ")
}

func (m model) errorView() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Error.Render("× "+m.backendErr.Error()) + "\n")
	b.WriteString(theme.Dimmed.Render("  are you inside a sway or i3 session?") + "\n")
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(theme.Footer(true, [2]string{"q", "quit"}))
	return m.place(b.String())
}

func (m model) outputsView() string {
	var b strings.Builder
	b.WriteString(m.tabBar())
	b.WriteString("\n\n")
	if m.loading {
		b.WriteString("\n" + theme.Spinner(0) + " " + theme.Dimmed.Render("probing the glass…") + "\n")
	} else if len(m.outputs) == 0 {
		b.WriteString(theme.Dimmed.Render("  no connected outputs — is a display plugged in?") + "\n")
	} else {
		for i, o := range m.outputs {
			cursor := "  "
			name := truncateRunes(o.Name, 14)
			if i == m.cursor {
				cursor = theme.Selected.Render("▸ ")
				name = theme.Selected.Render(name)
			}
			dot := theme.DotOff.Render("○")
			state := "off"
			if o.Active {
				dot = theme.DotOn.Render("●")
				state = "on"
			}
			ident := ""
			if o.Make != "" || o.Model != "" {
				ident = "  " + theme.Dimmed.Render(truncateRunes(strings.TrimSpace(o.Make+" "+o.Model), 30))
			}
			b.WriteString(fmt.Sprintf("%s%s  %s  %s  %s%s\n",
				cursor, name,
				theme.Normal.Render(o.ModeLabel()),
				theme.Dimmed.Render(fmt.Sprintf("pos %d,%d", o.X, o.Y)),
				dot+" "+theme.Dimmed.Render(state),
				ident,
			))
		}
	}
	b.WriteString(m.notice())
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	line1 := theme.Footer(true,
		[2]string{"1-2", "tabs"},
		[2]string{"↑↓", "navigate"},
		[2]string{"enter", "detail"},
	)
	line2 := theme.Footer(len(m.outputs) > 1, [2]string{"h/l/k/j", "arrange"}) + "   " +
		theme.Footer(len(m.outputs) > 1, [2]string{"m", "mirror"}) + "   " +
		theme.Footer(len(m.outputs) > 0, [2]string{"a", "auto"})
	line3 := theme.Footer(true,
		[2]string{"R", "refresh"},
		[2]string{"q", "quit"},
	)
	b.WriteString(line1 + "\n" + line2 + "\n" + line3)
	return m.place(b.String())
}

func (m model) detailView() string {
	o := m.detailOutput()
	var b strings.Builder
	if o == nil {
		b.WriteString(theme.Dimmed.Render("  output vanished — press esc") + "\n")
	} else {
		b.WriteString(theme.Header.Render(strings.ToUpper(o.Name)) + "\n")
		sub := fmt.Sprintf("%s · %s · scale %.1f · rot %s",
			o.ModeLabel(), onOff(o.Active), o.Scale, o.Rotation)
		if o.AdaptiveSync != "" {
			sub += " · sync " + o.AdaptiveSync
		}
		b.WriteString(theme.Dimmed.Render("  "+sub) + "\n\n")
		b.WriteString(theme.Dimmed.Render("  modes") + "\n")
		for i, md := range o.Modes {
			cursor := "    "
			label := md.Label()
			if md.Current {
				label += "  *"
			} else if md.Preferred {
				label += "  +"
			}
			if i == m.modeCursor {
				cursor = theme.Selected.Render("  › ")
				label = theme.Selected.Render(label)
			}
			b.WriteString(cursor + theme.Normal.Render(truncateRunes(label, 50)) + "\n")
		}
		if len(o.Modes) == 0 {
			b.WriteString(theme.Dimmed.Render("    no modes reported") + "\n")
		}
	}
	b.WriteString(m.notice())
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	scaleActive := m.backend == BackendSway
	line1 := theme.Footer(true,
		[2]string{"↑↓", "mode"},
		[2]string{"enter", "apply"},
		[2]string{"t", "rotate"},
	)
	line2 := theme.Footer(scaleActive, [2]string{"+/-", "scale"}) + "   " +
		theme.Footer(true, [2]string{"e", "on/off"}) + "   " +
		theme.Footer(true, [2]string{"esc", "back"})
	note := ""
	if !scaleActive {
		note = theme.Dimmed.Render("  scale needs Wayland — xrandr can't do fractional")
	}
	b.WriteString(line1 + "\n" + line2 + note)
	return m.place(b.String())
}

func (m model) nightView() string {
	var b strings.Builder
	b.WriteString(m.tabBar())
	b.WriteString("\n\n")
	daemon := nightDaemon(m.backend)
	if daemon == "" {
		want := "wlsunset"
		if m.backend == BackendX11 {
			want = "gammastep"
		}
		b.WriteString(theme.Error.Render("  × "+want+" not installed") + "\n")
		b.WriteString(theme.Dimmed.Render("    doas apt install "+want) + "\n")
		b.WriteString(theme.Dimmed.Render("    night light stays dim until then") + "\n\n")
	}
	rows := []struct {
		label string
		value string
	}{
		{"day temp", fmt.Sprintf("%dK", m.night.DayTemp)},
		{"night temp", fmt.Sprintf("%dK", m.night.NightTemp)},
		{"schedule", scheduleLabel(m.night, m.backend)},
		{"latitude", m.night.Lat},
		{"longitude", m.night.Lon},
		{"dawn", m.night.Dawn},
		{"dusk", m.night.Dusk},
	}
	state := theme.DotOff.Render("○ off")
	if m.night.Enabled {
		state = theme.DotOn.Render("● on")
	}
	b.WriteString("  " + theme.Normal.Render("night light") + "  " + state + "\n\n")
	for i, r := range rows {
		cursor := "    "
		label := theme.Dimmed.Render(r.label)
		val := theme.Normal.Render(r.value)
		if i == m.nightRow {
			cursor = theme.Selected.Render("  › ")
			label = theme.Selected.Render(r.label)
		}
		if m.editField != "" && nightRows[i] == editFieldName(m.editField) {
			val = theme.Input.Render(string(m.editBuf) + "▌")
		}
		// manual dawn/dusk only mean something on wlsunset
		if m.backend == BackendX11 && (r.label == "dawn" || r.label == "dusk") {
			val = theme.Dimmed.Render("—")
		}
		b.WriteString(fmt.Sprintf("%s%-12s %s\n", cursor, label, val))
	}
	b.WriteString(m.notice())
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	line1 := theme.Footer(true,
		[2]string{"1-2", "tabs"},
		[2]string{"↑↓", "select"},
		[2]string{"←/→", "adjust"},
	)
	line2 := theme.Footer(true, [2]string{"n", "on/off"}) + "   " +
		theme.Footer(true, [2]string{"enter", "edit/toggle"}) + "   " +
		theme.Footer(true, [2]string{"q", "quit"})
	b.WriteString(line1 + "\n" + line2)
	if m.editField != "" {
		b.WriteString("\n" + theme.Footer(true,
			[2]string{"enter", "commit"},
			[2]string{"esc", "cancel"},
		))
	}
	return m.place(b.String())
}

func editFieldName(f string) string {
	switch f {
	case "lat":
		return "latitude"
	case "lon":
		return "longitude"
	}
	return f
}

func scheduleLabel(n NightConfig, b Backend) string {
	if b == BackendX11 {
		return fmt.Sprintf("auto  %s,%s", n.Lat, n.Lon)
	}
	if n.Auto {
		return fmt.Sprintf("auto  %s,%s", n.Lat, n.Lon)
	}
	return fmt.Sprintf("manual  %s–%s", n.Dawn, n.Dusk)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m model) notice() string {
	var b strings.Builder
	if m.message != "" {
		b.WriteString("\n" + okStyle.Render("✓ "+m.message) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + theme.Error.Render("× "+m.err.Error()) + "\n")
	}
	return b.String()
}

// ── small helpers ────────────────────────────────────────────────────

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// ── main ─────────────────────────────────────────────────────────────

func main() {
	dump := flag.Bool("dump", false, "render sample screens to stdout and exit")
	flag.Parse()

	if *dump {
		dumpScreens()
		return
	}

	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	// Let the terminal restore cleanly before we print anything else.
	time.Sleep(50 * time.Millisecond)
	_ = final
	if err != nil {
		fmt.Fprintln(os.Stderr, "navi-display:", err)
		os.Exit(1)
	}
}

// dumpScreens renders sample screens headless for visual checks.
func dumpScreens() {
	m := initialModel()
	m.width = 100
	m.backendErr = nil
	m.outputs = []Output{
		{
			Name: "HDMI-A-1", Active: true, Make: "Samsung", Model: "S24C570",
			CurW: 1920, CurH: 1080, CurMHz: 60000, X: 1366, Y: 0, Scale: 1,
			Rotation: "normal",
			Modes: []Mode{
				{Width: 1920, Height: 1080, RefreshMHz: 60000, Current: true, Preferred: true},
				{Width: 1920, Height: 1080, RefreshMHz: 50000},
				{Width: 1280, Height: 720, RefreshMHz: 60000},
			},
		},
		{
			Name: "eDP-1", Active: true, Make: "AUO", Model: "0x06FA",
			CurW: 1366, CurH: 768, CurMHz: 60000, X: 0, Y: 0, Scale: 1,
			Rotation: "normal",
			Modes: []Mode{
				{Width: 1366, Height: 768, RefreshMHz: 60000, Current: true, Preferred: true},
			},
		},
	}
	m.night = defaultNightConfig()
	m.night.Enabled = true

	fmt.Println("=== OUTPUTS ===")
	fmt.Println(m.outputsView())
	m.tab = tabNight
	fmt.Println("=== NIGHT LIGHT ===")
	fmt.Println(m.nightView())
	m.tab = tabOutputs
	m.screen = screenDetail
	m.detailName = "HDMI-A-1"
	m.modeCursor = 1
	fmt.Println("=== DETAIL ===")
	fmt.Println(m.detailView())
}
