package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// startDiscovery runs the two discovery paths from the spec until ctx
// ends:
//
//   - LAN: LocalSend multicast (224.0.0.167:53317) — announce every 30s +
//     on startup, listen continuously, answer via /register. This is what
//     makes phones and official LocalSend apps see us and vice versa.
//   - Tailscale: multicast does NOT cross the tailnet, so instead we parse
//     `tailscale status --json` and POST /register directly at each peer's
//     tailnet IP.
//
// A one-time HTTPS sweep of the local subnets covers the cold-start gap
// (a fresh daemon would otherwise wait up to 30s for the next announce).
func startDiscovery(ctx context.Context, srv *Server) {
	go announceLoop(ctx, srv)
	go listenLoop(ctx, srv)
	go tailscaleLoop(ctx, srv)
	go func() {
		lanSweep(srv) // cold start
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				lanSweep(srv)
			}
		}
	}()
}

// insecureClient talks to self-signed peers. The fingerprint is verified
// out-of-band by TOFU afterwards — never trust the JSON body alone.
func insecureClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// postRegister performs the two-way discovery handshake against one
// address. Returns the peer's info on success.
func postRegister(ip string, port int, self DeviceInfo) (*DeviceInfo, error) {
	body, _ := json.Marshal(self)
	url := "https://" + net.JoinHostPort(ip, itoa(port)) + apiPrefix + "/register"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := insecureClient(5 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errBadStatus(resp.StatusCode)
	}
	var info DeviceInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	// Discovery is informational, but the peer cache keys trust off this
	// fingerprint — take it from the TLS handshake, not the JSON body.
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		info.Fingerprint = fingerprintOf(resp.TLS.PeerCertificates[0])
	}
	return &info, nil
}

type httpStatusError int

func (e httpStatusError) Error() string { return "unexpected status " + itoa(int(e)) }

func errBadStatus(code int) error { return httpStatusError(code) }

// selfInfo snapshots our announce/register identity.
func (s *Server) selfInfo() DeviceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// announceLoop multicasts our presence every 30s (plus once immediately).
func announceLoop(ctx context.Context, srv *Server) {
	send := func() {
		self := srv.selfInfo()
		yes := true
		self.Announce = &yes
		body, err := json.Marshal(self)
		if err != nil {
			return
		}
		addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
		if err != nil {
			return
		}
		// Dial without binding: the packet leaves via the default route.
		// Multihomed machines announce on their primary LAN, which is
		// where LocalSend peers live in practice.
		c, err := net.DialUDP("udp4", nil, addr)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Write(body)
	}
	send()
	t := time.NewTicker(announceIntervalSeconds * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}

// listenLoop receives multicast announces and answers them.
func listenLoop(ctx context.Context, srv *Server) {
	addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		log.Printf("wiredrop: multicast resolve: %v", err)
		return
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("wiredrop: multicast listen: %v (LAN discovery disabled)", err)
		return
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(1 << 20)
	buf := make([]byte, 64<<10)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		var ann DeviceInfo
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue
		}
		self := srv.selfInfo()
		if ann.Fingerprint == "" || ann.Fingerprint == self.Fingerprint {
			continue // self or garbage
		}
		srcIP := src.IP.String()
		if ann.Announce != nil && *ann.Announce {
			// Two-way: answer directly via /register.
			port := ann.Port
			if port == 0 {
				port = DefaultPort
			}
			if peer, err := postRegister(srcIP, port, self); err == nil && peer.Fingerprint != "" {
				srv.peers.Upsert(*peer, srcIP, "lan")
			} else {
				// Fallback: record what the announce told us.
				srv.peers.Upsert(ann, srcIP, "lan")
			}
		} else {
			srv.peers.Upsert(ann, srcIP, "lan")
		}
	}
}

// tailscalePeers parses `tailscale status --json` into tailnet IPs.
func tailscalePeers() []string {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return nil
	}
	var st struct {
		Peer map[string]struct {
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nil
	}
	var ips []string
	for _, p := range st.Peer {
		for _, ip := range p.TailscaleIPs {
			if parsed := net.ParseIP(ip); parsed != nil && tailnetNet.Contains(parsed) {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

// tailscaleLoop registers directly against tailnet peers every minute.
// Multicast doesn't cross the tailnet, so this is the only way they find
// us (and we find them).
func tailscaleLoop(ctx context.Context, srv *Server) {
	sweep := func() {
		self := srv.selfInfo()
		for _, ip := range tailscalePeers() {
			port := DefaultPort
			// Prefer a cached port for this IP if we've seen it.
			for _, peer := range srv.peers.List() {
				for _, a := range peer.Addresses {
					if a == ip && peer.Port != 0 {
						port = peer.Port
					}
				}
			}
			if peer, err := postRegister(ip, port, self); err == nil && peer.Fingerprint != "" {
				srv.peers.Upsert(*peer, ip, "tailnet")
			}
		}
	}
	sweep()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

// lanSweep POSTs /register across our local subnets — the protocol's HTTP
// discovery fallback. Parallel, short timeouts, best-effort.
func lanSweep(srv *Server) {
	self := srv.selfInfo()
	var targets []string
	for _, addr := range listenAddrs() {
		ip := net.ParseIP(addr)
		if ip == nil || ip.IsLoopback() {
			continue
		}
		// Sweep the /24 containing each LAN address.
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		for i := 1; i < 255; i++ {
			cand := net.IPv4(v4[0], v4[1], v4[2], byte(i)).String()
			if cand == addr {
				continue
			}
			targets = append(targets, cand)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for _, ip := range targets {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if peer, err := postRegister(ip, DefaultPort, self); err == nil && peer.Fingerprint != "" {
				if peer.Fingerprint != self.Fingerprint {
					srv.peers.Upsert(*peer, ip, "lan")
				}
			}
		}(ip)
	}
	wg.Wait()
}
