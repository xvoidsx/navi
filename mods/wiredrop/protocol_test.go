package main

import (
	"crypto/tls"
	"encoding/json"
	"strings"
	"testing"
)

// The announce shape must carry port/protocol/announce:true…
func TestAnnounceShape(t *testing.T) {
	yes := true
	model := "navi"
	d := DeviceInfo{
		Alias: "cloudbook", Version: ProtoVersion, DeviceModel: &model,
		DeviceType: "desktop", Fingerprint: "abc123",
		Port: 53317, Protocol: "https", Download: false, Announce: &yes,
	}
	data, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	for _, k := range []string{"alias", "version", "deviceModel", "deviceType", "fingerprint", "port", "protocol", "download", "announce"} {
		if _, ok := m[k]; !ok {
			t.Errorf("announce missing key %q: %s", k, data)
		}
	}
	if m["announce"] != true || m["version"] != "2.2" {
		t.Errorf("bad announce values: %s", data)
	}
}

// …while the /register response must OMIT port/protocol/announce
// (the spec documents a leaner response shape).
func TestRegisterResponseShape(t *testing.T) {
	d := DeviceInfo{
		Alias: "cloudbook", Version: ProtoVersion,
		DeviceType: "desktop", Fingerprint: "abc123",
		Port: 53317, Protocol: "https", Announce: func() *bool { b := true; return &b }(),
	}
	data, _ := json.Marshal(d.infoResponse())
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	for _, k := range []string{"port", "protocol", "announce"} {
		if _, ok := m[k]; ok {
			t.Errorf("register response must omit %q: %s", k, data)
		}
	}
	for _, k := range []string{"alias", "version", "deviceType", "fingerprint", "download"} {
		if _, ok := m[k]; !ok {
			t.Errorf("register response missing key %q: %s", k, data)
		}
	}
}

// deviceModel must serialize as null when unknown (spec: nullable).
func TestDeviceModelNullable(t *testing.T) {
	d := DeviceInfo{Alias: "x", Version: "2.2", DeviceType: "desktop", Fingerprint: "f"}
	data, _ := json.Marshal(d.infoResponse())
	if !strings.Contains(string(data), `"deviceModel":null`) {
		t.Errorf("deviceModel should be null, got: %s", data)
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	dir := t.TempDir()
	cert, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	x1, err := parseCert(cert)
	if err != nil {
		t.Fatal(err)
	}
	fp1 := fingerprintOf(x1)
	// Reload from disk — the fingerprint must be stable (TOFU depends on it).
	cert2, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	x2, _ := parseCert(cert2)
	if fp2 := fingerprintOf(x2); fp1 != fp2 {
		t.Errorf("fingerprint not stable across reload: %s vs %s", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Errorf("fingerprint should be 64 hex chars, got %d", len(fp1))
	}
	var _ tls.Certificate = cert
}

func TestFingerprintWords(t *testing.T) {
	fp := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	w1 := fingerprintWords(fp)
	w2 := fingerprintWords(fp)
	if w1 != w2 {
		t.Errorf("words not deterministic: %q vs %q", w1, w2)
	}
	if len(strings.Fields(w1)) != 6 {
		t.Errorf("want 6 words, got %q", w1)
	}
	// Different fingerprints -> different words (almost surely).
	fp2 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if fingerprintWords(fp2) == w1 {
		t.Errorf("word encoding not sensitive to input")
	}
	if fingerprintWords("zzz") != "(unreadable fingerprint)" {
		t.Errorf("bad hex should give the unreadable marker")
	}
}

func TestWordlistSize(t *testing.T) {
	if len(wordlist) != 256 {
		t.Errorf("wordlist has %d entries, want 256", len(wordlist))
	}
}
