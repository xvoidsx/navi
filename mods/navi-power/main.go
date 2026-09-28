// navi-power — the navi power mod for navi 2 "eiri".
//
// power-profiles-daemon switching, honest sysfs+upower battery
// readouts, and ThinkPad charge thresholds (thinkpad_acpi) behind a
// doas-preferred elevation — all in the nightshadeNeon visual language.
//
// The permacomputing flex: old hardware lives longer when the OS helps
// it sip power and spares its battery.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/rav3ndust/navi-theme"
)

// frameWidth is the family-standard mod window width. Views assume it.
var frameWidth = 62

// ── rows ────────────────────────────────────────────────────────────

const (
	rowProfiles = iota
	rowThresholds
	numRows
)

// ── messages ────────────────────────────────────────────────────────

type snapshotMsg struct {
	ppd       ppdState
	batteries []battery
	thOK      bool
	thStart   int
	thStop    int
	err       error
}

type setProfileMsg struct {
	actual string
	err    error
}

type writeThMsg struct {
	start, stop int
	elev        string
	err         error
}

type animTickMsg struct{}

// ── model ───────────────────────────────────────────────────────────

type model struct {
	width, height int
	tx            theme.Transmission
	animFrame     int

	loading bool

	ppd       ppdState
	batteries []battery

	thOK    bool
	thStart int // kernel values
	thStop  int

	cursor      int // rowProfiles | rowThresholds
	profCursor  int // highlighted profile within the profile row
	editStart   int // working threshold values
	editStop    int
	editingStop bool // tab toggles which threshold ←/→ adjusts

	note string // transient result line
	err  error
}

func initialModel() model {
	return model{
		tx:         theme.Transmission{},
		loading:    true,
		editStart:  75,
		editStop:   85,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.tx.Init(),
		loadSnapshotCmd(),
		animTickCmd(),
	)
}

func animTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

// loadSnapshotCmd re-reads everything: daemon state, batteries, and
// thresholds. The UI always follows the system's actual state.
func loadSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		msg := snapshotMsg{}
		msg.ppd = ppdList()
		msg.batteries = readBatteries()
		if s, e, ok := thresholdPaths(); ok {
			msg.thOK = true
			if v, err := readSysfsInt(s); err == nil {
				msg.thStart = v
			}
			if v, err := readSysfsInt(e); err == nil {
				msg.thStop = v
			}
		}
		return msg
	}
}

func setProfileCmd(name string) tea.Cmd {
	return func() tea.Msg {
		actual, err := ppdSet(name)
		return setProfileMsg{actual: actual, err: err}
	}
}

func writeThCmd(start, stop int) tea.Cmd {
	return func() tea.Msg {
		elev, err := writeThresholds(start, stop)
		return writeThMsg{start: start, stop: stop, elev: elev, err: err}
	}
}

// ── update ──────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case animTickMsg:
		m.animFrame++
		return m, animTickCmd()

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case snapshotMsg:
		m.loading = false
		m.ppd = msg.ppd
		m.batteries = msg.batteries
		m.thOK = msg.thOK
		m.thStart, m.thStop = msg.thStart, msg.thStop
		// Working values track the kernel until the user touches them.
		m.editStart, m.editStop = msg.thStart, msg.thStop
		if m.profCursor >= len(m.ppd.profiles) {
			m.profCursor = 0
		}
		// Keep the highlight on the daemon's truth.
		for i, p := range m.ppd.profiles {
			if p == m.ppd.active {
				m.profCursor = i
			}
		}
		return m, nil

	case setProfileMsg:
		if msg.err != nil {
			m.note = "× " + msg.err.Error()
		} else {
			m.note = "profile → " + msg.actual
		}
		// Re-list so the UI follows the daemon, never the request.
		return m, loadSnapshotCmd()

	case writeThMsg:
		if msg.err != nil {
			m.note = "× " + msg.err.Error()
		} else {
			m.note = fmt.Sprintf("applied via %s · start=%d stop=%d", msg.elev, msg.start, msg.stop)
		}
		return m, loadSnapshotCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "r", "R":
		m.loading = true
		m.note = ""
		return m, loadSnapshotCmd()

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "down", "j":
		if m.cursor < numRows-1 {
			m.cursor++
		}
		return m, nil

	case "1", "2", "3":
		return m.applyProfileDigit(msg.String())

	case "left", "h":
		return m.nudge(-1)

	case "right", "l":
		return m.nudge(1)

	case "tab":
		if m.cursor == rowThresholds && m.thOK {
			m.editingStop = !m.editingStop
		}
		return m, nil

	case "enter":
		return m.applyRow()
	}
	return m, nil
}

