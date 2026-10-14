package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trust errors — the human must resolve these, never the code.
type trustError struct {
	kind        string // "unknown" or "changed"
	alias       string
	fingerprint string
	words       string
}

func (e *trustError) Error() string {
	if e.kind == "changed" {
		return fmt.Sprintf("wiredrop: REFUSED — %q changed fingerprints (possible MITM). run `wiredrop forget %s` after verifying out-of-band, then retry", e.alias, e.alias)
	}
	return fmt.Sprintf("wiredrop: unknown device %q — confirm the fingerprint before first transfer", e.alias)
}

// resolvePeer finds a peer by alias (case-insensitive) or fingerprint
// prefix. Ambiguous or missing targets are errors, not guesses.
func resolvePeer(cache *PeerCache, target string) (*Peer, error) {
	peers := cache.List()
	var byAlias, byPrefix []*Peer
	for _, p := range peers {
		if strings.EqualFold(p.Alias, target) {
			byAlias = append(byAlias, p)
		} else if strings.HasPrefix(strings.ToLower(p.Fingerprint), strings.ToLower(target)) {
			byPrefix = append(byPrefix, p)
		}
	}
	if len(byAlias) == 1 {
		return byAlias[0], nil
	}
	if len(byAlias) > 1 {
		return nil, fmt.Errorf("wiredrop: %q matches %d devices — use a fingerprint prefix", target, len(byAlias))
	}
	if len(byPrefix) == 1 {
		return byPrefix[0], nil
	}
	if len(byPrefix) > 1 {
		return nil, fmt.Errorf("wiredrop: fingerprint prefix matches %d devices — be more specific", len(byPrefix))
	}
	return nil, fmt.Errorf("wiredrop: no known device %q (is the daemon running? try `wiredrop ls`)", target)
}

// checkTrust enforces TOFU before any byte moves. Unknown fingerprints
// come back as *trustError so the CLI/TUI can run the 6-word ceremony;
// a changed fingerprint is a hard refusal.
func checkTrust(known *KnownHosts, alias, fingerprint string) error {
	switch known.Check(alias, fingerprint) {
	case TrustKnown:
		return nil
	case TrustChanged:
		return &trustError{kind: "changed", alias: alias, fingerprint: fingerprint, words: fingerprintWords(fingerprint)}
	default:
		return &trustError{kind: "unknown", alias: alias, fingerprint: fingerprint, words: fingerprintWords(fingerprint)}
	}
}

