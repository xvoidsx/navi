package main

// art_test.go — the cover-art renderer.
//
// The geometry assertions are the important ones. Album art is rendered
// as half-blocks, so one terminal cell carries two vertical pixels; a
// mistake in the sampling maths shows up as a sheared or half-height
// image, which no unit test of the colour maths would catch.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// solidImage builds a cols x rows image of one colour.
func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// gradientImage varies per pixel so a downscale that samples only one
// source pixel per destination cell is detectable.
func gradientImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: 128, A: 255,
			})
		}
	}
	return img
}

func TestDownscaleGeometry(t *testing.T) {
	out := downscale(solidImage(600, 600, color.RGBA{200, 100, 50, 255}), 22, 16)
	b := out.Bounds()
	if b.Dx() != 22 || b.Dy() != 16 {
		t.Fatalf("bounds = %dx%d, want 22x16", b.Dx(), b.Dy())
	}
}

// The renderer dims art by 0.72 so it reads as chrome. If that factor is
// ever dropped, cover art out-shouts the pink — check it is applied.
func TestDownscaleDims(t *testing.T) {
	full := color.RGBA{255, 255, 255, 255}
	out := downscale(solidImage(10, 10, full), 4, 4)
	got := out.RGBAAt(0, 0)
	if got.R == 255 {
		t.Error("white art was not dimmed; the 0.72 factor is gone")
	}
	// 0.72 of 255 is 183, give or rounding.
	if got.R < 178 || got.R > 188 {
		t.Errorf("dimmed red = %d, want ~183 (0.72 of 255)", got.R)
	}
	if got.A != 255 {
		t.Errorf("alpha = %d, want 255 (composited over black)", got.A)
	}
}

// A box filter must average the source rect, not point-sample it. The
// source is square so the centre crop doesn't discard half of it before
// the averaging even happens.
func TestDownscaleAverages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		img.SetRGBA(x, 0, color.RGBA{0, 0, 0, 255})
		img.SetRGBA(x, 1, color.RGBA{200, 200, 200, 255})
	}
	out := downscale(img, 1, 1)
	got := out.RGBAAt(0, 0)
	// Mean of 0 and 200 is 100, then dimmed by 0.72 → ~72.
	if got.R < 66 || got.R > 80 {
		t.Errorf("averaged+dimmed red = %d, want ~72", got.R)
	}
}

// Non-zero-origin bounds are legal for an image.Image; sampling must use
// Bounds(), not assume (0,0).
func TestDownscaleNonZeroOrigin(t *testing.T) {
	base := image.NewRGBA(image.Rect(10, 20, 16, 26))
	for y := 20; y < 26; y++ {
		for x := 10; x < 16; x++ {
			base.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	out := downscale(base, 2, 2)
	if out.RGBAAt(0, 0).R == 0 {
		t.Error("non-zero-origin image sampled to black; Bounds() is being ignored")
	}
}

// A real capture from Chromium handed out a 150x84 video thumbnail, not a
// square cover. The crop exists so that doesn't get stretched, and it must
// take the centre — the middle columns of that capture are the subject.
func TestCenterSquare(t *testing.T) {
	tests := []struct {
		name               string
		r                  image.Rectangle
		wantW, wantH       int
		wantMinX, wantMinY int
	}{
		{"square", image.Rect(0, 0, 100, 100), 100, 100, 0, 0},
		// The real shape: 150x84 crops to a centred 84x84 starting at x=33.
		{"wide", image.Rect(0, 0, 150, 84), 84, 84, 33, 0},
		{"tall", image.Rect(0, 0, 84, 150), 84, 84, 0, 33},
		{"odd wide", image.Rect(0, 0, 15, 10), 10, 10, 2, 0},
		// 50x24 with a non-zero origin: side 24, centred, so the crop
		// starts 13 in from the left edge of the rect.
		{"offset origin", image.Rect(10, 20, 60, 44), 24, 24, 23, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := centerSquare(tt.r)
			if got.Dx() != tt.wantW || got.Dy() != tt.wantH {
				t.Errorf("size = %dx%d, want %dx%d", got.Dx(), got.Dy(), tt.wantW, tt.wantH)
			}
			if got.Min.X != tt.wantMinX || got.Min.Y != tt.wantMinY {
				t.Errorf("origin = (%d,%d), want (%d,%d)",
					got.Min.X, got.Min.Y, tt.wantMinX, tt.wantMinY)
			}
		})
	}
}

// Degenerate bounds must not divide by zero — a player can hand out a
// zero-dimension image and a panic here would take the whole mod down.
func TestCenterSquareDegenerate(t *testing.T) {
	for _, r := range []image.Rectangle{
		image.Rect(0, 0, 0, 0), image.Rect(0, 0, 0, 10), image.Rect(0, 0, 10, 0),
	} {
		got := centerSquare(r)
		if got.Dx() < 1 || got.Dy() < 1 {
			t.Errorf("centerSquare(%v) = %v, want at least 1x1", r, got)
		}
	}
}

// A wide image must be cropped, not squeezed. Encode the intent: colour the
// outer columns green and the centre red, then assert the centre survives.
func TestDownscaleCropsWideToCentre(t *testing.T) {
	const w, h = 150, 84
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255} // red centre
			if x < 30 || x >= w-30 {
				c = color.RGBA{0, 255, 0, 255} // green margins
			}
			img.SetRGBA(x, y, c)
		}
	}
	out := downscale(img, 4, 4)
	mid := out.RGBAAt(2, 2)
	if mid.G > mid.R {
		t.Errorf("centre cell is green (%v) — the wide image was squeezed, not cropped", mid)
	}
}

