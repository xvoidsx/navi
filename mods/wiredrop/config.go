package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is ~/.config/wiredrop/config.json. Everything has a sane
// default; the file only needs to exist when the user changes something.
type Config struct {
	Alias       string `json:"alias"`       // shown to peers; default: hostname
	Port        int    `json:"port"`        // https port; 0 = 53317-or-next-free
	DownloadDir string `json:"downloadDir"` // default: XDG_DOWNLOAD_DIR or ~/Downloads
	PIN         string `json:"pin"`         // optional static PIN; empty = consent-via-dunst
}

// Paths groups every filesystem location wiredrop uses.
type Paths struct {
	ConfigDir  string // ~/.config/wiredrop
	ConfigFile string // .../config.json
	CertDir    string // ... (cert.pem/key.pem)
	KnownHosts string // .../known_hosts
	RuntimeDir string // $XDG_RUNTIME_DIR/wiredrop (peers.json, control.sock)
	StateDir   string // ~/.local/share/... no — history lives in state
}

// resolvePaths computes locations from the environment.
func resolvePaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("wiredrop: no home dir: %w", err)
	}
	cfgBase := os.Getenv("XDG_CONFIG_HOME")
	if cfgBase == "" {
		cfgBase = filepath.Join(home, ".config")
	}
	runBase := os.Getenv("XDG_RUNTIME_DIR")
	if runBase == "" {
		// No session runtime dir (ssh without pam_systemd, containers).
		// Fall back to a per-user tmp dir — the daemon still works, the
		// socket just isn't cleaned by the session manager.
		runBase = filepath.Join(os.TempDir(), fmt.Sprintf("wiredrop-%d", os.Getuid()))
	}
	cfgDir := filepath.Join(cfgBase, "wiredrop")
	return &Paths{
		ConfigDir:  cfgDir,
		ConfigFile: filepath.Join(cfgDir, "config.json"),
		CertDir:    cfgDir,
		KnownHosts: filepath.Join(cfgDir, "known_hosts"),
		RuntimeDir: filepath.Join(runBase, "wiredrop"),
	}, nil
}

// loadConfig reads config.json, filling defaults for anything absent.
func loadConfig(p *Paths) (*Config, error) {
	cfg := &Config{}
	if data, err := os.ReadFile(p.ConfigFile); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("wiredrop: parse config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("wiredrop: read config: %w", err)
	}
	if cfg.Alias == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			h = "navi"
		}
		cfg.Alias = h
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	return cfg, nil
}

// defaultDownloadDir honors XDG user-dirs when present, else ~/Downloads.
func defaultDownloadDir() string {
	if data, err := os.ReadFile(os.ExpandEnv("$HOME/.config/user-dirs.dirs")); err == nil {
		for _, line := range splitLines(string(data)) {
			var val string
			if n, _ := fmt.Sscanf(line, "XDG_DOWNLOAD_DIR=%q", &val); n == 1 {
				val = os.ExpandEnv(val)
				if val != "" {
					return val
				}
			}
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
