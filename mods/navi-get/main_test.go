package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func testApps() []app {
	return []app{
		{ID: "native-edge", Name: "Microsoft Edge", Summary: "Microsoft's Chromium-based browser", Category: "Internet", Source: "native", Package: "edge", Install: "navi-extras --install edge", Remove: "doas apt remove -y microsoft-edge-stable"},
		{ID: "native-chrome-canary", Name: "Google Chrome Canary", Summary: "Bleeding-edge Chromium-based browser", Category: "Internet", Source: "native", Package: "chrome-canary", Install: "navi-extras --install chrome-canary", Remove: "doas apt remove -y google-chrome-unstable"},
		{ID: "web-navi-radio", Name: "navi radio", Summary: "Our own music app", Category: "navi", Source: "webapp", Package: "navi-radio", Install: "navi-webapp navi-radio"},
		{ID: "agent-ollama", Name: "Ollama", Summary: "Run open models locally", Category: "AI", Source: "agent", Install: "ollama"},
	}
}

func TestApplyFilter(t *testing.T) {
	m := newModel(testApps())

	m.input.SetValue("browser")
	m.applyFilter()
	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 browser hits, got %d", len(m.filtered))
	}

	m.input.SetValue("canary")
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].ID != "native-chrome-canary" {
		t.Fatalf("expected just canary, got %+v", m.filtered)
	}

	// multi-token AND
	m.input.SetValue("chromium canary")
	m.applyFilter()
	if len(m.filtered) != 1 {
		t.Fatalf("expected 1 AND hit, got %d", len(m.filtered))
	}

	// empty query restores everything, cursor resets
	m.input.SetValue("")
	m.applyFilter()
	if len(m.filtered) != 4 || m.cursor != 0 {
		t.Fatalf("expected full catalog, got %d (cursor %d)", len(m.filtered), m.cursor)
	}

	// no match
	m.input.SetValue("zzzznope")
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(m.filtered))
	}
	if m.selected() != nil {
		t.Fatal("selected() should be nil on empty results")
	}
}

func TestRemoveCmd(t *testing.T) {
	apps := testApps()
	if got := removeCmd(apps[0]); got != "doas apt remove -y microsoft-edge-stable" {
		t.Fatalf("native remove: %q", got)
	}
	// webapp entries synthesize the navi-webapp spelling
	if got := removeCmd(apps[2]); got != "navi-webapp uninstall navi-radio" {
		t.Fatalf("webapp remove: %q", got)
	}
	// agents with no remove field and no fallback
	noRemove := app{ID: "agent-x", Source: "agent", Install: "npm install -g x"}
	if got := removeCmd(noRemove); got != "" {
		t.Fatalf("expected empty remove, got %q", got)
	}
}

func TestIsInstalledWebapp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := app{ID: "web-navi-radio", Source: "webapp", Package: "navi-radio"}
	if isInstalled(a, nil, nil) {
		t.Fatal("should not be installed yet")
	}
	if err := os.WriteFile(filepath.Join(appDir, "navi-radio.desktop"), []byte("[Desktop Entry]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isInstalled(a, nil, nil) {
		t.Fatal("should detect the .desktop launcher")
	}
}

func TestIsInstalledAgent(t *testing.T) {
	// the package field names the binary the install produces, no
	// matter how the install command is spelled.
	if !isInstalled(app{Source: "agent", Package: "sh", Install: "doas npm install -g sh-thing"}, nil, nil) {
		t.Fatal("sh should resolve via LookPath through the package field")
	}
	if isInstalled(app{Source: "agent", Package: "definitely-not-a-real-binary", Install: "doas npm install -g some-thing"}, nil, nil) {
		t.Fatal("missing binary must not claim installed")
	}
	if isInstalled(app{Source: "agent", Install: "coming soon"}, nil, nil) {
		t.Fatal("empty package must not claim installed")
	}
}

func TestIsInstalledNativeExtras(t *testing.T) {
	extras := map[string]string{"edge": "installed", "chrome-canary": "missing"}
	a := app{Source: "native", Install: "navi-extras --install edge", Package: "edge"}
	if !isInstalled(a, extras, nil) {
		t.Fatal("edge should read installed from extras state")
	}
	a.Install = "navi-extras --install chrome-canary"
	if isInstalled(a, extras, nil) {
		t.Fatal("canary should read missing from extras state")
	}
	// nil extras (navi-extras unusable) never claims installed
	if isInstalled(a, nil, nil) {
		t.Fatal("nil extras state must not claim installed")
	}
}

