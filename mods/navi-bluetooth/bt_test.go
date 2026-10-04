package main

import (
	"strings"
	"testing"
)

// Realistic `printf 'devices\nquit\n' | bluetoothctl` output: prompt
// echoes and agent chatter interleaved with Device lines.
const sampleDevices = `Agent registered
[bluetooth]# devices
Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4
Device 00:1A:7D:11:22:33 ThinkPad TrackPoint Keyboard II
Device A4:CF:12:9B:3D:E1 JBL Flip 6
[bluetooth]# quit
`

const samplePaired = `Agent registered
[bluetooth]# paired-devices
Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4
Device 00:1A:7D:11:22:33 ThinkPad TrackPoint Keyboard II
[bluetooth]# quit
`

const sampleConnected = `Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4
`

// Real `bluetoothctl info` shape for a headset with battery.
const sampleInfoBattery = `Device 4C:87:5D:AA:BB:CC (public)
	Name: Sony WH-1000XM4
	Alias: Sony WH-1000XM4
	Class: 0x00240404
	Icon: audio-headset
	Paired: yes
	Bonded: yes
	Trusted: yes
	Blocked: no
	Connected: yes
	LegacyPairing: no
	RSSI: -53
	Battery Percentage: 0x52 (82)
	UUID: 0000110b-0000-1000-8000-00805f9b34fb
`

// A keyboard: no battery line, no RSSI.
const sampleInfoNoBattery = `Device 00:1A:7D:11:22:33 (public)
	Name: ThinkPad TrackPoint Keyboard II
	Alias: ThinkPad TrackPoint Keyboard II
	Class: 0x00000540
	Icon: input-keyboard
	Paired: yes
	Trusted: no
	Blocked: no
	Connected: no
	LegacyPairing: no
`

const sampleShow = `Controller 00:11:22:33:44:55 (public)
	Name: navi
	Alias: navi
	Class: 0x1c010c
	Powered: yes
	Discoverable: no
	DiscoverableTimeout: 0x000000b4
	Pairable: yes
	UUID: 00001800-0000-1000-8000-00805f9b34fb
	Modalias: usb:v1D6Bp0246d0548
	Discovering: yes
`

func TestParseDeviceLines(t *testing.T) {
	devs := parseDeviceLines(sampleDevices)
	if len(devs) != 3 {
		t.Fatalf("want 3 devices, got %d", len(devs))
	}
	if devs[0].mac != "4C:87:5D:AA:BB:CC" || devs[0].name != "Sony WH-1000XM4" {
		t.Fatalf("bad first device: %+v", devs[0])
	}
	if devs[2].name != "JBL Flip 6" {
		t.Fatalf("bad third device: %+v", devs[2])
	}
	for _, d := range devs {
		if d.battery != -1 {
			t.Fatalf("battery should default to unknown: %+v", d)
		}
	}
}

func TestParseDeviceLinesDedupes(t *testing.T) {
	out := sampleDevices + "Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4\n"
	if n := len(parseDeviceLines(out)); n != 3 {
		t.Fatalf("want 3 after dedupe, got %d", n)
	}
}

func TestParseDeviceLinesLowercaseMAC(t *testing.T) {
	devs := parseDeviceLines("Device 4c:87:5d:aa:bb:cc Lower Case\n")
	if len(devs) != 1 || devs[0].mac != "4C:87:5D:AA:BB:CC" {
		t.Fatalf("MAC should normalize to upper: %+v", devs)
	}
}

func TestParseAdapter(t *testing.T) {
	a := parseAdapter(sampleShow)
	if !a.present || !a.powered || !a.discovering {
		t.Fatalf("bad adapter flags: %+v", a)
	}
	if a.discoverable {
		t.Fatalf("should not be discoverable: %+v", a)
	}
	if a.name != "navi" {
		t.Fatalf("bad adapter name: %+v", a)
	}
}

func TestParseAdapterAbsent(t *testing.T) {
	a := parseAdapter("No default controller available\n")
	if a.present {
		t.Fatalf("should not detect an adapter: %+v", a)
	}
}

func TestBatteryFromInfo(t *testing.T) {
	for _, line := range strings.Split(sampleInfoBattery, "\n") {
		if m := batteryRe.FindStringSubmatch(line); m != nil {
			if m[1] != "82" {
				t.Fatalf("want 82, got %s", m[1])
			}
			return
		}
	}
	t.Fatal("battery line not matched")
}

