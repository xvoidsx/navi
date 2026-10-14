package main

import (
	"os"
	"testing"
)

// The key resolution order for non-local backends: a typed key wins,
// otherwise the canonical environment variable (agents.env, sourced into
// the environment by wired/bashrc and mod-open.sh). Env-sourced keys must
// never be persisted to brain.json — brain.sh resolves them at runtime.
func TestEffectiveKeyEnvFallback(t *testing.T) {
	m := initialModel()
	m.backend = &backendDef{id: "openai", name: "OpenAI", needsKey: true}

	t.Setenv("OPENAI_API_KEY", "sk-env-fallback")
	key, env := m.effectiveKey()
	if key != "sk-env-fallback" || env != "OPENAI_API_KEY" {
		t.Fatalf("env fallback: got %q from %q", key, env)
	}

	// a typed key wins over the environment
	m.keyInput.SetValue("sk-typed")
	key, env = m.effectiveKey()
	if key != "sk-typed" || env != "" {
		t.Fatalf("typed key should win: got %q from %q", key, env)
	}

	// local backend has no canonical env var
	m.backend = &backendDef{id: "local", name: "Local Ollama"}
	m.keyInput.SetValue("")
	os.Unsetenv("OPENAI_API_KEY")
	if key, _ := m.effectiveKey(); key != "" {
		t.Fatalf("local backend should resolve no key, got %q", key)
	}
}

func TestKeyEnvVarMapping(t *testing.T) {
	cases := map[string]string{
		"openai":       "OPENAI_API_KEY",
		"openrouter":   "OPENROUTER_API_KEY",
		"ollama-cloud": "OLLAMA_API_KEY",
		"local":        "",
		"custom":       "",
	}
	for id, want := range cases {
		if got := (&backendDef{id: id}).keyEnvVar(); got != want {
			t.Errorf("backend %q: keyEnvVar = %q, want %q", id, got, want)
		}
	}
}
