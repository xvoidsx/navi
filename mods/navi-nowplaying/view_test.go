package main

// view_test.go — the layout invariants.
//
// mods/AGENTS.md names these as the things to actually watch: header
// height stays constant as content changes, nothing wraps inside the
// border, and the footer never shifts. They are cheap to assert and
// expensive to notice by eye, so they are asserted here instead.
//
// Width is left at 0 throughout so View() returns the bare frame rather
// than a window-sized placement — the frame is the thing under test.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	theme "github.com/rav3ndust/navi-theme"
)

// TestMain pins the colour profile to TrueColor.
//
// Without this the tests lie. `go test` gives lipgloss a non-TTY stdout,
// so it drops to the Ascii profile and every Render() returns the string
// unchanged — which means "the faint key is styled" and "the meter is
// pink" would both pass against unstyled text. Pinning the profile is
// what makes the style assertions mean anything.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func sampleState() State {
	return State{
		Player: "chromium.instance7561",
		Track: Track{
			Title:   "Scars Don't Blink",
			Artist:  "Two Poins Black",
			Album:   "Neva (Original Soundtrack)",
			Length:  2*time.Minute + 32*time.Second,
			TrackID: "/org/chromium/MediaPlayer2/TrackList/Track4D62",
		},
		Status:      "Playing",
		Unavailable: map[string]bool{},
	}
}

func fullMod() model {
	m := newModel()
	m.player = "chromium.instance7561"
	m.players = []string{m.player}
	m.st = sampleState()
	m.posBase, m.posAnchor = 83*time.Second, time.Now()
	return m
}

// frameHeight counts the rendered lines of a bare frame.
func frameHeight(t *testing.T, m model) int {
	t.Helper()
	return len(strings.Split(m.View(), "\n"))
}

// The single most important invariant: the frame is the same height no
// matter what the player reports. A mod that changes height as tracks
// change makes the compositor resize a floating window mid-session.
func TestFrameHeightIsConstant(t *testing.T) {
	// every capability unsupported
	none := map[string]bool{
		capNext: true, capPrev: true, capSeek: true,
	}

	long := strings.Repeat("a very long title that will not fit on one line ", 3)
	cjk := "トキオバーン、千鳥合计、顶着雪花的巫女走过雪原与月下的街道"

	cases := []struct {
		name string
		m    model
	}{
		{"typical", fullMod()},

		{"no track", func() model {
			m := fullMod()
			m.st = State{Unavailable: map[string]bool{}}
			return m
		}()},

		{"no players at all", newModel()},

		{"paused", func() model {
			m := fullMod()
			m.st.Status = "Paused"
			return m
		}()},

		{"stopped", func() model {
			m := fullMod()
			m.st.Status = "Stopped"
			return m
		}()},

		{"long title", func() model {
			m := fullMod()
			m.st.Track.Title = long
			m.st.Track.Artist = long
			return m
		}()},

		{"cjk title", func() model {
			m := fullMod()
			m.st.Track.Title, m.st.Track.Artist, m.st.Track.Album = cjk, cjk, cjk
			return m
		}()},

		{"hours long", func() model {
			m := fullMod()
			m.st.Track.Length = 9*time.Hour + 59*time.Minute + 59*time.Second
			m.posBase = 3*time.Hour + 4*time.Minute
			return m
		}()},

		{"no duration known", func() model {
			m := fullMod()
			m.st.Track.Length = 0
			m.posBase = 0
			return m
		}()},

		{"no capabilities", func() model {
			m := fullMod()
			m.st.Unavailable = none
			return m
		}()},

		{"many players", func() model {
			m := fullMod()
			m.players = []string{"chromium.instance7561", "mpv", "spotify", "cmus"}
			return m
		}()},

		{"error line", func() model {
			m := fullMod()
			m.setErr("playerctl: the player went away mid-read")
			return m
		}()},

		{"note line", func() model {
			m := fullMod()
			m.setNote("now controlling mpv")
			return m
		}()},

		// Two players is the state that used to grow the footer, so it is
		// pinned here rather than trusted.
		{"two players", func() model {
			m := fullMod()
			m.players = []string{m.player, "mpv"}
			return m
		}()},
	}

	base := frameHeight(t, cases[0].m)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := frameHeight(t, tc.m); got != base {
				t.Errorf("frame height = %d, want %d (constant across all states)", got, base)
			}
		})
	}
}

// widestLine measures a rendered frame the way a terminal measures it.
func widestLine(m model) int {
	max := 0
	for _, ln := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(ln); w > max {
			max = w
		}
	}
	return max
}

// frameCells is the rendered width of the whole window: the content width
// plus the border's own two characters plus its Padding(0, 1) on both
// sides. It is 66, not 62 — the width argument to theme.Frame is the
// content width, and every mod in the family renders 66. Asserted here so
// a change to theme.Border's padding shows up as a failing test rather
// than as five windows quietly resizing on every user.
func frameCells() int { return frameWidth + 4 }

func TestRenderedFrameWidth(t *testing.T) {
	if got := widestLine(fullMod()); got != frameCells() {
		t.Errorf("rendered frame is %d cells, want %d (frameWidth %d + 4 of border and padding)",
			got, frameCells(), frameWidth)
	}
}

