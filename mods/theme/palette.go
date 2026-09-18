package theme

import "github.com/charmbracelet/lipgloss"

// nightshadeNeon — the canonical navi palette.
//
// Every mod renders from these tokens. Never hardcode a hex color inside
// a mod; if a color you need isn't here, propose it as a token addition
// so the whole widget family shifts together.
const (
	Pink   = lipgloss.Color("#ff10f0") // neon pink — primary accent, titles, selection
	Green  = lipgloss.Color("#39ff14") // phosphor green — the ∅ mark, ok/active states
	Cyan   = lipgloss.Color("#00ffff") // cyan — headers, resolve states
	White  = lipgloss.Color("#ffffff") // primary text
	Black  = lipgloss.Color("#000000") // deepest background
	Violet = lipgloss.Color("#bf5fff") // glitch states
	Ghost  = lipgloss.Color("#7a4a7a") // static states, muted purple
	Red    = lipgloss.Color("#ff3131") // errors, destructive actions, off states

	Dim   = lipgloss.Color("#5c4a5c") // hairlines, dimmed chrome (muted mauve, in the Ghost family — never neutral grey)
	Gray  = lipgloss.Color("#666666") // secondary text
	Faint = lipgloss.Color("#3a2f3d") // barely-there chrome (inactive footer keys)
	Dark  = lipgloss.Color("#0f0f0f") // panel background
)
