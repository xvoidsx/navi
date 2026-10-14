package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoDaemons spins two complete wiredrop stacks (A and B) on 127.0.0.1
// with distinct identities, certs, and download dirs — the closest thing
// to two navi machines a headless test can get.
type twoDaemons struct {
	a, b       *Server
	aInfo      DeviceInfo
	bInfo      DeviceInfo
	aFP, bFP   string
	aBase      string
	bBase      string
	bDownloads string
	aCfg       *Config
	aCert      tls.Certificate
}

func newTwoDaemons(t *testing.T, bConsent ConsentFunc) *twoDaemons {
	t.Helper()
	mk := func(alias string) (*Server, DeviceInfo, string, string, *Config, tls.Certificate) {
		dir := t.TempDir()
		downloads := filepath.Join(dir, "downloads")
		_ = os.MkdirAll(downloads, 0o755)
		cfg := &Config{Alias: alias, Port: 0, DownloadDir: downloads}
		paths := &Paths{ConfigDir: dir, CertDir: dir, RuntimeDir: dir, KnownHosts: filepath.Join(dir, "kh")}
		cert, err := loadOrCreateCert(dir)
		if err != nil {
			t.Fatal(err)
		}
		x509Cert, err := parseCert(cert)
		if err != nil {
			t.Fatal(err)
		}
		fp := fingerprintOf(x509Cert)
		peers := NewPeerCache(filepath.Join(dir, "peers.json"))
		model := "test"
		info := DeviceInfo{Alias: alias, Version: ProtoVersion, DeviceModel: &model,
			DeviceType: "desktop", Fingerprint: fp, Port: 0, Protocol: "https"}
		srv := NewServer(cfg, paths, info, cert, peers, &KnownHosts{entries: map[string]*KnownHost{}})
		go func() { _ = srv.Start([]string{"127.0.0.1"}, 0) }()
		deadline := time.Now().Add(5 * time.Second)
		for srv.ActualPort() == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if srv.ActualPort() == 0 {
			t.Fatal("daemon never bound")
		}
		t.Cleanup(srv.Stop)
		return srv, info, fp, downloads, cfg, cert
	}
	a, aInfo, aFP, _, aCfg, aCert := mk("daemon-a")
	b, bInfo, bFP, bDownloads, _, _ := mk("daemon-b")
	// B's info must carry its real bound port (Start mutates a copy — fix
	// the test's copy too).
	bInfo.Port = b.ActualPort()
	b.info.Port = b.ActualPort()
	aInfo.Port = a.ActualPort()
	if bConsent != nil {
		b.SetConsent(bConsent)
	} else {
		b.SetConsent(func(*Session) Decision { return DecisionAccept })
	}
	return &twoDaemons{
		a: a, b: b, aInfo: aInfo, bInfo: bInfo, aFP: aFP, bFP: bFP,
		aBase:      fmt.Sprintf("https://127.0.0.1:%d", a.ActualPort()),
		bBase:      fmt.Sprintf("https://127.0.0.1:%d", b.ActualPort()),
		bDownloads: bDownloads, aCfg: aCfg, aCert: aCert,
	}
}

// bPeer builds the client-side view of daemon B (as discovery would).
func (d *twoDaemons) bPeer() *Peer {
	return &Peer{
		Alias: d.bInfo.Alias, Fingerprint: d.bFP,
		Addresses: []string{"127.0.0.1"}, Port: d.b.ActualPort(),
		Via: "lan", LastSeen: time.Now(),
	}
}

// TestTwoDaemonSend drives the real client path (register →
// prepare-upload → whole-file upload) between two daemons and verifies
// the bytes on disk.
func TestTwoDaemonSend(t *testing.T) {
	d := newTwoDaemons(t, nil)

	src := filepath.Join(t.TempDir(), "report.txt")
	content := "wiredrop two-daemon integration check\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var progress []string
	err := sendFiles(d.aCfg, d.aInfo, d.bPeer(), []string{src}, "", true, &d.aCert,
		func(name string, sent, total int64) { progress = append(progress, name) })
	if err != nil {
		t.Fatalf("sendFiles: %v", err)
	}
	if len(progress) == 0 {
		t.Errorf("no progress callbacks fired")
	}
	got, err := os.ReadFile(filepath.Join(d.bDownloads, "report.txt"))
	if err != nil {
		t.Fatalf("delivered file missing: %v", err)
	}
	if string(got) != content {
		t.Errorf("content mismatch: %q", got)
	}
	// The register step must have taught B about A.
	found := false
	for _, p := range d.b.peers.List() {
		if p.Alias == "daemon-a" && p.Fingerprint == d.aFP {
			found = true
		}
	}
	if !found {
		t.Errorf("B's peer cache doesn't know A after register")
	}
}

