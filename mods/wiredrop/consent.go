package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Consenter implements the human consent gate for incoming transfers.
// It sends the dunst notification itself over the session bus (godbus,
// pure Go) and listens for ActionInvoked. Timeout (60s) and any bus
// failure default to DECLINE — never auto-accept blind.
type Consenter struct {
	known *KnownHosts
	peers *PeerCache

	mu      sync.Mutex
	conn    *dbus.Conn
	pending map[string]chan Decision // sessionID -> decision channel
	notifs  map[string]uint32        // sessionID -> notification id

	// newNotifier builds the notification sink; replaced by a fake in
	// tests so the timeout/decline logic runs headless.
	newNotifier func() (notifier, error)
}

// NewConsenter builds the consent gate.
func NewConsenter(known *KnownHosts, peers *PeerCache) *Consenter {
	c := &Consenter{
		known:   known,
		peers:   peers,
		pending: map[string]chan Decision{},
		notifs:  map[string]uint32{},
	}
	c.newNotifier = func() (notifier, error) {
		conn, err := c.bus()
		if err != nil {
			return nil, err
		}
		return busNotifier{conn: conn, c: c}, nil
	}
	return c
}

// notifier sends the consent notification and closes it. The production
// implementation talks to dunst over the session bus; tests inject a fake.
type notifier interface {
	notify(summary, body string, actions bool) (uint32, error)
	close(id uint32)
}

type busNotifier struct {
	conn *dbus.Conn
	c    *Consenter
}

func (n busNotifier) notify(summary, body string, actions bool) (uint32, error) {
	return n.c.notify(n.conn, summary, body, actions)
}
func (n busNotifier) close(id uint32) { n.c.close(n.conn, id) }

// nullNotifier is the headless stand-in: notify "succeeds" so Ask waits
// the full timeout for a `wiredrop ctl` decision, close is a no-op.
type nullNotifier struct{}

func (nullNotifier) notify(summary, body string, actions bool) (uint32, error) {
	return 1, nil
}
func (nullNotifier) close(id uint32) {}

func (c *Consenter) bus() (*dbus.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	c.conn = conn
	go c.signalLoop(conn)
	return conn, nil
}

// Ask implements ConsentFunc.
func (c *Consenter) Ask(sess *Session) Decision {
	fp := sess.Info.Fingerprint
	alias := sess.Info.Alias

	// TOFU first. A changed fingerprint is a hard refusal — the consent
	// UI is not the place to re-pin; the human runs `wiredrop forget`
	// after verifying out-of-band.
	switch c.known.Check(alias, fp) {
	case TrustChanged:
		c.warn(fmt.Sprintf("wiredrop: REFUSED transfer from %q — its fingerprint changed. Possible MITM. Run `wiredrop forget %s` after verifying the device, then retry.", alias, alias))
		log.Printf("wiredrop: refused session %s: fingerprint change for %q", sess.ID, alias)
		return DecisionDecline
	}

	n, err := c.newNotifier()
	if err != nil {
		// No notification daemon (headless/SSH): the human decides via
		// the control socket instead. The 60s default-decline still
		// applies — a null notifier just means nobody pops up.
		log.Printf("wiredrop: no session bus — `wiredrop ctl accept|decline %s` to decide", sess.ID)
		n = nullNotifier{}
	}

	summary, body := c.renderAsk(sess)
	ch := make(chan Decision, 1)
	c.mu.Lock()
	c.pending[sess.ID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, sess.ID)
		delete(c.notifs, sess.ID)
		c.mu.Unlock()
	}()

	id, err := n.notify(summary, body, true)
	if err != nil {
		log.Printf("wiredrop: notify failed — auto-declining session %s: %v", sess.ID, err)
		return DecisionDecline
	}
	c.mu.Lock()
	c.notifs[sess.ID] = id
	c.mu.Unlock()
	// The session ID in the log is the headless/SSH path: without a
	// notification daemon the human decides via
	// `wiredrop ctl accept|decline <sessionID>`.
	log.Printf("wiredrop: incoming transfer %s from %q (%d file(s), %s) — `wiredrop ctl accept|decline %s`",
		sess.ID, alias, len(sess.Files), totalSize(sess), sess.ID)

	select {
	case d := <-ch:
		n.close(id)
		if d == DecisionAccept {
			// First sight pins here: the notification WAS the ceremony
			// (6 words + hex shown, human clicked Accept).
			if err := c.known.Confirm(alias, fp); err != nil {
				log.Printf("wiredrop: pin device: %v", err)
			}
		}
		return d
	case <-time.After(consentTimeoutSeconds * time.Second):
		n.close(id)
		log.Printf("wiredrop: consent timeout — declined session %s", sess.ID)
		return DecisionDecline
	}
}

// Resolve lets `wiredrop ctl accept|decline` decide a pending session
// (ssh sessions, scripts, or a second terminal).
func (c *Consenter) Resolve(sessionID string, d Decision) bool {
	c.mu.Lock()
	ch, ok := c.pending[sessionID]
	c.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- d:
		return true
	default:
		return false
	}
}

// PendingIDs lists sessions currently awaiting consent (for `ctl` UX).
func (c *Consenter) PendingIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.pending))
	for id := range c.pending {
		out = append(out, id)
	}
	return out
}

