// bt.go — the bluetoothctl backend for navi-bluetooth.
//
// bluetoothctl is interactive-only, so every command is driven over stdin
// with a terminating "quit":
//
//	printf 'devices\nquit\n' | bluetoothctl
//
// The parser anchors on the stable line shapes ("Device <MAC> <name>",
// "Paired: yes", …) and ignores prompt echoes ("[bluetooth]#") and chatter
// ("Agent registered"), because those vary between BlueZ versions.
//
// Pairing is the fiddly part: BlueZ may ask for passkey confirmation, a
// PIN, or service authorization mid-pair. startPair keeps one bluetoothctl
// child alive, classifies its stdout lines, and hands prompts to the TUI
// over channels — the TUI answers, the session writes the answer back.

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// btTimeout bounds every one-shot bluetoothctl invocation.
const btTimeout = 15 * time.Second

// device is one bluetooth device as bluetoothd knows it.
type device struct {
	mac       string
	name      string
	paired    bool
	connected bool
	battery   int // -1 = unknown
}

// adapterState is the controller state from `show`.
type adapterState struct {
	present      bool
	name         string
	powered      bool
	discoverable bool
	discovering  bool
}

// snapshot is one full refresh of radio state.
type snapshot struct {
	adapter adapterState
	devs    []device
}

var (
	// ^Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4
	deviceLineRe = regexp.MustCompile(`^Device\s+([0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5})\s+(.*)$`)
	macRe        = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)
	// Battery Percentage: 0x52 (82)
	batteryRe    = regexp.MustCompile(`Battery Percentage:\s*(?:0x[0-9a-fA-F]+\s*)?\((\d+)\)`)
	batteryHexRe = regexp.MustCompile(`Battery Percentage:\s*0x([0-9a-fA-F]+)`)
	// upower -i: "  percentage:          82%"
	upowerPctRe = regexp.MustCompile(`(?i)^\s*percentage:\s*(\d+)%`)
)

// btRun drives one-shot bluetoothctl: each command on its own line,
// terminated by "quit". Returns combined stdout+stderr.
func btRun(commands ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), btTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bluetoothctl")
	cmd.Stdin = strings.NewReader(strings.Join(append(commands, "quit"), "\n") + "\n")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// parseDeviceLines extracts devices from `devices`-family output.
func parseDeviceLines(out string) []device {
	var devs []device
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		m := deviceLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		mac := strings.ToUpper(m[1])
		if seen[mac] {
			continue
		}
		seen[mac] = true
		name := strings.TrimSpace(m[2])
		// If the name is just the MAC (unresolved), try `info` for the real name.
		if name == "" || strings.ToUpper(name) == mac {
			if n, ok := deviceName(mac); ok {
				name = n
			} else {
				name = mac // last resort: show the MAC
			}
		}
		devs = append(devs, device{mac: mac, name: name, battery: -1})
	}
	return devs
}

// deviceName gets the friendly name via `bluetoothctl info`.
func deviceName(mac string) (string, bool) {
	out, err := btRun("info " + mac)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name:") {
			n := strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
			if n != "" {
				return n, true
			}
		}
		// Some BlueZ versions use "Alias:" for the friendly name.
		if strings.HasPrefix(line, "Alias:") {
			n := strings.TrimSpace(strings.TrimPrefix(line, "Alias:"))
			if n != "" {
				return n, true
			}
		}
	}
	return "", false
}

// listSnapshot refreshes everything: adapter state, all devices, paired
// set, connected set, and battery for connected devices.
func listSnapshot() (snapshot, error) {
	var snap snapshot

	showOut, err := btRun("show")
	if err != nil {
		return snap, fmt.Errorf("bluetoothctl show: %w", err)
	}
	snap.adapter = parseAdapter(showOut)
	if !snap.adapter.present {
		return snap, fmt.Errorf("no bluetooth adapter found")
	}

	devOut, _ := btRun("devices")
	pairedOut, _ := btRun("paired-devices")
	connOut, _ := btRun("devices Connected")

	all := parseDeviceLines(devOut)
	paired := map[string]bool{}
	for _, d := range parseDeviceLines(pairedOut) {
		paired[d.mac] = true
	}
	conn := map[string]bool{}
	for _, d := range parseDeviceLines(connOut) {
		conn[d.mac] = true
	}
	for i := range all {
		all[i].paired = paired[all[i].mac]
		all[i].connected = conn[all[i].mac]
		if all[i].connected {
			if pct, ok := deviceBattery(all[i].mac); ok {
				all[i].battery = pct
			}
		}
	}
	snap.devs = all
	return snap, nil
}

