// navi-get — the navi software center TUI.
//
// search + install + remove over the naviApps catalog (apps.json).
// every catalog entry carries its own install/remove shell commands,
// so navi-get is the action surface and the catalog is the data:
// webapps, native extras installers, and agents all flow through the
// same list. apt and flatpak sources are the next milestone; the
// curated catalog comes first.
//
// install/remove commands run with the real terminal attached (the
// ReleaseTerminal pattern), so doas password prompts and installer
// progress render live, exactly as they would in a shell.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const frameWidth = 68
const pageSize = 10

// ---------------------------------------------------------------------------
// catalog
// ---------------------------------------------------------------------------

// app is one entry from wired/naviApps/apps.json.
type app struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Category string `json:"category"`
	Source   string `json:"source"`
	Package  string `json:"package"`
	Install  string `json:"install"`
	Remove   string `json:"remove"`
	Icon     string `json:"icon"`
	Homepage string `json:"homepage"`
}

// catalogPaths is where apps.json can live, in preference order.
func catalogPaths() []string {
	var paths []string
	if env := os.Getenv("NAVI_GET_CATALOG"); env != "" {
		paths = append(paths, env)
	}
	paths = append(paths,
		"/usr/share/navi/wired/naviApps/apps.json",
		"/opt/navi-iso/wired/naviApps/apps.json",
		"wired/naviApps/apps.json",
	)
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "apps.json"))
	}
	return paths
}

func loadCatalog() ([]app, string, error) {
	for _, p := range catalogPaths() {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var apps []app
		if err := json.Unmarshal(raw, &apps); err != nil {
			return nil, p, fmt.Errorf("parse %s: %w", p, err)
		}
		return apps, p, nil
	}
	return nil, "", fmt.Errorf("apps.json not found (tried %s)", strings.Join(catalogPaths(), ", "))
}

// ---------------------------------------------------------------------------
// installed detection
// ---------------------------------------------------------------------------

// extrasState shells to `navi-extras --list` once and maps extra id ->
// "installed"/"missing". nil when navi-extras isn't usable.
func extrasState() map[string]string {
	out, err := exec.Command("navi-extras", "--list").Output()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 3 {
			m[f[0]] = f[2]
		}
	}
	return m
}

func isInstalled(a app, extras map[string]string) bool {
	switch a.Source {
	case "webapp":
		// navi-webapp drops the launcher here on install.
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		_, err = os.Stat(filepath.Join(home, ".local", "share", "applications", a.Package+".desktop"))
		return err == nil
	case "native":
		if strings.HasPrefix(a.Install, "navi-extras --install ") && extras != nil {
			id := strings.TrimPrefix(a.Install, "navi-extras --install ")
			return extras[id] == "installed"
		}
		return false
	case "agent":
		// single-word installs are the binary itself ("ollama").
		if a.Install != "" && !strings.Contains(a.Install, " ") {
			_, err := exec.LookPath(a.Install)
			return err == nil
		}
		return false
	}
	return false
}

// removeCmd resolves the uninstall step: the catalog's remove field,
// or the navi-webapp uninstall spelling for webapps.
func removeCmd(a app) string {
	if a.Remove != "" {
		return a.Remove
	}
	if a.Source == "webapp" && a.Package != "" {
		return "navi-webapp --uninstall " + a.Package
	}
	return ""
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type screen int

const (
	screenBrowse screen = iota
	screenConfirmInstall
	screenConfirmRemove
	screenResult
)

type runDoneMsg struct {
	action string // "install" | "remove"
	name   string
	err    error
}

type model struct {
	apps     []app
	filtered []app
	inst     map[string]bool // app id -> installed
	extras   map[string]string

	input  textinput.Model
	cursor int
	offset int

	screen screen
	target *app // entry being confirmed / run
	status string
	ok     bool // last result ok?

	width, height int
}

var prog *tea.Program

func newModel(apps []app) model {
	ti := textinput.New()
	ti.Placeholder = "search the catalog…"
	ti.Prompt = "⌕ "
	ti.Focus()
	ti.CharLimit = 64

	extras := extrasState()
	inst := map[string]bool{}
	for _, a := range apps {
		inst[a.ID] = isInstalled(a, extras)
	}
	m := model{apps: apps, filtered: apps, inst: inst, extras: extras, input: ti}
	return m
}

func (m *model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		m.filtered = m.apps
	} else {
		toks := strings.Fields(q)
		var out []app
		for _, a := range m.apps {
			hay := strings.ToLower(a.Name + " " + a.Summary + " " + a.ID + " " + a.Package + " " + a.Category)
			hit := true
			for _, t := range toks {
				if !strings.Contains(hay, t) {
					hit = false
					break
				}
			}
			if hit {
				out = append(out, a)
			}
		}
		m.filtered = out
	}
	m.cursor = 0
	m.offset = 0
}

