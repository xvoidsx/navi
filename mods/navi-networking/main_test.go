package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseWifiDevice(t *testing.T) {
	// Real `nmcli -t -f DEVICE,TYPE device status` shape.
	out := "lo:loopback\neth0:ethernet\nwlan0:wifi\n"
	if got := parseWifiDevice(out); got != "wlan0" {
		t.Fatalf("got %q, want wlan0", got)
	}
	if got := parseWifiDevice("lo:loopback\neth0:ethernet\n"); got != "" {
		t.Fatalf("got %q, want empty (no wifi)", got)
	}
	if got := parseWifiDevice(""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestParseVPNProfiles(t *testing.T) {
	// Real `nmcli -t -f NAME,UUID,TYPE connection show` shape. Note the
	// backslash-escaped colon in the second profile name.
	conns := "HomeWiFi:11111111-2222-3333-4444-555555555555:802-11-wireless\n" +
		"My\\:VPN:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:wireguard\n" +
		"Wired connection 1:99999999-8888-7777-6666-555555555555:802-3-ethernet\n" +
		"office-wg:cccccccc-dddd-eeee-ffff-000000000000:wireguard\n"
	active := "HomeWiFi\nMy:VPN\n"
	got := parseVPNProfiles(conns, active)
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want 2: %+v", len(got), got)
	}
	if got[0].Name != "My:VPN" || got[0].UUID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || !got[0].Active {
		t.Fatalf("first profile wrong: %+v", got[0])
	}
	if got[1].Name != "office-wg" || got[1].Active {
		t.Fatalf("second profile wrong: %+v", got[1])
	}
}

func TestParseVPNProfilesNoActive(t *testing.T) {
	conns := "office-wg:cccccccc-dddd-eeee-ffff-000000000000:wireguard\n"
	got := parseVPNProfiles(conns, "")
	if len(got) != 1 || got[0].Active {
		t.Fatalf("expected one inactive profile, got %+v", got)
	}
}

func TestParseHotspotActive(t *testing.T) {
	out := "HomeWiFi:802-11-wireless\nHotspot:802-11-wireless\n"
	if !parseHotspotActive(out) {
		t.Fatal("expected hotspot active")
	}
	if parseHotspotActive("HomeWiFi:802-11-wireless\n") {
		t.Fatal("expected hotspot inactive")
	}
	if parseHotspotActive("") {
		t.Fatal("expected hotspot inactive on empty input")
	}
}

func TestSpeedHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if got := loadSpeedHistory(); len(got) != 0 {
		t.Fatalf("expected empty history, got %d", len(got))
	}
	now := time.Now().Truncate(time.Second)
	appendSpeedHistory(speedEntry{Time: now, PingMs: 23.4, DownMbps: 112.5, UpMbps: 11.2})
	got := loadSpeedHistory()
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	e := got[0]
	if !e.Time.Equal(now) || e.PingMs != 23.4 || e.DownMbps != 112.5 || e.UpMbps != 11.2 {
		t.Fatalf("entry mismatch: %+v", e)
	}
	// File shape is the documented JSON contract.
	raw, _ := os.ReadFile(filepath.Join(dir, "navi", "navi-networking", "speed-history.json"))
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("history file is not valid JSON: %v", err)
	}
	for _, k := range []string{"time", "ping_ms", "down_mbps", "up_mbps"} {
		if _, ok := decoded[0][k]; !ok {
			t.Fatalf("history JSON missing key %q", k)
		}
	}
}

func TestSpeedHistoryCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	for i := 0; i < maxSpeedHistory+10; i++ {
		appendSpeedHistory(speedEntry{Time: time.Now(), PingMs: float64(i), DownMbps: float64(i), UpMbps: 1})
	}
	got := loadSpeedHistory()
	if len(got) != maxSpeedHistory {
		t.Fatalf("expected cap of %d, got %d", maxSpeedHistory, len(got))
	}
	// Oldest entries dropped first.
	if got[0].PingMs != 10 {
		t.Fatalf("expected oldest kept entry to be #10, got ping %v", got[0].PingMs)
	}
}

func TestSpeedHistoryCorruptReadsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	p := filepath.Join(dir, "navi", "navi-networking")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "speed-history.json"), []byte("{nope"), 0o644)
	if got := loadSpeedHistory(); len(got) != 0 {
		t.Fatalf("corrupt file should read as empty, got %d", len(got))
	}
	// Appending over a corrupt file heals it.
	appendSpeedHistory(speedEntry{Time: time.Now(), PingMs: 1, DownMbps: 2, UpMbps: 3})
	if got := loadSpeedHistory(); len(got) != 1 {
		t.Fatalf("expected healed history to have 1 entry, got %d", len(got))
	}
}

func TestClearSpeedHistory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	appendSpeedHistory(speedEntry{Time: time.Now(), PingMs: 1, DownMbps: 2, UpMbps: 3})
	clearSpeedHistory()
	if got := loadSpeedHistory(); len(got) != 0 {
		t.Fatalf("expected empty history after clear, got %d", len(got))
	}
}

func TestHotspotQR(t *testing.T) {
	// The QR payload guests scan must be the standard WIFI: format.
	got := wifiQR("navi-cloudbook", "WPA2", "wired-drop-01")
	want := "WIFI:T:WPA;S:navi-cloudbook;P:wired-drop-01;;"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Secrets with QR-hostile characters stay escaped.
	got = wifiQR("my;ssid", "WPA2", "p:a,ss")
	if !strings.Contains(got, `S:my\;ssid`) || !strings.Contains(got, `P:p\:a\,ss`) {
		t.Fatalf("escaping broken: %q", got)
	}
}

func TestGenHotspotPassword(t *testing.T) {
	pw := genHotspotPassword()
	if len(pw) != 12 {
		t.Fatalf("expected 12-char password, got %q", pw)
	}
	// Unambiguous alphabet only: no 0/O/1/l/I.
	for _, r := range pw {
		if strings.ContainsRune("0O1lI", r) {
			t.Fatalf("ambiguous char %q in %q", r, pw)
		}
	}
	if genHotspotPassword() == pw {
		t.Fatal("two generated passwords should not collide (astronomically unlikely)")
	}
}

func TestDefaultHotspotSSID(t *testing.T) {
	// Must always return something usable, even without a hostname.
	if got := defaultHotspotSSID(); got == "" {
		t.Fatal("SSID default must not be empty")
	}
}

