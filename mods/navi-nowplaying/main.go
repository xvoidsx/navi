// navi-nowplaying — what the wired is listening to, and the keys to
// change it.
//
// A media mod for navi: cover art, metadata, a live position meter, and
// transport. Everything runs through playerctl (MPRIS), so it works with
// whatever is actually playing — Chromium, cmus, mpv, spotify — rather
// than owning a player of its own.
//
// Scope is transport only: play/pause, next, back, seek, and quit. Volume,
// shuffle, and loop are deliberately absent, because navi already owns
// them — `navi-audio` is a full PulseAudio widget, and the bar carries a
// volume module. A second volume control in a media panel is not a
// feature, it is two places to look and a second place to be wrong. The
// same goes for shuffle and loop, which are properties of how someone is
// listening rather than of what is playing. A media panel that only
// answers "what is this, and how do I skip it" has one job and does it
// well.
//
// The interesting constraint is that MPRIS players are *partial*. A
// player can serve position and metadata while having no next, no
// previous, and no seek (Chromium on trixie answers only some of them).
// So every control here is capability-gated: a key the player can't
// service is drawn faint instead of vanishing, because a footer that
// rearranges itself under your fingers is worse than a footer with a
// dead key.
//
// The clock is two-speed. A fast tick (200ms) only advances the position
// meter from a local anchor and costs zero subprocesses; a slow poll
// (1.5s) does the real playerctl reads. That's the difference between a
// smooth meter and the old bash loop that shelled out three times a
// second, forever.
//
// Styled entirely through the shared nightshadeNeon theme package
// (mods/theme).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const (
	// frameWidth is the family standard, 62. Read it as the *content*
	// width, which is what theme.Frame's width parameter actually is:
	// Border carries Padding(0, 1) on top of the border itself, so a
	// Frame(62) is 66 cells on screen (62 + 2 padding + 2 border).
	frameWidth = 62

	// innerWidth is how much of that content a body line may occupy.
	// The border and its padding take 4 cells, leaving 58.
	innerWidth = frameWidth - 4

	artCols   = 22
	artRows   = 8
	metaWidth = innerWidth - artCols - 2

	// labelWidth is the right-hand column holding the time pair, so its
	// numbers sit at a fixed distance from the meter's right edge. It has
	// to hold the longest label fmtTime can produce — an hours field on
	// both sides, "10:00:00 / 10:00:00" — or the row wraps and tears the
	// frame's height. 19 covers every case below 100 hours.
	labelWidth = 19
	meterWidth = innerWidth - 2 - 2 - labelWidth

	// bodyHeight is every view's exact body height, in lines. The frame
	// adds four chrome lines (top border, title, transmission row, bottom
	// border), so a 15-line body renders a 19-line window.
	//
	// This is the constant that matters most. A floating mod is opened
	// once and left open; if the window resizes itself every time a track
	// starts, stops, or errors, the compositor fights the app and the mod
	// visibly jumps. So every view pads to exactly this, and
	// TestFrameHeightIsConstant fails if one drifts.
	bodyHeight = 15

	seekStep = 5 * time.Second

	fastTickEvery = 200 * time.Millisecond
	slowTickEvery = 1500 * time.Millisecond
)

// ---------------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------------

type stateMsg struct {
	st State
}

type artMsg struct {
	key   string
	block artBlock
}

type controlMsg struct {
	verb string
	err  error
}

type fastTickMsg time.Time
type pollMsg time.Time

// ---------------------------------------------------------------------------
// commands
// ---------------------------------------------------------------------------

// pollCmd does every playerctl read in one goroutine, off the render path.
//
// One goroutine, one message. The read is three playerctl calls —
// metadata, status, position — which is the floor for a mod that has to
// know what is playing without owning the player.
func pollCmd(player string) tea.Cmd {
	return func() tea.Msg { return stateMsg{st: Read(player)} }
}

