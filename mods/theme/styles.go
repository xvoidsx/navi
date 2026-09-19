package theme

import "github.com/charmbracelet/lipgloss"

// Base styles shared by every mod. Compose these; don't redeclare them.
var (
	// Title is the mod name in the frame header ("navi networking").
	Title = lipgloss.NewStyle().Foreground(Pink).Background(Black).Bold(true)
	// Logo is the ∅ mark that opens every frame header.
	Logo = lipgloss.NewStyle().Foreground(Green).Background(Black).Bold(true)
	// Header is for section titles inside the body ("NETWORKS").
	Header = lipgloss.NewStyle().Foreground(Cyan).Bold(true)
	// Selected marks the focused row / active choice.
	Selected = lipgloss.NewStyle().Foreground(Pink).Bold(true)
	// Normal is primary body text.
	Normal = lipgloss.NewStyle().Foreground(White)

	// Dimmed is chrome text: hairlines, hints, inactive labels.
	Dimmed = lipgloss.NewStyle().Foreground(Dim)
	// Grayed is secondary text.
	Grayed = lipgloss.NewStyle().Foreground(Gray)
	// Fainted is barely-there chrome (footer keys with no active context).
	Fainted = lipgloss.NewStyle().Foreground(Faint)

	// Error is for failure lines and destructive confirmations.
	Error = lipgloss.NewStyle().Foreground(Red).Bold(true)

	// Border is the standard mod window frame: a solid obsidian slab edged
	// in neon pink — the nightshadeNeon glow. Pure black keeps body text
	// readable in transparent terminals; the pink edge is the frame's
	// signature, shared by every mod through theme.Frame.
	Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Pink).
		Background(Black).
		Padding(0, 1)

	// Input is for editable / focused input text.
	Input = lipgloss.NewStyle().Foreground(Pink).Bold(true)

	// DotOn / DotOff are the status dot: green when the mod's domain is
	// live (connected, unmuted…), red when it isn't.
	DotOn  = lipgloss.NewStyle().Foreground(Green)
	DotOff = lipgloss.NewStyle().Foreground(Red)
)
