package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

// ─────────────────────────────────────────────────────────────────────────
// navi-imprint — native ISO flasher for navi.
//
// Pick an .iso, pick a USB/SD device, confirm, flash. No Electron, no
// gconf2, no 200MB Chromium runtime to write a disk image.
//
// Safety is the whole design:
//   - only removable/USB block devices are ever listed
//   - the device holding / or /boot is never listed, even if removable
//   - flashing requires typing the device name to confirm
//
// Wraps lsblk for discovery; the write itself is Go (io.Copy with a
// progress reader) so we own the progress bar.
// ─────────────────────────────────────────────────────────────────────────

var frameWidth = 62

// ── Screens ──────────────────────────────────────────────────────────────

type screen int

const (
	screenPickISO screen = iota
	screenPickDevice
	screenConfirm
	screenFlashing
	screenDone
)

// ── Domain types ─────────────────────────────────────────────────────────

type Device struct {
	Name   string // sdb
	Path   string // /dev/sdb
	Size   string // 28.7G
	Model  string
	Tran   string // usb
}

type lsblkOut struct {
	Blockdevices []lsblkDev `json:"blockdevices"`
}

type lsblkDev struct {
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	Size       string     `json:"size"`
	Model      string     `json:"model"`
	Tran       string     `json:"tran"`
	Rm         bool       `json:"rm"`
	Mountpoint *string    `json:"mountpoint"`
	Children   []lsblkDev `json:"children"`
}

// listDevices returns candidate flash targets: removable or USB block
// devices, excluding anything holding / or /boot.
func listDevices() ([]Device, error) {
	out, err := exec.Command("lsblk", "--json", "-o",
		"NAME,PATH,SIZE,MODEL,TRAN,RM,MOUNTPOINT").Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed: %w", err)
	}
	var parsed lsblkOut
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse lsblk: %w", err)
	}

	// find devices holding the running system
	systemDevs := map[string]bool{}
	var markSystem func(d lsblkDev)
	markSystem = func(d lsblkDev) {
		if d.Mountpoint != nil && (*d.Mountpoint == "/" || *d.Mountpoint == "/boot" ||
			strings.HasPrefix(*d.Mountpoint, "/boot/")) {
			systemDevs[d.Name] = true
		}
		for _, c := range d.Children {
			markSystem(c)
		}
	}

	var devs []Device
	for _, d := range parsed.Blockdevices {
		markSystem(d)
		// whole-disk entries only (no partitions)
		if strings.Contains(d.Name, "p") && len(d.Children) == 0 {
			// heuristic: skip partition-like leaf names; lsblk top level
			// is already whole disks, so this rarely triggers
		}
		// candidate: removable or USB transport
		if !d.Rm && d.Tran != "usb" {
			continue
		}
		if systemDevs[d.Name] {
			continue
		}
		// also exclude if any child holds /
		skip := false
		var checkChildren func(dd lsblkDev)
		checkChildren = func(dd lsblkDev) {
			if systemDevs[dd.Name] {
				skip = true
			}
			for _, c := range dd.Children {
				checkChildren(c)
			}
		}
		checkChildren(d)
		if skip {
			continue
		}
		devs = append(devs, Device{
			Name:  d.Name,
			Path:  d.Path,
			Size:  d.Size,
			Model: strings.TrimSpace(d.Model),
			Tran:  d.Tran,
		})
	}
	return devs, nil
}

// listISOs finds .iso files under common locations.
func listISOs() []string {
	var isos []string
	seen := map[string]bool{}
	dirs := []string{}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(h, "Downloads"),
			filepath.Join(h, "ISOs"),
			filepath.Join(h, "iso"),
			h,
		)
	}
	dirs = append(dirs, "/tmp", "/opt/navi-iso")
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if !seen[p] {
				seen[p] = true
				isos = append(isos, p)
			}
		}
	}
	sort.Slice(isos, func(i, j int) bool {
		ai, _ := os.Stat(isos[i])
		aj, _ := os.Stat(isos[j])
		if ai == nil || aj == nil {
			return isos[i] < isos[j]
		}
		return ai.ModTime().After(aj.ModTime())
	})
	return isos
}

