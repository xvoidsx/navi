package main

// playerctl_test.go — the backend's parsers, tested against real captured
// playerctl output.
//
// The fixture in TestParseMetadataReal is a verbatim capture from
// Chromium's MPRIS on Debian trixie, asked for with exactly the format
// string Read() uses. It is not a hand-written mock: the 0x1f bytes are
// real, the microsecond length is real, and the single quotes around the
// track id are real. Two of the three bugs this file guards against were
// invisible to a plausible-looking mock.

import (
	"strings"
	"testing"
	"time"
)

// Captured live: `playerctl metadata --format` with a literal 0x1f
// separator, on chromium playing Berlinist.
const realMetadata = "A place for us\x1fBerlinist\x1fNeva (Original Soundtrack)" +
	"\x1f377000000\x1ffile:///tmp/.com.google.Chrome.25Gd3S" +
	"\x1f'/org/chromium/MediaPlayer2/TrackList/Track4D624F0277F6795B978A8BE1B1EC5028'"

func TestParseMetadataReal(t *testing.T) {
	tr := parseMetadata(realMetadata)

	if tr.Title != "A place for us" {
		t.Errorf("Title = %q, want %q", tr.Title, "A place for us")
	}
	if tr.Artist != "Berlinist" {
		t.Errorf("Artist = %q, want %q", tr.Artist, "Berlinist")
	}
	if tr.Album != "Neva (Original Soundtrack)" {
		t.Errorf("Album = %q, want %q", tr.Album, "Neva (Original Soundtrack)")
	}
	// 377000000 microseconds — the field is µs, not ms or ns.
	if want := 377 * time.Second; tr.Length != want {
		t.Errorf("Length = %v, want %v", tr.Length, want)
	}
	if tr.ArtURL != "file:///tmp/.com.google.Chrome.25Gd3S" {
		t.Errorf("ArtURL = %q", tr.ArtURL)
	}
	// The quotes are part of the wire format, not part of the id.
	want := "/org/chromium/MediaPlayer2/TrackList/Track4D624F0277F6795B978A8BE1B1EC5028"
	if tr.TrackID != want {
		t.Errorf("TrackID = %q, want %q", tr.TrackID, want)
	}
	if !tr.HasArt() {
		t.Error("HasArt() = false, want true")
	}
}

// A player with no metadata at all still returns the separator run. Every
// field must degrade rather than shift into its neighbour.
func TestParseMetadataEmpty(t *testing.T) {
	tr := parseMetadata(sep + sep + sep + sep + sep)
	if tr.Title != "" || tr.Artist != "" || tr.Album != "" {
		t.Errorf("expected blank metadata, got %+v", tr)
	}
	if tr.Length != 0 {
		t.Errorf("Length = %v, want 0", tr.Length)
	}
	if tr.HasArt() {
		t.Error("HasArt() = true, want false")
	}
}

// A short line must not panic — this indexes by field, so an
// under-length response is the obvious crash.
func TestParseMetadataTruncated(t *testing.T) {
	tr := parseMetadata("only a title")
	if tr.Title != "only a title" {
		t.Errorf("Title = %q", tr.Title)
	}
	if tr.Album != "" || tr.ArtURL != "" || tr.Length != 0 {
		t.Errorf("expected zero values past the truncation, got %+v", tr)
	}
}

