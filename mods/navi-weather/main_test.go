package main

import (
	"encoding/json"
	"strings"
	"testing"
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
