// agenttrace.go — field diagnostics for the BlueZ agent.
//
// The pairing dance is invisible from the TUI: BlueZ picks the association
// model from the IO-capability exchange, then calls exactly one Agent1
// method. When pairing "does nothing" the only honest question is which
// method fired, with what arguments, and what the device looked like at
// that instant. Guessing from the visible screen is how the wrong flow gets
// hard-coded in.
//
// Enabled by --trace-pair <MAC>, which runs the *real* naviAgent headless
// with a scripted policy and appends every callback to
// ~/.local/share/navi/navi-bluetooth/agent-trace.log.
//
// The hooks below are no-ops unless tracing is on, so the TUI path pays
// nothing for them.

package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

var (
	traceMu   sync.Mutex
	traceFile *os.File
	traceOn   bool
)

// traceDir is where every navi-bluetooth diagnostic lands.
func traceDir() string {
	return os.ExpandEnv("$HOME/.local/share/navi/navi-bluetooth")
}

// traceStart opens the trace log. Safe to call once.
func traceStart(header string) error {
	traceMu.Lock()
	defer traceMu.Unlock()
	if traceOn {
		return nil
	}
	os.MkdirAll(traceDir(), 0755)
	f, err := os.OpenFile(traceDir()+"/agent-trace.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	traceFile = f
	traceOn = true
	fmt.Fprintf(traceFile, "\n########## %s ##########\n", header)
	fmt.Fprintf(traceFile, "pid=%d started %s\n", os.Getpid(),
		time.Now().Format(time.RFC3339Nano))
	return nil
}

func traceStop() {
	traceMu.Lock()
	defer traceMu.Unlock()
	if traceFile != nil {
		fmt.Fprintf(traceFile, "pid=%d ended %s\n", os.Getpid(),
			time.Now().Format(time.RFC3339Nano))
		traceFile.Close()
		traceFile = nil
	}
	traceOn = false
}

// tracef appends one timestamped line to the trace log.
func tracef(format string, args ...interface{}) {
	traceMu.Lock()
	defer traceMu.Unlock()
	if !traceOn || traceFile == nil {
		return
	}
	fmt.Fprintf(traceFile, "[%s] %s\n",
		time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// traceDevice renders the classification inputs for a device, so a trace
// line answers "why did the agent think it was (not) a keyboard?".
func traceDevice(tag string, d BlueZDevice) {
	if !traceOn {
		return
	}
	tracef("%s path=%s addr=%s name=%q alias-derived=%q icon=%q class=0x%04x "+
		"kind=%s paired=%v trusted=%v connected=%v uuids=%v",
		tag, d.Path, d.Address, d.Name, d.DisplayName(), d.Icon, d.Class,
		d.Kind, d.Paired, d.Trusted, d.Connected, d.UUIDs)
}

// tracePairTarget records what the agent believes the user is pairing with.
// The pair-target short-circuit in RequestConfirmation depends on this, so a
// trace without it can't explain an auto-approve.
func (b *BlueZBackend) tracePairTarget(where string) {
	if !traceOn {
		return
	}
	b.mu.Lock()
	set, tgt := b.pairTargetSet, b.pairTarget
	b.mu.Unlock()
	if !set {
		tracef("pairTarget@%s: UNSET", where)
		return
	}
	tracef("pairTarget@%s: path=%s addr=%s name=%q kind=%s",
		where, tgt.Path, tgt.Address, tgt.Name, tgt.Kind)
}

// tracePolicy records the decision an agent method made about how to answer.
func tracePolicy(cb string, device dbus.ObjectPath, decision string, detail string) {
	if !traceOn {
		return
	}
	tracef("DECIDE %s(%s): %s%s", cb, device, decision,
		map[bool]string{true: " — " + detail, false: ""}[detail != ""])
}

// ── headless trace driver ────────────────────────────────────────────

// traceAnswer is the scripted policy for one agent callback.
type traceAnswer int

const (
	traceApprove traceAnswer = iota
	traceDeny
	tracePrompt // wait for a human on stdin (interactive runs)
)

type traceDriver struct {
	backend *BlueZBackend
	answer  traceAnswer
	target  string // MAC, uppercase
	events  int
}

func (t *traceDriver) note(format string, args ...interface{}) {
	t.events++
	tracef("CALLBACK #%d %s", t.events, fmt.Sprintf(format, args...))
}

func (t *traceDriver) settle(kind, decision, detail string) {
	tracef("  -> responding %s%s", decision,
		map[bool]string{true: " (" + detail + ")", false: ""}[detail != ""])
}

// pump mirrors the TUI's dbusPumpCmd: consume backend events, log the ones
// that matter to pairing, and answer every response channel so BlueZ is
// never left waiting on us.
func (t *traceDriver) pump() {
	for ev := range t.backend.Events() {
		switch e := ev.(type) {
		case DeviceAddedEvent:
			traceDevice("EVENT DeviceAdded", e.Device)
		case DeviceUpdatedEvent:
			traceDevice("EVENT DeviceUpdated", e.Device)
		case DeviceRemovedEvent:
			tracef("EVENT DeviceRemoved %s", e.Path)
		case DiscoveryEvent:
			tracef("EVENT Discovering=%v", e.Discovering)
		case PairDisplayPasskeyEvent:
			t.note("DisplayPasskeyEvent passkey=%06d entered=%d (no reply needed)",
				e.Passkey, e.Entered)
			t.settle("display", "nothing — BlueZ waits for the device", "")
		case PairConfirmEvent:
			t.note("PairConfirmEvent passkey=%06d", e.Passkey)
			traceDevice("  event device", e.Device)
			if t.answer == traceDeny {
				e.Resp <- false
				t.settle("confirm", "DENY", "scripted")
			} else {
				e.Resp <- true
				t.settle("confirm", "APPROVE", "scripted")
			}
		case PairRequestPasskeyEvent:
			t.note("PairRequestPasskeyEvent (host must supply a passkey)")
			e.Resp <- 0xFFFFFFFF
			t.settle("passkey", "CANCEL", "scripted")
		case PairRequestPINEvent:
			t.note("PairRequestPINEvent")
			e.Resp <- ""
			t.settle("pin", "CANCEL", "scripted")
		case PairCancelledEvent:
			t.note("CancelEvent")
		case AuthorizeEvent:
			t.note("AuthorizeEvent uuid=%s", e.UUID)
			traceDevice("  event device", e.Device)
			if t.answer == traceDeny {
				e.Resp <- false
				t.settle("authorize", "DENY", "scripted")
			} else {
				e.Resp <- true
				t.settle("authorize", "ALLOW", "scripted")
			}
		default:
			tracef("EVENT %T", ev)
		}
	}
}

// runTracePair is the --trace-pair entry point: register the agent, pair,
// trust, connect, and report. It exercises the same naviAgent the TUI uses.
func runTracePair(mac string, answer traceAnswer) int {
	if err := traceStart("trace-pair " + mac); err != nil {
		fmt.Fprintln(os.Stderr, "trace: cannot open log:", err)
		return 1
	}
	defer traceStop()

	b, err := NewBlueZBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: backend:", err)
		return 1
	}
	defer b.Close()
	if err := b.SetPowered(true); err != nil {
		tracef("SetPowered: %v", err)
	}
	if err := b.Watch(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: Watch:", err)
		return 1
	}
	if err := b.RegisterAgent(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: RegisterAgent:", err)
		return 1
	}
	tracef("agent registered at %s capability=DisplayOnly", agentPath)
	tracef("adapter=%s", b.adapterPath)

	// Drain the backend event channel exactly like the TUI does, and answer
	// with the scripted policy. Without this, any callback that waits on a
	// response channel (AuthorizeService, RequestConfirmation for a
	// non-input device) deadlocks the trace instead of reporting.
	driver := &traceDriver{backend: b, answer: answer}
	go driver.pump()

	// Give discovery a moment so the device is in the cache. Apple
	// keyboards only advertise for a short window after a power-cycle, so
	// keep re-arming discovery instead of scanning once and giving up.
	deadline := time.Now().Add(60 * time.Second)
	var dev BlueZDevice
	found := false
	nextScan := time.Now()
	for time.Now().Before(deadline) && !found {
		if time.Now().After(nextScan) {
			b.StartDiscovery()
			nextScan = time.Now().Add(5 * time.Second)
		}
		if d, ok := b.FindByAddress(mac); ok {
			dev, found = d, true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		// Cache miss is survivable: ask BlueZ directly for the path.
		tracef("device %s not in cache; probing adapter child paths", mac)
		path := dbus.ObjectPath("/org/bluez/" + string(b.adapterPath)[len("/org/bluez/"):] +
			"/dev_" + replaceMAC(mac))
		dev = b.lookupDevice(path)
		if dev.Address == "" {
			fmt.Fprintf(os.Stderr, "FAIL: %s not found — is it powered on and in pairing mode?\n", mac)
			tracef("FAIL: device not found")
			return 1
		}
	}
	traceDevice("TARGET", dev)
	b.SetPairTarget(dev)
	b.tracePairTarget("before-Pair")

	fmt.Printf("target: %s (%s) kind=%s path=%s\n",
		dev.DisplayName(), dev.Address, dev.Kind, dev.Path)

	if err := b.EnsureDefaultAgent(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: EnsureDefaultAgent:", err)
		return 1
	}

	fmt.Println("pairing… (this can take ~30s; type the code on the device if asked)")
	done := make(chan error, 1)
	b.PairAsync(dev.Path, func(err error) { done <- err })
	var pairErr error
	select {
	case pairErr = <-done:
	case <-time.After(120 * time.Second):
		pairErr = fmt.Errorf("local timeout waiting for Device1.Pair")
		tracef("FAIL: %v", pairErr)
	}
	if pairErr != nil {
		fmt.Fprintf(os.Stderr, "pair failed: %v\n", pairErr)
		tracef("PAIR FAILED: %v", pairErr)
		b.dumpDevProps(dev.Path)
		return 1
	}
	tracef("PAIR OK")

	cur := b.lookupDevice(dev.Path)
	traceDevice("PAIRED", cur)
	if !cur.Paired {
		fmt.Fprintln(os.Stderr, "FAIL: Pair() returned but Paired=false")
		tracef("FAIL: Pair() returned but Paired=false")
		return 1
	}

	if err := b.SetTrusted(dev.Path, true); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: trust:", err)
		tracef("WARN: SetTrusted: %v", err)
	} else {
		tracef("SetTrusted OK")
	}

	fmt.Println("connecting…")
	if err := b.Connect(dev.Path); err != nil {
		fmt.Fprintf(os.Stderr, "connect failed: %v\n", err)
		tracef("CONNECT FAILED: %v", err)
		b.dumpDevProps(dev.Path)
		return 1
	}
	tracef("Connect OK")

	// Give BlueZ a moment to settle profiles, then report.
	time.Sleep(2 * time.Second)
	final := b.lookupDevice(dev.Path)
	traceDevice("FINAL", final)
	fmt.Printf("RESULT paired=%v trusted=%v connected=%v\n",
		final.Paired, final.Trusted, final.Connected)
	if !final.Connected {
		fmt.Fprintln(os.Stderr, "WARN: paired but not connected")
		return 2
	}
	fmt.Println("OK: paired, trusted, connected")
	return 0
}

// dumpDevProps records the device's UUIDs at failure time — the fastest way
// to tell "bonded but HID never attached" from "never bonded".
func (b *BlueZBackend) dumpDevProps(path dbus.ObjectPath) {
	props := b.getDeviceProps(path)
	if props == nil {
		tracef("props: GetAll failed for %s", path)
		return
	}
	for _, k := range []string{"Address", "Name", "Paired", "Bonded", "Trusted",
		"Connected", "Blocked", "LegacyPairing", "ServicesResolved", "UUIDs", "Icon"} {
		if v, ok := props[k]; ok {
			tracef("prop %-16s = %v", k, v.Value())
		}
	}
}

// replaceMAC turns 04:69:F8:DA:18:E4 into 04_69_F8_DA_18_E4 for D-Bus paths.
func replaceMAC(mac string) string {
	out := make([]rune, 0, 17)
	for _, r := range mac {
		if r == ':' || r == '-' {
			out = append(out, '_')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
