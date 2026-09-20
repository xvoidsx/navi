// Package agentenv is the single source of truth for AI provider API keys
// on navi: which environment variable each major provider uses, how to
// test a key, and where the system-wide key store lives.
//
// Keys live in ONE file: ~/.config/navi/agents.env (KEY=VALUE, mode 0600),
// managed by the Navi Agent Configuration app. Everything else — interactive
// shells (via wired/bashrc), waybar/rofi-launched apps (via mod-open.sh),
// Hey Lain's brain.sh, navi-lain-config — reads from that file or from the
// process environment, never from a second store. stdlib only.
package agentenv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AuthStyle describes how a provider wants its key presented on the
// models-list probe used by the connection test.
type AuthStyle string

const (
	// AuthBearer sends Authorization: Bearer <key> (OpenAI-compatible).
	AuthBearer AuthStyle = "bearer"
	// AuthXAPIKey sends x-api-key: <key> plus anthropic-version (Anthropic).
	AuthXAPIKey AuthStyle = "x-api-key"
	// AuthGoogKey sends x-goog-api-key: <key> (Google Gemini).
	AuthGoogKey AuthStyle = "x-goog-api-key"
)

// Provider describes one key-issuing AI provider.
type Provider struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	EnvVar    string    `json:"env_var"`
	ModelsURL string    `json:"models_url"`
	Auth      AuthStyle `json:"auth"`
	KeyHint   string    `json:"key_hint,omitempty"`
	Custom    bool      `json:"custom,omitempty"`
}

// Providers returns the built-in provider table.
func Providers() []Provider {
	return []Provider{
		{ID: "openai", Name: "OpenAI", EnvVar: "OPENAI_API_KEY",
			ModelsURL: "https://api.openai.com/v1/models", Auth: AuthBearer, KeyHint: "sk-…"},
		{ID: "anthropic", Name: "Anthropic", EnvVar: "ANTHROPIC_API_KEY",
			ModelsURL: "https://api.anthropic.com/v1/models", Auth: AuthXAPIKey, KeyHint: "sk-ant-…"},
		{ID: "openrouter", Name: "OpenRouter", EnvVar: "OPENROUTER_API_KEY",
			ModelsURL: "https://openrouter.ai/api/v1/models", Auth: AuthBearer, KeyHint: "sk-or-…"},
		{ID: "ollama-cloud", Name: "Ollama Cloud", EnvVar: "OLLAMA_API_KEY",
			ModelsURL: "https://ollama.com/v1/models", Auth: AuthBearer, KeyHint: "(from ollama.com settings)"},
		{ID: "gemini", Name: "Google Gemini", EnvVar: "GEMINI_API_KEY",
			ModelsURL: "https://generativelanguage.googleapis.com/v1beta/models", Auth: AuthGoogKey, KeyHint: "AIza…"},
		{ID: "xai", Name: "xAI", EnvVar: "XAI_API_KEY",
			ModelsURL: "https://api.x.ai/v1/models", Auth: AuthBearer, KeyHint: "xai-…"},
		{ID: "deepseek", Name: "DeepSeek", EnvVar: "DEEPSEEK_API_KEY",
			ModelsURL: "https://api.deepseek.com/v1/models", Auth: AuthBearer, KeyHint: "sk-…"},
		{ID: "mistral", Name: "Mistral AI", EnvVar: "MISTRAL_API_KEY",
			ModelsURL: "https://api.mistral.ai/v1/models", Auth: AuthBearer, KeyHint: "…"},
	}
}

// ByID finds a built-in provider by id, or nil.
func ByID(id string) *Provider {
	for _, p := range Providers() {
		if p.ID == id {
			cp := p
			return &cp
		}
	}
	return nil
}

// EnvFilePath is the system-wide key store.
func EnvFilePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "navi", "agents.env")
}

// CustomFilePath stores user-added provider definitions.
func CustomFilePath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "navi", "agents-providers.json")
}

// LoadEnvFile parses a KEY=VALUE file. Blank lines and # comments are
// skipped; single/double quotes around values are stripped; a line without
// '=' is ignored. Missing file is not an error — it just means no keys yet.
func LoadEnvFile(path string) (map[string]string, error) {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return vals, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// allow a leading "export "
		line = strings.TrimPrefix(line, "export ")
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			// undo the '\'' escaping SaveEnvFile emits
			v = strings.ReplaceAll(v[1:len(v)-1], `'\''`, "'")
		} else if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		if k != "" && isEnvName(k) {
			vals[k] = v
		}
	}
	return vals, sc.Err()
}

func isEnvName(s string) bool {
	for i, r := range s {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (i > 0 && r >= 'a' && r <= 'z')
		if !ok {
			return false
		}
	}
	return len(s) > 0
}

// SaveEnvFile writes vals as sorted KEY=VALUE lines, mode 0600. Values are
// single-quoted when they contain whitespace or quotes.
func SaveEnvFile(path string, vals map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(vals))
	for k, v := range vals {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("# navi agent API keys — managed by Navi Agent Configuration.\n")
	sb.WriteString("# sourced by your shell (wired/bashrc) and by mod-open.sh; mode 0600.\n")
	for _, k := range keys {
		v := vals[k]
		if strings.ContainsAny(v, " \t\"'") {
			v = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
		}
		fmt.Fprintf(&sb, "%s=%s\n", k, v)
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

// Masked returns a non-revealing preview of a key: •••• + last 4 chars.
func Masked(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "not set"
	}
	if len(key) <= 4 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

// LoadCustom reads user-added provider definitions. Missing file is fine.
func LoadCustom(path string) ([]Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ps []Provider
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].Custom = true
	}
	return ps, nil
}

// SaveCustom persists user-added provider definitions, mode 0600.
func SaveCustom(path string, ps []Provider) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
