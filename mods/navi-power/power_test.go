package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const samplePPDList = `* performance:
    Driver:     intel_pstate
    Degraded:   no

  balanced:
    Driver:     intel_pstate

  power-saver:
    Driver:     intel_pstate
`

const samplePPDListActiveLast = `  performance:
    Driver:     platform_profile

  balanced:
    Driver:     platform_profile

* power-saver:
    Driver:     platform_profile
`

func TestParsePPDList(t *testing.T) {
	profiles, active, driver := parsePPDList(samplePPDList)
	if !reflect.DeepEqual(profiles, []string{"performance", "balanced", "power-saver"}) {
		t.Fatalf("profiles = %v", profiles)
	}
	if active != "performance" {
		t.Fatalf("active = %q", active)
	}
	if driver != "intel_pstate" {
		t.Fatalf("driver = %q", driver)
	}
}

func TestParsePPDListActiveLast(t *testing.T) {
	profiles, active, driver := parsePPDList(samplePPDListActiveLast)
	if len(profiles) != 3 || active != "power-saver" {
		t.Fatalf("profiles = %v active = %q", profiles, active)
	}
	if driver != "platform_profile" {
		t.Fatalf("driver = %q (must come from the active block, not an earlier one)", driver)
	}
}

func TestFormatUpowerDuration(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.7 hours", "2h 42m"},
		{"2.0 hours", "2h"},
		{"45.2 minutes", "45m"},
		{"1.0 minutes", "1m"},
		{"unknown", ""},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := formatUpowerDuration(c.in); got != c.want {
			t.Errorf("formatUpowerDuration(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

const sampleUpower = `  native-path:          BAT0
  battery
    present:             yes
    state:               discharging
    energy-rate:         12.5 W
    time to empty:       2.7 hours
    time to full:        unknown
    percentage:          78%
`

func TestParseUpowerTimes(t *testing.T) {
	empty, full := parseUpowerTimes(sampleUpower)
	if empty != "2h 42m" {
		t.Fatalf("toEmpty = %q", empty)
	}
	if full != "" {
		t.Fatalf("toFull = %q, want empty for unknown", full)
	}
}

func TestValidateThresholds(t *testing.T) {
	if err := validateThresholds(75, 85); err != nil {
		t.Fatalf("75/85 should validate: %v", err)
	}
	for _, tc := range [][2]int{{85, 75}, {80, 80}, {-1, 50}, {50, 101}} {
		if err := validateThresholds(tc[0], tc[1]); err == nil {
			t.Errorf("validateThresholds(%d,%d) should fail", tc[0], tc[1])
		}
	}
}

func TestThresholdWriteScript(t *testing.T) {
	s := thresholdWriteScript("/s/start", "/s/stop", 75, 85)
	if !strings.Contains(s, "echo 75 > /s/start") || !strings.Contains(s, "echo 85 > /s/stop") {
		t.Fatalf("script = %q", s)
	}
	// One shell: a single elevation prompt covers both writes.
	if strings.Count(s, "sh") > 0 {
		t.Fatalf("script must not nest shells: %q", s)
	}
}

func TestClamp5(t *testing.T) {
	cases := []struct{ in, want int }{
		{73, 70}, {75, 75}, {77, 75}, {-3, 0}, {103, 100}, {0, 0}, {100, 100},
	}
	for _, c := range cases {
		if got := clamp5(c.in); got != c.want {
			t.Errorf("clamp5(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// fixtureSysfs builds a fake /sys/class/power_supply tree:
// BAT0 (ThinkPad-ish, with thresholds) and BAT1 (no thresholds).
func fixtureSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(dir, name, val string) {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("BAT0", "type", "Battery")
	write("BAT0", "capacity", "78")
	write("BAT0", "status", "Discharging")
	write("BAT0", "charge_full", "40100000")
	write("BAT0", "charge_full_design", "44000000")
	write("BAT0", "charge_control_start_threshold", "75")
	write("BAT0", "charge_control_end_threshold", "85")
	write("BAT1", "type", "Battery")
	write("BAT1", "capacity", "100")
	write("BAT1", "status", "Full")
	write("AC", "type", "Mains")
	return root
}

func TestReadBatteriesFixture(t *testing.T) {
	t.Setenv("NAVI_POWER_SYSFS", fixtureSysfs(t))
	bats := readBatteries()
	if len(bats) != 2 {
		t.Fatalf("got %d batteries, want 2 (AC must be skipped)", len(bats))
	}
	b0 := bats[0]
	if b0.name != "BAT0" || b0.capacity != 78 || b0.status != "Discharging" {
		t.Fatalf("BAT0 = %+v", b0)
	}
	if b0.health != 91 { // 40100000*100/44000000
		t.Fatalf("BAT0 health = %d, want 91", b0.health)
	}
	if bats[1].health != 0 {
		t.Fatalf("BAT1 health = %d, want 0 (unknown without design files)", bats[1].health)
	}
}

func TestThresholdPathsFixture(t *testing.T) {
	root := fixtureSysfs(t)
	t.Setenv("NAVI_POWER_SYSFS", root)
	s, e, ok := thresholdPaths()
	if !ok {
		t.Fatal("thresholds should be detected on fixture BAT0")
	}
	if !strings.HasPrefix(s, root) || !strings.HasSuffix(e, "charge_control_end_threshold") {
		t.Fatalf("paths = %q %q", s, e)
	}
	start, stop, err := readThresholds()
	if err != nil || start != 75 || stop != 85 {
		t.Fatalf("readThresholds = %d %d %v", start, stop, err)
	}
}

// fixtureBin drops a fake powerprofilesctl + upower on PATH and points
// NAVI_POWER_SYSFS at a fixture tree. The fake ppd is stateful: `set`
// records the profile, `get` reports it.
func fixtureBin(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "ppd-state")
	ppd := `#!/bin/sh
case "$1" in
  list) cat <<'EOF'
* performance:
    Driver:     intel_pstate
    Degraded:   no

  balanced:
    Driver:     intel_pstate

  power-saver:
    Driver:     intel_pstate
EOF
    ;;
  get)
    if [ -f "` + state + `" ]; then cat "` + state + `"; else echo balanced; fi ;;
  set) printf '%s' "$2" > "` + state + `" ;;
esac
`
	upower := `#!/bin/sh
cat <<'EOF'
  native-path:          BAT0
  battery
    state:               discharging
    time to empty:       2.7 hours
    time to full:        unknown
    percentage:          78%
EOF
`
	for name, body := range map[string]string{"powerprofilesctl": ppd, "upower": upower} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NAVI_POWER_SYSFS", fixtureSysfs(t))
}

func TestSnapshotEndToEnd(t *testing.T) {
	fixtureBin(t)
	st := ppdList()
	if !st.ok || len(st.profiles) != 3 || st.active != "performance" || st.driver != "intel_pstate" {
		t.Fatalf("ppdList = %+v", st)
	}
	actual, err := ppdSet("power-saver")
	if err != nil || actual != "power-saver" {
		t.Fatalf("ppdSet = %q %v (UI must follow the daemon's report)", actual, err)
	}
	bats := readBatteries()
	if len(bats) != 2 || bats[0].toEmpty != "2h 42m" || bats[0].toFull != "" {
		t.Fatalf("batteries = %+v", bats)
	}
	if _, _, ok := thresholdPaths(); !ok {
		t.Fatal("thresholds should be detected")
	}
}
