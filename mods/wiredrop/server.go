package main

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Decision is the human's verdict on an incoming transfer.
type Decision int

const (
	DecisionDecline Decision = iota
	DecisionAccept
)

// ConsentFunc asks the human whether an incoming session may proceed.
// Production: dunst notification with a 60s timeout defaulting to
// decline. Tests inject auto-accept/decline.
type ConsentFunc func(sess *Session) Decision

// sessionFile is one file inside a session.
type sessionFile struct {
	meta    FileMeta
	token   string
	done    bool
	tmpPath string
}

// Session is one inbound prepare-upload, from consent through delivery.
type Session struct {
	ID       string
	SenderIP string
	Info     DeviceInfo              // fingerprint: observed cert when the sender
	Files    map[string]*sessionFile // presented one, asserted otherwise
	Created  time.Time
	// FPVerified is true when the sender presented a TLS client
	// certificate and Info.Fingerprint was re-derived from it (wiredrop
	// senders always do; phones never do). The consent UI shows the
	// difference so the human knows what "the words" are worth.
	FPVerified bool
	decided    bool
	completed  bool
	cancelled  bool
}

// Trust note: the server requests but never requires TLS client certs.
// A wiredrop sender presents its certificate, so the handshake proves its
// fingerprint and the consent UI can show it as verified. A phone running
// the official app presents nothing — then the fingerprint is
// self-asserted JSON, exactly LocalSend's own threat model (the PIN is
// the auth mechanism there). Our hardening either way: the session is
// bound to the sender's IP (403 otherwise), per-file tokens are
// unguessable, and the consent UI shows the asserted fingerprint's TOFU
// state — including a loud warning
// when a known fingerprint arrives from an unfamiliar address.

type rateEntry struct {
	count       int
	windowStart time.Time
}

// Server is the wiredrop HTTPS daemon: LocalSend upload API + discovery
// replies + a loopback control socket for `wiredrop ctl`.
type Server struct {
	cfg     *Config
	paths   *Paths
	info    DeviceInfo
	cert    tls.Certificate
	peers   *PeerCache
	known   *KnownHosts
	consent ConsentFunc
	// resolve pushes an out-of-band decision into a pending consent
	// (the Consenter's Resolve — `wiredrop ctl` and /cancel use it).
	resolve func(sessionID string, d Decision) bool
	pin     string

	mu       sync.Mutex
	sessions map[string]*Session
	rates    map[string]*rateEntry

	mux        *http.ServeMux
	listeners  []net.Listener
	actualPort int
	done       chan struct{}
}

// NewServer builds the daemon. consent may be nil (defaults to decline).
func NewServer(cfg *Config, paths *Paths, info DeviceInfo, cert tls.Certificate, peers *PeerCache, known *KnownHosts) *Server {
	s := &Server{
		cfg:      cfg,
		paths:    paths,
		info:     info,
		cert:     cert,
		peers:    peers,
		known:    known,
		pin:      cfg.PIN,
		sessions: map[string]*Session{},
		rates:    map[string]*rateEntry{},
		mux:      http.NewServeMux(),
		done:     make(chan struct{}),
	}
	if s.consent == nil {
		s.consent = func(*Session) Decision { return DecisionDecline }
	}
	s.mux.HandleFunc("POST "+apiPrefix+"/register", s.handleRegister)
	s.mux.HandleFunc("POST "+apiPrefix+"/prepare-upload", s.handlePrepareUpload)
	s.mux.HandleFunc("POST "+apiPrefix+"/upload", s.handleUpload)
	s.mux.HandleFunc("POST "+apiPrefix+"/cancel", s.handleCancel)
	s.mux.HandleFunc("GET "+apiPrefix+"/info", s.handleInfo)
	return s
}

// SetConsent installs the human-consent function (dunst in production).
func (s *Server) SetConsent(fn ConsentFunc) { s.consent = fn }

// SetResolver installs the out-of-band decision hook (Consenter.Resolve).
// handleCancel uses it so a cancelled session unblocks the consent gate
// immediately instead of at the 60s timeout.
func (s *Server) SetResolver(fn func(string, Decision) bool) { s.resolve = fn }

// ActualPort is the bound TCP port (after next-free-port fallback).
func (s *Server) ActualPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actualPort
}

// --- bind scope -----------------------------------------------------------

