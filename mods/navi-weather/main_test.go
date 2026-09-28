package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// a small but realistic wttr.in j1 payload: current + one day + two slots.
const sampleJ1 = `{
  "current_condition": [{
    "temp_C": "25", "temp_F": "78",
    "FeelsLikeC": "27", "FeelsLikeF": "80",
    "humidity": "65", "weatherCode": "116",
    "weatherDesc": [{"value": "Partly cloudy"}],
    "windspeedKmph": "13", "windspeedMiles": "8",
    "winddir16Point": "NE",
    "visibility": "10", "visibilityMiles": "6",
    "observation_time": "11:45 AM"
  }],
  "nearest_area": [{
    "areaName": [{"value": "Mena"}],
    "region": [{"value": "Arkansas"}],
    "country": [{"value": "United States of America"}]
  }],
  "weather": [{
    "date": "2026-09-18",
    "maxtempC": "28", "maxtempF": "82",
    "mintempC": "18", "mintempF": "64",
    "astronomy": [{"sunrise": "07:02 AM", "sunset": "07:19 PM"}],
    "hourly": [
      {"time": "900", "tempC": "24", "tempF": "75",
       "weatherCode": "116", "weatherDesc": [{"value": "Partly cloudy"}],
       "chanceofrain": "10", "humidity": "66"},
      {"time": "1200", "tempC": "27", "tempF": "81",
       "weatherCode": "113", "weatherDesc": [{"value": "Sunny"}],
       "chanceofrain": "0", "humidity": "55"}
    ]
  },
  {
    "date": "2026-09-19",
    "maxtempC": "29", "maxtempF": "84",
    "mintempC": "19", "mintempF": "66",
    "astronomy": [{"sunrise": "07:03 AM", "sunset": "07:18 PM"}],
    "hourly": [
      {"time": "1200", "tempC": "28", "tempF": "83",
       "weatherCode": "113", "weatherDesc": [{"value": "Sunny"}],
       "chanceofrain": "0", "humidity": "50"}
    ]
  }]
}`

func sampleResp(t *testing.T) *wttrResp {
	t.Helper()
	var w wttrResp
	if err := json.Unmarshal([]byte(sampleJ1), &w); err != nil {
		t.Fatalf("parse sample: %v", err)
	}
	return &w
}

func TestParseSample(t *testing.T) {
	w := sampleResp(t)
	if w.Current[0].TempF != "78" {
		t.Fatalf("temp_F = %q, want 78", w.Current[0].TempF)
	}
	if got := areaLabel(w, ""); got != "Mena, Arkansas" {
		t.Fatalf("areaLabel = %q, want Mena, Arkansas", got)
	}
}

func TestEmojiFor(t *testing.T) {
	cases := []struct {
		code, want string
		day        bool
	}{
		{"113", "☀️", true},
		{"113", "🌙", false},
		{"116", "⛅", true},
		{"122", "☁️", true},
		{"248", "🌫️", true},
		{"296", "🌦️", true},
		{"308", "🌧️", true},
		{"338", "❄️", true},
		{"389", "⛈️", true},
		{"999", "☁️", true}, // unknown code degrades gracefully
	}
	for _, c := range cases {
		if got := emojiFor(c.code, c.day); got != c.want {
			t.Errorf("emojiFor(%q, day=%v) = %q, want %q", c.code, c.day, got, c.want)
		}
	}
}

func TestIsDay(t *testing.T) {
	w := sampleResp(t) // 11:45 AM against 07:02 AM – 07:19 PM
	if !isDay(w) {
		t.Fatal("isDay = false at 11:45 AM, want true")
	}
	w.Current[0].ObservationTime = "11:45 PM"
	if isDay(w) {
		t.Fatal("isDay = true at 11:45 PM, want false")
	}
}

func TestHourLabel(t *testing.T) {
	if got := hourLabel("0"); got != "00" {
		t.Errorf("hourLabel(0) = %q", got)
	}
	if got := hourLabel("2100"); got != "21" {
		t.Errorf("hourLabel(2100) = %q", got)
	}
}

