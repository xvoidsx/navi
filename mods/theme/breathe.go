package theme

import (
	"fmt"
	"math"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The breathing idle: a slow ~4 s pulse that waiting states breathe with.
// One motion vocabulary across the whole desktop — the bar breathes, the
// overlay breathes, idle mods breathe.

// Pulse returns a 0..1..0 breathing intensity for time t on a ~4 s sine
// period. 0 is the dim extreme, 1 the bright extreme.
func Pulse(t time.Time) float64 {
	const period = 4 * time.Second
	p := float64(t.UnixNano()%int64(period)) / float64(period)
	return 0.5 - 0.5*math.Cos(2*math.Pi*p)
}

// Glow renders text breathing between Faint and Pink — the standard idle
// glow for waiting states. Call it on every frame with time.Now().
func Glow(text string, t time.Time) string {
	return lipgloss.NewStyle().
		Foreground(lerpColor(string(Faint), string(Pink), Pulse(t))).
		Render(text)
}

func lerpColor(a, b string, t float64) lipgloss.Color {
	pa, pb := parseHex(a), parseHex(b)
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x) + t*float64(int(y)-int(x)))
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		mix(pa[0], pb[0]), mix(pa[1], pb[1]), mix(pa[2], pb[2])))
}

func parseHex(s string) [3]uint8 {
	var v [3]uint8
	s = s[1:] // strip '#'
	for i := 0; i < 3; i++ {
		var n uint8
		fmt.Sscanf(s[i*2:i*2+2], "%02x", &n)
		v[i] = n
	}
	return v
}
