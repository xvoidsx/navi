package theme

import (
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Divider renders a full-width hairline for the given frame width.
func Divider(width int) string {
	if width < 2 {
		return ""
	}
	return Dimmed.Render(strings.Repeat("─", width-2))
}

// Footer renders the persistent hotkey bar. Every item is shown all the
// time — items that need a live context the mod doesn't currently have
// are rendered via the faint style (active=false) instead of disappearing,
// so the keymap never shifts under the user's fingers.
func Footer(active bool, items ...[2]string) string {
	style := Dimmed
	if !active {
		style = Fainted
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, style.Render(it[0]+" "+it[1]))
	}
	return strings.Join(parts, "   ")
}

// Frame renders the standard mod window: the ∅ logo + mod name title row
// with a status dot, the ambient transmission row, and the body — all
// inside the standard border.
//
//   - width: total frame width in cells (62 is the family standard).
//   - name: mod name rendered after the ∅ mark ("navi networking").
//   - live: whether the mod's domain is currently live; drives the dot.
//   - txRow: a pre-rendered transmission row, e.g. Transmission.View(width).
//     The row is always allocated — even when blank — so frame height
//     never jumps between transmissions.
//   - body: the mod's main content, already width-constrained.
func Frame(width int, name string, live bool, txRow, body string) string {
	dot := DotOff.Render("●")
	if live {
		dot = DotOn.Render("●")
	}
	title := Logo.Render(" ∅ ") + Title.Render(" "+name)
	headerLine := lipgloss.NewStyle().Width(width - 4).Render(title)
	header := lipgloss.JoinHorizontal(lipgloss.Top, headerLine, dot+"  ")

	framed := lipgloss.NewStyle().Width(width).Render(body)
	return Border.Render(header + "\n" + txRow + "\n" + framed)
}

// Meter renders a horizontal bar meter: frac is clamped to [0,1] and
// fills width cells with block characters.
func Meter(width int, frac float64, fill, empty lipgloss.TerminalColor) string {
	if width < 1 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	fillStyle := lipgloss.NewStyle().Foreground(fill)
	emptyStyle := lipgloss.NewStyle().Foreground(empty)
	return fillStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", width-filled))
}

// Sparkline renders recent samples as a block-character sparkline —
// ▁▂▃▄▅▆▇█ — normalized against the window max. Built for live
// throughput graphs (speed tests) and audio level meters: push a sample
// per tick, render the tail.
func Sparkline(samples []float64, width int, color lipgloss.TerminalColor) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	if width < 1 || len(samples) == 0 {
		return ""
	}
	if len(samples) > width {
		samples = samples[len(samples)-width:]
	}
	max := 1e-9
	for _, s := range samples {
		if s > max {
			max = s
		}
	}
	var b strings.Builder
	for _, s := range samples {
		idx := int(s/max*7 + 0.5)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		b.WriteRune(blocks[idx])
	}
	return lipgloss.NewStyle().Foreground(color).Render(b.String())
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// GlitchANSI applies Glitch to the visible text of a string that already
// contains ANSI escape sequences, leaving the sequences themselves intact.
// This is what makes full-frame transitions (screen changes) possible:
// glitch the composed view without corrupting its colors.
func GlitchANSI(s string, intensity float64) string {
	if intensity <= 0 {
		return s
	}
	parts := ansiSeq.Split(s, -1)
	matches := ansiSeq.FindAllString(s, -1)
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(Glitch(p, intensity))
		if i < len(matches) {
			b.WriteString(matches[i])
		}
	}
	return b.String()
}

// SpinnerFrames are the nightshadeNeon spinner cells, in order.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner renders one frame of the neon spinner.
func Spinner(frame int) string {
	return lipgloss.NewStyle().Foreground(Pink).
		Render(SpinnerFrames[frame%len(SpinnerFrames)])
}

// SpinnerTickMsg is a spinner animation tick. Tag lets a mod ignore ticks
// from a spinner it already replaced (e.g. after a rescan).
type SpinnerTickMsg struct{ Tag int }

// SpinnerTick returns a command that delivers a SpinnerTickMsg after d.
func SpinnerTick(tag int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return SpinnerTickMsg{Tag: tag} })
}
