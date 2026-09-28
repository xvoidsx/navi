// navi-notifs — a bubble tea notifications mod for navi.
//
// A terminal viewer over dunst's notification history: browse recent
// notifications, invoke an action, dismiss one, or clear them all with a
// glitch. Styled entirely through the shared nightshadeNeon theme package
// (mods/theme). Launched from the waybar bell module via notif-open.sh.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

// frameWidth is the family-standard mod window width.
const frameWidth = 62

// ---------------------------------------------------------------------------
// dunst history
// ---------------------------------------------------------------------------

// notif is one entry from dunst's in-memory history.
type notif struct {
	ID      int64
	App     string
	Summary string
	Body    string
	Actions map[string]string // key -> label
}

// dunstctl speaks busctl JSON: every D-Bus variant arrives wrapped as
// {"type": "<sig>", "data": <value>}. unwrap that; leave anything else
// (including a plain value, if dunst ever changes shape) alone.
func unwrap(raw json.RawMessage) json.RawMessage {
	var v struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	if len(v.Data) == 0 {
		return raw
	}
	return v.Data
}

func asString(raw json.RawMessage) string {
	raw = unwrap(raw)
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func asInt(raw json.RawMessage) int64 {
	raw = unwrap(raw)
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0
	}
	return int64(f)
}

// fetchHistory runs `dunstctl history` and parses the entries.
func fetchHistory(ctx context.Context) ([]notif, error) {
	out, err := exec.CommandContext(ctx, "dunstctl", "history").Output()
	if err != nil {
		return nil, err
	}
	var root json.RawMessage = out
	data := unwrap(root)
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) == 0 {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(unwrap(arr[0]), &entries); err != nil {
		return nil, nil
	}
	notifs := make([]notif, 0, len(entries))
	for _, e := range entries {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(e, &m); err != nil {
			continue
		}
		n := notif{
			ID:      asInt(m["id"]),
			App:     asString(m["appname"]),
			Summary: strings.Join(strings.Fields(asString(m["summary"])), " "),
			Body:    strings.Join(strings.Fields(asString(m["body"])), " "),
			Actions: map[string]string{},
		}
		if n.App == "" {
			n.App = "?"
		}
		var acts map[string]json.RawMessage
		if err := json.Unmarshal(unwrap(m["actions"]), &acts); err == nil {
			for k, v := range acts {
				n.Actions[k] = asString(v)
			}
		}
		notifs = append(notifs, n)
	}
	return notifs, nil
}

func dunstAction(ctx context.Context, id int64, key string) error {
	return exec.CommandContext(ctx, "dunstctl", "action", strconv.FormatInt(id, 10), key).Run()
}

func dunstDismiss(ctx context.Context, id int64) error {
	return exec.CommandContext(ctx, "dunstctl", "history-rm", strconv.FormatInt(id, 10)).Run()
}

func dunstClear(ctx context.Context) error {
	return exec.CommandContext(ctx, "dunstctl", "history-clear").Run()
}

func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// ---------------------------------------------------------------------------
// word wrap (rune-aware, ANSI-free — call on plain text only)
// ---------------------------------------------------------------------------

func wrapText(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		var cur []rune
		curW := 0
		flush := func() {
			if len(cur) > 0 {
				lines = append(lines, string(cur))
				cur = nil
				curW = 0
			}
		}
		for _, word := range strings.Fields(para) {
			wr := []rune(word)
			if curW+len(wr)+1 > width && curW > 0 {
				flush()
			}
			if curW > 0 {
				cur = append(cur, ' ')
				curW++
			}
			cur = append(cur, wr...)
			curW += len(wr)
		}
		flush()
		if strings.TrimSpace(para) == "" {
			lines = append(lines, "")
		}
	}
	return lines
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type historyMsg struct {
	notifs []notif
	err    error
}

type clearTickMsg struct{}

type clearDoneMsg struct{ err error }

type dismissTickMsg struct{}

type dismissDoneMsg struct{ err error }

type statusMsg struct{ text string }

type model struct {
	notifs []notif
	cursor int

	vp         viewport.Model
	itemLines  []int // first content line of each notification block
	itemHeight []int // line count of each notification block
	totalLines int

	width  int
	height int
	ready  bool

	status   string
	loadErr  error
	loading  bool
	clearing bool
	clearN   int // glitch animation frame

	dismissing bool
	dismissN   int // single-dismiss glitch frame
	dismissIdx int // which notification is glitching away

	tx theme.Transmission
}

