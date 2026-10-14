package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TrustStatus describes what TOFU says about a (alias, fingerprint) pair.
type TrustStatus int

const (
	// TrustUnknown: this fingerprint was never seen. Transfer blocked
	// until the human confirms the 6-word ceremony.
	TrustUnknown TrustStatus = iota
	// TrustKnown: fingerprint is pinned to this alias. Proceed.
	TrustKnown
	// TrustChanged: the alias was seen before with a DIFFERENT
	// fingerprint. Refuse loudly — could be a reinstall, could be a MITM.
	// The human must forget and re-confirm the device.
	TrustChanged
)

// KnownHost is one pinned device.
type KnownHost struct {
	Fingerprint string    `json:"fingerprint"`
	Alias       string    `json:"alias"`
	FirstSeen   time.Time `json:"firstSeen"`
}

// KnownHosts is the ~/.config/wiredrop/known_hosts store. It is keyed by
// fingerprint; alias collisions are resolved by refusing (TrustChanged)
// rather than guessing.
type KnownHosts struct {
	mu      sync.Mutex
	path    string
	entries map[string]*KnownHost // fingerprint -> entry
}

// LoadKnownHosts reads the store, creating an empty one if absent.
func LoadKnownHosts(path string) (*KnownHosts, error) {
	kh := &KnownHosts{path: path, entries: map[string]*KnownHost{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return kh, nil
		}
		return nil, fmt.Errorf("wiredrop: read known_hosts: %w", err)
	}
	var list []*KnownHost
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("wiredrop: parse known_hosts: %w", err)
	}
	for _, e := range list {
		kh.entries[e.Fingerprint] = e
	}
	return kh, nil
}

// save writes the store atomically at mode 0600.
func (kh *KnownHosts) save() error {
	list := make([]*KnownHost, 0, len(kh.entries))
	for _, e := range kh.entries {
		list = append(list, e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(kh.path), 0o700); err != nil {
		return err
	}
	tmp := kh.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, kh.path)
}

// Check classifies a sighting. It never mutates: confirming an unknown
// device is an explicit Confirm call from the human.
func (kh *KnownHosts) Check(alias, fingerprint string) TrustStatus {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	if e, ok := kh.entries[fingerprint]; ok {
		_ = e
		return TrustKnown
	}
	for _, e := range kh.entries {
		if e.Alias == alias {
			return TrustChanged
		}
	}
	return TrustUnknown
}

// Confirm pins a fingerprint to an alias after the human approves the
// ceremony. Refuses to silently re-pin an alias that already maps to a
// different fingerprint — the old entry must be Forgotten first.
func (kh *KnownHosts) Confirm(alias, fingerprint string) error {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	for fp, e := range kh.entries {
		if e.Alias == alias && fp != fingerprint {
			return fmt.Errorf("wiredrop: alias %q is pinned to a different fingerprint — forget it first", alias)
		}
	}
	kh.entries[fingerprint] = &KnownHost{
		Fingerprint: fingerprint,
		Alias:       alias,
		FirstSeen:   time.Now(),
	}
	return kh.save()
}

// Forget removes every entry for an alias (the recovery path after a
// TrustChanged refusal: the human verified out-of-band that the device
// was reinstalled, then re-runs the ceremony).
func (kh *KnownHosts) Forget(alias string) bool {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	removed := false
	for fp, e := range kh.entries {
		if e.Alias == alias {
			delete(kh.entries, fp)
			removed = true
		}
	}
	if removed {
		_ = kh.save()
	}
	return removed
}

// AliasFor returns the pinned alias for a fingerprint, if any.
func (kh *KnownHosts) AliasFor(fingerprint string) (string, bool) {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	e, ok := kh.entries[fingerprint]
	if !ok {
		return "", false
	}
	return e.Alias, true
}

// All returns a snapshot of every pinned device.
func (kh *KnownHosts) All() []*KnownHost {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	out := make([]*KnownHost, 0, len(kh.entries))
	for _, e := range kh.entries {
		c := *e
		out = append(out, &c)
	}
	return out
}
