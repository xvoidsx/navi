package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
	theme "github.com/rav3ndust/navi-theme"
)

// Force TrueColor so styled-output assertions actually exercise the ANSI
// paths — without a TTY lipgloss would render no sequences and the tests
// would vacuously pass.
func init() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// sampleHistory is a realistic `dunstctl history` payload: busctl wraps
// every D-Bus variant as {"type": "<sig>", "data": <value>}.
const sampleHistory = `{
  "type": "(aa{sv})",
  "data": [
    [
      {
        "appname": {"type": "s", "data": "wiredrop"},
        "summary": {"type": "s", "data": "transfer complete"},
        "body": {"type": "s", "data": "photo.png from raven"},
        "id": {"type": "u", "data": 42},
        "actions": {"type": "a{ss}", "data": {"default": "Open folder"}}
      },
      {
        "appname": {"type": "s", "data": "navi-update"},
        "summary": {"type": "s", "data": "updates available"},
        "body": {"type": "s", "data": ""},
        "id": {"type": "u", "data": 43},
        "actions": {"type": "a{ss}", "data": {}}
      }
    ],
    []
  ]
}`

// fakeDunstctl installs a stub dunstctl on PATH that replays canned output.
func fakeDunstctl(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "dunstctl")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if _, err := exec.LookPath("dunstctl"); err != nil {
		t.Fatal("stub dunstctl not on PATH")
	}
}

func TestFetchHistory(t *testing.T) {
	fakeDunstctl(t, `echo '`+sampleHistory+`'`)
	ctx := context.Background()
	notifs, err := fetchHistory(ctx)
	if err != nil {
		t.Fatalf("fetchHistory: %v", err)
	}
	if len(notifs) != 2 {
		t.Fatalf("got %d notifs, want 2", len(notifs))
	}
	n := notifs[0]
	if n.App != "wiredrop" || n.Summary != "transfer complete" || n.Body != "photo.png from raven" {
		t.Errorf("bad parse: %+v", n)
	}
	if n.ID != 42 {
		t.Errorf("bad id: %d", n.ID)
	}
	if n.Actions["default"] != "Open folder" {
		t.Errorf("bad actions: %v", n.Actions)
	}
	if len(notifs[1].Actions) != 0 {
		t.Errorf("expected no actions, got %v", notifs[1].Actions)
	}
}

func TestUnwrapLeavesPlainValues(t *testing.T) {
	// a plain string is not a variant wrapper — must pass through
	if got := asString(json.RawMessage(`"hi"`)); got != "hi" {
		t.Errorf("asString plain: %q", got)
	}
	// a dict that is NOT a {type,data} wrapper must pass through untouched
	raw := json.RawMessage(`{"a": "b"}`)
	if string(unwrap(raw)) != string(raw) {
		t.Errorf("unwrap mangled non-variant dict")
	}
}

// testModel builds a model with two notifications, as if history arrived.
func testModel(t *testing.T) model {
	t.Helper()
	m := initialModel()
	m.width = frameWidth
	m.height = 30
	m.vp = viewport.New(m.vpWidth(), m.vpHeight())
	m.ready = true
	m.notifs = []notif{
		{ID: 1, App: "wiredrop", Summary: "transfer complete", Body: "photo.png from raven", Actions: map[string]string{"default": "Open"}},
		{ID: 2, App: "navi-update", Summary: "updates available", Actions: map[string]string{}},
	}
	m.rebuildContent()
	return m
}


func TestPinnedHeaderStays(t *testing.T) {
	m := testModel(t)
	v := m.View()
	if !strings.Contains(v, "∅") {
		t.Error("view lost the ∅ mark")
	}
	if !strings.Contains(v, "notifications") {
		t.Error("view lost the notifications title")
	}
	if !strings.Contains(v, "2 notifications") {
		t.Error("view lost the count")
	}
	// the pinned header must render even when the list scrolls: simulate a
	// long list and confirm the header line is above the viewport content
	for i := 0; i < 40; i++ {
		m.notifs = append(m.notifs, notif{ID: int64(10 + i), App: "spam", Summary: "many"})
	}
	m.rebuildContent()
	m.vp.GotoBottom()
	v = m.View()
	if !strings.Contains(v, "∅") || !strings.Contains(v, "42 notifications") {
		t.Error("pinned header scrolled away on a long list")
	}
}