func TestRenderHalfBlocksGeometry(t *testing.T) {
	const cols, rows = 22, 8
	got := renderHalfBlocks(gradientImage(300, 300), cols, rows)
	lines := strings.Split(got, "\n")
	if len(lines) != rows {
		t.Fatalf("got %d rows, want %d", len(lines), rows)
	}
	// Each row is cols half-blocks with truecolor escapes; strip the
	// escapes and count the cells.
	for i, ln := range lines {
		if n := countCells(ln); n != cols {
			t.Errorf("row %d has %d cells, want %d", i, n, cols)
		}
	}
}

// The top pixel is the foreground and the bottom the background. If those
// are swapped the image is vertically mirrored, which is invisible on a
// symmetric gradient and obvious on real art. The source is square so the
// centre crop doesn't discard half of it first.
func TestRenderHalfBlocksPixelOrder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if y >= 2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	got := renderHalfBlocks(img, 1, 1)
	if !strings.Contains(got, "38;2;183;0;0") {
		t.Errorf("top pixel should be the dimmed red foreground, got %q", got)
	}
	if !strings.Contains(got, "48;2;0;0;183") {
		t.Errorf("bottom pixel should be the dimmed blue background, got %q", got)
	}
}

// Every row must close its colour run, or a colour bleeds into the next
// line of the frame.
func TestRenderHalfBlocksResetsEachRow(t *testing.T) {
	lines := strings.Split(renderHalfBlocks(gradientImage(40, 40), 4, 3), "\n")
	for i, ln := range lines {
		if !strings.HasSuffix(ln, "\x1b[0m") {
			t.Errorf("row %d does not end with a reset: %q", i, ln)
		}
	}
}

func countCells(s string) int {
	n := 0
	esc := false
	for _, r := range s {
		switch {
		case esc:
			if r == 'm' {
				esc = false
			}
		case r == 0x1b:
			esc = true
		default:
			n++
		}
	}
	return n
}

// The placeholder exists so the frame height never jumps. That is its
// whole job, so assert the geometry matches what real art produces.
func TestPlaceholderGeometry(t *testing.T) {
	blk := placeholder(22, 8)
	if blk.ok {
		t.Error("placeholder must not claim to be real art")
	}
	if blk.cols != 22 || blk.rows != 8 {
		t.Errorf("geometry = %dx%d, want 22x8", blk.cols, blk.rows)
	}
	lines := strings.Split(blk.rendered, "\n")
	if len(lines) != 8 {
		t.Fatalf("got %d rows, want 8", len(lines))
	}
	for i, ln := range lines {
		if n := countCells(ln); n != 22 {
			t.Errorf("row %d has %d cells, want 22", i, n)
		}
	}
}

func TestPlaceholderIsNotBlank(t *testing.T) {
	// A blank block is the original bug: an empty art slot looked like a
	// hole in the frame rather than a panel.
	if !strings.Contains(placeholder(22, 8).rendered, "▀") {
		t.Error("placeholder is empty; it must render a visible fill")
	}
}

func TestArtKey(t *testing.T) {
	if artKey("file:///a", 22, 8) != artKey("file:///a", 22, 8) {
		t.Error("same url and geometry must produce the same key")
	}
	// A resize invalidates the memo: the old block is the wrong shape.
	if artKey("file:///a", 22, 8) == artKey("file:///a", 22, 9) {
		t.Error("geometry must be part of the key")
	}
	if artKey("file:///a", 22, 8) == artKey("file:///b", 22, 8) {
		t.Error("url must be part of the key")
	}
}

// LoadArt must never fail loudly. A missing file, a bad URL, an
// unsupported scheme and a blank URL all degrade to the placeholder —
// art is personality, never a blocker.
func TestLoadArtDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cover.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, gradientImage(40, 40)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	bad := filepath.Join(dir, "notanimage.png")
	if err := os.WriteFile(bad, []byte("this is not a png"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, url string
		wantOK    bool
	}{
		{"real file url", "file://" + p, true},
		{"blank", "", false},
		{"whitespace", "   ", false},
		{"missing file", "file://" + filepath.Join(dir, "gone.png"), false},
		{"undecodable", "file://" + bad, false},
		{"unsupported scheme", "gopher://example.com/x.png", false},
		{"nonsense", "::::", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := LoadArt(tt.url, 22, 8)
			if blk.ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", blk.ok, tt.wantOK)
			}
			if blk.cols != 22 || blk.rows != 8 {
				t.Errorf("geometry = %dx%d, want 22x8 even on failure", blk.cols, blk.rows)
			}
			if lines := strings.Split(blk.rendered, "\n"); len(lines) != 8 {
				t.Errorf("got %d rows, want 8 — a failure must not change frame height",
					len(lines))
			}
		})
	}
}

// A percent-escaped file:// URL must resolve: players hand out names with
// spaces in them, and url.Parse keeps the escapes in Path.
func TestFetchArtPercentEscapedPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cover art.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradientImage(20, 20)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fetchArt("file://" + strings.ReplaceAll(p, " ", "%20"))
	if err != nil {
		t.Fatalf("fetchArt: %v", err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Error("fetched bytes differ from the file on disk")
	}
}

func TestLoadArtClampsGeometry(t *testing.T) {
	// Zero or negative geometry must not divide by zero or panic.
	for _, tt := range []struct{ cols, rows int }{{0, 0}, {-1, 5}, {5, -1}} {
		blk := LoadArt("", tt.cols, tt.rows)
		if blk.cols < 1 || blk.rows < 1 {
			t.Errorf("geometry %dx%d was not clamped, got %dx%d",
				tt.cols, tt.rows, blk.cols, blk.rows)
		}
	}
}
