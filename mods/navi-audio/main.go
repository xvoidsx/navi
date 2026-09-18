// navi-audio — the navi volume/device mixer for navi 2 "eiri".
//
// pavucontrol parity through PulseAudio's pactl(1) JSON interface:
// Playback and Recording streams, Output and Input devices, and card
// Configuration — volume, mute, routing, fallback devices, ports, and
// profiles, all in the nightshadeNeon visual language.
//
// Live state arrives over one persistent `pactl subscribe` subprocess;
// event bursts are coalesced with a short debounce before re-listing,
// and a slow safety re-list backs it up. Per-stream level meters have no
// pactl CLI, so streams that are actually moving audio get a decorative
// glitch shimmer instead — aesthetic, never presented as measured data.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rav3ndust/navi-theme"
)

// frameWidth is the family-standard mod window width. Views assume it.
var frameWidth = 62

var okStyle = theme.DotOn.Copy().Bold(true)
var mutedStyle = lipgloss.NewStyle().Foreground(theme.Red)

// ── tabs & screens ─────────────────────────────────────────────────────

type tab int

const (
	tabPlayback tab = iota
	tabRecording
	tabOutputs
	tabInputs
	tabConfig
)

const numTabs = 5

var tabTitles = []string{"PLAYBACK", "RECORDING", "OUTPUTS", "INPUTS", "CONFIG"}

type screen int

const (
	screenMain screen = iota
	screenRoute
)

// ── messages ───────────────────────────────────────────────────────────

type startMsg struct{}

type snapshotMsg struct {
	snap audioSnapshot
	err  error
}

type opMsg struct {
	err  error
	note string
}

type subscribeLineMsg struct{ line string }
type subscribeDoneMsg struct{ err error }
type restartSubMsg struct{}

type refreshDebounceMsg struct{}
type safetyTickMsg struct{}
type animTickMsg struct{}
type transTickMsg struct{}

// transFrames is the ~4-frame, ~45ms-per-frame ANSI-aware static
// dissolve between screens, matching navi-networking.
const transFrames = 4

func transTickCmd() tea.Cmd {
	return tea.Tick(45*time.Millisecond, func(time.Time) tea.Msg { return transTickMsg{} })
}

// animTick drives the spinner, the stream shimmer, and the breathing
// idle re-render. ~7 fps keeps it cheap.
func animTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

// safetyTick is the slow backstop re-list for missed subscribe events.
func safetyTickCmd() tea.Cmd {
	return tea.Tick(45*time.Second, func(time.Time) tea.Msg { return safetyTickMsg{} })
}

// debounceCmd coalesces subscribe event bursts: drain briefly, re-list
// once.
func debounceCmd() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return refreshDebounceMsg{} })
}

// ── model ──────────────────────────────────────────────────────────────

type model struct {
	width, height int
	tx            theme.Transmission
	transition    int

	screen screen
	tab    tab
	cursor [numTabs]int

	loading bool
	cancel  context.CancelFunc

	sinks     []AudioNode
	sources   []AudioNode
	playback  []Stream
	recording []Stream
	cards     []Card

	defaultSink   string
	defaultSource string
	daemonErr     error

	showMonitors bool

	message string
	err     error

	animFrame int

	// subscribe plumbing
	subCh      chan string
	subCtx     context.Context
	subCancel  context.CancelFunc
	subBackoff time.Duration

	refreshDue    bool // a debounced refresh is scheduled
	refreshQueued bool // another event arrived while one was scheduled
	refreshing    bool // a refresh is currently running
	quitting      bool

	// route picker state
	routeStream   Stream
	routePlayback bool
	routeTargets  []AudioNode
	routeCursor   int
}

func initialModel() model {
	subCtx, subCancel := context.WithCancel(context.Background())
	return model{
		tx:         theme.Transmission{},
		screen:     screenMain,
		tab:        tabPlayback,
		loading:    true,
		subCh:      make(chan string, 64),
		subCtx:     subCtx,
		subCancel:  subCancel,
		subBackoff: time.Second,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.tx.Init(),
		func() tea.Msg { return startMsg{} },
		animTickCmd(),
		safetyTickCmd(),
	)
}

// ── commands ───────────────────────────────────────────────────────────

func (m *model) startOp() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return ctx
}

func refreshAllCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		snap, err := fetchSnapshot(ctx)
		return snapshotMsg{snap: snap, err: err}
	}
}

func (m *model) launchSubscriber() tea.Cmd {
	return func() tea.Msg {
		err := runSubscriber(m.subCtx, m.subCh)
		return subscribeDoneMsg{err: err}
	}
}

func waitSubCmd(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return subscribeDoneMsg{}
		}
		return subscribeLineMsg{line: line}
	}
}

// scheduleRefresh runs a full re-list now unless one is already running.
func (m *model) scheduleRefresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	ctx := m.startOp()
	return refreshAllCmd(ctx)
}