func TestViewRenders(t *testing.T) {
	m := newModel()
	m.loading = false
	m.data = sampleResp(t)
	view := m.View()
	for _, want := range []string{"Mena, Arkansas", "78°F", "Partly cloudy", "next 12 hours", "3-day"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	// metric toggle flips the units in the same data
	m.cfg.Units = "metric"
	view = m.View()
	if !strings.Contains(view, "25°C") {
		t.Error("metric view missing 25°C")
	}
}

func TestLocationsView(t *testing.T) {
	m := newModel()
	m.view = "locations"
	m.cfg.Locations = []string{"", "Mena, Arkansas"}
	view := m.View()
	if !strings.Contains(view, "here (auto)") || !strings.Contains(view, "Mena, Arkansas") {
		t.Error("locations view missing entries")
	}
}

// ---------------------------------------------------------------------------
// NWS alerts
// ---------------------------------------------------------------------------

// sampleNWS is a trimmed but real-shaped api.weather.gov response: one
// Moderate statement (captured live for Mena, AR) and one synthetic
// Extreme warning so every severity step is exercised.
const sampleNWS = `{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "properties": {
        "event": "Special Weather Statement",
        "severity": "Moderate",
        "headline": "Special Weather Statement issued September 21 at 6:35PM CDT by NWS Little Rock AR",
        "description": "* WHAT...Minor flooding is possible.\n\n* WHERE...Portions of western Arkansas.",
        "instruction": "Stay tuned to weather radio for updates.",
        "effective": "2026-09-21T18:35:00-05:00",
        "expires": "2026-09-21T19:15:00-05:00",
        "senderName": "NWS Little Rock AR"
      }
    },
    {
      "type": "Feature",
      "properties": {
        "event": "Tornado Warning",
        "severity": "Extreme",
        "headline": "Tornado Warning issued May 3 at 2:10PM CDT by NWS Tulsa OK",
        "description": "A confirmed large tornado is on the ground.",
        "instruction": "Take shelter immediately in a basement or interior room.",
        "effective": "2026-05-03T14:10:00-05:00",
        "expires": "2026-05-03T15:00:00-05:00",
        "senderName": "NWS Tulsa OK"
      }
    }
  ]
}`

func sampleAlerts(t *testing.T) []nwsAlert {
	t.Helper()
	var col nwsCollection
	if err := json.Unmarshal([]byte(sampleNWS), &col); err != nil {
		t.Fatalf("parse NWS sample: %v", err)
	}
	var out []nwsAlert
	for _, f := range col.Features {
		out = append(out, f.Properties)
	}
	return out
}

func TestParseNWSAlerts(t *testing.T) {
	alerts := sampleAlerts(t)
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2", len(alerts))
	}
	if alerts[0].Event != "Special Weather Statement" || alerts[0].Severity != "Moderate" {
		t.Errorf("alert[0] = %q/%q", alerts[0].Event, alerts[0].Severity)
	}
	if alerts[0].SenderName != "NWS Little Rock AR" {
		t.Errorf("alert[0].SenderName = %q", alerts[0].SenderName)
	}
	if alerts[1].Event != "Tornado Warning" || alerts[1].Severity != "Extreme" {
		t.Errorf("alert[1] = %q/%q", alerts[1].Event, alerts[1].Severity)
	}
	if !strings.Contains(alerts[1].Instruction, "Take shelter") {
		t.Error("alert[1] instruction not parsed")
	}
}

func TestSeverityColor(t *testing.T) {
	cases := []struct {
		sev  string
		want string // expected hex
	}{
		{"Extreme", "#ff3131"},
		{"extreme", "#ff3131"}, // case-insensitive
		{"Severe", "#ff9f1c"},
		{"Moderate", "#ffd60a"},
		{"Minor", "#00ffff"},
		{"Unknown", "#5c4a5c"},  // dims out
		{"weird", "#5c4a5c"},    // unknown severity degrades to dim
		{"", "#5c4a5c"},
	}
	for _, c := range cases {
		if got := string(severityColor(c.sev)); got != c.want {
			t.Errorf("severityColor(%q) = %q, want %q", c.sev, got, c.want)
		}
	}
}