func initialModel() model {
	return model{
		width:   frameWidth,
		height:  24,
		loading: true,
		tx: theme.Transmission{
			Visible: true,
			Clean:   "present day, present time",
			Text:    "present day, present time",
		},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := opCtx()
			defer cancel()
			n, err := fetchHistory(ctx)
			return historyMsg{notifs: n, err: err}
		},
		m.tx.Init(),
	)
}

// viewport geometry: inner width after the frame border/padding, minus the
// scrollbar column.
func (m model) innerWidth() int {
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	return w
}

func (m model) vpWidth() int { return m.innerWidth() - 2 }

func (m model) vpHeight() int {
	h := m.height - 12 // frame chrome: border, header, tx, pinned, divider, footer
	if h < 5 {
		h = 5
	}
	if h > 22 {
		h = 22
	}
	return h
}

// renderBlock renders one notification as styled lines (no trailing newline).
// Selection is marked with a pink index and a cursor chevron.
func (m model) renderBlock(i int, n notif, selected bool) string {
	w := m.vpWidth()
	idx := "[" + strconv.Itoa(i) + "]"
	var head string
	if selected {
		head = theme.Selected.Render(idx) + " " + theme.Selected.Render(n.App)
	} else {
		head = theme.Dimmed.Render(idx) + " " + theme.Grayed.Render(n.App)
	}
	lines := []string{head}
	if n.Summary != "" {
		// wrap the plain text, then style each line (never style twice)
		for _, l := range wrapText(n.Summary, w) {
			if selected {
				lines = append(lines, lipgloss.NewStyle().Foreground(theme.White).Bold(true).Render(l))
			} else {
				lines = append(lines, theme.Normal.Render(l))
			}
		}
	}
	if n.Body != "" {
		for _, l := range wrapText(n.Body, w) {
			lines = append(lines, theme.Grayed.Render(l))
		}
	}
	if len(n.Actions) > 0 {
		labels := make([]string, 0, len(n.Actions))
		// deterministic order: sort keys
		keys := make([]string, 0, len(n.Actions))
		for k := range n.Actions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			labels = append(labels, n.Actions[k])
		}
		act := "> " + strings.Join(labels, ", ")
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Green).Render(act))
	}
	marker := "  "
	if selected {
		marker = theme.Selected.Render("❯ ")
	}
	for j := range lines {
		if j == 0 {
			lines[j] = marker + lines[j]
		} else {
			lines[j] = "  " + lines[j]
		}
	}
	return strings.Join(lines, "\n")
}

// plainBlock renders one notification as plain text (for the glitch pass).
func (m model) plainBlock(i int, n notif) string {
	w := m.vpWidth()
	lines := []string{"[" + strconv.Itoa(i) + "] " + n.App}
	lines = append(lines, wrapText(n.Summary, w)...)
	lines = append(lines, wrapText(n.Body, w)...)
	if len(n.Actions) > 0 {
		labels := make([]string, 0, len(n.Actions))
		for _, v := range n.Actions {
			labels = append(labels, v)
		}
		lines = append(lines, "> "+strings.Join(labels, ", "))
	}
	return strings.Join(lines, "\n")
}

// rebuildContent re-renders every block into the viewport and recomputes
// line offsets for cursor tracking.
func (m *model) rebuildContent() {
	m.itemLines = make([]int, len(m.notifs))
	m.itemHeight = make([]int, len(m.notifs))
	var blocks []string
	line := 0
	for i, n := range m.notifs {
		b := m.renderBlock(i, n, i == m.cursor)
		blocks = append(blocks, b)
		h := strings.Count(b, "\n") + 1
		m.itemLines[i] = line
		m.itemHeight[i] = h
		line += h + 1 // blank separator line
	}
	content := strings.Join(blocks, "\n\n")
	m.vp.SetContent(content)
	m.totalLines = strings.Count(content, "\n") + 1
	if len(m.notifs) == 0 {
		m.totalLines = 0
	}
	m.ensureVisible()
}

// ensureVisible scrolls the viewport just enough to keep the cursor's block
// on screen.
func (m *model) ensureVisible() {
	if len(m.notifs) == 0 {
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.notifs) {
		m.cursor = len(m.notifs) - 1
	}
	start := m.itemLines[m.cursor]
	end := start + m.itemHeight[m.cursor]
	if start < m.vp.YOffset {
		m.vp.SetYOffset(start)
	} else if end > m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(end - m.vp.Height)
	}
}

