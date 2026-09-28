package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestValidDate(t *testing.T) {
	for _, ok := range []string{"2026-09-18", "2024-02-29", "2000-01-01"} {
		if !validDate(ok) {
			t.Errorf("validDate(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "18-09-2026", "2026-13-01", "2026-02-30", "2026-9-8", "not-a-date"} {
		if validDate(bad) {
			t.Errorf("validDate(%q) = true, want false", bad)
		}
	}
}

func TestValidTime(t *testing.T) {
	for _, ok := range []string{"00:00", "01:30", "14:05", "23:59"} {
		if !validTime(ok) {
			t.Errorf("validTime(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "24:00", "1:30", "14:60", "noon", "14-30"} {
		if validTime(bad) {
			t.Errorf("validTime(%q) = true, want false", bad)
		}
	}
}

func TestDaysInMonth(t *testing.T) {
	if got := daysInMonth(2024, time.February); got != 29 {
		t.Errorf("Feb 2024 = %d, want 29", got)
	}
	if got := daysInMonth(2025, time.February); got != 28 {
		t.Errorf("Feb 2025 = %d, want 28", got)
	}
	if got := daysInMonth(2026, time.September); got != 30 {
		t.Errorf("Sep 2026 = %d, want 30", got)
	}
}

func TestMonthStartOffset(t *testing.T) {
	// 2026-09-01 is a Tuesday -> one blank cell before it (Monday start).
	if got := monthStartOffset(2026, time.September); got != 1 {
		t.Errorf("Sep 2026 offset = %d, want 1", got)
	}
	// 2026-06-01 is a Monday -> no offset.
	if got := monthStartOffset(2026, time.June); got != 0 {
		t.Errorf("Jun 2026 offset = %d, want 0", got)
	}
	// 2026-11-01 is a Sunday -> six blank cells.
	if got := monthStartOffset(2026, time.November); got != 6 {
		t.Errorf("Nov 2026 offset = %d, want 6", got)
	}
}

func TestEventsForSortsByTime(t *testing.T) {
	m := model{events: []event{
		{ID: "3", Title: "late", Date: "2026-09-18", Time: "18:00"},
		{ID: "1", Title: "early", Date: "2026-09-18", Time: "08:00"},
		{ID: "2", Title: "other day", Date: "2026-09-19", Time: "07:00"},
		{ID: "4", Title: "mid", Date: "2026-09-18", Time: "12:30"},
	}}
	got := m.eventsFor("2026-09-18")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"08:00", "12:30", "18:00"}
	for i, w := range want {
		if got[i].Time != w {
			t.Errorf("eventsFor[%d].Time = %s, want %s", i, got[i].Time, w)
		}
	}
}

func TestDateKeyRoundTrip(t *testing.T) {
	d := time.Date(2026, time.September, 18, 15, 4, 0, 0, time.Local)
	if got := dateKey(d); got != "2026-09-18" {
		t.Errorf("dateKey = %s, want 2026-09-18", got)
	}
	if !validDate(dateKey(d)) {
		t.Error("dateKey output fails validDate")
	}
}

func TestParseQuickAdd(t *testing.T) {
	mon := time.Date(2026, time.September, 28, 15, 0, 0, 0, time.Local) // a Monday
	fri := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.Local)    // a Friday
	jan := time.Date(2027, time.January, 15, 12, 0, 0, 0, time.Local)

	ok := []struct {
		name            string
		input           string
		now             time.Time
		title, date, tm string
	}{
		{"spec example", "dinner with ryoko friday 7pm", mon, "dinner with ryoko", "2026-10-02", "19:00"},
		{"tomorrow", "standup tomorrow 9am", mon, "standup", "2026-09-29", "09:00"},
		{"tmr + 24h time", "dentist tmr 14:30", mon, "dentist", "2026-09-29", "14:30"},
		{"day month", "dentist 28 sep 14:30", mon, "dentist", "2026-09-28", "14:30"},
		{"month day", "dentist sep 28 14:30", mon, "dentist", "2026-09-28", "14:30"},
		{"next monday noon", "deploy freeze next monday noon", mon, "deploy freeze", "2026-10-05", "12:00"},
		{"next friday from monday", "x next friday 5pm", mon, "x", "2026-10-09", "17:00"},
		{"next friday from friday", "x next friday 5pm", fri, "x", "2026-10-09", "17:00"},
		{"title case kept", "Call Mom today 7:30pm", mon, "Call Mom", "2026-09-28", "19:30"},
		{"tonight midnight", "party tonight midnight", mon, "party", "2026-09-28", "00:00"},
		{"bare 7:00 is 07:00", "standup tomorrow 7:00", mon, "standup", "2026-09-29", "07:00"},
		{"a suffix", "gym tmr 6a", mon, "gym", "2026-09-29", "06:00"},
		{"12am is midnight", "thing today 12am", mon, "thing", "2026-09-28", "00:00"},
		{"12pm is noon", "thing today 12pm", mon, "thing", "2026-09-28", "12:00"},
		{"iso date", "review 2026-10-05 10:00", mon, "review", "2026-10-05", "10:00"},
		{"weekday abbrev", "meeting fri 9:00", mon, "meeting", "2026-10-02", "09:00"},
		// Friday-today edge: later today stays today, passed rolls a week
		{"friday later today", "coffee friday 4pm", fri, "coffee", "2026-10-02", "16:00"},
		{"friday passed today", "coffee friday 9am", fri, "coffee", "2026-10-09", "09:00"},
		// year rollover for month-day dates
		{"december ahead", "xmas 25 dec 12:00", mon, "xmas", "2026-12-25", "12:00"},
		{"december in january", "xmas 25 dec 12:00", jan, "xmas", "2027-12-25", "12:00"},
		{"january passed", "x 5 jan 8pm", jan, "x", "2028-01-05", "20:00"},
	}
	for _, tc := range ok {
		ev, err := parseQuickAdd(tc.input, tc.now)
		if err != nil {
			t.Errorf("%s: unexpected error %q", tc.name, err)
			continue
		}
		if ev.Title != tc.title || ev.Date != tc.date || ev.Time != tc.tm {
			t.Errorf("%s: got (%q %q %q), want (%q %q %q)",
				tc.name, ev.Title, ev.Date, ev.Time, tc.title, tc.date, tc.tm)
		}
		if ev.ID != "" {
			t.Errorf("%s: parser must not assign an ID, got %q", tc.name, ev.ID)
		}
	}

	bad := []struct {
		name, input, wantErr string
	}{
		{"no time", "dinner friday", qaErrTime},
		{"no time junk", "dinner sometime", qaErrJunk},
		{"no date", "lunch 7pm", qaErrDate},
		{"empty title", "friday 7pm", qaErrTitle},
		{"ambiguous junk", "maybe friday-ish sometime", qaErrJunk},
		{"empty input", "", qaErrJunk},
		{"impossible date", "x 31 feb 7pm", qaErrJunk},
		{"bad time", "x friday 25:00", qaErrTime},
	}
	for _, tc := range bad {
		_, err := parseQuickAdd(tc.input, mon)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		if err.Error() != tc.wantErr {
			t.Errorf("%s: error = %q, want %q", tc.name, err.Error(), tc.wantErr)
		}
	}
}

func TestQuickIsPast(t *testing.T) {
	now := time.Date(2026, time.September, 28, 15, 0, 0, 0, time.Local)
	past := event{Title: "x", Date: "2026-09-28", Time: "07:00"}
	if !quickIsPast(past, now) {
		t.Error("07:00 today is past at 15:00")
	}
	future := event{Title: "x", Date: "2026-09-28", Time: "19:00"}
	if quickIsPast(future, now) {
		t.Error("19:00 today is not past at 15:00")
	}
	nextWeek := event{Title: "x", Date: "2026-10-02", Time: "09:00"}
	if quickIsPast(nextWeek, now) {
		t.Error("next friday is not past")
	}
}
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func openQuickAdd(t *testing.T, m model) model {
	t.Helper()
	mdl, _ := m.Update(keyMsg("q"))
	m = mdl.(model)
	if m.mode != modeQuickAdd {
		t.Fatalf("q: mode = %v, want modeQuickAdd", m.mode)
	}
	return m
}

// Full flow: q -> type -> enter (parse) -> enter (save) -> event persisted
// and cursor jumped to the event's day.
func TestQuickAddSaveFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := openQuickAdd(t, newModel())
	m.quickInput.SetValue("stream next monday 8pm")

	mdl, _ := m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeQuickConfirm {
		t.Fatalf("parse enter: mode = %v, want modeQuickConfirm (err=%q)", m.mode, m.quickErr)
	}
	if m.quickEv == nil || m.quickEv.Title != "stream" || m.quickEv.Time != "20:00" {
		t.Fatalf("quickEv = %+v", m.quickEv)
	}

	mdl, _ = m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeBrowse {
		t.Fatalf("save enter: mode = %v, want modeBrowse", m.mode)
	}

	evs, err := loadEvents()
	if err != nil {
		t.Fatalf("loadEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("saved %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Title != "stream" || ev.Time != "20:00" || ev.ID == "" {
		t.Errorf("saved event = %+v", ev)
	}
	if dateKey(m.cursor) != ev.Date {
		t.Errorf("cursor on %s, want event day %s", dateKey(m.cursor), ev.Date)
	}
}

// e on the confirm screen drops into the manual form pre-filled.
func TestQuickAddManualFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := openQuickAdd(t, newModel())
	m.quickInput.SetValue("stream friday 8pm")
	mdl, _ := m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeQuickConfirm {
		t.Fatalf("parse enter: mode = %v (err=%q)", m.mode, m.quickErr)
	}
	mdl, _ = m.Update(keyMsg("e"))
	m = mdl.(model)
	if m.mode != modeAdd {
		t.Fatalf("e: mode = %v, want modeAdd", m.mode)
	}
	if m.inputs[0].Value() != "stream" {
		t.Errorf("title not pre-filled: %q", m.inputs[0].Value())
	}
	if m.inputs[2].Value() != "20:00" {
		t.Errorf("time not pre-filled: %q", m.inputs[2].Value())
	}
	if !validDate(m.inputs[1].Value()) {
		t.Errorf("date not pre-filled: %q", m.inputs[1].Value())
	}
}

// Gibberish keeps the input, shows the refusal, and stays editable.
func TestQuickAddRefusalKeepsInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := openQuickAdd(t, newModel())
	m.quickInput.SetValue("maybe friday-ish sometime")
	mdl, _ := m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeQuickAdd {
		t.Fatalf("refusal: mode = %v, want modeQuickAdd", m.mode)
	}
	if m.quickErr != qaErrJunk {
		t.Errorf("quickErr = %q, want %q", m.quickErr, qaErrJunk)
	}
	if m.quickInput.Value() != "maybe friday-ish sometime" {
		t.Errorf("input not preserved: %q", m.quickInput.Value())
	}
}

// A past datetime warns and needs a second Enter — no silent save.
func TestQuickAddPastNeedsTwoEnters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := openQuickAdd(t, newModel())
	m.quickInput.SetValue("x 2000-01-01 8am")
	mdl, _ := m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeQuickConfirm {
		t.Fatalf("parse enter: mode = %v (err=%q)", m.mode, m.quickErr)
	}
	if m.quickArmed {
		t.Fatal("past event must not be armed on first confirm")
	}
	// first Enter arms the warning, saves nothing
	mdl, _ = m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeQuickConfirm {
		t.Fatalf("arm enter: mode = %v, want modeQuickConfirm", m.mode)
	}
	if evs, _ := loadEvents(); len(evs) != 0 {
		t.Fatalf("saved %d events after arming enter, want 0", len(evs))
	}
	// second Enter saves
	mdl, _ = m.Update(keyMsg("enter"))
	m = mdl.(model)
	if m.mode != modeBrowse {
		t.Fatalf("save enter: mode = %v, want modeBrowse", m.mode)
	}
	if evs, _ := loadEvents(); len(evs) != 1 {
		t.Fatalf("saved %d events, want 1", len(evs))
	}
}