var (
	rfc1918_10  = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)}
	rfc1918_172 = net.IPNet{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)}
	rfc1918_192 = net.IPNet{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)}
	tailnetNet  = net.IPNet{IP: net.ParseIP("100.64.0.0"), Mask: net.CIDRMask(10, 32)}
)

// bindable reports whether we may serve on ip: loopback, RFC1918 LAN, or
// the Tailscale CGNAT range. Never the public internet.
func bindable(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.To4() == nil {
		return false // v4 only for v1
	}
	return rfc1918_10.Contains(ip) || rfc1918_172.Contains(ip) ||
		rfc1918_192.Contains(ip) || tailnetNet.Contains(ip)
}

// listenAddrs enumerates the local addresses we serve on.
func listenAddrs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || !bindable(ip) {
				continue
			}
			out = append(out, ip.String())
		}
	}
	if len(out) == 0 {
		out = []string{"127.0.0.1"}
	}
	return out
}

// --- lifecycle ------------------------------------------------------------

// Start binds (with next-free-port fallback) and serves until ctx ends.
func (s *Server) Start(addrs []string, port int) error {
	// Defense in depth at the API boundary: never bind a public address,
	// no matter what the caller passes. listenAddrs only enumerates
	// bindable addresses, but Start refuses anything else outright so a
	// future caller can't accidentally widen the scope.
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			return fmt.Errorf("wiredrop: refusing to bind unparseable address %q", a)
		}
		if !bindable(ip) {
			return fmt.Errorf("wiredrop: refusing to bind public address %s", a)
		}
	}
	if len(addrs) == 0 {
		addrs = []string{"127.0.0.1"}
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{s.cert},
		// Request, never require: wiredrop senders present their cert
		// (mutual auth), phones running the official app present nothing
		// and keep working. Either way the handshake completes.
		ClientAuth: tls.RequestClientCert,
	}
	bound := false
	for p := port; p < port+32; p++ {
		// Bind the first address, then pin every other address to the
		// SAME port. This matters for port 0 (ephemeral): without it
		// each interface would land on a different random port and the
		// announced port would be a lie.
		l0, err := tls.Listen("tcp", net.JoinHostPort(addrs[0], itoa(p)), tlsCfg)
		if err != nil {
			continue
		}
		actual := p
		if tcpAddr, ok := l0.Addr().(*net.TCPAddr); ok {
			actual = tcpAddr.Port
		}
		ls := []net.Listener{l0}
		ok := true
		for _, addr := range addrs[1:] {
			l, err := tls.Listen("tcp", net.JoinHostPort(addr, itoa(actual)), tlsCfg)
			if err != nil {
				ok = false
				break
			}
			ls = append(ls, l)
		}
		if !ok {
			for _, l := range ls {
				l.Close()
			}
			continue
		}
		s.mu.Lock()
		s.listeners = ls
		s.actualPort = actual
		s.info.Port = actual
		s.mu.Unlock()
		bound = true
		break
	}
	if !bound {
		return fmt.Errorf("wiredrop: no free port in %d..%d", port, port+31)
	}
	go s.sweepSessions()
	go func() {
		// Serve on every bound listener (loopback, LAN, tailnet).
		var wg sync.WaitGroup
		for _, l := range s.listeners {
			wg.Add(1)
			go func(l net.Listener) {
				defer wg.Done()
				if err := http.Serve(l, s.mux); err != nil {
					// Errors after Stop closes the listeners are
					// expected; anything else is a real failure and
					// gets logged so the field has footprints.
					select {
					case <-s.done:
					default:
						log.Printf("wiredrop: serve error: %v", err)
					}
				}
			}(l)
		}
		wg.Wait()
	}()
	<-s.done
	return nil
}

// Stop closes all listeners.
func (s *Server) Stop() {
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.listeners {
		l.Close()
	}
}

// sweepSessions expires stale sessions (15 min) and their temp files.
func (s *Server) sweepSessions() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			now := time.Now()
			s.mu.Lock()
			for id, sess := range s.sessions {
				if now.Sub(sess.Created) > 15*time.Minute {
					s.cancelLocked(sess)
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) cancelLocked(sess *Session) {
	sess.cancelled = true
	for _, f := range sess.Files {
		if f.tmpPath != "" {
			os.Remove(f.tmpPath)
		}
	}
}

// --- helpers --------------------------------------------------------------

func itoa(i int) string { return fmt.Sprintf("%d", i) }

func senderIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimited implements a tiny per-IP token bucket: 120 requests per
// minute, then 429. Generous for protocol chatter, present per the spec.
func (s *Server) rateLimited(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	e, ok := s.rates[ip]
	if !ok || now.Sub(e.windowStart) > time.Minute {
		s.rates[ip] = &rateEntry{count: 1, windowStart: now}
		return false
	}
	e.count++
	return e.count > 120
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) checkPIN(r *http.Request) bool {
	if s.pin == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("pin")), []byte(s.pin)) == 1
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func newToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// --- handlers -------------------------------------------------------------

