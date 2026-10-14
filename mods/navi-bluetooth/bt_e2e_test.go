package main

// End-to-end backend tests against a fake bluetoothctl written to a temp
// dir. Hermetic: no /tmp fixtures, no real BlueZ needed.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fakeOneShot = `#!/bin/bash
echo "Agent registered"
while IFS= read -r line; do
  case "$line" in
    quit) echo "[bluetooth]# quit"; break;;
    show) echo "[bluetooth]# show"; cat <<'SHOW'
Controller 00:11:22:33:44:55 (public)
	Name: navi
	Alias: navi
	Powered: yes
	Discoverable: no
	Pairable: yes
	Discovering: no
SHOW
      echo "[bluetooth]# ";;
    "devices Connected") echo "Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4";;
    "paired-devices") echo "Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4"; echo "Device 00:1A:7D:11:22:33 ThinkPad TrackPoint Keyboard II";;
    devices) echo "Device 4C:87:5D:AA:BB:CC Sony WH-1000XM4"; echo "Device 00:1A:7D:11:22:33 ThinkPad TrackPoint Keyboard II"; echo "Device A4:CF:12:9B:3D:E1 JBL Flip 6";;
    "info 4C:87:5D:AA:BB:CC") printf '\tPaired: yes\n\tConnected: yes\n\tBattery Percentage: 0x52 (82)\n';;
    info*) printf '\tPaired: yes\n\tConnected: no\n';;
    "scan on") echo "Discovery started";;
    "scan off") echo "Discovery stopped";;
    "power on") echo "Changing power on succeeded";;
    "power off") echo "Changing power off succeeded";;
    "discoverable on") echo "Changing discoverable on succeeded";;
    "discoverable off") echo "Changing discoverable off succeeded";;
    "connect "*) echo "Connection successful";;
    "disconnect "*) echo "Successful disconnected";;
    "remove "*) echo "Device has been removed";;
    "trust "*) echo "Changing trust succeeded";;
  esac
done
`

const fakePairCeremony = `#!/bin/bash
echo "Agent registered"
echo "[bluetooth]# "
while IFS= read -r line; do
  case "$line" in
    "pair 4C:87:5D:AA:BB:CC")
      echo "Attempting to pair with 4C:87:5D:AA:BB:CC"
      sleep 0.2
      echo "[agent] Confirm passkey 583920 (yes/no): "
      IFS= read -r ans
      if [ "$ans" = "yes" ]; then
        echo "[CHG] Device 4C:87:5D:AA:BB:CC Paired: yes"
        echo "Pairing successful"
      else
        echo "Failed to pair: org.bluez.Error.AuthenticationRejected"
      fi
      echo "[bluetooth]# ";;
    quit) break;;
  esac
done
`

func withFakeBT(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "bluetoothctl")
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestE2EListSnapshot(t *testing.T) {
	withFakeBT(t, fakeOneShot)
	snap, err := listSnapshot()
	if err != nil {
		t.Fatalf("listSnapshot: %v", err)
	}
	if !snap.adapter.present || !snap.adapter.powered {
		t.Fatalf("bad adapter: %+v", snap.adapter)
	}
	if len(snap.devs) != 3 {
		t.Fatalf("want 3 devs, got %d", len(snap.devs))
	}
	byMAC := map[string]device{}
	for _, d := range snap.devs {
		byMAC[d.mac] = d
	}
	sony := byMAC["4C:87:5D:AA:BB:CC"]
	if !sony.connected || !sony.paired || sony.battery != 82 {
		t.Fatalf("bad sony: %+v", sony)
	}
	kb := byMAC["00:1A:7D:11:22:33"]
	if kb.connected || !kb.paired {
		t.Fatalf("bad keyboard: %+v", kb)
	}
	jbl := byMAC["A4:CF:12:9B:3D:E1"]
	if jbl.connected || jbl.paired {
		t.Fatalf("bad JBL: %+v", jbl)
	}
}

func TestE2EActions(t *testing.T) {
	withFakeBT(t, fakeOneShot)
	for name, fn := range map[string]func(string) error{
		"connect":    connectDevice,
		"disconnect": disconnectDevice,
		"remove":     removeDevice,
	} {
		if err := fn("4C:87:5D:AA:BB:CC"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := trustDevice("4C:87:5D:AA:BB:CC", true); err != nil {
		t.Fatalf("trust: %v", err)
	}
	for name, fn := range map[string]func(bool) error{
		"power":        setPower,
		"discoverable": setDiscoverable,
		"scan":         setScan,
	} {
		if err := fn(true); err != nil {
			t.Fatalf("%s on: %v", name, err)
		}
		if err := fn(false); err != nil {
			t.Fatalf("%s off: %v", name, err)
		}
	}
}

func TestE2EPairPasskeyYes(t *testing.T) {
	withFakeBT(t, fakePairCeremony)
	sess, err := startPair("4C:87:5D:AA:BB:CC")
	if err != nil {
		t.Fatalf("startPair: %v", err)
	}
	defer sess.Close()
	select {
	case ev := <-sess.Events:
		if ev.kind != pairPromptPasskey || ev.text != "583920" {
			t.Fatalf("want passkey 583920, got %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no passkey prompt arrived")
	}
	sess.Answers <- "yes"
	select {
	case ev := <-sess.Events:
		if ev.kind != pairDone {
			t.Fatalf("want pairDone, got %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no pair verdict arrived")
	}
}

func TestE2EPairPasskeyNo(t *testing.T) {
	withFakeBT(t, fakePairCeremony)
	sess, err := startPair("4C:87:5D:AA:BB:CC")
	if err != nil {
		t.Fatalf("startPair: %v", err)
	}
	defer sess.Close()
	select {
	case ev := <-sess.Events:
		if ev.kind != pairPromptPasskey {
			t.Fatalf("want passkey prompt, got %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no passkey prompt arrived")
	}
	sess.Answers <- "no"
	select {
	case ev := <-sess.Events:
		if ev.kind != pairFailed {
			t.Fatalf("want pairFailed, got %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no pair verdict arrived")
	}
}