func TestViewsRender(t *testing.T) {
	m := newModel()
	for _, tc := range []struct {
		name string
		mode mode
		want string
	}{
		{"browse grid", modeBrowse, "September"},
		{"browse agenda", modeBrowse, "AGENDA"},
		{"browse footer", modeBrowse, "quick add"},
		{"add form", modeAdd, "ADD EVENT"},
		{"quick add", modeQuickAdd, "QUICK ADD"},
	} {
		m.mode = tc.mode
		if got := m.View(); !strings.Contains(got, tc.want) {
			t.Errorf("%s view missing %q", tc.name, tc.want)
		}
	}
	m.mode = modeConfirmDelete
	ev := event{ID: "9", Title: "doomed", Date: "2026-09-18", Time: "10:00"}
	m.confirmEv = &ev
	if got := m.View(); !strings.Contains(got, "doomed") {
		t.Error("confirm view missing event title")
	}
	m.mode = modeQuickConfirm
	qev := event{Title: "stream", Date: "2026-10-02", Time: "20:00"}
	m.quickEv = &qev
	if got := m.View(); !strings.Contains(got, "stream") {
		t.Error("quick confirm view missing event title")
	}
}

// Regression test: the event marker is a pre-styled string ("•" rendered
// through dotStyle). Nesting it inside another style's Render made lipgloss
// wrap the inner ESC bytes rune-by-rune, detaching them from their "[" —
// the terminal then printed the sequence bodies as literal text, so the
// cursor day showed "25[38;2;0;255;255m•[0m" instead of "25•".
// See: "calendar event marker shows garbage" (2026-09-20).
func TestGridEventMarkerSurvivesIntact(t *testing.T) {
	// force real escape sequences: without a TTY lipgloss defaults to
	// the Ascii profile and renders no sequences at all, which would
	// make this test vacuously pass.
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := newModel()
	m.viewYear, m.viewMonth = 2026, time.September
	m.cursor = time.Date(2026, time.September, 25, 0, 0, 0, 0, time.Local)
	m.today = time.Date(2026, time.September, 20, 0, 0, 0, 0, time.Local)
	m.events = []event{{ID: "1", Title: "Joe's birthday", Date: "2026-09-25", Time: "00:00"}}

	grid := m.monthGrid()
	// the marker must appear with its escape sequences intact — nested
	// Render calls shatter them into per-rune fragments.
	if want := dotStyle.Render("•"); !strings.Contains(grid, want) {
		t.Errorf("event marker not intact in grid; want %q present", want)
	}
}