// parseAdapter reads `show` output.
func parseAdapter(out string) adapterState {
	var a adapterState
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Controller "):
			a.present = true
			// "Controller 00:11:22:33:44:55 (public)" — name comes on Name: line
		case strings.HasPrefix(line, "Name:"):
			a.name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "Powered:"):
			a.powered = strings.HasSuffix(line, "yes")
		case strings.HasPrefix(line, "Discoverable:"):
			a.discoverable = strings.HasSuffix(line, "yes")
		case strings.HasPrefix(line, "Discovering:"):
			a.discovering = strings.HasSuffix(line, "yes")
		}
	}
	return a
}

// deviceBattery prefers `bluetoothctl info`'s Battery Percentage and falls
// back to upower. ok=false when neither reports.
func deviceBattery(mac string) (pct int, ok bool) {
	out, err := btRun("info " + mac)
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			if m := batteryRe.FindStringSubmatch(line); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					return n, true
				}
			}
		}
		for _, line := range strings.Split(out, "\n") {
			if m := batteryHexRe.FindStringSubmatch(line); m != nil {
				if n, err := strconv.ParseInt(m[1], 16, 32); err == nil {
					return int(n), true
				}
			}
		}
	}
	return upowerBattery(mac)
}

// upowerBattery finds the upower device whose path carries the MAC
// (upower renders it with underscores: dev_4C_87_5D_AA_BB_CC).
func upowerBattery(mac string) (int, bool) {
	if _, err := exec.LookPath("upower"); err != nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "upower", "-e").Output()
	if err != nil {
		return 0, false
	}
	needle := strings.ReplaceAll(strings.ToUpper(mac), ":", "_")
	var path string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(strings.ToUpper(line), needle) {
			path = strings.TrimSpace(line)
			break
		}
	}
	if path == "" {
		return 0, false
	}
	out, err = exec.CommandContext(ctx, "upower", "-i", path).Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if m := upowerPctRe.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// isAppleDevice reports whether the device name looks like Apple hardware.
// Apple HID devices (Magic Keyboard/Mouse/Trackpad) are finicky: they need
// explicit trust before the HID profile will establish, and they benefit
// from a connect right after trust.
func isAppleDevice(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "apple") ||
		strings.Contains(n, "magic keyboard") ||
		strings.Contains(n, "magic mouse") ||
		strings.Contains(n, "magic trackpad")
}

// connectAppleDevice trusts then connects — the sequence Apple HID needs.
func connectAppleDevice(mac string) error {
	if err := trustDevice(mac, true); err != nil {
		return fmt.Errorf("trust: %w", err)
	}
	// Brief pause: BlueZ needs a moment after trust before HID connects.
	time.Sleep(500 * time.Millisecond)
	return connectDevice(mac)
}

// btAction runs a single command and reports success by looking for a
// success marker; on failure it returns the last meaningful output line.
func btAction(cmd string, okMarkers ...string) error {
	out, err := btRun(cmd)
	if err != nil {
		return fmt.Errorf("%s: %v", cmd, lastLine(out))
	}
	for _, mk := range okMarkers {
		if strings.Contains(out, mk) {
			return nil
		}
	}
	return fmt.Errorf("%s: %v", cmd, lastLine(out))
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "[bluetooth]") {
			continue
		}
		return l
	}
	return "no response"
}

func connectDevice(mac string) error {
	return btAction("connect "+mac, "Connection successful")
}

func disconnectDevice(mac string) error {
	return btAction("disconnect "+mac, "Successful disconnected")
}

func removeDevice(mac string) error {
	return btAction("remove "+mac, "Device has been removed")
}

func trustDevice(mac string, trust bool) error {
	verb := "trust"
	if !trust {
		verb = "untrust"
	}
	return btAction(verb+" "+mac, "trust succeeded")
}

func setPower(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	return btAction("power "+v, "Changing power "+v+" succeeded")
}

func setDiscoverable(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	return btAction("discoverable "+v, "Changing discoverable "+v+" succeeded")
}

func setScan(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	marker := "Discovery started"
	if !on {
		marker = "Discovery stopped"
	}
	return btAction("scan "+v, marker)
}

