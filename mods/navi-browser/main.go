// navi-browser — choose navi's default Chromium browser.
//
// navi can be opinionated about the integration layer without pretending
// there is one correct browser for everyone. This picker detects the
// installed Chromium-family browsers, runs a launch smoke test, and
// records the choice in ~/.config/navi/default-browser. Webapp launchers
// resolve that setting at launch time (via navi-browser-run) instead of
// hardcoding chromium — pick Brave and every webapp runs under
// brave-browser --app.
//
// Switching the binary alone would silently drop navi's managed policy
// (theme, blackice, Proton Pass), so the picker also deploys navi.json to
// the chosen browser's managed-policy directory. Policy directories differ
// across Chromium forks, so the picker probes each browser binary for its
// compiled-in policy path and falls back to the known table when the probe
// finds nothing.
//
// CLI (non-interactive):
//
//	navi-browser --list               show known browsers and state
//	navi-browser --current            print the current default id
//	navi-browser --set <id>           set the default browser
//	navi-browser --print-binary       print the resolved browser binary
//	navi-browser --deploy-policy [id] deploy navi policy (default: current)
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const frameWidth = 68

// ---------------------------------------------------------------------------
// browser table
// ---------------------------------------------------------------------------

type browserDef struct {
	ID        string   // config id, written to ~/.config/navi/default-browser
	Name      string   // display name
	Blurb     string   // one-liner for the picker
	Binaries  []string // detection candidates, in preference order
	PolicyDir string   // managed-policy dir fallback (probe may refine)
	ExtrasID  string   // navi-extras id for installing when missing ("" = n/a)
}

var browserTable = []browserDef{
	{
		ID: "chromium", Name: "Chromium",
		Blurb:     "Debian's Chromium — the default baseline.",
		Binaries:  []string{"chromium", "chromium-browser"},
		PolicyDir: "/etc/chromium/policies/managed",
		ExtrasID:  "",
	},
	{
		ID: "helium", Name: "Helium",
		Blurb:     "ungoogled-chromium, productized — the likely accela base.",
		Binaries:  []string{"helium", "helium-browser"},
		PolicyDir: "/etc/chromium/policies/managed",
		ExtrasID:  "helium",
	},
	{
		ID: "edge", Name: "Microsoft Edge",
		Blurb:     "Granular performance controls; the telemetry tradeoff is stated, not hidden.",
		Binaries:  []string{"microsoft-edge-stable", "microsoft-edge", "microsoft-edge-dev", "microsoft-edge-beta"},
		PolicyDir: "/etc/opt/edge/policies/managed",
		ExtrasID:  "edge",
	},
	{
		ID: "brave", Name: "Brave",
		Blurb:     "Privacy as the product — Shields, fingerprinting resistance, no account.",
		Binaries:  []string{"brave-browser-nightly", "brave-browser", "brave-browser-beta"},
		PolicyDir: "/etc/brave/policies/managed",
		ExtrasID:  "brave-nightly",
	},
	{
		ID: "brave-origin", Name: "Brave Origin",
		Blurb:     "Brave stripped to its essentials — Shields and speed, none of the crypto/AI/VPN extras.",
		Binaries:  []string{"brave-origin-stable", "brave-origin"},
		PolicyDir: "/etc/brave/policies/managed",
		ExtrasID:  "brave-origin",
	},
	{
		ID: "chrome", Name: "Google Chrome",
		Blurb:     "The reference build — stock Chromium plus Google's services.",
		Binaries:  []string{"google-chrome-stable", "google-chrome"},
		PolicyDir: "/etc/opt/chrome/policies/managed",
		ExtrasID:  "chrome",
	},
	{
		ID: "yandex", Name: "Yandex Browser",
		Blurb:     "The new arrival — its own take on the Chromium experience.",
		Binaries:  []string{"yandex-browser"},
		PolicyDir: "/etc/yandex-browser/policies/managed",
		ExtrasID:  "yandex",
	},
}

