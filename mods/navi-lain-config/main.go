// navi-lain-config — a bubble tea TUI for choosing Hey Lain's brain.
//
// Lets the user pick a backend (local Ollama, Ollama Cloud, OpenAI,
// OpenRouter, or a custom endpoint), a model, and (for key-based backends)
// an API key, then writes ~/.config/hey-lain/brain.json (0600), which
// bin/brain.sh reads. Compositor-agnostic: runs in any terminal, launched
// via rofi as `alacritty -e navi-lain-config`.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	agentenv "github.com/rav3ndust/navi-agentenv"
	theme "github.com/rav3ndust/navi-theme"
)

// ---------------------------------------------------------------------------
// backends
// ---------------------------------------------------------------------------

type backendDef struct {
	id           string
	name         string
	desc         string
	needsKey     bool
	styleChoice  bool // custom: the user picks the wire protocol
	apiStyle     string
	defaultModel string
	modelHint    string
	apiURL       string
	keyEnv       string // hint for where a key comes from
}

var backends = []backendDef{
	{
		id:           "local",
		name:         "Local Ollama",
		desc:         "tiny + private — runs on this machine, no keys, no cloud",
		apiStyle:     "ollama",
		defaultModel: "gemma3:270m",
		modelHint:    "any model your local ollama knows",
		apiURL:       "http://127.0.0.1:11434/api/chat",
	},
	{
		id:           "ollama-cloud",
		name:         "Ollama Cloud",
		desc:         "bigger brains through your local ollama daemon — no key handling here",
		apiStyle:     "ollama",
		defaultModel: "gemma4:31b-cloud",
		modelHint:    "a *-cloud model name, e.g. gemma4:31b-cloud",
		apiURL:       "http://127.0.0.1:11434/api/chat",
	},
	{
		id:           "openai",
		name:         "OpenAI",
		desc:         "your own API key, billed by OpenAI",
		needsKey:     true,
		apiStyle:     "openai",
		defaultModel: "gpt-4o-mini",
		modelHint:    "e.g. gpt-4o-mini",
		apiURL:       "https://api.openai.com/v1/chat/completions",
		keyEnv:       "sk-…",
	},
	{
		id:           "openrouter",
		name:         "OpenRouter",
		desc:         "your own API key — any model on the router",
		needsKey:     true,
		apiStyle:     "openai",
		defaultModel: "openai/gpt-oss-20b",
		modelHint:    "e.g. openai/gpt-oss-20b",
		apiURL:       "https://openrouter.ai/api/v1/chat/completions",
		keyEnv:       "sk-or-…",
	},
	{
		id:          "custom",
		name:        "Custom endpoint",
		desc:        "any OpenAI- or Ollama-compatible chat endpoint — you bring the URL",
		styleChoice: true,
		modelHint:   "the model name your endpoint expects",
		apiURL:      "",
	},
}

// local model suggestions for low-end hardware, offered alongside whatever
// `ollama list` reports as installed.
var suggestedLocal = []string{"gemma3:270m", "qwen3:0.6b", "smollm2:360m", "llama3.2:1b"}

var suggestedCloud = []string{"gemma4:31b-cloud", "gpt-oss:120b-cloud", "qwen3:235b-cloud"}

// ---------------------------------------------------------------------------
// config file
// ---------------------------------------------------------------------------

type brainConfig struct {
	Backend  string `json:"backend"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key,omitempty"`
	APIURL   string `json:"api_url"`
	APIStyle string `json:"api_style,omitempty"`
}

func configPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "hey-lain", "brain.json")
}

func loadConfig() brainConfig {
	cfg := brainConfig{Backend: "local", Model: "gemma3:270m", APIURL: backends[0].apiURL, APIStyle: "ollama"}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	var loaded brainConfig
	if json.Unmarshal(data, &loaded) == nil && loaded.Backend != "" {
		cfg = loaded
	}
	return cfg
}

func saveConfig(cfg brainConfig) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

func installedModels() []string {
	out, err := exec.Command("ollama", "list").Output()
	if err != nil {
		return nil
	}
	var names []string
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header / blank
		}
		if f := strings.Fields(line); len(f) > 0 {
			names = append(names, f[0])
		}
	}
	return names
}