func TestExpiresShort(t *testing.T) {
	got := expiresShort("2026-09-21T19:15:00-05:00")
	if len(got) != 5 || got[2] != ':' {
		t.Errorf("expiresShort = %q, want HH:MM shape", got)
	}
	if expiresShort("") != "" {
		t.Error("expiresShort(\"\") should be empty")
	}
	if expiresShort("not a time") != "" {
		t.Error("expiresShort(garbage) should be empty")
	}
}

func TestIsOutOfBoundsBody(t *testing.T) {
	london := []byte(`{"type":"https://api.weather.gov/problems/InvalidParameter","status":400,"detail":"Parameter \"point\" is invalid: out of bounds"}`)
	if !isOutOfBoundsBody(london) {
		t.Error("London 400 body not detected as out-of-bounds")
	}
	other := []byte(`{"type":"https://api.weather.gov/problems/ServerError","status":500,"detail":"boom"}`)
	if isOutOfBoundsBody(other) {
		t.Error("server error misclassified as out-of-bounds")
	}
	if isOutOfBoundsBody([]byte("not json")) {
		t.Error("garbage body misclassified as out-of-bounds")
	}
}

func TestCoordsOf(t *testing.T) {
	w := sampleResp(t)
	if _, _, ok := coordsOf(w); ok {
		t.Error("coordsOf should fail when nearest_area has no lat/lon")
	}
	w.Areas[0].Latitude = "34.586"
	w.Areas[0].Longitude = "-94.239"
	lat, lon, ok := coordsOf(w)
	if !ok || lat != "34.586" || lon != "-94.239" {
		t.Errorf("coordsOf = %q,%q,%v", lat, lon, ok)
	}
	if _, _, ok := coordsOf(nil); ok {
		t.Error("coordsOf(nil) should fail")
	}
}

func TestAlertsCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := alertsEnvelope{FetchedAt: time.Now(), Alerts: sampleAlerts(t)}
	if err := saveAlertsCacheTo(dir, "34.586", "-94.239", env); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, ok := loadAlertsCacheFrom(dir, "34.586", "-94.239")
	if !ok {
		t.Fatal("load failed")
	}
	if !cacheFresh(loaded) {
		t.Error("just-saved cache should be fresh")
	}
	if len(loaded.Alerts) != 2 || loaded.Alerts[1].Event != "Tornado Warning" {
		t.Error("cache round-trip lost alerts")
	}
	// stale cache is detected
	loaded.FetchedAt = time.Now().Add(-time.Hour)
	if cacheFresh(loaded) {
		t.Error("hour-old cache should be stale")
	}
	// unsupported locations round-trip too
	uenv := alertsEnvelope{FetchedAt: time.Now(), Unsupported: true}
	if err := saveAlertsCacheTo(dir, "51.5074", "-0.1278", uenv); err != nil {
		t.Fatalf("save unsupported: %v", err)
	}
	uloaded, ok := loadAlertsCacheFrom(dir, "51.5074", "-0.1278")
	if !ok || !uloaded.Unsupported {
		t.Error("unsupported flag did not survive the round-trip")
	}
	// missing cache
	if _, ok := loadAlertsCacheFrom(dir, "0", "0"); ok {
		t.Error("missing cache should not load")
	}
}

func TestSanitizeCoord(t *testing.T) {
	if got := sanitizeCoord("-94.239"); got != "-94.239" {
		t.Errorf("sanitizeCoord = %q", got)
	}
	if strings.Contains(sanitizeCoord("../../etc"), "/") {
		t.Error("sanitizeCoord left a path separator in place")
	}
}

