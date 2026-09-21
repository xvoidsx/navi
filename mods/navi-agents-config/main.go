// Navi Agent Center — one home for everything agents on navi.
//
// Tabs: status (live herdr agents, jump to one), models (ollama service and
// model management), harnesses (installed harnesses, default agent),
// providers (API keys — the original navi-agents-config UI).
//
// Keys live in ~/.config/navi/agents.env (KEY=VALUE, mode 0600). Interactive
// shells pick it up through wired/bashrc, panel/rofi-launched apps through
// mod-open.sh, and Hey Lain's brain.sh reads it for any non-local backend.
// Nothing else stores keys; this app is the only writer.
// The default agent (used by navi-Q) lives in ~/.config/navi/default-agent.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	agentenv "github.com/rav3ndust/navi-agentenv"
	theme "github.com/rav3ndust/navi-theme"
)

// prog is the running bubbletea program, needed for the ReleaseTerminal /
// RestoreTerminal dance around privileged or full-screen child commands
// (doas prompts, ollama pull progress).
var prog *tea.Program

// ---------------------------------------------------------------------------
// tabs
// ---------------------------------------------------------------------------

type tab int

const (
	tabStatus tab = iota
	tabModels
	tabHarnesses
	tabProviders
)

var tabNames = []string{"status", "models", "harnesses", "providers"}

// provider screens (the original config UI, now the providers tab)
type pscreen int

const (
	pscreenList pscreen = iota
	pscreenDetail
	pscreenKey
	pscreenCustom
)

// ---------------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------------

type herdrPollMsg struct {
	agents  []herdrAgent
	errNote string
	heylain string
	ollama  bool
}

type modelsRefreshMsg struct {
	active  bool
	enabled bool
	models  []ollamaModel
	disk    string
	errNote string
}

type ollamaToggleDoneMsg struct {
	err   error
	start bool
}

type pullDoneMsg struct{ err error }

type deleteDoneMsg struct {
	err  error
	name string
}

type readDoneMsg struct {
	err  error
	name string
	text string
}

type testDoneMsg struct {
	ok     bool
	detail string
	lat    string
}

// ---------------------------------------------------------------------------
// herdr agents
// ---------------------------------------------------------------------------

type herdrAgent struct {
	Name   string
	Status string // idle | working | blocked | done | unknown
	Raw    map[string]any
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// listHerdrAgents parses `herdr agent list`. The server-down shape is JSON
// with an "error" key and exit code 0, so the body is parsed, not the exit.
func listHerdrAgents() ([]herdrAgent, string) {
	out, err := exec.Command("herdr", "agent", "list").Output()
	if err != nil {
		return nil, "herdr isn't installed or won't run"
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, "couldn't parse herdr output"
	}
	if e, ok := doc["error"]; ok && e != nil {
		return nil, "herdr server not running — run herdr to start it"
	}
	var raw []any
	if r, ok := doc["result"].(map[string]any); ok {
		raw, _ = r["agents"].([]any)
	}
	if raw == nil {
		raw, _ = doc["agents"].([]any)
	}
	var agents []herdrAgent
	for _, a := range raw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		agents = append(agents, herdrAgent{
			Name:   strField(am, "agent", "name", "id"),
			Status: strField(am, "agent_status", "status", "state"),
			Raw:    am,
		})
	}
	return agents, ""
}

// faceFor renders the little status face beside an agent — the same visual
// language as the waybar module: dim when quiet, cyan ready, green working,
// pink when it needs you.
func faceFor(status string) string {
	var c lipgloss.Color
	switch strings.ToLower(status) {
	case "working":
		c = theme.Green
	case "blocked":
		c = theme.Pink
	case "ready", "idle":
		c = theme.Cyan
	default:
		c = theme.Dim
	}
	return lipgloss.NewStyle().Foreground(c).Render("󰚩")
}

func agentStateLabel(status string) string {
	switch strings.ToLower(status) {
	case "working":
		return "working"
	case "blocked":
		return "needs you"
	case "ready":
		return "ready"
	case "idle":
		return "idle"
	case "done":
		return "done"
	default:
		return "unknown"
	}
}

