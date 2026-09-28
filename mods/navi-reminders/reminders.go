// reminders.go — the navi-reminders backend.
//
// One reminder = one systemd user timer+service pair. The registry at
// ~/.config/navi-reminders/reminders.json is the source of truth for
// content; unit files carry only the id and the OnCalendar= schedule.
// Content edits never need daemon-reload — only reschedules touch units.
//
// The reliability story: Persistent=true on every timer (sleep/wake fires
// once on wake), loginctl enable-linger at deploy (timers run from boot),
// and the fire helper's bus-wait + missed-marking (boot-before-login
// becomes a first-class MISSED state, never a silent drop).
package reminders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Repeat kinds. systemd-native: repeats are real OnCalendar= rules, so
// the scheduler — not us — owns the cadence.
const (
	RepeatOnce     = "once"
	RepeatDaily    = "daily"
	RepeatWeekdays = "weekdays"
	RepeatWeekly   = "weekly"
)

// Reminder is one entry in the registry.
type Reminder struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Calendar string    `json:"calendar"` // systemd OnCalendar= value
	Repeat   string    `json:"repeat"`
	At       time.Time `json:"at"` // next fire, local time
	Created  time.Time `json:"created"`
	Missed   bool      `json:"missed"`
	MissedAt time.Time `json:"missed_at,omitempty"`
	Done     bool      `json:"done"`
}

// Registry is the on-disk shape of reminders.json.
type Registry struct {
	Reminders []Reminder `json:"reminders"`
}

// --- paths ---

func configHome() string {
	if h, err := os.UserConfigDir(); err == nil && h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

// RemindersDir is ~/.config/navi-reminders.
func RemindersDir() string { return filepath.Join(configHome(), "navi-reminders") }

// RegistryPath is ~/.config/navi-reminders/reminders.json.
func RegistryPath() string { return filepath.Join(RemindersDir(), "reminders.json") }

// UnitsDir is ~/.config/systemd/user — where the timer+service pairs live.
func UnitsDir() string { return filepath.Join(configHome(), "systemd", "user") }

func unitBase(id string) string { return "navi-reminder-" + id }

// --- registry ---

// LoadRegistry reads the registry, returning an empty one when the file
// doesn't exist yet.
func LoadRegistry() (*Registry, error) {
	return loadRegistryAt(RegistryPath())
}

func loadRegistryAt(path string) (*Registry, error) {
	reg := &Registry{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return reg, nil
}

// SaveRegistry writes the registry atomically (0600 — it's the user's own
// data, but there's no reason to be loose with it).
func SaveRegistry(reg *Registry) error {
	return saveRegistryAt(RegistryPath(), reg)
}

func saveRegistryAt(path string, reg *Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find returns the reminder with id, or nil.
func (r *Registry) Find(id string) *Reminder {
	for i := range r.Reminders {
		if r.Reminders[i].ID == id {
			return &r.Reminders[i]
		}
	}
	return nil
}

// Remove drops the reminder with id. Reports whether one was removed.
func (r *Registry) Remove(id string) bool {
	for i := range r.Reminders {
		if r.Reminders[i].ID == id {
			r.Reminders = append(r.Reminders[:i], r.Reminders[i+1:]...)
			return true
		}
	}
	return false
}

// --- ids and schedules ---

// newID is timestamp + random hex: sortable, unique, unit-name safe.
func newID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// OnCalendarFor renders the systemd OnCalendar= rule for a fire time and
// repeat kind.
func OnCalendarFor(at time.Time, repeat string) string {
	t := at.Format("15:04:05")
	switch repeat {
	case RepeatDaily:
		return "*-*-* " + t
	case RepeatWeekdays:
		return "Mon..Fri *-*-* " + t
	case RepeatWeekly:
		return at.Format("Mon") + " *-*-* " + t
	default: // RepeatOnce
		return at.Format("2006-01-02 15:04:05")
	}
}

// NextOccurrence returns the next fire strictly after `after` for a
// repeating reminder. `at` carries the wall-clock time (and, for weekly,
// the weekday). Once-reminders just return their single fire time.
func NextOccurrence(repeat string, at, after time.Time) time.Time {
	loc := at.Location()
	cand := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(),
			at.Hour(), at.Minute(), at.Second(), 0, loc)
	}
	switch repeat {
	case RepeatDaily:
		d := cand(after)
		if !d.After(after) {
			d = d.AddDate(0, 0, 1)
		}
		return d
	case RepeatWeekdays:
		d := cand(after)
		for i := 0; i < 8; i++ {
			if d.After(after) && d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
				return d
			}
			d = d.AddDate(0, 0, 1)
		}
		return d
	case RepeatWeekly:
		d := cand(after)
		for !d.After(after) || d.Weekday() != at.Weekday() {
			d = d.AddDate(0, 0, 1)
		}
		return d
	default:
		return at
	}
}

