// quickadd.go — the navi-calendar quick-add parser.
//
// Pure, stdlib-only, no network: turns one line like
// "dinner with ryoko friday 7pm" into an event. The grammar it accepts
// is exactly the one in the quick-add spec; anything else is a refusal
// with a user-facing message. Tests pin `now` by passing it in.
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Refusal messages — user-facing, pinned by tests.
const (
	qaErrTime  = `i need a time — try "7pm" or "19:00"`
	qaErrDate  = `no date found — try "today", "friday", or "28 sep"`
	qaErrTitle = `give the event a title`
	qaErrJunk  = `couldn't parse that — press e for the manual form`
)

var (
	// 24-hour bare: "19:00", "7:00" (bare "7:00" means 07:00).
	qaTime24 = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)
	// 12-hour with suffix: "7pm", "7:30pm", "7a", "7:30a".
	qaTime12 = regexp.MustCompile(`^(\d{1,2})(?::([0-5]\d))?(am|pm|a|p)$`)
	qaISO    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	qaDayNum = regexp.MustCompile(`^([1-9]|[12][0-9]|3[01])$`)
)

var qaWeekdays = map[string]time.Weekday{
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
	"sunday": time.Sunday, "sun": time.Sunday,
}

var qaMonths = map[string]time.Month{
	"january": time.January, "jan": time.January,
	"february": time.February, "feb": time.February,
	"march": time.March, "mar": time.March,
	"april": time.April, "apr": time.April,
	"may":  time.May,
	"june": time.June, "jun": time.June,
	"july": time.July, "jul": time.July,
	"august": time.August, "aug": time.August,
	"september": time.September, "sep": time.September, "sept": time.September,
	"october": time.October, "oct": time.October,
	"november": time.November, "nov": time.November,
	"december": time.December, "dec": time.December,
}

// parseTimeToken parses one time token into 24-hour hh:mm.
func parseTimeToken(tok string) (hh, mm int, ok bool) {
	t := strings.ToLower(tok)
	switch t {
	case "noon":
		return 12, 0, true
	case "midnight":
		return 0, 0, true
	}
	if m := qaTime24.FindStringSubmatch(t); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		return hh, mm, true
	}
	if m := qaTime12.FindStringSubmatch(t); m != nil {
		h, _ := strconv.Atoi(m[1])
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		mm := 0
		if m[2] != "" {
			mm, _ = strconv.Atoi(m[2])
		}
		switch m[3] {
		case "am", "a":
			return h % 12, mm, true
		default: // pm, p
			return h%12 + 12, mm, true
		}
	}
	return 0, 0, false
}

func qaMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// resolveMonthDay turns "28 sep" into a date: this year, or next year if
// the day already passed. Rejects impossible dates (31 feb, 29 feb on a
// common year).
func resolveMonthDay(now time.Time, day int, mo time.Month) (time.Time, bool) {
	loc := now.Location()
	try := func(y int) (time.Time, bool) {
		d := time.Date(y, mo, day, 0, 0, 0, 0, loc)
		if d.Month() != mo || d.Day() != day {
			return time.Time{}, false
		}
		return d, true
	}
	d, ok := try(now.Year())
	if !ok {
		return time.Time{}, false
	}
	if d.Before(qaMidnight(now)) {
		d, ok = try(now.Year() + 1)
		if !ok {
			return time.Time{}, false
		}
	}
	return d, true
}

type qaDateStatus int

const (
	qaDateNone qaDateStatus = iota // no date-shaped tokens at all
	qaDateBad                      // date-shaped, but invalid (e.g. 31 feb)
	qaDateOK
)

