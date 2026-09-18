// navi-audio backend: PulseAudio control through pactl(1).
//
// The target stack is PulseAudio 17.0 (pulseaudio-utils), guaranteed on a
// navi install. Every command here follows the Debian trixie pactl(1) man
// page; JSON shapes follow the backend spec. Keys flagged VERIFY ON
// HARDWARE in that spec are parsed tolerantly: the parsers try several
// key spellings and never hard-fail on unexpected shapes, so a new field
// layout degrades to "unknown" instead of crashing the mod.
//
// Rule: real shapes, never hand-made mocks — update these parsers from a
// bench capture (`pactl -f json list …`) before trusting a new shape.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── data model ─────────────────────────────────────────────────────────

// AudioNode is a sink (output device) or source (input device).
type AudioNode struct {
	Index       int
	Name        string
	Description string
	VolumePct   float64 // max across channels; <0 when unknown
	Mute        bool
	MuteKnown   bool
	State       string // RUNNING / IDLE / SUSPENDED
	ActivePort  string
	Ports       []AudioPort
	IconName    string
}

// AudioPort is one physical/logical port on a device.
type AudioPort struct {
	Name        string
	Description string
	Available   string // "yes" / "no" / "unknown"
}

// Stream is a live playback (sink-input) or recording (source-output)
// stream. Streams are addressed by numeric index only — they have no
// stable name.
type Stream struct {
	Index     int
	Device    int // sink or source index the stream is routed to
	VolumePct float64
	Mute      bool
	Active    bool // audio flowing: RUNNING and not corked
	AppName   string
	Binary    string
	MediaName string
}

// CardProfile is one configuration profile on a card.
type CardProfile struct {
	Name        string
	Description string
	Available   string
}

// Card is an audio card (ALSA, Bluetooth, …) for the Configuration tab.
type Card struct {
	Index         int
	Name          string
	Description   string
	Profiles      []CardProfile
	ActiveProfile string
	Driver        string
}

// ── tolerant field extraction ──────────────────────────────────────────

// strField pulls a string out of a loosely-typed object, trying each key
// spelling in order. Accepts JSON strings and numbers.
func strField(o map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw, ok := o[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		var n float64
		if err := json.Unmarshal(raw, &n); err == nil {
			return strconv.FormatFloat(n, 'f', -1, 64)
		}
	}
	return ""
}

// intField is strField coerced to int (0 when absent/unparseable).
func intField(o map[string]json.RawMessage, keys ...string) int {
	n, _ := strconv.Atoi(strField(o, keys...))
	return n
}

// parseBool coerces a value that should be boolean. pactl JSON uses real
// booleans; the string forms are a fallback for older/other outputs.
func parseBool(raw json.RawMessage) (bool, bool) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "yes", "true", "1", "on":
			return true, true
		case "no", "false", "0", "off":
			return false, true
		}
	}
	return false, false
}

// availString coerces port/profile "available" to its canonical string.
// JSON may carry "yes"/"no"/"unknown" or a boolean.
func availString(raw json.RawMessage) string {
	if raw == nil {
		return "unknown"
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.ToLower(strings.TrimSpace(s))
	}
	if b, ok := parseBool(raw); ok {
		if b {
			return "yes"
		}
		return "no"
	}
	return "unknown"
}

// parseVolume reduces pactl's per-channel volume map to a single
// percentage: the max across channels. The per-channel object shape is
// VERIFY ON HARDWARE — we accept {"value_percent": "74%"} (documented)
// and fall back to the raw {"value": 49152} 0..65536 scale, ignoring any
// other sibling keys.
func parseVolume(raw json.RawMessage) (float64, bool) {
	var chans map[string]json.RawMessage
	if err := json.Unmarshal(raw, &chans); err != nil {
		return -1, false
	}
	best, ok := -1.0, false
	for _, ch := range chans {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(ch, &fields); err != nil {
			continue
		}
		if p, good := channelPct(fields); good && p > best {
			best, ok = p, true
		}
	}
	return best, ok
}

// channelPct reads one channel's volume object.
func channelPct(fields map[string]json.RawMessage) (float64, bool) {
	if raw, found := fields["value_percent"]; found {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			s = strings.TrimSuffix(strings.TrimSpace(s), "%")
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f, true
			}
		}
		var n float64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n, true
		}
	}
	if raw, found := fields["value"]; found {
		var n float64
		if err := json.Unmarshal(raw, &n); err == nil {
			return n / 65536 * 100, true
		}
	}
	return -1, false
}

// parsePorts reads a device's port array tolerantly.
func parsePorts(raw json.RawMessage) []AudioPort {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	ports := make([]AudioPort, 0, len(arr))
	for _, p := range arr {
		ports = append(ports, AudioPort{
			Name:        strField(p, "name"),
			Description: strField(p, "description"),
			Available:   availString(p["available"]),
		})
	}
	return ports
}

// ── object constructors ────────────────────────────────────────────────

