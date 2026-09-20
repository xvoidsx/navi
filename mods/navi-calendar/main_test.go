package main

import (
	"strings"
	"testing"
	"time"

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

func TestViewsRender(t *testing.T) {
	m := newModel()
	for _, tc := range []struct {
		name string
		mode mode
		want string
	}{
		{"browse grid", modeBrowse, "September"},
		{"browse agenda", modeBrowse, "AGENDA"},
		{"browse footer", modeBrowse, "q quit"},
		{"add form", modeAdd, "ADD EVENT"},
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