// parseDateTokens matches the date part greedily from the right end of the
// token list and returns the resolved day plus the leftover title tokens.
func parseDateTokens(tokens []string, now time.Time, hh, mm int) (time.Time, []string, qaDateStatus) {
	n := len(tokens)
	lower := func(i int) string { return strings.ToLower(tokens[i]) }

	// two-token forms first
	if n >= 2 {
		a, b := lower(n-2), lower(n-1)
		if a == "next" {
			if wd, ok := qaWeekdays[b]; ok {
				// strictly the weekday of next week (never today):
				// next Monday plus the weekday's Monday-based offset
				// (weeks start Monday, like the calendar grid).
				toMon := (8 - int(now.Weekday())) % 7
				if toMon == 0 {
					toMon = 7
				}
				delta := toMon + (int(wd)+6)%7
				return qaMidnight(now).AddDate(0, 0, delta), tokens[:n-2], qaDateOK
			}
		}
		if day, ok := a, qaDayNum.MatchString(a); ok {
			if mo, ok := qaMonths[b]; ok {
				dn, _ := strconv.Atoi(day)
				if d, ok := resolveMonthDay(now, dn, mo); ok {
					return d, tokens[:n-2], qaDateOK
				}
				return time.Time{}, nil, qaDateBad
			}
		}
		if mo, ok := qaMonths[a]; ok {
			if day, ok := b, qaDayNum.MatchString(b); ok {
				dn, _ := strconv.Atoi(day)
				if d, ok := resolveMonthDay(now, dn, mo); ok {
					return d, tokens[:n-2], qaDateOK
				}
				return time.Time{}, nil, qaDateBad
			}
		}
	}

	if n >= 1 {
		t := lower(n - 1)
		switch t {
		case "today", "tonight":
			return qaMidnight(now), tokens[:n-1], qaDateOK
		case "tomorrow", "tmr":
			return qaMidnight(now).AddDate(0, 0, 1), tokens[:n-1], qaDateOK
		}
		if wd, ok := qaWeekdays[t]; ok {
			delta := (int(wd) - int(now.Weekday()) + 7) % 7
			d := qaMidnight(now).AddDate(0, 0, delta)
			if delta == 0 {
				// Friday-today edge: it's today only if the parsed
				// time is still later today, otherwise next week.
				at := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, now.Location())
				if !at.After(now) {
					d = d.AddDate(0, 0, 7)
				}
			}
			return d, tokens[:n-1], qaDateOK
		}
		if qaISO.MatchString(t) {
			if d, err := time.ParseInLocation("2006-01-02", t, now.Location()); err == nil {
				return qaMidnight(d), tokens[:n-1], qaDateOK
			}
			return time.Time{}, nil, qaDateBad
		}
	}
	return time.Time{}, nil, qaDateNone
}

// tokensHaveDate reports whether any token looks date-shaped — used to tell
// "i need a time" apart from "couldn't parse that".
func tokensHaveDate(tokens []string) bool {
	for _, tok := range tokens {
		t := strings.ToLower(tok)
		if _, ok := qaWeekdays[t]; ok {
			return true
		}
		if _, ok := qaMonths[t]; ok {
			return true
		}
		switch t {
		case "today", "tonight", "tomorrow", "tmr", "next":
			return true
		}
		if qaISO.MatchString(t) || qaDayNum.MatchString(t) {
			return true
		}
	}
	return false
}

// parseQuickAdd turns one quick-add line into an event. Pure function:
// `now` is passed in so tests can pin it. The returned event has no ID —
// that's assigned on save.
func parseQuickAdd(input string, now time.Time) (event, error) {
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return event{}, errors.New(qaErrJunk)
	}

	// time part: the trailing token
	hh, mm, ok := parseTimeToken(tokens[len(tokens)-1])
	rest := tokens[:len(tokens)-1]
	if !ok {
		if !tokensHaveDate(tokens) {
			return event{}, errors.New(qaErrJunk)
		}
		return event{}, errors.New(qaErrTime)
	}

	// date part: token(s) before the time, matched from the right
	day, leftover, status := parseDateTokens(rest, now, hh, mm)
	switch status {
	case qaDateNone:
		return event{}, errors.New(qaErrDate)
	case qaDateBad:
		return event{}, errors.New(qaErrJunk)
	}

	title := strings.TrimSpace(strings.Join(leftover, " "))
	if title == "" {
		return event{}, errors.New(qaErrTitle)
	}
	return event{
		Title: title,
		Date:  dateKey(day),
		Time:  fmt.Sprintf("%02d:%02d", hh, mm),
	}, nil
}

// quickIsPast reports whether the event's datetime is already behind `now`.
func quickIsPast(ev event, now time.Time) bool {
	d, err := time.ParseInLocation("2006-01-02", ev.Date, now.Location())
	if err != nil {
		return false
	}
	var hh, mm int
	if _, err := fmt.Sscanf(ev.Time, "%d:%d", &hh, &mm); err != nil {
		return false
	}
	at := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, now.Location())
	return at.Before(now)
}