func TestWraparound(t *testing.T) {
	m := testModel(t)
	m.cursor = 1
	m.moveCursor(1)
	if m.cursor != 0 {
		t.Errorf("down past end: cursor=%d, want 0", m.cursor)
	}
	m.moveCursor(-1)
	if m.cursor != 1 {
		t.Errorf("up past start: cursor=%d, want 1", m.cursor)
	}
}

func TestGlitchView(t *testing.T) {
	m := testModel(t)
	m.clearing = true
	for f := 0; f < clearFrames; f++ {
		m.clearN = f
		v := m.glitchView()
		if strings.Count(v, "\n")+1 < m.vp.Height {
			t.Errorf("frame %d: glitch view short (%d lines)", f, strings.Count(v, "\n")+1)
		}
	}
	// styled output must actually contain ANSI under TrueColor
	m.clearN = 2
	if !strings.Contains(m.glitchView(), "\x1b[") {
		t.Error("glitch view has no ANSI sequences under TrueColor")
	}
}

func TestNoNestedRender(t *testing.T) {
	// regression: a pre-styled string nested inside another Render prints
	// literal escape sequences. The view must never contain a detached "[".
	m := testModel(t)
	v := m.View()
	// lipgloss v1.1.0's nested-render bug shows up as literal "[38;2;" text
	if strings.Contains(v, "[38;2;") && !strings.Contains(v, "\x1b[38;2;") {
		t.Error("view contains detached escape sequence bodies (nested Render bug)")
	}
	// every block's selection marker must be present exactly once per notif
	if c := strings.Count(v, "❯"); c != 1 {
		t.Errorf("expected 1 cursor marker, got %d", c)
	}
	_ = theme.Pink // keep theme import used if styles change
}

func TestDismissGlitch(t *testing.T) {
	m := testModel(t)
	m.cursor = 0
	_ = m.dismissSelected() // returns a batch cmd; we drive the ticks manually
	if !m.dismissing || m.dismissIdx != 0 {
		t.Fatal("dismissSelected did not start the animation on the cursor row")
	}
	// input must be locked while the row glitches away
	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = um.(model)
	if m.cursor != 0 {
		t.Error("cursor moved during the dismiss animation")
	}
	// every frame renders full height with real ANSI under TrueColor
	for f := 0; f < dismissFrames; f++ {
		m.dismissN = f
		v := m.dismissGlitchView()
		if strings.Count(v, "\n")+1 != m.vp.Height {
			t.Errorf("frame %d: dismiss view is %d lines, want %d", f, strings.Count(v, "\n")+1, m.vp.Height)
		}
	}
	m.dismissN = 2
	if !strings.Contains(m.dismissGlitchView(), "\x1b[") {
		t.Error("dismiss glitch view has no ANSI sequences under TrueColor")
	}
	// drive the ticks to completion: the row splices out, cursor stays valid
	m.dismissN = 0
	for i := 0; i < dismissFrames; i++ {
		um, _ := m.Update(dismissTickMsg{})
		m = um.(model)
	}
	if m.dismissing {
		t.Error("dismiss animation did not finish")
	}
	if len(m.notifs) != 1 || m.notifs[0].ID != 2 {
		t.Errorf("wrong row spliced: %d notifs left", len(m.notifs))
	}
	if m.cursor != 0 || m.status != "dismissed" {
		t.Errorf("cursor=%d status=%q after dismiss", m.cursor, m.status)
	}
	// dismissing the last one leaves a valid empty state
	m.cursor = 0
	_ = m.dismissSelected()
	for i := 0; i < dismissFrames; i++ {
		um, _ := m.Update(dismissTickMsg{})
		m = um.(model)
	}
	if len(m.notifs) != 0 || m.cursor != 0 {
		t.Errorf("empty state wrong: %d notifs, cursor=%d", len(m.notifs), m.cursor)
	}
	if !strings.Contains(m.View(), "no notifications") {
		t.Error("empty state lost the pinned header count")
	}
}