func keyMsgFor(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestHotspotFieldEditing(t *testing.T) {
	m := initialModel()
	m.wifiDev = "wlan0"
	nm, _ := m.openExtra(screenHotspot)
	m = nm.(model)
	if m.screen != screenHotspot {
		t.Fatalf("expected hotspot screen, got %v", m.screen)
	}
	if m.hotspotSSID == "" || m.hotspotPW == "" {
		t.Fatal("expected defaulted SSID and generated password")
	}
	// Type into the SSID field (cursor 0).
	m.hotspotSSID = ""
	nm, _ = m.updateHotspot(keyMsgFor("a"))
	m = nm.(model)
	nm, _ = m.updateHotspot(keyMsgFor("b"))
	m = nm.(model)
	if m.hotspotSSID != "ab" {
		t.Fatalf("expected SSID %q, got %q", "ab", m.hotspotSSID)
	}
	// Up at field 0 must stay at 0 (no wraparound).
	nm, _ = m.updateHotspot(keyMsgFor("up"))
	m = nm.(model)
	if m.hotspotCursor != 0 {
		t.Fatalf("up at field 0 moved cursor to %d", m.hotspotCursor)
	}
	// Down then up moves between fields.
	nm, _ = m.updateHotspot(keyMsgFor("down"))
	m = nm.(model)
	if m.hotspotCursor != 1 {
		t.Fatalf("down did not move to password field: %d", m.hotspotCursor)
	}
	nm, _ = m.updateHotspot(keyMsgFor("up"))
	m = nm.(model)
	if m.hotspotCursor != 0 {
		t.Fatalf("up did not return to SSID field: %d", m.hotspotCursor)
	}
	// Backspace edits the focused field.
	nm, _ = m.updateHotspot(keyMsgFor("backspace"))
	m = nm.(model)
	if m.hotspotSSID != "a" {
		t.Fatalf("expected SSID %q after backspace, got %q", "a", m.hotspotSSID)
	}
	// Letters that look like hotkeys still type.
	nm, _ = m.updateHotspot(keyMsgFor("x"))
	m = nm.(model)
	if m.hotspotSSID != "ax" {
		t.Fatalf("expected %q, got %q", "ax", m.hotspotSSID)
	}
	// Short password refuses to start.
	m.hotspotPW = "short"
	m.info = ConnectionInfo{}
	nm, _ = m.updateHotspot(keyMsgFor("enter"))
	m = nm.(model)
	if m.err == nil || !strings.Contains(m.err.Error(), "8 characters") {
		t.Fatalf("expected password-length error, got %v", m.err)
	}
	// ctrl+r regenerates the password.
	nm, _ = m.updateHotspot(keyMsgFor("ctrl+r"))
	m = nm.(model)
	if len(m.hotspotPW) != 12 {
		t.Fatalf("expected regenerated 12-char password, got %q", m.hotspotPW)
	}
}

func TestHotspotConfirmWhenClientConnected(t *testing.T) {
	m := initialModel()
	m.wifiDev = "wlan0"
	nm, _ := m.openExtra(screenHotspot)
	m = nm.(model)
	m.hotspotSSID = "test-ap"
	m.hotspotPW = "longenoughpw"
	m.info = ConnectionInfo{SSID: "HomeWiFi", Device: "wlan0"}
	m.loading = false // startup scan has settled by the time a user opens this
	nm, _ = m.updateHotspot(keyMsgFor("enter"))
	m = nm.(model)
	if !m.hotspotConfirm {
		t.Fatal("expected disconnect-confirm when wifi is joined as a client")
	}
	if m.loading {
		t.Fatal("should not start loading before confirm")
	}
	nm, _ = m.updateHotspot(keyMsgFor("esc"))
	m = nm.(model)
	if m.hotspotConfirm {
		t.Fatal("esc should cancel the confirm")
	}
}

func TestVPNKeys(t *testing.T) {
	m := initialModel()
	m.vpnProfiles = []vpnProfile{{Name: "a", UUID: "u1"}, {Name: "b", UUID: "u2"}}
	m.screen = screenVPN
	// Cursor movement.
	nm, _ := m.updateVPN(keyMsgFor("down"))
	m = nm.(model)
	if m.vpnCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.vpnCursor)
	}
	nm, _ = m.updateVPN(keyMsgFor("up"))
	m = nm.(model)
	if m.vpnCursor != 0 {
		t.Fatalf("expected cursor 0, got %d", m.vpnCursor)
	}
	// Import mode.
	nm, _ = m.updateVPN(keyMsgFor("i"))
	m = nm.(model)
	if !m.vpnImporting {
		t.Fatal("expected import mode")
	}
	nm, _ = m.updateVPN(keyMsgFor("~"))
	m = nm.(model)
	if !strings.HasPrefix(m.vpnImportPath, "~") {
		t.Fatalf("expected typed path, got %q", m.vpnImportPath)
	}
	nm, _ = m.updateVPN(keyMsgFor("esc"))
	m = nm.(model)
	if m.vpnImporting {
		t.Fatal("esc should leave import mode")
	}
}

func TestOpenExtraHotspotNoWifi(t *testing.T) {
	m := initialModel()
	m.wifiDev = ""
	nm, _ := m.openExtra(screenHotspot)
	m = nm.(model)
	if m.screen == screenHotspot {
		t.Fatal("hotspot screen must not open without a wifi device")
	}
	if m.err == nil {
		t.Fatal("expected an error explaining the missing wifi device")
	}
}

func TestHistoryClearFlow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	m := initialModel()
	nm, _ := m.openExtra(screenSpeedHistory)
	m = nm.(model)
	appendSpeedHistory(speedEntry{Time: time.Now(), PingMs: 1, DownMbps: 2, UpMbps: 3})
	m.history = loadSpeedHistory()
	// x arms the confirm.
	nm, _ = m.updateHistory(keyMsgFor("x"))
	m = nm.(model)
	if !m.historyConfirmClear {
		t.Fatal("expected clear confirm to arm")
	}
	// esc backs out.
	nm, _ = m.updateHistory(keyMsgFor("esc"))
	m = nm.(model)
	if m.historyConfirmClear {
		t.Fatal("esc should disarm the confirm")
	}
	// x then enter clears.
	nm, _ = m.updateHistory(keyMsgFor("x"))
	m = nm.(model)
	nm, _ = m.updateHistory(keyMsgFor("enter"))
	m = nm.(model)
	if len(m.history) != 0 || m.historyConfirmClear {
		t.Fatalf("expected cleared history, got %+v", m.history)
	}
	if got := loadSpeedHistory(); len(got) != 0 {
		t.Fatal("expected history file gone after clear")
	}
}