// handleRegister answers discovery: two-way per the spec. The requester
// is recorded as a peer (via lan or tailnet by source address); the
// response is our info WITHOUT port/protocol/announce.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.rateLimited(senderIP(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	var info DeviceInfo
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&info); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if info.Fingerprint == "" || info.Fingerprint == s.info.Fingerprint {
		// Self-discovery or garbage — acknowledge, don't record.
		writeJSON(w, http.StatusOK, s.info.infoResponse())
		return
	}
	via := "lan"
	if ip := net.ParseIP(senderIP(r)); ip != nil && tailnetNet.Contains(ip) {
		via = "tailnet"
	}
	// Never trust the fingerprint inside the JSON body — re-derive it from
	// the TLS handshake before caching. A liar can claim any alias; they
	// can't claim our observed certificate.
	if r.TLS != nil {
		if fp, ok := fingerprintFromConn(*r.TLS); ok {
			info.Fingerprint = fp
		}
	}
	s.peers.Upsert(info, senderIP(r), via)
	writeJSON(w, http.StatusOK, s.info.infoResponse())
}

// handlePrepareUpload is the consent gate. The session is created first
// (so the notification can name the files), then the handler blocks up to
// 60s for the human. Accept -> 200 with tokens; decline/timeout -> 403;
// another live session -> 409; bad PIN -> 401.
func (s *Server) handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	ip := senderIP(r)
	if s.rateLimited(ip) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	if !s.checkPIN(r) {
		http.Error(w, "PIN required", http.StatusUnauthorized)
		return
	}
	var req PrepareUploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Same rule as /register: the consent UI and known-hosts must show
	// the observed certificate fingerprint when the sender presented one,
	// not the asserted one. fpVerified records which it was.
	fpVerified := false
	if r.TLS != nil {
		if fp, ok := fingerprintFromConn(*r.TLS); ok {
			req.Info.Fingerprint = fp
			fpVerified = true
		}
	}
	if len(req.Files) == 0 {
		w.WriteHeader(http.StatusNoContent) // 204: nothing to transfer
		return
	}

	s.mu.Lock()
	for _, sess := range s.sessions {
		if !sess.decided || (!sess.completed && !sess.cancelled) {
			// One live inbound session at a time — keeps the consent UX
			// unambiguous. (Outbound sends are unaffected.)
			s.mu.Unlock()
			http.Error(w, "blocked by another session", http.StatusConflict)
			return
		}
	}
	sess := &Session{
		ID:         newSessionID(),
		SenderIP:   ip,
		Info:       req.Info,
		Files:      map[string]*sessionFile{},
		Created:    time.Now(),
		FPVerified: fpVerified,
	}
	tokens := map[string]string{}
	for id, meta := range req.Files {
		if meta.Size <= 0 || meta.Size > 100<<30 { // 100 GiB sanity cap
			s.mu.Unlock()
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		tok := newToken()
		sess.Files[id] = &sessionFile{meta: meta, token: tok}
		tokens[id] = tok
	}
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	// Block for the human. The consent function owns the timeout; the
	// production one defaults to DECLINE after 60s.
	decision := s.consent(sess)

	s.mu.Lock()
	defer s.mu.Unlock()
	if decision != DecisionAccept || sess.cancelled {
		s.cancelLocked(sess)
		delete(s.sessions, sess.ID)
		http.Error(w, "rejected", http.StatusForbidden)
		return
	}
	sess.decided = true
	writeJSON(w, http.StatusOK, PrepareUploadResponse{SessionID: sess.ID, Files: tokens})
}