// ── update ─────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case startMsg:
		ctx := m.startOp()
		return m, tea.Batch(
			m.launchSubscriber(),
			waitSubCmd(m.subCh),
			refreshAllCmd(ctx),
		)

	case snapshotMsg:
		m.refreshing = false
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.loading = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.daemonErr = msg.err
		if msg.err == nil {
			m.sinks = msg.snap.sinks
			m.sources = msg.snap.sources
			m.playback = msg.snap.playback
			m.recording = msg.snap.recording
			m.cards = msg.snap.cards
			m.defaultSink = msg.snap.defaultSink
			m.defaultSource = msg.snap.defaultSource
		}
		m.clampCursors()

	case opMsg:
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err
			m.message = ""
		} else {
			m.message = msg.note
			m.err = nil
		}
		// The write landed; re-list so the UI converges fast instead
		// of waiting on the subscribe echo.
		return m, m.scheduleRefresh()

	case subscribeLineMsg:
		m.subBackoff = time.Second
		wait := waitSubCmd(m.subCh)
		if subRefreshWanted(msg.line) && !m.refreshing {
			if !m.refreshDue {
				m.refreshDue = true
				return m, tea.Batch(wait, debounceCmd())
			}
			m.refreshQueued = true
		}
		return m, wait

	case subscribeDoneMsg:
		if m.quitting || errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		// Server restarted or login race: reconnect with backoff,
		// never die on it.
		d := m.subBackoff
		m.subBackoff *= 2
		if m.subBackoff > 15*time.Second {
			m.subBackoff = 15 * time.Second
		}
		return m, tea.Tick(d, func(time.Time) tea.Msg { return restartSubMsg{} })

	case restartSubMsg:
		if m.quitting {
			return m, nil
		}
		return m, tea.Batch(m.launchSubscriber(), waitSubCmd(m.subCh))

	case refreshDebounceMsg:
		if m.refreshQueued {
			m.refreshQueued = false
			return m, debounceCmd()
		}
		m.refreshDue = false
		return m, m.scheduleRefresh()

	case safetyTickMsg:
		cmd := safetyTickCmd()
		if !m.refreshDue {
			if rc := m.scheduleRefresh(); rc != nil {
				return m, tea.Batch(cmd, rc)
			}
		}
		return m, cmd

	case animTickMsg:
		m.animFrame++
		return m, animTickCmd()

	case theme.TxShowMsg, theme.TxGlitchTickMsg, theme.TxHoldDoneMsg, theme.TxFlickerMsg:
		var cmd tea.Cmd
		m.tx, cmd = m.tx.Update(msg)
		return m, cmd

	case transTickMsg:
		if m.transition > 0 {
			m.transition--
			if m.transition > 0 {
				return m, transTickCmd()
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m.quit()
		case "esc":
			if m.screen == screenRoute {
				m.screen = screenMain
				m.transition = transFrames
				return m, transTickCmd()
			}
			return m.quit()
		}
		var cmd tea.Cmd
		var nm tea.Model
		switch m.screen {
		case screenRoute:
			nm, cmd = m.updateRoute(msg)
		default:
			nm, cmd = m.updateMain(msg)
		}
		if mm, ok := nm.(model); ok {
			m = mm
		}
		return m, cmd
	}
	return m, nil
}

func (m model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	if m.cancel != nil {
		m.cancel()
	}
	m.subCancel()
	return m, tea.Quit
}

// ── cursor & selection helpers ─────────────────────────────────────────

func (m *model) clampCursors() {
	lengths := [numTabs]int{
		len(m.playback),
		len(m.recording),
		len(m.sinks),
		len(m.visibleSources()),
		len(m.cards),
	}
	for i := 0; i < numTabs; i++ {
		if m.cursor[i] >= lengths[i] {
			m.cursor[i] = lengths[i] - 1
		}
		if m.cursor[i] < 0 {
			m.cursor[i] = 0
		}
	}
	if m.routeCursor >= len(m.routeTargets) {
		m.routeCursor = len(m.routeTargets) - 1
	}
	if m.routeCursor < 0 {
		m.routeCursor = 0
	}
}