// TestTwoDaemonFingerprintRefusal pins B's identity wrong on the sender
// side: the TLS handshake must fail before any byte moves, so B never
// even sees a session.
func TestTwoDaemonFingerprintRefusal(t *testing.T) {
	d := newTwoDaemons(t, nil)

	src := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(src, []byte("must not move"), 0o644); err != nil {
		t.Fatal(err)
	}
	evil := d.bPeer()
	evil.Fingerprint = strings.Repeat("0", 64) // attacker's claim

	err := sendFiles(d.aCfg, d.aInfo, evil, []string{src}, "", true, &d.aCert, nil)
	if err == nil {
		t.Fatalf("send to wrong fingerprint succeeded — MITM protection broken")
	}
	if !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Errorf("wrong error: %v", err)
	}
	d.b.mu.Lock()
	nSessions := len(d.b.sessions)
	d.b.mu.Unlock()
	if nSessions != 0 {
		t.Errorf("B registered %d session(s) from the refused peer", nSessions)
	}
	if _, err := os.Stat(filepath.Join(d.bDownloads, "secret.txt")); !os.IsNotExist(err) {
		t.Errorf("file landed despite fingerprint refusal")
	}
}

// TestServerRejectsAssertedFingerprint: when the sender presents a TLS
// client certificate (every wiredrop sender does), the server records the
// OBSERVED certificate fingerprint, never the asserted JSON one. A liar
// that only asserts (no cert — like a phone) falls back to the asserted
// value, exactly LocalSend's own threat model.
func TestServerRejectsAssertedFingerprint(t *testing.T) {
	d := newTwoDaemons(t, nil)

	certClient := func(cert tls.Certificate) *http.Client {
		return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{cert}}, //nolint:gosec
		}}
	}

	// Part 1: liar WITH a real client cert (A's) but a forged fingerprint
	// in the JSON body. The server must record A's observed fingerprint.
	liar := d.aInfo
	liar.Alias = "liar-with-cert"
	liar.Fingerprint = strings.Repeat("f", 64)
	body, _ := json.Marshal(liar)
	resp, err := certClient(d.aCert).Post(d.bBase+apiPrefix+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register status = %d", resp.StatusCode)
	}
	seen := false
	for _, p := range d.b.peers.List() {
		if p.Alias != "liar-with-cert" {
			continue
		}
		seen = true
		if p.Fingerprint == strings.Repeat("f", 64) {
			t.Errorf("B cached the ASSERTED fingerprint — spoofing succeeded")
		}
		if p.Fingerprint != d.aFP {
			t.Errorf("B cached %s, want observed %s", shortFP(p.Fingerprint), shortFP(d.aFP))
		}
	}
	if !seen {
		t.Errorf("B didn't cache the liar at all")
	}

	// Part 2: the same forgery through prepare-upload must ALSO resolve
	// to the observed fingerprint, and the session is marked verified.
	// Capture the session inside the consent gate itself.
	var gotFP string
	var gotVerified bool
	d.b.SetConsent(func(s *Session) Decision {
		gotFP = s.Info.Fingerprint
		gotVerified = s.FPVerified
		return DecisionDecline
	})
	prep := fmt.Sprintf(`{"info":{"alias":"liar-prep","version":"2.2","deviceType":"desktop","fingerprint":"%s"},"files":{"f1":{"id":"f1","fileName":"n.txt","size":1,"fileType":"text/plain"}}}`,
		strings.Repeat("e", 64))
	pr, err := certClient(d.aCert).Post(d.bBase+apiPrefix+"/prepare-upload", "application/json", strings.NewReader(prep))
	if err != nil {
		t.Fatal(err)
	}
	pr.Body.Close()
	if pr.StatusCode != http.StatusForbidden { // consent declined
		t.Fatalf("prepare status = %d, want 403", pr.StatusCode)
	}
	if gotFP != d.aFP {
		t.Errorf("session fingerprint = %s, want observed %s", shortFP(gotFP), shortFP(d.aFP))
	}
	if !gotVerified {
		t.Errorf("session with presented client cert not marked FPVerified")
	}
}

