// alerts.go — NWS severe-weather alerts backend for navi-weather.
//
// api.weather.gov is free and keyless, but US-coverage only. Coordinates
// come from the wttr.in nearest_area the mod already fetches — no extra
// geocoding. Polite by design: 10-minute cache, no polling loops, one
// identifying User-Agent.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	theme "github.com/rav3ndust/navi-theme"
)

const (
	nwsAlertsURL = "https://api.weather.gov/alerts"
	nwsUserAgent = "navi-weather/1.0 (xvoidsx.org)"
	// alertsCacheTTL is a courtesy as much as an optimization: NWS
	// rate-limits aggressively, so we never ask more often than this.
	alertsCacheTTL = 10 * time.Minute
)

// nwsAlert is the slice of an api.weather.gov feature we actually show.
type nwsAlert struct {
	Event       string `json:"event"`
	Severity    string `json:"severity"` // Extreme/Severe/Moderate/Minor/Unknown
	Headline    string `json:"headline"`
	Description string `json:"description"`
	Instruction string `json:"instruction"`
	Effective   string `json:"effective"`
	Expires     string `json:"expires"`
	SenderName  string `json:"senderName"`
}

// alertsEnvelope is what we persist: the alerts, when we got them, and
// whether NWS told us this spot is outside its coverage area.
type alertsEnvelope struct {
	FetchedAt   time.Time  `json:"fetched_at"`
	Unsupported bool       `json:"unsupported"`
	Alerts      []nwsAlert `json:"alerts"`
}

// alertsResult is what the fetch command hands back to the model.
type alertsResult struct {
	Alerts      []nwsAlert
	Unsupported bool
	FromCache   bool
	FetchedAt   time.Time
	Err         error
}

// coordsOf pulls latitude/longitude out of the wttr.in nearest_area.
func coordsOf(w *wttrResp) (lat, lon string, ok bool) {
	if w == nil || len(w.Areas) == 0 {
		return "", "", false
	}
	lat, lon = strings.TrimSpace(w.Areas[0].Latitude), strings.TrimSpace(w.Areas[0].Longitude)
	if lat == "" || lon == "" {
		return "", "", false
	}
	return lat, lon, true
}

func alertsCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "navi-weather")
}

func sanitizeCoord(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func alertsCachePath(dir, lat, lon string) string {
	return filepath.Join(dir, fmt.Sprintf("alerts-%s_%s.json", sanitizeCoord(lat), sanitizeCoord(lon)))
}

func saveAlertsCacheTo(dir, lat, lon string, env alertsEnvelope) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(alertsCachePath(dir, lat, lon), append(raw, '\n'), 0o644)
}

func loadAlertsCacheFrom(dir, lat, lon string) (alertsEnvelope, bool) {
	var env alertsEnvelope
	raw, err := os.ReadFile(alertsCachePath(dir, lat, lon))
	if err != nil {
		return env, false
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, false
	}
	return env, true
}

func cacheFresh(env alertsEnvelope) bool {
	return time.Since(env.FetchedAt) < alertsCacheTTL
}

// expired reports whether the alert's NWS expiry has passed. An empty or
// unparseable expiry is treated as live — we never drop an alert on bad
// data, only on a real timestamp that is behind us.
func (a nwsAlert) expired(now time.Time) bool {
	s := strings.TrimSpace(a.Expires)
	if s == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return false
	}
	return now.After(t)
}

// liveAlerts drops alerts whose NWS expiry has passed. NWS sometimes leaves
// long-tail statements in the feed past their useful life; the feed is
// "active" but the sky has moved on. Applied at serve time (not fetch
// time) so an alert that expires mid-cache still disappears on schedule.
func liveAlerts(in []nwsAlert) []nwsAlert {
	now := time.Now()
	out := make([]nwsAlert, 0, len(in))
	for _, a := range in {
		if !a.expired(now) {
			out = append(out, a)
		}
	}
	return out
}