// visibleSources hides monitor sources (name ends in .monitor) unless
// the user toggles them on — pavucontrol's "Show:" dropdown, client-side.
func (m model) visibleSources() []AudioNode {
	if m.showMonitors {
		return m.sources
	}
	out := make([]AudioNode, 0, len(m.sources))
	for _, s := range m.sources {
		if strings.HasSuffix(s.Name, ".monitor") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (m model) rowCount() int {
	switch m.tab {
	case tabPlayback:
		return len(m.playback)
	case tabRecording:
		return len(m.recording)
	case tabOutputs:
		return len(m.sinks)
	case tabInputs:
		return len(m.visibleSources())
	default:
		return len(m.cards)
	}
}

func (m *model) moveCursor(d int) {
	n := m.rowCount()
	if n == 0 {
		return
	}
	m.cursor[m.tab] = (m.cursor[m.tab] + d + n) % n
}

func (m *model) setTab(t tab) {
	m.tab = t
	m.transition = transFrames
	m.message = ""
	m.err = nil
}

func (m model) curStream(playback bool) (Stream, bool) {
	var list []Stream
	if playback {
		list = m.playback
	} else {
		list = m.recording
	}
	i := m.cursor[m.tab]
	if i < 0 || i >= len(list) {
		return Stream{}, false
	}
	return list[i], true
}

func (m model) curSink() (AudioNode, bool) {
	i := m.cursor[tabOutputs]
	if i < 0 || i >= len(m.sinks) {
		return AudioNode{}, false
	}
	return m.sinks[i], true
}

func (m model) curSource() (AudioNode, bool) {
	list := m.visibleSources()
	i := m.cursor[tabInputs]
	if i < 0 || i >= len(list) {
		return AudioNode{}, false
	}
	return list[i], true
}

func (m model) curCard() (Card, bool) {
	i := m.cursor[tabConfig]
	if i < 0 || i >= len(m.cards) {
		return Card{}, false
	}
	return m.cards[i], true
}

// ── main-screen keys ───────────────────────────────────────────────────

func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m.quit()
	case "tab":
		m.setTab((m.tab + 1) % numTabs)
		return m, transTickCmd()
	case "shift+tab":
		m.setTab((m.tab + numTabs - 1) % numTabs)
		return m, transTickCmd()
	case "1", "2", "3", "4", "5":
		t := tab(msg.String()[0] - '1')
		if t != m.tab {
			m.setTab(t)
			return m, transTickCmd()
		}
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "+", "=", "]":
		return m, m.volCmd(5)
	case "-", "_", "[":
		return m, m.volCmd(-5)
	case "m":
		return m, m.muteCmd()
	case "d":
		return m, m.defaultCmd()
	case "r":
		return m, m.actionRCmd()
	case "v":
		if m.tab == tabInputs {
			m.showMonitors = !m.showMonitors
			m.clampCursors()
			m.err = nil
			if m.showMonitors {
				m.message = "showing monitor sources"
			} else {
				m.message = "hiding monitor sources"
			}
		}
		return m, nil
	case "R":
		m.loading = true
		m.err = nil
		m.message = ""
		return m, m.scheduleRefresh()
	}
	return m, nil
}

// volCmd adjusts the selected row's volume by step percent, capped at
// 100% like pavucontrol.
func (m *model) volCmd(step int) tea.Cmd {
	switch m.tab {
	case tabPlayback:
		if s, ok := m.curStream(true); ok {
			spec := volTarget(s.VolumePct, step)
			return setVolumeCmd("sink-input", s.Index, spec, volNote(s.VolumePct, step, streamName(s)))
		}
	case tabRecording:
		if s, ok := m.curStream(false); ok {
			spec := volTarget(s.VolumePct, step)
			return setVolumeCmd("source-output", s.Index, spec, volNote(s.VolumePct, step, streamName(s)))
		}
	case tabOutputs:
		if n, ok := m.curSink(); ok {
			spec := volTarget(n.VolumePct, step)
			return setVolumeCmd("sink", n.Index, spec, volNote(n.VolumePct, step, shortName(n.Description)))
		}
	case tabInputs:
		if n, ok := m.curSource(); ok {
			spec := volTarget(n.VolumePct, step)
			return setVolumeCmd("source", n.Index, spec, volNote(n.VolumePct, step, shortName(n.Description)))
		}
	}
	return nil
}

func (m *model) muteCmd() tea.Cmd {
	noteFor := func(muted bool, what string) string {
		if muted {
			return what + " unmuted"
		}
		return what + " muted"
	}
	switch m.tab {
	case tabPlayback:
		if s, ok := m.curStream(true); ok {
			return muteToggleCmd("sink-input", s.Index, noteFor(s.Mute, streamName(s)))
		}
	case tabRecording:
		if s, ok := m.curStream(false); ok {
			return muteToggleCmd("source-output", s.Index, noteFor(s.Mute, streamName(s)))
		}
	case tabOutputs:
		if n, ok := m.curSink(); ok {
			return muteToggleCmd("sink", n.Index, noteFor(n.Mute, shortName(n.Description)))
		}
	case tabInputs:
		if n, ok := m.curSource(); ok {
			return muteToggleCmd("source", n.Index, noteFor(n.Mute, shortName(n.Description)))
		}
	}
	return nil
}