func (m *model) moveCursor(d int) {
	if len(m.notifs) == 0 {
		return
	}
	m.cursor += d
	// wraparound navigation
	if m.cursor < 0 {
		m.cursor = len(m.notifs) - 1
		m.rebuildContent()
		m.vp.GotoBottom()
		return
	}
	if m.cursor >= len(m.notifs) {
		m.cursor = 0
		m.rebuildContent()
		m.vp.GotoTop()
		return
	}
	m.rebuildContent()
}

func (m *model) refresh() tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		n, err := fetchHistory(ctx)
		return historyMsg{notifs: n, err: err}
	}
}

// ---------------------------------------------------------------------------
// glitch clear-all
// ---------------------------------------------------------------------------

const clearFrames = 7

func clearTickCmd() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return clearTickMsg{} })
}

// glitchView renders the list mid-clear: rows scramble into glitch glyphs
// with pink/cyan flicker and slice offsets, then collapse away.
func (m model) glitchView() string {
	w := m.vpWidth()
	var lines []string
	for i, n := range m.notifs {
		lines = append(lines, strings.Split(m.plainBlock(i, n), "\n")...)
		lines = append(lines, "")
	}
	intensity := 0.25 + 0.65*float64(m.clearN)/float64(clearFrames)
	flicker := lipgloss.NewStyle().Foreground(theme.Pink)
	if m.clearN%2 == 1 {
		flicker = lipgloss.NewStyle().Foreground(theme.Cyan)
	}
	var out []string
	for li, l := range lines {
		g := theme.Glitch(l, intensity)
		// slice offset: shove a few rows sideways for the tear effect
		if (li*7+m.clearN*3)%11 < 3 && strings.TrimSpace(g) != "" {
			g = strings.Repeat(" ", 2+(li+m.clearN)%5) + g
			if len([]rune(g)) > w {
				g = string([]rune(g)[:w])
			}
		}
		out = append(out, flicker.Render(g))
	}
	// collapse: drop rows from the bottom as the animation finishes
	keep := len(out)
	if m.clearN >= clearFrames-2 {
		keep = len(out) * (clearFrames - m.clearN) / 3
		if keep < 0 {
			keep = 0
		}
	}
	if keep > len(out) {
		keep = len(out)
	}
	out = out[:keep]
	for len(out) < m.vp.Height {
		out = append(out, "")
	}
	if len(out) > m.vp.Height {
		out = out[:m.vp.Height]
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------------------
// glitch single-dismiss
// ---------------------------------------------------------------------------

// dismissFrames is shorter than the clear-all: one row glitching away
// should feel snappy, not ceremonial.
const dismissFrames = 5

func dismissTickCmd() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return dismissTickMsg{} })
}

