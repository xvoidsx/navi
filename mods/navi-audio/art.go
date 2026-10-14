package main

// art.go — album art.
//
// MPRIS hands out an art URL: usually a file:// path into the player's
// cache, sometimes http(s). We decode it with the standard library (no
// image dependency), downscale, and render it as a truecolor half-block
// block — the same ▀ trick navi-networking uses for its QR, so cover art
// and QR codes are made of the same stuff.
//
// Two rules from mods/AGENTS.md apply here:
//
//   - the art block is always the same size, art or not. A missing cover
//     renders a placeholder of identical dimensions, so the frame height
//     never jumps between tracks.
//   - the block is memoised by (url, cols, rows). Art is decoded once per
//     track, not once per tick, which is the whole reason the fast tick
//     can afford to fire four times a second.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // register JPEG
	_ "image/png"  // register PNG
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const (
	// artDimQ is artDim scaled to a fixed-point 0..255 so the dim step
	// stays integer maths. 0.72 * 255 = 183.
	artDimQ    = 183
	artMaxHTTP = 12 << 20
	artTimeout = 8 * time.Second
)

// artBlock is one rendered, ready-to-print cover.
type artBlock struct {
	cols, rows int
	rendered   string
	ok         bool
}

// artDir is where downloaded covers live. Players hand out URLs that
// point at a temp file they delete on the next track, so a downloaded
// cover has to outlive the track that asked for it.
func artDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "navi-nowplaying", "art")
}

// artKey is the memo key the model holds: the same cover at the same
// geometry is the same block, so nothing is ever decoded twice.
func artKey(artURL string, cols, rows int) string {
	return fmt.Sprintf("%s|%dx%d", artURL, cols, rows)
}

// LoadArt returns the cover for artURL at the given cell geometry. A
// blank URL, an unreachable file, or a decode failure all return the
// placeholder rather than an error: art is personality, never a blocker.
//
// This touches the filesystem and an image decoder, so the model runs it
// in a tea.Cmd goroutine — never on the render path.
func LoadArt(artURL string, cols, rows int) artBlock {
	if cols < 1 || rows < 1 {
		cols, rows = 1, 1
	}
	if strings.TrimSpace(artURL) == "" {
		return placeholder(cols, rows)
	}
	data, err := fetchArt(artURL)
	if err != nil {
		return placeholder(cols, rows)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return placeholder(cols, rows)
	}
	return artBlock{cols: cols, rows: rows, ok: true, rendered: renderHalfBlocks(img, cols, rows)}
}

// fetchArt resolves an art URL to bytes. file:// is read directly;
// http(s) is downloaded once and cached on disk.
func fetchArt(artURL string) ([]byte, error) {
	u, err := url.Parse(artURL)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "file", "":
		p := u.Path
		if p == "" {
			p = artURL
		}
		// url.Parse keeps percent-escapes in Path; players hand out
		// names with spaces in them.
		if dec, err := url.PathUnescape(p); err == nil {
			p = dec
		}
		return os.ReadFile(p)
	case "http", "https":
		return downloadArt(u.String())
	default:
		return nil, fmt.Errorf("unsupported art scheme %q", u.Scheme)
	}
}