// peerClient returns an HTTPS client that pins the peer's fingerprint:
// the handshake must present exactly the expected certificate. Pinning
// happens in VerifyConnection so the handshake itself fails on mismatch,
// before any request byte is sent; verifyPeerFP re-checks per response
// as defense in depth. On the receive side the human consent gate covers
// what TLS can't (a phone's asserted identity), with the observed client
// certificate marked verified when present.
// own, when non-nil, is presented as our client certificate so a
// wiredrop receiver can observe (not just trust) our fingerprint.
func peerClient(expectFP string, own *tls.Certificate) *http.Client {
	tlsCfg := &tls.Config{ //nolint:gosec — pinned in VerifyConnection
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("wiredrop: peer presented no certificate")
			}
			got := fingerprintOf(cs.PeerCertificates[0])
			if !strings.EqualFold(got, expectFP) {
				return fmt.Errorf("wiredrop: peer fingerprint mismatch: got %s want %s", shortFP(got), shortFP(expectFP))
			}
			return nil
		},
	}
	if own != nil {
		tlsCfg.Certificates = []tls.Certificate{*own}
	}
	return &http.Client{
		Timeout: 0, // transfers can be long; per-request timeouts are set by callers
		Transport: &http.Transport{
			TLSClientConfig:       tlsCfg,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// verifyPeerFP extracts the server cert fingerprint from a completed
// request and compares it to the expected value.
func verifyPeerFP(resp *http.Response, expectFP string) error {
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return fmt.Errorf("wiredrop: no peer certificate")
	}
	got := fingerprintOf(resp.TLS.PeerCertificates[0])
	if !strings.EqualFold(got, expectFP) {
		return fmt.Errorf("wiredrop: peer fingerprint mismatch: got %s want %s", shortFP(got), shortFP(expectFP))
	}
	return nil
}

func shortFP(fp string) string {
	if len(fp) > 16 {
		return fp[:16] + "…"
	}
	return fp
}

// SendProgress is called as each file uploads.
type SendProgress func(fileName string, sent, total int64)

// sendFiles implements `wiredrop send`: register → prepare-upload →
// whole-file POSTs with progress. trusted must be true — callers run
// checkTrust first and only set it after the human confirms.
// own is this machine's certificate, presented to the receiver so
// wiredrop→wiredrop transfers are mutually authenticated (the receiver
// requests but never requires client certs, so phone interop is
// unaffected).
func sendFiles(cfg *Config, self DeviceInfo, peer *Peer, files []string, pin string, trusted bool, own *tls.Certificate, progress SendProgress) error {
	if !trusted {
		return fmt.Errorf("wiredrop: refusing to send to unconfirmed device")
	}
	client := peerClient(peer.Fingerprint, own)
	base := "https://" + net.JoinHostPort(peerAddr(peer), itoa(peerPort(peer)))

	// 1. register (two-way discovery doubles as a liveness check)
	regBody, _ := json.Marshal(self)
	if _, err := doPost(client, base+apiPrefix+"/register", regBody, peer.Fingerprint, ""); err != nil {
		return fmt.Errorf("wiredrop: register: %w", err)
	}

	// 2. prepare-upload with metadata + sha256 for every file
	metas := map[string]FileMeta{}
	paths := map[string]string{} // fileID -> local path
	for i, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			return fmt.Errorf("wiredrop: %s: %w", f, err)
		}
		if st.IsDir() {
			return fmt.Errorf("wiredrop: %s is a directory — send files, not folders (v1)", f)
		}
		sum, err := hashFile(f)
		if err != nil {
			return fmt.Errorf("wiredrop: hash %s: %w", f, err)
		}
		id := fmt.Sprintf("file-%d", i)
		ext := strings.ToLower(filepath.Ext(f))
		ft := mime.TypeByExtension(ext)
		if ft == "" {
			ft = "application/octet-stream"
		}
		mod := st.ModTime().UTC().Format(time.RFC3339)
		metas[id] = FileMeta{
			ID:       id,
			FileName: filepath.Base(f),
			Size:     st.Size(),
			FileType: ft,
			SHA256:   &sum,
			Metadata: &FileMetadata{Modified: &mod, Accessed: &mod},
		}
		paths[id] = f
	}
	prepBody, _ := json.Marshal(PrepareUploadRequest{Info: self, Files: metas})
	resp, err := doPostTimeout(client, base+apiPrefix+"/prepare-upload", prepBody, peer.Fingerprint, pin,
		(consentTimeoutSeconds+15)*time.Second)
	if err != nil {
		if isStatus(err, http.StatusUnauthorized) && pin == "" {
			return fmt.Errorf("wiredrop: %s requires a PIN — pass --pin", peer.Alias)
		}
		if isStatus(err, http.StatusUnauthorized) {
			return fmt.Errorf("wiredrop: wrong PIN for %s", peer.Alias)
		}
		if isStatus(err, http.StatusForbidden) {
			return fmt.Errorf("wiredrop: %s rejected the transfer", peer.Alias)
		}
		if isStatus(err, http.StatusConflict) {
			return fmt.Errorf("wiredrop: %s is busy with another transfer", peer.Alias)
		}
		return fmt.Errorf("wiredrop: prepare-upload: %w", err)
	}
	var prep PrepareUploadResponse
	if err := json.Unmarshal(resp, &prep); err != nil {
		return fmt.Errorf("wiredrop: bad prepare-upload response: %w", err)
	}
	if len(prep.Files) == 0 {
		return fmt.Errorf("wiredrop: %s accepted no files", peer.Alias)
	}

	// 3. upload each file, whole-file POST with progress
	sessionID := prep.SessionID
	cancelled := false
	cancel := func() {
		if cancelled {
			return
		}
		cancelled = true
		_, _ = doPost(client, base+apiPrefix+"/cancel?sessionId="+sessionID, nil, peer.Fingerprint, "")
	}
	for id, token := range prep.Files {
		meta := metas[id]
		path := paths[id]
		if err := uploadOne(client, base, peer.Alias, sessionID, id, token, path, meta, peer.Fingerprint, progress); err != nil {
			cancel()
			return err
		}
	}
	return nil
}