func (m *model) selected() *app {
	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.cursor]
}

func (m *model) refreshOne(id string) {
	for _, a := range m.apps {
		if a.ID == id {
			m.inst[id] = isInstalled(a, m.extras)
			return
		}
	}
}

// runShell executes a catalog command with the real terminal attached so
// doas prompts and installer progress render live.
func runShellCmd(command, action, name string) tea.Cmd {
	return func() tea.Msg {
		if prog != nil {
			_ = prog.ReleaseTerminal()
			defer func() { _ = prog.RestoreTerminal() }()
		}
		fmt.Printf("\n  running: %s\n\n", command)
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Run()
		return runDoneMsg{action: action, name: name, err: err}
	}
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case runDoneMsg:
		m.ok = msg.err == nil
		if m.target != nil {
			m.extras = extrasState() // installers may have changed things
			m.refreshOne(m.target.ID)
		}
		if m.ok {
			m.status = fmt.Sprintf("%s %s.", msg.action, msg.name)
		} else {
			m.status = fmt.Sprintf("%s failed — see the output above.", msg.action)
		}
		m.screen = screenResult
		m.target = nil
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenBrowse:
			return m.updateBrowse(msg)
		case screenConfirmInstall, screenConfirmRemove:
			return m.updateConfirm(msg)
		case screenResult:
			if msg.Type == tea.KeyEnter || msg.Type == tea.KeyEsc {
				m.screen = screenBrowse
				m.status = ""
			}
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Structural keys win no matter what has focus.
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.applyFilter()
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		}
		return m, nil
	case tea.KeyDown:
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			if m.cursor >= m.offset+pageSize {
				m.offset = m.cursor - pageSize + 1
			}
		}
		return m, nil
	case tea.KeyEnter:
		sel := m.selected()
		if sel == nil {
			return m, nil
		}
		if sel.Install == "" {
			m.status = "no install step defined for this entry."
			return m, nil
		}
		if m.inst[sel.ID] {
			m.status = fmt.Sprintf("%s is already installed.", sel.Name)
			return m, nil
		}
		m.target = sel
		m.screen = screenConfirmInstall
		return m, nil
	case tea.KeyTab:
		// Tab toggles between typing in the search box and the
		// shortcut layer. Shortcuts only fire when the search box
		// is blurred, so typing can never trigger them.
		if m.input.Focused() {
			m.input.Blur()
		} else {
			m.input.Focus()
		}
		return m, nil
	}

	if m.input.Focused() {
		// Typing mode: every other key belongs to the search box.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.applyFilter()
		return m, cmd
	}

	// Shortcut mode: the search box is blurred.
	switch msg.String() {
	case "/":
		m.input.Focus()
		return m, nil
	case "q":
		return m, tea.Quit
	case "u", "d":
		sel := m.selected()
		if sel == nil {
			return m, nil
		}
		if removeCmd(*sel) == "" {
			m.status = "no remove step defined for this entry."
			return m, nil
		}
		if !m.inst[sel.ID] {
			m.status = fmt.Sprintf("%s isn't installed.", sel.Name)
			return m, nil
		}
		m.target = sel
		m.screen = screenConfirmRemove
		return m, nil
	case "r":
		// refresh installed states
		m.extras = extrasState()
		for _, a := range m.apps {
			m.inst[a.ID] = isInstalled(a, m.extras)
		}
		m.status = "states refreshed."
		return m, nil
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.screen = screenBrowse
		m.target = nil
		return m, nil
	}
	switch strings.ToLower(msg.String()) {
	case "y":
		t := m.target
		if t == nil {
			m.screen = screenBrowse
			return m, nil
		}
		if m.screen == screenConfirmInstall {
			return m, runShellCmd(t.Install, "installed", t.Name)
		}
		return m, runShellCmd(removeCmd(*t), "removed", t.Name)
	default:
		m.screen = screenBrowse
		m.target = nil
		return m, nil
	}
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func badge(installed bool) string {
	if installed {
		return theme.DotOn.Render("● installed")
	}
	return theme.Dimmed.Render("○ not installed")
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (m model) View() string {
	var b strings.Builder

	switch m.screen {
	case screenConfirmInstall, screenConfirmRemove:
		b.WriteString(m.confirmView())
	case screenResult:
		b.WriteString(m.resultView())
	default:
		b.WriteString(m.browseView())
	}

	frame := theme.Frame(frameWidth, "navi-get", false, "", b.String())
	if m.width == 0 {
		return frame
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, frame)
}

func (m model) browseView() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render(fmt.Sprintf("SOFTWARE  ·  %d entries", len(m.apps))))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(theme.Divider(frameWidth - 4))
	b.WriteString("\n")

	if len(m.filtered) == 0 {
		b.WriteString(theme.Dimmed.Render("  nothing in the catalog matches."))
		b.WriteString("\n")
	} else {
		end := m.offset + pageSize
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := m.offset; i < end; i++ {
			a := m.filtered[i]
			cursor := "  "
			name := theme.Normal.Render(a.Name)
			if i == m.cursor {
				cursor = theme.Selected.Render("▸ ")
				name = theme.Selected.Render(a.Name)
			}
			icon := a.Icon
			if icon == "" {
				icon = "◇"
			}
			b.WriteString(fmt.Sprintf("%s%s %s  %s\n", cursor, icon, name, badge(m.inst[a.ID])))
			b.WriteString(fmt.Sprintf("    %s\n", theme.Dimmed.Render(trunc(a.Summary, frameWidth-10))))
		}
		if len(m.filtered) > pageSize {
			b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  showing %d–%d of %d", m.offset+1, end, len(m.filtered))))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(theme.Grayed.Render("  " + m.status))
		b.WriteString("\n")
	}
	b.WriteString(theme.Footer(true,
		[2]string{"↑↓", "move"},
		[2]string{"enter", "install"},
		[2]string{"u", "remove"},
		[2]string{"r", "refresh"},
		[2]string{"q", "quit"},
		[2]string{"tab", "focus search"},
	))
	return b.String()
}

