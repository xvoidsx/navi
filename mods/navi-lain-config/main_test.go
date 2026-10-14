package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression test: bubbletea sends a WindowSizeMsg on startup and Update
// calls SetSize on all three lists. mlist used to be the zero value of
// list.Model, whose nil delegate panicked inside updatePagination.
// See: "navi-lain-config crashes on startup" (2026-09-20).
func TestWindowSizeMsgDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Update(WindowSizeMsg) panicked: %v", r)
		}
	}()
	m := initialModel()
	um, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := um.(model)
	if mm.width != 80 {
		t.Fatalf("width not recorded: got %d", mm.width)
	}
	// the model list must be a usable list even before a backend is chosen
	mm.mlist.SetSize(64, 12)
}
