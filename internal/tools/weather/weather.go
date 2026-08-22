package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

const (
	geocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	forecastURL  = "https://api.open-meteo.com/v1/forecast"

	timeout = 10 * time.Second

	maxDays = 7 // the ceiling the schema advertises and Execute enforces

	current = "temperature_2m,apparent_temperature,relative_humidity_2m,precipitation,weather_code,wind_speed_10m"
	daily   = "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum"
)

// Tool answers questions about the weather somewhere, over Open-Meteo. The
// two endpoints and the client are fields rather than constants so a test can
// point them at a server of its own: nothing here reaches the network unless
// the caller left the defaults in place.
type Tool struct {
	client    *http.Client
	geocoding string
	forecast  string
}

// Option adjusts what [New] builds.
type Option func(*Tool)

// WithHTTPClient hands the tool the client to call with.
func WithHTTPClient(client *http.Client) Option {
	return func(tool *Tool) { tool.client = client }
}

// WithBaseURLs points the tool at somewhere other than Open-Meteo — a test
// server, most of the time.
func WithBaseURLs(geocoding, forecast string) Option {
	return func(tool *Tool) {
		tool.geocoding = geocoding
		tool.forecast = forecast
	}
}

// New builds the tool. The defaults are Open-Meteo's public endpoints, which
// ask for no credentials, and a client that gives up rather than hanging.
func New(options ...Option) Tool {
	tool := Tool{
		client:    &http.Client{Timeout: timeout},
		geocoding: geocodingURL,
		forecast:  forecastURL,
	}
	for _, option := range options {
		option(&tool)
	}
	return tool
}

func (Tool) Name() string { return "weather" }

func (Tool) Description() string {
	return "Look up the weather for a place by name. Returns the current " +
		"conditions — temperature, what it feels like, humidity, wind and " +
		"precipitation — for the place that best matches the name given. " +
		"Pass days to get a daily forecast for that many days on top of the " +
		"current conditions. The place is resolved by name, so 'Paris' or " +
		"'Austin, Texas' both work; the answer names the place it settled on."
}

func (Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "City or place name, for example 'Paris' or 'Austin, Texas'.",
			},
			"days": map[string]any{
				"type": "integer",
				"description": "Days of daily forecast to return on top of the current " +
					"conditions, 0 to 7. Defaults to 0, which is current conditions only.",
				"minimum": 0,
				"maximum": maxDays,
			},
			"units": map[string]any{
				"type":        "string",
				"description": "Unit system for temperature and wind. Defaults to celsius.",
				"enum":        []string{"celsius", "fahrenheit"},
			},
		},
		"required": []string{"location"},
	}
}

type arguments struct {
	Location string `json:"location"`
	Days     int    `json:"days"`
	Units    string `json:"units"`
}

// Execute resolves the place, asks for its weather, and writes the answer out
// as prose. Every error here is written to be read by the model: it is the
// one that has to decide whether to fix the arguments and ask again.
func (tool Tool) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args arguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Location) == "" {
		return "", fmt.Errorf("location is required")
	}
	if args.Days < 0 || args.Days > maxDays {
		return "", fmt.Errorf("days must be between 0 and %d, got %d", maxDays, args.Days)
	}
	switch args.Units {
	case "", "celsius", "fahrenheit":
	default:
		return "", fmt.Errorf("units must be celsius or fahrenheit, got '%s'", args.Units)
	}

	place, err := tool.geocode(ctx, args.Location)
	if err != nil {
		return "", err
	}

	weather, err := tool.weather(ctx, place, args)
	if err != nil {
		return "", err
	}

	return report(place, weather, args.Days), nil
}

