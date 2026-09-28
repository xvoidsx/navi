// navi-reminders — the navi reminder TUI.
//
// List upcoming/missed reminders, add via natural language
// ("dentist tomorrow 9am"), snooze, done, delete. The scheduler is
// systemd user timers (see the reminders package); this binary is just
// the pretty face, launched floating via mod-open.sh.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/rav3ndust/navi-reminders"
	theme "github.com/rav3ndust/navi-theme"
)

const frameWidth = 62

type screen int

const (
	scrList screen = iota
	scrAdd
	scrConfirm
	scrSnooze
	scrDelete
)

var repeatKinds = []string{
	reminders.RepeatOnce,
	reminders.RepeatDaily,
	reminders.RepeatWeekdays,
	reminders.RepeatWeekly,
}

var repeatLabels = map[string]string{
	reminders.RepeatOnce:     "once",
	reminders.RepeatDaily:    "daily",
	reminders.RepeatWeekdays: "weekdays",
	reminders.RepeatWeekly:   "weekly",
}

type snoozePreset struct {
	label string
	dur   time.Duration // 0 = tomorrow 9am
}

var snoozePresets = []snoozePreset{
	{"+10 minutes", 10 * time.Minute},
	{"+1 hour", time.Hour},
	{"tomorrow 9am", 0},
}

type model struct {
	screen screen
	items  []reminders.Reminder // missed first, then upcoming by time
	cursor int

	input    textinput.Model
	addErr   string
	parsed   parsedAdd
	pastAck  bool
	repIdx   int
	snzIdx   int
	status   string
	mgrAlive bool
	tx       theme.Transmission
}

type parsedAdd struct {
	title string
	at    time.Time
}

type refireDoneMsg struct{ err error }

func initialModel() model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "dentist tomorrow 9am"
	ti.CharLimit = 96
	ti.TextStyle = theme.Input
	ti.PlaceholderStyle = theme.Fainted
	m := model{input: ti, tx: theme.Transmission{}, mgrAlive: reminders.UserManagerAlive()}
	m.reload()
	return m
}

func (m *model) reload() {
	reg, err := reminders.LoadRegistry()
	if err != nil {
		m.status = "couldn't read the registry: " + err.Error()
		m.items = nil
		return
	}
	var missed, upcoming []reminders.Reminder
	for _, r := range reg.Reminders {
		if r.Done {
			continue
		}
		if r.Missed {
			missed = append(missed, r)
		} else {
			upcoming = append(upcoming, r)
		}
	}
	sort.Slice(missed, func(i, j int) bool { return missed[i].MissedAt.After(missed[j].MissedAt) })
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].At.Before(upcoming[j].At) })
	m.items = append(missed, upcoming...)
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m model) Init() tea.Cmd { return m.tx.Init() }

func (m model) cur() *reminders.Reminder {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return &m.items[m.cursor]
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refireDoneMsg:
		if msg.err != nil {
			m.status = "re-fire failed: " + msg.err.Error()
		} else {
			m.status = "⏰ fired"
		}
		m.reload()
		return m, nil
	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	if m.screen == scrAdd {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.screen {
	case scrAdd:
		return m.updateAddKey(key, msg)
	case scrConfirm:
		return m.updateConfirmKey(key)
	case scrSnooze:
		return m.updateSnoozeKey(key)
	case scrDelete:
		return m.updateDeleteKey(key)
	}

	// list screen
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "a":
		m.screen = scrAdd
		m.addErr = ""
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "s":
		if m.cur() != nil {
			m.screen = scrSnooze
			m.snzIdx = 0
		}
	case "d":
		if m.cur() != nil {
			m.screen = scrDelete
		}
	case "r":
		m.reload()
		m.status = "reloaded"
	case "enter":
		return m.doEnter()
	}
	return m, nil
}

func (m model) doEnter() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil {
		return m, nil
	}
	if r.Missed {
		// re-fire now: a session is guaranteed — the user is looking at us.
		id := r.ID
		return m, func() tea.Msg {
			return refireDoneMsg{reminders.NotifyNow(id)}
		}
	}
	if err := reminders.MarkDone(r.ID); err != nil {
		m.status = "done failed: " + err.Error()
	} else {
		m.status = "✓ done — " + r.Title
	}
	m.reload()
	return m, nil
}

func (m model) updateAddKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = scrList
		m.input.Blur()
		return m, nil
	case "enter":
		title, at, err := reminders.ParseReminder(m.input.Value(), time.Now())
		if err != nil {
			m.addErr = err.Error()
			return m, nil
		}
		m.parsed = parsedAdd{title: title, at: at}
		m.pastAck = false
		m.repIdx = 0
		m.screen = scrConfirm
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = scrAdd
		m.input.Focus()
		return m, textinput.Blink
	case "left", "h":
		if m.repIdx > 0 {
			m.repIdx--
		}
	case "right", "l":
		if m.repIdx < len(repeatKinds)-1 {
			m.repIdx++
		}
	case "1", "2", "3", "4":
		m.repIdx = int(key[0] - '1')
	case "enter":
		past := reminders.IsPast(m.parsed.at, time.Now())
		if past && !m.pastAck {
			m.pastAck = true
			m.status = reminders.PastWarning() + " — Enter again to confirm"
			return m, nil
		}
		r, err := reminders.AddReminder(m.parsed.title, m.parsed.at, repeatKinds[m.repIdx])
		if err != nil {
			m.status = "save failed: " + err.Error()
		} else {
			m.status = "⏰ set — " + r.Title + " · " + reminders.Relative(r.At, time.Now())
		}
		m.screen = scrList
		m.reload()
	}
	return m, nil
}