func nodeFromMap(o map[string]json.RawMessage) AudioNode {
	n := AudioNode{
		Index:       intField(o, "index"),
		Name:        strField(o, "name"),
		Description: strField(o, "description"),
		State:       strField(o, "state"),
		// "active_port" is the documented spelling; "active-port" is a
		// hedge — VERIFY ON HARDWARE.
		ActivePort: strField(o, "active_port", "active-port"),
		VolumePct:  -1,
	}
	if n.Description == "" {
		n.Description = n.Name
	}
	if raw, ok := o["volume"]; ok {
		if p, good := parseVolume(raw); good {
			n.VolumePct = p
		}
	}
	if raw, ok := o["mute"]; ok {
		n.Mute, n.MuteKnown = parseBool(raw)
	}
	if raw, ok := o["ports"]; ok {
		n.Ports = parsePorts(raw)
	}
	if raw, ok := o["properties"]; ok {
		var props map[string]json.RawMessage
		if err := json.Unmarshal(raw, &props); err == nil {
			n.IconName = strField(props, "device.icon_name")
		}
	}
	return n
}

func streamFromMap(o map[string]json.RawMessage, playback bool) Stream {
	s := Stream{
		Index:     intField(o, "index"),
		VolumePct: -1,
		Active:    true,
	}
	if playback {
		s.Device = intField(o, "sink")
	} else {
		s.Device = intField(o, "source")
	}
	if raw, ok := o["volume"]; ok {
		if p, good := parseVolume(raw); good {
			s.VolumePct = p
		}
	}
	if raw, ok := o["mute"]; ok {
		s.Mute, _ = parseBool(raw)
	}
	// "corked" is VERIFY ON HARDWARE; "state" is a second opinion.
	if raw, ok := o["corked"]; ok {
		if corked, good := parseBool(raw); good {
			s.Active = !corked
		}
	}
	switch strings.ToUpper(strField(o, "state")) {
	case "RUNNING":
		s.Active = true
	case "CORKED", "DRAINED", "PAUSED", "IDLE", "SUSPENDED":
		s.Active = false
	}
	if raw, ok := o["properties"]; ok {
		var props map[string]json.RawMessage
		if err := json.Unmarshal(raw, &props); err == nil {
			s.AppName = strField(props, "application.name")
			s.Binary = strField(props, "application.process.binary")
			s.MediaName = strField(props, "media.name", "media.title")
			if s.AppName == "" {
				s.AppName = s.Binary
			}
		}
	}
	if s.AppName == "" {
		s.AppName = fmt.Sprintf("stream #%d", s.Index)
	}
	return s
}

func cardFromMap(o map[string]json.RawMessage) Card {
	c := Card{
		Index:       intField(o, "index"),
		Name:        strField(o, "name"),
		Description: strField(o, "description"),
		Driver:      strField(o, "driver"),
		// Key name is VERIFY ON HARDWARE; try both spellings.
		ActiveProfile: strField(o, "active_profile", "active-profile"),
	}
	if c.Description == "" {
		c.Description = c.Name
	}
	if raw, ok := o["profiles"]; ok {
		var arr []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil {
			for _, p := range arr {
				c.Profiles = append(c.Profiles, CardProfile{
					Name:        strField(p, "name"),
					Description: strField(p, "description"),
					Available:   availString(p["available"]),
				})
			}
		}
	}
	return c
}

// ── pactl plumbing ─────────────────────────────────────────────────────

// pactl runs pactl with a context and returns stdout. Stderr is folded
// into the error so the UI can show what the daemon complained about.
func pactl(ctx context.Context, args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "pactl", args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), fmt.Errorf("pactl %s: %s", strings.Join(args, " "), msg)
	}
	return out.Bytes(), nil
}

// pactlJSON runs `pactl -f json list <noun>` and decodes the bare array
// of loosely-typed objects.
func pactlJSON(ctx context.Context, noun string) ([]map[string]json.RawMessage, error) {
	raw, err := pactl(ctx, "-f", "json", "list", noun)
	if err != nil {
		return nil, err
	}
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, fmt.Errorf("pactl list %s: bad JSON: %w", noun, err)
	}
	return objs, nil
}

// audioSnapshot is one full refresh of the audio world.
type audioSnapshot struct {
	sinks     []AudioNode
	sources   []AudioNode
	playback  []Stream
	recording []Stream
	cards     []Card

	defaultSink   string
	defaultSource string
}