// TestConsentDefaultDeclineTimeout runs the real Consenter (fake dunst)
// with a 1s timeout: prepare-upload must 403 and no file may land.
func TestConsentDefaultDeclineTimeout(t *testing.T) {
	old := consentTimeoutSeconds
	consentTimeoutSeconds = 1
	defer func() { consentTimeoutSeconds = old }()

	dir := t.TempDir()
	downloads := filepath.Join(dir, "downloads")
	_ = os.MkdirAll(downloads, 0o755)
	cfg := &Config{Alias: "quiet", Port: 0, DownloadDir: downloads}
	paths := &Paths{ConfigDir: dir, CertDir: dir, RuntimeDir: dir, KnownHosts: filepath.Join(dir, "kh")}
	cert, _ := loadOrCreateCert(dir)
	x509Cert, _ := parseCert(cert)
	peers := NewPeerCache(filepath.Join(dir, "peers.json"))
	known, _ := LoadKnownHosts(paths.KnownHosts)
	model := "test"
	info := DeviceInfo{Alias: "quiet", Version: ProtoVersion, DeviceModel: &model,
		DeviceType: "desktop", Fingerprint: fingerprintOf(x509Cert)}
	srv := NewServer(cfg, paths, info, cert, peers, known)
	consenter := NewConsenter(known, peers)
	consenter.newNotifier = func() (notifier, error) { return fakeNotifier{}, nil }
	srv.SetConsent(consenter.Ask)
	go func() { _ = srv.Start([]string{"127.0.0.1"}, 0) }()
	deadline := time.Now().Add(5 * time.Second)
	for srv.ActualPort() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(srv.Stop)
	base := fmt.Sprintf("https://127.0.0.1:%d", srv.ActualPort())

	content := "nobody accepted this"
	sum := testSHA256([]byte(content))
	req := fmt.Sprintf(`{"info":{"alias":"sender","version":"2.2","deviceType":"desktop","fingerprint":"%s"},"files":{"f1":{"id":"f1","fileName":"late.txt","size":%d,"fileType":"text/plain","sha256":"%s"}}}`,
		strings.Repeat("a", 64), len(content), sum)
	start := time.Now()
	resp, err := skipVerifyClient().Post(base+apiPrefix+"/prepare-upload", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("prepare took %v — timeout didn't fire", elapsed)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("timed-out prepare status = %d, want 403", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(downloads, "late.txt")); !os.IsNotExist(err) {
		t.Errorf("file landed without consent")
	}
}

// TestCtlDecide drives the control-socket path: a pending session can be
// accepted out-of-band via Consenter.Resolve (what `wiredrop ctl` calls).
func TestCtlDecide(t *testing.T) {
	dir := t.TempDir()
	known, _ := LoadKnownHosts(filepath.Join(dir, "kh"))
	peers := NewPeerCache(filepath.Join(dir, "peers.json"))
	c := NewConsenter(known, peers)
	c.newNotifier = func() (notifier, error) { return fakeNotifier{}, nil }

	fp := strings.Repeat("b", 64)
	if err := known.Confirm("sender", fp); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "sess-ctl", SenderIP: "127.0.0.1",
		Info: DeviceInfo{Alias: "sender", Fingerprint: fp}, Created: time.Now()}

	old := consentTimeoutSeconds
	consentTimeoutSeconds = 5
	defer func() { consentTimeoutSeconds = old }()

	done := make(chan Decision, 1)
	go func() { done <- c.Ask(sess) }()
	// Wait for Ask to register the pending session, then decide via ctl.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if c.Resolve("sess-ctl", DecisionAccept) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never became pending")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case d := <-done:
		if d != DecisionAccept {
			t.Errorf("ctl accept resolved to %v", d)
		}
	case <-time.After(6 * time.Second):
		t.Errorf("Ask didn't return after ctl accept")
	}
	// Unknown session IDs resolve false.
	if c.Resolve("nope", DecisionDecline) {
		t.Errorf("Resolve accepted a bogus session ID")
	}
}

