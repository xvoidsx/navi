package main

import (
	"path/filepath"
	"testing"
)

func testCache(t *testing.T) *PeerCache {
	t.Helper()
	return NewPeerCache(filepath.Join(t.TempDir(), "peers.json"))
}

func devInfo(alias, fp string, port int) DeviceInfo {
	return DeviceInfo{Alias: alias, Fingerprint: fp, Port: port}
}

// A reinstall keeps alias+address but changes fingerprint: the stale
// entry must be dropped, not kept as a ghost that breaks resolvePeer.
func TestUpsertReinstallDropsGhost(t *testing.T) {
	c := testCache(t)
	c.Upsert(devInfo("bravo", "aaa", 53317), "127.0.0.1", "lan")
	c.Upsert(devInfo("bravo", "bbb", 53317), "127.0.0.1", "lan")
	peers := c.List()
	if len(peers) != 1 {
		t.Fatalf("want 1 peer after reinstall, got %d", len(peers))
	}
	if peers[0].Fingerprint != "bbb" {
		t.Fatalf("want surviving fingerprint bbb, got %s", peers[0].Fingerprint)
	}
}

// Same alias on a DIFFERENT address is a different device — keep both.
func TestUpsertSameAliasDifferentAddr(t *testing.T) {
	c := testCache(t)
	c.Upsert(devInfo("bravo", "aaa", 53317), "127.0.0.1", "lan")
	c.Upsert(devInfo("bravo", "bbb", 53317), "192.168.1.5", "lan")
	peers := c.List()
	if len(peers) != 2 {
		t.Fatalf("want 2 peers for same alias on different addrs, got %d", len(peers))
	}
	if _, err := resolvePeer(c, "bravo"); err == nil {
		t.Fatal("want ambiguity error for two same-alias peers, got nil")
	}
}

// resolvePeer still finds a single alias match.
func TestResolvePeerAlias(t *testing.T) {
	c := testCache(t)
	c.Upsert(devInfo("bravo", "bbb", 53317), "127.0.0.1", "lan")
	p, err := resolvePeer(c, "BRAVO")
	if err != nil {
		t.Fatal(err)
	}
	if p.Fingerprint != "bbb" {
		t.Fatalf("want bbb, got %s", p.Fingerprint)
	}
}