// dismissGlitchView renders the list mid-dismiss: every block renders
// plain except the target, which scrambles into glitch glyphs with
// pink/cyan flicker and then crumples away.
func (m model) dismissGlitchView() string {
	w := m.vpWidth()
	intensity := 0.3 + 0.6*float64(m.dismissN)/float64(dismissFrames)
	flicker := lipgloss.NewStyle().Foreground(theme.Pink)
	if m.dismissN%2 == 1 {
		flicker = lipgloss.NewStyle().Foreground(theme.Cyan)
	}
	var out []string
	for i, n := range m.notifs {
		lines := strings.Split(m.plainBlock(i, n), "\n")
		if i == m.dismissIdx {
			var g []string
			for li, l := range lines {
				gl := theme.Glitch(l, intensity)
				// slice offset: shove a few rows sideways for the tear effect
				if (li*7+m.dismissN*3)%11 < 3 && strings.TrimSpace(gl) != "" {
					gl = strings.Repeat(" ", 2+(li+m.dismissN)%5) + gl
					if len([]rune(gl)) > w {
						gl = string([]rune(gl)[:w])
					}
				}
				g = append(g, flicker.Render(gl))
			}
			// crumple: the row collapses as the animation finishes
			if m.dismissN >= dismissFrames-2 {
				keep := len(g) * (dismissFrames - m.dismissN) / 3
				if keep < 0 {
					keep = 0
				}
				g = g[:keep]
			}
			lines = g
		}
		out = append(out, lines...)
		out = append(out, "")
	}
	for len(out) < m.vp.Height {
		out = append(out, "")
	}
	if len(out) > m.vp.Height {
		out = out[:m.vp.Height]
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width > frameWidth {
			m.width = frameWidth
		}
		if m.width < 30 {
			m.width = 30
		}
		m.height = msg.Height
		if !m.ready {
			m.vp = viewport.New(m.vpWidth(), m.vpHeight())
			m.ready = true
			m.rebuildContent()
		} else {
			m.vp.Width = m.vpWidth()
			m.vp.Height = m.vpHeight()
			m.rebuildContent()
		}
		return m, nil

	case historyMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			m.status = "couldn't read dunst history"
			return m, nil
		}
		m.loadErr = nil
		m.notifs = msg.notifs
		if m.cursor >= len(m.notifs) {
			m.cursor = 0
		}
		m.rebuildContent()
		return m, nil

	case clearTickMsg:
		if !m.clearing {
			return m, nil
		}
		m.clearN++
		if m.clearN >= clearFrames {
			m.clearing = false
			m.clearN = 0
			m.notifs = nil
			m.cursor = 0
			m.status = "cleared"
			m.rebuildContent()
			return m, nil
		}
		return m, clearTickCmd()

	case clearDoneMsg:
		if msg.err != nil {
			m.clearing = false
			m.clearN = 0
			m.status = "clear failed"
			m.rebuildContent()
		}
		return m, nil

	case dismissTickMsg:
		if !m.dismissing {
			return m, nil
		}
		m.dismissN++
		if m.dismissN >= dismissFrames {
			m.dismissing = false
			m.dismissN = 0
			if m.dismissIdx >= 0 && m.dismissIdx < len(m.notifs) {
				m.notifs = append(m.notifs[:m.dismissIdx], m.notifs[m.dismissIdx+1:]...)
			}
			if m.cursor >= len(m.notifs) {
				m.cursor = len(m.notifs) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.status = "dismissed"
			m.rebuildContent()
			return m, nil
		}
		return m, dismissTickCmd()

	case dismissDoneMsg:
		if msg.err != nil {
			if m.dismissing {
				// animation still running: cancel it, the row comes back
				m.dismissing = false
				m.dismissN = 0
				m.status = "dismiss failed"
				m.rebuildContent()
			} else {
				// the row already glitched away: resync with dunst
				m.status = "dismiss failed"
				return m, m.refresh()
			}
		}
		return m, nil

	case statusMsg:
		m.status = msg.text
		return m, nil

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.clearing || m.dismissing {
			// let the glitch play out undisturbed
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			m.moveCursor(-1)
			return m, nil
		case "down", "j":
			m.moveCursor(1)
			return m, nil
		case "home", "g":
			if len(m.notifs) > 0 {
				m.cursor = 0
				m.rebuildContent()
				m.vp.GotoTop()
			}
			return m, nil
		case "end", "G":
			if len(m.notifs) > 0 {
				m.cursor = len(m.notifs) - 1
				m.rebuildContent()
				m.vp.GotoBottom()
			}
			return m, nil
		case "r":
			m.status = "refreshing…"
			return m, m.refresh()
		case "enter":
			return m, m.invokeAction()
		case "d", "x":
			return m, m.dismissSelected()
		case "c":
			return m, m.clearAll()
		}
	}

	// route to the viewport for any other scrolling input (mouse wheel etc.)
	if m.ready && !m.clearing && !m.dismissing {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) invokeAction() tea.Cmd {
	if len(m.notifs) == 0 || m.cursor >= len(m.notifs) {
		m.status = "nothing selected"
		return nil
	}
	n := m.notifs[m.cursor]
	if len(n.Actions) == 0 {
		m.status = "that notification has no actions"
		return nil
	}
	// default action: smallest key (dunst uses "default" first)
	keys := make([]string, 0, len(n.Actions))
	for k := range n.Actions {
		keys = append(keys, k)
	}
	best := keys[0]
	for _, k := range keys[1:] {
		if k < best {
			best = k
		}
	}
	id := n.ID
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		if err := dunstAction(ctx, id, best); err != nil {
			return statusMsg{text: "action failed — the notification may be gone"}
		}
		return m.refresh()()
	}
}

func (m *model) dismissSelected() tea.Cmd {
	if len(m.notifs) == 0 || m.cursor >= len(m.notifs) || m.dismissing || m.clearing {
		if len(m.notifs) == 0 || m.cursor >= len(m.notifs) {
			m.status = "nothing selected"
		}
		return nil
	}
	id := m.notifs[m.cursor].ID
	m.dismissing = true
	m.dismissN = 0
	m.dismissIdx = m.cursor
	m.status = ""
	return tea.Batch(
		dismissTickCmd(),
		func() tea.Msg {
			ctx, cancel := opCtx()
			defer cancel()
			if err := dunstDismiss(ctx, id); err != nil {
				return dismissDoneMsg{err: err}
			}
			return dismissDoneMsg{}
		},
	)
}

