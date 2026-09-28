package main

// nowplaying.go — the NOW PLAYING tab: navi-nowplaying's MPRIS soul
// living inside navi-audio.
//
// Rationale, so nobody "simplifies" this away later: the bar's
// now-playing module got parked (a shape-shifting pill broke the bar's
// fixed-pill rhythm), which left the standalone navi-nowplaying mod
// orphaned with rofi as its only door. The volume module already opens
// navi-audio, so this tab puts "what's playing" where anyone looking
// for sound stuff will find it. The other five tabs are the plumbing
// (streams, devices, routing); this one is the identity of the sound.
//
// Scope stays transport-only, inherited from navi-nowplaying: play/
// pause, next, back, seek, player switching. No volume here — navi-audio
// already owns volume on the other five tabs, and a second volume
// control is two places to look and a second place to be wrong.
//
// Honesty note: MPRIS is not the PipeWire graph. A paused Spotify shows
// up here while making zero sound. The player chip names the MPRIS
// source explicitly so the tab never pretends to be the audio state.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const (
	npArtCols = 22
	npArtRows = 6

	// npInnerWidth is the frame content width minus the border's own
	// padding. frameWidth is a var (not a const), so the derived
	// values are spelled out: 62 - 4 = 58, per navi-nowplaying.
	npInnerWidth = 58
	npMetaWidth  = npInnerWidth - npArtCols - 2

	// npLabelWidth holds the longest time pair fmtTime can produce
	// ("10:00:00 / 10:00:00"); the meter takes the rest.
	npLabelWidth = 19
	npMeterWidth = npInnerWidth - 2 - 2 - npLabelWidth

	// npBodyLines is this tab's exact body height. The audio frame gives
	// every tab 15 content lines (19-line window minus 4 chrome lines);
	// tabBar + blank take 2, divider + blank take 2, footer takes 3, so
	// the tab body gets exactly 8. npPadBody enforces it.
	npBodyLines = 8

	npSeekStep      = 5 * time.Second
	npSlowTickEvery = 1500 * time.Millisecond
)

// ── messages ───────────────────────────────────────────────────────────

type npPlayersMsg []string

type npStateMsg struct{ st State }

type npArtMsg struct {
	key   string
	block artBlock
}

type npControlMsg struct {
	verb string
	err  error
}

type npPollTickMsg time.Time

// ── commands ───────────────────────────────────────────────────────────

// npPollCmd does every playerctl read in one goroutine, off the render
// path — the same one-goroutine-one-message shape as navi-nowplaying.
func npPollCmd(player string) tea.Cmd {
	return func() tea.Msg { return npStateMsg{st: Read(player)} }
}

// npArtCmd decodes cover art in a goroutine; decoding on the render
// path is a visible stutter.
func npArtCmd(key, artURL string) tea.Cmd {
	return func() tea.Msg { return npArtMsg{key: key, block: LoadArt(artURL, npArtCols, npArtRows)} }
}

// npControlCmd runs a playerctl verb and reports the result. A control
// is followed by an immediate re-poll so the UI never shows a value the
// player already changed underneath it.
func npControlCmd(player, verb string, value ...string) tea.Cmd {
	return func() tea.Msg { return npControlMsg{verb: verb, err: Do(player, verb, value...)} }
}

func (m *model) npPlayersCmd() tea.Cmd {
	return func() tea.Msg { return npPlayersMsg(Players()) }
}

func npPollTickCmd() tea.Cmd {
	return tea.Tick(npSlowTickEvery, func(t time.Time) tea.Msg { return npPollTickMsg(t) })
}

// npEnterCmd fires when the tab is selected: discover players, then poll.
func (m *model) npEnterCmd() tea.Cmd {
	if !havePlayer() {
		m.npNote, m.npErr = "", "playerctl not found — install playerctl"
		return nil
	}
	return tea.Batch(m.npPlayersCmd(), npPollTickCmd())
}

// ── state ──────────────────────────────────────────────────────────────

// npPos returns the interpolated playback position: npPosBase was true
// at npPosAnchor, and the render tick adds the elapsed time, so the
// meter moves smoothly with zero subprocesses between polls.
func (m model) npPos() time.Duration {
	if !m.npState.IsPlaying() {
		return m.npPosBase
	}
	d := time.Since(m.npPosAnchor)
	if d < 0 {
		return m.npPosBase
	}
	p := m.npPosBase + d
	if m.npState.Track.Length > 0 && p > m.npState.Track.Length {
		return m.npState.Track.Length
	}
	return p
}

func (m model) npHasTrack() bool {
	return m.npState.Track.Title != "" || m.npState.Track.Artist != ""
}