// renderAsk builds the notification text, with the TOFU state front and
// center: a new device shows its 6-word fingerprint + hex; a trusted one
// says so; an unfamiliar address on a known fingerprint warns loudly.
func (c *Consenter) renderAsk(sess *Session) (string, string) {
	fp := sess.Info.Fingerprint
	alias := sess.Info.Alias
	var total int64
	names := make([]string, 0, len(sess.Files))
	for _, f := range sess.Files {
		total += f.meta.Size
		names = append(names, f.meta.FileName)
	}
	files := strings.Join(names, ", ")
	if len(files) > 90 {
		files = files[:87] + "…"
	}
	summary := fmt.Sprintf("⤵ wiredrop: %s from %s", files, alias)
	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s), %s\n", len(sess.Files), humanSize(total))
	switch c.known.Check(alias, fp) {
	case TrustKnown:
		if sess.FPVerified {
			b.WriteString("✓ trusted device, certificate verified")
		} else {
			b.WriteString("✓ trusted device (self-asserted identity — no client certificate)")
		}
		if p, ok := c.peers.ByFingerprint(fp); ok {
			seen := false
			for _, a := range p.Addresses {
				if a == sess.SenderIP {
					seen = true
					break
				}
			}
			if !seen && len(p.Addresses) > 0 {
				fmt.Fprintf(&b, "\n⚠ new address for this device (%s)", sess.SenderIP)
			}
		}
	default:
		b.WriteString("NEW DEVICE — first sight, confirm before accepting:\n")
		b.WriteString("🔑 " + fingerprintWords(fp) + "\n" + fp + "\n")
		if sess.FPVerified {
			b.WriteString("certificate verified — these words are the device's own")
		} else {
			b.WriteString("⚠ self-asserted identity — compare out-of-band carefully")
		}
	}
	return summary, b.String()
}

func (c *Consenter) notify(conn *dbus.Conn, summary, body string, actions bool) (uint32, error) {
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	var acts []string
	if actions {
		acts = []string{"accept", "Accept", "decline", "Decline"}
	}
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		"wiredrop", uint32(0), "wiredrop",
		summary, body, acts,
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(2))},
		int32(-1))
	if call.Err != nil {
		return 0, call.Err
	}
	var id uint32
	if err := call.Store(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (c *Consenter) close(conn *dbus.Conn, id uint32) {
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	_ = obj.Call("org.freedesktop.Notifications.CloseNotification", 0, id).Err
}

// warn fires a one-shot warning notification (no actions).
func (c *Consenter) warn(body string) {
	conn, err := c.bus()
	if err != nil {
		return
	}
	_, _ = c.notify(conn, "⚠ wiredrop security", body, false)
}

// signalLoop routes ActionInvoked / NotificationClosed into pending
// decision channels.
func (c *Consenter) signalLoop(conn *dbus.Conn) {
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.Notifications"),
	); err != nil {
		return
	}
	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	for sig := range ch {
		switch sig.Name {
		case "org.freedesktop.Notifications.ActionInvoked":
			if len(sig.Body) < 2 {
				continue
			}
			id, _ := sig.Body[0].(uint32)
			key, _ := sig.Body[1].(string)
			c.route(id, key)
		case "org.freedesktop.Notifications.NotificationClosed":
			if len(sig.Body) < 1 {
				continue
			}
			id, _ := sig.Body[0].(uint32)
			c.route(id, "decline")
		}
	}
}

// totalSize sums the declared file sizes of a session.
func totalSize(sess *Session) string {
	var total int64
	for _, f := range sess.Files {
		total += f.meta.Size
	}
	return humanSize(total)
}

// decision. dunst expires the notification on timeout, but the daemon's
// own 60s timer is authoritative — either way the default is decline.
// route maps a notification id back to its session and delivers the
// decision. dunst expires the notification on timeout, but the daemon's
// own 60s timer is authoritative — either way the default is decline.
func (c *Consenter) route(notifID uint32, actionKey string) {
	c.mu.Lock()
	var sessionID string
	for sid, nid := range c.notifs {
		if nid == notifID {
			sessionID = sid
			break
		}
	}
	ch := c.pending[sessionID]
	c.mu.Unlock()
	if sessionID == "" || ch == nil {
		return
	}
	d := DecisionDecline
	if actionKey == "accept" {
		d = DecisionAccept
	}
	select {
	case ch <- d:
	default:
	}
}

// --- control socket -------------------------------------------------------

// serveControl exposes accept/decline to `wiredrop ctl` over a unix
// socket in the runtime dir. Loopback-only by construction (it's not TCP
// at all) — no network exposure.
func (c *Consenter) serveControl(sockPath string, stop <-chan struct{}) error {
	_ = os.Remove(sockPath)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		return err
	}
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer l.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /accept", func(w http.ResponseWriter, r *http.Request) {
		if c.Resolve(r.URL.Query().Get("sessionId"), DecisionAccept) {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "no such pending session", http.StatusNotFound)
		}
	})
	mux.HandleFunc("POST /decline", func(w http.ResponseWriter, r *http.Request) {
		if c.Resolve(r.URL.Query().Get("sessionId"), DecisionDecline) {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "no such pending session", http.StatusNotFound)
		}
	})
	go func() {
		<-stop
		l.Close()
	}()
	return http.Serve(l, mux)
}

// ctlCall is the client side of the control socket.
func ctlCall(sockPath, action, sessionID string) error {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("wiredrop: daemon not running (no control socket)")
	}
	defer conn.Close()
	// Minimal hand-rolled HTTP over the unix socket — two endpoints, no
	// Transport gymnastics needed.
	req := fmt.Sprintf("POST /%s?sessionId=%s HTTP/1.0\r\nHost: localhost\r\nContent-Length: 0\r\n\r\n", action, sessionID)
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	if n == 0 || !strings.Contains(string(buf[:n]), "200") {
		return fmt.Errorf("wiredrop: no such pending session")
	}
	return nil
}

func humanSize(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB"} {
		f /= u
		if f < u {
			return fmt.Sprintf("%.1f %s", f, s)
		}
	}
	return fmt.Sprintf("%.1f PiB", f/u)
}
