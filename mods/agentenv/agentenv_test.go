package agentenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderTableSane(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Providers() {
		if p.ID == "" || p.Name == "" || p.EnvVar == "" || p.ModelsURL == "" {
			t.Errorf("provider %+v has empty field", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate provider id %q", p.ID)
		}
		seen[p.ID] = true
		switch p.Auth {
		case AuthBearer, AuthXAPIKey, AuthGoogKey:
		default:
			t.Errorf("provider %q has unknown auth style %q", p.ID, p.Auth)
		}
	}
	if ByID("openai") == nil || ByID("openai").EnvVar != "OPENAI_API_KEY" {
		t.Error("ByID(openai) wrong")
	}
	if ByID("nope") != nil {
		t.Error("ByID(nope) should be nil")
	}
}

func TestEnvFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.env")
	in := map[string]string{
		"OPENAI_API_KEY":    "sk-test-1234",
		"OLLAMA_API_KEY":    "with spaces and 'quotes'",
		"EMPTY_SHOULD_DROP": "",
	}
	if err := SaveEnvFile(path, in); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
	out, err := LoadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out["OPENAI_API_KEY"] != "sk-test-1234" {
		t.Errorf("openai key mismatch: %q", out["OPENAI_API_KEY"])
	}
	if out["OLLAMA_API_KEY"] != "with spaces and 'quotes'" {
		t.Errorf("quoting round-trip failed: %q", out["OLLAMA_API_KEY"])
	}
	if _, ok := out["EMPTY_SHOULD_DROP"]; ok {
		t.Error("empty values should be dropped")
	}
	// missing file is fine
	if _, err := LoadEnvFile(filepath.Join(dir, "nope.env")); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
}

func TestMasked(t *testing.T) {
	if Masked("") != "not set" {
		t.Error("empty should be 'not set'")
	}
	if got := Masked("sk-abcdef123456"); got != "••••3456" {
		t.Errorf("masked = %q", got)
	}
}
