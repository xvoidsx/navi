package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Peer is one discovered device.
type Peer struct {
	Alias       string    `json:"alias"`
	Fingerprint string    `json:"fingerprint"`
	DeviceModel string    `json:"deviceModel"`
	DeviceType  string    `json:"deviceType"`
	Addresses   []string  `json:"addresses"` // IPs we've seen it on
	Port        int       `json:"port"`
	LastSeen    time.Time `json:"lastSeen"`
	Via         string    `json:"via"` // "lan", "tailnet", or "lan+tailnet"
}

// PeerCache is the daemon's discovery record, persisted to
// $XDG_RUNTIME_DIR/wiredrop/peers.json so `wiredrop ls` and the TUI can
// read it without any socket protocol between our own components.
// Dedupe key is the fingerprint: a device seen on LAN and tailnet merges
// into one entry.
type PeerCache struct {
	mu    sync.Mutex
	path  string
	peers map[string]*Peer // fingerprint -> peer
}

// NewPeerCache creates the cache, loading any persisted state.
func NewPeerCache(path string) *PeerCache {
	c := &PeerCache{path: path, peers: map[string]*Peer{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var list []*Peer
	if err := json.Unmarshal(data, &list); err != nil {
		return c
	}
	for _, p := range list {
		if p.Fingerprint != "" {
			c.peers[p.Fingerprint] = p
		}
	}
	return c
}

// Upsert records a sighting, merging addresses and keeping the freshest
// alias. via is "lan" or "tailnet".
func (c *PeerCache) Upsert(info DeviceInfo, ip string, via string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A reinstall keeps alias+address but changes the fingerprint. Drop
	// the stale entry so the cache doesn't accumulate ghosts under one
	// alias — trust keys off the fingerprint, so the 6-word ceremony and
	// re-pin still run for the new identity.
	for fp, p := range c.peers {
		if fp == info.Fingerprint {
			continue
		}
		if strings.EqualFold(p.Alias, info.Alias) && addrIn(p.Addresses, ip) {
			delete(c.peers, fp)
		}
	}
	p, ok := c.peers[info.Fingerprint]
	if !ok {
		p = &Peer{Fingerprint: info.Fingerprint}
		c.peers[info.Fingerprint] = p
	}
	p.Alias = info.Alias
	if info.DeviceModel != nil {
		p.DeviceModel = *info.DeviceModel
	}
	p.DeviceType = info.DeviceType
	if info.Port != 0 {
		p.Port = info.Port
	}
	if !addrIn(p.Addresses, ip) && ip != "" {
		p.Addresses = append(p.Addresses, ip)
		if len(p.Addresses) > 8 {
			p.Addresses = p.Addresses[len(p.Addresses)-8:]
		}
	}
	if p.Via == "" {
		p.Via = via
	} else if p.Via != via && p.Via != "lan+tailnet" {
		p.Via = "lan+tailnet"
	}
	p.LastSeen = time.Now()
	c.saveLocked()
}

func addrIn(addrs []string, ip string) bool {
	for _, a := range addrs {
		if a == ip {
			return true
		}
	}
	return false
}

// List returns peers sorted by most-recently-seen.
func (c *PeerCache) List() []*Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Peer, 0, len(c.peers))
	for _, p := range c.peers {
		cp := *p
		cp.Addresses = append([]string(nil), p.Addresses...)
		out = append(out, &cp)
	}
	// Most recent first (simple insertion sort — peer counts are tiny).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].LastSeen.After(out[j-1].LastSeen); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ByFingerprint returns one peer by fingerprint, if known.
func (c *PeerCache) ByFingerprint(fp string) (*Peer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.peers[fp]
	if !ok {
		return nil, false
	}
	cp := *p
	cp.Addresses = append([]string(nil), p.Addresses...)
	return &cp, true
}

// saveLocked persists the cache. Best-effort: a failed write must never
// take down discovery.
func (c *PeerCache) saveLocked() {
	list := make([]*Peer, 0, len(c.peers))
	for _, p := range c.peers {
		list = append(list, p)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.path), 0o700)
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, c.path)
}
