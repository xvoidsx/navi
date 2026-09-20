// navi-calendar — a bubble tea calendar mod for navi.
//
// Month grid (weeks start Monday, day-first dates, 24-hour times) with an
// agenda pane for the selected day. Events live as plain JSON in
// ~/.local/share/navi/navi-calendar/events.json — no daemon, no
// notifications in v1. Styled entirely through the shared nightshadeNeon
// theme package (mods/theme).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const frameWidth = 62

// ---------------------------------------------------------------------------
// storage
// ---------------------------------------------------------------------------

type event struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"` // YYYY-MM-DD
	Time  string `json:"time"` // HH:MM, 24-hour
}

type store struct {
	Events []event `json:"events"`
}

func eventsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "navi", "navi-calendar", "events.json")
}

func loadEvents() ([]event, error) {
	raw, err := os.ReadFile(eventsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return s.Events, nil
}

func saveEvents(events []event) error {
	p := eventsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store{Events: events}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(raw, '\n'), 0o644)
}

// ---------------------------------------------------------------------------
// date helpers (all local time)
// ---------------------------------------------------------------------------

func dateKey(t time.Time) string { return t.Format("2006-01-02") }

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validTime(s string) bool {
	if len(s) != 5 {
		return false
	}
	_, err := time.Parse("15:04", s)
	return err == nil
}

func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.Local).Day()
}

// monthStartOffset returns how many blank cells precede day 1 when weeks
// start on Monday.
func monthStartOffset(y int, m time.Month) int {
	wd := time.Date(y, m, 1, 0, 0, 0, 0, time.Local).Weekday()
	return (int(wd) + 6) % 7
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type mode int

const (
	modeBrowse mode = iota
	modeAdd
	modeConfirmDelete
)

type model struct {
	tx     theme.Transmission
	width  int
	height int

	viewYear  int
	viewMonth time.Month
	cursor    time.Time
	today     time.Time

	events []event

	mode      mode
	inputs    []textinput.Model
	focusIdx  int
	agendaSel int
	confirmEv *event
	errMsg    string
}

func newModel() model {
	now := time.Now()
	y, m, _ := now.Date()
	mdl := model{
		viewYear:  y,
		viewMonth: m,
		cursor:    time.Date(y, m, now.Day(), 0, 0, 0, 0, time.Local),
		today:     now,
	}
	mdl.events, _ = loadEvents()
	mdl.initInputs()
	return mdl
}

func (m *model) initInputs() {
	m.inputs = make([]textinput.Model, 3)
	labels := []string{"title", "date", "time"}
	placeholders := []string{"what's happening?", "YYYY-MM-DD", "HH:MM"}
	for i := range m.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 64
		ti.TextStyle = theme.Input
		ti.PlaceholderStyle = theme.Fainted
		_ = labels[i]
		m.inputs[i] = ti
	}
	m.inputs[1].CharLimit = 10
	m.inputs[2].CharLimit = 5
}

func (m model) Init() tea.Cmd {
	return m.tx.Init()
}

// eventsFor returns the day's events time-ordered.
func (m model) eventsFor(key string) []event {
	var out []event
	for _, e := range m.events {
		if e.Date == key {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time == out[j].Time {
			return out[i].Title < out[j].Title
		}
		return out[i].Time < out[j].Time
	})
	return out
}

func (m model) hasEvents(key string) bool {
	for _, e := range m.events {
		if e.Date == key {
			return true
		}
	}
	return false
}

func (m *model) syncViewToCursor() {
	y, mo, _ := m.cursor.Date()
	m.viewYear, m.viewMonth = y, mo
}

func (m *model) moveCursor(days int) {
	m.cursor = m.cursor.AddDate(0, 0, days)
	m.syncViewToCursor()
	m.agendaSel = 0
	m.errMsg = ""
}

