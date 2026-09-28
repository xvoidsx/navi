package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TransferRecord is one line of the transfer history.
type TransferRecord struct {
	Time        time.Time `json:"time"`
	Direction   string    `json:"direction"` // "sent" or "received"
	Peer        string    `json:"peer"`
	Files       []string  `json:"files"`
	Bytes       int64     `json:"bytes"`
	Fingerprint string    `json:"fingerprint"`
}

// History is a capped JSON-lines log of transfers.
type History struct {
	mu   sync.Mutex
	path string
}

// NewHistory locates the log at ~/.local/share/wiredrop/history.jsonl.
func NewHistory() *History {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return &History{path: filepath.Join(base, "wiredrop", "history.jsonl")}
}

// Append records a transfer, keeping the last 200.
func (h *History) Append(r TransferRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(h.path), 0o700)
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
	f.Close()
	// Cap: rewrite without the oldest lines when over 200.
	if lines, err := os.ReadFile(h.path); err == nil {
		n := 0
		for _, b := range lines {
			if b == '\n' {
				n++
			}
		}
		if n > 200 {
			parts := splitLines(string(lines))
			keep := parts[len(parts)-200:]
			_ = os.WriteFile(h.path, []byte(joinLines(keep)), 0o600)
		}
	}
}

// Recent returns up to n records, newest first.
func (h *History) Recent(n int) []TransferRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.path)
	if err != nil {
		return nil
	}
	var out []TransferRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var r TransferRecord
		if err := json.Unmarshal([]byte(line), &r); err == nil {
			out = append(out, r)
		}
	}
	// Newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		if l != "" {
			out += l + "\n"
		}
	}
	return out
}