// --- unit files ---

// fireBin is the helper the service units exec. Overridable for dev via
// NAVI_REMINDER_FIRE_BIN; production deploys to /usr/bin.
func fireBin() string {
	if p := os.Getenv("NAVI_REMINDER_FIRE_BIN"); p != "" {
		return p
	}
	return "/usr/bin/navi-reminder-fire"
}

// TimerUnit renders the .timer file for a reminder.
func TimerUnit(r Reminder) string {
	return fmt.Sprintf(`[Unit]
Description=navi reminder: %s

[Timer]
OnCalendar=%s
Persistent=true
AccuracySec=1min
Unit=%s.service

[Install]
WantedBy=timers.target
`, r.Title, r.Calendar, unitBase(r.ID))
}

// ServiceUnit renders the .service file for a reminder. The unit carries
// only the id — content lives in the registry.
func ServiceUnit(r Reminder) string {
	return fmt.Sprintf(`[Unit]
Description=navi reminder fire: %s

[Service]
Type=oneshot
ExecStart=%s %s
`, r.Title, fireBin(), r.ID)
}

// WriteUnits writes (or rewrites) the timer+service pair for r.
func WriteUnits(r Reminder) error {
	return writeUnitsAt(UnitsDir(), r)
}

func writeUnitsAt(dir string, r Reminder) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Join(dir, unitBase(r.ID))
	if err := os.WriteFile(base+".timer", []byte(TimerUnit(r)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(base+".service", []byte(ServiceUnit(r)), 0o644)
}

// RemoveUnits deletes the timer+service pair for r. Missing files are fine.
func RemoveUnits(r Reminder) error {
	return removeUnitsAt(UnitsDir(), r)
}

func removeUnitsAt(dir string, r Reminder) error {
	base := filepath.Join(dir, unitBase(r.ID))
	var first error
	for _, ext := range []string{".timer", ".service"} {
		if err := os.Remove(base + ext); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

// --- systemctl wrappers ---

func systemctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w (%s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DaemonReload tells the user manager to reread unit files.
func DaemonReload() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return systemctl(ctx, "daemon-reload")
}

// EnableTimer enables and starts the reminder's timer.
func EnableTimer(r Reminder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return systemctl(ctx, "enable", "--now", unitBase(r.ID)+".timer")
}

// DisableTimer stops and disables the reminder's timer.
func DisableTimer(r Reminder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return systemctl(ctx, "disable", "--now", unitBase(r.ID)+".timer")
}

// RestartTimer restarts the timer (used after rescheduling).
func RestartTimer(r Reminder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return systemctl(ctx, "restart", unitBase(r.ID)+".timer")
}

// TimerActive reports whether the reminder's timer is currently active.
func TimerActive(r Reminder) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return systemctl(ctx, "is-active", "--quiet", unitBase(r.ID)+".timer") == nil
}

// UserManagerAlive reports whether `systemctl --user` can talk to the
// user's systemd at all — the TUI's status dot.
func UserManagerAlive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return systemctl(ctx, "is-system-running") == nil ||
		systemctl(ctx, "show", "--property=Version") == nil
}

// --- lifecycle ---

// AddReminder creates the registry entry, writes the units, reloads the
// daemon, and enables the timer.
func AddReminder(title string, at time.Time, repeat string) (*Reminder, error) {
	now := time.Now()
	r := Reminder{
		ID:       newID(now),
		Title:    title,
		Calendar: OnCalendarFor(at, repeat),
		Repeat:   repeat,
		At:       at,
		Created:  now,
	}
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	reg.Reminders = append(reg.Reminders, r)
	if err := SaveRegistry(reg); err != nil {
		return nil, err
	}
	if err := WriteUnits(r); err != nil {
		return nil, err
	}
	if err := DaemonReload(); err != nil {
		return nil, err
	}
	if err := EnableTimer(r); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteReminder stops the timer, removes the units, and drops the
// registry entry.
func DeleteReminder(id string) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil {
		return fmt.Errorf("no reminder %q", id)
	}
	_ = DisableTimer(*r) // best effort: the timer may already be gone
	if err := RemoveUnits(*r); err != nil {
		return err
	}
	reg.Remove(id)
	if err := SaveRegistry(reg); err != nil {
		return err
	}
	return DaemonReload()
}

// SnoozeReminder pushes the reminder d into the future as a one-shot,
// keeping the same id. Used by the TUI snooze menu and the notification
// "Snooze 10m" action.
func SnoozeReminder(id string, d time.Duration) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil {
		return fmt.Errorf("no reminder %q", id)
	}
	at := time.Now().Add(d).Truncate(time.Second)
	r.At = at
	r.Repeat = RepeatOnce
	r.Calendar = OnCalendarFor(at, RepeatOnce)
	r.Missed = false
	r.MissedAt = time.Time{}
	r.Done = false
	if err := SaveRegistry(reg); err != nil {
		return err
	}
	if err := WriteUnits(*r); err != nil {
		return err
	}
	if err := DaemonReload(); err != nil {
		return err
	}
	return RestartTimer(*r)
}