func (m *model) moveMonth(delta int) {
	y, mo, d := m.cursor.Date()
	first := time.Date(y, mo+time.Month(delta), 1, 0, 0, 0, 0, time.Local)
	fy, fm, _ := first.Date()
	last := daysInMonth(fy, fm)
	if d > last {
		d = last
	}
	m.cursor = time.Date(fy, fm, d, 0, 0, 0, 0, time.Local)
	m.syncViewToCursor()
	m.agendaSel = 0
	m.errMsg = ""
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The ambient ticker is independent of everything else; delegate all
	// four message types and never cancel it.
	switch msg.(type) {
	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	if m.mode == modeAdd {
		var cmd tea.Cmd
		m.inputs[m.focusIdx], cmd = m.inputs[m.focusIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.mode == modeConfirmDelete {
		switch key {
		case "y", "Y":
			m.deleteConfirmed()
			return m, nil
		case "n", "N", "esc":
			m.mode = modeBrowse
			m.confirmEv = nil
			return m, nil
		}
		return m, nil
	}

	if m.mode == modeAdd {
		switch key {
		case "esc":
			m.mode = modeBrowse
			m.errMsg = ""
			return m, nil
		case "tab", "shift+tab":
			if key == "tab" {
				m.focusIdx = (m.focusIdx + 1) % len(m.inputs)
			} else {
				m.focusIdx = (m.focusIdx + len(m.inputs) - 1) % len(m.inputs)
			}
			m.focusInputs()
			return m, nil
		case "enter":
			if m.focusIdx < len(m.inputs)-1 {
				m.focusIdx++
				m.focusInputs()
				return m, nil
			}
			m.submitAdd()
			return m, nil
		}
		var cmd tea.Cmd
		m.inputs[m.focusIdx], cmd = m.inputs[m.focusIdx].Update(msg)
		return m, cmd
	}

	// browse mode
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.errMsg = ""
		return m, nil
	case "h", "left":
		m.moveCursor(-1)
	case "l", "right":
		m.moveCursor(1)
	case "j", "down":
		m.moveCursor(7)
	case "k", "up":
		m.moveCursor(-7)
	case "H", "pgup":
		m.moveMonth(-1)
	case "L", "pgdown":
		m.moveMonth(1)
	case "t":
		m.cursor = time.Date(m.today.Year(), m.today.Month(), m.today.Day(), 0, 0, 0, 0, time.Local)
		m.syncViewToCursor()
		m.agendaSel = 0
		m.errMsg = ""
	case "a":
		m.startAdd()
	case "d":
		m.startDelete()
	}
	return m, nil
}

func (m *model) focusInputs() {
	for i := range m.inputs {
		if i == m.focusIdx {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

func (m *model) startAdd() {
	m.mode = modeAdd
	m.focusIdx = 0
	m.inputs[0].SetValue("")
	m.inputs[1].SetValue(dateKey(m.cursor))
	m.inputs[2].SetValue("")
	m.errMsg = ""
	m.focusInputs()
}

func (m *model) submitAdd() {
	title := strings.TrimSpace(m.inputs[0].Value())
	date := strings.TrimSpace(m.inputs[1].Value())
	tm := strings.TrimSpace(m.inputs[2].Value())
	switch {
	case title == "":
		m.errMsg = "give the event a title"
		m.focusIdx = 0
		m.focusInputs()
		return
	case !validDate(date):
		m.errMsg = "date must be YYYY-MM-DD"
		m.focusIdx = 1
		m.focusInputs()
		return
	case !validTime(tm):
		m.errMsg = "time must be HH:MM, 24-hour"
		m.focusIdx = 2
		m.focusInputs()
		return
	}
	m.events = append(m.events, event{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Title: title,
		Date:  date,
		Time:  tm,
	})
	if err := saveEvents(m.events); err != nil {
		m.errMsg = "couldn't save: " + err.Error()
		return
	}
	// jump the cursor to the new event's day so it's visible
	if d, err := time.Parse("2006-01-02", date); err == nil {
		m.cursor = d
		m.syncViewToCursor()
	}
	m.agendaSel = 0
	m.mode = modeBrowse
	m.errMsg = ""
}

func (m *model) startDelete() {
	day := m.eventsFor(dateKey(m.cursor))
	if len(day) == 0 {
		m.errMsg = "nothing to delete on this day"
		return
	}
	if m.agendaSel >= len(day) {
		m.agendaSel = len(day) - 1
	}
	ev := day[m.agendaSel]
	m.confirmEv = &ev
	m.mode = modeConfirmDelete
}

func (m *model) deleteConfirmed() {
	if m.confirmEv == nil {
		m.mode = modeBrowse
		return
	}
	id := m.confirmEv.ID
	kept := m.events[:0]
	for _, e := range m.events {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	m.events = kept
	if err := saveEvents(m.events); err != nil {
		m.errMsg = "couldn't save: " + err.Error()
	} else {
		m.errMsg = ""
	}
	m.confirmEv = nil
	m.agendaSel = 0
	m.mode = modeBrowse
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

var (
	todayStyle = lipgloss.NewStyle().Foreground(theme.Green).Bold(true)
	// Cursor day: pink and bold, no background block — the frame is
	// transparent, so a filled block would look pasted-on.
	cursorStyle = lipgloss.NewStyle().Foreground(theme.Pink).Bold(true).Underline(true)
	dotStyle    = lipgloss.NewStyle().Foreground(theme.Cyan)
)

func (m model) View() string {
	body := m.monthGrid() + "\n\n" + theme.Divider(frameWidth) + "\n" + m.agendaPane() +
		"\n\n" + m.footer()
	frame := theme.Frame(frameWidth, "navi calendar", false, m.tx.View(frameWidth), body)
	if m.width == 0 {
		return frame
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, frame)
}

func (m model) monthGrid() string {
	const cols = 7
	const cellW = 7
	gridW := cols*cellW + (cols - 1) // 55

	var b strings.Builder
	title := fmt.Sprintf("%s %d", m.viewMonth.String(), m.viewYear)
	b.WriteString(lipgloss.PlaceHorizontal(gridW, lipgloss.Center, theme.Header.Render(title)))
	b.WriteString("\n")

	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	cells := make([]string, cols)
	for i, wd := range weekdays {
		cells[i] = lipgloss.PlaceHorizontal(cellW, lipgloss.Center, theme.Dimmed.Render(wd))
	}
	b.WriteString(strings.Join(cells, " "))
	b.WriteString("\n")

	offset := monthStartOffset(m.viewYear, m.viewMonth)
	days := daysInMonth(m.viewYear, m.viewMonth)
	day := 1
	for week := 0; week < 6 && day <= days; week++ {
		cells = cells[:0]
		for c := 0; c < cols; c++ {
			if (week == 0 && c < offset) || day > days {
				cells = append(cells, strings.Repeat(" ", cellW))
				continue
			}
			d := time.Date(m.viewYear, m.viewMonth, day, 0, 0, 0, 0, time.Local)
			key := dateKey(d)
			marker := " "
			if m.hasEvents(key) {
				marker = dotStyle.Render("•")
			}
			num := fmt.Sprintf("%2d", day)
			pad := strings.Repeat(" ", cellW-3)
			var styled string
			switch {
			case sameDay(d, m.cursor):
				// NB: marker is already ANSI-styled — never nest it
				// inside another Render. lipgloss styles the inner
				// ESC bytes rune-by-rune, detaching them from their
				// "[", and the terminal then prints the sequence
				// bodies as literal text ("[38;2;0;255;255m•[0m").
				styled = cursorStyle.Render(num) + marker + cursorStyle.Render(pad)
			case sameDay(d, m.today):
				styled = todayStyle.Render(num) + marker + pad
			default:
				styled = theme.Normal.Render(num) + marker + pad
			}
			cells = append(cells, styled)
			day++
		}
		b.WriteString(strings.Join(cells, " "))
		b.WriteString("\n")
	}
	return lipgloss.PlaceHorizontal(frameWidth, lipgloss.Center, strings.TrimRight(b.String(), "\n"))
}

func (m model) agendaPane() string {
	var b strings.Builder
	dayLabel := m.cursor.Format("Monday 02 January")
	head := theme.Header.Render("AGENDA") + "  " + theme.Grayed.Render(dayLabel)
	b.WriteString(head)
	b.WriteString("\n")

	switch m.mode {
	case modeAdd:
		b.WriteString(m.addForm())
		return b.String()
	case modeConfirmDelete:
		if m.confirmEv != nil {
			ev := m.confirmEv
			b.WriteString(theme.Error.Render(fmt.Sprintf("delete \"%s\" (%s %s)?", ev.Title, ev.Date, ev.Time)))
			b.WriteString("\n")
			b.WriteString(theme.Dimmed.Render("y delete   ·   n keep"))
			return b.String()
		}
	}

	day := m.eventsFor(dateKey(m.cursor))
	if len(day) == 0 {
		b.WriteString(theme.Dimmed.Render("no events — press a to add one"))
		return b.String()
	}
	const maxRows = 8
	for i, e := range day {
		if i >= maxRows {
			b.WriteString(theme.Dimmed.Render(fmt.Sprintf("… and %d more", len(day)-maxRows)))
			b.WriteString("\n")
			break
		}
		row := theme.Grayed.Render(e.Time) + "  " + theme.Normal.Render(e.Title)
		if i == m.agendaSel {
			row = theme.Selected.Render("> ") + theme.Grayed.Render(e.Time) + "  " + theme.Selected.Render(e.Title)
		} else {
			row = "  " + row
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) addForm() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("ADD EVENT"))
	b.WriteString("\n")
	labels := []string{"title", "date ", "time "}
	for i, inp := range m.inputs {
		lbl := theme.Dimmed.Render(labels[i] + " ")
		if i == m.focusIdx {
			lbl = theme.Selected.Render(labels[i] + " ")
		}
		b.WriteString(lbl + inp.View() + "\n")
	}
	if m.errMsg != "" {
		b.WriteString(theme.Error.Render(m.errMsg) + "\n")
	}
	b.WriteString(theme.Dimmed.Render("enter next · tab switch field · esc cancel"))
	return strings.TrimRight(b.String(), "\n")
}

func (m model) footer() string {
	line1 := theme.Footer(true,
		[2]string{"←→ hl", "day"},
		[2]string{"↑↓ jk", "week"},
		[2]string{"PgUp/PgDn", "month"},
		[2]string{"t", "today"},
	)
	line2 := theme.Footer(true,
		[2]string{"a", "add"},
		[2]string{"d", "delete"},
		[2]string{"esc", "cancel"},
		[2]string{"q", "quit"},
	)
	return line1 + "\n" + line2
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dump := flag.Bool("dump", false, "print the initial view and exit")
	flag.Parse()

	m := newModel()
	if *dump {
		fmt.Println(m.View())
		return
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-calendar:", err)
		os.Exit(1)
	}
	// let the terminal restore cleanly, same as the networking mod
	time.Sleep(50 * time.Millisecond)
}