// uploadOne POSTs a single whole file. v1 has no resume: an interrupted
// transfer restarts from zero — the progress UI says so honestly.
func uploadOne(client *http.Client, base, peerAlias, sessionID, fileID, token, path string, meta FileMeta, expectFP string, progress SendProgress) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("wiredrop: open %s: %w", path, err)
	}
	defer f.Close()
	pr := &progressReader{r: f, total: meta.Size, name: meta.FileName, cb: progress}
	url := fmt.Sprintf("%s%s/upload?sessionId=%s&fileId=%s&token=%s", base, apiPrefix, sessionID, fileID, token)
	req, err := http.NewRequest("POST", url, pr)
	if err != nil {
		return err
	}
	req.ContentLength = meta.Size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("wiredrop: upload %s: %w (interrupted transfers restart from zero — no resume in v1)", meta.FileName, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := verifyPeerFP(resp, expectFP); err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("wiredrop: %s rejected the file (checksum mismatch)", peerAlias)
	case http.StatusForbidden:
		return fmt.Errorf("wiredrop: upload rejected (bad token or IP binding)")
	default:
		return fmt.Errorf("wiredrop: upload failed: status %d", resp.StatusCode)
	}
}

// progressReader wraps a file and reports bytes as they stream.
type progressReader struct {
	r     io.Reader
	total int64
	sent  int64
	name  string
	cb    SendProgress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		if p.cb != nil {
			p.cb(p.name, p.sent, p.total)
		}
	}
	return n, err
}

// doPost is a small JSON POST helper with fingerprint verification.
func doPost(client *http.Client, url string, body []byte, expectFP, pin string) ([]byte, error) {
	return doPostTimeout(client, url, body, expectFP, pin, 30*time.Second)
}

// doPostTimeout is doPost with a caller-chosen timeout. prepare-upload
// legitimately blocks server-side for the whole consent window, so it
// needs longer than the default 30s.
func doPostTimeout(client *http.Client, url string, body []byte, expectFP, pin string, timeout time.Duration) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	if pin != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "pin=" + urlpkg.QueryEscape(pin)
	}
	req, err := http.NewRequest("POST", url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := withTimeout(client, timeout)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err := verifyPeerFP(resp, expectFP); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, statusError(resp.StatusCode)
	}
	return data, nil
}

// withTimeout clones the client with a per-request timeout.
func withTimeout(c *http.Client, d time.Duration) *http.Client {
	cc := *c
	cc.Timeout = d
	return &cc
}

type statusError int

func (e statusError) Error() string { return fmt.Sprintf("status %d", int(e)) }

func isStatus(err error, code int) bool {
	if se, ok := err.(statusError); ok {
		return int(se) == code
	}
	return false
}

// peerAddr picks the best address: prefer the first cached one.
func peerAddr(p *Peer) string {
	if len(p.Addresses) > 0 {
		return p.Addresses[0]
	}
	return ""
}

func peerPort(p *Peer) int {
	if p.Port != 0 {
		return p.Port
	}
	return DefaultPort
}
