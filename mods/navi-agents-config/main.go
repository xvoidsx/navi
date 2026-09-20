// Navi Agent Configuration — one home for every AI provider API key.
//
// Keys live in ~/.config/navi/agents.env (KEY=VALUE, mode 0600). Interactive
// shells pick it up through wired/bashrc, panel/rofi-launched apps through
// mod-open.sh, and Hey Lain's brain.sh reads it for any non-local backend.
// Nothing else stores keys; this app is the only writer.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	agentenv "github.com/rav3ndust/navi-agentenv"
	theme "github.com/rav3ndust/navi-theme"
)

// ---------------------------------------------------------------------------
// screens + messages
// ---------------------------------------------------------------------------

type screen int

const (
	screenList screen = iota
	screenDetail
	screenKey
	screenCustom
)

type testDoneMsg struct {
	ok     bool
	detail string
	lat    string
}

// ---------------------------------------------------------------------------
// list items
// ---------------------------------------------------------------------------

type pItem struct {
	p      agentenv.Provider
	status string // precomputed description
}

func (i pItem) Title() string       { return i.p.Name }
func (i pItem) Description() string { return i.status }
func (i pItem) FilterValue() string { return i.p.Name + " " + i.p.EnvVar }

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type model struct {
	screen    screen
	providers []agentenv.Provider
	fileVals  map[string]string
	plist     list.Model
	sel       agentenv.Provider
	keyInput  textinput.Model
	form      []textinput.Model
	formFocus int
	status    string
	testing   bool
	width     int
}

const frameWidth = 64

func initialModel() model {
	fileVals, _ := agentenv.LoadEnvFile(agentenv.EnvFilePath())
	custom, _ := agentenv.LoadCustom(agentenv.CustomFilePath())
	providers := append(append([]agentenv.Provider{}, agentenv.Providers()...), custom...)

	m := model{
		screen:    screenList,
		providers: providers,
		fileVals:  fileVals,
	}
	m.plist = m.buildList()
	m.plist.Title = "navi agent configuration — pick a provider"
	m.status = importHint(providers, fileVals)

	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Prompt = "> "
	ki.CharLimit = 256
	m.keyInput = ki
	return m
}

// importHint nudges when keys exist in the process environment but not in
// the store yet (e.g. OLLAMA_API_KEY exported in a terminal).
func importHint(providers []agentenv.Provider, fileVals map[string]string) string {
	n := 0
	for _, p := range providers {
		if _, ok := fileVals[p.EnvVar]; !ok && os.Getenv(p.EnvVar) != "" {
			n++
		}
	}
	if n > 0 {
		return fmt.Sprintf("found %d key(s) in your environment — press i to import", n)
	}
	return ""
}

// keyOf resolves the effective key: the store wins, the live environment is
// the fallback (mod-open.sh and wired/bashrc source the store into it).
func (m model) keyOf(p agentenv.Provider) (val, source string) {
	if v, ok := m.fileVals[p.EnvVar]; ok && strings.TrimSpace(v) != "" {
		return v, "agents.env"
	}
	if v := strings.TrimSpace(os.Getenv(p.EnvVar)); v != "" {
		return v, "environment"
	}
	return "", ""
}

func (m model) anySet() bool {
	for _, p := range m.providers {
		if v, _ := m.keyOf(p); v != "" {
			return true
		}
	}
	return false
}