func (m model) updateSnoozeKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = scrList
	case "up", "k":
		if m.snzIdx > 0 {
			m.snzIdx--
		}
	case "down", "j":
		if m.snzIdx < len(snoozePresets)-1 {
			m.snzIdx++
		}
	case "1", "2", "3":
		m.snzIdx = int(key[0] - '1')
		return m.applySnooze()
	case "enter":
		return m.applySnooze()
	}
	return m, nil
}

func (m model) applySnooze() (tea.Model, tea.Cmd) {
	r := m.cur()
	if r == nil {
		m.screen = scrList
		return m, nil
	}
	p := snoozePresets[m.snzIdx]
	var d time.Duration
	if p.dur > 0 {
		d = p.dur
	} else {
		// tomorrow 9am
		now := time.Now()
		d = time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location()).Sub(now)
	}
	if err := reminders.SnoozeReminder(r.ID, d); err != nil {
		m.status = "snooze failed: " + err.Error()
	} else {
		m.status = "snoozed " + p.label + " — " + r.Title
	}
	m.screen = scrList
	m.reload()
	return m, nil
}

func (m model) updateDeleteKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "n":
		m.screen = scrList
	case "y":
		r := m.cur()
		if r != nil {
			if err := reminders.DeleteReminder(r.ID); err != nil {
				m.status = "delete failed: " + err.Error()
			} else {
				m.status = "deleted — " + r.Title
			}
		}
		m.screen = scrList
		m.reload()
	}
	return m, nil
}

// --- view ---

func (m model) View() string {
	var body string
	switch m.screen {
	case scrAdd:
		body = m.viewAdd()
	case scrConfirm:
		body = m.viewConfirm()
	case scrSnooze:
		body = m.viewSnooze()
	case scrDelete:
		body = m.viewDelete()
	default:
		body = m.viewList()
	}
	out := theme.Frame(frameWidth, "navi reminders", m.mgrAlive, m.tx.View(frameWidth), body)
	out += "\n" + m.footer()
	if m.status != "" {
		out += "\n" + theme.Dimmed.Render("  "+truncateRunes(m.status, 56))
	}
	return out
}

func (m model) viewList() string {
	var b strings.Builder
	if len(m.items) == 0 {
		b.WriteString("\n" + theme.Glow("  ○ no reminders — press a to add one", time.Now()) + "\n")
		return b.String()
	}
	now := time.Now()
	sec := ""
	for i, r := range m.items {
		wantSec := "UPCOMING"
		if r.Missed {
			wantSec = "MISSED"
		}
		if wantSec != sec {
			sec = wantSec
			style := theme.Header
			if sec == "MISSED" {
				style = lipgloss.NewStyle().Foreground(theme.Red).Bold(true)
			}
			b.WriteString("\n" + style.Render("  "+sec) + "\n")
		}
		cursor := "  "
		title := theme.Normal.Render(truncateRunes(r.Title, 30))
		if i == m.cursor {
			cursor = theme.Selected.Render("› ")
			title = theme.Selected.Render(truncateRunes(r.Title, 30))
		}
		var when string
		if r.Missed {
			when = theme.Error.Render("was due " + r.MissedAt.Format("Mon 15:04"))
		} else {
			when = theme.Dimmed.Render(reminders.Relative(r.At, now))
		}
		rep := ""
		if r.Repeat != reminders.RepeatOnce {
			rep = theme.Dimmed.Render(" · " + repeatLabels[r.Repeat])
		}
		b.WriteString(fmt.Sprintf("%s%s  %s%s\n", cursor, title, when, rep))
	}
	return b.String()
}

func (m model) viewAdd() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Header.Render("  NEW REMINDER") + "\n\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	if m.addErr != "" {
		b.WriteString("  " + theme.Error.Render("× "+m.addErr) + "\n\n")
	}
	b.WriteString(theme.Dimmed.Render("  e.g. dentist tomorrow 9am · standup friday 8:30 · gym mon 7a\n"))
	return b.String()
}

