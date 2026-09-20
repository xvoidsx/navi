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
