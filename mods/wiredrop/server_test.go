package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// testServer spins a full Server on 127.0.0.1 with a throwaway config.
func testServer(t *testing.T, pin string) (*Server, *KnownHosts, string, func()) {
	t.Helper()
	dir := t.TempDir()
	downloads := filepath.Join(dir, "downloads")
	_ = os.MkdirAll(downloads, 0o755)
	cfg := &Config{Alias: "testpeer", Port: 53317, PIN: pin, DownloadDir: downloads}
	paths := &Paths{ConfigDir: dir, CertDir: dir, RuntimeDir: dir, KnownHosts: filepath.Join(dir, "kh")}
	cert, err := loadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	x509Cert, err := parseCert(cert)
	if err != nil {
		t.Fatal(err)
	}
	peers := NewPeerCache(filepath.Join(dir, "peers.json"))
	known, err := LoadKnownHosts(filepath.Join(dir, "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	model := "test"
	info := DeviceInfo{Alias: "testpeer", Version: ProtoVersion, DeviceModel: &model,
		DeviceType: "desktop", Fingerprint: fingerprintOf(x509Cert), Port: 0, Protocol: "https"}
	srv := NewServer(cfg, paths, info, cert, peers, known)
	// Tests drive the full exchange: auto-accept consent so prepare-upload
	// returns a live session instead of the production default-decline.
	srv.SetConsent(func(*Session) Decision { return DecisionAccept })
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start([]string{"127.0.0.1"}, 0) }()
	deadline := time.Now().Add(5 * time.Second)
	for srv.ActualPort() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.ActualPort() == 0 {
		t.Fatal("server never bound")
	}
	_ = errCh
	return srv, known, fmt.Sprintf("https://127.0.0.1:%d", srv.ActualPort()), srv.Stop
}

// skipVerifyClient is what LocalSend peers (and our tests) use against
// self-signed certs.
func skipVerifyClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
}

func TestInfoRoute(t *testing.T) {
	_, _, base, stop := testServer(t, "")
	defer stop()
	resp, err := skipVerifyClient().Get(base + "/api/localsend/v2/info")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("info status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"version":"2.2"`) {
		t.Errorf("info body: %s", body)
	}
}

func TestBindScopeRefusesPublic(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Alias: "x", Port: 53317, DownloadDir: dir}
	paths := &Paths{ConfigDir: dir, CertDir: dir, RuntimeDir: dir}
	cert, _ := loadOrCreateCert(dir)
	peers := NewPeerCache(filepath.Join(dir, "peers.json"))
	model := "t"
	srv := NewServer(cfg, paths, DeviceInfo{Alias: "x", Version: ProtoVersion,
		DeviceModel: &model, DeviceType: "desktop", Fingerprint: "f"}, cert, peers, &KnownHosts{})
	// A non-local unicast address must be rejected outright.
	if err := srv.Start([]string{"8.8.8.8"}, 53317); err == nil {
		t.Errorf("Start on 8.8.8.8 succeeded — public binding must refuse")
	} else if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("wrong error: %v", err)
	}
	// So must a wildcard and an unparseable address.
	for _, bad := range []string{"0.0.0.0", "::", "not-an-ip"} {
		if err := srv.Start([]string{bad}, 53317); err == nil {
			t.Errorf("Start on %q succeeded — must refuse", bad)
		}
	}
}