// heyLainState reports Hey Lain's mic/processing state from her runtime dir.
func heyLainState() string {
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		rt = "/run/user/" + fmt.Sprint(os.Getuid())
	}
	dir := filepath.Join(rt, "hey-lain")
	if pid, err := os.ReadFile(filepath.Join(dir, "rec.pid")); err == nil {
		if _, err := os.Stat("/proc/" + strings.TrimSpace(string(pid))); err == nil {
			return "listening"
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "processing.lock")); err == nil {
		return "thinking"
	}
	return "quiet"
}

func ollamaReachable() bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:11434/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func pollHerdr() tea.Msg {
	agents, note := listHerdrAgents()
	return herdrPollMsg{agents: agents, errNote: note, heylain: heyLainState(), ollama: ollamaReachable()}
}

func tickHerdr() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return pollHerdr() })
}

// ---------------------------------------------------------------------------
// ollama + models
// ---------------------------------------------------------------------------

type ollamaModel struct {
	Name string
	Size string
}

func ollamaService() (active, enabled bool) {
	active = exec.Command("systemctl", "is-active", "--quiet", "ollama").Run() == nil
	enabled = exec.Command("systemctl", "is-enabled", "--quiet", "ollama").Run() == nil
	return active, enabled
}

func listOllamaModels() ([]ollamaModel, string) {
	out, err := exec.Command("ollama", "list").Output()
	if err != nil {
		return nil, "ollama list failed — is ollama running?"
	}
	var models []ollamaModel
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] == "NAME" {
			continue
		}
		models = append(models, ollamaModel{Name: f[0], Size: f[2] + " " + f[3]})
	}
	return models, ""
}

func ollamaDisk() string {
	dir := os.Getenv("OLLAMA_MODELS")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".ollama", "models")
	}
	out, err := exec.Command("du", "-sh", dir).Output()
	if err != nil {
		return ""
	}
	if f := strings.Fields(string(out)); len(f) > 0 {
		return f[0]
	}
	return ""
}

func refreshModels() tea.Msg {
	active, enabled := ollamaService()
	models, note := listOllamaModels()
	return modelsRefreshMsg{active: active, enabled: enabled, models: models, disk: ollamaDisk(), errNote: note}
}

// toggleOllamaCmd shells out to doas with the real terminal attached, so the
// password prompt works when the persist timestamp has expired.
func toggleOllamaCmd(start bool) tea.Cmd {
	return func() tea.Msg {
		if prog != nil {
			_ = prog.ReleaseTerminal()
			defer func() { _ = prog.RestoreTerminal() }()
		}
		action := "stop"
		if start {
			action = "start"
		}
		cmd := exec.Command("doas", "systemctl", action, "ollama")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		return ollamaToggleDoneMsg{err: err, start: start}
	}
}

func pullModelCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if prog != nil {
			_ = prog.ReleaseTerminal()
			defer func() { _ = prog.RestoreTerminal() }()
		}
		cmd := exec.Command("ollama", "pull", name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		return pullDoneMsg{err: err}
	}
}

func deleteModelCmd(name string) tea.Msg {
	out, err := exec.Command("ollama", "rm", name).CombinedOutput()
	if err != nil {
		return deleteDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(out))), name: name}
	}
	return deleteDoneMsg{name: name}
}

func readAgentCmd(name string) tea.Msg {
	out, err := exec.Command("herdr", "agent", "read", name, "--lines", "60", "--format", "text").CombinedOutput()
	if err != nil {
		return readDoneMsg{err: err, name: name, text: strings.TrimSpace(string(out))}
	}
	return readDoneMsg{name: name, text: string(out)}
}

// ---------------------------------------------------------------------------
// harnesses
// ---------------------------------------------------------------------------

type harness struct {
	Bin       string
	Installed bool
	Hint      string // where to get it when missing
}

var knownHarnesses = []struct{ bin, hint string }{
	{"opencode", "ships with navi"},
	{"omp", "ships with navi"},
	{"goose", "ships with navi"},
	{"pi", "naviApps → Agents shelf"},
	{"codex", "navi-extras"},
	{"crush", "naviApps → Agents shelf"},
	{"agy", "navi-extras"},
}

func defaultAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "navi", "default-agent")
}

