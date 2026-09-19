// navi-weather — a bubble tea weather mod for navi.
//
// wttr.in backend (no API key). IP geolocation by default; saved locations
// and the imperial/metric toggle live in ~/.config/navi-weather/config.json,
// shared with the waybar module script so the bar and the TUI always agree.
// Styled entirely through the shared nightshadeNeon theme package
// (mods/theme).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	theme "github.com/rav3ndust/navi-theme"
)

const frameWidth = 62

// ---------------------------------------------------------------------------
// config — shared with wired/waybar/weather.sh
// ---------------------------------------------------------------------------

type weatherConfig struct {
	Units           string   `json:"units"`            // "imperial" | "metric"
	DefaultLocation string   `json:"default_location"` // "" = auto-detect via IP
	Locations       []string `json:"locations"`        // "" = auto-detect
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "navi-weather", "config.json")
}

func defaultConfig() weatherConfig {
	return weatherConfig{
		Units:           "imperial",
		DefaultLocation: "",
		Locations:       []string{""},
	}
}

func loadConfig() weatherConfig {
	cfg := defaultConfig()
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	var loaded weatherConfig
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return cfg
	}
	if loaded.Units != "metric" && loaded.Units != "imperial" {
		loaded.Units = "imperial"
	}
	if len(loaded.Locations) == 0 {
		loaded.Locations = []string{""}
	}
	return loaded
}

func saveConfig(cfg weatherConfig) {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, append(raw, '\n'), 0o644)
}

func locationLabel(loc string) string {
	if loc == "" {
		return "here (auto)"
	}
	return loc
}

// ---------------------------------------------------------------------------
// wttr.in — format=j1
// ---------------------------------------------------------------------------

type wttrDesc struct {
	Value string `json:"value"`
}

type wttrCurrent struct {
	TempC          string     `json:"temp_C"`
	TempF          string     `json:"temp_F"`
	FeelsLikeC     string     `json:"FeelsLikeC"`
	FeelsLikeF     string     `json:"FeelsLikeF"`
	Humidity       string     `json:"humidity"`
	WeatherCode    string     `json:"weatherCode"`
	WeatherDesc    []wttrDesc `json:"weatherDesc"`
	WindKmph       string     `json:"windspeedKmph"`
	WindMph        string     `json:"windspeedMiles"`
	WindDir        string     `json:"winddir16Point"`
	VisibilityMi   string     `json:"visibilityMiles"`
	VisibilityKm   string     `json:"visibility"`
	ObservationTime string    `json:"observation_time"`
}

type wttrAreaName struct {
	Value string `json:"value"`
}

type wttrArea struct {
	AreaName []wttrAreaName `json:"areaName"`
	Region   []wttrAreaName `json:"region"`
	Country  []wttrAreaName `json:"country"`
}

type wttrAstronomy struct {
	Sunrise string `json:"sunrise"`
	Sunset  string `json:"sunset"`
}

type wttrHourly struct {
	Time         string     `json:"time"`
	TempC        string     `json:"tempC"`
	TempF        string     `json:"tempF"`
	WeatherCode  string     `json:"weatherCode"`
	WeatherDesc  []wttrDesc `json:"weatherDesc"`
	ChanceOfRain string     `json:"chanceofrain"`
	Humidity     string     `json:"humidity"`
}

type wttrDay struct {
	Date      string          `json:"date"`
	MaxTempC  string          `json:"maxtempC"`
	MaxTempF  string          `json:"maxtempF"`
	MinTempC  string          `json:"mintempC"`
	MinTempF  string          `json:"mintempF"`
	Astronomy []wttrAstronomy `json:"astronomy"`
	Hourly    []wttrHourly    `json:"hourly"`
}

type wttrResp struct {
	Current []wttrCurrent `json:"current_condition"`
	Areas   []wttrArea    `json:"nearest_area"`
	Days    []wttrDay     `json:"weather"`
}

func getWeather(loc, units string) (*wttrResp, error) {
	flag := "u"
	if units == "metric" {
		flag = "m"
	}
	path := ""
	if loc != "" {
		path = url.PathEscape(loc)
	}
	u := "https://wttr.in/" + path + "?format=j1&" + flag
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "navi-weather/1.0 (navi linux)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wttr.in: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var w wttrResp
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, err
	}
	if len(w.Current) == 0 {
		return nil, fmt.Errorf("wttr.in returned no current conditions")
	}
	return &w, nil
}

// ---------------------------------------------------------------------------
// condition emoji + day/night
// ---------------------------------------------------------------------------