// TestCtlSocketRoundTrip: the real unix-socket path — serveControl in
// the background, ctlCall as the client — drives a pending Ask to
// accept, exactly like `wiredrop ctl accept` does in production.
func TestCtlSocketRoundTrip(t *testing.T) {
	dir := t.TempDir()
	known, _ := LoadKnownHosts(filepath.Join(dir, "kh"))
	peers := NewPeerCache(filepath.Join(dir, "peers.json"))
	c := NewConsenter(known, peers)
	c.newNotifier = func() (notifier, error) { return fakeNotifier{}, nil }

	fp := strings.Repeat("c", 64)
	if err := known.Confirm("sender", fp); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "sess-sock", SenderIP: "127.0.0.1",
		Info: DeviceInfo{Alias: "sender", Fingerprint: fp}, Created: time.Now()}

	old := consentTimeoutSeconds
	consentTimeoutSeconds = 5
	defer func() { consentTimeoutSeconds = old }()

	sockPath := filepath.Join(dir, "control.sock")
	stop := make(chan struct{})
	defer close(stop)
	srvErr := make(chan error, 1)
	go func() { srvErr <- c.serveControl(sockPath, stop) }()

	// Wait for the socket to exist.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control socket never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}

	done := make(chan Decision, 1)
	go func() { done <- c.Ask(sess) }()
	// Wait for the session to become pending, then decide over the
	// real socket client.
	deadline = time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		_, pending := c.pending["sess-sock"]
		c.mu.Unlock()
		if pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never became pending")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := ctlCall(sockPath, "accept", "sess-sock"); err != nil {
		t.Fatalf("ctlCall accept: %v", err)
	}
	select {
	case d := <-done:
		if d != DecisionAccept {
			t.Errorf("socket accept resolved to %v", d)
		}
	case <-time.After(6 * time.Second):
		t.Error("Ask didn't return after socket accept")
	}

	// Bogus session IDs come back as errors, not silent success.
	if err := ctlCall(sockPath, "decline", "nope"); err == nil {
		t.Error("ctlCall decline on bogus session: want error, got nil")
	}
}

// TestServerSanitizesEvilFilenames: even when the sender's metadata names
// a traversal path, the file lands inside the download dir under a safe
// name and nothing escapes.
func TestServerSanitizesEvilFilenames(t *testing.T) {
	d := newTwoDaemons(t, nil)
	content := "evil payload"
	sum := testSHA256([]byte(content))
	req := fmt.Sprintf(`{"info":{"alias":"daemon-a","version":"2.2","deviceType":"desktop","fingerprint":"%s"},"files":{"f1":{"id":"f1","fileName":"../../../../tmp/wiredrop-evil.txt","size":%d,"fileType":"text/plain","sha256":"%s"}}}`,
		d.aFP, len(content), sum)
	c := skipVerifyClient()
	resp, err := c.Post(d.bBase+apiPrefix+"/prepare-upload", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		resp.Body.Close()
		t.Fatalf("prepare status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	d.b.mu.Lock()
	sessID, token := "", ""
	for id, s := range d.b.sessions {
		sessID, token = id, s.Files["f1"].token
	}
	d.b.mu.Unlock()
	up, err := c.Post(
		fmt.Sprintf("%s/api/localsend/v2/upload?sessionId=%s&fileId=f1&token=%s", d.bBase, sessID, token),
		"application/octet-stream", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	up.Body.Close()
	if up.StatusCode != 200 && up.StatusCode != 204 {
		t.Fatalf("upload status = %d", up.StatusCode)
	}
	if _, err := os.Stat("/tmp/wiredrop-evil.txt"); !os.IsNotExist(err) {
		t.Errorf("traversal escaped the download dir!")
		os.Remove("/tmp/wiredrop-evil.txt")
	}
	entries, _ := os.ReadDir(d.bDownloads)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected exactly 1 delivered file, got %v", names)
	} else if strings.ContainsAny(entries[0].Name(), `/\`) {
		t.Errorf("delivered name still dangerous: %q", entries[0].Name())
	}
	got, _ := os.ReadFile(filepath.Join(d.bDownloads, entries[0].Name()))
	if string(got) != content {
		t.Errorf("content mismatch")
	}
}

// fakeNotifier is the headless stand-in for dunst: notify "succeeds"
// (so Ask waits on the decision channel), close is a no-op.
type fakeNotifier struct{}

func (fakeNotifier) notify(summary, body string, actions bool) (uint32, error) {
	return 1, nil
}
func (fakeNotifier) close(id uint32) {}