// defaultCmd sets the selected device as the fallback (pavucontrol's
// "Set as fallback").
func (m *model) defaultCmd() tea.Cmd {
	switch m.tab {
	case tabOutputs:
		if n, ok := m.curSink(); ok {
			return setDefaultCmd(true, n.Name, shortName(n.Description)+" is now the fallback output")
		}
	case tabInputs:
		if n, ok := m.curSource(); ok {
			return setDefaultCmd(false, n.Name, shortName(n.Description)+" is now the fallback input")
		}
	}
	return nil
}

// actionRCmd is the context key: route a stream, cycle a device port, or
// cycle a card profile.
func (m *model) actionRCmd() tea.Cmd {
	switch m.tab {
	case tabPlayback, tabRecording:
		playback := m.tab == tabPlayback
		if s, ok := m.curStream(playback); ok {
			m.screen = screenRoute
			m.routeStream = s
			m.routePlayback = playback
			if playback {
				m.routeTargets = m.sinks
			} else {
				m.routeTargets = m.sources
			}
			m.routeCursor = 0
			m.message = ""
			m.err = nil
			m.transition = transFrames
			return transTickCmd()
		}
	case tabOutputs:
		if n, ok := m.curSink(); ok {
			return cyclePortCmd(true, n)
		}
	case tabInputs:
		if n, ok := m.curSource(); ok {
			return cyclePortCmd(false, n)
		}
	case tabConfig:
		if c, ok := m.curCard(); ok {
			return cycleProfileCmd(c)
		}
	}
	return nil
}

// cyclePortCmd steps to the next port on a device and applies it.
func cyclePortCmd(isSink bool, n AudioNode) tea.Cmd {
	if len(n.Ports) == 0 {
		return func() tea.Msg { return opMsg{note: "no ports on this device"} }
	}
	cur := -1
	for i, p := range n.Ports {
		if p.Name == n.ActivePort {
			cur = i
			break
		}
	}
	next := n.Ports[(cur+1)%len(n.Ports)]
	label := next.Description
	if label == "" {
		label = next.Name
	}
	return setPortCmd(isSink, n.Index, next.Name, "port: "+label)
}

// cycleProfileCmd steps to the next available profile on a card and
// applies it, skipping profiles the card reports as unavailable.
func cycleProfileCmd(c Card) tea.Cmd {
	if len(c.Profiles) == 0 {
		return func() tea.Msg { return opMsg{note: "no profiles on this card"} }
	}
	cur := -1
	for i, p := range c.Profiles {
		if p.Name == c.ActiveProfile {
			cur = i
			break
		}
	}
	for step := 1; step <= len(c.Profiles); step++ {
		next := c.Profiles[(cur+step)%len(c.Profiles)]
		if next.Available == "no" {
			continue
		}
		label := next.Description
		if label == "" {
			label = next.Name
		}
		return setProfileCmd(c.Index, next.Name, "profile: "+label)
	}
	return func() tea.Msg { return opMsg{note: "no available profiles"} }
}

// ── route picker keys ──────────────────────────────────────────────────

func (m model) updateRoute(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.screen = screenMain
		m.transition = transFrames
		return m, transTickCmd()
	case "up", "k":
		if len(m.routeTargets) > 0 {
			m.routeCursor = (m.routeCursor - 1 + len(m.routeTargets)) % len(m.routeTargets)
		}
	case "down", "j":
		if len(m.routeTargets) > 0 {
			m.routeCursor = (m.routeCursor + 1) % len(m.routeTargets)
		}
	case "enter":
		if m.routeCursor < len(m.routeTargets) {
			t := m.routeTargets[m.routeCursor]
			m.screen = screenMain
			m.transition = transFrames
			note := streamName(m.routeStream) + " → " + shortName(t.Description)
			move := moveStreamCmd(m.routePlayback, m.routeStream.Index, t.Index, note)
			return m, tea.Batch(move, transTickCmd())
		}
	}
	return m, nil
}

// ── views ─────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.screen {
	case screenRoute:
		return m.routeView()
	default:
		return m.mainView()
	}
}

// audioLive drives the frame's status dot: green while any audio is
// actually flowing or a device is RUNNING.
func (m model) audioLive() bool {
	for _, s := range m.playback {
		if s.Active && !s.Mute {
			return true
		}
	}
	for _, s := range m.recording {
		if s.Active && !s.Mute {
			return true
		}
	}
	for _, n := range m.sinks {
		if strings.ToUpper(n.State) == "RUNNING" {
			return true
		}
	}
	return false
}

func (m model) frame(content string) string {
	return theme.Frame(frameWidth, "navi audio", m.audioLive(), m.tx.View(frameWidth), content)
}

func (m model) place(content string) string {
	if m.width == 0 {
		return content
	}
	out := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.frame(content))
	if m.transition > 0 {
		// Static dissolve between screens: glitch the composed frame,
		// fading as the transition counts down. ANSI-aware so colors
		// survive the noise.
		intensity := 0.12 + 0.55*float64(m.transition)/float64(transFrames)
		out = theme.GlitchANSI(out, intensity)
	}
	return out
}