// handleUpload receives one whole file: POST /upload?sessionId=&fileId=&token=
// with the raw bytes as the body. Hash-then-write: the bytes land in a
// temp file, the sha256 is verified BEFORE the rename into Downloads, and
// a mismatch yields 422 with the temp file discarded. No partial file is
// ever left behind — interrupted transfers restart from zero (v1).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ip := senderIP(r)
	if s.rateLimited(ip) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	sessionID, fileID, token := q.Get("sessionId"), q.Get("fileId"), q.Get("token")
	if sessionID == "" || fileID == "" || token == "" {
		http.Error(w, "missing parameters", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.cancelled {
		s.mu.Unlock()
		http.Error(w, "invalid session", http.StatusForbidden)
		return
	}
	// Session/IP binding (Haiku implementation's hardening, per spec).
	if sess.SenderIP != ip {
		s.mu.Unlock()
		http.Error(w, "invalid token or IP address", http.StatusForbidden)
		return
	}
	f, ok := sess.Files[fileID]
	if !ok || subtle.ConstantTimeCompare([]byte(f.token), []byte(token)) != 1 {
		s.mu.Unlock()
		http.Error(w, "invalid token or IP address", http.StatusForbidden)
		return
	}
	if f.done {
		s.mu.Unlock() // idempotent retry
		w.WriteHeader(http.StatusOK)
		return
	}
	if !sess.decided {
		s.mu.Unlock()
		http.Error(w, "invalid session", http.StatusForbidden)
		return
	}
	// Reserve the temp path while holding the lock so parallel uploads of
	// the same file can't double-write.
	tmp, err := os.CreateTemp(s.cfg.DownloadDir, ".wiredrop-*.part")
	if err != nil {
		s.mu.Unlock()
		http.Error(w, "cannot stage file", http.StatusInternalServerError)
		return
	}
	f.tmpPath = tmp.Name()
	s.mu.Unlock()

	// Stream + hash. Cap at declared size + 64 MiB slack so a lying
	// Content-Length can't fill the disk.
	limit := f.meta.Size + (64 << 20)
	h := sha256New()
	n, copyErr := io.Copy(tmp, io.TeeReader(http.MaxBytesReader(w, r.Body, limit), h))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(tmp.Name())
		s.mu.Lock()
		f.tmpPath = ""
		s.mu.Unlock()
		http.Error(w, "transfer failed", http.StatusInternalServerError)
		return
	}
	if n != f.meta.Size {
		os.Remove(tmp.Name())
		s.mu.Lock()
		f.tmpPath = ""
		s.mu.Unlock()
		http.Error(w, "size mismatch", http.StatusBadRequest)
		return
	}
	if f.meta.SHA256 != nil && !strings.EqualFold(h.hex(), *f.meta.SHA256) {
		os.Remove(tmp.Name())
		s.mu.Lock()
		f.tmpPath = ""
		s.mu.Unlock()
		http.Error(w, "checksum mismatch", http.StatusUnprocessableEntity) // 422
		return
	}

	dest, err := uniqueDestPath(s.cfg.DownloadDir, f.meta.FileName)
	if err != nil {
		os.Remove(tmp.Name())
		s.mu.Lock()
		f.tmpPath = ""
		s.mu.Unlock()
		http.Error(w, "bad file name", http.StatusBadRequest)
		return
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		s.mu.Lock()
		f.tmpPath = ""
		s.mu.Unlock()
		http.Error(w, "cannot store file", http.StatusInternalServerError)
		return
	}
	// Best-effort timestamps from metadata.
	if f.meta.Metadata != nil {
		applyFileTimes(dest, f.meta.Metadata)
	}

	s.mu.Lock()
	f.done = true
	f.tmpPath = ""
	allDone := true
	for _, sf := range sess.Files {
		if !sf.done {
			allDone = false
			break
		}
	}
	if allDone {
		sess.completed = true
	}
	s.mu.Unlock()

	log.Printf("wiredrop: received %s (%d bytes) from %s -> %s",
		f.meta.FileName, n, sess.Info.Alias, filepath.Base(dest))
	w.WriteHeader(http.StatusOK)
}

// handleCancel lets the sender abort a session: temp files are removed.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	// Unblock a consent gate stuck on this session first, so the sender's
	// prepare-upload returns now instead of at the 60s timeout.
	if s.resolve != nil {
		s.resolve(sessionID, DecisionDecline)
	}
	s.mu.Lock()
	if sess, ok := s.sessions[sessionID]; ok {
		s.cancelLocked(sess)
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// handleInfo is the debug-only route from the spec.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.info.infoResponse())
}
