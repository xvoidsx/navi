package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withEnv swaps env vars and PATH for a test, restoring afterwards.
func withEnv(t *testing.T, home, path string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("PATH", path)
}

func TestTableValid(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range browserTable {
		if def.ID == "" || def.Name == "" {
			t.Errorf("browser entry missing id/name: %+v", def)
		}
		if seen[def.ID] {
			t.Errorf("duplicate browser id %q", def.ID)
		}
		seen[def.ID] = true
		if len(def.Binaries) == 0 {
			t.Errorf("browser %q has no detection binaries", def.ID)
		}
		if !filepath.IsAbs(def.PolicyDir) {
			t.Errorf("browser %q policy dir %q is not absolute", def.ID, def.PolicyDir)
		}
		if !strings.HasSuffix(def.PolicyDir, "managed") {
			t.Errorf("browser %q policy dir %q should end in managed", def.ID, def.PolicyDir)
		}
	}
	if findDef("chromium") == nil {
		t.Error("table must contain a chromium entry (the fallback)")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	withEnv(t, home, os.Getenv("PATH"))
	if err := writeDefault("brave"); err != nil {
		t.Fatalf("writeDefault: %v", err)
	}
	if got := readDefault(); got != "brave" {
		t.Fatalf("readDefault = %q, want brave", got)
	}
	if err := writeDefault("nope"); err == nil {
		t.Fatal("writeDefault accepted an unknown id")
	}
	// corrupt the file: unknown id reads back as unset
	os.WriteFile(configPath(), []byte("nope\n"), 0o600)
	if got := readDefault(); got != "" {
		t.Fatalf("readDefault on unknown id = %q, want empty", got)
	}
}

func TestResolveFallbackChain(t *testing.T) {
	bindir := t.TempDir()
	home := t.TempDir()
	withEnv(t, home, bindir) // empty PATH dir: nothing installed
	for _, name := range []string{"chromium", "brave-browser-nightly", "yandex-browser"} {
		p := filepath.Join(bindir, name)
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	}

	// configured browser wins
	if err := writeDefault("brave"); err != nil {
		t.Fatal(err)
	}
	id, bin := resolveBinary()
	if id != "brave" || !strings.HasSuffix(bin, "brave-browser-nightly") {
		t.Fatalf("resolve = %q %q, want brave .../brave-browser-nightly", id, bin)
	}

	// configured browser missing -> chromium fallback
	os.WriteFile(configPath(), []byte("brave\n"), 0o600)
	os.Remove(filepath.Join(bindir, "brave-browser-nightly"))
	id, bin = resolveBinary()
	if id != "chromium" || !strings.HasSuffix(bin, "chromium") {
		t.Fatalf("resolve = %q %q, want chromium fallback", id, bin)
	}

	// nothing configured and no chromium -> first installed
	os.Remove(configPath())
	os.Remove(filepath.Join(bindir, "chromium"))
	id, bin = resolveBinary()
	if id != "yandex" || !strings.HasSuffix(bin, "yandex-browser") {
		t.Fatalf("resolve = %q %q, want yandex first-installed", id, bin)
	}

	// nothing at all -> unresolved, caller falls back to bare "chromium"
	os.Remove(filepath.Join(bindir, "yandex-browser"))
	id, bin = resolveBinary()
	if id != "" || bin != "chromium" {
		t.Fatalf("resolve = %q %q, want empty + chromium", id, bin)
	}
}

func TestDiscoverPolicyDir(t *testing.T) {
	dir := t.TempDir()
	// fake binary containing a compiled-in policy path
	fake := filepath.Join(dir, "fake-browser")
	payload := append([]byte{0, 1, 2}, []byte("/etc/fake-browser/policies")...)
	payload = append(payload, 0, 3, 4)
	os.WriteFile(fake, payload, 0o755)

	got, probed := discoverPolicyDir(fake, "/etc/fallback/policies/managed")
	if !probed || got != "/etc/fake-browser/policies/managed" {
		t.Fatalf("discover = %q probed=%v, want probed /etc/fake-browser/policies/managed", got, probed)
	}

	// binary without the marker -> table fallback
	plain := filepath.Join(dir, "plain")
	os.WriteFile(plain, []byte("nothing to see here"), 0o755)
	got, probed = discoverPolicyDir(plain, "/etc/fallback/policies/managed")
	if probed || got != "/etc/fallback/policies/managed" {
		t.Fatalf("discover = %q probed=%v, want fallback", got, probed)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/etc/opt/edge/policies/managed"); got != "'/etc/opt/edge/policies/managed'" {
		t.Fatalf("shellQuote = %q", got)
	}
	if got := shellQuote("o'brien"); got != `'o'\''brien'` {
		t.Fatalf("shellQuote with quote = %q", got)
	}
}

func TestDeployPolicyShell(t *testing.T) {
	t.Setenv("NAVI_POLICY_SRC", "/nonexistent.json")
	if _, err := deployPolicyShell("/etc/brave/policies/managed"); err == nil {
		t.Fatal("deployPolicyShell succeeded with no policy source")
	}
	src := filepath.Join(t.TempDir(), "navi.json")
	os.WriteFile(src, []byte("{}"), 0o644)
	t.Setenv("NAVI_POLICY_SRC", src)
	cmd, err := deployPolicyShell("/etc/brave/policies/managed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd, "doas install -d -m 0755 '/etc/brave/policies/managed'") {
		t.Fatalf("missing mkdir in %q", cmd)
	}
	if !strings.Contains(cmd, "'"+src+"'") || !strings.Contains(cmd, "navi.json'") {
		t.Fatalf("missing src/dst in %q", cmd)
	}
}

// TestBrowseViewNoSequenceLeak renders the TUI in forced true color and
// asserts no ANSI sequence bodies leak as literal text (the lipgloss
// nesting bug class: an ESC detached from its "[" prints as "[38;2;…m").
// A cursor-selected row exercises the heaviest styling in the view.
func TestBrowseViewNoSequenceLeak(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	home := t.TempDir()
	bindir := t.TempDir()
	withEnv(t, home, bindir)
	// one installed browser so rows render in both states
	p := filepath.Join(bindir, "chromium")
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)

	m := newModel()
	m.cursor = 0 // selected row: Selected.Render over styled parts
	m.width, m.height = 100, 40
	out := m.View()

	// strip every well-formed SGR sequence; what remains must not look
	// like a detached sequence body.
	stripped := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(out, "")
	if regexp.MustCompile(`\[[0-9;]+m`).MatchString(stripped) {
		t.Fatalf("detached ANSI sequence body in view output:\n%s", stripped)
	}
	if !strings.Contains(out, "Chromium") {
		t.Fatal("view does not mention the installed browser")
	}
}
