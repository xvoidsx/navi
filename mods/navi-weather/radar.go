// radar.go — "open in radar" for navi-weather alerts.
//
// The NWS /points endpoint tells us the nearest NEXRAD station for our
// coordinates; radar.weather.gov serves a classic looped GIF per station
// (https://radar.weather.gov/ridge/standard/<STATION>_loop.gif). We open
// it in a chromium webapp view via navi-browser-run, the same --app
// convention navi's webapp launchers use.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const nwsPointsURL = "https://api.weather.gov/points"

type nwsPointsProps struct {
	RadarStation string `json:"radarStation"`
}

type nwsPointsResp struct {
	Properties nwsPointsProps `json:"properties"`
}

// radarStationCache keeps the lookup to one HTTP round-trip per session
// per location; the station for a fixed point never changes.
var radarStationCache = map[string]string{}

// radarStationFor returns the NWS radar station ID (e.g. "SHV") nearest
// the given coordinates.
func radarStationFor(lat, lon string) (string, error) {
	key := strings.TrimSpace(lat) + "," + strings.TrimSpace(lon)
	if s, ok := radarStationCache[key]; ok {
		return s, nil
	}
	u := fmt.Sprintf("%s/%s", nwsPointsURL, key)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", nwsUserAgent) // required — NWS drops requests without it
	req.Header.Set("Accept", "application/geo+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api.weather.gov: %s", resp.Status)
	}
	var p nwsPointsResp
	if err := json.Unmarshal(body, &p); err != nil {
		return "", fmt.Errorf("api.weather.gov: bad payload: %w", err)
	}
	station := strings.ToUpper(strings.TrimSpace(p.Properties.RadarStation))
	if station == "" {
		return "", fmt.Errorf("api.weather.gov: no radar station for this point")
	}
	radarStationCache[key] = station
	return station, nil
}

// radarLoopURL is the looped NEXRAD GIF for a station.
func radarLoopURL(station string) string {
	return fmt.Sprintf("https://radar.weather.gov/ridge/standard/%s_loop.gif",
		strings.ToUpper(strings.TrimSpace(station)))
}

// openRadarLoop launches the station's radar loop in a chromium webapp
// view. Detached — the TUI keeps running underneath.
func openRadarLoop(station string) error {
	bin := "navi-browser-run"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "chromium"
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("no chromium-family browser found (need navi-browser-run or chromium)")
		}
	}
	cmd := exec.Command(bin, "--force-dark-mode", "--app="+radarLoopURL(station))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't open radar: %w", err)
	}
	return nil
}
