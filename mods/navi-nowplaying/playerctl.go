package main

// playerctl.go — the MPRIS backend.
//
// Everything that talks to playerctl lives here so the Bubble Tea model
// never shells out itself. The model receives finished State values.
//
// The real shape of playerctl on Debian trixie (playerctl 2.4.1), learned
// the hard way — check it against a live player before changing anything:
//
//   - --format does NOT interpret escapes. "{{a}}\x1f{{b}}" emits the four
//     literal characters \x1f, not a separator. A *literal* byte works, so
//     the delimiter below is a real 0x1f rune in the Go string, not an
//     escape playerctl has to parse.
//   - a player can lack a capability entirely, and answers with "No
//     player could handle this command" while other verbs work fine.
//     Chromium's MPRIS does exactly this. That is a capability gap, not
//     an error, and the UI dims the affected keys rather than hiding
//     them.
//
// Deliberately absent, and all three for the same reason — navi already
// owns them:
//
//   - volume. `navi-audio` is a full PulseAudio widget and the bar
//     carries a volume module. A second volume control is two places to
//     look and a second place to be wrong.
//   - shuffle and loop. Properties of how someone is listening, not of
//     what is playing.
//   - the track list. playerctl 2.4.1 has no `playlist` command at all
//     (it arrived in 3.x), so on trixie the feature could only ever
//     render a "this player can't do that" screen. An explanation aimed
//     at MPRIS implementors is worse than no feature for the person
//     looking at it.
//
// One trap is worth keeping even though nothing here uses it any more,
// because it is the shape of the whole interface. A Set can exit 0 and
// do nothing at all: Chromium has no MPRIS Volume property, so
// `playerctl volume 0.4` returned success and a read-back reported 1.0
// forever. "The call returned 0" is not evidence that the player did
// anything. Any write added later has to be verified by reading back what
// the player actually did, or it will look like it works while doing
// nothing — which is a worse bug than a missing feature, because it is
// silent.

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// sep delimits metadata fields. 0x1f (unit separator) is the POSIX
// choice for "cannot appear in text"; MPRIS strings never carry it.
const sep = "\x1f"

// Track is the current item's metadata.
type Track struct {
	Title   string
	Artist  string
	Album   string
	ArtURL  string
	TrackID string
	Length  time.Duration
}

// HasArt reports whether the player offered cover art at all.
func (t Track) HasArt() bool { return strings.TrimSpace(t.ArtURL) != "" }

// State is one full read of a player.
type State struct {
	Player   string
	Track    Track
	Status   string // Playing | Paused | Stopped
	Position time.Duration
	// Unavailable records which capabilities this player does not
	// implement, so the UI can dim those keys instead of dropping them.
	// The footer never shifts — see mods/AGENTS.md.
	Unavailable map[string]bool

	Err string
}

// IsPlaying reports whether the position meter should advance.
func (s State) IsPlaying() bool { return s.Status == "Playing" }

// Has reports whether a named capability is usable.
func (s State) Has(cap string) bool { return !s.Unavailable[cap] }

// capability names, used both as playerctl verbs and as dim-footer keys.
const (
	capNext = "next"
	capPrev = "previous"
	capSeek = "position"
)

// pc runs playerctl and returns trimmed stdout. A non-zero exit is the
// caller's business — several "failures" here are capability answers.
func pc(args ...string) (string, error) {
	out, err := exec.Command("playerctl", args...).Output()
	s := strings.TrimRight(string(out), "\n")
	if err != nil {
		// playerctl reports capability gaps on stderr, and its text is the
		// only way to tell "player is busy" from "player can't do this".
		if ee, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(ee.Stderr))
			if msg == "" {
				msg = s
			}
			return s, &unsupportedErr{msg}
		}
		return s, err
	}
	return s, nil
}

// unsupportedErr marks "this player/command doesn't do that", as opposed
// to a real failure. Callers demote it to a dimmed key.
type unsupportedErr struct{ msg string }

func (e *unsupportedErr) Error() string { return e.msg }

func unsupported(err error) bool {
	_, ok := err.(*unsupportedErr)
	return ok
}

// havePlayer reports whether playerctl exists at all. The mod is useless
// without it, but it should say so once rather than spam the footer.
func havePlayer() bool {
	_, err := exec.LookPath("playerctl")
	return err == nil
}