// MarkDone flags the reminder done and tears down its timer+units —
// a done reminder will never fire again.
func MarkDone(id string) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil {
		return fmt.Errorf("no reminder %q", id)
	}
	r.Done = true
	r.Missed = false
	_ = DisableTimer(*r) // best effort: the timer may already be gone
	_ = RemoveUnits(*r)
	if err := SaveRegistry(reg); err != nil {
		return err
	}
	return DaemonReload()
}

// MarkMissed flags the reminder missed — the fire helper's last resort.
// Never a silent drop: the TUI surfaces these.
func MarkMissed(id string) error {
	return updateReminder(id, func(r *Reminder) {
		r.Missed = true
		r.MissedAt = time.Now()
	}, false)
}

// ClearMissed clears the missed flag (user re-fired or re-armed it).
func ClearMissed(id string) error {
	return updateReminder(id, func(r *Reminder) {
		r.Missed = false
		r.MissedAt = time.Time{}
	}, false)
}

func updateReminder(id string, fn func(*Reminder), stopTimer bool) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil {
		return fmt.Errorf("no reminder %q", id)
	}
	fn(r)
	if stopTimer {
		_ = DisableTimer(*r)
	}
	return SaveRegistry(reg)
}

// AdvanceRepeat moves a repeating reminder's At to its next occurrence
// after now, keeping the registry honest between systemd firings.
func AdvanceRepeat(id string) error {
	return updateReminder(id, func(r *Reminder) {
		r.At = NextOccurrence(r.Repeat, r.At, time.Now())
	}, false)
}

// --- notification plumbing ---

// busProbeTimeout is how long the fire helper waits for a notification
// daemon before declaring the reminder missed (boot-before-login).
const busProbeTimeout = 60 * time.Second

// notifierAlive reports whether org.freedesktop.Notifications is reachable
// on the session bus AND a sender (dunstify/notify-send) is on PATH.
func notifierAlive(ctx context.Context) bool {
	if _, err := exec.LookPath("dunstify"); err != nil {
		if _, err := exec.LookPath("notify-send"); err != nil {
			return false
		}
	}
	probe := exec.CommandContext(ctx, "busctl", "--user", "--no-pager",
		"call", "org.freedesktop.Notifications",
		"/org/freedesktop/Notifications",
		"org.freedesktop.Notifications", "GetServerInformation")
	return probe.Run() == nil
}

// WaitForNotifier polls for a notification daemon until timeout. True when
// one appears — the boot/login window.
func WaitForNotifier(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		alive := notifierAlive(ctx)
		cancel()
		if alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Second)
	}
}

// notifyAction is what the user picked on the notification.
type notifyAction string

const (
	actionNone   notifyAction = ""
	actionDone   notifyAction = "done"
	actionSnooze notifyAction = "snooze"
)

