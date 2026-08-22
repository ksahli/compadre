package weather_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/tools/weather"
)

// The two answers the real service gives, trimmed to the fields the tool
// reads. Keeping them verbatim is the point: a test that invents a shape
// proves the tool parses the test's idea of Open-Meteo.
const (
	paris = `{"results":[{"id":2988507,"name":"Paris","latitude":48.85341,` +
		`"longitude":2.3488,"country_code":"FR","timezone":"Europe/Paris",` +
		`"country":"France","admin1":"Île-de-France Region"}],` +
		`"generationtime_ms":0.81312656}`

	nowhere = `{"generationtime_ms":0.2}`

	forecast = `{"latitude":48.84,"longitude":2.3599997,"timezone":"Europe/Paris",` +
		`"current_units":{"time":"iso8601","temperature_2m":"°C",` +
		`"apparent_temperature":"°C","relative_humidity_2m":"%",` +
		`"precipitation":"mm","weather_code":"wmo code","wind_speed_10m":"km/h"},` +
		`"current":{"time":"2026-08-22T10:00","temperature_2m":16.9,` +
		`"apparent_temperature":16.5,"relative_humidity_2m":68,"precipitation":0.00,` +
		`"weather_code":2,"wind_speed_10m":5.4},` +
		`"daily_units":{"time":"iso8601","weather_code":"wmo code",` +
		`"temperature_2m_max":"°C","temperature_2m_min":"°C","precipitation_sum":"mm"},` +
		`"daily":{"time":["2026-08-22","2026-08-23","2026-08-24"],` +
		`"weather_code":[3,3,80],"temperature_2m_max":[21.8,24.0,26.2],` +
		`"temperature_2m_min":[13.9,14.0,14.5],"precipitation_sum":[0.00,0.00,0.30]}}`
)

// server stands in for Open-Meteo. It records the queries it was asked, so a
// test can pin what the tool sent as well as what it made of the answer.
type server struct {
	geocoding url.Values
	forecast  url.Values
	calls     int
}

// stub serves the two bodies given, in order of endpoint, and hands back the
// options that point a tool at it.
func stub(t *testing.T, place, weatherBody string, status int) (*server, []weather.Option) {
	t.Helper()

	s := new(server)
	handler := func(w http.ResponseWriter, r *http.Request) {
		s.calls++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			s.geocoding = r.URL.Query()
			_, _ = w.Write([]byte(place))
		case "/forecast":
			s.forecast = r.URL.Query()
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			_, _ = w.Write([]byte(weatherBody))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(ts.Close)

	return s, []weather.Option{
		weather.WithBaseURLs(ts.URL+"/search", ts.URL+"/forecast"),
		weather.WithHTTPClient(ts.Client()),
	}
}

// TestDefinition pins the half of a tool the model reads. The name is the key
// a use is resolved through, so it is a contract and not a label.
func TestDefinition(t *testing.T) {
	tool := weather.New()

	if got, want := tool.Name(), "weather"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if tool.Description() == "" {
		t.Error("Description() is empty, want something the model can read")
	}

	schema := tool.Schema()
	if got, want := schema["type"], "object"; got != want {
		t.Errorf("Schema()[\"type\"] = %v, want %v", got, want)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema()[\"properties\"] is %T, want map[string]any", schema["properties"])
	}
	for _, name := range []string{"location", "days", "units"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("Schema() has no property %q", name)
		}
	}

	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "location" {
		t.Errorf("Schema()[\"required\"] = %v, want [location]", schema["required"])
	}

	// The schema is on its way to a wire format, so what matters is that it
	// survives the trip as JSON rather than that it is a map at all.
	if _, err := json.Marshal(schema); err != nil {
		t.Errorf("json.Marshal(Schema()) error = %v, want nil", err)
	}
}

// TestSatisfiesThePort is the assertion the rest of the package rests on: a
// tool the core cannot hold is not a tool. It is a compile-time check that
// happens to be written as a test, the same instinct as TestAliases.
func TestSatisfiesThePort(t *testing.T) {
	var _ definitions.Type = weather.New()
}