func (m *model) clearAll() tea.Cmd {
	if len(m.notifs) == 0 || m.clearing {
		return nil
	}
	m.clearing = true
	m.clearN = 0
	m.status = ""
	return tea.Batch(
		clearTickCmd(),
		func() tea.Msg {
			ctx, cancel := opCtx()
			defer cancel()
			if err := dunstClear(ctx); err != nil {
				return clearDoneMsg{err: err}
			}
			return clearDoneMsg{}
		},
	)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// scrollBar is the thin position indicator: a dim track with a pink thumb
// marking the viewport's position in the full list.
func (m model) scrollBar() string {
	h := m.vp.Height
	if h < 1 {
		return ""
	}
	thumb := 0
	if m.totalLines > h {
		frac := float64(m.vp.YOffset) / float64(m.totalLines-h)
		thumb = int(frac*float64(h-1) + 0.5)
	}
	var b strings.Builder
	for i := 0; i < h; i++ {
		if m.totalLines <= h {
			b.WriteString(theme.Dimmed.Render("│"))
		} else if i == thumb {
			b.WriteString(theme.Selected.Render("█"))
		} else {
			b.WriteString(theme.Dimmed.Render("│"))
		}
		if i < h-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// pinnedHeader is the identity row that never scrolls away: the ∅ mark plus
// the notification count.
func (m model) pinnedHeader() string {
	n := len(m.notifs)
	count := "no notifications"
	if n == 1 {
		count = "1 notification"
	} else if n > 1 {
		count = strconv.Itoa(n) + " notifications"
	}
	return theme.Logo.Render("∅") + " " + theme.Title.Render("notifications") +
		" " + theme.Dimmed.Render("· "+count)
}

func (m model) listView() string {
	if m.clearing {
		body := m.glitchView()
		return lipgloss.JoinHorizontal(lipgloss.Top, body, " "+m.scrollBar())
	}
	if m.dismissing {
		body := m.dismissGlitchView()
		return lipgloss.JoinHorizontal(lipgloss.Top, body, " "+m.scrollBar())
	}
	if m.loadErr != nil {
		return theme.Error.Render("couldn't read dunst history — is dunst running?")
	}
	if len(m.notifs) == 0 && !m.loading {
		empty := theme.Dimmed.Render("  all quiet — no notifications in history.")
		pad := m.vp.Height - 1
		if pad < 0 {
			pad = 0
		}
		return empty + strings.Repeat("\n", pad)
	}
	if len(m.notifs) == 0 {
		return theme.Dimmed.Render("  listening…") + strings.Repeat("\n", m.vp.Height-1)
	}
	body := m.vp.View()
	return lipgloss.JoinHorizontal(lipgloss.Top, body, " "+m.scrollBar())
}

func (m model) View() string {
	if !m.ready {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.pinnedHeader())
	b.WriteString("\n")
	b.WriteString(theme.Divider(m.innerWidth()))
	b.WriteString("\n")
	b.WriteString(m.listView())
	if m.status != "" {
		b.WriteString("\n")
		b.WriteString(theme.Grayed.Render("  " + m.status))
	}
	footer := theme.Footer(true,
		[2]string{"↑↓/jk", "move"},
		[2]string{"enter", "action"},
		[2]string{"d", "dismiss"},
		[2]string{"c", "clear all"},
	) + "\n" + theme.Footer(true,
		[2]string{"r", "refresh"},
		[2]string{"q", "quit"},
	)
	body := b.String() + "\n" + footer
	return theme.Frame(m.width, "navi notifs", len(m.notifs) > 0, m.tx.View(m.width), body)
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: navi-notifs")
			fmt.Fprintln(os.Stderr, "  browse dunst notification history, invoke actions, dismiss, clear.")
			os.Exit(0)
		default:
			fmt.Fprintln(os.Stderr, "usage: navi-notifs")
			os.Exit(2)
		}
	}
	if _, err := exec.LookPath("dunstctl"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-notifs: dunstctl was not found.")
		fmt.Fprintln(os.Stderr, "Is dunst running?")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-notifs: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(50 * time.Millisecond)
}