// emojiFor maps wttr.in weather codes to condition emoji.
func emojiFor(code string, day bool) string {
	switch code {
	case "113": // clear / sunny
		if day {
			return "☀️"
		}
		return "🌙"
	case "116": // partly cloudy
		if day {
			return "⛅"
		}
		return "☁️"
	case "119", "122": // cloudy / overcast
		return "☁️"
	case "143", "248", "260": // mist / fog
		return "🌫️"
	case "176", "263", "266", "293", "296", "353": // drizzle / light showers
		return "🌦️"
	case "299", "302", "305", "308", "356", "359": // rain
		return "🌧️"
	case "179", "182", "185", "281", "284", "311", "314", "317", "350", "362", "365", "374", "377": // sleet / wintry mix
		return "🌨️"
	case "227", "230", "320", "323", "326", "329", "332", "335", "338", "368", "371": // snow
		return "❄️"
	case "200", "386", "389", "392", "395": // thunder
		return "⛈️"
	default:
		return "☁️"
	}
}

// hmToMin parses "07:02 AM" into minutes after midnight.
func hmToMin(s string) (int, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, false
	}
	hm := strings.Split(parts[0], ":")
	if len(hm) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(hm[0])
	m, err2 := strconv.Atoi(hm[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	ampm := strings.ToUpper(parts[1])
	if ampm == "PM" && h != 12 {
		h += 12
	}
	if ampm == "AM" && h == 12 {
		h = 0
	}
	return h*60 + m, true
}

// isDay reports whether the location is currently in daylight, using the
// observation time against today's sunrise/sunset. Falls back to day.
func isDay(w *wttrResp) bool {
	if len(w.Current) == 0 || len(w.Days) == 0 || len(w.Days[0].Astronomy) == 0 {
		return true
	}
	obs, ok1 := hmToMin(w.Current[0].ObservationTime)
	rise, ok2 := hmToMin(w.Days[0].Astronomy[0].Sunrise)
	set, ok3 := hmToMin(w.Days[0].Astronomy[0].Sunset)
	if !ok1 || !ok2 || !ok3 {
		return true
	}
	return obs >= rise && obs < set
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

type weatherMsg struct {
	data *wttrResp
	err  error
}

type tickMsg time.Time

type model struct {
	cfg       weatherConfig
	data      *wttrResp
	fetchErr  string
	loading   bool
	lastFetch time.Time
	view      string // "main" | "locations"
	locCursor int
	adding    bool
	input     textinput.Model
}

func newModel() model {
	cfg := loadConfig()
	ti := textinput.New()
	ti.Placeholder = "City, State or ZIP…"
	ti.CharLimit = 80
	return model{
		cfg:     cfg,
		loading: true,
		input:   ti,
	}
}

func (m model) activeLocation() string {
	return m.cfg.DefaultLocation
}

func fetchWeatherCmd(loc, units string) tea.Cmd {
	return func() tea.Msg {
		data, err := getWeather(loc, units)
		return weatherMsg{data: data, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(30*time.Minute, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		fetchWeatherCmd(m.activeLocation(), m.cfg.Units),
		tickCmd(),
	)
}

func (m model) refetch() (model, tea.Cmd) {
	m.loading = true
	m.fetchErr = ""
	return m, fetchWeatherCmd(m.activeLocation(), m.cfg.Units)
}

func (m *model) setLocation(loc string) {
	m.cfg.DefaultLocation = loc
	if !contains(m.cfg.Locations, loc) {
		m.cfg.Locations = append(m.cfg.Locations, loc)
	}
	saveConfig(m.cfg)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case weatherMsg:
		m.loading = false
		if msg.err != nil {
			// keep last good data on screen; note the staleness
			m.fetchErr = msg.err.Error()
		} else {
			m.data = msg.data
			m.fetchErr = ""
			m.lastFetch = time.Now()
		}
		return m, nil

	case tickMsg:
		m.loading = true
		return m, tea.Batch(
			fetchWeatherCmd(m.activeLocation(), m.cfg.Units),
			tickCmd(),
		)

	case tea.KeyMsg:
		if m.adding {
			switch msg.String() {
			case "enter":
				loc := strings.TrimSpace(m.input.Value())
				if loc != "" {
					m.setLocation(loc)
					m.adding = false
					m.input.Reset()
					m.view = "main"
					return m.refetch()
				}
				return m, nil
			case "esc":
				m.adding = false
				m.input.Reset()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch m.view {
		case "locations":
			switch msg.String() {
			case "esc", "q":
				m.view = "main"
				return m, nil
			case "up", "k":
				if m.locCursor > 0 {
					m.locCursor--
				}
				return m, nil
			case "down", "j":
				if m.locCursor < len(m.cfg.Locations)-1 {
					m.locCursor++
				}
				return m, nil
			case "enter":
				if len(m.cfg.Locations) > 0 {
					m.setLocation(m.cfg.Locations[m.locCursor])
					m.view = "main"
					return m.refetch()
				}
				return m, nil
			case "a":
				m.adding = true
				m.input.Focus()
				return m, nil
			case "d":
				if len(m.cfg.Locations) > 1 && m.locCursor < len(m.cfg.Locations) {
					m.cfg.Locations = append(m.cfg.Locations[:m.locCursor], m.cfg.Locations[m.locCursor+1:]...)
					if m.locCursor >= len(m.cfg.Locations) {
						m.locCursor = len(m.cfg.Locations) - 1
					}
					if !contains(m.cfg.Locations, m.cfg.DefaultLocation) {
						m.cfg.DefaultLocation = m.cfg.Locations[0]
					}
					saveConfig(m.cfg)
				}
				return m, nil
			}
			return m, nil
		}

		// main view keys
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "u":
			if m.cfg.Units == "imperial" {
				m.cfg.Units = "metric"
			} else {
				m.cfg.Units = "imperial"
			}
			saveConfig(m.cfg)
			return m.refetch()
		case "r":
			return m.refetch()
		case "l":
			m.view = "locations"
			m.locCursor = 0
			for i, loc := range m.cfg.Locations {
				if loc == m.cfg.DefaultLocation {
					m.locCursor = i
					break
				}
			}
			return m, nil
		case "tab":
			if len(m.cfg.Locations) > 1 {
				idx := 0
				for i, loc := range m.cfg.Locations {
					if loc == m.cfg.DefaultLocation {
						idx = i
						break
					}
				}
				m.setLocation(m.cfg.Locations[(idx+1)%len(m.cfg.Locations)])
				return m.refetch()
			}
			return m, nil
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// view
// ---------------------------------------------------------------------------

func (m model) temp(f, c string) string {
	if m.cfg.Units == "metric" {
		return c + "°C"
	}
	return f + "°F"
}

func (m model) wind(kmph, mph, dir string) string {
	if m.cfg.Units == "metric" {
		return kmph + " km/h " + dir
	}
	return mph + " mph " + dir
}

func (m model) vis(km, mi string) string {
	if m.cfg.Units == "metric" {
		return km + " km"
	}
	return mi + " mi"
}

func areaLabel(w *wttrResp, fallback string) string {
	if len(w.Areas) > 0 {
		a := w.Areas[0]
		name := ""
		if len(a.AreaName) > 0 {
			name = a.AreaName[0].Value
		}
		region := ""
		if len(a.Region) > 0 {
			region = a.Region[0].Value
		}
		if name == "" {
			name = fallback
		}
		if region != "" && region != name {
			return name + ", " + region
		}
		return name
	}
	if fallback == "" {
		return "here"
	}
	return fallback
}

func descOf(d []wttrDesc) string {
	if len(d) > 0 {
		return d[0].Value
	}
	return ""
}

// hourLabel converts wttr.in's "0".."2100" slot into a 24h "HH" label.
func hourLabel(t string) string {
	return fmt.Sprintf("%02d", atoi(t)/100)
}

// dayLabel converts "2026-09-18" into "Today" or "19 Sep".
func dayLabel(date string, idx int) string {
	if idx == 0 {
		return "Today"
	}
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.Format("02 Jan")
	}
	return date
}

// nextSlots returns up to n hourly slots starting from the current time.
func nextSlots(w *wttrResp, n int) []wttrHourly {
	if len(w.Current) == 0 || len(w.Days) == 0 {
		return nil
	}
	obsMin, ok := hmToMin(w.Current[0].ObservationTime)
	if !ok {
		obsMin = 12 * 60
	}
	var slots []wttrHourly
	for di, day := range w.Days {
		for _, h := range day.Hourly {
			slotMin := (atoi(h.Time) / 100) * 60
			if di == 0 && slotMin < obsMin-90 {
				continue // already passed (with a little overlap)
			}
			slots = append(slots, h)
			if len(slots) >= n {
				return slots
			}
		}
	}
	return slots
}

func (m model) viewMain() string {
	if m.loading && m.data == nil {
		return theme.Frame(frameWidth, "navi weather", true, "",
			theme.Grayed.Render("  asking the sky…"))
	}
	if m.data == nil {
		return theme.Frame(frameWidth, "navi weather", false, "",
			theme.Error.Render("  couldn't reach wttr.in — check your connection and press r to retry."))
	}

	w := m.data
	cur := w.Current[0]
	day := isDay(w)

	var b strings.Builder
	// location
	b.WriteString(theme.Header.Render("  " + areaLabel(w, m.activeLocation())) + "\n")
	// current conditions, big
	b.WriteString("  " + theme.Normal.Render(emojiFor(cur.WeatherCode, day)+"  "+m.temp(cur.TempF, cur.TempC)) +
		theme.Grayed.Render("  —  "+descOf(cur.WeatherDesc)) + "\n")
	// feels like + today's high/low
	hi, lo := "", ""
	if len(w.Days) > 0 {
		hi = m.temp(w.Days[0].MaxTempF, w.Days[0].MaxTempC)
		lo = m.temp(w.Days[0].MinTempF, w.Days[0].MinTempC)
	}
	details := "feels like " + m.temp(cur.FeelsLikeF, cur.FeelsLikeC)
	if hi != "" {
		details += "   ·   H " + hi + "  L " + lo
	}
	b.WriteString(theme.Grayed.Render("  "+details) + "\n")
	b.WriteString(theme.Grayed.Render("  humidity "+cur.Humidity+"%   ·   wind "+m.wind(cur.WindKmph, cur.WindMph, cur.WindDir)+
		"   ·   visibility "+m.vis(cur.VisibilityKm, cur.VisibilityMi)) + "\n")

	b.WriteString("\n" + theme.Header.Render("  next 12 hours") + "\n")
	for _, h := range nextSlots(w, 4) {
		line := "  " + hourLabel(h.Time) + "   " + emojiFor(h.WeatherCode, true) + "   " +
			m.temp(h.TempF, h.TempC)
		if r := atoi(h.ChanceOfRain); r > 0 {
			line += theme.Grayed.Render(fmt.Sprintf("   ·   %d%% rain", r))
		}
		b.WriteString(line + "\n")
	}

	if len(w.Days) > 1 {
		b.WriteString("\n" + theme.Header.Render("  3-day") + "\n")
		for i, d := range w.Days {
			if i > 2 {
				break
			}
			code := ""
			if len(d.Hourly) > 4 {
				code = d.Hourly[4].WeatherCode // midday slot
			}
			line := "  " + dayLabel(d.Date, i) + "   " + emojiFor(code, true) + "   " +
				m.temp(d.MaxTempF, d.MaxTempC) + " / " + m.temp(d.MinTempF, d.MinTempC)
			b.WriteString(line + "\n")
		}
	}

	if m.fetchErr != "" {
		b.WriteString("\n" + theme.Error.Render("  stale — couldn't refresh ("+shortErr(m.fetchErr)+")") + "\n")
	}

	unitKey := "°F/°C"
	footer := theme.Footer(true,
		[2]string{"q", "quit"},
		[2]string{"u", unitKey},
		[2]string{"r", "refresh"},
		[2]string{"l", "locations"},
		[2]string{"tab", "next"},
	)
	return theme.Frame(frameWidth, "navi weather", true, "", b.String()+footer)
}

func shortErr(s string) string {
	if len(s) > 60 {
		return s[:57] + "…"
	}
	return s
}

func (m model) viewLocations() string {
	var b strings.Builder
	b.WriteString(theme.Header.Render("  saved locations") + "\n\n")
	for i, loc := range m.cfg.Locations {
		marker := "  "
		if loc == m.cfg.DefaultLocation {
			marker = "● "
		}
		line := marker + locationLabel(loc)
		if i == m.locCursor {
			b.WriteString("  " + theme.Selected.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + theme.Normal.Render("  "+line) + "\n")
		}
	}
	b.WriteString("\n")
	if m.adding {
		b.WriteString("  " + theme.Grayed.Render("add location:") + "\n")
		b.WriteString("  " + m.input.View() + "\n")
		footer := theme.Footer(false,
			[2]string{"enter", "save"},
			[2]string{"esc", "cancel"},
		)
		return theme.Frame(frameWidth, "navi weather", true, "", b.String()+footer)
	}
	footer := theme.Footer(false,
		[2]string{"enter", "select"},
		[2]string{"a", "add"},
		[2]string{"d", "delete"},
		[2]string{"esc", "back"},
	)
	return theme.Frame(frameWidth, "navi weather", true, "", b.String()+footer)
}

func (m model) View() string {
	if m.view == "locations" {
		return m.viewLocations()
	}
	return m.viewMain()
}

func main() {
	dump := flag.Bool("dump", false, "print the initial view and exit")
	flag.Parse()

	m := newModel()
	if *dump {
		fmt.Println(m.View())
		return
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "navi-weather:", err)
		os.Exit(1)
	}
	// let the terminal restore cleanly, same as the other mods
	time.Sleep(50 * time.Millisecond)
}