func (m model) applyProfileDigit(d string) (tea.Model, tea.Cmd) {
	idx := int(d[0] - '1')
	if !m.ppd.ok || idx < 0 || idx >= len(m.ppd.profiles) {
		return m, nil
	}
	m.profCursor = idx
	return m.setHighlightedProfile()
}

func (m model) setHighlightedProfile() (tea.Model, tea.Cmd) {
	name := m.ppd.profiles[m.profCursor]
	if name == m.ppd.active {
		m.note = "already " + name
		return m, nil
	}
	m.note = theme.Spinner(m.animFrame) + " asking the daemon…"
	return m, setProfileCmd(name)
}

// nudge moves left/right: on the profile row it walks the highlight
// and applies (guarded: no-op when the daemon already reports it); on
// the threshold row it adjusts the working value in 5% steps.
func (m model) nudge(dir int) (tea.Model, tea.Cmd) {
	if m.cursor == rowProfiles {
		if !m.ppd.ok || len(m.ppd.profiles) == 0 {
			return m, nil
		}
		m.profCursor += dir
		if m.profCursor < 0 {
			m.profCursor = 0
		}
		if m.profCursor >= len(m.ppd.profiles) {
			m.profCursor = len(m.ppd.profiles) - 1
		}
		return m.setHighlightedProfile()
	}
	if m.cursor == rowThresholds && m.thOK {
		if m.editingStop {
			m.editStop = clamp5(m.editStop+dir*5)
		} else {
			m.editStart = clamp5(m.editStart + dir*5)
		}
		m.note = ""
	}
	return m, nil
}

func (m model) applyRow() (tea.Model, tea.Cmd) {
	if m.cursor == rowProfiles {
		if !m.ppd.ok || len(m.ppd.profiles) == 0 {
			return m, nil
		}
		return m.setHighlightedProfile()
	}
	// Threshold row.
	if !m.thOK {
		return m, nil
	}
	if err := validateThresholds(m.editStart, m.editStop); err != nil {
		m.note = "× " + err.Error()
		return m, nil
	}
	m.note = theme.Spinner(m.animFrame) + " elevating…"
	return m, writeThCmd(m.editStart, m.editStop)
}

func clamp5(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v - (v % 5)
}

// ── view ────────────────────────────────────────────────────────────

func (m model) View() string {
	var b strings.Builder

	if m.loading {
		b.WriteString("\n" + theme.Spinner(m.animFrame) + " " +
			theme.Dimmed.Render("reading the power daemon…") + "\n")
	} else {
		b.WriteString(m.profileSection())
		b.WriteString(m.batterySection())
		b.WriteString(m.thresholdSection())
		if m.note != "" {
			b.WriteString("\n" + noteStyle(m.note) + "\n")
		}
	}
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(m.footer())

	return theme.Frame(frameWidth, "navi power", m.ppd.ok, m.tx.View(frameWidth), b.String())
}

func noteStyle(s string) string {
	if strings.HasPrefix(s, "×") {
		return theme.Error.Render("  " + s)
	}
	return theme.Dimmed.Render("  " + s)
}

func (m model) profileSection() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Header.Render("  PROFILE") + "\n")
	if !m.ppd.ok {
		b.WriteString(theme.Dimmed.Render("  power-profiles-daemon unreachable") + "\n")
		return b.String()
	}
	var parts []string
	for i, p := range m.ppd.profiles {
		glyph := "○"
		if p == m.ppd.active {
			glyph = "●"
		}
		label := fmt.Sprintf("%s %s", glyph, p)
		// Never nest a styled string inside another Render — lipgloss
		// v1.1.0 detaches the inner escape bytes. Concatenate instead.
		if m.cursor == rowProfiles && i == m.profCursor {
			parts = append(parts, theme.Selected.Render(label))
		} else if p == m.ppd.active {
			parts = append(parts, theme.Normal.Render(label))
		} else {
			parts = append(parts, theme.Dimmed.Render(label))
		}
	}
	b.WriteString("  " + strings.Join(parts, "    ") + "\n")
	if m.ppd.driver != "" {
		b.WriteString(theme.Dimmed.Render("  driver: "+m.ppd.driver) + "\n")
	}
	return b.String()
}

func (m model) batterySection() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Header.Render("  BATTERY") + "\n")
	if len(m.batteries) == 0 {
		b.WriteString(theme.Dimmed.Render("  no battery found") + "\n")
		return b.String()
	}
	for _, bat := range m.batteries {
		b.WriteString("  " + m.batteryLine(bat) + "\n")
		if bat.health > 0 {
			b.WriteString(theme.Dimmed.Render(
				fmt.Sprintf("  health %d%% of design capacity", bat.health)) + "\n")
		}
	}
	return b.String()
}

