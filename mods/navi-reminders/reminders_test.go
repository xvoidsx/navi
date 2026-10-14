package reminders

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testNow() time.Time {
	// Monday 2026-09-28 01:48 America/Chicago — pinned for parser tests.
	loc, _ := time.LoadLocation("America/Chicago")
	return time.Date(2026, 9, 28, 1, 48, 0, 0, loc)
}

func TestParseReminder(t *testing.T) {
	now := testNow()
	cases := []struct {
		in    string
		title string
		want  string // "2006-01-02 15:04" in Chicago
	}{
		{"dinner with ryoko friday 7pm", "dinner with ryoko", "2026-10-02 19:00"},
		{"standup tomorrow 9am", "standup", "2026-09-29 09:00"},
		{"dentist 28 sep 14:30", "dentist", "2026-09-28 14:30"},
		{"deploy freeze next monday noon", "deploy freeze", "2026-10-05 12:00"},
		{"call mom today 6p", "call mom", "2026-09-28 18:00"},
		{"water plants tmr 7:00", "water plants", "2026-09-29 07:00"},
		{"meeting 2026-10-01 10:00", "meeting", "2026-10-01 10:00"},
		{"new year party 1 jan midnight", "new year party", "2027-01-01 00:00"},
		{"lunch fri 12:30pm", "lunch", "2026-10-02 12:30"},
		{"gym mon 12a", "gym", "2026-10-05 00:00"}, // Monday today, midnight passed → next Mon
	}
	for _, c := range cases {
		title, at, err := ParseReminder(c.in, now)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if title != c.title {
			t.Errorf("%q: title = %q, want %q", c.in, title, c.title)
		}
		if got := at.Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("%q: at = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseReminderRefusals(t *testing.T) {
	now := testNow()
	cases := []struct {
		in   string
		want string
	}{
		{"", qaErrJunk},
		{"maybe friday-ish sometime", qaErrJunk},
		{"dentist friday", qaErrTime},     // date-shaped, no time
		{"dentist 7pm", qaErrDate},        // time, no date
		{"friday 7pm", qaErrTitle},        // no title left
		{"dentist 31 feb 9am", qaErrJunk}, // impossible date
		{"blah blah blah", qaErrJunk},     // nothing recognizable
	}
	for _, c := range cases {
		_, _, err := ParseReminder(c.in, now)
		if err == nil {
			t.Errorf("%q: expected error, got nil", c.in)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%q: error = %q, want %q", c.in, err.Error(), c.want)
		}
	}
}

func TestParseReminderFridayTodayEdge(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	// Friday 2026-10-02 18:00 — "friday 7pm" is still later today.
	fri := time.Date(2026, 10, 2, 18, 0, 0, 0, loc)
	_, at, err := ParseReminder("movie friday 7pm", fri)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.Format("2006-01-02"); got != "2026-10-02" {
		t.Errorf("friday 7pm at 6pm Friday = %s, want today", got)
	}
	// Same Friday at 20:00 — 7pm passed, so next Friday.
	friLate := time.Date(2026, 10, 2, 20, 0, 0, 0, loc)
	_, at, err = ParseReminder("movie friday 7pm", friLate)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.Format("2006-01-02"); got != "2026-10-09" {
		t.Errorf("friday 7pm at 8pm Friday = %s, want next Friday", got)
	}
}

func TestParseReminderYearRollover(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	jan := time.Date(2026, 1, 5, 12, 0, 0, 0, loc)
	_, at, err := ParseReminder("party 28 dec 8pm", jan)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.Format("2006-01-02"); got != "2026-12-28" {
		t.Errorf("28 dec from January = %s, want 2026-12-28", got)
	}
	_, at, err = ParseReminder("party 3 jan 8pm", jan)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.Format("2006-01-02"); got != "2027-01-03" {
		t.Errorf("3 jan from 5 Jan = %s, want 2027-01-03", got)
	}
}

func TestOnCalendarFor(t *testing.T) {
	loc := time.UTC
	at := time.Date(2026, 10, 2, 19, 30, 0, 0, loc) // a Friday
	cases := []struct {
		repeat string
		want   string
	}{
		{RepeatOnce, "2026-10-02 19:30:00"},
		{RepeatDaily, "*-*-* 19:30:00"},
		{RepeatWeekdays, "Mon..Fri *-*-* 19:30:00"},
		{RepeatWeekly, "Fri *-*-* 19:30:00"},
	}
	for _, c := range cases {
		if got := OnCalendarFor(at, c.repeat); got != c.want {
			t.Errorf("repeat %q: got %q, want %q", c.repeat, got, c.want)
		}
	}
}

func TestNextOccurrence(t *testing.T) {
	loc := time.UTC
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, loc) // Fri 09:00
	after := time.Date(2026, 10, 2, 10, 0, 0, 0, loc)

	if got := NextOccurrence(RepeatOnce, at, after); !got.Equal(at) {
		t.Errorf("once: got %v, want %v", got, at)
	}
	if got := NextOccurrence(RepeatDaily, at, after); got.Format("2006-01-02 15:04") != "2026-10-03 09:00" {
		t.Errorf("daily: got %v", got)
	}
	// weekdays: Fri 09:00 passed, Sat/Sun skipped → Mon
	if got := NextOccurrence(RepeatWeekdays, at, after); got.Format("2006-01-02 15:04") != "2026-10-05 09:00" {
		t.Errorf("weekdays: got %v", got)
	}
	// weekly: next Friday
	if got := NextOccurrence(RepeatWeekly, at, after); got.Format("2006-01-02 15:04") != "2026-10-09 09:00" {
		t.Errorf("weekly: got %v", got)
	}
	// daily, still later today
	morning := time.Date(2026, 10, 2, 8, 0, 0, 0, loc)
	if got := NextOccurrence(RepeatDaily, at, morning); got.Format("2006-01-02 15:04") != "2026-10-02 09:00" {
		t.Errorf("daily later-today: got %v", got)
	}
}

func TestNewIDUnitSafe(t *testing.T) {
	id := newID(time.Now())
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Fatalf("id %q contains unit-unsafe rune %q", id, r)
		}
	}
	if newID(time.Now()) == id {
		t.Fatal("ids should be unique")
	}
}