func modelInstalled(name string) bool {
	for _, m := range installedModels() {
		if m == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// connection test — one tiny inference against the pending config.
// never includes the key in any returned string.
// ---------------------------------------------------------------------------

type testDoneMsg struct {
	result  string
	latency string
}

func testConnection(cfg brainConfig) (string, string) {
	start := time.Now()
	style := cfg.APIStyle
	if style == "" {
		style = "openai"
	}
	msgs := []map[string]string{{"role": "user", "content": "say hi"}}
	var payload map[string]any
	if style == "ollama" {
		payload = map[string]any{
			"model": cfg.Model, "stream": false, "messages": msgs,
			"options": map[string]any{"num_predict": 8},
		}
	} else {
		if cfg.APIKey == "" {
			return "fail: no API key set", ""
		}
		payload = map[string]any{
			"model": cfg.Model, "max_tokens": 8, "messages": msgs,
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "fail: " + err.Error(), ""
	}
	req, err := http.NewRequest("POST", cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return "fail: bad url — " + err.Error(), ""
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if style == "openai" {
		req.Header.Set("HTTP-Referer", "https://navi.xvoidsx.org")
		req.Header.Set("X-Title", "Hey Lain")
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	lat := time.Since(start).Round(100 * time.Millisecond).String()
	if err != nil {
		return "fail: unreachable — " + firstLine(err.Error()), lat
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		if style == "ollama" {
			// No key problem to chase here: this backend talks to the
			// local ollama daemon, and a 401 from it means the daemon
			// refused — for *-cloud models that is almost always a
			// missing `ollama signin`, not a bad API key.
			return "fail: HTTP 401 from the local ollama daemon — for *-cloud models run `ollama signin` in a terminal, then retry", lat
		}
		return fmt.Sprintf("fail: rejected (HTTP %d) — check your API key", resp.StatusCode), lat
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("fail: HTTP %d", resp.StatusCode), lat
	}
	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "fail: unreadable reply", lat
	}
	var said string
	if style == "ollama" {
		if m, ok := parsed["message"].(map[string]any); ok {
			said, _ = m["content"].(string)
		}
	} else {
		if ch, ok := parsed["choices"].([]any); ok && len(ch) > 0 {
			if c0, ok := ch[0].(map[string]any); ok {
				if m, ok := c0["message"].(map[string]any); ok {
					said, _ = m["content"].(string)
				}
			}
		}
	}
	if strings.TrimSpace(said) == "" {
		return "fail: empty reply", lat
	}
	return "ok", lat
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func runTest(cfg brainConfig) tea.Cmd {
	return func() tea.Msg {
		r, l := testConnection(cfg)
		return testDoneMsg{result: r, latency: l}
	}
}

// ---------------------------------------------------------------------------
// list items
// ---------------------------------------------------------------------------

type item struct{ title, desc string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

func newList(items []list.Item, w, h int) list.Model {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = theme.Selected
	d.Styles.SelectedDesc = theme.Dimmed
	d.Styles.NormalTitle = theme.Normal
	d.Styles.NormalDesc = theme.Dimmed
	d.Styles.DimmedTitle = theme.Dimmed
	l := list.New(items, d, w, h)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = theme.Header
	return l
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

const (
	screenBackend = iota
	screenModel
	screenModelCustom
	screenKey
	screenCustomURL
	screenCustomStyle
	screenURL // advanced: override the backend's API URL
	screenReview
	screenDone
)

type model struct {
	screen      int
	current     brainConfig
	backend     *backendDef
	backendIdx  int
	blist       list.Model
	mlist       list.Model
	slist       list.Model
	modelInput  textinput.Model
	keyInput    textinput.Model
	urlInput    textinput.Model
	chosenModel string
	chosenKey   string
	chosenURL   string
	chosenStyle string
	keyOptional bool
	// keyFromEnv tracks that the active key came from the environment
	// (agents.env via the shell/mod-open.sh) rather than being typed:
	// it is used for tests and display but never written to brain.json.
	keyFromEnv  bool
	keyEnvName  string
	testResult  string
	testLatency string
	testing     bool
	errMsg      string
	width       int
}

func initialModel() model {
	cur := loadConfig()
	bitems := make([]list.Item, len(backends))
	for i, b := range backends {
		cur2 := ""
		if cur.Backend == b.id {
			cur2 = "  ◀ current"
		}
		bitems[i] = item{title: b.name + cur2, desc: b.desc}
	}
	bl := newList(bitems, 64, 12)
	bl.Title = "hey lain — choose a brain backend"

	mi := textinput.New()
	mi.Placeholder = "model name"
	mi.CharLimit = 120
	mi.Width = 48

	ki := textinput.New()
	ki.Placeholder = "paste API key"
	ki.EchoMode = textinput.EchoPassword
	ki.CharLimit = 200
	ki.Width = 48

	ui := textinput.New()
	ui.Placeholder = "https://…"
	ui.CharLimit = 300
	ui.Width = 48

	sitems := []list.Item{
		item{title: "OpenAI-compatible", desc: "/chat/completions style — OpenAI, OpenRouter, most gateways"},
		item{title: "Ollama-native", desc: "/api/chat style — ollama, llama.cpp server, LM Studio"},
	}
	sl := newList(sitems, 64, 8)
	sl.Title = "wire protocol"

	// mlist is rebuilt by buildModelList() once a backend is chosen, but it
	// must still be a valid list before then: bubbletea delivers a
	// WindowSizeMsg on startup and Update calls mlist.SetSize, which
	// dereferences the delegate — a zero-value list.Model panics here.
	ml := newList(nil, 64, 12)
	ml.Title = "model"

	return model{
		screen:     screenBackend,
		current:    cur,
		blist:      bl,
		mlist:      ml,
		slist:      sl,
		modelInput: mi,
		keyInput:   ki,
		urlInput:   ui,
	}
}

func (m model) Init() tea.Cmd { return nil }

// keyEnvVar is the canonical environment variable for this backend's key,
// from the shared agentenv provider table (""). local needs no key and
// custom has no standard variable.
func (b *backendDef) keyEnvVar() string {
	if b == nil {
		return ""
	}
	if p := agentenv.ByID(b.id); p != nil {
		return p.EnvVar
	}
	return ""
}

// effectiveKey resolves the key for tests and display: a typed key wins,
// otherwise the canonical environment variable (agents.env, sourced into
// the environment by wired/bashrc and mod-open.sh).
func (m model) effectiveKey() (key, envName string) {
	if v := strings.TrimSpace(m.keyInput.Value()); v != "" {
		return v, ""
	}
	if v := strings.TrimSpace(m.chosenKey); v != "" {
		return v, ""
	}
	if env := m.backend.keyEnvVar(); env != "" {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, env
		}
	}
	return "", ""
}

// pendingConfig assembles the brainConfig from the current choices — used
// by both the connection test and the final save.
func (m model) pendingConfig() brainConfig {
	style := m.chosenStyle
	if style == "" && m.backend != nil {
		style = m.backend.apiStyle
	}
	url := m.chosenURL
	if url == "" && m.backend != nil {
		url = m.backend.apiURL
	}
	key, _ := m.effectiveKey()
	return brainConfig{
		Backend:  m.backend.id,
		Model:    m.chosenModel,
		APIKey:   key,
		APIURL:   url,
		APIStyle: style,
	}
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) buildModelList() list.Model {
	var items []list.Item
	b := m.backend
	switch b.id {
	case "local":
		seen := map[string]bool{}
		for _, name := range installedModels() {
			seen[name] = true
			items = append(items, item{title: name, desc: "installed"})
		}
		for _, name := range suggestedLocal {
			if !seen[name] {
				items = append(items, item{title: name, desc: "suggested — ollama pull " + name})
			}
		}
	case "ollama-cloud":
		for _, name := range suggestedCloud {
			items = append(items, item{title: name, desc: "via ollama cloud"})
		}
	}
	items = append(items, item{title: "⌨  type a model name…", desc: b.modelHint})
	l := newList(items, 64, 12)
	l.Title = "model for " + b.name
	return l
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case testDoneMsg:
		m.testing = false
		m.testResult = msg.result
		m.testLatency = msg.latency
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.blist.SetSize(64, 12)
		m.mlist.SetSize(64, 12)
		m.slist.SetSize(64, 8)
		return m, nil
	case tea.KeyMsg:
		if m.screen == screenDone {
			return m, tea.Quit // any key closes after save
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// typed as text inside inputs; quits (no save) everywhere else
			if m.screen != screenModelCustom && m.screen != screenKey &&
				m.screen != screenCustomURL && m.screen != screenURL {
				return m, tea.Quit
			}
		case "t":
			if m.screen == screenReview && !m.testing {
				m.testing = true
				m.testResult = ""
				return m, runTest(m.pendingConfig())
			}
		case "u":
			if m.screen == screenReview && !m.testing {
				m.urlInput.SetValue(m.pendingConfig().APIURL)
				m.urlInput.Focus()
				m.testResult = ""
				m.screen = screenURL
				return m, nil
			}
		case "esc":
			m.testing = false
			m.testResult = ""
			switch m.screen {
			case screenModel:
				m.screen = screenBackend
			case screenModelCustom:
				// openai/openrouter came from backend; custom from style picker;
				// local/cloud from the model list
				if m.backend.id == "openai" || m.backend.id == "openrouter" {
					m.screen = screenBackend
				} else if m.backend.styleChoice {
					m.screen = screenCustomStyle
				} else {
					m.screen = screenModel
				}
			case screenCustomURL:
				m.screen = screenBackend
			case screenCustomStyle:
				m.screen = screenCustomURL
			case screenURL:
				m.screen = screenReview
			case screenKey:
				m.screen = screenModelCustom
			case screenReview:
				if m.backend.needsKey || m.keyOptional {
					m.screen = screenKey
				} else {
					m.screen = screenModel
				}
			}
			return m, nil
		case "enter":
			return m.handleEnter()
		}
	}

	// route messages to the focused widget
	switch m.screen {
	case screenBackend:
		var cmd tea.Cmd
		m.blist, cmd = m.blist.Update(msg)
		return m, cmd
	case screenModel:
		var cmd tea.Cmd
		m.mlist, cmd = m.mlist.Update(msg)
		return m, cmd
	case screenCustomStyle:
		var cmd tea.Cmd
		m.slist, cmd = m.slist.Update(msg)
		return m, cmd
	case screenModelCustom:
		var cmd tea.Cmd
		m.modelInput, cmd = m.modelInput.Update(msg)
		return m, cmd
	case screenKey:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	case screenCustomURL, screenURL:
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func validURL(v string) bool {
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	m.errMsg = ""
	switch m.screen {
	case screenBackend:
		if it, ok := m.blist.SelectedItem().(item); ok {
			for i := range backends {
				if strings.HasPrefix(it.title, backends[i].name) {
					m.backendIdx = i
					break
				}
			}
			m.backend = &backends[m.backendIdx]
			m.chosenModel = ""
			m.chosenKey = ""
			m.chosenURL = ""
			m.chosenStyle = ""
			m.keyOptional = false
			m.keyFromEnv = false
			m.keyEnvName = ""
			m.testResult = ""
			if m.backend.styleChoice {
				m.urlInput.SetValue("")
				m.urlInput.Placeholder = "https://your-host/v1/chat/completions  (full chat endpoint)"
				m.urlInput.Focus()
				m.screen = screenCustomURL
			} else if m.backend.id == "openai" || m.backend.id == "openrouter" {
				m.modelInput.SetValue(m.backend.defaultModel)
				m.modelInput.Focus()
				m.screen = screenModelCustom
			} else {
				m.mlist = m.buildModelList()
				m.screen = screenModel
			}
		}
	case screenCustomURL:
		v := strings.TrimSpace(m.urlInput.Value())
		if !validURL(v) {
			m.errMsg = "that doesn't look like a URL — it should start with http:// or https://"
			return m, nil
		}
		m.chosenURL = v
		m.screen = screenCustomStyle
	case screenCustomStyle:
		if it, ok := m.slist.SelectedItem().(item); ok {
			if strings.HasPrefix(it.title, "OpenAI") {
				m.chosenStyle = "openai"
			} else {
				m.chosenStyle = "ollama"
			}
			m.modelInput.SetValue("")
			m.modelInput.Placeholder = m.backend.modelHint
			m.modelInput.Focus()
			m.screen = screenModelCustom
		}
	case screenModel:
		if it, ok := m.mlist.SelectedItem().(item); ok {
			if strings.HasPrefix(it.title, "⌨") {
				m.modelInput.SetValue("")
				m.modelInput.Placeholder = m.backend.modelHint
				m.modelInput.Focus()
				m.screen = screenModelCustom
			} else {
				m.chosenModel = it.title
				m.advanceFromModel()
			}
		}
	case screenModelCustom:
		v := strings.TrimSpace(m.modelInput.Value())
		if v == "" {
			m.errMsg = "type a model name first"
			return m, nil
		}
		m.chosenModel = v
		m.advanceFromModel()
	case screenKey:
		v := strings.TrimSpace(m.keyInput.Value())
		if v == "" {
			// empty paste: fall back to the environment (agents.env).
			// accepted for keyed backends; the key itself is never
			// written to brain.json — brain.sh resolves it at runtime.
			if env := m.backend.keyEnvVar(); env != "" && strings.TrimSpace(os.Getenv(env)) != "" {
				m.chosenKey = ""
				m.keyFromEnv = true
				m.keyEnvName = env
				m.screen = screenReview
				return m, nil
			}
			if !m.keyOptional {
				m.errMsg = "paste an API key first (esc to go back)"
				return m, nil
			}
		}
		m.chosenKey = v
		m.keyFromEnv = false
		m.keyEnvName = ""
		m.screen = screenReview
	case screenURL:
		v := strings.TrimSpace(m.urlInput.Value())
		if !validURL(v) {
			m.errMsg = "that doesn't look like a URL — it should start with http:// or https://"
			return m, nil
		}
		m.chosenURL = v
		m.screen = screenReview
	case screenReview:
		cfg := m.pendingConfig()
		if m.keyFromEnv {
			// env-sourced keys are never persisted to brain.json —
			// brain.sh resolves them from agents.env at runtime.
			cfg.APIKey = ""
		}
		if err := saveConfig(cfg); err != nil {
			m.errMsg = "save failed: " + err.Error()
			return m, nil
		}
		m.screen = screenDone
	case screenDone:
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) advanceFromModel() {
	if m.backend.needsKey {
		m.keyInput.SetValue("")
		// if agents.env (or the live environment) already has this
		// backend's key, say so — enter keeps it, pasting overrides.
		if env := m.backend.keyEnvVar(); env != "" && strings.TrimSpace(os.Getenv(env)) != "" {
			m.keyInput.Placeholder = "key found in " + env + " — enter to keep, or paste to override"
		} else {
			m.keyInput.Placeholder = "paste API key"
		}
		m.keyInput.Focus()
		m.screen = screenKey
	} else if m.backend.styleChoice {
		// custom endpoints may or may not want a key
		m.keyOptional = true
		m.keyInput.SetValue("")
		m.keyInput.Placeholder = "paste API key (enter to skip)"
		m.keyInput.Focus()
		m.screen = screenKey
	} else {
		m.screen = screenReview
	}
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func frame(content string) string {
	return theme.Border.Width(68).Render(content)
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(theme.Logo.Render("∅ ") + theme.Title.Render("hey lain — brain config") + "\n")
	cur := m.current
	curDesc := cur.Backend + " / " + cur.Model
	if cur.Backend == "" {
		curDesc = "local / gemma3:270m (default)"
	}
	b.WriteString(theme.Dimmed.Render("current: "+curDesc) + "\n\n")

	switch m.screen {
	case screenBackend:
		b.WriteString(m.blist.View() + "\n")
	case screenModel:
		b.WriteString(m.mlist.View() + "\n")
	case screenCustomURL:
		b.WriteString(theme.Header.Render("endpoint URL for Custom endpoint") + "\n")
		b.WriteString(theme.Dimmed.Render("the full chat endpoint — e.g. https://your-host/v1/chat/completions") + "\n\n")
		b.WriteString(m.urlInput.View() + "\n")
	case screenCustomStyle:
		b.WriteString(m.slist.View() + "\n")
	case screenModelCustom:
		b.WriteString(theme.Header.Render("model for "+m.backend.name) + "\n")
		b.WriteString(theme.Dimmed.Render(m.backend.modelHint) + "\n\n")
		b.WriteString(m.modelInput.View() + "\n")
	case screenKey:
		if m.keyOptional {
			b.WriteString(theme.Header.Render("API key (optional) for "+m.backend.name) + "\n")
			b.WriteString(theme.Dimmed.Render("enter to skip — no key will be sent") + "\n\n")
		} else {
			b.WriteString(theme.Header.Render("API key for "+m.backend.name) + "\n")
			b.WriteString(theme.Dimmed.Render("stored in ~/.config/hey-lain/brain.json (mode 0600) — never leaves this machine except to "+m.backend.name) + "\n\n")
		}
		b.WriteString(m.keyInput.View() + "\n")
	case screenURL:
		b.WriteString(theme.Header.Render("API URL override") + "\n")
		b.WriteString(theme.Dimmed.Render("advanced: replace the default endpoint for "+m.backend.name) + "\n\n")
		b.WriteString(m.urlInput.View() + "\n")
	case screenReview:
		cfg := m.pendingConfig()
		b.WriteString(theme.Header.Render("review") + "\n\n")
		b.WriteString(fmt.Sprintf("  backend   %s\n", theme.Normal.Render(m.backend.name)))
		b.WriteString(fmt.Sprintf("  model     %s\n", theme.Selected.Render(m.chosenModel)))
		b.WriteString(fmt.Sprintf("  api url   %s\n", theme.Normal.Render(cfg.APIURL)))
		b.WriteString(fmt.Sprintf("  protocol  %s\n", theme.Normal.Render(cfg.APIStyle)))
		if m.backend.needsKey || m.keyOptional {
			keyNote := "•••••••• (set)"
			if m.keyFromEnv {
				keyNote = "•••••••• (from " + m.keyEnvName + ")"
			} else if m.chosenKey == "" {
				keyNote = "(not set)"
			}
			b.WriteString(fmt.Sprintf("  api key   %s\n", theme.Normal.Render(keyNote)))
		}
		b.WriteString("\n")
		if m.backend.id != "local" {
			b.WriteString(theme.Error.Render("  ⚠ transcripts leave this machine — your words go to "+m.backend.name) + "\n\n")
		}
		if m.backend.id == "local" && !modelInstalled(m.chosenModel) {
			b.WriteString(theme.Error.Render("  ! "+m.chosenModel+" isn't installed — run: ollama pull "+m.chosenModel) + "\n\n")
		}
		if m.testing {
			b.WriteString(theme.Dimmed.Render("  testing connection…") + "\n\n")
		} else if m.testResult != "" {
			style := theme.Selected
			if !strings.HasPrefix(m.testResult, "ok") {
				style = theme.Error
			}
			line := "  test: " + m.testResult
			if m.testLatency != "" {
				line += " (" + m.testLatency + ")"
			}
			b.WriteString(style.Render(line) + "\n\n")
		}
		b.WriteString(theme.Dimmed.Render("  enter save · t test connection · u override api url · esc back") + "\n")
	case screenDone:
		b.WriteString(theme.Header.Render("saved ✓") + "\n\n")
		b.WriteString(theme.Normal.Render("  ~/.config/hey-lain/brain.json") + "\n")
		b.WriteString(theme.Dimmed.Render("  tap Alt+V and talk — the new brain is live on the next utterance") + "\n\n")
		b.WriteString(theme.Dimmed.Render("  press any key to close") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n" + theme.Error.Render(m.errMsg) + "\n")
	} else if m.screen != screenDone {
		b.WriteString("\n" + theme.Dimmed.Render("enter select · esc back · q quit (no save)") + "\n")
	}

	return frame(b.String())
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-lain-config:", err)
		os.Exit(1)
	}
}