func TestBatteryAbsent(t *testing.T) {
	for _, line := range strings.Split(sampleInfoNoBattery, "\n") {
		if batteryRe.FindString(line) != "" || batteryHexRe.FindString(line) != "" {
			t.Fatalf("false battery match on %q", line)
		}
	}
}

func TestUpowerPctParse(t *testing.T) {
	out := "  native-path:          headset_dev_4C_87_5D_AA_BB_CC\n  percentage:          82%\n"
	found := false
	for _, line := range strings.Split(out, "\n") {
		if m := upowerPctRe.FindStringSubmatch(line); m != nil {
			found = true
			if m[1] != "82" {
				t.Fatalf("want 82, got %s", m[1])
			}
		}
	}
	if !found {
		t.Fatal("upower percentage not matched")
	}
}

func TestClassifyPairLine(t *testing.T) {
	cases := []struct {
		line string
		kind pairEventKind
		text string
	}{
		{`[agent] Confirm passkey 583920 (yes/no): `, pairPromptPasskey, "583920"},
		{`[agent] Request PIN code`, pairPromptPIN, ""},
		{`Enter PIN code: `, pairPromptPIN, ""},
		{`[agent] Authorize service 0000110b-0000-1000-8000-00805f9b34fb (yes/no):`, pairPromptAuthorize, "0000110b-0000-1000-8000-00805f9b34fb"},
		{`Pairing successful`, pairDone, ""},
		{`[CHG] Device 4C:87:5D:AA:BB:CC Paired: yes`, pairDone, ""},
		{`Failed to pair: org.bluez.Error.AuthenticationFailed`, pairFailed, "org.bluez.Error.AuthenticationFailed"},
		{`Failed to pair: org.bluez.Error.AlreadyExists`, pairFailed, "org.bluez.Error.AlreadyExists"},
	}
	for _, c := range cases {
		ev, ok := classifyPairLine(c.line)
		if !ok {
			t.Fatalf("line not classified: %q", c.line)
		}
		if ev.kind != c.kind || ev.text != c.text {
			t.Fatalf("line %q: want (%d,%q), got (%d,%q)",
				c.line, c.kind, c.text, ev.kind, ev.text)
		}
	}
}

func TestClassifyPairLineIgnoresChatter(t *testing.T) {
	chatter := []string{
		`Agent registered`,
		`[bluetooth]# pair 4C:87:5D:AA:BB:CC`,
		`Attempting to pair with 4C:87:5D:AA:BB:CC`,
		`[CHG] Device 4C:87:5D:AA:BB:CC RSSI: -53`,
		`[CHG] Device 4C:87:5D:AA:BB:CC Connected: yes`,
		``,
	}
	for _, line := range chatter {
		if _, ok := classifyPairLine(line); ok {
			t.Fatalf("chatter misclassified as event: %q", line)
		}
	}
}

func TestValidMAC(t *testing.T) {
	if !macRe.MatchString("4C:87:5D:AA:BB:CC") {
		t.Fatal("valid MAC rejected")
	}
	if macRe.MatchString("not-a-mac") {
		t.Fatal("invalid MAC accepted")
	}
}

func TestIsInputDevice(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Keyboard", true},                    // BlueZ generic name
		{"Magic Keyboard", true},              // Apple
		{"Apple Magic Mouse", true},           // Apple
		{"Logitech MX Keys", true},            // brand keyboard
		{"My Mouse", true},                    // generic mouse
		{"Trackpad", true},                    // trackpad
		{"AirPods Pro", false},                // audio, not input
		{"Pixel 8", false},                    // phone, not input
		{"JBL Speaker", false},                // audio
		{"", false},                           // empty
	}
	for _, c := range cases {
		if got := isInputDevice(c.name); got != c.want {
			t.Errorf("isInputDevice(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDisplayNameNeverBareMAC(t *testing.T) {
	// A device with no name must not show a bare MAC.
	d := BlueZDevice{Address: "04:69:F8:DA:18:E4", Kind: DeviceKeyboard}
	if got := d.DisplayName(); got == "04:69:F8:DA:18:E4" {
		t.Errorf("DisplayName returned bare MAC: %q", got)
	}
	// Named device shows its name.
	d2 := BlueZDevice{Address: "04:69:F8:DA:18:E4", Name: "Magic Keyboard", Kind: DeviceKeyboard}
	if got := d2.DisplayName(); got != "Magic Keyboard" {
		t.Errorf("DisplayName = %q, want %q", got, "Magic Keyboard")
	}
}