func findDef(id string) *browserDef {
	for i := range browserTable {
		if browserTable[i].ID == id {
			return &browserTable[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// config: ~/.config/navi/default-browser
// ---------------------------------------------------------------------------

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "navi", "default-browser")
}

func readDefault() string {
	p := configPath()
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if findDef(id) == nil {
		return ""
	}
	return id
}

func writeDefault(id string) error {
	if findDef(id) == nil {
		return fmt.Errorf("unknown browser id %q", id)
	}
	p := configPath()
	if p == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(id+"\n"), 0o600)
}

// ---------------------------------------------------------------------------
// system default browser (xdg-settings)
// ---------------------------------------------------------------------------

// applicationsDirs returns the .desktop search dirs in preference order.
func applicationsDirs() []string {
	home, err := os.UserHomeDir()
	dirs := []string{}
	if err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "applications"))
	}
	dirs = append(dirs, "/usr/share/applications")
	return dirs
}

// resolveDesktopFile finds the .desktop file whose Exec line launches
// binPath. It matches on the binary basename so channel variants
// (brave-browser-nightly, microsoft-edge-beta) resolve to their own
// launcher. Returns "" when nothing matches.
func resolveDesktopFile(binPath string, dirs []string) string {
	base := filepath.Base(binPath)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "Exec=") {
					continue
				}
				fields := strings.Fields(strings.TrimPrefix(line, "Exec="))
				if len(fields) == 0 {
					continue
				}
				prog := strings.Trim(fields[0], `"'`)
				if prog == binPath || filepath.Base(prog) == base {
					return e.Name()
				}
			}
		}
	}
	return ""
}