func (m *model) npSetNote(s string) { m.npNote, m.npErr = s, "" }
func (m *model) npSetErr(s string)  { m.npErr, m.npNote = s, "" }

func (m *model) npApplyState(st State) tea.Cmd {
	prev := m.npState
	m.npState = st
	m.npPosBase, m.npPosAnchor = st.Position, time.Now()

	if m.npState.Player == "" {
		m.npState.Player = m.npPlayer
	}
	if st.Err != "" {
		m.npSetErr(st.Err)
	}

	// A new track means a new cover: drop the memo and re-render.
	if st.Track.ArtURL != prev.Track.ArtURL || st.Track.TrackID != prev.Track.TrackID {
		key := artKey(st.Track.ArtURL, npArtCols, npArtRows)
		if key != m.npArtKey {
			m.npArtKey, m.npArt = key, placeholder(npArtCols, npArtRows)
			if st.Track.HasArt() && !m.npArtBusy {
				m.npArtBusy = true
				return npArtCmd(key, st.Track.ArtURL)
			}
		}
	}
	return nil
}

// ── keys ─────────────────────────────────────────────────────────────────

func (m model) updateNowPlaying(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case " ", "p":
		return m, npControlCmd(m.npPlayer, "play-pause")
	case "n":
		return m, npControlCmd(m.npPlayer, "next")
	case "b":
		return m, npControlCmd(m.npPlayer, "previous")
	case "left":
		return m.npSeek(-npSeekStep)
	case "right":
		return m.npSeek(npSeekStep)
	case "T":
		return m.npCyclePlayer()
	case "R":
		m.npNote, m.npErr = "", ""
		return m, tea.Batch(npPollCmd(m.npPlayer), npPollTickCmd())
	}
	return m, nil
}

func (m model) npSeek(d time.Duration) (tea.Model, tea.Cmd) {
	if !m.npState.Has(capSeek) {
		m.npSetNote("this player can't seek")
		return m, nil
	}
	target := m.npPos() + d
	if m.npState.Track.Length > 0 {
		target = npClampDur(target, 0, m.npState.Track.Length)
	}
	m.npPosBase, m.npPosAnchor = target, time.Now()
	return m, npControlCmd(m.npPlayer, "position", fmt.Sprintf("%.3f", target.Seconds()))
}

// npCyclePlayer moves to the next MPRIS player on the bus. On a real
// navi desk there is usually more than one thing controllable — a
// browser tab and a local player both expose MPRIS, and playerctl binds
// whichever it found first. Bound to T because tab already cycles the
// audio tabs; the footer names it so the key is never mysterious.
func (m model) npCyclePlayer() (tea.Model, tea.Cmd) {
	if len(m.npPlayers) < 2 {
		m.npSetNote("only " + npShortPlayer(m.npPlayer) + " on the bus")
		return m, nil
	}
	i := 0
	for j, p := range m.npPlayers {
		if p == m.npPlayer {
			i = j
			break
		}
	}
	m.npPlayer = m.npPlayers[(i+1)%len(m.npPlayers)]
	m.npState = State{Unavailable: map[string]bool{}}
	m.npArtKey, m.npArt = "", placeholder(npArtCols, npArtRows)
	m.npSetNote("now controlling " + npShortPlayer(m.npPlayer))
	return m, npPollCmd(m.npPlayer)
}

// ── view ─────────────────────────────────────────────────────────────────

// writeNowPlaying renders the tab body: exactly npBodyLines lines, so
// the frame never jumps between playing, paused, and idle.
func (m model) writeNowPlaying(b *strings.Builder) {
	if !m.npHasTrack() && len(m.npPlayers) == 0 {
		b.WriteString(m.npIdleView())
		return
	}
	var body strings.Builder
	body.WriteString(m.npHeaderBlock())
	body.WriteString("\n")
	body.WriteString(m.npProgressRow())
	body.WriteString("\n")
	body.WriteString(m.npNoteLine())
	b.WriteString(npPadBody(body.String()))
}

// npIdleView is the "nothing is playing" state: 8 lines, breathing, not
// dead. No art — there is nothing to illustrate.
func (m model) npIdleView() string {
	lines := []string{
		"",
		"  " + theme.Glow("♪", time.Now()),
		"",
		"  " + theme.Grayed.Render("the wired is quiet."),
		"  " + theme.Fainted.Render("nothing is playing right now."),
		"",
		m.npNoteLine(),
		"",
	}
	return strings.Join(lines, "\n")
}