func readDefaultAgent() string {
	b, err := os.ReadFile(defaultAgentPath())
	if err != nil {
		return "opencode"
	}
	for _, h := range knownHarnesses {
		if strings.TrimSpace(string(b)) == h.bin {
			return h.bin
		}
	}
	return "opencode"
}

func writeDefaultAgent(bin string) error {
	p := defaultAgentPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(bin+"\n"), 0o644)
}

func pollHarnesses() []harness {
	var out []harness
	for _, h := range knownHarnesses {
		_, err := exec.LookPath(h.bin)
		out = append(out, harness{Bin: h.bin, Installed: err == nil, Hint: h.hint})
	}
	return out
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type model struct {
	tab    tab
	width  int
	status string // footer status line

	// status tab
	agents      []herdrAgent
	agentsErr   string
	agentCursor int
	heylain     string
	ollamaUp    bool
	detailName  string
	detailVP    viewport.Model
	detailReady bool

	// models tab
	ollamaActive  bool
	ollamaEnabled bool
	ollamaModels  []ollamaModel
	modelsDisk    string
	modelsErr     string
	modelCursor   int
	pullInput     textinput.Model
	pulling       bool
	confirmDelete string

	// harnesses tab
	harnesses     []harness
	defaultAgent  string
	harnessCursor int

	// providers tab (the original config UI)
	pscreen   pscreen
	providers []agentenv.Provider
	fileVals  map[string]string
	plist     list.Model
	sel       agentenv.Provider
	keyInput  textinput.Model
	form      []textinput.Model
	formFocus int
	testing   bool

	jumpTarget string
}

const frameWidth = 64

type pItem struct {
	p      agentenv.Provider
	status string // precomputed description
}

func (i pItem) Title() string       { return i.p.Name }
func (i pItem) Description() string { return i.status }
func (i pItem) FilterValue() string { return i.p.Name + " " + i.p.EnvVar }

func initialModel() model {
	fileVals, _ := agentenv.LoadEnvFile(agentenv.EnvFilePath())
	custom, _ := agentenv.LoadCustom(agentenv.CustomFilePath())
	providers := append(append([]agentenv.Provider{}, agentenv.Providers()...), custom...)

	m := model{
		tab:          tabStatus,
		pscreen:      pscreenList,
		providers:    providers,
		fileVals:     fileVals,
		harnesses:    pollHarnesses(),
		defaultAgent: readDefaultAgent(),
		heylain:      "quiet",
	}
	m.plist = m.buildList()
	m.plist.Title = "providers — pick one"

	pi := textinput.New()
	pi.Prompt = "> "
	pi.Placeholder = "e.g. gemma3:270m"
	pi.CharLimit = 120
	m.pullInput = pi

	ki := textinput.New()
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Prompt = "> "
	ki.CharLimit = 256
	m.keyInput = ki
	return m
}

func (m *model) clampCursors() {
	if m.agentCursor >= len(m.agents) {
		m.agentCursor = max(0, len(m.agents)-1)
	}
	if m.modelCursor >= len(m.ollamaModels) {
		m.modelCursor = max(0, len(m.ollamaModels)-1)
	}
	if m.harnessCursor >= len(m.harnesses) {
		m.harnessCursor = max(0, len(m.harnesses)-1)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// providers tab — the original config UI, unchanged in behavior
// ---------------------------------------------------------------------------

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

func (m model) anyLive() bool {
	if m.anySet() || m.ollamaUp {
		return true
	}
	for _, a := range m.agents {
		if strings.ToLower(a.Status) == "working" || strings.ToLower(a.Status) == "blocked" {
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
	m.plist.Title = "providers — pick one"
	if sel >= 0 && sel < len(m.providers) {
		m.plist.Select(sel)
	}
}

func (m *model) saveStore() error {
	return agentenv.SaveEnvFile(agentenv.EnvFilePath(), m.fileVals)
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
// provider connection test
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
			return testDoneMsg{ok: false, detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
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

func (m model) Init() tea.Cmd {
	return tea.Batch(
		pollHerdr,
		tickHerdr(),
		func() tea.Msg { return refreshModels() },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.plist.SetSize(min(msg.Width-8, frameWidth-6), 12)
		if m.detailReady {
			m.detailVP.Width = min(msg.Width-8, frameWidth-2)
			m.detailVP.Height = 14
		}
		return m, nil

	case herdrPollMsg:
		m.agents = msg.agents
		m.agentsErr = msg.errNote
		m.heylain = msg.heylain
		m.ollamaUp = msg.ollama
		m.clampCursors()
		return m, tickHerdr()

	case modelsRefreshMsg:
		m.ollamaActive = msg.active
		m.ollamaEnabled = msg.enabled
		m.ollamaModels = msg.models
		m.modelsDisk = msg.disk
		m.modelsErr = msg.errNote
		m.clampCursors()
		return m, nil

	case ollamaToggleDoneMsg:
		if msg.err != nil {
			m.status = "ollama toggle failed: " + firstLine(msg.err.Error())
		} else if msg.start {
			m.status = "ollama started"
		} else {
			m.status = "ollama stopped"
		}
		return m, func() tea.Msg { return refreshModels() }

	case pullDoneMsg:
		m.pulling = false
		m.pullInput.Blur()
		m.pullInput.SetValue("")
		if msg.err != nil {
			m.status = "pull failed: " + firstLine(msg.err.Error())
		} else {
			m.status = "model pulled"
		}
		return m, func() tea.Msg { return refreshModels() }

	case deleteDoneMsg:
		m.confirmDelete = ""
		if msg.err != nil {
			m.status = "delete failed: " + firstLine(msg.err.Error())
		} else {
			m.status = msg.name + " deleted"
		}
		return m, func() tea.Msg { return refreshModels() }

	case readDoneMsg:
		if msg.err != nil {
			m.status = "couldn't read " + msg.name + ": " + firstLine(msg.err.Error())
			return m, nil
		}
		m.detailName = msg.name
		m.detailReady = true
		m.detailVP = viewport.New(min(max(m.width-8, 20), frameWidth-2), 14)
		m.detailVP.SetContent(msg.text)
		m.detailVP.GotoBottom()
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
	if m.tab == tabProviders {
		switch m.pscreen {
		case pscreenList:
			var cmd tea.Cmd
			m.plist, cmd = m.plist.Update(msg)
			return m, cmd
		case pscreenKey:
			var cmd tea.Cmd
			m.keyInput, cmd = m.keyInput.Update(msg)
			return m, cmd
		case pscreenCustom:
			return m.updateCustomForm(msg)
		}
	}
	if m.tab == tabModels && m.pulling {
		var cmd tea.Cmd
		m.pullInput, cmd = m.pullInput.Update(msg)
		return m, cmd
	}
	if m.detailReady {
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

// switchTab moves between tabs; models/harnesses refresh on entry.
func (m *model) switchTab(t tab) tea.Cmd {
	m.tab = t
	m.status = ""
	m.detailReady = false
	m.pulling = false
	m.confirmDelete = ""
	var cmds []tea.Cmd
	if t == tabModels {
		cmds = append(cmds, func() tea.Msg { return refreshModels() })
	}
	if t == tabHarnesses {
		m.harnesses = pollHarnesses()
		m.defaultAgent = readDefaultAgent()
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func tabKey(key string) (tab, bool) {
	switch key {
	case "1":
		return tabStatus, true
	case "2":
		return tabModels, true
	case "3":
		return tabHarnesses, true
	case "4":
		return tabProviders, true
	}
	return 0, false
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// detail overlay (agent output reader) captures its own keys
	if m.detailReady {
		switch key {
		case "esc", "q":
			m.detailReady = false
			return m, nil
		}
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}

	// pull input captures its own keys
	if m.tab == tabModels && m.pulling {
		switch key {
		case "esc":
			m.pulling = false
			m.pullInput.Blur()
			m.pullInput.SetValue("")
			m.status = ""
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.pullInput.Value())
			if name == "" {
				m.status = "name a model first"
				return m, nil
			}
			m.status = "pulling " + name + "…"
			return m, pullModelCmd(name)
		}
		var cmd tea.Cmd
		m.pullInput, cmd = m.pullInput.Update(msg)
		return m, cmd
	}

	// tab switching is global (except inside provider key/custom forms)
	editing := m.tab == tabProviders && (m.pscreen == pscreenKey || m.pscreen == pscreenCustom)
	if !editing {
		if t, ok := tabKey(key); ok {
			return m, m.switchTab(t)
		}
		if key == "tab" {
			return m, m.switchTab((m.tab + 1) % 4)
		}
		if key == "shift+tab" {
			return m, m.switchTab((m.tab + 3) % 4)
		}
	}
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.tab {
	case tabStatus:
		return m.handleStatusKey(key)
	case tabModels:
		return m.handleModelsKey(key)
	case tabHarnesses:
		return m.handleHarnessesKey(key)
	case tabProviders:
		return m.handleProviderKey(key, msg)
	}
	return m, nil
}

func (m model) handleStatusKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.agentCursor > 0 {
			m.agentCursor--
		}
		return m, nil
	case "down", "j":
		if m.agentCursor < len(m.agents)-1 {
			m.agentCursor++
		}
		return m, nil
	case "enter":
		if len(m.agents) == 0 {
			m.status = "no agents to jump to"
			return m, nil
		}
		m.jumpTarget = m.agents[m.agentCursor].Name
		return m, tea.Quit
	case "r", "R":
		if len(m.agents) == 0 {
			m.status = "no agents running"
			return m, nil
		}
		name := m.agents[m.agentCursor].Name
		m.status = "reading " + name + "…"
		return m, func() tea.Msg { return readAgentCmd(name) }
	}
	return m, nil
}

func (m model) handleModelsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.modelCursor > 0 {
			m.modelCursor--
		}
		m.confirmDelete = ""
		return m, nil
	case "down", "j":
		if m.modelCursor < len(m.ollamaModels)-1 {
			m.modelCursor++
		}
		m.confirmDelete = ""
		return m, nil
	case "r", "R":
		m.status = "refreshing…"
		return m, func() tea.Msg { return refreshModels() }
	case "o", "O":
		m.status = "talking to systemd…"
		return m, toggleOllamaCmd(!m.ollamaActive)
	case "p", "P":
		if !m.ollamaActive {
			m.status = "start ollama first (o)"
			return m, nil
		}
		m.pulling = true
		m.pullInput.SetValue("")
		m.pullInput.Focus()
		m.status = ""
		return m, textinput.Blink
	case "d", "D":
		if len(m.ollamaModels) == 0 {
			return m, nil
		}
		name := m.ollamaModels[m.modelCursor].Name
		if m.confirmDelete == name {
			m.status = "deleting " + name + "…"
			return m, func() tea.Msg { return deleteModelCmd(name) }
		}
		m.confirmDelete = name
		m.status = "press d again to delete " + name
		return m, nil
	}
	return m, nil
}

func (m model) handleHarnessesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.harnessCursor > 0 {
			m.harnessCursor--
		}
		return m, nil
	case "down", "j":
		if m.harnessCursor < len(m.harnesses)-1 {
			m.harnessCursor++
		}
		return m, nil
	case "enter":
		h := m.harnesses[m.harnessCursor]
		if !h.Installed {
			m.status = h.Bin + " isn't installed — get it via " + h.Hint
			return m, nil
		}
		if err := writeDefaultAgent(h.Bin); err != nil {
			m.status = "save failed: " + firstLine(err.Error())
			return m, nil
		}
		m.defaultAgent = h.Bin
		m.status = h.Bin + " is now the default agent (and navi-Q)"
		return m, nil
	}
	return m, nil
}

// handleProviderKey is the original config UI's key handling, now living on
// the providers tab.
func (m model) handleProviderKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.pscreen {
	case pscreenList:
		switch key {
		case "q", "esc":
			return m, tea.Quit
		case "enter":
			if it, ok := m.plist.SelectedItem().(pItem); ok {
				m.sel = it.p
				m.pscreen = pscreenDetail
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

	case pscreenDetail:
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			m.pscreen = pscreenList
			m.refreshList()
			m.status = ""
			return m, nil
		case "s", "S":
			m.keyInput.SetValue("")
			m.keyInput.Placeholder = "paste key" + keyHintSuffix(m.sel)
			m.keyInput.Focus()
			m.pscreen = pscreenKey
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
			m.pscreen = pscreenList
			m.refreshList()
			m.status = "custom provider removed"
			return m, nil
		}

	case pscreenKey:
		switch key {
		case "esc":
			m.pscreen = pscreenDetail
			m.keyInput.Blur()
			return m, nil
		case "enter":
			v := strings.TrimSpace(m.keyInput.Value())
			m.keyInput.Blur()
			if v == "" {
				m.status = "empty — nothing saved"
				m.pscreen = pscreenDetail
				return m, nil
			}
			m.fileVals[m.sel.EnvVar] = v
			if err := m.saveStore(); err != nil {
				m.status = "save failed: " + err.Error()
			} else {
				m.status = "key saved to agents.env (mode 0600)"
			}
			m.pscreen = pscreenDetail
			m.refreshList()
			return m, nil
		}
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd

	case pscreenCustom:
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
	m.pscreen = pscreenCustom
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
		m.pscreen = pscreenList
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
	m.pscreen = pscreenList
	m.refreshList()
	m.status = name + " added — select it and press s to set its key"
	return m, nil
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func (m model) tabBar() string {
	var parts []string
	for i, n := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if tab(i) == m.tab {
			parts = append(parts, theme.Selected.Render("["+label+"]"))
		} else {
			parts = append(parts, theme.Dimmed.Render(" "+label+" "))
		}
	}
	return strings.Join(parts, " ")
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.tabBar() + "\n\n")
	switch m.tab {
	case tabStatus:
		b.WriteString(m.statusView())
	case tabModels:
		b.WriteString(m.modelsView())
	case tabHarnesses:
		b.WriteString(m.harnessView())
	case tabProviders:
		b.WriteString(m.providerView())
	}
	if m.status != "" {
		b.WriteString("\n" + theme.Normal.Render(m.status))
	}
	if m.testing {
		b.WriteString("\n" + theme.Dimmed.Render("testing…"))
	}
	return theme.Frame(frameWidth, "navi agent center", m.anyLive(), "", b.String())
}

func (m model) statusView() string {
	var b strings.Builder
	if m.detailReady {
		b.WriteString(theme.Header.Render("output — "+m.detailName) + "\n")
		b.WriteString(m.detailVP.View() + "\n")
		b.WriteString(theme.Dimmed.Render("up/down scroll · esc back"))
		return b.String()
	}
	if m.agentsErr != "" {
		b.WriteString(theme.Dimmed.Render(m.agentsErr) + "\n\n")
	} else if len(m.agents) == 0 {
		b.WriteString(theme.Dimmed.Render("no agents running in herdr") + "\n\n")
	} else {
		b.WriteString(theme.Header.Render(fmt.Sprintf("agents (%d)", len(m.agents))) + "\n")
		for i, a := range m.agents {
			name := a.Name
			if name == "" {
				name = "(unnamed)"
			}
			if len(name) > 34 {
				name = name[:33] + "…"
			}
			row := fmt.Sprintf("%s  %-34s %s", faceFor(a.Status), name, agentStateLabel(a.Status))
			if i == m.agentCursor {
				b.WriteString(theme.Selected.Render("> "+row) + "\n")
			} else {
				b.WriteString("  "+theme.Normal.Render(row) + "\n")
			}
		}
		b.WriteString("\n")
	}
	// ambient lines: Hey Lain + ollama, same language as the panel
	hl := m.heylain
	hlStyle := theme.Dimmed
	if hl == "listening" || hl == "thinking" {
		hlStyle = lipgloss.NewStyle().Foreground(theme.Pink)
	}
	b.WriteString(theme.Dimmed.Render("hey lain  ") + hlStyle.Render(hl) + "\n")
	ol := "down"
	olStyle := theme.Dimmed
	if m.ollamaUp {
		ol, olStyle = "up", lipgloss.NewStyle().Foreground(theme.Green)
	}
	b.WriteString(theme.Dimmed.Render("ollama    ") + olStyle.Render(ol) + "\n\n")
	b.WriteString(theme.Dimmed.Render("enter jump to agent · r read output · 1-4 tabs · q quit"))
	return b.String()
}

func (m model) modelsView() string {
	var b strings.Builder
	if m.pulling {
		b.WriteString(theme.Header.Render("pull a model") + "\n")
		b.WriteString(theme.Dimmed.Render("any ollama model name, e.g. gemma3:270m") + "\n\n")
		b.WriteString(m.pullInput.View() + "\n")
		b.WriteString(theme.Dimmed.Render("enter pull · esc cancel"))
		return b.String()
	}
	st, stStyle := "down", theme.Dimmed
	if m.ollamaActive {
		st, stStyle = "up", lipgloss.NewStyle().Foreground(theme.Green)
	}
	en := ""
	if m.ollamaEnabled {
		en = " · starts at boot"
	}
	b.WriteString(theme.Header.Render("ollama") + "  " + stStyle.Render("● "+st) + theme.Dimmed.Render(en) + "\n\n")
	if m.modelsErr != "" {
		b.WriteString(theme.Dimmed.Render(m.modelsErr) + "\n\n")
	} else {
		title := fmt.Sprintf("models (%d)", len(m.ollamaModels))
		if m.modelsDisk != "" {
			title += " · " + m.modelsDisk + " on disk"
		}
		b.WriteString(theme.Header.Render(title) + "\n")
		if len(m.ollamaModels) == 0 {
			b.WriteString(theme.Dimmed.Render("nothing pulled yet — press p") + "\n")
		}
		for i, om := range m.ollamaModels {
			row := fmt.Sprintf("%-34s %s", om.Name, om.Size)
			if i == m.modelCursor {
				mark := "  "
				if m.confirmDelete == om.Name {
					mark = theme.Error.Render("! ")
				}
				b.WriteString(theme.Selected.Render(mark+row) + "\n")
			} else {
				b.WriteString("  " + theme.Normal.Render(row) + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(theme.Dimmed.Render("o on/off · p pull · d delete · r refresh · 1-4 tabs · q quit"))
	return b.String()
}

func (m model) harnessView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("harnesses") + "\n")
	for i, h := range m.harnesses {
		mark := "  "
		st := theme.Dimmed.Render("not installed · " + h.Hint)
		if h.Installed {
			st = lipgloss.NewStyle().Foreground(theme.Green).Render("installed")
		}
		if h.Bin == m.defaultAgent {
			mark = lipgloss.NewStyle().Foreground(theme.Pink).Render("★ ")
		}
		row := fmt.Sprintf("%s%-10s %s", mark, h.Bin, st)
		if i == m.harnessCursor {
			b.WriteString(theme.Selected.Render("> "+row) + "\n")
		} else {
			b.WriteString("  " + theme.Normal.Render(row) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(theme.Dimmed.Render("the ★ harness is the default agent — and what navi-Q (alt+a) opens") + "\n\n")
	b.WriteString(theme.Dimmed.Render("enter set default · 1-4 tabs · q quit"))
	return b.String()
}

func (m model) providerView() string {
	var b strings.Builder
	switch m.pscreen {
	case pscreenList:
		b.WriteString(m.plist.View() + "\n")
		b.WriteString(theme.Dimmed.Render("enter select · a add custom · i import from environment · 1-4 tabs · q quit"))
		if h := importHint(m.providers, m.fileVals); h != "" && m.status == "" {
			b.WriteString("\n" + theme.Dimmed.Render(h))
		}
	case pscreenDetail:
		b.WriteString(m.detailView())
	case pscreenKey:
		b.WriteString(theme.Header.Render("API key for "+m.sel.Name) + "\n")
		b.WriteString(theme.Dimmed.Render("stored in ~/.config/navi/agents.env (mode 0600) — never printed, never logged") + "\n\n")
		b.WriteString(m.keyInput.View() + "\n")
		b.WriteString(theme.Dimmed.Render("enter save · esc cancel"))
	case pscreenCustom:
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
	return b.String()
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
	m := initialModel()
	prog = tea.NewProgram(m, tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "navi-agents-config:", err)
		os.Exit(1)
	}
	// jump to agent: the floating terminal becomes that agent's herdr session
	if fm, ok := final.(model); ok && fm.jumpTarget != "" {
		if p, err := exec.LookPath("herdr"); err == nil {
			_ = syscall.Exec(p, []string{"herdr", "agent", "attach", fm.jumpTarget}, os.Environ())
		}
	}
}