// fetchSnapshot pulls every list the UI needs. Partial failures still
// return whatever parsed; the first error is reported alongside so the
// UI can degrade (e.g. daemon down) instead of dying.
func fetchSnapshot(ctx context.Context) (audioSnapshot, error) {
	var snap audioSnapshot
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil && !errors.Is(err, context.Canceled) {
			firstErr = err
		}
	}

	if objs, err := pactlJSON(ctx, "sinks"); err != nil {
		keep(err)
	} else {
		for _, o := range objs {
			snap.sinks = append(snap.sinks, nodeFromMap(o))
		}
	}
	if objs, err := pactlJSON(ctx, "sources"); err != nil {
		keep(err)
	} else {
		for _, o := range objs {
			snap.sources = append(snap.sources, nodeFromMap(o))
		}
	}
	if objs, err := pactlJSON(ctx, "sink-inputs"); err != nil {
		keep(err)
	} else {
		for _, o := range objs {
			snap.playback = append(snap.playback, streamFromMap(o, true))
		}
	}
	if objs, err := pactlJSON(ctx, "source-outputs"); err != nil {
		keep(err)
	} else {
		for _, o := range objs {
			snap.recording = append(snap.recording, streamFromMap(o, false))
		}
	}
	if objs, err := pactlJSON(ctx, "cards"); err != nil {
		keep(err)
	} else {
		for _, o := range objs {
			snap.cards = append(snap.cards, cardFromMap(o))
		}
	}
	if raw, err := pactl(ctx, "get-default-sink"); err == nil {
		snap.defaultSink = strings.TrimSpace(string(raw))
	} else {
		keep(err)
	}
	if raw, err := pactl(ctx, "get-default-source"); err == nil {
		snap.defaultSource = strings.TrimSpace(string(raw))
	} else {
		keep(err)
	}
	return snap, firstErr
}

// ── control operations ─────────────────────────────────────────────────
// Each returns a tea.Cmd reporting an opMsg. Volumes are capped at 100%
// (pavucontrol parity). Streams are addressed by numeric index only.

// volTarget computes the pactl volume spec for a ±step move. When the
// current volume is unknown we fall back to a relative spec.
func volTarget(cur float64, step int) string {
	if cur < 0 {
		if step > 0 {
			return "+5%"
		}
		return "-5%"
	}
	t := cur + float64(step)
	if t < 0 {
		t = 0
	}
	if t > 100 {
		t = 100
	}
	return fmt.Sprintf("%.0f%%", t)
}

// volNote is the status-line text for a volume move.
func volNote(cur float64, step int, what string) string {
	if cur < 0 {
		if step > 0 {
			return what + " volume up"
		}
		return what + " volume down"
	}
	t := cur + float64(step)
	if t < 0 {
		t = 0
	}
	if t > 100 {
		t = 100
	}
	return fmt.Sprintf("%s volume %.0f%%", what, t)
}

// opCtx is the short-lived context for one control command.
func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 8*time.Second)
}

func setVolumeCmd(kind string, id int, spec, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		var cmd string
		switch kind {
		case "sink":
			cmd = "set-sink-volume"
		case "source":
			cmd = "set-source-volume"
		case "sink-input":
			cmd = "set-sink-input-volume"
		case "source-output":
			cmd = "set-source-output-volume"
		}
		if _, err := pactl(ctx, cmd, strconv.Itoa(id), spec); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

func muteToggleCmd(kind string, id int, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		var cmd string
		switch kind {
		case "sink":
			cmd = "set-sink-mute"
		case "source":
			cmd = "set-source-mute"
		case "sink-input":
			cmd = "set-sink-input-mute"
		case "source-output":
			cmd = "set-source-output-mute"
		}
		if _, err := pactl(ctx, cmd, strconv.Itoa(id), "toggle"); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

func setDefaultCmd(isSink bool, name, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		setCmd := "set-default-source"
		if isSink {
			setCmd = "set-default-sink"
		}
		if _, err := pactl(ctx, setCmd, name); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

func moveStreamCmd(playback bool, id, target int, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		cmd := "move-source-output"
		if playback {
			cmd = "move-sink-input"
		}
		if _, err := pactl(ctx, cmd, strconv.Itoa(id), strconv.Itoa(target)); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

func setPortCmd(isSink bool, id int, port, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		cmd := "set-source-port"
		if isSink {
			cmd = "set-sink-port"
		}
		if _, err := pactl(ctx, cmd, strconv.Itoa(id), port); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

func setProfileCmd(card int, profile, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := opCtx()
		defer cancel()
		if _, err := pactl(ctx, "set-card-profile", strconv.Itoa(card), profile); err != nil {
			return opMsg{err: err}
		}
		return opMsg{note: note}
	}
}

// ── live updates: pactl subscribe ──────────────────────────────────────

// subEventRe parses `pactl subscribe` lines:
//
//	Event 'change' on sink #0
var subEventRe = regexp.MustCompile(`^Event '(\w+)' on ([\w-]+) #(\d+)`)

// subRefreshWanted reports whether a subscribe line should trigger a UI
// re-list. Client/module churn is ignored per the backend spec; server
// events matter because default-device moves surface there.
func subRefreshWanted(line string) bool {
	m := subEventRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	switch m[2] {
	case "sink", "source", "sink-input", "source-output", "card", "server":
		return true
	}
	return false
}

// runSubscriber forwards `pactl subscribe` lines until ctx is cancelled
// or the stream ends. One persistent subprocess for the mod's lifetime —
// never a per-tick spawn.
func runSubscriber(ctx context.Context, lines chan<- string) error {
	cmd := exec.CommandContext(ctx, "pactl", "subscribe")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case lines <- sc.Text():
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("pactl subscribe exited")
}