// npHeaderBlock is the art beside the metadata, joined as one 6-line row.
func (m model) npHeaderBlock() string {
	art := lipgloss.NewStyle().Width(npArtCols).Render(m.npArt.rendered)

	meta := []string{
		lipgloss.NewStyle().Width(npMetaWidth).MaxHeight(2).Render(m.npTitle()),
		lipgloss.NewStyle().Width(npMetaWidth).MaxHeight(1).Render(theme.Grayed.Render(npOrDash(m.npState.Track.Artist))),
		lipgloss.NewStyle().Width(npMetaWidth).MaxHeight(1).Render(theme.Dimmed.Render(npOrDash(m.npState.Track.Album))),
		lipgloss.NewStyle().Width(npMetaWidth).MaxHeight(1).Render(m.npPlayerChip()),
		"",
		"",
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, art, "  ", strings.Join(meta, "\n"))
}

func (m model) npTitle() string {
	if m.npState.Track.Title == "" {
		return theme.Glow("♪  unknown track", time.Now())
	}
	return theme.Normal.Render(m.npState.Track.Title)
}

// npPlayerChip names the MPRIS source explicitly: this tab reports what
// the media bus says, which is not the same claim as the audio graph.
func (m model) npPlayerChip() string {
	name := npShortPlayer(m.npState.Player)
	if name == "" {
		return ""
	}
	if m.npState.Status == "" {
		return theme.Header.Render(name)
	}
	return theme.Header.Render(name) + " " + npStatusChip(m.npState.Status)
}

func npStatusChip(s string) string {
	switch s {
	case "Playing":
		return theme.Selected.Render("▶ playing")
	case "Paused":
		return theme.Grayed.Render("▮▮ paused")
	default:
		return theme.Dimmed.Render("· " + strings.ToLower(s))
	}
}

func (m model) npProgressRow() string {
	frac := 0.0
	if m.npState.Track.Length > 0 {
		frac = m.npPos().Seconds() / m.npState.Track.Length.Seconds()
	}
	fill, empty := theme.Green, theme.Faint
	if m.npState.IsPlaying() {
		fill = theme.Pink // pink while it runs, green when parked
	}
	meter := theme.Meter(npMeterWidth, frac, fill, empty)
	return "  " + meter + "  " + npLabelCell(fmt.Sprintf("%s / %s",
		npFmtTime(m.npPos()), npFmtTime(m.npState.Track.Length)))
}

// npLabelCell is the fixed-width right-hand time pair. It truncates
// rather than wraps: a number becoming two lines would change the
// frame's height, and a fixed frame is the whole point.
func npLabelCell(s string) string {
	return lipgloss.NewStyle().
		Width(npLabelWidth).
		MaxWidth(npLabelWidth).
		Align(lipgloss.Right).
		Render(theme.Normal.Render(s))
}

// npNoteLine is the reserved message line: always allocated, so a
// player hiccup never grows the frame at the worst moment.
func (m model) npNoteLine() string {
	switch {
	case m.npErr != "":
		return "  " + theme.Error.Render("× "+m.npErr)
	case m.npNote != "":
		return "  " + theme.Fainted.Render(m.npNote)
	default:
		return ""
	}
}

// npPadBody forces a body to exactly npBodyLines lines. A body that is
// already too long is a bug — truncate from the bottom so the frame
// survives it visibly rather than tearing.
func npPadBody(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) < npBodyLines {
		lines = append(lines, "")
	}
	if len(lines) > npBodyLines {
		lines = lines[:npBodyLines]
	}
	return strings.Join(lines, "\n")
}

// npFooter is the tab's own 3-line footer: transport keys, capability-
// dimmed (a key the player can't service renders faint instead of
// vanishing, so the bar never rearranges under the user's fingers).
func (m model) npFooter() string {
	line1 := theme.Footer(true,
		[2]string{"1-6", "tabs"},
		[2]string{"space", "play"},
		[2]string{"←→", "seek"},
	)
	line2 := theme.Footer(m.npState.Has(capNext), [2]string{"n", "next"}) + "   " +
		theme.Footer(m.npState.Has(capPrev), [2]string{"b", "back"}) + "   " +
		theme.Footer(len(m.npPlayers) > 1, [2]string{"T", "switch"})
	line3 := theme.Footer(true,
		[2]string{"R", "refresh"},
		[2]string{"q", "quit"},
	)
	return line1 + "\n" + line2 + "\n" + line3
}

// ── helpers ──────────────────────────────────────────────────────────────

func npFmtTime(d time.Duration) string {
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

func npOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func npClampDur(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// npShortPlayer trims the noisy ".instanceNNNN" suffix MPRIS players carry.
func npShortPlayer(p string) string {
	if i := strings.Index(p, ".instance"); i > 0 {
		return p[:i]
	}
	return p
}

// npHumanVerb turns a playerctl verb into something a person can read.
func npHumanVerb(v string) string {
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
