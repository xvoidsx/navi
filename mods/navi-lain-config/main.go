// navi-lain-config — a bubble tea TUI for choosing Hey Lain's brain.
//
// Lets the user pick a backend (local Ollama, Ollama Cloud, OpenAI,
// OpenRouter), a model, and (for key-based backends) an API key, then
// writes ~/.config/hey-lain/brain.json (0600), which bin/brain.sh reads.
// Compositor-agnostic: runs in any terminal, launched via rofi as
// `alacritty -e navi-lain-config`.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
		defaultModel: "gemma3:270m",
		modelHint:    "any model your local ollama knows",
		apiURL:       "http://127.0.0.1:11434/api/chat",
	},
	{
		id:           "ollama-cloud",
		name:         "Ollama Cloud",
		desc:         "bigger brains through your local ollama daemon — no key handling here",
		defaultModel: "gemma4:31b-cloud",
		modelHint:    "a *-cloud model name, e.g. gemma4:31b-cloud",
		apiURL:       "http://127.0.0.1:11434/api/chat",
	},
	{
		id:           "openai",
		name:         "OpenAI",
		desc:         "your own API key, billed by OpenAI",
		needsKey:     true,
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
		defaultModel: "openai/gpt-oss-20b",
		modelHint:    "e.g. openai/gpt-oss-20b",
		apiURL:       "https://openrouter.ai/api/v1/chat/completions",
		keyEnv:       "sk-or-…",
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
	Backend string `json:"backend"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
	APIURL  string `json:"api_url"`
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
	cfg := brainConfig{Backend: "local", Model: "gemma3:270m", APIURL: backends[0].apiURL}
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
	screenReview
	screenDone
)

type model struct {
	screen     int
	current    brainConfig
	backend    *backendDef
	backendIdx int
	blist      list.Model
	mlist      list.Model
	modelInput textinput.Model
	keyInput   textinput.Model
	chosenModel string
	chosenKey   string
	errMsg     string
	width      int
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
	bl := newList(bitems, 64, 10)
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

	return model{
		screen:     screenBackend,
		current:    cur,
		blist:      bl,
		modelInput: mi,
		keyInput:   ki,
	}
}

func (m model) Init() tea.Cmd { return nil }

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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.blist.SetSize(64, 10)
		m.mlist.SetSize(64, 12)
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
			if m.screen != screenModelCustom && m.screen != screenKey {
				return m, tea.Quit
			}
		case "esc":
			switch m.screen {
			case screenModel:
				m.screen = screenBackend
			case screenModelCustom:
				// openai/openrouter came from backend; local/cloud from model list
				if m.backend.id == "openai" || m.backend.id == "openrouter" {
					m.screen = screenBackend
				} else {
					m.screen = screenModel
				}
			case screenKey:
				m.screen = screenModelCustom
			case screenReview:
				if m.backend.needsKey {
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
	case screenModelCustom:
		var cmd tea.Cmd
		m.modelInput, cmd = m.modelInput.Update(msg)
		return m, cmd
	case screenKey:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
	return m, nil
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
			if m.backend.id == "openai" || m.backend.id == "openrouter" {
				m.modelInput.SetValue(m.backend.defaultModel)
				m.modelInput.Focus()
				m.screen = screenModelCustom
			} else {
				m.mlist = m.buildModelList()
				m.screen = screenModel
			}
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
			m.errMsg = "paste an API key first (esc to go back)"
			return m, nil
		}
		m.chosenKey = v
		m.screen = screenReview
	case screenReview:
		cfg := brainConfig{
			Backend: m.backend.id,
			Model:   m.chosenModel,
			APIKey:  m.chosenKey,
			APIURL:  m.backend.apiURL,
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
	case screenModelCustom:
		b.WriteString(theme.Header.Render("model for "+m.backend.name) + "\n")
		b.WriteString(theme.Dimmed.Render(m.backend.modelHint) + "\n\n")
		b.WriteString(m.modelInput.View() + "\n")
	case screenKey:
		b.WriteString(theme.Header.Render("API key for "+m.backend.name) + "\n")
		b.WriteString(theme.Dimmed.Render("stored in ~/.config/hey-lain/brain.json (mode 0600) — never leaves this machine except to "+m.backend.name) + "\n\n")
		b.WriteString(m.keyInput.View() + "\n")
	case screenReview:
		b.WriteString(theme.Header.Render("review") + "\n\n")
		b.WriteString(fmt.Sprintf("  backend  %s\n", theme.Normal.Render(m.backend.name)))
		b.WriteString(fmt.Sprintf("  model    %s\n", theme.Selected.Render(m.chosenModel)))
		if m.backend.needsKey {
			b.WriteString(fmt.Sprintf("  api key  %s\n", theme.Normal.Render("•••••••• (set)")))
		}
		b.WriteString("\n")
		if m.backend.id == "local" && !modelInstalled(m.chosenModel) {
			b.WriteString(theme.Error.Render("  ! "+m.chosenModel+" isn't installed — run: ollama pull "+m.chosenModel) + "\n\n")
		}
		b.WriteString(theme.Dimmed.Render("  enter to save   esc to go back") + "\n")
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