func (m model) buildList() list.Model {
	items := make([]list.Item, 0, len(m.providers))
	for _, p := range m.providers {
		v, src := m.keyOf(p)
		st := p.EnvVar + " · "
		if v != "" {
			st += "key set (" + src + ")"
		} else {
			st += "no key"
		}
		if p.Custom {
			st += " · custom"
		}
		items = append(items, pItem{p: p, status: st})
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	return l
}

func (m *model) refreshList() {
	sel := -1
	if it, ok := m.plist.SelectedItem().(pItem); ok {
		for i, p := range m.providers {
			if p.ID == it.p.ID {
				sel = i
				break
			}
		}
	}
	m.plist = m.buildList()
	m.plist.Title = "navi agent configuration — pick a provider"
	if sel >= 0 && sel < len(m.providers) {
		m.plist.Select(sel)
	}
}

func (m *model) saveStore() error {
	if err := agentenv.SaveEnvFile(agentenv.EnvFilePath(), m.fileVals); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// connection test
// ---------------------------------------------------------------------------

func testProvider(p agentenv.Provider, key string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		if strings.TrimSpace(p.ModelsURL) == "" {
			return testDoneMsg{ok: false, detail: "no test URL for this provider"}
		}
		method := p.TestMethod
		if method == "" {
			method = "GET"
		}
		var body io.Reader
		if p.TestBody != "" {
			body = strings.NewReader(p.TestBody)
		}
		req, err := http.NewRequest(method, p.ModelsURL, body)
		if err != nil {
			return testDoneMsg{ok: false, detail: "bad test URL"}
		}
		if p.TestBody != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		switch p.Auth {
		case agentenv.AuthBearer:
			req.Header.Set("Authorization", "Bearer "+key)
		case agentenv.AuthXAPIKey:
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
		case agentenv.AuthGoogKey:
			req.Header.Set("x-goog-api-key", key)
		}
		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Do(req)
		lat := time.Since(start).Round(100 * time.Millisecond).String()
		if err != nil {
			return testDoneMsg{ok: false, detail: "unreachable — " + firstLine(err.Error()), lat: lat}
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return testDoneMsg{ok: false, detail: fmt.Sprintf("rejected (HTTP %d) — key looks invalid", resp.StatusCode), lat: lat}
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return testDoneMsg{ok: true, detail: "key accepted", lat: lat}
		case p.TestSoftFail:
			// Probe answers with an error even for valid keys by design
			// (bogus-model chat probe): anything that isn't 401/403 means
			// the key passed authentication.
			return testDoneMsg{ok: true, detail: fmt.Sprintf("key accepted (probe HTTP %d)", resp.StatusCode), lat: lat}
		default:
			return testDoneMsg{ok: false, detail: fmt.Sprintf("HTTP %d", resp.StatusCode), lat: lat}
		}
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.plist.SetSize(min(msg.Width-8, frameWidth-6), 12)
		return m, nil
	case testDoneMsg:
		m.testing = false
		if msg.ok {
			m.status = "test ok · " + msg.detail + " · " + msg.lat
		} else {
			m.status = "test failed · " + msg.detail
			if msg.lat != "" {
				m.status += " · " + msg.lat
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// route to focused widget
	switch m.screen {
	case screenList:
		var cmd tea.Cmd
		m.plist, cmd = m.plist.Update(msg)
		return m, cmd
	case screenKey:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	case screenCustom:
		return m.updateCustomForm(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.screen {
	case screenList:
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			return m, tea.Quit
		case "enter":
			if it, ok := m.plist.SelectedItem().(pItem); ok {
				m.sel = it.p
				m.screen = screenDetail
				m.status = ""
			}
			return m, nil
		case "a", "A":
			m.startCustomForm()
			return m, nil
		case "i", "I":
			n := m.importFromEnv()
			if n == 0 {
				m.status = "nothing new in the environment to import"
			} else {
				m.status = fmt.Sprintf("imported %d key(s) from the environment", n)
			}
			m.refreshList()
			return m, nil
		}
		var cmd tea.Cmd
		m.plist, cmd = m.plist.Update(msg)
		return m, cmd

	case screenDetail:
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			m.screen = screenList
			m.refreshList()
			m.status = ""
			return m, nil
		case "s", "S":
			m.keyInput.SetValue("")
			m.keyInput.Placeholder = "paste key" + keyHintSuffix(m.sel)
			m.keyInput.Focus()
			m.screen = screenKey
			m.status = ""
			return m, textinput.Blink
		case "t", "T":
			v, _ := m.keyOf(m.sel)
			if v == "" {
				m.status = "no key set — press s to add one first"
				return m, nil
			}
			m.testing = true
			m.status = "testing…"
			return m, testProvider(m.sel, v)
		case "c", "C":
			if _, ok := m.fileVals[m.sel.EnvVar]; ok {
				delete(m.fileVals, m.sel.EnvVar)
				if err := m.saveStore(); err != nil {
					m.status = "clear failed: " + err.Error()
				} else {
					m.status = "key cleared from agents.env"
				}
				m.refreshList()
			} else {
				m.status = "no stored key to clear (it may come from the live environment)"
			}
			return m, nil
		case "d", "D":
			if !m.sel.Custom {
				m.status = "built-in providers can't be deleted — clear the key instead"
				return m, nil
			}
			m.deleteCustom(m.sel.ID)
			m.screen = screenList
			m.refreshList()
			m.status = "custom provider removed"
			return m, nil
		}

	case screenKey:
		switch key {
		case "esc":
			m.screen = screenDetail
			m.keyInput.Blur()
			return m, nil
		case "enter":
			v := strings.TrimSpace(m.keyInput.Value())
			m.keyInput.Blur()
			if v == "" {
				m.status = "empty — nothing saved"
				m.screen = screenDetail
				return m, nil
			}
			m.fileVals[m.sel.EnvVar] = v
			if err := m.saveStore(); err != nil {
				m.status = "save failed: " + err.Error()
			} else {
				m.status = "key saved to agents.env (mode 0600)"
			}
			m.screen = screenDetail
			m.refreshList()
			return m, nil
		}
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd

	case screenCustom:
		return m.updateCustomForm(msg)
	}
	return m, nil
}

func keyHintSuffix(p agentenv.Provider) string {
	if p.KeyHint != "" {
		return " (" + p.KeyHint + ")"
	}
	return ""
}

// importFromEnv pulls any known provider keys present in the process
// environment but missing from the store. Returns how many were imported.
func (m *model) importFromEnv() int {
	n := 0
	for _, p := range m.providers {
		if _, ok := m.fileVals[p.EnvVar]; ok {
			continue
		}
		if v := strings.TrimSpace(os.Getenv(p.EnvVar)); v != "" {
			m.fileVals[p.EnvVar] = v
			n++
		}
	}
	if n > 0 {
		_ = m.saveStore()
	}
	return n
}

func (m *model) deleteCustom(id string) {
	kept := m.providers[:0]
	for _, p := range m.providers {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	m.providers = kept
	var customs []agentenv.Provider
	for _, p := range m.providers {
		if p.Custom {
			customs = append(customs, p)
		}
	}
	_ = agentenv.SaveCustom(agentenv.CustomFilePath(), customs)
	delete(m.fileVals, m.sel.EnvVar)
	_ = m.saveStore()
}

// ---------------------------------------------------------------------------
// custom provider form
// ---------------------------------------------------------------------------

func (m *model) startCustomForm() {
	m.form = make([]textinput.Model, 4)
	labels := []string{"display name", "env var name", "models test URL (optional)", "auth style: bearer | x-api-key | x-goog-api-key"}
	placeholders := []string{"e.g. My Provider", "e.g. MY_PROVIDER_API_KEY", "https://…/v1/models", "bearer"}
	for i := range m.form {
		ti := textinput.New()
		ti.Prompt = labels[i] + "\n> "
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 160
		if i == 3 {
			ti.SetValue("bearer")
		}
		m.form[i] = ti
	}
	m.formFocus = 0
	m.form[0].Focus()
	m.screen = screenCustom
	m.status = ""
}

func (m model) updateCustomForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.form[m.formFocus], cmd = m.form[m.formFocus].Update(msg)
		return m, cmd
	}
	switch kmsg.String() {
	case "esc":
		m.screen = screenList
		return m, nil
	case "tab", "shift+tab", "up", "down":
		m.form[m.formFocus].Blur()
		if kmsg.String() == "up" || kmsg.String() == "shift+tab" {
			m.formFocus = (m.formFocus + len(m.form) - 1) % len(m.form)
		} else {
			m.formFocus = (m.formFocus + 1) % len(m.form)
		}
		m.form[m.formFocus].Focus()
		return m, textinput.Blink
	case "enter":
		if m.formFocus < len(m.form)-1 {
			m.form[m.formFocus].Blur()
			m.formFocus++
			m.form[m.formFocus].Focus()
			return m, textinput.Blink
		}
		return m.saveCustomForm()
	}
	var cmd tea.Cmd
	m.form[m.formFocus], cmd = m.form[m.formFocus].Update(msg)
	return m, cmd
}

func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (i > 0 && r >= 'a' && r <= 'z')
		if !ok {
			return false
		}
	}
	return true
}

func (m *model) saveCustomForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.form[0].Value())
	envVar := strings.TrimSpace(m.form[1].Value())
	modelsURL := strings.TrimSpace(m.form[2].Value())
	authStr := strings.ToLower(strings.TrimSpace(m.form[3].Value()))

	if name == "" {
		m.status = "give the provider a name"
		return m, nil
	}
	if !validEnvName(envVar) {
		m.status = "env var name looks wrong — e.g. MY_PROVIDER_API_KEY"
		return m, nil
	}
	for _, p := range m.providers {
		if strings.EqualFold(p.EnvVar, envVar) {
			m.status = envVar + " is already used by " + p.Name
			return m, nil
		}
	}
	var auth agentenv.AuthStyle
	switch authStr {
	case "bearer", "":
		auth = agentenv.AuthBearer
	case "x-api-key":
		auth = agentenv.AuthXAPIKey
	case "x-goog-api-key":
		auth = agentenv.AuthGoogKey
	default:
		m.status = "auth style must be bearer, x-api-key, or x-goog-api-key"
		return m, nil
	}
	id := "custom-" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, " ", "-"), "_", "-"))
	p := agentenv.Provider{ID: id, Name: name, EnvVar: envVar, ModelsURL: modelsURL, Auth: auth, Custom: true}
	m.providers = append(m.providers, p)
	var customs []agentenv.Provider
	for _, cp := range m.providers {
		if cp.Custom {
			customs = append(customs, cp)
		}
	}
	if err := agentenv.SaveCustom(agentenv.CustomFilePath(), customs); err != nil {
		m.status = "save failed: " + err.Error()
		return m, nil
	}
	m.screen = screenList
	m.refreshList()
	m.status = name + " added — select it and press s to set its key"
	return m, nil
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func (m model) View() string {
	var b strings.Builder
	switch m.screen {
	case screenList:
		b.WriteString(m.plist.View() + "\n")
		b.WriteString(theme.Dimmed.Render("enter select · a add custom · i import from environment · q quit"))
	case screenDetail:
		b.WriteString(m.detailView())
	case screenKey:
		b.WriteString(theme.Header.Render("API key for "+m.sel.Name) + "\n")
		b.WriteString(theme.Dimmed.Render("stored in ~/.config/navi/agents.env (mode 0600) — never printed, never logged") + "\n\n")
		b.WriteString(m.keyInput.View() + "\n")
		b.WriteString(theme.Dimmed.Render("enter save · esc cancel"))
	case screenCustom:
		b.WriteString(theme.Header.Render("add a custom provider") + "\n\n")
		for i, ti := range m.form {
			if i == m.formFocus {
				b.WriteString(theme.Selected.Render(ti.View()) + "\n\n")
			} else {
				b.WriteString(ti.View() + "\n\n")
			}
		}
		b.WriteString(theme.Dimmed.Render("tab move · enter next/save · esc cancel"))
	}
	if m.status != "" {
		b.WriteString("\n" + theme.Normal.Render(m.status))
	}
	if m.testing {
		b.WriteString("\n" + theme.Dimmed.Render("testing…"))
	}
	return theme.Frame(frameWidth, "navi agent configuration", m.anySet(), "", b.String())
}

func (m model) detailView() string {
	var b strings.Builder
	p := m.sel
	b.WriteString(theme.Header.Render(p.Name) + "\n")
	b.WriteString(theme.Dimmed.Render("env var  "+p.EnvVar) + "\n")
	if p.KeyHint != "" {
		b.WriteString(theme.Dimmed.Render("format   "+p.KeyHint) + "\n")
	}
	v, src := m.keyOf(p)
	if v != "" {
		b.WriteString(theme.Normal.Render("key      "+agentenv.Masked(v)) + " " + theme.Dimmed.Render("("+src+")") + "\n")
	} else {
		b.WriteString(theme.Dimmed.Render("key      not set") + "\n")
	}
	b.WriteString("\n")
	keys := "s set/update key · t test connection · c clear key"
	if p.Custom {
		keys += " · d delete provider"
	}
	b.WriteString(theme.Dimmed.Render(keys + " · esc back"))
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-agents-config:", err)
		os.Exit(1)
	}
}