func TestExecuteCurrentConditions(t *testing.T) {
	s, options := stub(t, paris, forecast, http.StatusOK)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// The place the geocoder settled on is named, so the model can see when
	// it answered about somewhere other than what was asked for.
	for _, want := range []string{
		"Paris", "France", "Europe/Paris",
		"16.9°C", "feels 16.5°C", "partly cloudy",
		"humidity 68%", "wind 5.4 km/h", "precipitation 0.0 mm",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Execute() = %q, want it to contain %q", out, want)
		}
	}

	// No days asked for is no forecast asked for: the second request must
	// not carry the daily parameters at all.
	if got := s.forecast.Get("daily"); got != "" {
		t.Errorf("forecast query daily = %q, want empty", got)
	}
	if got := s.forecast.Get("forecast_days"); got != "" {
		t.Errorf("forecast query forecast_days = %q, want empty", got)
	}
	if strings.Contains(out, "2026-08-22") {
		t.Errorf("Execute() = %q, want no daily lines", out)
	}

	if got, want := s.geocoding.Get("name"), "Paris"; got != want {
		t.Errorf("geocoding query name = %q, want %q", got, want)
	}
	// The forecast is asked for by coordinate, not by name: the geocoding
	// step exists precisely because the service cannot take the name.
	if got, want := s.forecast.Get("latitude"), "48.85341"; got != want {
		t.Errorf("forecast query latitude = %q, want %q", got, want)
	}
	if got, want := s.forecast.Get("longitude"), "2.3488"; got != want {
		t.Errorf("forecast query longitude = %q, want %q", got, want)
	}
}

func TestExecuteWithForecast(t *testing.T) {
	s, options := stub(t, paris, forecast, http.StatusOK)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris","days":3}`))
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if got, want := s.forecast.Get("forecast_days"), "3"; got != want {
		t.Errorf("forecast query forecast_days = %q, want %q", got, want)
	}
	if got := s.forecast.Get("daily"); !strings.Contains(got, "temperature_2m_max") {
		t.Errorf("forecast query daily = %q, want it to ask for the daily maximum", got)
	}

	for _, want := range []string{
		"2026-08-22", "2026-08-23", "2026-08-24",
		"max 21.8°C", "min 13.9°C", "rain showers", "precip 0.3 mm",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Execute() = %q, want it to contain %q", out, want)
		}
	}
}

// TestExecuteUnits pins that the choice reaches the service rather than being
// applied to the numbers here. The tool converts nothing: it asks.
func TestExecuteUnits(t *testing.T) {
	cases := []struct {
		name        string
		arguments   string
		temperature string
		wind        string
	}{
		{"celsius by default", `{"location":"Paris"}`, "", ""},
		{"celsius asked for", `{"location":"Paris","units":"celsius"}`, "", ""},
		{"fahrenheit", `{"location":"Paris","units":"fahrenheit"}`, "fahrenheit", "mph"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, options := stub(t, paris, forecast, http.StatusOK)

			if _, err := weather.New(options...).Execute(t.Context(), args(t, c.arguments)); err != nil {
				t.Fatalf("Execute() error = %v, want nil", err)
			}

			if got := s.forecast.Get("temperature_unit"); got != c.temperature {
				t.Errorf("forecast query temperature_unit = %q, want %q", got, c.temperature)
			}
			if got := s.forecast.Get("wind_speed_unit"); got != c.wind {
				t.Errorf("forecast query wind_speed_unit = %q, want %q", got, c.wind)
			}
		})
	}
}

// TestExecuteRejectsArguments pins the errors the model can fix and ask again
// with. Each names the value that was wrong, because the model is the only
// one who can correct it.
func TestExecuteRejectsArguments(t *testing.T) {
	cases := []struct {
		name      string
		arguments string
		message   string
		// prefix is set where the message wraps whatever the decoder
		// said. What is pinned is the sentence the model reads first;
		// the rest is the standard library's to word.
		prefix bool
	}{
		{"not json", `not json`, "could not parse arguments:", true},
		{"wrong type", `{"location":42}`, "could not parse arguments:", true},
		{"no location", `{}`, "location is required", false},
		{"empty location", `{"location":""}`, "location is required", false},
		{"blank location", `{"location":"   "}`, "location is required", false},
		{"negative days", `{"location":"Paris","days":-1}`, "days must be between 0 and 7, got -1", false},
		{"too many days", `{"location":"Paris","days":9}`, "days must be between 0 and 7, got 9", false},
		{"unknown units", `{"location":"Paris","units":"kelvin"}`, "units must be celsius or fahrenheit, got 'kelvin'", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, options := stub(t, paris, forecast, http.StatusOK)

			out, err := weather.New(options...).Execute(t.Context(), args(t, c.arguments))
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			got := err.Error()
			if c.prefix && !strings.HasPrefix(got, c.message) {
				t.Errorf("Execute() error = %q, want it to start with %q", got, c.message)
			}
			if !c.prefix && got != c.message {
				t.Errorf("Execute() error = %q, want %q", got, c.message)
			}
			// Arguments that cannot be right are refused before the
			// service is troubled with them.
			if s.calls != 0 {
				t.Errorf("server saw %d requests, want 0", s.calls)
			}
		})
	}
}

