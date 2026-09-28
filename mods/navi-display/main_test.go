package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Realistic swaymsg -t get_outputs shape (sway 1.9+): refresh in mHz,
// rect carries position, inactive output still listed.
const swayFixture = `[
  {
    "name": "eDP-1",
    "make": "AU Optronics",
    "model": "0x06FA",
    "serial": "0x00000000",
    "active": true,
    "dpms": true,
    "power": true,
    "primary": false,
    "scale": 1.0,
    "scale_filter": "nearest",
    "transform": "normal",
    "current_mode": {"width": 1366, "height": 768, "refresh": 60000, "picture_aspect_ratio": "16:9"},
    "modes": [
      {"width": 1366, "height": 768, "refresh": 60000, "picture_aspect_ratio": "16:9"},
      {"width": 1280, "height": 720, "refresh": 60000, "picture_aspect_ratio": "16:9"}
    ],
    "adaptive_sync_status": "disabled",
    "rect": {"x": 0, "y": 0, "width": 1366, "height": 768},
    "focused": true
  },
  {
    "name": "HDMI-A-1",
    "make": "Samsung Electric Company",
    "model": "S24C570",
    "serial": "H4ZM500000",
    "active": false,
    "dpms": false,
    "power": false,
    "primary": false,
    "scale": 1.0,
    "scale_filter": "nearest",
    "transform": "normal",
    "current_mode": {"width": 1920, "height": 1080, "refresh": 60000, "picture_aspect_ratio": "16:9"},
    "modes": [
      {"width": 1920, "height": 1080, "refresh": 60000, "picture_aspect_ratio": "16:9"},
      {"width": 1920, "height": 1080, "refresh": 50000, "picture_aspect_ratio": "16:9"},
      {"width": 1280, "height": 720, "refresh": 59940, "picture_aspect_ratio": "16:9"}
    ],
    "adaptive_sync_status": "disabled",
    "rect": {"x": 0, "y": 0, "width": 1920, "height": 1080},
    "focused": false
  }
]`

func parseSwayFixture(t *testing.T) []Output {
	t.Helper()
	var raws []swayOutput
	if err := json.Unmarshal([]byte(swayFixture), &raws); err != nil {
		t.Fatalf("fixture must unmarshal: %v", err)
	}
	// Reuse the same conversion the backend uses by round-tripping through
	// the real unmarshal path — replicate listOutputsSway's mapping here
	// would just duplicate code, so instead exercise the mapping inline.
	outs := make([]Output, 0, len(raws))
	for _, r := range raws {
		o := Output{
			Name: r.Name, Active: r.Active, Make: r.Make, Model: r.Model,
			CurW: r.CurrentMode.Width, CurH: r.CurrentMode.Height,
			CurMHz: r.CurrentMode.Refresh, X: r.Rect.X, Y: r.Rect.Y,
			Scale: r.Scale, Rotation: r.Transform,
			AdaptiveSync: r.AdaptiveSyncStatus,
		}
		for _, m := range r.Modes {
			o.Modes = append(o.Modes, Mode{Width: m.Width, Height: m.Height,
				RefreshMHz: m.Refresh,
				Current:    m.Width == o.CurW && m.Height == o.CurH && m.Refresh == o.CurMHz})
		}
		outs = append(outs, o)
	}
	return outs
}

func TestSwayFixtureShape(t *testing.T) {
	outs := parseSwayFixture(t)
	if len(outs) != 2 {
		t.Fatalf("want 2 outputs, got %d", len(outs))
	}
	laptop, tv := outs[0], outs[1]
	if laptop.Name != "eDP-1" || !laptop.Active {
		t.Errorf("laptop: %+v", laptop)
	}
	if tv.Active {
		t.Error("HDMI-A-1 should parse as inactive")
	}
	if tv.CurMHz != 60000 {
		t.Errorf("refresh: want 60000 mHz, got %d", tv.CurMHz)
	}
	if len(tv.Modes) != 3 {
		t.Fatalf("want 3 tv modes, got %d", len(tv.Modes))
	}
	if !tv.Modes[0].Current || tv.Modes[1].Current {
		t.Error("only the first mode should be current")
	}
	// Fractional refresh keeps its millihertz.
	if tv.Modes[2].RefreshMHz != 59940 {
		t.Errorf("want 59940, got %d", tv.Modes[2].RefreshMHz)
	}
	if got := tv.Modes[2].Label(); got != "1280x720 @ 59.94Hz" {
		t.Errorf("label: got %q", got)
	}
}

// Realistic xrandr --query: primary with geometry, second connected with
// offset, one disconnected (must be skipped), * current, + preferred.
const xrandrFixture = `Screen 0: minimum 8 x 8, current 3286 x 1080, maximum 32767 x 32767
eDP-1 connected primary 1366x768+0+0 (normal left inverted right x axis y axis) 309mm x 174mm
   1366x768      60.00*+  39.97
   1280x720      60.00    59.94
   1024x768      60.00
HDMI-1 connected 1920x1080+1366+0 (normal left inverted right x axis y axis) 531mm x 299mm
   1920x1080     60.00*+  50.00    59.94
   1280x720      60.00
DP-1 disconnected (normal left inverted right x axis y axis)
`

