// power.go — system backend for navi-power.
//
// power-profiles-daemon (powerprofilesctl), sysfs battery telemetry,
// upower time estimates, and ThinkPad charge thresholds. No Bubble Tea
// in this file: the model talks to these helpers, and tests talk to
// them too.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// sysfsRoot is /sys/class/power_supply, overridable via NAVI_POWER_SYSFS
// so tests can point at a fixture tree.
func sysfsRoot() string {
	if v := os.Getenv("NAVI_POWER_SYSFS"); v != "" {
		return v
	}
	return "/sys/class/power_supply"
}

func readSysfsLine(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readSysfsInt(path string) (int, error) {
	s, err := readSysfsLine(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// ── power-profiles-daemon ─────────────────────────────────────────────

type ppdState struct {
	ok       bool
	profiles []string
	active   string
	driver   string
}

func ppdList() ppdState {
	st := ppdState{}
	if _, err := exec.LookPath("powerprofilesctl"); err != nil {
		return st
	}
	st.ok = true
	out, err := exec.Command("powerprofilesctl", "list").Output()
	if err != nil {
		return st
	}
	st.profiles, st.active, st.driver = parsePPDList(string(out))
	return st
}

// parsePPDList reads the human output of `powerprofilesctl list`:
//
//   - performance:
//     Driver:     intel_pstate
//     Degraded:   no
//
//     balanced:
//     Driver:     intel_pstate
//
//     power-saver:
//     Driver:     intel_pstate
//
// The active profile carries the `* ` marker. Profile lines sit at
// indent 2; the Driver/Degraded detail lines sit deeper, so indent
// disambiguates them.
func parsePPDList(out string) (profiles []string, active, driver string) {
	var activeIdx = -1
	inActiveBlock := false
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		isActive := strings.HasPrefix(trimmed, "* ")
		name := trimmed
		if isActive {
			name = strings.TrimPrefix(trimmed, "* ")
		}
		if indent < 4 && strings.HasSuffix(name, ":") && !strings.Contains(name, " ") {
			profiles = append(profiles, strings.TrimSuffix(name, ":"))
			inActiveBlock = isActive
			if isActive {
				activeIdx = len(profiles) - 1
			}
			continue
		}
		// Detail line inside the active profile's block: capture Driver.
		if inActiveBlock && driver == "" && strings.HasPrefix(trimmed, "Driver:") {
			driver = strings.TrimSpace(strings.TrimPrefix(trimmed, "Driver:"))
		}
	}
	if activeIdx >= 0 {
		active = profiles[activeIdx]
	}
	return profiles, active, driver
}

func ppdGet() (string, error) {
	out, err := exec.Command("powerprofilesctl", "get").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ppdSet asks the daemon for a profile, then reports the daemon's
// ACTUAL active profile — the daemon can refuse (driver support), so
// the UI must follow the daemon, never the request.
func ppdSet(name string) (string, error) {
	if out, err := exec.Command("powerprofilesctl", "set", name).CombinedOutput(); err != nil {
		return "", fmt.Errorf("powerprofilesctl set: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return ppdGet()
}

// ── batteries ───────────────────────────────────────────────────────

type battery struct {
	name     string // BAT0
	capacity int    // percent
	status   string // Charging / Discharging / Full / Unknown
	health   int    // charge_full / charge_full_design, percent; 0 = unknown
	toEmpty  string // upower estimate, "" when unknown
	toFull   string
}

// readBatteries enumerates BAT* power supplies of type Battery.
// Multiple batteries (X-series ThinkPads) each get their own entry.
func readBatteries() []battery {
	root := sysfsRoot()
	matches, _ := filepath.Glob(filepath.Join(root, "BAT*"))
	sort.Strings(matches)
	var out []battery
	for _, dir := range matches {
		if typ, err := readSysfsLine(filepath.Join(dir, "type")); err == nil && typ != "" && typ != "Battery" {
			continue
		}
		b := battery{name: filepath.Base(dir)}
		if v, err := readSysfsInt(filepath.Join(dir, "capacity")); err == nil {
			b.capacity = v
		}
		if s, err := readSysfsLine(filepath.Join(dir, "status")); err == nil {
			b.status = s
		}
		full, errFull := readSysfsInt(filepath.Join(dir, "charge_full"))
		design, errDesign := readSysfsInt(filepath.Join(dir, "charge_full_design"))
		if errFull == nil && errDesign == nil && design > 0 {
			b.health = full * 100 / design
		}
		b.toEmpty, b.toFull = upowerTimes(b.name)
		out = append(out, b)
	}
	return out
}

// upowerTimes asks upower for the only sane time estimates on the
// system. Returns "" for either leg when upower is missing or the
// value is unknown. We never hand-roll the math.
func upowerTimes(batName string) (toEmpty, toFull string) {
	if _, err := exec.LookPath("upower"); err != nil {
		return "", ""
	}
	out, err := exec.Command("upower", "-i",
		"/org/freedesktop/UPower/devices/battery_"+batName).Output()
	if err != nil {
		return "", ""
	}
	return parseUpowerTimes(string(out))
}

// parseUpowerTimes pulls `time to empty:` / `time to full:` from
// `upower -i` output. Values look like "2.7 hours", "45.2 minutes",
// or "unknown".
func parseUpowerTimes(out string) (toEmpty, toFull string) {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		var dst *string
		switch {
		case strings.HasPrefix(trimmed, "time to empty:"):
			dst = &toEmpty
		case strings.HasPrefix(trimmed, "time to full:"):
			dst = &toFull
		default:
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(trimmed,
			strings.SplitN(trimmed, ":", 2)[0]+":"))
		*dst = formatUpowerDuration(val)
	}
	return toEmpty, toFull
}

// formatUpowerDuration turns "2.7 hours" into "2h 42m", "45.2 minutes"
// into "45m". "unknown" (or anything unparseable) becomes "".
func formatUpowerDuration(val string) string {
	fields := strings.Fields(val)
	if len(fields) < 2 {
		return ""
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return ""
	}
	switch strings.ToLower(fields[1]) {
	case "hour", "hours":
		h := int(n)
		m := int(n*60) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	case "minute", "minutes":
		return fmt.Sprintf("%dm", int(n+0.5))
	default:
		return ""
	}
}

// ── ThinkPad charge thresholds ──────────────────────────────────────

// thresholdPaths returns the sysfs paths for the start/stop charge
// thresholds, preferring BAT0. ok is false when the kernel doesn't
// expose them — the UI must dim, never offer a dead control.
func thresholdPaths() (start, stop string, ok bool) {
	root := sysfsRoot()
	matches, _ := filepath.Glob(filepath.Join(root, "BAT*"))
	sort.Strings(matches)
	for _, dir := range matches {
		s := filepath.Join(dir, "charge_control_start_threshold")
		e := filepath.Join(dir, "charge_control_end_threshold")
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if _, err := os.Stat(e); err != nil {
			continue
		}
		return s, e, true
	}
	return "", "", false
}

func readThresholds() (start, stop int, err error) {
	s, e, ok := thresholdPaths()
	if !ok {
		return 0, 0, fmt.Errorf("charge thresholds not supported on this hardware")
	}
	start, err = readSysfsInt(s)
	if err != nil {
		return 0, 0, err
	}
	stop, err = readSysfsInt(e)
	if err != nil {
		return 0, 0, err
	}
	return start, stop, nil
}

func validateThresholds(start, stop int) error {
	if start < 0 || start > 100 || stop < 0 || stop > 100 {
		return fmt.Errorf("thresholds must be 0–100")
	}
	if start >= stop {
		return fmt.Errorf("start (%d%%) must be below stop (%d%%)", start, stop)
	}
	return nil
}

// elevator is doas preferred, sudo fallback — the navi-native way up.
func elevator() string {
	if _, err := exec.LookPath("doas"); err == nil {
		return "doas"
	}
	return "sudo"
}

// thresholdWriteScript writes both values in ONE elevated shell, so the
// user sees a single elevation prompt for the pair.
func thresholdWriteScript(startPath, stopPath string, start, stop int) string {
	return fmt.Sprintf("echo %d > %s && echo %d > %s", start, startPath, stop, stopPath)
}

func writeThresholds(start, stop int) (elev string, err error) {
	if err := validateThresholds(start, stop); err != nil {
		return "", err
	}
	s, e, ok := thresholdPaths()
	if !ok {
		return "", fmt.Errorf("charge thresholds not supported on this hardware")
	}
	elev = elevator()
	cmd := exec.Command(elev, "sh", "-c", thresholdWriteScript(s, e, start, stop))
	// Keep the terminal on stdin so the doas/sudo password prompt works.
	cmd.Stdin = os.Stdin
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return elev, fmt.Errorf("%s: %s", elev, msg)
	}
	return elev, nil
}