func (m model) tabBar() string {
	parts := make([]string, 0, numTabs)
	for i, t := range tabTitles {
		label := fmt.Sprintf("%d %s", i+1, t)
		if tab(i) == m.tab {
			parts = append(parts, theme.Selected.Render("▸"+label))
		} else {
			parts = append(parts, theme.Dimmed.Render(" "+label))
		}
	}
	return strings.Join(parts, " ")
}

func (m model) mainView() string {
	var b strings.Builder
	b.WriteString(m.tabBar())
	b.WriteString("\n\n")
	switch m.tab {
	case tabPlayback:
		m.writeStreams(&b, m.playback, true)
	case tabRecording:
		m.writeStreams(&b, m.recording, false)
	case tabOutputs:
		m.writeDevices(&b, m.sinks, true)
	case tabInputs:
		m.writeDevices(&b, m.visibleSources(), false)
	case tabConfig:
		m.writeCards(&b)
	}
	if m.message != "" {
		b.WriteString("\n" + okStyle.Render("✓ "+m.message) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + theme.Error.Render("× "+m.err.Error()) + "\n")
	}
	if m.daemonErr != nil {
		b.WriteString("\n" + theme.Error.Render("× audio daemon unreachable") + "\n")
		b.WriteString(theme.Dimmed.Render("  "+truncateRunes(m.daemonErr.Error(), 56)) + "\n")
		b.WriteString(theme.Dimmed.Render("  is PulseAudio running? press R to retry") + "\n")
	} else if m.loading {
		b.WriteString("\n" + theme.Spinner(m.animFrame) + " " + theme.Dimmed.Render("listening to the daemon...") + "\n")
	} else if !m.audioLive() {
		// Nothing moving: the mod breathes, waiting.
		b.WriteString("\n" + theme.Glow("  ○ the Wired is silent — listening", time.Now()) + "\n")
	}
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(m.footer())
	return m.place(b.String())
}

func (m model) writeStreams(b *strings.Builder, streams []Stream, playback bool) {
	color := theme.Pink
	if !playback {
		color = theme.Cyan
	}
	if len(streams) == 0 {
		what := "playback"
		if !playback {
			what = "recording"
		}
		b.WriteString(theme.Dimmed.Render("  no "+what+" streams right now") + "\n")
		return
	}
	for i, s := range streams {
		cursor := "  "
		name := padRight(truncateRunes(streamName(s), 24), 24)
		if i == m.cursor[m.tab] {
			cursor = theme.Selected.Render("› ")
			name = theme.Selected.Render(name)
		} else {
			name = theme.Normal.Render(name)
		}
		meter := volumeBar(s.VolumePct, 10, color)
		sig := streamShimmer(s, m.animFrame, playback)
		b.WriteString(fmt.Sprintf("%s%s %s %s %s\n", cursor, name, meter, volTextPct(s.VolumePct), sig))
		detail := "  " + m.streamDetail(s, playback)
		if s.Mute {
			b.WriteString(theme.Dimmed.Render(truncateRunes(detail, 52)) + "  " + mutedStyle.Render("muted") + "\n")
		} else {
			b.WriteString(theme.Dimmed.Render(truncateRunes(detail, 56)) + "\n")
		}
	}
	// Honest footnote: the shimmer is aesthetic. pactl exposes no live
	// levels, and we never present the decoration as measured data.
	b.WriteString(theme.Fainted.Render("  signal shimmer is decorative — pactl exposes no live levels") + "\n")
}

func (m model) writeDevices(b *strings.Builder, nodes []AudioNode, isSink bool) {
	if len(nodes) == 0 {
		b.WriteString(theme.Dimmed.Render("  no devices found") + "\n")
		return
	}
	for i, n := range nodes {
		cursor := "  "
		name := padRight(truncateRunes(n.Description, 34), 34)
		if i == m.cursor[m.tab] {
			cursor = theme.Selected.Render("› ")
			name = theme.Selected.Render(name)
		} else {
			name = theme.Normal.Render(name)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, name, volumeBar(n.VolumePct, 10, theme.Green), volTextPct(n.VolumePct)))
		var sub strings.Builder
		sub.WriteString("    ")
		if n.ActivePort != "" {
			sub.WriteString("port: " + truncateRunes(portDesc(n), 22))
		}
		if st := strings.ToUpper(n.State); st == "RUNNING" {
			sub.WriteString("  · live")
		}
		line := theme.Dimmed.Render(sub.String())
		isDefault := (isSink && n.Name == m.defaultSink) || (!isSink && n.Name == m.defaultSource)
		if isDefault {
			line += "  " + okStyle.Render("● fallback")
		}
		if n.Mute {
			line += "  " + mutedStyle.Render("muted")
		}
		b.WriteString(line + "\n")
	}
	if m.tab == tabInputs && !m.showMonitors {
		hidden := 0
		for _, s := range m.sources {
			if strings.HasSuffix(s.Name, ".monitor") {
				hidden++
			}
		}
		if hidden > 0 {
			b.WriteString(theme.Dimmed.Render(fmt.Sprintf("  %d monitor source(s) hidden — v to show", hidden)) + "\n")
		}
	}
}

