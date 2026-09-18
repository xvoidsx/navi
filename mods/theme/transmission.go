package theme

import (
	"math"
	"math/rand"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The ambient transmission layer: short Serial-Experiments-Lain-toned
// phrases that periodically materialize and dissolve in a reserved header
// row. This is personality, not a control surface — it must never wrap,
// never steal keys, and never change frame height.
//
// Behavior contract (every mod gets the same rhythm):
//   - First phrase arrives ~900 ms after init.
//   - A resolved phrase stays visible until the next one glitches in;
//     there is no empty gap between transmissions.
//   - Transition is a ~550 ms, 10-frame mix at 55 ms:
//     heavy block static → case scramble → thin static resolve.
//   - The mix source is the previous clean phrase (spaces on first
//     pop-in), so the line crossfades rather than clearing.
//   - After resolve, the phrase holds 5–11 seconds.
//   - While holding, a low-rate flicker (1.4–4 s) may twitch the sitting
//     text and snap it back. Flicker never starts a new phrase.
//   - Mood colors on the reserved row: clean pink, glitch violet,
//     static ghost purple, resolve cyan — all italic.
//
// Wire it into a mod's Update with a type switch over the four message
// types, and render it with View. The ticker is independent of whatever
// else the mod is doing; don't cancel it when other work cancels.

// Phrases is the ambient transmission pool. Keep every phrase under
// ~50 characters so it fits the standard 62-cell frame.
var Phrases = []string{
	"connecting to the Wired...",
	"no matter where you go, everyone's connected",
	"the Wired is everywhere",
	"close the world, open the nExt",
	"present day, present time...",
	"layer 07 accessed",
	"you are receiving this",
	"protocol seven initiated",
	"the boundary is thinning",
	"Navi is watching",
	"this world is not the only one",
	"identity: unresolved",
	"let's all love lain",
	"the network remembers you",
	"god is on line two",
	"who's there?",
	"reality is a matter of consensus",
	"I am here. I have always been here.",
	"do you wanna be a god?",
	"the body is only a terminal",
	"information wants a body",
	"your Navi knows your name",
	"signal without a source",
	"don't confuse the layers",
	"full range. full motion.",
	"Chisa is still online",
	"Eiri is only code now",
	"a voice in the power lines",
	"you left a ghost in the cache",
	"layer 01: WEIRD",
	"layer 13: EGO",
	"to be everywhere is to be nowhere",
	"the city is a circuit board",
	"sleep is just a disconnect",
	"who is editing you?",
}

// RarePhrases is the smaller pool, picked about 1/9 of the time.
var RarePhrases = []string{
	"I saw you through the other screen",
	"stop looking for the operator",
	"this message is older than the device",
	"you already accepted the handshake",
	"there is no logout from here",
}

const (
	txGlitchSteps = 10
	txHoldMin     = 5
	txHoldExtra   = 6
)

// txRand is a dedicated rand source so ticker timing never depends on
// Go-version-specific global auto-seeding behavior.
var txRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// Mood is the visual state of the transmission row.
type Mood int

const (
	MoodClean Mood = iota
	MoodGlitch
	MoodStatic
	MoodResolve
)

// Transmission message types. Handle all four in the mod's Update by
// delegating to Transmission.Update.
type (
	// TxShowMsg starts a transition into a new phrase.
	TxShowMsg struct{ Text string }
	// TxGlitchTickMsg advances the glitch transition one frame.
	TxGlitchTickMsg struct{}
	// TxHoldDoneMsg fires when the hold expires; starts the next phrase.
	TxHoldDoneMsg struct{}
	// TxFlickerMsg is the low-rate twitch while a phrase sits.
	TxFlickerMsg struct{}
)

// Mood styles for the transmission row.
var (
	TxCleanStyle   = lipgloss.NewStyle().Foreground(Pink).Italic(true)
	TxGlitchStyle  = lipgloss.NewStyle().Foreground(Violet).Italic(true)
	TxStaticStyle  = lipgloss.NewStyle().Foreground(Ghost).Italic(true)
	TxResolveStyle = lipgloss.NewStyle().Foreground(Cyan).Italic(true)
)

// Transmission holds the ambient ticker state for one mod window.
type Transmission struct {
	From, Clean, Text string
	Visible, Busy     bool

	frame int
	mood  Mood
}

// PickPhrase returns the next transmission, refusing to repeat prev.
// About 1/9 of picks come from the rare pool.
func PickPhrase(prev string) string {
	pool := Phrases
	if txRand.Intn(9) == 0 {
		pool = RarePhrases
	}
	next := pool[txRand.Intn(len(pool))]
	for next == prev && len(pool) > 1 {
		next = pool[txRand.Intn(len(pool))]
	}
	return next
}

// Init starts the ticker: first phrase ~900 ms in.
func (t Transmission) Init() tea.Cmd {
	return tea.Tick(900*time.Millisecond, func(time.Time) tea.Msg {
		return TxShowMsg{Text: PickPhrase("")}
	})
}

// Update advances the ticker state machine. Delegate all four message
// types here from the mod's Update.
func (t Transmission) Update(msg tea.Msg) (Transmission, tea.Cmd) {
	switch msg := msg.(type) {
	case TxShowMsg:
		t.From = t.Clean
		if strings.TrimSpace(t.From) == "" {
			t.From = strings.Repeat(" ", len([]rune(msg.Text)))
		}
		t.Clean = msg.Text
		t.Visible = true
		t.Busy = true
		t.frame = 0
		t.mood = MoodStatic
		t.Text = glitchMix(t.From, t.Clean, 0.05)
		return t, txGlitchCmd()

	case TxGlitchTickMsg:
		if !t.Busy {
			return t, nil
		}
		t.frame++
		x := float64(t.frame) / float64(txGlitchSteps)
		if x >= 1 {
			t.Text = t.Clean
			t.mood = MoodClean
			t.Busy = false
			return t, tea.Batch(txHoldCmd(), txFlickerCmd())
		}
		mixed := glitchMix(t.From, t.Clean, x)
		switch {
		case x < 0.35:
			t.mood = MoodStatic
			t.Text = Glitch(mixed, 0.85)
		case x < 0.7:
			t.mood = MoodGlitch
			t.Text = scrambleCase(mixed, 0.45)
		default:
			t.mood = MoodResolve
			t.Text = Glitch(mixed, 0.18)
		}
		return t, txGlitchCmd()

	case TxHoldDoneMsg:
		if t.Busy {
			return t, nil
		}
		clean := t.Clean
		return t, func() tea.Msg {
			return TxShowMsg{Text: PickPhrase(clean)}
		}

	case TxFlickerMsg:
		if t.Busy || !t.Visible || t.Clean == "" {
			return t, nil
		}
		// A brief nervous twitch while the phrase sits. Resolves on the
		// next flicker tick unless a real transition has started.
		if txRand.Intn(3) == 0 {
			t.mood = MoodGlitch
			t.Text = scrambleCase(Glitch(t.Clean, 0.22), 0.3)
		} else {
			t.mood = MoodClean
			t.Text = t.Clean
		}
		return t, txFlickerCmd()
	}
	return t, nil
}

// View renders the reserved transmission row at the given width. The row
// is always allocated — even when blank — so frame height never jumps.
func (t Transmission) View(width int) string {
	text := " "
	if t.Visible && t.Text != "" {
		text = " " + t.Text
	}
	style := TxCleanStyle
	switch t.mood {
	case MoodGlitch:
		style = TxGlitchStyle
	case MoodStatic:
		style = TxStaticStyle
	case MoodResolve:
		style = TxResolveStyle
	}
	return style.Width(width).Render(text)
}

func txGlitchCmd() tea.Cmd {
	return tea.Tick(55*time.Millisecond, func(time.Time) tea.Msg {
		return TxGlitchTickMsg{}
	})
}

func txHoldCmd() tea.Cmd {
	d := time.Duration(txHoldMin+txRand.Intn(txHoldExtra)) * time.Second
	return tea.Tick(d, func(time.Time) tea.Msg { return TxHoldDoneMsg{} })
}

func txFlickerCmd() tea.Cmd {
	d := time.Duration(1400+txRand.Intn(2600)) * time.Millisecond
	return tea.Tick(d, func(time.Time) tea.Msg { return TxFlickerMsg{} })
}

// Glitch replaces non-space runes with block noise at the given intensity.
// It is the shared static-burst primitive: transitions, screen changes,
// and the transmission layer all dissolve through it.
func Glitch(s string, intensity float64) string {
	if intensity <= 0 {
		return s
	}
	noise := []rune("░▒▓█#%&@?▌▐▄▀╱╲╳·")
	runes := []rune(s)
	out := make([]rune, len(runes))
	for i, r := range runes {
		if r == ' ' {
			out[i] = r
			continue
		}
		if txRand.Float64() < intensity {
			out[i] = noise[txRand.Intn(len(noise))]
		} else {
			out[i] = r
		}
	}
	return string(out)
}

func scrambleCase(s string, intensity float64) string {
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) {
			continue
		}
		if txRand.Float64() < intensity {
			if unicode.IsUpper(r) {
				runes[i] = unicode.ToLower(r)
			} else {
				runes[i] = unicode.ToUpper(r)
			}
		}
	}
	return string(runes)
}

func padRunes(s string, n int) []rune {
	r := []rune(s)
	if len(r) >= n {
		return r
	}
	out := make([]rune, n)
	copy(out, r)
	for i := len(r); i < n; i++ {
		out[i] = ' '
	}
	return out
}

// glitchMix crossfades two strings through a noise peak so the row never
// blanks between transmissions.
func glitchMix(from, to string, t float64) string {
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	a, b := []rune(from), []rune(to)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	fa, fb := padRunes(from, n), padRunes(to, n)
	out := make([]rune, n)
	noise := []rune("░▒▓█#%&@?▌▐")
	peak := 1 - 2*math.Abs(t-0.5)
	if peak < 0 {
		peak = 0
	}
	for i := 0; i < n; i++ {
		var src rune
		if t < 0.5 {
			src = fa[i]
		} else {
			src = fb[i]
		}
		switch {
		case src == ' ':
			if peak > 0.7 && txRand.Intn(8) == 0 {
				out[i] = noise[txRand.Intn(len(noise))]
			} else {
				out[i] = ' '
			}
		case txRand.Float64() < peak*0.9:
			out[i] = noise[txRand.Intn(len(noise))]
		default:
			out[i] = src
		}
	}
	_, _ = a, b
	return string(out)
}
