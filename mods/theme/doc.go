// Package theme is the shared nightshadeNeon design language for navi mods.
//
// Every mod renders from these tokens and components — never hardcode a
// hex color or rebuild a header/footer inside a mod. Cohesion comes from
// shared code, not shared taste: when the palette shifts, every mod
// shifts with it.
//
// A mod adopts the theme by adding to its go.mod:
//
//	require github.com/rav3ndust/navi-theme v0.0.0
//	replace github.com/rav3ndust/navi-theme => ../theme
//
// and rendering its window through the Frame, Footer, Divider, Meter,
// Spinner, Transmission, and Glow helpers here.
package theme
