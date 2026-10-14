package main

// Hermetic D-Bus tests for the BlueZ backend. No real adapter, no root, no
// hardware: a private dbus-daemon hosts an in-process fake BlueZ.

import (
	"bufio"
	"os/exec"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeDevice stands in for an org.bluez.Device1.
//
// Pair deliberately takes no arguments, exactly like the real Device1.Pair.
// That makes the test below a true wire assertion: a call carrying zero
// arguments succeeds, while any stray argument — the trailing nil this
// guards against — comes back as org.freedesktop.DBus.Error.InvalidArgs.
// (A variadic handler cannot be used here: godbus counts the variadic
// parameter as one argument, so it rejects a legitimately empty call.)
type fakeDevice struct {
	calls chan struct{}
}

func (f *fakeDevice) Pair() *dbus.Error {
	select {
	case f.calls <- struct{}{}:
	default:
	}
	return nil
}

const fakeKeyboardPath = dbus.ObjectPath("/org/bluez/hci0/dev_04_69_F8_DA_18_E4")

// privateBus starts a throwaway message bus and returns its address. The
// daemon is left running and killed on cleanup, so the test owns it.
//
// Both ends of the test share this one bus: a second daemon would be a
// second bus, and the fake org.bluez would be invisible from it.
func privateBus(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}
	// --nofork keeps the daemon in the foreground so we can own its
	// lifetime; read the address off its stdout instead of waiting for it
	// to exit.
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	type addrResult struct {
		addr string
		err  error
	}
	ch := make(chan addrResult, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		ch <- addrResult{addr: trimNewline(line), err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Skipf("dbus-daemon produced no address: %v", r.err)
		}
		if r.addr == "" {
			t.Skip("dbus-daemon printed an empty address")
		}
		return r.addr
	case <-time.After(10 * time.Second):
		t.Skip("timed out waiting for the private bus address")
		return ""
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// dialBus connects to a message bus and completes the handshake. dbus.Dial
// deliberately hands back an unauthenticated connection — without Auth and
// Hello the daemon never routes anything to us and every call just hangs.
func dialBus(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Dial(addr)
	if err != nil {
		t.Fatalf("dial bus %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Auth(nil); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := conn.Hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}
	return conn
}

// TestPairAsyncSendsNoArguments is the regression guard for the bug that
// made every pairing attempt die silently.
//
// org.bluez.Device1.Pair takes no arguments, and Object.Go's signature is
// Go(method, flags, ch, args ...interface{}) — so passing a trailing nil
// does not mean "no arguments", it means "one nil argument". godbus then
// computes SignatureOf(nil), dereferences it, and panics before a single
// byte reaches BlueZ. Because the call sat inside a Bubble Tea command that
// panic killed the process somewhere nobody could see it, and pairing simply
// never happened.
//
// This asserts on the wire: the fake BlueZ must receive Pair with an empty
// argument list, and PairAsync must not panic on the way there.
func TestPairAsyncSendsNoArguments(t *testing.T) {
	addr := privateBus(t)

	// The fake BlueZ side.
	fakeBus := dialBus(t, addr)
	dev := &fakeDevice{calls: make(chan struct{}, 4)}
	if err := fakeBus.Export(dev, fakeKeyboardPath, "org.bluez.Device1"); err != nil {
		t.Fatalf("export fake device: %v", err)
	}
	if _, err := fakeBus.RequestName("org.bluez", 0); err != nil {
		t.Fatalf("own org.bluez on the private bus: %v", err)
	}

	// The navi-bluetooth side.
	b := &BlueZBackend{conn: dialBus(t, addr)}

	done := make(chan error, 1)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("PairAsync panicked: %v", r)
			}
		}()
		b.PairAsync(fakeKeyboardPath, func(err error) { done <- err })
	}()

	select {
	case <-dev.calls:
		// Reached the fake with an empty argument list. Good.
	case err := <-done:
		t.Fatalf("Pair returned before the fake device saw the call: %v "+
			"(a stray argument shows up here as InvalidArgs)", err)
	case <-time.After(10 * time.Second):
		t.Fatal("fake BlueZ never received Device1.Pair")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PairAsync reported %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("PairAsync never completed")
	}
}

// TestNilArgPanicsSignatureOf pins the mechanism, so the reason PairAsync
// takes no arguments is written down where the next editor will find it.
// SignatureOf on a nil argument is a nil dereference — this is the exact
// crash the trailing nil produced.
func TestNilArgPanicsSignatureOf(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SignatureOf(nil) did not panic; the trailing-nil trap " +
				"this guards may have changed")
		}
	}()
	// A trailing nil in a variadic call becomes one real argument.
	args := []interface{}{nil}
	_ = dbus.SignatureOf(args...)
}