// ── Flashing ─────────────────────────────────────────────────────────────

type progressReader struct {
	r        io.Reader
	total    int64
	read     int64
	onUpdate func(read, total int64)
	lastSent time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		// throttle updates to ~10/sec
		if time.Since(p.lastSent) > 100*time.Millisecond {
			p.lastSent = time.Now()
			p.onUpdate(p.read, p.total)
		}
	}
	return n, err
}

type flashProgressMsg struct {
	read  int64
	total int64
}

type flashDoneMsg struct {
	err error
}

// flashISO writes src to dst with progress callbacks. Must run as root
// (or with write access to the block device).
func flashISO(src, dst string, total int64, prog chan<- flashProgressMsg) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open ISO: %w", err)
	}
	defer in.Close()

	// unmount any mounted partitions on the target first
	if out, err := exec.Command("lsblk", "-rn", "-o", "MOUNTPOINT", dst).Output(); err == nil {
		for _, mp := range strings.Fields(string(out)) {
			if mp != "" {
				exec.Command("umount", mp).Run()
			}
		}
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("open %s: %w (try running with doas)", dst, err)
	}
	defer out.Close()

	pr := &progressReader{
		r:     in,
		total: total,
		onUpdate: func(read, total int64) {
			prog <- flashProgressMsg{read, total}
		},
	}
	if _, err := io.Copy(out, pr); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	// final update
	prog <- flashProgressMsg{total, total}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}
	return nil
}

// ── Model ────────────────────────────────────────────────────────────────

type model struct {
	screen   screen
	isos     []string
	isoCursor int
	devices  []Device
	devCursor int
	isoPath  string
	isoSize  int64
	device   Device
	confirmInput string
	progress float64
	progressText string
	doneErr  string
	quitting bool
	err      string
}

type devicesMsg struct {
	devs []Device
	err  error
}

func initialModel() model {
	return model{
		screen: screenPickISO,
		isos:   listISOs(),
	}
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		devs, err := listDevices()
		return devicesMsg{devs, err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case devicesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.devices = msg.devs
		}
		return m, nil

	case flashProgressMsg:
		if msg.total > 0 {
			m.progress = float64(msg.read) / float64(msg.total)
		}
		m.progressText = fmt.Sprintf("%s / %s",
			formatBytes(msg.read), formatBytes(msg.total))
		return m, nil

	case flashDoneMsg:
		m.screen = screenDone
		if msg.err != nil {
			m.doneErr = msg.err.Error()
		}
		return m, nil
	}
	return m, nil
}