func TestPIN401(t *testing.T) {
	_, _, base, stop := testServer(t, "1234")
	defer stop()
	c := skipVerifyClient()
	// No pin at all.
	resp, err := c.Post(base+"/api/localsend/v2/prepare-upload", "application/json",
		strings.NewReader(`{"info":{"alias":"x","version":"2.2","deviceType":"desktop","fingerprint":"y"},"files":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-pin prepare status = %d, want 401", resp.StatusCode)
	}
	// Wrong pin.
	resp2, err := c.Post(base+"/api/localsend/v2/prepare-upload?pin=9999", "application/json",
		strings.NewReader(`{"info":{"alias":"x","version":"2.2","deviceType":"desktop","fingerprint":"y"},"files":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong-pin prepare status = %d, want 401", resp2.StatusCode)
	}
	// Right pin passes the PIN gate (the test server auto-accepts
	// consent, so a valid prepare returns 200 with a session).
	resp3, err := c.Post(base+"/api/localsend/v2/prepare-upload?pin=1234", "application/json",
		strings.NewReader(`{"info":{"alias":"x","version":"2.2","deviceType":"desktop","fingerprint":"y"},"files":{"f1":{"id":"f1","fileName":"n.txt","size":1,"fileType":"text/plain"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("right-pin prepare status = %d, want 200", resp3.StatusCode)
	}
}

// TestFullUploadCycle drives the whole LocalSend exchange against a live
// server: prepare → upload → content verified on disk.
func TestFullUploadCycle(t *testing.T) {
	srv, _, base, stop := testServer(t, "")
	defer stop()
	c := skipVerifyClient()

	content := "hello wiredrop — this is the whole file in one PUT\n"
	sum := testSHA256([]byte(content))

	req := fmt.Sprintf(`{"info":{"alias":"sender","version":"2.2","deviceType":"desktop","fingerprint":"%s"},"files":{"f1":{"id":"f1","fileName":"hello.txt","size":%d,"fileType":"text/plain","sha256":"%s","preview":null,"metadata":null}}}`,
		"aa", len(content), sum)
	resp, err := c.Post(base+"/api/localsend/v2/prepare-upload", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("prepare status = %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Session is registered on the server; fetch its ID + token from the
	// server's session map (test-only access).
	sessID, token := "", ""
	srv.mu.Lock()
	for id, s := range srv.sessions {
		sessID, token = id, s.Files["f1"].token
	}
	srv.mu.Unlock()
	if sessID == "" {
		t.Fatal("no session registered")
	}

	up, err := c.Post(
		fmt.Sprintf("%s/api/localsend/v2/upload?sessionId=%s&fileId=f1&token=%s", base, sessID, token),
		"application/octet-stream", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	up.Body.Close()
	if up.StatusCode != 200 && up.StatusCode != 204 {
		t.Fatalf("upload status = %d", up.StatusCode)
	}

	// File must exist on disk with exact content.
	dir := srv.cfg.DownloadDir
	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("delivered file missing: %v", err)
	}
	if string(got) != content {
		t.Errorf("content mismatch: %q", got)
	}
}

func TestChecksumMismatch422(t *testing.T) {
	srv, _, base, stop := testServer(t, "")
	defer stop()
	c := skipVerifyClient()

	content := "corrupted bytes"
	wrongSum := testSHA256([]byte("something else entirely"))
	req := fmt.Sprintf(`{"info":{"alias":"sender","version":"2.2","deviceType":"desktop","fingerprint":"%s"},"files":{"f1":{"id":"f1","fileName":"bad.bin","size":%d,"fileType":"application/octet-stream","sha256":"%s"}}}`,
		"bb", len(content), wrongSum)
	resp, err := c.Post(base+"/api/localsend/v2/prepare-upload", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	srv.mu.Lock()
	sessID, token := "", ""
	for id, s := range srv.sessions {
		sessID, token = id, s.Files["f1"].token
	}
	srv.mu.Unlock()
	up, err := c.Post(
		fmt.Sprintf("%s/api/localsend/v2/upload?sessionId=%s&fileId=f1&token=%s", base, sessID, token),
		"application/octet-stream", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	up.Body.Close()
	if up.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("checksum mismatch status = %d, want 422", up.StatusCode)
	}
	// And no final file may remain.
	if _, err := os.Stat(filepath.Join(srv.cfg.DownloadDir, "bad.bin")); !os.IsNotExist(err) {
		t.Errorf("corrupt file landed on disk")
	}
}

func TestCancelClearsSession(t *testing.T) {
	srv, _, base, stop := testServer(t, "")
	defer stop()
	c := skipVerifyClient()
	req := `{"info":{"alias":"sender","version":"2.2","deviceType":"desktop","fingerprint":"cc"},"files":{"f1":{"id":"f1","fileName":"n.txt","size":1,"fileType":"text/plain"}}}`
	resp, err := c.Post(base+"/api/localsend/v2/prepare-upload", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	srv.mu.Lock()
	sessID := ""
	for id := range srv.sessions {
		sessID = id
	}
	srv.mu.Unlock()
	cancel, err := c.Post(base+"/api/localsend/v2/cancel?sessionId="+sessID, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel.Body.Close()
	if cancel.StatusCode != 200 && cancel.StatusCode != 204 {
		t.Errorf("cancel status = %d", cancel.StatusCode)
	}
	srv.mu.Lock()
	_, ok := srv.sessions[sessID]
	srv.mu.Unlock()
	if ok {
		t.Errorf("session still present after cancel")
	}
}

func TestTrustTOFU(t *testing.T) {
	kh, err := LoadKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if kh.Check("cloudbook", "fp1") != TrustUnknown {
		t.Errorf("fresh device should be unknown")
	}
	if err := kh.Confirm("cloudbook", "fp1"); err != nil {
		t.Fatal(err)
	}
	if kh.Check("cloudbook", "fp1") != TrustKnown {
		t.Errorf("pinned device should be known")
	}
	if kh.Check("cloudbook", "fp2") != TrustChanged {
		t.Errorf("changed fingerprint must report TrustChanged")
	}
	if err := kh.Confirm("cloudbook", "fp2"); err == nil {
		t.Errorf("re-pinning a changed fingerprint must be refused")
	}
	if !kh.Forget("cloudbook") {
		t.Errorf("forget should report true")
	}
	if kh.Check("cloudbook", "fp2") != TrustUnknown {
		t.Errorf("after forget, device is unknown again")
	}
	if err := kh.Confirm("cloudbook", "fp2"); err != nil {
		t.Errorf("pin after forget should work: %v", err)
	}
}

func TestConfigXDGDownloadDir(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "user-dirs.dirs"),
		[]byte("XDG_DOWNLOAD_DIR=\"$HOME/Downloads\"\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(home, "Downloads"), 0o755)
	got := defaultDownloadDir()
	if got != filepath.Join(home, "Downloads") {
		t.Errorf("download dir = %q", got)
	}
}