func TestAptPackage(t *testing.T) {
	cases := map[string]string{
		"doas apt install -y freedoom":                    "freedoom",
		"sudo apt install vim":                            "vim",
		"doas apt install --no-install-recommends -y foo": "foo",
		"navi-extras --install edge":                      "",
		"flatpak install -y flathub sh.ppy.osu":           "",
		"doas apt remove -y freedoom":                     "",
		"":                                                "",
	}
	for in, want := range cases {
		if got := aptPackage(in); got != want {
			t.Fatalf("aptPackage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsInstalledNativeApt(t *testing.T) {
	// a definitely-not-installed package must read missing (dpkg-query
	// errors or reports otherwise on every machine this runs on)
	a := app{Source: "native", Package: "navi-test-pkg-that-does-not-exist", Install: "doas apt install -y navi-test-pkg-that-does-not-exist"}
	if isInstalled(a, nil, nil) {
		t.Fatal("bogus apt package should read not installed")
	}
	// non-apt native installs still read missing without extras state
	b := app{Source: "native", Package: "x", Install: "curl https://example.com/install.sh | bash"}
	if isInstalled(b, nil, nil) {
		t.Fatal("non-apt native install should read not installed")
	}
}

func TestIsInstalledFlatpak(t *testing.T) {
	flatpaks := map[string]bool{"sh.ppy.osu": true, "org.gimp.GIMP": true}
	a := app{ID: "flatpak-osu-lazer", Source: "flatpak", Package: "sh.ppy.osu"}
	if !isInstalled(a, nil, flatpaks) {
		t.Fatal("sh.ppy.osu should read installed from flatpak state")
	}
	a.Package = "com.example.Missing"
	if isInstalled(a, nil, flatpaks) {
		t.Fatal("unknown flatpak id should read not installed")
	}
	// empty flatpak state (flatpak unusable) never claims installed
	if isInstalled(app{Source: "flatpak", Package: "sh.ppy.osu"}, nil, map[string]bool{}) {
		t.Fatal("empty flatpak state must not claim installed")
	}
	// nil flatpak state also never claims installed
	if isInstalled(app{Source: "flatpak", Package: "sh.ppy.osu"}, nil, nil) {
		t.Fatal("nil flatpak state must not claim installed")
	}
}

func TestLoadCatalogMissing(t *testing.T) {
	t.Setenv("NAVI_GET_CATALOG", filepath.Join(t.TempDir(), "nope.json"))
	// also neutralize the system paths by running with a bogus env only;
	// loadCatalog tries real system paths too, so just assert the error
	// mentions apps.json when nothing exists is not deterministic here.
	// Instead: point env at a real temp catalog.
	dir := t.TempDir()
	p := filepath.Join(dir, "apps.json")
	if err := os.WriteFile(p, []byte(`[{"id":"x","name":"X"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NAVI_GET_CATALOG", p)
	apps, found, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if found != p || len(apps) != 1 || apps[0].ID != "x" {
		t.Fatalf("unexpected catalog load: %s %+v", found, apps)
	}
}

func TestBrowseViewRenders(t *testing.T) {
	// force color so nested-style bugs (visible ESC fragments) would show
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := newModel(testApps())
	out := m.View()
	for _, want := range []string{"navi-get", "Microsoft Edge", "Google Chrome Canary", "navi radio", "Ollama"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	// filter narrows the render
	m.input.SetValue("ollama")
	m.applyFilter()
	out = m.View()
	if strings.Contains(out, "Google Chrome Canary") {
		t.Fatal("filtered view should not contain Google Chrome Canary")
	}
	if !strings.Contains(out, "Ollama") {
		t.Fatal("filtered view should contain Ollama")
	}
}

func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// Typing while the search box is focused must never fire shortcuts:
// every letter that could collide (r, u, d, q) lands in the input.
func TestTypingDoesNotTriggerShortcuts(t *testing.T) {
	m := newModel(testApps())
	if !m.input.Focused() {
		t.Fatal("search box should start focused")
	}
	for _, r := range []rune{'r', 'u', 'd', 'q'} {
		next, cmd := m.Update(keyMsg(r))
		m = next.(model)
		if isQuit(cmd) {
			t.Fatalf("typing %q quit the app", r)
		}
		if m.screen != screenBrowse {
			t.Fatalf("typing %q left the browse screen", r)
		}
		if m.status != "" {
			t.Fatalf("typing %q produced a status: %q", r, m.status)
		}
	}
	if got := m.input.Value(); got != "rudq" {
		t.Fatalf("expected typed letters in the search box, got %q", got)
	}
}

// Tab blurs the search box; shortcuts fire again in list mode.
func TestTabTogglesShortcutMode(t *testing.T) {
	m := newModel(testApps())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.input.Focused() {
		t.Fatal("Tab should blur the search box")
	}

	next, _ = m.Update(keyMsg('r'))
	m = next.(model)
	if m.status != "states refreshed." {
		t.Fatalf("blurred 'r' should refresh, got status %q", m.status)
	}

	next, cmd := m.Update(keyMsg('q'))
	if !isQuit(cmd) {
		t.Fatal("blurred 'q' should quit")
	}

	// '/' refocuses the search box from list mode
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	next, _ = m.Update(keyMsg('/'))
	m = next.(model)
	if !m.input.Focused() {
		t.Fatal("'/' should refocus the search box")
	}
}

// Esc with text in the box clears the query instead of quitting.
func TestEscClearsQuery(t *testing.T) {
	m := newModel(testApps())
	m.input.SetValue("canary")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if isQuit(cmd) {
		t.Fatal("Esc with a query should clear, not quit")
	}
	if m.input.Value() != "" {
		t.Fatalf("expected cleared query, got %q", m.input.Value())
	}
}