func (m model) batteryLine(bat battery) string {
	bar := theme.Meter(10, float64(bat.capacity)/100, theme.Green, theme.Dim)
	status := strings.ToLower(bat.status)
	est := ""
	switch status {
	case "charging":
		if bat.toFull != "" {
			est = " · " + bat.toFull + " to full"
		}
	case "discharging":
		if bat.toEmpty != "" {
			est = " · " + bat.toEmpty + " left"
		}
	case "full":
		est = " · full"
	}
	// Concatenate separately-rendered parts; never nest Renders.
	return theme.Normal.Render(bat.name) + "  " + bar + " " +
		theme.Normal.Render(fmt.Sprintf("%d%%", bat.capacity)) + "  " +
		theme.Dimmed.Render(status+est)
}

func (m model) thresholdSection() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Header.Render("  CHARGE THRESHOLDS") + "\n")
	if len(m.batteries) == 0 {
		b.WriteString(theme.Dimmed.Render("  no battery found") + "\n")
		return b.String()
	}
	if !m.thOK {
		b.WriteString(theme.Dimmed.Render("  not supported on this hardware") + "\n")
		b.WriteString(theme.Dimmed.Render("  needs thinkpad_acpi charge_control_*") + "\n")
		return b.String()
	}
	b.WriteString(theme.Dimmed.Render("  thinkpad_acpi — the battery idles in its comfort zone") + "\n")
	startBox := thresholdBox("start", m.editStart, m.cursor == rowThresholds && !m.editingStop)
	stopBox := thresholdBox("stop", m.editStop, m.cursor == rowThresholds && m.editingStop)
	b.WriteString("  " + startBox + "  " + stopBox + "\n")
	b.WriteString(theme.Dimmed.Render("  ←/→ adjust · tab picks start/stop · enter applies") + "\n")
	return b.String()
}

// thresholdBox renders one threshold value; focused boxes get the
// Selected treatment. Parts are concatenated, never nested.
func thresholdBox(label string, val int, focused bool) string {
	inner := fmt.Sprintf("%s [%d%%]", label, val)
	if focused {
		return theme.Selected.Render("▸ " + inner)
	}
	return theme.Normal.Render("  "+inner)
}

func (m model) footer() string {
	thActive := m.cursor == rowThresholds && m.thOK
	line1 := theme.Footer(m.ppd.ok, [2]string{"1-3", "profile"}) + "   " +
		theme.Footer(thActive, [2]string{"←/→", "adjust"}) + "   " +
		theme.Footer(thActive, [2]string{"tab", "start/stop"}) + "   " +
		theme.Footer(m.cursor == rowProfiles && m.ppd.ok || thActive, [2]string{"enter", "apply"})
	line2 := theme.Footer(true, [2]string{"r", "refresh"}) + "   " +
		theme.Footer(true, [2]string{"q", "quit"})
	return line1 + "\n" + line2
}

// ── headless samples ────────────────────────────────────────────────

func dumpSample() {
	// Force TrueColor so the headless render matches a real terminal.
	lipgloss.SetColorProfile(termenv.TrueColor)

	full := initialModel()
	full.width, full.height = 80, 24
	full.loading = false
	full.ppd = ppdState{
		ok:       true,
		profiles: []string{"performance", "balanced", "power-saver"},
		active:   "balanced",
		driver:   "intel_pstate",
	}
	full.profCursor = 1
	full.batteries = []battery{
		{name: "BAT0", capacity: 78, status: "Discharging", health: 91,
			toEmpty: "2h 42m"},
	}
	full.thOK = true
	full.thStart, full.thStop = 75, 85
	full.editStart, full.editStop = 75, 85
	full.cursor = rowThresholds
	full.note = "applied via doas · start=75 stop=85"
	fmt.Println(full.View())

	fmt.Println()
	desktop := initialModel()
	desktop.width, desktop.height = 80, 24
	desktop.loading = false
	desktop.ppd = full.ppd
	desktop.batteries = nil
	desktop.thOK = false
	fmt.Println(desktop.View())
}

// ── main ────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--dump":
			dumpSample()
			return
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: navi-power [--dump]")
			os.Exit(0)
		}
	}
	if _, err := exec.LookPath("powerprofilesctl"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-power: powerprofilesctl was not found.")
		fmt.Fprintln(os.Stderr, "Install power-profiles-daemon first.")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-power:", err)
		os.Exit(1)
	}
	// Brief settle so the terminal restores cleanly after alt-screen.
	time.Sleep(50 * time.Millisecond)
}