func (m model) writeCards(b *strings.Builder) {
	if len(m.cards) == 0 {
		b.WriteString(theme.Dimmed.Render("  no cards found") + "\n")
		return
	}
	for i, c := range m.cards {
		cursor := "  "
		name := truncateRunes(c.Description, 54)
		if i == m.cursor[m.tab] {
			cursor = theme.Selected.Render("› ")
			name = theme.Selected.Render(name)
		} else {
			name = theme.Normal.Render(name)
		}
		b.WriteString(cursor + name + "\n")
		profDesc := c.ActiveProfile
		for _, p := range c.Profiles {
			if p.Name == c.ActiveProfile && p.Description != "" {
				profDesc = p.Description
				break
			}
		}
		if profDesc == "" {
			profDesc = "unknown"
		}
		b.WriteString("    " + theme.Dimmed.Render("profile: ") +
			theme.Normal.Render(truncateRunes(profDesc, 38)) +
			theme.Dimmed.Render(fmt.Sprintf("  · %d profiles", len(c.Profiles))) + "\n")
	}
}

// portDesc resolves the active port's human description.
func portDesc(n AudioNode) string {
	for _, p := range n.Ports {
		if p.Name == n.ActivePort && p.Description != "" {
			return p.Description
		}
	}
	return n.ActivePort
}

// streamShimmer is the decorative activity shimmer for streams that are
// actually moving audio. pactl exposes no live levels, so this is
// aesthetic only — a glitch waveform, never presented as measured data.
// The plain glyphs are glitched BEFORE styling, since theme.Glitch is
// not ANSI-aware.
func streamShimmer(s Stream, frame int, playback bool) string {
	const cells = 8
	if !s.Active || s.Mute {
		return theme.Fainted.Render("· · · ·")
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var sb strings.Builder
	for i := 0; i < cells; i++ {
		sb.WriteRune(blocks[(i*3+frame*2+s.Index*5)%len(blocks)])
	}
	glitched := theme.Glitch(sb.String(), 0.25)
	color := theme.Pink
	if !playback {
		color = theme.Cyan
	}
	return lipgloss.NewStyle().Foreground(color).Render(glitched)
}

// volumeBar renders a small volume meter; unknown volume shows dots.
func volumeBar(pct float64, width int, color lipgloss.TerminalColor) string {
	if pct < 0 {
		return theme.Dimmed.Render(strings.Repeat("·", width))
	}
	return theme.Meter(width, pct/100, color, theme.Faint)
}

func volTextPct(pct float64) string {
	if pct < 0 {
		return theme.Dimmed.Render(" -- ")
	}
	return theme.Normal.Render(fmt.Sprintf("%3.0f%%", pct))
}

func (m model) streamDetail(s Stream, playback bool) string {
	target := "?"
	if playback {
		for _, n := range m.sinks {
			if n.Index == s.Device {
				target = shortName(n.Description)
				break
			}
		}
	} else {
		for _, n := range m.sources {
			if n.Index == s.Device {
				target = shortName(n.Description)
				break
			}
		}
	}
	media := s.MediaName
	if media == "" {
		media = s.Binary
	}
	if media != "" {
		return truncateRunes(media, 28) + " → " + target
	}
	return "→ " + target
}

func (m model) footer() string {
	onDevice := m.tab == tabOutputs || m.tab == tabInputs
	rLabel := "route"
	switch m.tab {
	case tabOutputs, tabInputs:
		rLabel = "port"
	case tabConfig:
		rLabel = "profile"
	}
	line1 := theme.Footer(true,
		[2]string{"1-5", "tabs"},
		[2]string{"↑↓", "navigate"},
		[2]string{"+/-", "volume"},
	)
	line2 := theme.Footer(true, [2]string{"m", "mute"}) + "   " +
		theme.Footer(onDevice, [2]string{"d", "fallback"}) + "   " +
		theme.Footer(true, [2]string{"r", rLabel}) + "   " +
		theme.Footer(m.tab == tabInputs, [2]string{"v", "monitors"})
	line3 := theme.Footer(true,
		[2]string{"R", "refresh"},
		[2]string{"q", "quit"},
	)
	return line1 + "\n" + line2 + "\n" + line3
}

func (m model) routeView() string {
	var b strings.Builder
	what := "output"
	if !m.routePlayback {
		what = "input"
	}
	b.WriteString(theme.Header.Render("ROUTE STREAM") + "\n\n")
	b.WriteString(theme.Normal.Render("  "+streamName(m.routeStream)) + "\n")
	b.WriteString(theme.Dimmed.Render("  choose an "+what+" device") + "\n\n")
	if len(m.routeTargets) == 0 {
		b.WriteString(theme.Dimmed.Render("  no devices available") + "\n")
	} else {
		for i, n := range m.routeTargets {
			cursor := "  "
			name := truncateRunes(n.Description, 50)
			if i == m.routeCursor {
				cursor = theme.Selected.Render("› ")
				name = theme.Selected.Render(name)
			}
			b.WriteString(cursor + name + "\n")
		}
	}
	b.WriteString("\n" + theme.Divider(frameWidth) + "\n")
	b.WriteString(theme.Footer(true,
		[2]string{"↑↓", "navigate"},
		[2]string{"enter", "route"},
		[2]string{"esc", "back"},
	))
	return m.place(b.String())
}

// ── small text helpers ─────────────────────────────────────────────────

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

func shortName(s string) string { return truncateRunes(s, 24) }

func streamName(s Stream) string {
	if s.AppName != "" {
		return s.AppName
	}
	return fmt.Sprintf("stream #%d", s.Index)
}

// ── main & headless dump ───────────────────────────────────────────────

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dump" {
		dumpSample()
		return
	}
	if _, err := exec.LookPath("pactl"); err != nil {
		fmt.Fprintln(os.Stderr, "navi-audio: pactl was not found.")
		fmt.Fprintln(os.Stderr, "Install pulseaudio-utils first.")
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "navi-audio: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(50 * time.Millisecond)
}

