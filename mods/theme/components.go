package theme

import (
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