func TestBannerLine(t *testing.T) {
	m := newModel()
	if got := m.bannerLine(); got != "" {
		t.Errorf("empty alerts banner = %q, want empty", got)
	}
	m.alerts = sampleAlerts(t)
	banner := m.bannerLine()
	if !strings.Contains(banner, "SPECIAL WEATHER STATEMENT") {
		t.Errorf("banner = %q, want the event", banner)
	}
	if !strings.Contains(banner, "expires") {
		t.Errorf("banner = %q, want expires time", banner)
	}
	// cycling: second alert shows after advancing the cycle
	m.alertCycle = 1
	if banner2 := m.bannerLine(); !strings.Contains(banner2, "TORNADO WARNING") {
		t.Errorf("cycled banner = %q, want tornado warning", banner2)
	}
	// unsupported dims honestly
	m2 := newModel()
	m2.alertsUnsupported = true
	if got := m2.bannerLine(); !strings.Contains(got, "outside the US") {
		t.Errorf("unsupported banner = %q", got)
	}
}

func TestAlertsViews(t *testing.T) {
	m := newModel()
	m.view = "alerts"
	if view := m.View(); !strings.Contains(view, "all clear") {
		t.Error("empty alerts view should say all clear")
	}
	m.alerts = sampleAlerts(t)
	view := m.View()
	if !strings.Contains(view, "Tornado Warning") || !strings.Contains(view, "[EXTREME]") {
		t.Error("alerts list missing rows/chips")
	}
	m.alertsUnsupported = true
	if view := m.View(); !strings.Contains(view, "outside the US") {
		t.Error("unsupported alerts view missing the honest note")
	}
	// detail view renders full text
	m.alertsUnsupported = false
	m.view = "alertdetail"
	m.alertCursor = 1
	dview := m.View()
	for _, want := range []string{"TORNADO WARNING", "Take shelter", "NWS Tulsa OK"} {
		if !strings.Contains(dview, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("one two three four", 10)
	if len(lines) != 2 || lines[0] != "one two" || lines[1] != "three four" {
		t.Errorf("wrapText = %q", lines)
	}
	lines = wrapText("para one\n\npara two", 40)
	if len(lines) != 3 || lines[1] != "" {
		t.Errorf("wrapText paragraph break = %q", lines)
	}
}

func TestAlertExpiry(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	cases := []struct {
		name    string
		expires string
		want    bool
	}{
		{"expired an hour ago", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), true},
		{"expires in an hour", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), false},
		{"empty expiry stays live", "", false},
		{"garbage expiry stays live", "not-a-time", false},
	}
	for _, c := range cases {
		if got := (nwsAlert{Expires: c.expires}).expired(time.Now()); got != c.want {
			t.Errorf("%s: expired = %v, want %v", c.name, got, c.want)
		}
	}
	// the Sept-21-statement shape: long past expiry must not survive
	in := []nwsAlert{
		{Event: "Special Weather Statement", Expires: past},
		{Event: "Heat Advisory", Expires: future},
		{Event: "No Expiry Field"},
	}
	live := liveAlerts(in)
	if len(live) != 2 {
		t.Fatalf("liveAlerts kept %d, want 2", len(live))
	}
	for _, a := range live {
		if a.Event == "Special Weather Statement" {
			t.Error("liveAlerts kept the expired statement")
		}
	}
}

func TestEffectiveShort(t *testing.T) {
	if got := effectiveShort("2026-09-21T14:00:00-05:00"); got != "21 Sep" {
		t.Errorf("effectiveShort = %q, want %q", got, "21 Sep")
	}
	if got := effectiveShort("garbage"); got != "" {
		t.Errorf("effectiveShort(garbage) = %q, want empty", got)
	}
}

func TestRadarLoopURL(t *testing.T) {
	if got := radarLoopURL("shv"); got != "https://radar.weather.gov/ridge/standard/SHV_loop.gif" {
		t.Errorf("radarLoopURL = %q", got)
	}
	if got := radarLoopURL(" KLWX "); got != "https://radar.weather.gov/ridge/standard/KLWX_loop.gif" {
		t.Errorf("radarLoopURL trims/cases = %q", got)
	}
}