func (m model) confirmView() string {
	t := m.target
	var b strings.Builder
	action := "install"
	cmd := ""
	if m.screen == screenConfirmInstall {
		cmd = t.Install
	} else {
		action = "remove"
		cmd = removeCmd(*t)
	}
	b.WriteString(theme.Header.Render(strings.ToUpper(action)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", t.Icon, theme.Normal.Render(t.Name)))
	b.WriteString(fmt.Sprintf("  %s\n\n", theme.Dimmed.Render(trunc(t.Summary, frameWidth-8))))
	b.WriteString(theme.Dimmed.Render("  will run:"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s\n\n", theme.Grayed.Render(trunc(cmd, frameWidth-8))))
	if m.screen == screenConfirmRemove {
		b.WriteString(theme.Error.Render("  this removes software from your machine."))
		b.WriteString("\n\n")
	}
	b.WriteString(theme.Footer(true,
		[2]string{"y", action},
		[2]string{"n", "cancel"},
	))
	return b.String()
}

func (m model) resultView() string {
	var b strings.Builder
	if m.ok {
		b.WriteString(theme.DotOn.Render("  ✓ " + m.status))
	} else {
		b.WriteString(theme.Error.Render("  ✗ " + m.status))
	}
	b.WriteString("\n\n")
	b.WriteString(theme.Footer(true,
		[2]string{"enter", "back to the catalog"},
	))
	return b.String()
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	apps, _, err := loadCatalog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "navi-get: %v\n", err)
		os.Exit(1)
	}
	prog = tea.NewProgram(newModel(apps), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-get: %v\n", err)
		os.Exit(1)
	}
}