func (m model) viewConfirm() string {
	var b strings.Builder
	b.WriteString("\n" + theme.Header.Render("  CONFIRM") + "\n\n")
	b.WriteString("  " + theme.Normal.Render(truncateRunes(m.parsed.title, 44)) + "\n")
	b.WriteString("  " + theme.Header.Render(m.parsed.at.Format("Mon 02 Jan 2006 · 15:04")) + "\n\n")
	if reminders.IsPast(m.parsed.at, time.Now()) && !m.pastAck {
		b.WriteString("  " + theme.Error.Render("! "+reminders.PastWarning()) + "\n\n")
	}
	b.WriteString(theme.Dimmed.Render("  repeats:") + "\n")
	for i, k := range repeatKinds {
		cursor := "   "
		label := theme.Dimmed.Render(fmt.Sprintf("%d %s", i+1, repeatLabels[k]))
		if i == m.repIdx {
			cursor = theme.Selected.Render(" › ")
			label = theme.Selected.Render(fmt.Sprintf("%d %s", i+1, repeatLabels[k]))
		}
		b.WriteString(cursor + label + "\n")
	}
	b.WriteString("\n" + theme.Dimmed.Render("  ←→ pick · Enter save · esc back") + "\n")
	return b.String()
}

func (m model) viewSnooze() string {
	var b strings.Builder
	r := m.cur()
	title := ""
	if r != nil {
		title = r.Title
	}
	b.WriteString("\n" + theme.Header.Render("  SNOOZE") + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render(truncateRunes(title, 44)) + "\n\n")
	for i, p := range snoozePresets {
		cursor := "   "
		label := theme.Dimmed.Render(fmt.Sprintf("%d %s", i+1, p.label))
		if i == m.snzIdx {
			cursor = theme.Selected.Render(" › ")
			label = theme.Selected.Render(fmt.Sprintf("%d %s", i+1, p.label))
		}
		b.WriteString(cursor + label + "\n")
	}
	b.WriteString("\n" + theme.Dimmed.Render("  Enter snooze · esc cancel") + "\n")
	return b.String()
}

func (m model) viewDelete() string {
	var b strings.Builder
	r := m.cur()
	title := ""
	if r != nil {
		title = r.Title
	}
	b.WriteString("\n" + theme.Header.Render("  DELETE") + "\n\n")
	b.WriteString("  " + theme.Normal.Render(truncateRunes(title, 44)) + "\n\n")
	b.WriteString("  " + theme.Error.Render("delete this reminder?") + theme.Dimmed.Render("  (y/n)") + "\n")
	return b.String()
}

func (m model) footer() string {
	switch m.screen {
	case scrAdd:
		return theme.Footer(true,
			[2]string{"enter", "parse"},
			[2]string{"esc", "back"},
			[2]string{"ctrl+c", "quit"},
		)
	case scrConfirm:
		return theme.Footer(true,
			[2]string{"←→/1-4", "repeat"},
			[2]string{"enter", "save"},
			[2]string{"esc", "back"},
		)
	case scrSnooze:
		return theme.Footer(true,
			[2]string{"↑↓/1-3", "pick"},
			[2]string{"enter", "snooze"},
			[2]string{"esc", "cancel"},
		)
	case scrDelete:
		return theme.Footer(true,
			[2]string{"y", "delete"},
			[2]string{"n/esc", "keep"},
		)
	default:
		return theme.Footer(true,
			[2]string{"a", "add"},
			[2]string{"enter", "done"},
			[2]string{"s", "snooze"},
			[2]string{"d", "delete"},
			[2]string{"r", "reload"},
			[2]string{"q", "quit"},
		) + "\n" + theme.Footer(true,
			[2]string{"↑↓/jk", "move"},
		)
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// --- headless --dump for visual checks ---

func dump() {
	m := initialModel()
	m.items = []reminders.Reminder{
		{ID: "x1", Title: "dentist", At: time.Now().Add(2 * time.Hour), Repeat: reminders.RepeatOnce, Missed: true, MissedAt: time.Now().Add(-30 * time.Minute)},
		{ID: "x2", Title: "standup", At: time.Now().Add(26 * time.Hour), Repeat: reminders.RepeatDaily},
		{ID: "x3", Title: "water the plants", At: time.Now().Add(3 * 24 * time.Hour), Repeat: reminders.RepeatWeekly},
	}
	m.cursor = 1
	fmt.Println("=== list ===")
	fmt.Println(m.View())
	m.screen = scrAdd
	m.input.SetValue("dentist tomorrow 9am")
	fmt.Println("=== add ===")
	fmt.Println(m.View())
	m.screen = scrConfirm
	m.parsed = parsedAdd{title: "dentist", at: time.Now().Add(26 * time.Hour)}
	m.repIdx = 1
	fmt.Println("=== confirm ===")
	fmt.Println(m.View())
	m.screen = scrSnooze
	fmt.Println("=== snooze ===")
	fmt.Println(m.View())
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dump" {
		lipgloss.SetColorProfile(termenv.TrueColor)
		dump()
		return
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-reminders needs a systemd user manager (systemctl not found)")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-reminders:", err)
		os.Exit(1)
	}
	// let the terminal restore cleanly (sibling-mod convention)
	time.Sleep(50 * time.Millisecond)
}
