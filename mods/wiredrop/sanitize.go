package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxFileNameLen caps received file names. 255 is the common filesystem
// limit; we stay well under it.
const maxFileNameLen = 200

// sanitizeFileName turns an untrusted remote fileName into a safe local
// name. Rules: strip to the base component (kills "../" and absolute
// paths), reject empty/dot names, cap length, and keep the extension when
// truncating. The returned name is guaranteed to contain no path
// separator.
func sanitizeFileName(name string) (string, error) {
	base := filepath.Base(name)
	// filepath.Base("") is "."; an input of ".." stays "..".
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("wiredrop: rejected unsafe file name %q", name)
	}
	// On top of Base, strip any lingering separators defensively
	// (a name like "a/b" can't survive Base, but belt and suspenders).
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("wiredrop: rejected unsafe file name %q", name)
	}
	if len(base) > maxFileNameLen {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		// Keep the extension; truncate the stem. If even the extension
		// alone is too long, hard-cut.
		keep := maxFileNameLen - len(ext)
		if keep <= 0 {
			base = base[:maxFileNameLen]
		} else {
			if len(stem) > keep {
				stem = stem[:keep]
			}
			base = stem + ext
		}
	}
	// Control characters and NUL have no business in a file name.
	var clean strings.Builder
	for _, r := range base {
		if r < 0x20 || r == 0x7f {
			continue
		}
		clean.WriteRune(r)
	}
	base = clean.String()
	if base == "" {
		return "", fmt.Errorf("wiredrop: rejected unsafe file name %q", name)
	}
	return base, nil
}

// uniqueDestPath returns a destination path inside dir for name that does
// not collide with an existing file: photo.png, photo-1.png, photo-2.png…
// It verifies the final path stays inside dir (paranoia: sanitizeFileName
// already guarantees a bare name, but the check is one line).
func uniqueDestPath(dir, name string) (string, error) {
	safe, err := sanitizeFileName(name)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(dir, safe)
	if !strings.HasPrefix(filepath.Clean(candidate), filepath.Clean(dir)+string(os.PathSeparator)) &&
		filepath.Clean(candidate) != filepath.Clean(dir) {
		return "", fmt.Errorf("wiredrop: destination escapes directory: %q", name)
	}
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	}
	ext := filepath.Ext(safe)
	stem := strings.TrimSuffix(safe, ext)
	for i := 1; ; i++ {
		try := fmt.Sprintf("%s-%d%s", stem, i, ext)
		// Re-cap in case the suffix pushed us over the limit.
		if len(try) > maxFileNameLen {
			cut := len(try) - maxFileNameLen
			if cut < len(stem) {
				stem = stem[:len(stem)-cut]
				try = fmt.Sprintf("%s-%d%s", stem, i, ext)
			} else {
				try = try[:maxFileNameLen]
			}
		}
		candidate = filepath.Join(dir, try)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
		if i > 100000 {
			return "", fmt.Errorf("wiredrop: too many collisions for %q", name)
		}
	}
}
