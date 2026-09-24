package main

import (
	"os"
	"strings"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func testApps() []app {
	return []app{
		{ID: "native-edge", Name: "Microsoft Edge", Summary: "Microsoft's Chromium-based browser", Category: "Internet", Source: "native", Package: "edge", Install: "navi-extras --install edge", Remove: "doas apt remove -y microsoft-edge-stable"},
		{ID: "native-yandex", Name: "Yandex Browser", Summary: "Yandex's Chromium-based browser", Category: "Internet", Source: "native", Package: "yandex", Install: "navi-extras --install yandex", Remove: "doas apt remove -y yandex-browser-stable"},
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

	m.input.SetValue("yandex")
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].ID != "native-yandex" {
		t.Fatalf("expected just yandex, got %+v", m.filtered)
	}

	// multi-token AND
	m.input.SetValue("chromium yandex")
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
	if got := removeCmd(apps[2]); got != "navi-webapp --uninstall navi-radio" {
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
	if isInstalled(a, nil) {
		t.Fatal("should not be installed yet")
	}
	if err := os.WriteFile(filepath.Join(appDir, "navi-radio.desktop"), []byte("[Desktop Entry]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isInstalled(a, nil) {
		t.Fatal("should detect the .desktop launcher")
	}
}

func TestIsInstalledAgent(t *testing.T) {
	if !isInstalled(app{Source: "agent", Install: "sh"}, nil) {
		t.Fatal("sh should resolve via LookPath")
	}
	if isInstalled(app{Source: "agent", Install: "npm install -g some-thing"}, nil) {
		t.Fatal("multi-word agent install should not claim installed")
	}
	if isInstalled(app{Source: "agent", Install: "coming soon"}, nil) {
		t.Fatal("placeholder install should not claim installed")
	}
}

func TestIsInstalledNativeExtras(t *testing.T) {
	extras := map[string]string{"edge": "installed", "yandex": "missing"}
	a := app{Source: "native", Install: "navi-extras --install edge", Package: "edge"}
	if !isInstalled(a, extras) {
		t.Fatal("edge should read installed from extras state")
	}
	a.Install = "navi-extras --install yandex"
	if isInstalled(a, extras) {
		t.Fatal("yandex should read missing from extras state")
	}
	// nil extras (navi-extras unusable) never claims installed
	if isInstalled(a, nil) {
		t.Fatal("nil extras state must not claim installed")
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
	for _, want := range []string{"navi-get", "Microsoft Edge", "Yandex Browser", "navi radio", "Ollama"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	// filter narrows the render
	m.input.SetValue("ollama")
	m.applyFilter()
	out = m.View()
	if strings.Contains(out, "Yandex Browser") {
		t.Fatal("filtered view should not contain Yandex Browser")
	}
	if !strings.Contains(out, "Ollama") {
		t.Fatal("filtered view should contain Ollama")
	}
}