// ── pairing session ─────────────────────────────────────────────────

// pairEventKind classifies one line of pair-session output.
type pairEventKind int

const (
	pairPromptPasskey   pairEventKind = iota // text = "583920" — confirm yes/no
	pairPromptPIN                            // enter PIN digits
	pairPromptAuthorize                      // text = service desc — yes/no
	pairDone
	pairFailed // text = reason
)

type pairEvent struct {
	kind pairEventKind
	text string
}

var (
	passkeyRe   = regexp.MustCompile(`Confirm passkey\s+(\d+)`)
	pinRe       = regexp.MustCompile(`(?i)(Request PIN code|Enter PIN)`)
	authorizeRe = regexp.MustCompile(`Authorize service\s+(.*?)\s*\(yes/no\)`)
)

// classifyPairLine maps one bluetoothctl stdout line to a pair event.
// ok=false means "chatter, keep scanning".
func classifyPairLine(line string) (ev pairEvent, ok bool) {
	if m := passkeyRe.FindStringSubmatch(line); m != nil {
		return pairEvent{kind: pairPromptPasskey, text: m[1]}, true
	}
	if pinRe.FindString(line) != "" {
		return pairEvent{kind: pairPromptPIN}, true
	}
	if m := authorizeRe.FindStringSubmatch(line); m != nil {
		return pairEvent{kind: pairPromptAuthorize, text: strings.TrimSpace(m[1])}, true
	}
	switch {
	case strings.Contains(line, "Pairing successful"):
		return pairEvent{kind: pairDone}, true
	case strings.Contains(line, "Paired: yes") && strings.Contains(line, "[CHG]"):
		return pairEvent{kind: pairDone}, true
	case strings.Contains(line, "Failed to pair"):
		rest := line[strings.Index(line, "Failed to pair")+len("Failed to pair"):]
		rest = strings.TrimPrefix(strings.TrimSpace(rest), ":")
		return pairEvent{kind: pairFailed, text: strings.TrimSpace(rest)}, true
	}
	return pairEvent{}, false
}

// pairSession is one live bluetoothctl child doing a pair. The TUI reads
// prompts from Events and writes answers to Answers.
type pairSession struct {
	mac     string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	Events  chan pairEvent
	Answers chan string
	done    chan struct{}
	closed  chan struct{}
}

func startPair(mac string) (*pairSession, error) {
	cmd := exec.Command("bluetoothctl")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard // keep child chatter out of the TUI
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &pairSession{
		mac:     mac,
		cmd:     cmd,
		stdin:   stdin,
		Events:  make(chan pairEvent, 8),
		Answers: make(chan string, 8),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
	}
	go s.loop(bufio.NewScanner(stdout))
	fmt.Fprintf(stdin, "pair %s\n", mac)
	return s, nil
}

func (s *pairSession) loop(scan *bufio.Scanner) {
	defer close(s.done)
	answered := map[pairEventKind]bool{}
	for scan.Scan() {
		ev, ok := classifyPairLine(scan.Text())
		if !ok {
			continue
		}
		switch ev.kind {
		case pairPromptPasskey, pairPromptPIN, pairPromptAuthorize:
			// Answer each prompt kind at most once; a repeated prompt
			// after an answer means the peer rejected it.
			if answered[ev.kind] {
				s.Events <- pairEvent{kind: pairFailed, text: "peer rejected the answer"}
				return
			}
			answered[ev.kind] = true
			s.Events <- ev
			select {
			case ans := <-s.Answers:
				fmt.Fprintf(s.stdin, "%s\n", ans)
			case <-s.closed:
				return
			case <-time.After(60 * time.Second):
				s.Events <- pairEvent{kind: pairFailed, text: "prompt timed out"}
				return
			}
		case pairDone, pairFailed:
			s.Events <- ev
			return
		}
	}
	// stdout closed without a verdict (child died?)
	select {
	case s.Events <- pairEvent{kind: pairFailed, text: "bluetoothctl exited unexpectedly"}:
	default:
	}
}

// Close ends the session: quit the child, don't leave it orphaned.
func (s *pairSession) Close() {
	select {
	case <-s.closed:
		return // already closed
	default:
		close(s.closed)
	}
	fmt.Fprintln(s.stdin, "quit")
	s.stdin.Close()
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
	}
}