// artCmd decodes cover art in a goroutine. Decoding a photo is tens of
// milliseconds; on the render path that is a visible stutter.
func artCmd(key, artURL string, cols, rows int) tea.Cmd {
	return func() tea.Msg { return artMsg{key: key, block: LoadArt(artURL, cols, rows)} }
}

// controlCmd runs a playerctl verb and reports the result. Commands are
// followed by an immediate re-poll so the UI never shows a value the
// player has already changed underneath it.
//
// The verb travels with the result because a capability error is itself
// information: "No player could handle this command" means the key is
// about to become permanently faint. See the controlMsg handler.
func controlCmd(player, verb string, value ...string) tea.Cmd {
	return func() tea.Msg { return controlMsg{verb: verb, err: Do(player, verb, value...)} }
}

func fastTick() tea.Cmd {
	return tea.Tick(fastTickEvery, func(t time.Time) tea.Msg { return fastTickMsg(t) })
}

func slowTick() tea.Cmd {
	return tea.Tick(slowTickEvery, func(t time.Time) tea.Msg { return pollMsg(t) })
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type model struct {
	tx     theme.Transmission
	width  int
	height int

	players []string
	player  string

	st State

	// position interpolation: posBase was true at posAnchor; the fast
	// tick adds the elapsed time between them, so the meter moves
	// smoothly without a D-Bus round trip per frame.
	posBase   time.Duration
	posAnchor time.Time

	artKey  string
	art     artBlock
	artBusy bool

	err  string
	note string
}

func newModel() model {
	return model{
		art:       placeholder(artCols, artRows),
		posAnchor: time.Now(),
	}
}

// pos returns the interpolated playback position.
func (m model) pos() time.Duration {
	if !m.st.IsPlaying() {
		return m.posBase
	}
	d := time.Since(m.posAnchor)
	if d < 0 {
		return m.posBase
	}
	p := m.posBase + d
	// Don't run the meter past the end of the track between polls.
	if m.st.Track.Length > 0 && p > m.st.Track.Length {
		return m.st.Track.Length
	}
	return p
}

func (m model) hasTrack() bool { return m.st.Track.Title != "" || m.st.Track.Artist != "" }

func (m *model) setNote(s string) { m.note, m.err = s, "" }
func (m *model) setErr(s string)  { m.err, m.note = s, "" }

func (m *model) playersCmd() tea.Cmd {
	return func() tea.Msg { return playersMsg(Players()) }
}

type playersMsg []string

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tx.Init(), fastTick(), slowTick(), m.playersCmd()}
	if len(m.players) > 0 {
		cmds = append(cmds, pollCmd(m.player))
	}
	return tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The ambient Lain ticker is independent of everything else here;
	// delegate all four message types and never cancel it.
	switch msg.(type) {
	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case playersMsg:
		m.players = msg
		if m.player == "" && len(m.players) > 0 {
			m.player = m.players[0]
			return m, pollCmd(m.player)
		}
		return m, nil

	case stateMsg:
		return m.applyState(msg)

	case artMsg:
		// Drop a stale render that arrived after the track changed.
		if msg.key == m.artKey {
			m.art, m.artBusy = msg.block, false
		}
		return m, nil

	case controlMsg:
		if msg.err != nil {
			if unsupported(msg.err) {
				// Self-healing capability discovery. Some things
				// simply cannot be asked in advance — MPRIS exposes
				// CanGoNext as a property, but playerctl has no verb
				// for it. So the first press that fails teaches the
				// mod the key is dead, and from then on it renders
				// faint. One wasted round trip, once per session.
				if m.st.Unavailable == nil {
					m.st.Unavailable = map[string]bool{}
				}
				m.st.Unavailable[msg.verb] = true
				m.setNote("this player can't " + humanVerb(msg.verb))
			} else {
				m.setErr(msg.err.Error())
			}
		}
		return m, pollCmd(m.player)

	case fastTickMsg:
		return m, fastTick()

	case pollMsg:
		return m, tea.Batch(slowTick(), pollCmd(m.player))

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m model) applyState(msg stateMsg) (tea.Model, tea.Cmd) {
	prev := m.st
	m.st = msg.st
	m.posBase, m.posAnchor = msg.st.Position, time.Now()

	if m.st.Player == "" {
		m.st.Player = m.player
	}
	if m.st.Err != "" {
		m.setErr(m.st.Err)
	}

	// A new track means a new cover: drop the memo and re-render.
	if m.st.Track.ArtURL != prev.Track.ArtURL || m.st.Track.TrackID != prev.Track.TrackID {
		key := artKey(m.st.Track.ArtURL, artCols, artRows)
		if key != m.artKey {
			m.artKey, m.art = key, placeholder(artCols, artRows)
			if m.st.Track.HasArt() && !m.artBusy {
				m.artBusy = true
				return m, artCmd(key, m.st.Track.ArtURL, artCols, artRows)
			}
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// keys
// ---------------------------------------------------------------------------

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	}

	switch key {
	case " ", "p":
		return m, controlCmd(m.player, "play-pause")
	case "n":
		// No upfront capability probe exists for next/previous —
		// playerctl can't ask. The control path learns the answer and
		// dims the key permanently, so this stays one round trip.
		return m, controlCmd(m.player, "next")
	case "b":
		return m, controlCmd(m.player, "previous")
	case "left":
		return m.seek(-seekStep)
	case "right":
		return m.seek(seekStep)
	case "tab":
		return m.cyclePlayer()
	}
	return m, nil
}

func (m model) seek(d time.Duration) (tea.Model, tea.Cmd) {
	if !m.st.Has(capSeek) {
		return m, m.dim("this player can't seek")
	}
	target := m.pos() + d
	if m.st.Track.Length > 0 {
		target = clampDur(target, 0, m.st.Track.Length)
	}
	m.posBase, m.posAnchor = target, time.Now()
	return m, controlCmd(m.player, "position", fmt.Sprintf("%.3f", target.Seconds()))
}

// cyclePlayer moves to the next MPRIS player on the bus.
//
// The whole reason this exists: on a real navi desk there is usually more
// than one thing that can be controlled. A browser tab playing a video and
// a local player running an album both expose MPRIS, playerctl picks
// whichever it finds first, and there is no other way to reach the second
// one. Without this the mod would be permanently bound to whatever won
// that race — which is how a user ends up convinced it only sees
// Chromium.
//
// The name is announced rather than left for the user to infer, because
// the key is faint whenever there is nothing to switch to, and a faint
// unexplained key reads as a bug.
func (m model) cyclePlayer() (tea.Model, tea.Cmd) {
	if len(m.players) < 2 {
		return m, m.dim("only " + shortPlayer(m.player) + " on the bus")
	}
	i := 0
	for j, p := range m.players {
		if p == m.player {
			i = j
			break
		}
	}
	m.player = m.players[(i+1)%len(m.players)]
	m.st = State{Unavailable: map[string]bool{}}
	m.artKey, m.art = "", placeholder(artCols, artRows)
	m.setNote("now controlling " + shortPlayer(m.player))
	return m, pollCmd(m.player)
}

// dim reports a soft refusal in the note line — not an error, because
// nothing is actually broken.
func (m model) dim(note string) tea.Cmd {
	m.setNote(note)
	return nil
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func (m model) View() string {
	frame := m.nowView()
	live := m.hasTrack() && m.st.IsPlaying()
	f := theme.Frame(frameWidth, "navi now playing", live, m.tx.View(frameWidth), frame)
	if m.width == 0 {
		return f
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, f)
}

// padBody forces a view body to exactly bodyHeight lines, padding with
// blanks below. A body that is already too long is returned untouched so
// the height test reports the real overflow instead of hiding it.
func padBody(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= bodyHeight {
		return s
	}
	return s + strings.Repeat("\n", bodyHeight-len(lines))
}

// padBodyCentered is padBody for a view that has nothing to fill the
// window with. The idle state has no art, no metadata, and no meters, so
// padding only at the bottom leaves a tall empty box with a caption
// pinned to its ceiling. Splitting the slack puts the message where the
// eye already is.
func padBodyCentered(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= bodyHeight {
		return s
	}
	slack := bodyHeight - len(lines)
	top := slack / 2
	return strings.Repeat("\n", top) + s + strings.Repeat("\n", slack-top)
}

func (m model) nowView() string {
	var b strings.Builder

	// The idle view is for "there is nothing to show" — no player on the
	// bus, or a player with no track. A player that merely paused keeps
	// its full view, because you still want to see what's loaded.
	if !m.hasTrack() && m.st.Player == "" && len(m.players) == 0 {
		return m.idleView()
	}

	b.WriteString(m.headerBlock())
	b.WriteString("\n\n")
	b.WriteString(m.progressRow())
	b.WriteString("\n\n")
	b.WriteString(theme.Divider(frameWidth))
	b.WriteString("\n")
	b.WriteString(m.footer())
	return padBody(b.String())
}

// idleView is the "nothing is playing" state. It breathes rather than
// sitting dead — the same idle motion as the rest of the wired.
func (m model) idleView() string {
	var b strings.Builder
	b.WriteString(theme.Glow("  ♪", time.Now()))
	b.WriteString("\n\n")
	b.WriteString("  " + theme.Grayed.Render("the wired is quiet."))
	b.WriteString("\n")
	b.WriteString("  " + theme.Fainted.Render("nothing is playing right now."))
	b.WriteString("\n\n")
	b.WriteString(theme.Divider(frameWidth))
	b.WriteString("\n")
	b.WriteString(m.footer())
	return padBodyCentered(b.String())
}

// headerBlock is the art beside the metadata, joined as one row.
func (m model) headerBlock() string {
	art := lipgloss.NewStyle().Width(artCols).Render(m.art.rendered)

	var meta strings.Builder
	title := m.st.Track.Title
	if title == "" {
		title = theme.Glow("♪  unknown track", time.Now())
	} else {
		title = theme.Normal.Render(title)
	}
	meta.WriteString(lipgloss.NewStyle().Width(metaWidth).MaxHeight(2).Render(title))
	meta.WriteString("\n")
	meta.WriteString(lipgloss.NewStyle().Width(metaWidth).MaxHeight(1).
		Render(theme.Grayed.Render(orDash(m.st.Track.Artist))))
	meta.WriteString("\n")
	meta.WriteString(lipgloss.NewStyle().Width(metaWidth).MaxHeight(1).
		Render(theme.Dimmed.Render(orDash(m.st.Track.Album))))
	meta.WriteString("\n\n")
	meta.WriteString(lipgloss.NewStyle().Width(metaWidth).MaxHeight(1).
		Render(m.playerChip()))

	return lipgloss.JoinHorizontal(lipgloss.Top, art, "  ", meta.String())
}

// playerChip is the player name and transport state, sharing one line.
func (m model) playerChip() string {
	name := shortPlayer(m.st.Player)
	if name == "" {
		return ""
	}
	if m.st.Status == "" {
		return theme.Header.Render(name)
	}
	return theme.Header.Render(name) + " " + statusChip(m.st.Status)
}

func (m model) progressRow() string {
	frac := 0.0
	if m.st.Track.Length > 0 {
		frac = m.pos().Seconds() / m.st.Track.Length.Seconds()
	}
	fill, empty := theme.Green, theme.Faint
	if m.st.IsPlaying() {
		fill = theme.Pink // the meter is pink while it runs, green when parked
	}
	meter := theme.Meter(meterWidth, frac, fill, empty)
	return "  " + meter + "  " + labelCell(fmt.Sprintf("%s / %s",
		fmtTime(m.pos()), fmtTime(m.st.Track.Length)))
}

// labelCell renders a right-hand value in the shared label column. It
// truncates rather than wraps: a number that silently becomes two lines
// changes the frame's height, and a fixed frame is the whole point of a
// fixed-width design. MaxWidth is belt-and-braces against a duration
// format that grows again in some future player.
func labelCell(s string) string {
	return lipgloss.NewStyle().
		Width(labelWidth).
		MaxWidth(labelWidth).
		Align(lipgloss.Right).
		Render(theme.Normal.Render(s))
}

func statusChip(s string) string {
	switch s {
	case "Playing":
		return theme.Selected.Render("▶ playing")
	case "Paused":
		return theme.Grayed.Render("▮▮ paused")
	default:
		return theme.Dimmed.Render("· " + strings.ToLower(s))
	}
}

// shortPlayer trims the noisy ".instanceNNNN" suffix MPRIS players carry.
func shortPlayer(p string) string {
	if i := strings.Index(p, ".instance"); i > 0 {
		return p[:i]
	}
	return p
}

// footer is always exactly two key lines plus the reserved message line.
//
// Fixed, not "as many as fit": a footer that grows when a second player
// appears would shift under the user's fingers, which is the one thing
// these bars must never do. Six keys is the whole vocabulary now, and two
// short lines keep every one of them legible instead of abbreviating to
// fit. Three-and-three so neither line carries the weight.
func (m model) footer() string {
	line1 := theme.Footer(true,
		[2]string{"space", "play"},
		[2]string{"←→", "seek"},
	) + "   " + theme.Footer(m.st.Has(capNext), [2]string{"n", "next"})

	// "tab switch" rather than "tab player": with a single player this key
	// is faint and has nothing to do, and a bare noun gives a first-time
	// user no reason to press it. A verb describes the action; the note
	// line below then explains why it did nothing.
	line2 := theme.Footer(m.st.Has(capPrev), [2]string{"b", "back"}) + "   " +
		theme.Footer(len(m.players) > 1, [2]string{"tab", "switch"}) + "   " +
		theme.Footer(true, [2]string{"q", "quit"})

	// The message line is reserved whether or not there is a message.
	// Appending one only when something has to be said would grow the
	// frame the instant a player hiccuped, which is the least welcome
	// moment to resize a floating window.
	msg := ""
	switch {
	case m.err != "":
		msg = theme.Error.Render("× " + m.err)
	case m.note != "":
		msg = theme.Fainted.Render(m.note)
	}
	return line1 + "\n" + line2 + "\n" + msg
}

// humanVerb turns a playerctl verb into something a person can read in
// the note line.
func humanVerb(v string) string {
	switch v {
	case "play-pause":
		return "play"
	case "previous":
		return "go back"
	case "position":
		return "seek"
	default:
		return v
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func fmtTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second) / time.Second)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func clampDur(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dump := flag.Bool("dump", false, "print sample screens and exit")
	dumpLive := flag.Bool("dump-live", false, "print the live player's current screen and exit")
	probe := flag.Bool("probe", false, "read the live player once, print what was parsed, and exit")
	flag.Parse()

	// Same contract as navi-networking: if the backend isn't on PATH, say
	// so once and exit rather than opening an empty window. --dump is
	// exempt: it's deterministic sample data for headless layout review,
	// and must work in build environments without a player bus.
	if *dump {
		dumpSample()
		return
	}

	if !havePlayer() {
		fmt.Fprintln(os.Stderr, "navi-nowplaying: playerctl not found — install playerctl (apt install playerctl)")
		os.Exit(1)
	}

	if *probe {
		probeLive()
		return
	}

	if *dumpLive {
		dumpLiveFrame()
		return
	}

	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-nowplaying:", err)
		os.Exit(1)
	}
	// let the terminal restore cleanly, same as the other mods
	time.Sleep(50 * time.Millisecond)
}

// probeLive reads the real bus once and reports what the parser made of
// it. MPRIS players are partial and inconsistent, so when a track, an art
// URL, or a toggle looks wrong on screen, this is how you find out whether
// the mod is wrong or the player is — with no terminal, no compositor, and
// no guesswork. Unavailable capabilities are reported explicitly, because
// a dimmed key is supposed to be explained rather than mysterious.
func probeLive() {
	players := Players()
	if len(players) == 0 {
		fmt.Println("no MPRIS players on the bus")
		return
	}
	fmt.Println("players:")
	for _, p := range players {
		fmt.Println("  -", p)
	}

	target := players[0]
	st := Read(target)

	fmt.Printf("\nreading %s\n", st.Player)
	fmt.Printf("  title      %q\n", st.Track.Title)
	fmt.Printf("  artist     %q\n", st.Track.Artist)
	fmt.Printf("  album      %q\n", st.Track.Album)
	fmt.Printf("  trackid    %q\n", st.Track.TrackID)
	fmt.Printf("  length     %v\n", st.Track.Length)
	fmt.Printf("  artUrl     %q\n", st.Track.ArtURL)
	fmt.Printf("  status     %q\n", st.Status)
	fmt.Printf("  position   %v\n", st.Position)
	if st.Err != "" {
		fmt.Printf("  error      %s\n", st.Err)
	}

	if st.Track.HasArt() {
		blk := LoadArt(st.Track.ArtURL, artCols, artRows)
		fmt.Printf("  art        %s rendered=%v cells=%dx%d\n",
			artKey(st.Track.ArtURL, artCols, artRows), blk.ok, blk.cols, blk.rows)
		if !blk.ok {
			fmt.Println("             (degraded to the placeholder — the URL did not resolve)")
		}
	}
}

// dumpLiveFrame renders whatever is genuinely playing right now, real
// cover art included. --dump is deterministic sample data for layout
// review; this is the other half — the only way to see whether real
// titles, real CJK text, and a real photograph actually survive the
// frame, without opening a window and racing the clock.
func dumpLiveFrame() {
	players := Players()
	if len(players) == 0 {
		fmt.Println("no MPRIS players on the bus")
		return
	}
	m := newModel()
	m.width, m.height = 80, 30
	m.player, m.players = players[0], players
	st := Read(players[0])
	m.st = st
	m.posBase, m.posAnchor = st.Position, time.Now()
	if st.Track.HasArt() {
		m.artKey = artKey(st.Track.ArtURL, artCols, artRows)
		m.art = LoadArt(st.Track.ArtURL, artCols, artRows)
	}
	fmt.Println(m.View())
}

// dumpSample renders the key screens with fake data to stdout, for
// headless visual checks. The art slot is filled with the placeholder so
// the frame geometry is honest.
func dumpSample() {
	base := newModel()
	base.width, base.height = 80, 30
	base.player = "chromium.instance7561"
	base.players = []string{base.player}
	base.st = State{
		Player: "chromium.instance7561",
		Track: Track{
			Title:  "Scars Don't Blink",
			Artist: "Two Poins Black",
			Album:  "トキオバーン",
			Length: 2*time.Minute + 32*time.Second,
		},
		Status:      "Playing",
		Unavailable: map[string]bool{capNext: true},
	}
	base.posBase, base.posAnchor = 83*time.Second, time.Now()
	base.tx = theme.Transmission{Visible: true, Clean: "present day, present time...", Text: "present day, present time..."}
	fmt.Println(base.View())

	// paused, every capability available, two players to switch between
	paused := base
	paused.st.Status = "Paused"
	paused.st.Unavailable = map[string]bool{}
	paused.players = []string{paused.player, "mpv"}
	paused.setNote("now controlling mpv")
	fmt.Println(paused.View())

	// nothing playing
	idle := newModel()
	idle.width, idle.height = 80, 30
	fmt.Println(idle.View())
}