func TestUnitFiles(t *testing.T) {
	dir := t.TempDir()
	r := Reminder{ID: "20260928-014800-abc123", Title: "dentist", Calendar: "2026-09-28 09:00:00", Repeat: RepeatOnce}
	if err := writeUnitsAt(dir, r); err != nil {
		t.Fatal(err)
	}
	timer, err := os.ReadFile(filepath.Join(dir, "navi-reminder-20260928-014800-abc123.timer"))
	if err != nil {
		t.Fatal(err)
	}
	ts := string(timer)
	for _, want := range []string{
		"OnCalendar=2026-09-28 09:00:00",
		"Persistent=true",
		"AccuracySec=1min",
		"Unit=navi-reminder-20260928-014800-abc123.service",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("timer unit missing %q\n%s", want, ts)
		}
	}
	svc, err := os.ReadFile(filepath.Join(dir, "navi-reminder-20260928-014800-abc123.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svc), "ExecStart=") || !strings.Contains(string(svc), "navi-reminder-fire 20260928-014800-abc123") {
		t.Errorf("service unit ExecStart wrong:\n%s", svc)
	}
	if !strings.Contains(string(svc), "Type=oneshot") {
		t.Errorf("service unit should be Type=oneshot:\n%s", svc)
	}
	// service carries only the id — no title/content baked in beyond Description
	if strings.Count(string(svc), "dentist") > 1 {
		t.Errorf("service unit should not bake in content:\n%s", svc)
	}
	if err := removeUnitsAt(dir, r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "navi-reminder-20260928-014800-abc123.timer")); !os.IsNotExist(err) {
		t.Error("timer file should be removed")
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Reminders) != 0 {
		t.Fatal("fresh registry should be empty")
	}
	now := time.Now().Truncate(time.Second)
	reg.Reminders = append(reg.Reminders, Reminder{
		ID: "x1", Title: "test", Calendar: "2026-09-28 09:00:00",
		Repeat: RepeatOnce, At: now, Created: now,
	})
	if err := SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}
	// 0600 on the registry
	fi, err := os.Stat(RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("registry mode = %o, want 600", fi.Mode().Perm())
	}

	reg2, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := reg2.Find("x1")
	if got == nil || got.Title != "test" || !got.At.Equal(now) {
		t.Fatalf("round trip failed: %+v", got)
	}
	if !reg2.Remove("x1") || reg2.Find("x1") != nil {
		t.Fatal("Remove failed")
	}
	if reg2.Remove("nope") {
		t.Fatal("Remove of missing id should return false")
	}
}

func TestMarkMissedRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	reg := &Registry{Reminders: []Reminder{{
		ID: "m1", Title: "miss me", Repeat: RepeatOnce,
		At: time.Now().Add(-time.Hour), Created: time.Now().Add(-2 * time.Hour),
	}}}
	if err := SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}
	if err := MarkMissed("m1"); err != nil {
		t.Fatal(err)
	}
	reg2, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := reg2.Find("m1")
	if got == nil || !got.Missed || got.MissedAt.IsZero() {
		t.Fatalf("missed not recorded: %+v", got)
	}
	if err := ClearMissed("m1"); err != nil {
		t.Fatal(err)
	}
	reg3, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if got := reg3.Find("m1"); got == nil || got.Missed {
		t.Fatalf("missed not cleared: %+v", got)
	}
}

func TestRelative(t *testing.T) {
	now := time.Date(2026, 9, 28, 1, 48, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-time.Hour), "past due"},
		{now.Add(30 * time.Second), "any moment"},
		{now.Add(20 * time.Minute), "in 20m"},
		{now.Add(3 * time.Hour), "in 3h"},
		{now.Add(30 * time.Hour), "tomorrow 07:48"},
		{now.Add(3 * 24 * time.Hour), "Thu 01:48"},
		{now.Add(30 * 24 * time.Hour), "28 Oct 01:48"},
	}
	for _, c := range cases {
		if got := Relative(c.at, now); got != c.want {
			t.Errorf("Relative(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}
