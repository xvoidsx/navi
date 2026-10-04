package main

// Hardware-gated end-to-end test for the native D-Bus pairing path.
//
// The TUI is awkward to drive from a test, but everything that matters lives
// underneath it: BlueZ signals → handleDBusEvent → the device list →
// beginPair → Device1.Pair → trust → connect. That path is testable without
// a terminal, and only a real adapter can tell you pairing actually works.
//
// Skipped unless NAVI_BT_LIVE=1. Set NAVI_BT_MAC to target a specific device
// (e.g. an Apple Magic Keyboard); leave it unset to use the first
// keyboard-shaped device found.
//
//	NAVI_BT_LIVE=1 NAVI_BT_MAC=04:69:F8:DA:18:E4 go test -run Live -v -timeout 150s
//
// Power the device on first — Apple keyboards only advertise for a short
// window after the side switch is cycled.

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// ok2 reports whether a tea.Model is our model type.
func ok2(v tea.Model) bool { _, is := v.(model); return is }

func liveEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("NAVI_BT_LIVE") != "1" {
		t.Skip("set NAVI_BT_LIVE=1 to run against real hardware")
	}
	return true
}

// liveModel builds a model wired to the real adapter, plus the goroutine
// that feeds it backend events — the same job dbusPumpCmd does in the TUI.
// The returned function answers agent prompts the way a user would.
func liveModel(t *testing.T) (*BlueZBackend, *model) {
	t.Helper()
	b, err := NewBlueZBackend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	t.Cleanup(b.Close)
	if err := b.SetPowered(true); err != nil {
		t.Logf("SetPowered: %v", err)
	}
	if err := b.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := b.RegisterAgent(); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	b.StartDiscovery()

	m := initialModel()
	m.useDBus = true
	m.backend = b
	// Seed from the devices BlueZ already knows, exactly as dbusInitMsg does
	// in the TUI. Without this the model only ever learns about devices via
	// InterfacesAdded, which BlueZ re-emits solely for *newly* discovered
	// hardware — an already-paired device would never appear.
	for _, d := range b.Devices() {
		m.devs = append(m.devs, fromBlueZ(d))
	}
	m.rebuildRows()
	go func() {
		for ev := range b.Events() {
			switch e := ev.(type) {
			case PairConfirmEvent:
				t.Logf("agent: RequestConfirmation passkey=%06d -> approve", e.Passkey)
				e.Resp <- true
			case AuthorizeEvent:
				t.Logf("agent: AuthorizeService %s -> allow", e.UUID)
				e.Resp <- true
			case PairDisplayPasskeyEvent:
				t.Logf("agent: DisplayPasskey %06d (the device shows a code)", e.Passkey)
			case PairRequestPasskeyEvent:
				e.Resp <- 0xFFFFFFFF
			case PairRequestPINEvent:
				e.Resp <- ""
			}
			next, _ := m.handleDBusEvent(ev)
			if mm, ok := next.(model); ok {
				m = mm
			}
		}
	}()
	return b, &m
}

// liveTarget waits for the requested device to show up in the model's list.
func liveTarget(t *testing.T, m *model) device {
	t.Helper()
	want := strings.ToUpper(strings.TrimSpace(os.Getenv("NAVI_BT_MAC")))
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range m.devs {
			if want != "" {
				if strings.EqualFold(d.mac, want) {
					return d
				}
				continue
			}
			if isInputDevice(d.name) || d.kind == DeviceKeyboard {
				return d
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if want == "" {
		t.Skipf("no keyboard advertising (%d other devices seen); power one on", len(m.devs))
	}
	t.Fatalf("%s did not appear within 60s (%d devices seen); is it powered on and advertising?",
		want, len(m.devs))
	return device{}
}

// TestLiveDeviceList proves discovery signals reach the model and produce a
// selectable row with a D-Bus path. If this fails, nothing else in the UI
// can work.
func TestLiveDeviceList(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	_, m := liveModel(t)
	d := liveTarget(t, m)
	t.Logf("found: %s (%s) kind=%s", d.name, d.mac, d.kind)
	if d.path == "" {
		t.Fatalf("%s has no D-Bus path; the UI would fall back to bluetoothctl", d.mac)
	}
	if m.selectableCount() == 0 {
		t.Fatal("device is in the list but no selectable row was built")
	}
}

// TestLivePairKeyboard runs the whole native path the way the TUI does: the
// user picks a row, beginPair drives Device1.Pair, and the model trusts and
// connects the result. This is the flow that used to panic on every attempt.
func TestLivePairKeyboard(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	b, m := liveModel(t)
	target := liveTarget(t, m)
	t.Logf("target: %s (%s) paired=%v", target.name, target.mac, target.paired)

	if target.paired {
		// Already bonded. Device1.Pair would only return AlreadyExists, but
		// trust + connect are exactly what a bonded-but-untrusted device
		// still needs, so run that half against a synthetic success.
		t.Log("already paired; exercising the trust/connect half only")
		// beginPair normally sets pairDev; here we skip it, so seed it the
		// way the picker would have.
		m.pairDev = target
		runTrustConnect(m, dbusPairDoneMsg{})
	} else {
		next, cmd := m.beginPair(target)
		if mm, ok := next.(model); ok {
			*m = mm
		}

		// beginPair returns Batch(<pairing cmd>, Tick(90s)). Run only the
		// first: it blocks until Device1.Pair returns, and the timeout tick
		// would just sit there for 90 seconds.
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			t.Fatal("beginPair did not return a command batch")
		}
		msg := batch[0]()

		done, ok := msg.(dbusPairDoneMsg)
		if !ok {
			t.Fatalf("expected dbusPairDoneMsg, got %T", msg)
		}
		if done.err != nil {
			t.Fatalf("pairing failed: %v", done.err)
		}
		t.Log("Device1.Pair returned success")

		// Trust + connect is the command dbusPairDoneMsg hands to Bubble Tea.
		runTrustConnect(m, done)
	}

	// Report the status line the user would see.
	reportOp(t, trustCmdOf(m))

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		final := b.lookupDevice(dbus.ObjectPath(target.path))
		if final.Paired && final.Trusted && final.Connected {
			t.Logf("OK paired+trusted+connected: %s (%s)", final.DisplayName(), final.Address)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	final := b.lookupDevice(dbus.ObjectPath(target.path))
	t.Fatalf("ended paired=%v trusted=%v connected=%v for %s",
		final.Paired, final.Trusted, final.Connected, final.Address)
}

// lastTrustCmd holds the trust+connect command so it can be reported after
// the model has been updated.
var lastTrustCmd tea.Cmd

// runTrustConnect feeds the post-pair message into the model and keeps the
// trust+connect command it yields.
func runTrustConnect(m *model, done dbusPairDoneMsg) {
	next, cmd := m.Update(done)
	if mm, ok := next.(model); ok {
		*m = mm
	}
	lastTrustCmd = cmd
}

// trustCmdOf returns the command produced by the last runTrustConnect.
func trustCmdOf(_ *model) tea.Cmd { return lastTrustCmd }

// reportOp runs a trust/connect command and surfaces its result the way the
// TUI's status line would.
func reportOp(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no trust/connect command was produced after pairing")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		if len(batch) == 0 {
			t.Fatal("empty trust/connect batch")
		}
		cmd = batch[0]
	}
	reply := cmd()
	if op, isOp := reply.(opMsg); isOp {
		if op.err != nil {
			t.Fatalf("trust/connect failed: %v", op.err)
		}
		t.Logf("status line the user sees: %q", op.note)
	}
}