// Players lists the MPRIS players currently on the bus.
func Players() []string {
	out, err := exec.Command("playerctl", "-l").Output()
	if err != nil {
		return nil
	}
	var ps []string
	for _, l := range strings.Split(string(out), "\n") {
		if p := clean(l); p != "" {
			ps = append(ps, p)
		}
	}
	return ps
}

// Read pulls one full State from the named player ("" = whatever playerctl
// picks). Reads are independent: a capability the player lacks fills its
// Unavailable flag and leaves the rest of the state intact, so one gap
// never blanks the whole mod.
func Read(player string) State {
	st := State{Player: player, Unavailable: map[string]bool{}}

	arg := func(extra ...string) []string {
		if player == "" {
			return extra
		}
		return append([]string{"--player", player}, extra...)
	}

	// --- metadata: one call, literal-separator delimited -----------------
	meta, err := pc(arg("metadata", "--format",
		"{{xesam:title}}"+sep+"{{xesam:artist}}"+sep+"{{xesam:album}}"+sep+
			"{{mpris:length}}"+sep+"{{mpris:artUrl}}"+sep+"{{mpris:trackid}}")...)
	if err != nil {
		// A player with no MPRIS metadata at all is still a real player;
		// the title simply stays blank.
		st.Err = clean(err.Error())
	} else {
		st.Track = parseMetadata(meta)
	}

	// --- scalars ---------------------------------------------------------
	if s, err := pc(arg("status")...); err == nil {
		st.Status = clean(s)
	} else {
		st.Err = firstErr(st.Err, err)
	}

	if s, err := pc(arg("position")...); err == nil {
		if secs, err := strconv.ParseFloat(clean(s), 64); err == nil {
			st.Position = time.Duration(secs * float64(time.Second))
		}
	} else {
		st.Unavailable[capSeek] = true
	}

	return st
}

// parseMetadata splits the delimited metadata line. Missing fields degrade
// to their zero value; mpris:length is microseconds.
//
// The track id arrives wrapped in single quotes — the real capture is
// '/org/chromium/MediaPlayer2/TrackList/Track4D62...'. Chromium's MPRIS
// returns a Python-style repr, so the quotes are stripped or every later
// comparison against the id is quietly off by two characters.
func parseMetadata(line string) Track {
	parts := strings.Split(line, sep)
	get := func(i int) string {
		if i < len(parts) {
			return clean(parts[i])
		}
		return ""
	}
	t := Track{
		Title:   get(0),
		Artist:  get(1),
		Album:   get(2),
		ArtURL:  get(4),
		TrackID: strings.Trim(get(5), "'"),
	}
	if us, err := strconv.ParseInt(get(3), 10, 64); err == nil && us > 0 {
		t.Length = time.Duration(us) * time.Microsecond
	}
	return t
}

// Do runs a playerctl control verb. Position takes a value; the rest are
// bare. A failure is reported, never silently swallowed.
func Do(player, verb string, value ...string) error {
	_, err := pc(playerArgs(player, append([]string{verb}, value...)...)...)
	return err
}

func playerArgs(player string, args ...string) []string {
	if player == "" {
		return args
	}
	return append([]string{"--player", player}, args...)
}

// clean normalises a playerctl field for display: escape sequences go,
// remaining control characters go, runs of whitespace collapse to one
// space, edges trimmed. Track titles arrive with embedded newlines
// constantly, and a stray newline inside a single-line TUI cell tears the
// frame apart.
//
// The escape pass is not paranoia. Stripping only the ESC byte would
// leave the rest of the sequence — a title containing "\x1b[31m" renders
// as the literal text "[31m" and, worse, the invisible parts then get
// counted as visible width. Only ever applied to playerctl output, never
// to strings this program has styled itself.
func clean(s string) string {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = stripEscapes(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			space = true
		case r == ' ':
			space = true
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripEscapes removes CSI and OSC escape sequences. A trailing lone ESC
// is dropped too, so an unterminated sequence can't leak its body.
func stripEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++
		if i >= len(s) {
			break // lone trailing ESC
		}
		switch s[i] {
		case '[': // CSI: parameter and intermediate bytes, then a final
			i++
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++ // the final byte
			}
		case ']': // OSC: runs until BEL or ST
			i++
			for i < len(s) && s[i] != 0x07 {
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
				i++
			}
			if i < len(s) {
				i++
			}
		default: // two-byte escape
			i++
		}
	}
	return b.String()
}

func firstErr(cur string, err error) string {
	if cur != "" {
		return cur
	}
	if err == nil {
		return ""
	}
	return clean(err.Error())
}