// dunstifyHasWait reports whether this dunstify supports --wait (action
// reporting). Ancient dunstify just sends; we degrade to no actions.
func dunstifyHasWait() bool {
	out, err := exec.Command("dunstify", "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--wait")
}

// Notify sends the reminder notification and blocks for the user's action
// (Done / Snooze 10m). Returns the action, or actionNone on dismiss,
// timeout, or when actions aren't supported. `wait` caps how long we
// block — the systemd oneshot shouldn't hang forever on an AFK desktop.
func Notify(r Reminder, wait time.Duration) (notifyAction, error) {
	title := "⏰ " + r.Title
	body := "navi reminder"
	if r.Repeat != RepeatOnce {
		body = "navi reminder · repeats " + r.Repeat
	}
	if _, err := exec.LookPath("dunstify"); err == nil {
		args := []string{"--appname=navi-reminders", "--urgency=normal", title, body}
		useWait := dunstifyHasWait()
		if useWait {
			args = append([]string{"--wait",
				"--action=done,Done", "--action=snooze,Snooze 10m"}, args...)
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		out, err := exec.CommandContext(ctx, "dunstify", args...).Output()
		if err == nil && useWait {
			switch notifyAction(strings.TrimSpace(string(out))) {
			case actionDone:
				return actionDone, nil
			case actionSnooze:
				return actionSnooze, nil
			}
			return actionNone, nil
		}
		if err == nil {
			return actionNone, nil
		}
		// fall through to notify-send
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "notify-send",
			"--app-name=navi-reminders", title, body).Run(); err == nil {
			return actionNone, nil
		}
	}
	return actionNone, fmt.Errorf("no working notification sender")
}

// NotifyNow sends the reminder notification immediately (used by the TUI
// re-fire path, where a session is guaranteed because the user is looking
// at the TUI).
func NotifyNow(id string) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil {
		return fmt.Errorf("no reminder %q", id)
	}
	act, err := Notify(*r, 90*time.Second)
	if err != nil {
		return err
	}
	switch act {
	case actionSnooze:
		return SnoozeReminder(id, 10*time.Minute)
	case actionDone:
		return MarkDone(id)
	}
	return ClearMissed(id)
}

// FireReminder is the whole fire-helper flow for one id:
//
//  1. load the entry; missing or done → nothing to do, exit 0.
//  2. wait up to 60s for a notification daemon; none → mark MISSED, exit 0.
//  3. notify with Done/Snooze actions; act on the choice.
//  4. repeats advance their At so the TUI stays honest.
//
// Missed is a first-class state, never a silent drop.
func FireReminder(id string) error {
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	r := reg.Find(id)
	if r == nil || r.Done {
		return nil
	}
	if !WaitForNotifier(busProbeTimeout) {
		return MarkMissed(id)
	}
	act, err := Notify(*r, 120*time.Second)
	if err != nil {
		// A daemon answered the probe but the send still failed —
		// treat it as undelivered, not as fired.
		return MarkMissed(id)
	}
	switch act {
	case actionSnooze:
		return SnoozeReminder(id, 10*time.Minute)
	case actionDone:
		if r.Repeat == RepeatOnce {
			return MarkDone(id)
		}
		// repeats: "done" dismisses this occurrence, the cadence lives on
		return AdvanceRepeat(id)
	default:
		if r.Repeat != RepeatOnce {
			return AdvanceRepeat(id)
		}
		return nil
	}
}

// Relative renders "in 2h", "tomorrow 09:00", "past due" for the TUI.
func Relative(at, now time.Time) string {
	if at.Before(now) {
		return "past due"
	}
	d := at.Sub(now)
	switch {
	case d < time.Hour:
		m := int(d.Minutes())
		if m < 1 {
			return "any moment"
		}
		return fmt.Sprintf("in %dm", m)
	case d < 48*time.Hour && at.Day() == now.Day():
		return fmt.Sprintf("in %dh", int(d.Hours()))
	case d < 48*time.Hour:
		return "tomorrow " + at.Format("15:04")
	case d < 7*24*time.Hour:
		return at.Format("Mon 15:04")
	default:
		return at.Format("02 Jan 15:04")
	}
}