func formatBytes(b int64) string {
	const u = 1024
	if b < u {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// global quit (not on confirm/flashing screens)
	if k == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	switch m.screen {
	case screenPickISO:
		switch k {
		case "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.isoCursor > 0 {
				m.isoCursor--
			}
		case "down", "j":
			if m.isoCursor < len(m.isos)-1 {
				m.isoCursor++
			}
		case "enter":
			if len(m.isos) == 0 {
				return m, nil
			}
			m.isoPath = m.isos[m.isoCursor]
			if fi, err := os.Stat(m.isoPath); err == nil {
				m.isoSize = fi.Size()
			}
			m.screen = screenPickDevice
		}

	case screenPickDevice:
		switch k {
		case "q", "esc", "backspace":
			m.screen = screenPickISO
			return m, nil
		case "up", "k":
			if m.devCursor > 0 {
				m.devCursor--
			}
		case "down", "j":
			if m.devCursor < len(m.devices)-1 {
				m.devCursor++
			}
		case "r":
			return m, func() tea.Msg {
				devs, err := listDevices()
				return devicesMsg{devs, err}
			}
		case "enter":
			if len(m.devices) == 0 {
				return m, nil
			}
			m.device = m.devices[m.devCursor]
			m.confirmInput = ""
			m.screen = screenConfirm
		}

	case screenConfirm:
		switch k {
		case "esc":
			m.screen = screenPickDevice
			m.confirmInput = ""
			return m, nil
		case "backspace":
			if len(m.confirmInput) > 0 {
				m.confirmInput = m.confirmInput[:len(m.confirmInput)-1]
			}
		case "enter":
			// must type the device name (e.g. sdb) to confirm
			if m.confirmInput == m.device.Name {
				m.screen = screenFlashing
				m.progress = 0
				return m, m.startFlash()
			}
		default:
			// accumulate typed characters
			if len(k) == 1 {
				m.confirmInput += k
			}
		}

	case screenDone:
		if k == "q" || k == "esc" || k == "enter" {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// prog is set in main before Run so the flash goroutine can stream
// progress messages into the event loop.
var prog *tea.Program

func (m model) startFlash() tea.Cmd {
	src := m.isoPath
	dst := m.device.Path
	total := m.isoSize
	return func() tea.Msg {
		ch := make(chan flashProgressMsg, 32)
		done := make(chan error, 1)
		go func() {
			done <- flashISO(src, dst, total, ch)
		}()
		// forward progress messages into the tea event loop
		go func() {
			for pm := range ch {
				if prog != nil {
					prog.Send(pm)
				}
			}
		}()
		err := <-done
		close(ch)
		return flashDoneMsg{err}
	}
}

// ── View ─────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder

	b.WriteString(theme.Logo.Render("∅") + " " +
		theme.Title.Render("navi imprint") + "\n")
	b.WriteString(theme.Divider(frameWidth) + "\n")

	switch m.screen {
	case screenPickISO:
		b.WriteString(m.viewPickISO())
	case screenPickDevice:
		b.WriteString(m.viewPickDevice())
	case screenConfirm:
		b.WriteString(m.viewConfirm())
	case screenFlashing:
		b.WriteString(m.viewFlashing())
	case screenDone:
		b.WriteString(m.viewDone())
	}

	b.WriteString("\n" + m.footer())
	return b.String()
}

func (m model) footer() string {
	switch m.screen {
	case screenPickISO:
		return theme.Footer(true,
			[2]string{"↑↓", "select"},
			[2]string{"enter", "choose"},
			[2]string{"q", "quit"})
	case screenPickDevice:
		return theme.Footer(true,
			[2]string{"↑↓", "select"},
			[2]string{"enter", "choose"},
			[2]string{"r", "rescan"},
			[2]string{"esc", "back"})
	case screenConfirm:
		return theme.Footer(true,
			[2]string{"type device name", "confirm"},
			[2]string{"esc", "back"})
	case screenFlashing:
		return theme.Footer(false, [2]string{"", "writing — do not remove the device…"})
	case screenDone:
		return theme.Footer(true, [2]string{"enter", "done"})
	}
	return ""
}

func (m model) viewPickISO() string {
	var b strings.Builder
	b.WriteString("  " + theme.Header.Render("SELECT IMAGE") + "\n")
	if len(m.isos) == 0 {
		b.WriteString("  " + theme.Dimmed.Render("no .iso files found in ~/Downloads, ~/ISOs, /tmp") + "\n")
		return b.String()
	}
	for i, p := range m.isos {
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Foreground(theme.White)
		if i == m.isoCursor {
			cursor = theme.Selected.Render("▸ ")
			nameStyle = nameStyle.Bold(true)
		}
		base := filepath.Base(p)
		var size string
		if fi, err := os.Stat(p); err == nil {
			size = "  " + theme.Dimmed.Render(formatBytes(fi.Size()))
		}
		b.WriteString(fmt.Sprintf("%s%s%s\n", cursor,
			nameStyle.Render(base), size))
		b.WriteString("    " + theme.Dimmed.Render(p) + "\n")
	}
	return b.String()
}

func (m model) viewPickDevice() string {
	var b strings.Builder
	b.WriteString("  " + theme.Header.Render("SELECT TARGET DEVICE") + "\n")
	b.WriteString("  " + theme.Dimmed.Render("image: "+filepath.Base(m.isoPath)) + "\n\n")
	if m.err != "" {
		b.WriteString("  " + theme.Error.Render(m.err) + "\n")
		return b.String()
	}
	if len(m.devices) == 0 {
		b.WriteString("  " + theme.Dimmed.Render("no removable USB/SD devices found.") + "\n")
		b.WriteString("  " + theme.Dimmed.Render("plug one in and press r to rescan.") + "\n")
		return b.String()
	}
	green := lipgloss.NewStyle().Foreground(theme.Green)
	for i, d := range m.devices {
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Foreground(theme.White)
		if i == m.devCursor {
			cursor = theme.Selected.Render("▸ ")
			nameStyle = nameStyle.Bold(true)
		}
		line := fmt.Sprintf("%s%s %s", cursor, green.Render(d.Path),
			nameStyle.Render(d.Name))
		line += "  " + theme.Dimmed.Render(d.Size)
		if d.Model != "" {
			line += "  " + theme.Dimmed.Render(d.Model)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n  " + theme.Dimmed.Render("system disks are never listed.") + "\n")
	return b.String()
}

func (m model) viewConfirm() string {
	var b strings.Builder
	red := lipgloss.NewStyle().Foreground(theme.Red).Bold(true)
	b.WriteString("\n")
	b.WriteString("  " + red.Render("⚠  THIS WILL ERASE EVERYTHING ON") + "\n")
	b.WriteString("  " + red.Render(m.device.Path) +
		theme.Dimmed.Render(fmt.Sprintf("  (%s %s)", m.device.Size, m.device.Model)) + "\n\n")
	b.WriteString("  " + theme.Normal.Render("image:  ") + filepath.Base(m.isoPath) + "\n")
	b.WriteString("  " + theme.Normal.Render("target: ") + m.device.Path + "\n\n")
	b.WriteString("  " + theme.Dimmed.Render(
		fmt.Sprintf("type %s to confirm:", m.device.Name)) + "\n")
	b.WriteString("  " + theme.Selected.Render(m.confirmInput) +
		theme.Dimmed.Render("▌") + "\n")
	if m.confirmInput != "" && m.confirmInput != m.device.Name {
		b.WriteString("  " + theme.Dimmed.Render(
			fmt.Sprintf("(%d/%d characters)", len(m.confirmInput), len(m.device.Name))) + "\n")
	}
	return b.String()
}

func (m model) viewFlashing() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + theme.Header.Render("WRITING") + "\n")
	b.WriteString("  " + theme.Dimmed.Render(
		fmt.Sprintf("%s → %s", filepath.Base(m.isoPath), m.device.Path)) + "\n\n")
	// progress bar
	width := 46
	filled := int(m.progress * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pink := lipgloss.NewStyle().Foreground(theme.Pink)
	b.WriteString("  " + pink.Render(bar) + "\n")
	pct := fmt.Sprintf("%.0f%%", m.progress*100)
	b.WriteString("  " + theme.Dimmed.Render(pct+"  "+m.progressText) + "\n")
	return b.String()
}

func (m model) viewDone() string {
	var b strings.Builder
	b.WriteString("\n")
	if m.doneErr != "" {
		b.WriteString("  " + theme.Error.Render("✗ flash failed") + "\n\n")
		b.WriteString("  " + theme.Dimmed.Render(m.doneErr) + "\n")
	} else {
		green := lipgloss.NewStyle().Foreground(theme.Green).Bold(true)
		b.WriteString("  " + green.Render("✓ "+m.device.Path+" is ready") + "\n\n")
		b.WriteString("  " + theme.Dimmed.Render(
			"safe to remove — the image was synced to the device.") + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// ── Main ─────────────────────────────────────────────────────────────────

func main() {
	// flashing needs raw block device writes — check early with a clear message
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr,
			"navi-imprint needs block device access — run with: doas navi-imprint")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	prog = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