// place is the one hit the geocoder was asked for.
type place struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
	Timezone  string  `json:"timezone"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type geocoding struct {
	Results []place `json:"results"`
}

// geocode turns a name into somewhere on the globe. A name that matches
// nothing is an error rather than an empty answer: the model can spell it
// differently and try again, which it cannot do with silence.
func (tool Tool) geocode(ctx context.Context, name string) (place, error) {
	query := url.Values{
		"name":     {name},
		"count":    {"1"},
		"language": {"en"},
		"format":   {"json"},
	}

	var response geocoding
	if err := tool.get(ctx, tool.geocoding, query, &response); err != nil {
		return place{}, fmt.Errorf("could not look up '%s': %w", name, err)
	}
	if len(response.Results) == 0 {
		return place{}, fmt.Errorf("no place found for '%s'", name)
	}

	return response.Results[0], nil
}

// units is the labels Open-Meteo answered in. They are read off the response
// rather than assumed, so the report says what the numbers actually are.
type units struct {
	Temperature   string `json:"temperature_2m"`
	Max           string `json:"temperature_2m_max"`
	Precipitation string `json:"precipitation"`
	Sum           string `json:"precipitation_sum"`
	Wind          string `json:"wind_speed_10m"`
	Humidity      string `json:"relative_humidity_2m"`
}

type forecast struct {
	Timezone     string `json:"timezone"`
	CurrentUnits units  `json:"current_units"`
	DailyUnits   units  `json:"daily_units"`
	Current      struct {
		Temperature float64 `json:"temperature_2m"`
		Apparent    float64 `json:"apparent_temperature"`
		Humidity    float64 `json:"relative_humidity_2m"`
		Rain        float64 `json:"precipitation"`
		Code        int     `json:"weather_code"`
		Wind        float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Time []string  `json:"time"`
		Code []int     `json:"weather_code"`
		Max  []float64 `json:"temperature_2m_max"`
		Min  []float64 `json:"temperature_2m_min"`
		Rain []float64 `json:"precipitation_sum"`
	} `json:"daily"`
}

func (tool Tool) weather(ctx context.Context, at place, args arguments) (forecast, error) {
	query := url.Values{
		"latitude":  {strconv.FormatFloat(at.Latitude, 'f', -1, 64)},
		"longitude": {strconv.FormatFloat(at.Longitude, 'f', -1, 64)},
		"current":   {current},
		"timezone":  {"auto"},
	}
	if args.Days > 0 {
		query.Set("daily", daily)
		query.Set("forecast_days", strconv.Itoa(args.Days))
	}
	if args.Units == "fahrenheit" {
		query.Set("temperature_unit", "fahrenheit")
		query.Set("wind_speed_unit", "mph")
	}

	var response forecast
	if err := tool.get(ctx, tool.forecast, query, &response); err != nil {
		return forecast{}, fmt.Errorf("could not get the weather for %s: %w", at.Name, err)
	}

	return response, nil
}

// get runs one request and decodes the body into value. Anything other than a
// 200 is reported by its status: the body of an error is Open-Meteo's to
// shape, and repeating it would only put noise in front of the model.
func (tool Tool) get(ctx context.Context, endpoint string, query url.Values, value any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}

	response, err := tool.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the weather service answered %s", response.Status)
	}
	if err := json.NewDecoder(response.Body).Decode(value); err != nil {
		return fmt.Errorf("could not read the answer: %w", err)
	}

	return nil
}

// report writes the answer the model reads. Prose rather than JSON: the
// reader is a model, and a sentence costs it less than a shape it has to
// decode first.
func report(at place, weather forecast, days int) string {
	var out strings.Builder

	fmt.Fprintln(&out, heading(at, weather.Timezone))

	now, u := weather.Current, weather.CurrentUnits
	fmt.Fprintf(&out, "Now: %.1f%s (feels %.1f%s), %s, humidity %.0f%s, wind %.1f %s, precipitation %.1f %s\n",
		now.Temperature, u.Temperature,
		now.Apparent, u.Temperature,
		condition(now.Code),
		now.Humidity, u.Humidity,
		now.Wind, u.Wind,
		now.Rain, u.Precipitation,
	)

	if days > 0 && len(weather.Daily.Time) > 0 {
		fmt.Fprintln(&out)
		d, du := weather.Daily, weather.DailyUnits
		for i, day := range d.Time {
			fmt.Fprintf(&out, "%s  %-20s max %.1f%s  min %.1f%s  precip %.1f %s\n",
				day, condition(nth(d.Code, i)),
				nth(d.Max, i), du.Max,
				nth(d.Min, i), du.Max,
				nth(d.Rain, i), du.Sum,
			)
		}
	}

	return strings.TrimRight(out.String(), "\n")
}

// heading names the place the geocoder settled on, so the model can see it
// answered about somewhere other than what was asked for.
func heading(at place, timezone string) string {
	parts := []string{at.Name}
	if at.Admin1 != "" && at.Admin1 != at.Name {
		parts = append(parts, at.Admin1)
	}
	if at.Country != "" {
		parts = append(parts, at.Country)
	}
	line := strings.Join(parts, ", ")
	if timezone == "" {
		timezone = at.Timezone
	}
	if timezone != "" {
		line += " (" + timezone + ")"
	}
	return line
}

// nth reads one day out of a parallel array. The arrays come back from the
// service and are only promised to line up, so a short one is read as absent
// rather than a panic.
func nth[T any](values []T, i int) T {
	var zero T
	if i >= len(values) {
		return zero
	}
	return values[i]
}

// condition puts a WMO weather code into words. A code with no entry is
// reported as itself: naming it wrongly would be worse than not naming it.
func condition(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1:
		return "mainly clear"
	case 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61, 63, 65:
		return "rain"
	case 66, 67:
		return "freezing rain"
	case 71, 73, 75:
		return "snow"
	case 77:
		return "snow grains"
	case 80, 81, 82:
		return "rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	default:
		return fmt.Sprintf("weather code %d", code)
	}
}
