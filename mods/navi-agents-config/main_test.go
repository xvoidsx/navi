package main

import (
	"os"
	"path/filepath"
	"testing"

	agentenv "github.com/rav3ndust/navi-agentenv"
)

// point the store at a temp dir via XDG_CONFIG_HOME
func testModel(t *testing.T) model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OLLAMA_API_KEY", "")
	m := initialModel()
	return m
}

func TestKeyOfPrefersStoreOverEnv(t *testing.T) {
	m := testModel(t)
	p := agentenv.ByID("openai")

	t.Setenv("OPENAI_API_KEY", "sk-env-1234")
	if v, src := m.keyOf(*p); v != "sk-env-1234" || src != "environment" {
		t.Fatalf("env fallback: got %q from %q", v, src)
	}

	m.fileVals["OPENAI_API_KEY"] = "sk-store-9999"
	if v, src := m.keyOf(*p); v != "sk-store-9999" || src != "agents.env" {
		t.Fatalf("store should win: got %q from %q", v, src)
	}

	if err := m.saveStore(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(agentenv.EnvFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("store mode = %o, want 600", fi.Mode().Perm())
	}
	loaded, err := agentenv.LoadEnvFile(agentenv.EnvFilePath())
	if err != nil || loaded["OPENAI_API_KEY"] != "sk-store-9999" {
		t.Errorf("round-trip failed: %v %+v", err, loaded)
	}
	_ = filepath.Join // keep import if unused in future edits
}

func TestImportFromEnv(t *testing.T) {
	m := testModel(t)
	t.Setenv("OLLAMA_API_KEY", "ollama-env-key")
	t.Setenv("OPENAI_API_KEY", "")
	if n := m.importFromEnv(); n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}
	if m.fileVals["OLLAMA_API_KEY"] != "ollama-env-key" {
		t.Error("OLLAMA_API_KEY not imported into store")
	}
	// second import finds nothing new
	if n := m.importFromEnv(); n != 0 {
		t.Fatalf("re-import found %d, want 0", n)
	}
}

func TestDefaultAgentRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// no navi-lain/hermes anywhere, so the no-file fallback is opencode.
	t.Setenv("PATH", t.TempDir())

	if got := readDefaultAgent(); got != "opencode" {
		t.Fatalf("no file: got %q, want opencode", got)
	}
	if err := writeDefaultAgent("goose"); err != nil {
		t.Fatal(err)
	}
	if got := readDefaultAgent(); got != "goose" {
		t.Fatalf("round-trip: got %q, want goose", got)
	}
	// garbage in the file resolves to the navi-Q fallback, not the garbage.
	p := defaultAgentPath()
	if err := os.WriteFile(p, []byte("not-a-harness\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readDefaultAgent(); got != "opencode" {
		t.Fatalf("garbage file: got %q, want opencode", got)
	}
}

func TestListHerdrAgentsNotInstalled(t *testing.T) {
	// empty PATH: herdr is nowhere to be found, so the message must say
	// it isn't installed — never that it isn't running.
	t.Setenv("PATH", t.TempDir())
	_, note := listHerdrAgents()
	if note != "herdr isn't installed" {
		t.Fatalf("got %q, want herdr-isn't-installed", note)
	}
}
