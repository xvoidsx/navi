// display.go — the compositor backend for navi-display.
//
// Session detection mirrors wired/waybar/mod-open.sh: probe
// `swaymsg -t get_version` with a real round-trip, then fall back to
// `i3-msg -t get_version`. The backend is picked once at startup; the UI
// speaks only through it.
//
//   - Wayland (sway/wiredWM): read `swaymsg -t get_outputs`, apply with
//     `swaymsg output <name> ...`.
//   - X11 (i3): read `xrandr --query`, apply with `xrandr --output ...`.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ── backend ──────────────────────────────────────────────────────────

type Backend int

const (
	BackendSway Backend = iota
	BackendX11
)

func (b Backend) String() string {
	if b == BackendX11 {
		return "x11"
	}
	return "sway"
}

// detectBackend probes for a live compositor, sway first.
func detectBackend() (Backend, error) {
	if err := exec.Command("swaymsg", "-t", "get_version").Run(); err == nil {
		return BackendSway, nil
	}
	if err := exec.Command("i3-msg", "-t", "get_version").Run(); err == nil {
		return BackendX11, nil
	}
	return BackendSway, errors.New("no compositor found — need swaymsg or i3-msg on PATH")
}

// ── model ────────────────────────────────────────────────────────────

// Mode is one resolution+refresh the backend reports for an output.
// RefreshMHz is millihertz, the unit sway reports (60000 = 60 Hz).
type Mode struct {
	Width      int
	Height     int
	RefreshMHz int
	Current    bool
	Preferred  bool
}

// Label renders "1920x1080 @ 60Hz".
func (m Mode) Label() string {
	return fmt.Sprintf("%dx%d @ %s", m.Width, m.Height, hzString(m.RefreshMHz))
}

// hzString formats millihertz as sway/xrandr accept: "60Hz" or "59.94Hz".
func hzString(mhz int) string {
	if mhz%1000 == 0 {
		return fmt.Sprintf("%dHz", mhz/1000)
	}
	s := strconv.FormatFloat(float64(mhz)/1000, 'f', 2, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".") + "Hz"
}

// Output is one connected display.
type Output struct {
	Name         string
	Active       bool // enabled (sway: active flag; x11: has a current mode)
	Make         string
	Model        string
	Modes        []Mode
	CurW, CurH   int
	CurMHz       int
	X, Y         int     // logical position (sway: rect; x11: geometry offset)
	Scale        float64 // sway only; 1.0 on x11
	Rotation     string  // "normal" | "90" | "180" | "270"
	Primary      bool    // x11 primary flag
	AdaptiveSync string  // sway only
}

// LogicalW is the output's width in layout space (sway divides by scale).
func (o Output) LogicalW() int {
	if o.Scale > 0 {
		return int(float64(o.CurW) / o.Scale)
	}
	return o.CurW
}

// ModeLabel is the short "1920x1080@60" line for the list card.
func (o Output) ModeLabel() string {
	if o.CurW == 0 {
		return "no mode"
	}
	return fmt.Sprintf("%dx%d@%s", o.CurW, o.CurH, strings.TrimSuffix(hzString(o.CurMHz), "Hz"))
}

// ── sway backend ─────────────────────────────────────────────────────

type swayMode struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	Refresh int `json:"refresh"`
}