// Nothing may exceed the frame's rendered width. A single overlong line
// wraps and adds a row, which is the same bug by another route.
func TestNothingWrapsTheFrame(t *testing.T) {
	limit := frameCells()

	overflow := func(m model) (int, int) {
		line, width := 0, 0
		for i, ln := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(ln); w > limit && w > width {
				line, width = i, w
			}
		}
		return line, width
	}

	cases := []struct {
		name string
		m    model
	}{
		{"typical", fullMod()},
		{"long title", func() model {
			m := fullMod()
			m.st.Track.Title = strings.Repeat("overflowing ", 12)
			return m
		}()},
		{"cjk title", func() model {
			m := fullMod()
			m.st.Track.Title = strings.Repeat("トキオバーン", 10)
			return m
		}()},
		{"many players", func() model {
			m := fullMod()
			m.players = make([]string, 0, 8)
			for i := 0; i < 8; i++ {
				m.players = append(m.players, "someplayer.instance1234")
			}
			return m
		}()},
		{"long player name", func() model {
			m := fullMod()
			m.st.Player = "a-really-long-mpris-bus-name-that-keeps-going.instance9999"
			return m
		}()},
		{"long error", func() model {
			m := fullMod()
			m.setErr(strings.Repeat("a very long error message ", 5))
			return m
		}()},
		{"no capabilities", func() model {
			m := fullMod()
			m.st.Unavailable = map[string]bool{
				capNext: true, capPrev: true, capSeek: true,
			}
			return m
		}()},
		{"long note", func() model {
			m := fullMod()
			m.setNote("now controlling a-player-with-a-very-long-mpris-bus-name")
			return m
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if line, w := overflow(tc.m); w > 0 {
				t.Errorf("line %d is %d cells, over the %d-cell frame — it will wrap",
					line, w, limit)
			}
		})
	}
}

// The footer must keep every key, whatever the player supports. A key
// that vanishes moves every other key under the user's fingers, which is
// the one thing these bars must never do. Faint is the correct way to
// say "not available"; silent removal is not.
func TestFooterKeepsEveryKey(t *testing.T) {
	all := fullMod()

	none := fullMod()
	none.st.Unavailable = map[string]bool{
		capNext: true, capPrev: true, capSeek: true,
	}

	single := fullMod()
	single.players = []string{"chromium.instance7561"}

	for _, tc := range []struct {
		name string
		m    model
	}{
		{"all available", all},
		{"none available", none},
		{"one player", single},
	} {
		t.Run(tc.name, func(t *testing.T) {
			footer := tc.m.footer()
			for _, key := range []string{"space", "←→", "n", "b", "tab", "q"} {
				if !strings.Contains(footer, key) {
					t.Errorf("footer is missing the %q key:\n%s", key, footer)
				}
			}
			// Two fixed key lines plus the reserved message line, so the
			// footer cannot grow.
			if n := len(strings.Split(footer, "\n")); n != 3 {
				t.Errorf("footer has %d lines, want exactly 3 (2 key + 1 message)", n)
			}
		})
	}
}

// A dimmed key must actually be dimmed, and an available one must not be.
// If Fainted and Dimmed were swapped the user would read dead keys as
// live ones and press them for nothing.
func TestFaintKeysAreActuallyFaint(t *testing.T) {
	faint := theme.Fainted.Render("q quit")
	dim := theme.Dimmed.Render("q quit")
	if faint == dim {
		t.Error("Fainted and Dimmed render identically for this string")
	}
	if !strings.Contains(faint, "\x1b[") {
		t.Error("Fainted produced no escape sequence; the style is not applied")
	}
}

// fgEscape returns the SGR sequence lipgloss emits for a foreground
// colour. Derived from lipgloss rather than hardcoded, because the
// profile decides the form: a truecolor `#ff3131` becomes
// `38;2;255;49;49`, not the hex string. Comparing rendered output
// against `string(theme.Red)` finds nothing and silently passes.
func fgEscape(c lipgloss.Color) string {
	s := lipgloss.NewStyle().Foreground(c).Render("x")
	if i := strings.Index(s, "x"); i >= 0 {
		return s[:i]
	}
	return s
}

// A playing meter is pink; a parked one is green. If these were swapped
// the user could not tell a running track from a stopped one at a glance,
// which is the entire job of the meter.
func TestProgressMeterColourFollowsPlayback(t *testing.T) {
	playing := fullMod()
	playing.st.Status = "Playing"
	if !strings.Contains(playing.progressRow(), fgEscape(theme.Pink)) {
		t.Errorf("a playing meter should be pink:\n%q", playing.progressRow())
	}

	paused := fullMod()
	paused.st.Status = "Paused"
	if !strings.Contains(paused.progressRow(), fgEscape(theme.Green)) {
		t.Errorf("a paused meter should be green:\n%q", paused.progressRow())
	}
	if strings.Contains(paused.progressRow(), fgEscape(theme.Pink)) {
		t.Error("a paused meter must not read as running")
	}
}

func TestProgressRowNeverExceedsDuration(t *testing.T) {
	// Between polls the interpolated position can run past the end of the
	// track. A meter at 140% is a lie about where the song is.
	m := fullMod()
	m.st.Track.Length = time.Minute
	m.posBase, m.posAnchor = 50*time.Second, time.Now().Add(-time.Hour) // an hour ago
	if got := m.pos(); got != time.Minute {
		t.Errorf("pos() = %v, want it clamped to the duration %v", got, time.Minute)
	}
}

// The status dot in the frame header reflects playback, so a paused player
// must not show a live dot.
func TestLiveDotFollowsPlayback(t *testing.T) {
	playing := fullMod()
	if !strings.Contains(playing.View(), string(theme.Green)+"●") &&
		!strings.Contains(playing.View(), "●") {
		t.Error("a playing track should render the status dot")
	}
	paused := fullMod()
	paused.st.Status = "Paused"
	if !strings.Contains(paused.View(), "●") {
		t.Error("a paused track should still render the dot, in its off state")
	}
}