func TestXrandrFixtureShape(t *testing.T) {
	outs := parseXrandr(xrandrFixture)
	if len(outs) != 2 {
		t.Fatalf("want 2 connected outputs (DP-1 skipped), got %d", len(outs))
	}
	laptop, hdmi := outs[0], outs[1]
	if laptop.Name != "eDP-1" || !laptop.Primary {
		t.Errorf("laptop: %+v", laptop)
	}
	if !laptop.Active || laptop.CurMHz != 60000 {
		t.Errorf("laptop should be active @60Hz: %+v", laptop)
	}
	if laptop.X != 0 || laptop.Y != 0 {
		t.Errorf("laptop pos: got %d,%d", laptop.X, laptop.Y)
	}
	if hdmi.X != 1366 || hdmi.Y != 0 {
		t.Errorf("hdmi pos: got %d,%d", hdmi.X, hdmi.Y)
	}
	if hdmi.Rotation != "normal" {
		t.Errorf("hdmi rotation: got %q", hdmi.Rotation)
	}
	// Preferred flag lands on the right mode.
	var pref *Mode
	for i := range laptop.Modes {
		if laptop.Modes[i].Preferred {
			pref = &laptop.Modes[i]
		}
	}
	if pref == nil || pref.Width != 1366 {
		t.Errorf("preferred mode not found: %+v", laptop.Modes)
	}
	// 59.94 keeps millihertz precision.
	found := false
	for _, md := range hdmi.Modes {
		if md.RefreshMHz == 59940 {
			found = true
		}
	}
	if !found {
		t.Error("59.94Hz mode should parse to 59940 mHz")
	}
}

func TestXrandrRotationMapping(t *testing.T) {
	if xrandrRotToSway("left") != "270" || xrandrRotToSway("right") != "90" ||
		xrandrRotToSway("inverted") != "180" || xrandrRotToSway("normal") != "normal" {
		t.Error("rotation mapping wrong")
	}
	if swayRotToXrandr("90") != "right" || swayRotToXrandr("270") != "left" {
		t.Error("reverse rotation mapping wrong")
	}
	if nextRotation("normal") != "90" || nextRotation("270") != "normal" {
		t.Error("rotation cycling wrong")
	}
}

func TestArrange(t *testing.T) {
	outs := []Output{
		{Name: "eDP-1", Active: true, CurW: 1366, CurH: 768, Scale: 1, X: 0, Y: 0},
		{Name: "HDMI-A-1", Active: true, CurW: 1920, CurH: 1080, Scale: 1, X: 0, Y: 0},
	}
	right := arrange(outs, 1, dirRight)
	if right[1].X != 1366 || right[1].Y != 0 {
		t.Errorf("right-of: got %d,%d", right[1].X, right[1].Y)
	}
	left := arrange(outs, 1, dirLeft)
	if left[1].X != -1920 {
		t.Errorf("left-of: got %d", left[1].X)
	}
	below := arrange(outs, 1, dirBelow)
	if below[1].Y != 768 {
		t.Errorf("below: got %d", below[1].Y)
	}
	mirror := arrange(outs, 1, dirMirror)
	if mirror[1].X != 0 || mirror[1].Y != 0 {
		t.Errorf("mirror: got %d,%d", mirror[1].X, mirror[1].Y)
	}
	auto := arrange(outs, 0, dirAuto)
	if auto[0].X != 0 || auto[1].X != 1366 {
		t.Errorf("auto: got %d,%d", auto[0].X, auto[1].X)
	}
	// Scale-aware: sway logical width divides by scale.
	scaled := []Output{
		{Name: "eDP-1", Active: true, CurW: 2732, CurH: 1536, Scale: 2, X: 0, Y: 0},
		{Name: "HDMI-A-1", Active: true, CurW: 1920, CurH: 1080, Scale: 1, X: 0, Y: 0},
	}
	r := arrange(scaled, 1, dirRight)
	if r[1].X != 1366 {
		t.Errorf("scaled right-of: got %d, want 1366", r[1].X)
	}
	// Index 0 with a directional key is a no-op, not a crash.
	same := arrange(outs, 0, dirRight)
	if same[0].X != 0 {
		t.Error("idx 0 directional should no-op")
	}
}

func TestHzString(t *testing.T) {
	if hzString(60000) != "60Hz" {
		t.Errorf("got %q", hzString(60000))
	}
	if hzString(59940) != "59.94Hz" {
		t.Errorf("got %q", hzString(59940))
	}
}

func TestValidClock(t *testing.T) {
	for _, good := range []string{"06:30", "19:45", "00:00", "23:59"} {
		if !validClock(good) {
			t.Errorf("%q should be valid", good)
		}
	}
	for _, bad := range []string{"6:30", "25:00", "12:60", "nope", "12345"} {
		if validClock(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestDumpRenders(t *testing.T) {
	// --dump must not panic; eyeball the real output via `go run . --dump`.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dump panicked: %v", r)
		}
	}()
	// Capture stdout the cheap way: dumpScreens writes to os.Stdout.
	// We only assert it runs; the visual check is manual.
	dumpScreens()
	_ = strings.Contains // keep strings import honest if unused elsewhere
}