// dumpSample renders every tab headless (no pactl, no TTY) with
// representative data. The sample JSON is parsed through the real
// backend parsers, so the dump also exercises them.
func dumpSample() {
	m := initialModel()
	m.width, m.height = 80, 24
	m.loading = false

	sinks, _ := pactlJSONBytes([]byte(sampleSinks))
	for _, o := range sinks {
		m.sinks = append(m.sinks, nodeFromMap(o))
	}
	sources, _ := pactlJSONBytes([]byte(sampleSources))
	for _, o := range sources {
		m.sources = append(m.sources, nodeFromMap(o))
	}
	si, _ := pactlJSONBytes([]byte(sampleSinkInputs))
	for _, o := range si {
		m.playback = append(m.playback, streamFromMap(o, true))
	}
	so, _ := pactlJSONBytes([]byte(sampleSourceOutputs))
	for _, o := range so {
		m.recording = append(m.recording, streamFromMap(o, false))
	}
	cards, _ := pactlJSONBytes([]byte(sampleCards))
	for _, o := range cards {
		m.cards = append(m.cards, cardFromMap(o))
	}
	m.defaultSink = "alsa_output.pci-0000_00_1b.0.analog-stereo"
	m.defaultSource = "alsa_input.pci-0000_00_1b.0.analog-stereo"

	m.tx = theme.Transmission{Visible: true, Clean: "present day, present time...", Text: "present day, present time..."}

	for t := tab(0); t < numTabs; t++ {
		m.tab = t
		m.animFrame = 3
		fmt.Printf("── %s ──\n", tabTitles[t])
		fmt.Println(m.mainView())
	}

	m.tab = tabPlayback
	m.screen = screenRoute
	m.routeStream = m.playback[0]
	m.routePlayback = true
	m.routeTargets = m.sinks
	fmt.Println("── ROUTE ──")
	fmt.Println(m.routeView())
}

// pactlJSONBytes decodes a bare JSON array of loosely-typed objects,
// mirroring pactlJSON without spawning pactl.
func pactlJSONBytes(raw []byte) ([]map[string]json.RawMessage, error) {
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, fmt.Errorf("sample JSON: bad shape: %w", err)
	}
	return objs, nil
}

// Sample data for --dump, shaped after the backend spec's documented
// pactl 17.0 objects. Note the deliberately odd sibling keys
// ("value_dB", "valueDb") inside the volume maps: the parser must read
// value_percent and ignore the rest.