// setSystemDefault makes desktop (a .desktop file name, e.g.
// "microsoft-edge.desktop") the system default browser via xdg-settings,
// so link handlers and login flows follow the chosen runtime.
func setSystemDefault(desktop string) error {
	if _, err := exec.LookPath("xdg-settings"); err != nil {
		return fmt.Errorf("xdg-settings not found")
	}
	cmd := exec.Command("xdg-settings", "set", "default-web-browser", desktop)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("xdg-settings: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// xdgCurrentDefault reports the current system default browser's .desktop
// file, or "" when unset or unknown.
func xdgCurrentDefault() string {
	if _, err := exec.LookPath("xdg-settings"); err != nil {
		return ""
	}
	out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// detection
// ---------------------------------------------------------------------------

// firstBinary returns the first candidate found on PATH.
func firstBinary(def *browserDef) string {
	for _, name := range def.Binaries {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// policyPathRe finds a compiled-in "/etc/<product>/policies" path inside a
// browser binary.
var policyPathRe = regexp.MustCompile(`/etc/[A-Za-z0-9_.\-]+/policies`)

// discoverPolicyDir probes the browser binary for its compiled-in policy
// path. Returns the managed dir and whether it was probed (true) or is the
// table fallback (false).
func discoverPolicyDir(binPath, fallback string) (string, bool) {
	if binPath == "" {
		return fallback, false
	}
	const maxScan = 512 << 20
	data, err := os.ReadFile(binPath)
	if err != nil || len(data) > maxScan {
		return fallback, false
	}
	if m := policyPathRe.Find(data); m != nil {
		return string(m) + "/managed", true
	}
	return fallback, false
}

// probeAppMode launches the browser headless and renders a data: URL,
// proving the binary actually runs. All Chromium-family browsers support
// --app; a binary that renders headless supports the webapp runtime.
func probeAppMode(binPath string) bool {
	if binPath == "" {
		return false
	}
	for _, headless := range []string{"--headless=new", "--headless"} {
		ctx, cancel := context.WithTimeout(context.Background(), 12_000_000_000)
		cmd := exec.CommandContext(ctx, binPath,
			headless, "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
			"--dump-dom", "data:text/html,<title>naviprobe</title>naviprobe")
		out, err := cmd.Output()
		cancel()
		if err == nil && strings.Contains(string(out), "naviprobe") {
			return true
		}
	}
	return false
}

// resolveBinary maps the configured default (with fallbacks) to an actual
// executable. Returns the browser id and the binary path or bare name.
func resolveBinary() (string, string) {
	if id := readDefault(); id != "" {
		if def := findDef(id); def != nil {
			if bin := firstBinary(def); bin != "" {
				return id, bin
			}
		}
	}
	if def := findDef("chromium"); def != nil {
		if bin := firstBinary(def); bin != "" {
			return "chromium", bin
		}
	}
	for i := range browserTable {
		if bin := firstBinary(&browserTable[i]); bin != "" {
			return browserTable[i].ID, bin
		}
	}
	return "", "chromium"
}

// policySrc locates navi's managed-policy source file.
func policySrc() string {
	candidates := []string{
		"/usr/share/navi/wired/chromium/policies/managed/navi.json",
		"/opt/navi-iso/wired/chromium/policies/managed/navi.json",
		"wired/chromium/policies/managed/navi.json",
	}
	if env := os.Getenv("NAVI_POLICY_SRC"); env != "" {
		candidates = append([]string{env}, candidates...)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// shellQuote wraps s for safe embedding in sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// deployPolicyShell builds the shell command that installs navi.json into
// a browser's managed-policy directory (needs doas).
func deployPolicyShell(policyDir string) (string, error) {
	src := policySrc()
	if src == "" {
		return "", fmt.Errorf("navi policy source not found")
	}
	if policyDir == "" {
		return "", fmt.Errorf("no policy directory for this browser")
	}
	return fmt.Sprintf("doas install -d -m 0755 %s && doas install -m 644 %s %s",
		shellQuote(policyDir), shellQuote(src), shellQuote(filepath.Join(policyDir, "navi.json"))), nil
}

// ---------------------------------------------------------------------------
// TUI state
// ---------------------------------------------------------------------------

const (
	probePending = iota
	probeOK
	probeFail
)

type browserState struct {
	def       *browserDef
	installed bool
	binPath   string
	policyDir string
	probed    bool // policy dir came from the binary probe
	probe     int
}

type screen int

const (
	screenBrowse screen = iota
	screenConfirm
	screenResult
)

type confirmKind int

const (
	confirmSetDefault confirmKind = iota
	confirmDeployPolicy
	confirmInstall
)

type model struct {
	states        []*browserState
	cursor        int
	current       string
	screen        screen
	ckind         confirmKind
	ctarget       *browserState
	status        string
	ok            bool
	pendingPolicy *browserState // after set-default: offer policy deploy
	pendingXdg    *browserState // after set-default: offer system-default flip
	width, height int
}

type probeDoneMsg struct {
	idx int
	ok  bool
}

type runDoneMsg struct {
	action string
	name   string
	err    error
	kind   confirmKind
}

var prog *tea.Program

func newModel() *model {
	m := &model{current: readDefault()}
	for i := range browserTable {
		def := &browserTable[i]
		st := &browserState{def: def, probe: probePending}
		if bin := firstBinary(def); bin != "" {
			st.installed = true
			st.binPath = bin
			st.policyDir, st.probed = discoverPolicyDir(bin, def.PolicyDir)
		} else {
			st.policyDir = def.PolicyDir
		}
		m.states = append(m.states, st)
	}
	return m
}

func (m *model) selected() *browserState {
	if len(m.states) == 0 {
		return nil
	}
	return m.states[m.cursor]
}

// runShellCmd executes a shell command with the real terminal attached so
// doas prompts and installer progress render live.
func runShellCmd(command, action, name string, kind confirmKind) tea.Cmd {
	return func() tea.Msg {
		if prog != nil {
			_ = prog.ReleaseTerminal()
			defer func() { _ = prog.RestoreTerminal() }()
		}
		fmt.Printf("\n  running: %s\n\n", command)
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		return runDoneMsg{action: action, name: name, err: err, kind: kind}
	}
}

func probeCmd(idx int, binPath string) tea.Cmd {
	return func() tea.Msg {
		return probeDoneMsg{idx: idx, ok: probeAppMode(binPath)}
	}
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for i, st := range m.states {
		if st.installed {
			cmds = append(cmds, probeCmd(i, st.binPath))
		}
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case probeDoneMsg:
		if msg.idx >= 0 && msg.idx < len(m.states) {
			if msg.ok {
				m.states[msg.idx].probe = probeOK
			} else {
				m.states[msg.idx].probe = probeFail
			}
		}
		return m, nil

	case runDoneMsg:
		m.ok = msg.err == nil
		switch msg.kind {
		case confirmInstall:
			// re-detect: installers may have added a browser
			fresh := newModel()
			m.states, m.current, m.cursor = fresh.states, fresh.current, fresh.cursor
			var cmds []tea.Cmd
			for i, st := range m.states {
				if st.installed {
					cmds = append(cmds, probeCmd(i, st.binPath))
				}
			}
			if msg.err == nil {
				m.status = fmt.Sprintf("%s %s.", msg.action, msg.name)
			} else {
				m.status = fmt.Sprintf("%s failed — see the output above.", msg.action)
			}
			m.screen = screenResult
			m.pendingPolicy = nil
			return m, tea.Batch(cmds...)
		default:
			if msg.err == nil {
				m.status = fmt.Sprintf("%s %s.", msg.action, msg.name)
			} else {
				m.status = fmt.Sprintf("%s failed — see the output above.", msg.action)
			}
			m.screen = screenResult
			return m, nil
		}

	case tea.KeyMsg:
		switch m.screen {
		case screenBrowse:
			return m.updateBrowse(msg)
		case screenConfirm:
			return m.updateConfirm(msg)
		case screenResult:
			return m.updateResult(msg)
		}
	}
	return m, nil
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(m.states)-1 {
			m.cursor++
		}
	case tea.KeyEnter:
		st := m.selected()
		if st == nil || !st.installed {
			return m, nil
		}
		m.ckind = confirmSetDefault
		m.ctarget = st
		m.screen = screenConfirm
		return m, nil
	case tea.KeyEsc:
		return m, tea.Quit
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "j":
		if m.cursor < len(m.states)-1 {
			m.cursor++
		}
	case "p":
		st := m.selected()
		if st == nil || !st.installed {
			return m, nil
		}
		m.ckind = confirmDeployPolicy
		m.ctarget = st
		m.screen = screenConfirm
		return m, nil
	case "i":
		st := m.selected()
		if st == nil || st.installed || st.def.ExtrasID == "" {
			return m, nil
		}
		m.ckind = confirmInstall
		m.ctarget = st
		m.screen = screenConfirm
		return m, nil
	case "r":
		var cmds []tea.Cmd
		for i, st := range m.states {
			if st.installed {
				st.probe = probePending
				cmds = append(cmds, probeCmd(i, st.binPath))
			}
		}
		m.status = "re-probing launch smoke test…"
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.screen = screenBrowse
		m.ctarget = nil
		return m, nil
	case tea.KeyEnter:
		st := m.ctarget
		if st == nil {
			m.screen = screenBrowse
			return m, nil
		}
		switch m.ckind {
		case confirmSetDefault:
			if err := writeDefault(st.def.ID); err != nil {
				m.status = fmt.Sprintf("could not save default: %v", err)
				m.ok = false
				m.screen = screenResult
				m.pendingPolicy = nil
				return m, nil
			}
			m.current = st.def.ID
			m.status = fmt.Sprintf("default browser is now %s — webapps will launch under %s.",
				st.def.Name, filepath.Base(st.binPath))
			m.ok = true
			m.screen = screenResult
			m.pendingPolicy = st
			m.pendingXdg = st
			return m, nil
		case confirmDeployPolicy:
			command, err := deployPolicyShell(st.policyDir)
			if err != nil {
				m.status = fmt.Sprintf("cannot deploy policy: %v", err)
				m.ok = false
				m.screen = screenResult
				return m, nil
			}
			m.pendingPolicy = nil
			return m, runShellCmd(command, "navi policy deployed to", st.def.Name, confirmDeployPolicy)
		case confirmInstall:
			command := fmt.Sprintf("navi-extras --install %s", shellQuote(st.def.ExtrasID))
			m.pendingPolicy = nil
			return m, runShellCmd(command, "installed", st.def.Name, confirmInstall)
		}
	}
	if msg.String() == "q" {
		m.screen = screenBrowse
		m.ctarget = nil
	}
	return m, nil
}

func (m model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// after a set-default, "p" offers the policy deploy follow-up
	if m.pendingPolicy != nil && msg.String() == "p" {
		st := m.pendingPolicy
		m.pendingPolicy = nil
		command, err := deployPolicyShell(st.policyDir)
		if err != nil {
			m.status = fmt.Sprintf("cannot deploy policy: %v", err)
			m.ok = false
			return m, nil
		}
		return m, runShellCmd(command, "navi policy deployed to", st.def.Name, confirmDeployPolicy)
	}

	// after a set-default, "d" offers the system-default flip. Opt-in: the
	// runtime choice alone never changes the xdg default.
	if m.pendingXdg != nil && msg.String() == "d" {
		st := m.pendingXdg
		m.pendingXdg = nil
		desktop := resolveDesktopFile(st.binPath, applicationsDirs())
		switch {
		case desktop == "":
			m.status = fmt.Sprintf("no .desktop launcher found for %s — system default unchanged.", st.def.Name)
			m.ok = false
		case desktop == xdgCurrentDefault():
			m.status = fmt.Sprintf("%s is already the system default browser.", st.def.Name)
			m.ok = true
		default:
			if err := setSystemDefault(desktop); err != nil {
				m.status = fmt.Sprintf("could not set system default: %v", err)
				m.ok = false
			} else {
				m.status = fmt.Sprintf("%s is now the system default browser too — link handlers and login flows follow it.", st.def.Name)
				m.ok = true
			}
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter, tea.KeyEsc:
		m.screen = screenBrowse
		m.status = ""
		m.pendingPolicy = nil
		m.pendingXdg = nil
		return m, nil
	}
	if msg.String() == "q" {
		return m, tea.Quit
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (m model) browseView() string {
	var b strings.Builder
	nInstalled := 0
	for _, st := range m.states {
		if st.installed {
			nInstalled++
		}
	}
	b.WriteString(theme.Header.Render(fmt.Sprintf("BROWSER RUNTIME  ·  %d of %d installed", nInstalled, len(m.states))))
	b.WriteString("\n")
	currentName := "none — Chromium is the fallback"
	if def := findDef(m.current); def != nil {
		currentName = def.Name
	}
	b.WriteString(theme.Dimmed.Render("  default: " + currentName))
	b.WriteString("\n")
	b.WriteString(theme.Divider(frameWidth - 4))
	b.WriteString("\n")

	for i, st := range m.states {
		cursor := "  "
		name := theme.Normal.Render(st.def.Name)
		if i == m.cursor {
			cursor = theme.Selected.Render("▸ ")
			name = theme.Selected.Render(st.def.Name)
		}
		dot := theme.DotOff.Render("●")
		state := "not installed"
		if st.installed {
			dot = theme.DotOn.Render("●")
			state = filepath.Base(st.binPath)
		}
		tags := ""
		if st.def.ID == m.current && st.installed {
			tags = theme.Selected.Render("  ◂ default")
		}
		// row: cursor + dot + name + binary/state + default tag
		// (parts are rendered separately, never nested — see the lipgloss
		// nesting rule in the navi mods handbook)
		b.WriteString(cursor)
		b.WriteString(dot)
		b.WriteString(" ")
		b.WriteString(name)
		b.WriteString(theme.Dimmed.Render("  " + state))
		b.WriteString(tags)
		b.WriteString("\n")
		probe := theme.Dimmed.Render("probing…")
		switch {
		case !st.installed:
			probe = theme.Dimmed.Render("—")
		case st.probe == probeOK:
			probe = theme.DotOn.Render("launch ok")
		case st.probe == probeFail:
			probe = theme.DotOff.Render("launch failed")
		}
		b.WriteString("    ")
		b.WriteString(theme.Dimmed.Render(trunc(st.def.Blurb, frameWidth-14)))
		b.WriteString("  ")
		b.WriteString(probe)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.status != "" {
		if m.ok {
			b.WriteString(theme.Grayed.Render("  " + m.status))
		} else {
			b.WriteString(theme.Error.Render("  " + m.status))
		}
		b.WriteString("\n")
	}
	st := m.selected()
	var keys [][2]string
	keys = append(keys, [2]string{"↑↓", "move"}, [2]string{"enter", "set default"})
	if st != nil && st.installed {
		keys = append(keys, [2]string{"p", "deploy policy"})
	}
	if st != nil && !st.installed && st.def.ExtrasID != "" {
		keys = append(keys, [2]string{"i", "install via extras"})
	}
	keys = append(keys, [2]string{"r", "re-probe"}, [2]string{"q", "quit"})
	b.WriteString(theme.Footer(true, keys...))
	return b.String()
}

func (m model) confirmView() string {
	var b strings.Builder
	st := m.ctarget
	b.WriteString(theme.Header.Render("CONFIRM"))
	b.WriteString("\n\n")
	switch m.ckind {
	case confirmSetDefault:
		b.WriteString(theme.Normal.Render(fmt.Sprintf("  Set %s as the default browser?", st.def.Name)))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  Webapps will launch under %s --app.", filepath.Base(st.binPath))))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render("  Saved to ~/.config/navi/default-browser."))
		b.WriteString("\n")
	case confirmDeployPolicy:
		b.WriteString(theme.Normal.Render(fmt.Sprintf("  Deploy navi's managed policy to %s?", st.def.Name)))
		b.WriteString("\n")
		src := policySrc()
		if src == "" {
			src = "(policy source not found)"
		}
		b.WriteString(theme.Dimmed.Render("  " + src))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render("  → " + st.policyDir + "/navi.json"))
		b.WriteString("\n")
		note := "policy path from browser table"
		if st.probed {
			note = "policy path probed from the browser binary"
		}
		b.WriteString(theme.Dimmed.Render("  " + note + " — needs doas."))
		b.WriteString("\n")
	case confirmInstall:
		b.WriteString(theme.Normal.Render(fmt.Sprintf("  Install %s via navi-extras?", st.def.Name)))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  Runs: navi-extras --install %s", st.def.ExtrasID)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(theme.Footer(true,
		[2]string{"enter", "confirm"},
		[2]string{"esc", "cancel"},
	))
	return b.String()
}

func (m model) resultView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("DONE"))
	b.WriteString("\n\n")
	if m.ok {
		b.WriteString(theme.DotOn.Render("  ● "))
	} else {
		b.WriteString(theme.DotOff.Render("  ● "))
	}
	b.WriteString(theme.Normal.Render(m.status))
	b.WriteString("\n\n")
	var keys [][2]string
	if m.pendingPolicy != nil {
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  %s is the default but doesn't have navi's policy yet.", m.pendingPolicy.def.Name)))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render("  Without it, webapps lose the theme, blackice, and Proton Pass."))
		b.WriteString("\n\n")
		keys = append(keys, [2]string{"p", "deploy policy now"})
	}
	if m.pendingXdg != nil {
		b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  Make %s the system default browser too?", m.pendingXdg.def.Name)))
		b.WriteString("\n")
		b.WriteString(theme.Dimmed.Render("  Link handlers and login flows (like GitHub auth) follow the system default."))
		b.WriteString("\n\n")
		keys = append(keys, [2]string{"d", "set as system default"})
	}
	keys = append(keys, [2]string{"enter", "back"}, [2]string{"q", "quit"})
	b.WriteString(theme.Footer(true, keys...))
	return b.String()
}

func (m model) View() string {
	var b strings.Builder
	switch m.screen {
	case screenConfirm:
		b.WriteString(m.confirmView())
	case screenResult:
		b.WriteString(m.resultView())
	default:
		b.WriteString(m.browseView())
	}
	frame := theme.Frame(frameWidth, "navi-browser", false, "", b.String())
	if m.width == 0 {
		return frame
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, frame)
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

func cliList() {
	current := readDefault()
	for i := range browserTable {
		def := &browserTable[i]
		bin := firstBinary(def)
		mark := " "
		if def.ID == current {
			mark = "●"
		}
		state := "not installed"
		if bin != "" {
			state = bin
		}
		fmt.Printf("%s %-9s %-16s %s\n", mark, def.ID, def.Name, state)
	}
}

func cliSet(id string) int {
	if findDef(id) == nil {
		fmt.Fprintf(os.Stderr, "unknown browser id %q\n", id)
		return 1
	}
	def := findDef(id)
	if firstBinary(def) == "" {
		fmt.Fprintf(os.Stderr, "%s is not installed (navi-extras --install %s)\n", def.Name, def.ExtrasID)
		return 1
	}
	if err := writeDefault(id); err != nil {
		fmt.Fprintf(os.Stderr, "could not save default: %v\n", err)
		return 1
	}
	fmt.Printf("default browser: %s\n", def.Name)
	fmt.Println("tip: navi-browser --deploy-policy applies navi's managed policy to it")
	return 0
}

func cliDeployPolicy(id string) int {
	if id == "" {
		id = readDefault()
	}
	if id == "" {
		id = "chromium"
	}
	def := findDef(id)
	if def == nil {
		fmt.Fprintf(os.Stderr, "unknown browser id %q\n", id)
		return 1
	}
	bin := firstBinary(def)
	if bin == "" {
		fmt.Fprintf(os.Stderr, "%s is not installed\n", def.Name)
		return 1
	}
	dir, probed := discoverPolicyDir(bin, def.PolicyDir)
	command, err := deployPolicyShell(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	note := "table"
	if probed {
		note = "probed from binary"
	}
	fmt.Printf("deploying navi policy to %s (%s, %s)\n", def.Name, dir, note)
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "deploy failed: %v\n", err)
		return 1
	}
	fmt.Println("done.")
	return 0
}

func runCLI(args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "--list", "-l":
		cliList()
		return true, 0
	case "--current":
		if id := readDefault(); id != "" {
			fmt.Println(id)
		} else {
			fmt.Println("(none — chromium is the fallback)")
		}
		return true, 0
	case "--set":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: navi-browser --set <id>")
			return true, 1
		}
		return true, cliSet(args[1])
	case "--print-binary":
		id, bin := resolveBinary()
		fmt.Println(bin)
		if id == "" {
			return true, 3 // nothing installed; caller falls back
		}
		return true, 0
	case "--deploy-policy":
		id := ""
		if len(args) > 1 {
			id = args[1]
		}
		return true, cliDeployPolicy(id)
	case "--probe":
		var wg sync.WaitGroup
		results := make([]bool, len(browserTable))
		for i := range browserTable {
			bin := firstBinary(&browserTable[i])
			if bin == "" {
				fmt.Printf("%-9s not installed\n", browserTable[i].ID)
				continue
			}
			wg.Add(1)
			go func(i int, bin string) {
				defer wg.Done()
				results[i] = probeAppMode(bin)
			}(i, bin)
		}
		wg.Wait()
		for i := range browserTable {
			if firstBinary(&browserTable[i]) == "" {
				continue
			}
			status := "launch ok"
			if !results[i] {
				status = "launch FAILED"
			}
			fmt.Printf("%-9s %s\n", browserTable[i].ID, status)
		}
		return true, 0
	case "-h", "--help", "help":
		fmt.Print(`navi-browser — choose navi's default Chromium browser

usage:
  navi-browser                 interactive picker (default)
  navi-browser --list          show known browsers and install state
  navi-browser --current       print the current default id
  navi-browser --set <id>      set the default browser
  navi-browser --print-binary  print the resolved browser binary
  navi-browser --deploy-policy [id]
                               deploy navi's managed policy
  navi-browser --probe         launch smoke-test (headless render)
`)
		return true, 0
	}
	return false, 0
}

func main() {
	if handled, code := runCLI(os.Args[1:]); handled {
		os.Exit(code)
	}
	prog = tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-browser: %v\n", err)
		os.Exit(1)
	}
}