// effectiveShort renders an NWS RFC3339 timestamp as a day-first date
// ("21 Sep") so old-but-active alerts show their age honestly.
func effectiveShort(s string) string {
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
		return t.Local().Format("02 Jan")
	}
	return ""
}

// isOutOfBoundsBody reports whether an NWS error body is the
// "this point is outside our coverage" shape (HTTP 400, InvalidParameter,
// detail mentioning "out of bounds"). Anything else is a real failure.
func isOutOfBoundsBody(body []byte) bool {
	var prob struct {
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &prob); err != nil {
		return false
	}
	return strings.Contains(prob.Type, "InvalidParameter") &&
		strings.Contains(strings.ToLower(prob.Detail), "out of bounds")
}

type nwsFeature struct {
	Properties nwsAlert `json:"properties"`
}

type nwsCollection struct {
	Features []nwsFeature `json:"features"`
}

// fetchNWSAlerts hits api.weather.gov directly. Returns unsupported=true
// when the point is outside NWS coverage (US territories only) — that is
// a fact about the location, not an error.
func fetchNWSAlerts(lat, lon string) (alerts []nwsAlert, unsupported bool, err error) {
	u := fmt.Sprintf("%s?point=%s,%s", nwsAlertsURL, lat, lon)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", nwsUserAgent) // required — NWS drops requests without it
	req.Header.Set("Accept", "application/geo+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode == http.StatusBadRequest && isOutOfBoundsBody(body) {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("api.weather.gov: %s", resp.Status)
	}
	var col nwsCollection
	if err := json.Unmarshal(body, &col); err != nil {
		return nil, false, fmt.Errorf("api.weather.gov: bad payload: %w", err)
	}
	for _, f := range col.Features {
		alerts = append(alerts, f.Properties)
	}
	return alerts, false, nil
}

// getAlerts is the cache-aware orchestrator: fresh cache wins, then the
// network, then a stale cache (marked FromCache), then the error.
func getAlerts(lat, lon string) alertsResult {
	dir := alertsCacheDir()
	if env, ok := loadAlertsCacheFrom(dir, lat, lon); ok && cacheFresh(env) {
		return alertsResult{Alerts: liveAlerts(env.Alerts), Unsupported: env.Unsupported, FromCache: true, FetchedAt: env.FetchedAt}
	}
	alerts, unsupported, err := fetchNWSAlerts(lat, lon)
	if err == nil {
		env := alertsEnvelope{FetchedAt: time.Now(), Unsupported: unsupported, Alerts: alerts}
		_ = saveAlertsCacheTo(dir, lat, lon, env) // cache is best-effort; a miss just costs a refetch
		return alertsResult{Alerts: liveAlerts(alerts), Unsupported: unsupported, FetchedAt: env.FetchedAt}
	}
	if env, ok := loadAlertsCacheFrom(dir, lat, lon); ok {
		return alertsResult{Alerts: liveAlerts(env.Alerts), Unsupported: env.Unsupported, FromCache: true, FetchedAt: env.FetchedAt}
	}
	return alertsResult{Err: err}
}

// expiresShort renders an NWS RFC3339 timestamp as local "15:04".
// Empty on unparseable input — the caller omits the expires bit.
func expiresShort(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("15:04")
	}
	return ""
}

// severityColor maps NWS severity to a lipgloss color. Red and cyan come
// from the shared theme tokens; orange and yellow are proposed additions
// (kept local per the mod conventions until the palette adopts them).
func severityColor(sev string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "extreme":
		return theme.Red
	case "severe":
		return lipgloss.Color("#ff9f1c") // proposed token: alert orange
	case "moderate":
		return lipgloss.Color("#ffd60a") // proposed token: alert yellow
	case "minor":
		return theme.Cyan
	default:
		return theme.Dim
	}
}

// severityStyle is the banner/chip style for a severity.
func severityStyle(sev string) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(severityColor(sev))
	if strings.EqualFold(strings.TrimSpace(sev), "extreme") {
		s = s.Bold(true)
	}
	return s
}