// TestExecuteNoSuchPlace pins that a name matching nothing is an error rather
// than an empty answer: the model can spell it differently and try again,
// which it cannot do with silence.
func TestExecuteNoSuchPlace(t *testing.T) {
	s, options := stub(t, nowhere, forecast, http.StatusOK)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Atlantis"}`))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
	if got, want := err.Error(), "no place found for 'Atlantis'"; got != want {
		t.Errorf("Execute() error = %q, want %q", got, want)
	}
	// A place that does not exist has no weather to ask about.
	if s.calls != 1 {
		t.Errorf("server saw %d requests, want 1", s.calls)
	}
}

// TestExecuteServiceFails pins that a service having a bad day is reported by
// its status rather than swallowed.
func TestExecuteServiceFails(t *testing.T) {
	_, options := stub(t, paris, "", http.StatusInternalServerError)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris"}`))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
	for _, want := range []string{"Paris", "500"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Execute() error = %q, want it to contain %q", err, want)
		}
	}
}

// TestExecuteRefusesAnEndlessBody pins the ceiling on what the service can
// make this process read. The body here is valid JSON and simply too long: cut
// off at the ceiling it stops mid-value, which is a read error and not a place.
func TestExecuteRefusesAnEndlessBody(t *testing.T) {
	padding := strings.Repeat("a", 2<<20)
	endless := `{"results":[{"name":"Paris","country":"France","latitude":48.85,` +
		`"longitude":2.35,"padding":"` + padding + `"}]}`

	_, options := stub(t, endless, forecast, http.StatusOK)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris"}`))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
	if got := err.Error(); !strings.Contains(got, "could not read the answer") {
		t.Errorf("Execute() error = %q, want it to report a failed read", got)
	}
}

// TestExecuteCancelled pins that the context reaches the request. A tool that
// ignored it would keep a cancelled run waiting on a network it no longer
// has any reason to be on.
func TestExecuteCancelled(t *testing.T) {
	_, options := stub(t, paris, forecast, http.StatusOK)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	out, err := weather.New(options...).Execute(ctx, args(t, `{"location":"Paris"}`))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
}

// TestThroughInvoke drives the tool the way the runtime will once there is a
// loop: assembled into a registry and reached by name. It is the one test
// that pins the tool answers to the id it was called under.
func TestThroughInvoke(t *testing.T) {
	_, options := stub(t, paris, forecast, http.StatusOK)
	registry := definitions.New(weather.New(options...))

	result := tools.Invoke(t.Context(), use.New("call_1", "weather", args(t, `{"location":"Paris"}`)), registry)

	if got, want := result.ID(), "call_1"; got != want {
		t.Errorf("Result.ID() = %q, want %q", got, want)
	}
	if result.Failed() {
		t.Errorf("Result.Failed() = true, want false: %s", result.Content())
	}
	if !strings.Contains(result.Content(), "Paris") {
		t.Errorf("Result.Content() = %q, want it to name the place", result.Content())
	}
}

// TestConditionThroughReport walks the WMO codes the way the model sees them:
// through the report, which is the only place they are turned into words.
func TestConditionThroughReport(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "clear sky"},
		{1, "mainly clear"},
		{2, "partly cloudy"},
		{3, "overcast"},
		{45, "fog"},
		{48, "fog"},
		{53, "drizzle"},
		{57, "freezing drizzle"},
		{63, "rain"},
		{67, "freezing rain"},
		{73, "snow"},
		{77, "snow grains"},
		{81, "rain showers"},
		{86, "snow showers"},
		{95, "thunderstorm"},
		{99, "thunderstorm with hail"},
		// A code with no entry is reported as itself. Naming it wrongly
		// would be worse than not naming it.
		{42, "weather code 42"},
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			body := strings.Replace(forecast, `"weather_code":2`, `"weather_code":`+strconv.Itoa(c.code), 1)
			_, options := stub(t, paris, body, http.StatusOK)

			out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris"}`))
			if err != nil {
				t.Fatalf("Execute() error = %v, want nil", err)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("Execute() = %q, want it to contain %q", out, c.want)
			}
		})
	}
}

// TestReportSurvivesShortArrays pins that the daily arrays are read as the
// promise they are. They come back from a service, and a short one is read as
// absent rather than a panic.
func TestReportSurvivesShortArrays(t *testing.T) {
	body := strings.Replace(forecast,
		`"temperature_2m_min":[13.9,14.0,14.5]`,
		`"temperature_2m_min":[13.9]`, 1)
	_, options := stub(t, paris, body, http.StatusOK)

	out, err := weather.New(options...).Execute(t.Context(), args(t, `{"location":"Paris","days":3}`))
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !strings.Contains(out, "2026-08-24") {
		t.Errorf("Execute() = %q, want the third day reported anyway", out)
	}
}

func args(t *testing.T, s string) use.Arguments {
	t.Helper()
	return use.Arguments(s)
}