type swayOutput struct {
	Name               string     `json:"name"`
	Make               string     `json:"make"`
	Model              string     `json:"model"`
	Active             bool       `json:"active"`
	Scale              float64    `json:"scale"`
	Transform          string     `json:"transform"`
	CurrentMode        swayMode   `json:"current_mode"`
	Modes              []swayMode `json:"modes"`
	AdaptiveSyncStatus string     `json:"adaptive_sync_status"`
	Rect               struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"rect"`
}

func listOutputsSway() ([]Output, error) {
	out, err := exec.Command("swaymsg", "-t", "get_outputs").Output()
	if err != nil {
		return nil, fmt.Errorf("swaymsg -t get_outputs: %w", err)
	}
	var raws []swayOutput
	if err := json.Unmarshal(out, &raws); err != nil {
		return nil, fmt.Errorf("parsing sway outputs: %w", err)
	}
	outs := make([]Output, 0, len(raws))
	for _, r := range raws {
		o := Output{
			Name:         r.Name,
			Active:       r.Active,
			Make:         r.Make,
			Model:        r.Model,
			CurW:         r.CurrentMode.Width,
			CurH:         r.CurrentMode.Height,
			CurMHz:       r.CurrentMode.Refresh,
			X:            r.Rect.X,
			Y:            r.Rect.Y,
			Scale:        r.Scale,
			Rotation:     r.Transform,
			AdaptiveSync: r.AdaptiveSyncStatus,
		}
		if o.Scale == 0 {
			o.Scale = 1
		}
		for _, m := range r.Modes {
			o.Modes = append(o.Modes, Mode{
				Width:      m.Width,
				Height:     m.Height,
				RefreshMHz: m.Refresh,
				Current:    m.Width == o.CurW && m.Height == o.CurH && m.Refresh == o.CurMHz,
			})
		}
		outs = append(outs, o)
	}
	return outs, nil
}

func swayCmd(name string, args ...string) error {
	full := append([]string{"output", name}, args...)
	out, err := exec.Command("swaymsg", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("swaymsg %s: %w", strings.Join(full, " "), err)
	}
	if strings.Contains(string(out), `"success": false`) {
		return fmt.Errorf("swaymsg %s: %s", strings.Join(full, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

// ── xrandr backend ───────────────────────────────────────────────────

var (
	xrandrOutRe  = regexp.MustCompile(`^(\S+)\s+(connected|disconnected)( primary)?(?:\s+(\d+)x(\d+)\+(\d+)\+(\d+))?\s*\(([^)]*)\)`)
	xrandrModeRe = regexp.MustCompile(`^\s+(\d+)x(\d+)\s+(.+)$`)
)

func listOutputsXrandr() ([]Output, error) {
	out, err := exec.Command("xrandr", "--query").Output()
	if err != nil {
		return nil, fmt.Errorf("xrandr --query: %w", err)
	}
	return parseXrandr(string(out)), nil
}

// parseXrandr is pure so tests can feed it captured output.
func parseXrandr(text string) []Output {
	var outs []Output
	var cur *Output
	flush := func() {
		if cur != nil {
			outs = append(outs, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if m := xrandrOutRe.FindStringSubmatch(line); m != nil {
			flush()
			if m[2] == "disconnected" {
				continue // spec: only connected outputs are shown
			}
			o := Output{Name: m[1], Scale: 1, Rotation: "normal"}
			if m[3] != "" {
				o.Primary = true
			}
			if m[4] != "" {
				o.CurW, _ = strconv.Atoi(m[4])
				o.CurH, _ = strconv.Atoi(m[5])
				o.X, _ = strconv.Atoi(m[6])
				o.Y, _ = strconv.Atoi(m[7])
			}
			// first token inside the parens is the current rotation
			if rot := strings.Fields(m[8]); len(rot) > 0 {
				o.Rotation = xrandrRotToSway(rot[0])
			}
			cur = &o
			continue
		}
		if cur != nil {
			if m := xrandrModeRe.FindStringSubmatch(line); m != nil {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				for _, tok := range strings.Fields(m[3]) {
					rate := tok
					current := strings.Contains(rate, "*")
					preferred := strings.Contains(rate, "+")
					rate = strings.Trim(rate, "*+")
					hz, err := strconv.ParseFloat(rate, 64)
					if err != nil {
						continue
					}
					mhz := int(hz*1000 + 0.5)
					md := Mode{Width: w, Height: h, RefreshMHz: mhz, Current: current, Preferred: preferred}
					cur.Modes = append(cur.Modes, md)
					if current {
						cur.Active = true
						cur.CurMHz = mhz
					}
				}
			}
		}
	}
	flush()
	return outs
}

// xrandrRotToSway normalizes xrandr rotation words to sway-style labels.
func xrandrRotToSway(r string) string {
	switch r {
	case "left":
		return "270"
	case "right":
		return "90"
	case "inverted":
		return "180"
	default:
		return "normal"
	}
}

// swayRotToXrandr maps back for xrandr --rotate.
func swayRotToXrandr(r string) string {
	switch r {
	case "90":
		return "right"
	case "180":
		return "inverted"
	case "270":
		return "left"
	default:
		return "normal"
	}
}

func xrandrApply(o Output) error {
	args := []string{"--output", o.Name}
	if !o.Active {
		args = append(args, "--off")
	} else {
		args = append(args,
			"--mode", fmt.Sprintf("%dx%d", o.CurW, o.CurH),
			"--rate", strings.TrimSuffix(hzString(o.CurMHz), "Hz"),
			"--pos", fmt.Sprintf("%dx%d", o.X, o.Y),
			"--rotate", swayRotToXrandr(o.Rotation),
		)
		if o.Primary {
			args = append(args, "--primary")
		}
	}
	out, err := exec.Command("xrandr", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xrandr %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ── apply helpers (backend-dispatched) ───────────────────────────────

// listOutputs reads the current output list from whichever backend is live.
func listOutputs(b Backend) ([]Output, error) {
	if b == BackendX11 {
		return listOutputsXrandr()
	}
	return listOutputsSway()
}

// applyMode sets an output's resolution+refresh.
func applyMode(b Backend, o Output, m Mode) error {
	if b == BackendX11 {
		o.CurW, o.CurH, o.CurMHz = m.Width, m.Height, m.RefreshMHz
		o.Active = true
		return xrandrApply(o)
	}
	return swayCmd(o.Name, "mode", fmt.Sprintf("%dx%d@%s", m.Width, m.Height, hzString(m.RefreshMHz)))
}

// applyRotation cycles an output's transform.
func applyRotation(b Backend, o Output, rot string) error {
	if b == BackendX11 {
		o.Rotation = rot
		return xrandrApply(o)
	}
	return swayCmd(o.Name, "transform", rot)
}

// applyScale sets the sway scale factor (x11: no-op, dimmed in the UI).
func applyScale(b Backend, o Output, scale float64) error {
	if b == BackendX11 {
		return errors.New("fractional scale is a Wayland thing — xrandr can't do it")
	}
	return swayCmd(o.Name, "scale", strconv.FormatFloat(scale, 'f', 1, 64))
}

// applyPower enables or disables an output.
func applyPower(b Backend, o Output, on bool) error {
	if b == BackendX11 {
		o.Active = on
		return xrandrApply(o)
	}
	if on {
		return swayCmd(o.Name, "enable")
	}
	return swayCmd(o.Name, "disable")
}

// applyPosition moves an output in layout space.
func applyPosition(b Backend, o Output, x, y int) error {
	if b == BackendX11 {
		o.X, o.Y = x, y
		return xrandrApply(o)
	}
	return swayCmd(o.Name, "position", strconv.Itoa(x), strconv.Itoa(y))
}

// ── layout ───────────────────────────────────────────────────────────

type layoutDir int

const (
	dirLeft layoutDir = iota
	dirRight
	dirAbove
	dirBelow
	dirMirror
	dirAuto
)

// arrange computes the new position for outputs[idx] relative to its
// predecessor in list order. Mirror shares the predecessor's position.
// Auto lays every output side-by-side from x=0. Pure: easy to test.
func arrange(outs []Output, idx int, dir layoutDir) []Output {
	next := make([]Output, len(outs))
	copy(next, outs)
	if dir == dirAuto {
		x := 0
		for i := range next {
			if next[i].Active {
				next[i].X, next[i].Y = x, 0
				x += next[i].LogicalW()
			}
		}
		return next
	}
	if idx <= 0 || idx >= len(next) {
		return next
	}
	prev := next[idx-1]
	o := &next[idx]
	switch dir {
	case dirLeft:
		o.X, o.Y = prev.X-o.LogicalW(), prev.Y
	case dirRight:
		o.X, o.Y = prev.X+prev.LogicalW(), prev.Y
	case dirAbove:
		o.X, o.Y = prev.X, prev.Y-o.CurH
	case dirBelow:
		o.X, o.Y = prev.X, prev.Y+prev.CurH
	case dirMirror:
		o.X, o.Y = prev.X, prev.Y
	}
	return next
}

// rotations cycles through the four plain transforms.
var rotations = []string{"normal", "90", "180", "270"}

func nextRotation(cur string) string {
	for i, r := range rotations {
		if r == cur {
			return rotations[(i+1)%len(rotations)]
		}
	}
	return "normal"
}
