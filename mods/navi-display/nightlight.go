// nightlight.go — night-light config and daemon control for navi-display.
//
// Wayland uses wlsunset, X11 uses gammastep. Config persists in
// ~/.config/navi-display/config.json. "Apply now" (re)spawns the daemon;
// the mod never babysits it afterward. When the daemon binary is missing,
// the UI dims the section with an install hint instead of failing.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// NightConfig is the persisted night-light state.
type NightConfig struct {
	Enabled   bool   `json:"enabled"`
	DayTemp   int    `json:"day_temp"`   // Kelvin
	NightTemp int    `json:"night_temp"` // Kelvin
	Auto      bool   `json:"auto"`       // lat/lon vs manual dawn/dusk
	Lat       string `json:"lat"`
	Lon       string `json:"lon"`
	Dawn      string `json:"dawn"` // "06:30", wlsunset only
	Dusk      string `json:"dusk"` // "19:45", wlsunset only
}

func defaultNightConfig() NightConfig {
	return NightConfig{
		Enabled:   false,
		DayTemp:   6500,
		NightTemp: 3400,
		Auto:      true,
		Lat:       "34.55", // Mena, AR — home turf; change it with p
		Lon:       "-94.24",
		Dawn:      "06:30",
		Dusk:      "19:45",
	}
}

func nightConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "navi-display", "config.json")
}

func loadNightConfig() NightConfig {
	cfg := defaultNightConfig()
	p := nightConfigPath()
	if p == "" {
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultNightConfig()
	}
	if cfg.DayTemp < 1000 || cfg.DayTemp > 10000 {
		cfg.DayTemp = 6500
	}
	if cfg.NightTemp < 1000 || cfg.NightTemp > 10000 {
		cfg.NightTemp = 3400
	}
	return cfg
}

func saveNightConfig(cfg NightConfig) error {
	p := nightConfigPath()
	if p == "" {
		return fmt.Errorf("no home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// nightDaemon is the binary for the backend, or "" when not installed.
func nightDaemon(b Backend) string {
	want := "wlsunset"
	if b == BackendX11 {
		want = "gammastep"
	}
	if _, err := exec.LookPath(want); err == nil {
		return want
	}
	return ""
}

// applyNightLight (re)spawns the night-light daemon per cfg. Disabling
// just kills it. The process is detached — we don't wait on it.
func applyNightLight(b Backend, cfg NightConfig) error {
	daemon := nightDaemon(b)
	if daemon == "" {
		return fmt.Errorf("no night-light daemon installed")
	}
	// Stop any running instance first so flags can't stack.
	_ = exec.Command("pkill", "-x", daemon).Run()
	if !cfg.Enabled {
		return nil
	}
	var args []string
	switch daemon {
	case "wlsunset":
		args = []string{"-t", fmt.Sprint(cfg.DayTemp), "-T", fmt.Sprint(cfg.NightTemp)}
		if cfg.Auto {
			args = append(args, "-l", cfg.Lat, "-L", cfg.Lon)
		} else {
			args = append(args, "-S", cfg.Dawn, "-s", cfg.Dusk)
		}
	case "gammastep":
		// gammastep has no manual dawn/dusk flags — it always derives
		// the schedule from location, so manual mode falls back to auto.
		args = []string{"-t", fmt.Sprintf("%d:%d", cfg.DayTemp, cfg.NightTemp),
			"-l", cfg.Lat+":"+cfg.Lon}
	}
	cmd := exec.Command(daemon, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", daemon, err)
	}
	return nil
}
