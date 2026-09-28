package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeTraversal(t *testing.T) {
	for _, evil := range []string{
		"../evil.sh",
		"../../etc/passwd",
		"/absolute/path.png",
		"..",
		".",
		"",
		"a/../../b",
	} {
		name, err := sanitizeFileName(evil)
		if err == nil {
			// "a/../../b" -> Base is "b", which is fine and expected.
			if strings.Contains(name, "/") || strings.Contains(name, "\\") || name == ".." {
				t.Errorf("sanitize(%q) = %q: still dangerous", evil, name)
			}
			continue
		}
		// Rejection is also fine.
	}
	// The critical property: nothing but a bare name comes out.
	for _, evil := range []string{"../x", "/y", "a/b/c.png", "..\\win"} {
		name, err := sanitizeFileName(evil)
		if err != nil {
			continue
		}
		if strings.ContainsAny(name, `/\`) {
			t.Errorf("sanitize(%q) = %q: contains separator", evil, name)
		}
	}
}

func TestSanitizeKeepsGoodNames(t *testing.T) {
	for in, want := range map[string]string{
		"photo.png":       "photo.png",
		"my file (1).jpg": "my file (1).jpg",
		"ünïcödé.txt":     "ünïcödé.txt",
	} {
		got, err := sanitizeFileName(in)
		if err != nil {
			t.Errorf("sanitize(%q) error: %v", in, err)
		} else if got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeLengthCap(t *testing.T) {
	long := strings.Repeat("a", 300) + ".png"
	got, err := sanitizeFileName(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > maxFileNameLen {
		t.Errorf("name too long: %d", len(got))
	}
	if !strings.HasSuffix(got, ".png") {
		t.Errorf("extension lost: %q", got)
	}
}

func TestUniqueDestPath(t *testing.T) {
	dir := t.TempDir()
	p1, err := uniqueDestPath(dir, "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(dir, "photo.png") {
		t.Errorf("first dest = %q", p1)
	}
	if err := os.WriteFile(p1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := uniqueDestPath(dir, "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if p2 != filepath.Join(dir, "photo-1.png") {
		t.Errorf("collision dest = %q, want photo-1.png", p2)
	}
	// And the final path is inside dir, always.
	if !strings.HasPrefix(filepath.Clean(p2), filepath.Clean(dir)) {
		t.Errorf("dest escapes dir: %q", p2)
	}
	// Traversal attempt can never escape.
	p3, err := uniqueDestPath(dir, "../../evil")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Clean(p3), filepath.Clean(dir)+string(os.PathSeparator)) {
		t.Errorf("traversal escaped: %q", p3)
	}
}