const sampleSinks = `[
  {
    "index": 0,
    "name": "alsa_output.pci-0000_00_1b.0.analog-stereo",
    "description": "Built-in Audio Analog Stereo",
    "driver": "module-alsa-card.c",
    "state": "RUNNING",
    "volume": {
      "front-left":  { "value_percent": "74%", "value": 48372, "value_dB": "-7.50 dB" },
      "front-right": { "value_percent": "74%", "value": 48372, "value_dB": "-7.50 dB" }
    },
    "mute": false,
    "ports": [
      { "name": "analog-output-speaker",    "description": "Speakers",   "available": "yes",     "priority": 10000 },
      { "name": "analog-output-headphones", "description": "Headphones", "available": "no",      "priority": 9000 }
    ],
    "active_port": "analog-output-speaker",
    "properties": { "device.icon_name": "audio-speakers", "device.form_factor": "internal" }
  },
  {
    "index": 1,
    "name": "bluez_output.2C_41_A1_9B_4E_77.a2dp-sink",
    "description": "WH-1000XM4",
    "driver": "module-bluez5-device.c",
    "state": "IDLE",
    "volume": {
      "front-left":  { "value_percent": "100%", "value": 65536 },
      "front-right": { "value_percent": "100%", "value": 65536 }
    },
    "mute": false,
    "ports": [
      { "name": "headphone-output", "description": "Headphones", "available": "unknown", "priority": 0 }
    ],
    "active_port": "headphone-output",
    "properties": { "device.icon_name": "audio-headphones", "device.form_factor": "headphone" }
  }
]`

const sampleSources = `[
  {
    "index": 2,
    "name": "alsa_input.pci-0000_00_1b.0.analog-stereo",
    "description": "Built-in Audio Analog Stereo",
    "driver": "module-alsa-card.c",
    "state": "IDLE",
    "volume": {
      "front-left":  { "value_percent": "60%", "valueDb": "-13.40 dB" },
      "front-right": { "value_percent": "60%", "valueDb": "-13.40 dB" }
    },
    "mute": false,
    "ports": [
      { "name": "analog-input-internal-mic", "description": "Internal Microphone", "available": "yes", "priority": 8900 },
      { "name": "analog-input-mic",          "description": "Microphone",          "available": "no",  "priority": 8700 }
    ],
    "active_port": "analog-input-internal-mic",
    "properties": { "device.icon_name": "audio-input-microphone", "device.form_factor": "internal" }
  },
  {
    "index": 3,
    "name": "alsa_output.pci-0000_00_1b.0.analog-stereo.monitor",
    "description": "Monitor of Built-in Audio Analog Stereo",
    "driver": "module-alsa-card.c",
    "state": "IDLE",
    "volume": {
      "front-left":  { "value_percent": "100%" },
      "front-right": { "value_percent": "100%" }
    },
    "mute": false,
    "properties": { "device.icon_name": "audio-speakers" }
  }
]`

const sampleSinkInputs = `[
  {
    "index": 304,
    "sink": 0,
    "client": 217,
    "volume": {
      "front-left":  { "value_percent": "82%" },
      "front-right": { "value_percent": "82%" }
    },
    "mute": false,
    "corked": false,
    "properties": {
      "application.name": "Firefox",
      "application.process.binary": "firefox",
      "media.name": "navi radio — night drive mix"
    }
  },
  {
    "index": 311,
    "sink": 0,
    "client": 240,
    "volume": {
      "front-left":  { "value_percent": "45%" },
      "front-right": { "value_percent": "45%" }
    },
    "mute": true,
    "corked": true,
    "properties": {
      "application.name": "mpv",
      "application.process.binary": "mpv",
      "media.name": "lain — omnipresence (paused)"
    }
  }
]`

const sampleSourceOutputs = `[
  {
    "index": 45,
    "source": 2,
    "client": 260,
    "volume": {
      "front-left":  { "value_percent": "90%" },
      "front-right": { "value_percent": "90%" }
    },
    "mute": false,
    "corked": false,
    "properties": {
      "application.name": "fractal",
      "application.process.binary": "fractal",
      "media.name": "voice call"
    }
  }
]`

const sampleCards = `[
  {
    "index": 0,
    "name": "alsa_card.pci-0000_00_1b.0",
    "description": "Built-in Audio",
    "driver": "module-alsa-card.c",
    "active_profile": "output:analog-stereo+input:analog-stereo",
    "profiles": [
      { "name": "output:analog-stereo+input:analog-stereo", "description": "Analog Stereo Duplex", "available": "yes", "priority": 100 },
      { "name": "output:analog-stereo",                     "description": "Analog Stereo Output", "available": "yes", "priority": 90 },
      { "name": "off",                                      "description": "Off",                  "available": "yes", "priority": 0 }
    ]
  },
  {
    "index": 1,
    "name": "bluez_card.2C_41_A1_9B_4E_77",
    "description": "WH-1000XM4",
    "driver": "module-bluez5-device.c",
    "active_profile": "a2dp-sink",
    "profiles": [
      { "name": "a2dp-sink",        "description": "High Fidelity Playback (A2DP Sink)", "available": "yes", "priority": 40 },
      { "name": "headset-head-unit","description": "Headset Head Unit (HSP/HFP)",        "available": "no",  "priority": 30 },
      { "name": "off",              "description": "Off",                                "available": "yes", "priority": 0 }
    ]
  }
]`