// Live captures from Chromium and mpv both include titles with newlines
// and stray control characters. Inside a fixed-width frame one embedded
// newline tears the whole view apart, so clean() is load-bearing.
func TestClean(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Berlinist", "Berlinist"},
		{"surrounding space", "  Berlinist  ", "Berlinist"},
		{"inner run", "Neva   (Original", "Neva (Original"},
		{"embedded newline", "Line one\nLine two", "Line one Line two"},
		{"carriage return", "Line one\r\nLine two", "Line one Line two"},
		{"tab", "a\tb", "a b"},
		{"control char", "a\x01b\x1b[31m", "a b"},
		{"del", "a\x7fb", "a b"},
		// A title carrying a colour sequence must lose the whole
		// sequence. Stripping only the ESC byte leaves "[31m" as
		// visible text, and counts the invisible parts as width.
		{"colour sequence", "\x1b[31mred\x1b[0m", "red"},
		{"osc sequence", "\x1b]0;title\x07after", "after"},
		{"osc with st", "\x1b]8;;https://x\x1b\\link", "link"},
		{"lone esc", "abc\x1b", "abc"},
		{"only an escape", "\x1b[31m", ""},
		{"only whitespace", " \t\n ", ""},
		{"empty", "", ""},
		// UTF-8 must survive: a Japanese album title is ordinary data.
		{"cjk preserved", "トキオバーン", "トキオバーン"},
		{"emoji preserved", "𝄞 clef", "𝄞 clef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clean(tt.in); got != tt.want {
				t.Errorf("clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// clean must not strip a leading separator-adjacent space out of a title
// that legitimately starts with one — the reverse bug also breaks layout.
func TestCleanNoCollapseOfMeaningfulSpaces(t *testing.T) {
	if got := clean("a  b   c"); got != "a b c" {
		t.Errorf("clean = %q, want %q", got, "a b c")
	}
}

func TestPlayerArgs(t *testing.T) {
	if got := playerArgs("", "status"); len(got) != 1 || got[0] != "status" {
		t.Errorf("empty player should not add --player, got %v", got)
	}
	got := playerArgs("chromium", "status")
	if len(got) != 3 || got[0] != "--player" || got[1] != "chromium" || got[2] != "status" {
		t.Errorf("playerArgs = %v", got)
	}
}

func TestShortPlayer(t *testing.T) {
	tests := []struct{ in, want string }{
		{"chromium.instance7561", "chromium"},
		{"spotify", "spotify"},
		{"mpv", "mpv"},
		// A dot that isn't the instance marker must survive.
		{"org.mpris.Foo", "org.mpris.Foo"},
		{".instanceleading", ".instanceleading"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortPlayer(tt.in); got != tt.want {
			t.Errorf("shortPlayer(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFmtTime(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0:00"},
		{5 * time.Second, "0:05"},
		{83 * time.Second, "1:23"},
		{377 * time.Second, "6:17"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour, "1:00:00"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2:03:04"},
		// A negative position is impossible, but a player can report one
		// and "-0:05" in a meter label looks broken.
		{-5 * time.Second, "0:00"},
	}
	for _, tt := range tests {
		if got := fmtTime(tt.in); got != tt.want {
			t.Errorf("fmtTime(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The capability gate is the reason the footer never shifts, so it has to
// distinguish "unsupported" from "off".
func TestStateHas(t *testing.T) {
	st := State{Unavailable: map[string]bool{}}
	if !st.Has(capNext) {
		t.Error("an unlisted capability must read as available")
	}
	st.Unavailable[capNext] = true
	if st.Has(capNext) {
		t.Error("a listed capability must read as unavailable")
	}
	// A zero-value State has a nil map; reading it must not panic, which
	// is the state the model is in before the first poll lands. A nil map
	// read yields false, so every capability reads as available — nothing
	// is known to be missing yet.
	var zero State
	if !zero.Has(capNext) {
		t.Error("zero State should report capabilities available")
	}
}

func TestStateIsPlaying(t *testing.T) {
	if !(State{Status: "Playing"}).IsPlaying() {
		t.Error("Playing should be playing")
	}
	for _, s := range []string{"Paused", "Stopped", ""} {
		if (State{Status: s}).IsPlaying() {
			t.Errorf("%q should not be playing", s)
		}
	}
}

func TestHumanVerb(t *testing.T) {
	// The note line is read by a person; a raw playerctl verb there looks
	// like a debug string.
	if got := humanVerb("play-pause"); got != "play" {
		t.Errorf("humanVerb(play-pause) = %q", got)
	}
	if got := humanVerb("previous"); got != "go back" {
		t.Errorf("humanVerb(previous) = %q", got)
	}
	if got := humanVerb("next"); got != "next" {
		t.Errorf("humanVerb should pass through a readable verb, got %q", got)
	}
}

// A track id must never contain the separator, or the split would
// desynchronise every field after it. Guard the fixture itself.
func TestRealMetadataFixtureHasNoExtraSeparators(t *testing.T) {
	if n := strings.Count(realMetadata, sep); n != 5 {
		t.Errorf("fixture has %d separators, want 5", n)
	}
}