func downloadArt(rawurl string) ([]byte, error) {
	dir := artDir()
	if dir == "" {
		return nil, fmt.Errorf("no cache dir")
	}
	sum := sha256.Sum256([]byte(rawurl))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])[:24])

	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return b, nil
	}

	client := &http.Client{Timeout: artTimeout}
	resp, err := client.Get(rawurl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("art http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, artMaxHTTP))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
	return data, nil
}

// renderHalfBlocks paints img into a cols x rows cell block.
//
// One cell carries two vertical pixels, so the source is sampled at
// cols x (rows*2) and each cell emits ▀ with the upper pixel as
// foreground and the lower as background. The per-cell reset is skipped;
// every cell sets both channels, and each row closes with one reset.
func renderHalfBlocks(img image.Image, cols, rows int) string {
	src := downscale(img, cols, rows*2)
	bounds := src.Bounds()

	var b strings.Builder
	b.Grow(cols * rows * 26)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			top := src.RGBAAt(c, r*2)
			bot := top
			if r*2+1 < bounds.Dy() {
				bot = src.RGBAAt(c, r*2+1)
			}
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		b.WriteString("\x1b[0m")
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// downscale box-filters img into a cols x rows RGBA image composited over
// black and dimmed. A box filter rather than nearest-neighbour because
// cover art is a photograph: point sampling at this size shimmers, and
// averaging keeps it looking like a thumbnail.
//
// The source is centre-cropped to a square first. The art block is
// deliberately near-square so the frame height never moves, and a real
// capture showed why that needs the crop: Chromium hands out a 150x84
// video thumbnail, and stretching that into a square smears the subject.
// Cropping keeps the centre of the frame, which is what a cover is.
func downscale(img image.Image, cols, rows int) *image.RGBA {
	src := centerSquare(img.Bounds())
	sw, sh := src.Dx(), src.Dy()
	if sw < 1 || sh < 1 {
		return image.NewRGBA(image.Rect(0, 0, cols, rows))
	}
	dst := image.NewRGBA(image.Rect(0, 0, cols, rows))
	for y := 0; y < rows; y++ {
		y0 := src.Min.Y + y*sh/rows
		y1 := src.Min.Y + (y+1)*sh/rows
		if y1 <= y0 {
			y1 = min(y0+1, src.Max.Y)
		}
		for x := 0; x < cols; x++ {
			x0 := src.Min.X + x*sw/cols
			x1 := src.Min.X + (x+1)*sw/cols
			if x1 <= x0 {
				x1 = min(x0+1, src.Max.X)
			}
			var r, g, bl, n uint32
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, _ := img.At(xx, yy).RGBA()
					r += cr
					g += cg
					bl += cb
					n++
				}
			}
			if n == 0 {
				continue
			}
			// RGBA() is alpha-premultiplied, so over black the stored
			// colour is already composited — just rescale 16 -> 8 bits.
			dst.SetRGBA(x, y, color8(r/n, g/n, bl/n))
		}
	}
	return dst
}

// centerSquare inscribes the largest centred square in r. Degenerate
// bounds return a 1x1 rect so callers never divide by zero.
func centerSquare(r image.Rectangle) image.Rectangle {
	w, h := r.Dx(), r.Dy()
	if w < 1 || h < 1 {
		return image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Min.Y+1)
	}
	side := min(w, h)
	dx := (w - side) / 2
	dy := (h - side) / 2
	return image.Rect(
		r.Min.X+dx, r.Min.Y+dy,
		r.Min.X+dx+side, r.Min.Y+dy+side,
	)
}

// color8 rescales 16-bit premultiplied channels to 8-bit and dims them,
// so cover art reads as background chrome rather than competing with the
// pink for attention. The 0.72 dim matches nslock.sh's image treatment.
func color8(r, g, b uint32) color.RGBA {
	m := func(v uint32) uint8 {
		v >>= 8 // 16-bit -> 8-bit
		return uint8((v*artDimQ + 127) / 255)
	}
	return color.RGBA{R: m(r), G: m(g), B: m(b), A: 255}
}

// placeholder returns an art-sized block for "no cover here".
//
// Same geometry as real art, so the frame never changes height between a
// track with a cover and one without. The fill is theme.Empty — a solid
// slab a hair above the panel background, so a missing cover reads as an
// empty sleeve rather than as an absence or as a hole. An earlier
// version hatched it with diagonal dots; that read as decoration and
// pulled the eye off the title. A quiet fill says "nothing here" and
// nothing more. Half-blocks keep the exact cell rhythm of real art.
func placeholder(cols, rows int) artBlock {
	b := artBlock{cols: cols, rows: rows}
	cell := lipgloss.NewStyle().Foreground(theme.Empty).Background(theme.Empty)
	sb := strings.Builder{}
	sb.Grow(cols * rows * 12)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			sb.WriteString(cell.Render("▀"))
		}
		if r < rows-1 {
			sb.WriteByte('\n')
		}
	}
	b.rendered = sb.String()
	return b
}
