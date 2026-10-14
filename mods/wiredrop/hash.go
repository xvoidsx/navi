package main

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"time"
)

// streamHash is a tiny wrapper so the upload path reads fluently.
type streamHash struct{ h hash.Hash }

func sha256New() *streamHash { return &streamHash{h: sha256.New()} }

func (s *streamHash) Write(p []byte) (int, error) { return s.h.Write(p) }

func (s *streamHash) hex() string { return hex.EncodeToString(s.h.Sum(nil)) }

// hashFile returns the hex SHA-256 of a file's contents.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// applyFileTimes sets mtime/atime from LocalSend metadata, best-effort.
func applyFileTimes(path string, m *FileMetadata) {
	if m == nil || m.Modified == nil {
		return
	}
	mt, err := time.Parse(time.RFC3339, *m.Modified)
	if err != nil {
		return
	}
	at := mt
	if m.Accessed != nil {
		if t, err := time.Parse(time.RFC3339, *m.Accessed); err == nil {
			at = t
		}
	}
	_ = os.Chtimes(path, at, mt)
}
