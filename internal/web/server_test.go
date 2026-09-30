package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Anshu331/weather-app/internal/service"
	"github.com/Anshu331/weather-app/internal/weather"
)

type stubService struct {
	search    service.SearchResult
	searchErr error
	weather   service.WeatherResult
	weatherFn func(weather.Location) (service.WeatherResult, error)
	recent    []weather.Location
}

func (s *stubService) Search(_ context.Context, q string) (service.SearchResult, error) {
	if s.searchErr != nil {
		return service.SearchResult{}, s.searchErr
	}
	if _, err := service.NormalizeQuery(q); err != nil {
		return service.SearchResult{}, err
	}
	return s.search, nil
}

func (s *stubService) Weather(_ context.Context, loc weather.Location) (service.WeatherResult, error) {
	if s.weatherFn != nil {
		return s.weatherFn(loc)
	}
	return s.weather, nil
}

func (s *stubService) Recent() []weather.Location { return s.recent }

var (
	london   = weather.Location{Name: "London", Region: "England", Country: "United Kingdom", Latitude: 51.5085, Longitude: -0.1257}
	londonCA = weather.Location{Name: "London", Region: "Ontario", Country: "Canada", Latitude: 42.9834, Longitude: -81.233}
)

func sampleResult(f service.Freshness, warning string) service.WeatherResult {
	now := time.Date(2026, 10, 1, 14, 0, 0, 0, time.FixedZone("BST", 3600))
	return service.WeatherResult{
		Report: weather.Report{
			Location: london,
			Current:  weather.Current{Time: now, Temperature: 17.6, FeelsLike: 16.9, WeatherCode: 3, WindDirection: 225},
			Hourly:   []weather.HourlyPoint{{Time: now, Temperature: 17.6, PrecipProbability: 80, WeatherCode: 61}},
			Daily:    []weather.DailyForecast{{Date: now, TempMax: 19, TempMin: 11, Sunrise: now.Add(-7 * time.Hour), Sunset: now.Add(5 * time.Hour)}},
		},
		Freshness: f,
		AgeText:   "3 minutes",
		Warning:   warning,
	}
}

func newTestServer(t *testing.T, svc *stubService) http.Handler {
	t.Helper()
	srv, err := NewServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv.Handler()
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func assertContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("response body missing %q", w)
		}
	}
}

func TestIndexShowsRecentSearches(t *testing.T) {
	h := newTestServer(t, &stubService{recent: []weather.Location{london}})
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Recent searches", "London, England, United Kingdom", "/weather?")
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("expected security headers")
	}
}

func TestSearchValidationError(t *testing.T) {
	h := newTestServer(t, &stubService{})
	rec := get(t, h, "/search?q=+")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertContains(t, rec.Body.String(), "please enter a location")
}

func TestSearchSingleResultRedirects(t *testing.T) {
	h := newTestServer(t, &stubService{search: service.SearchResult{Query: "London", Locations: []weather.Location{london}}})
	rec := get(t, h, "/search?q=London")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/weather" || loc.Query().Get("lat") != "51.5085" {
		t.Errorf("unexpected redirect target %q", rec.Header().Get("Location"))
	}
}

func TestSearchMultipleResultsListsChoices(t *testing.T) {
	h := newTestServer(t, &stubService{search: service.SearchResult{Query: "London", Locations: []weather.Location{london, londonCA}}})
	rec := get(t, h, "/search?q=London")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Choose a location", "England", "Ontario, Canada")
}

func TestSearchNoResults(t *testing.T) {
	h := newTestServer(t, &stubService{search: service.SearchResult{Query: "Qwxyz"}})
	rec := get(t, h, "/search?q=Qwxyz")
	assertContains(t, rec.Body.String(), "No places found")
}

func TestSearchEscapesUserInput(t *testing.T) {
	q := `<script>alert(1)</script>`
	h := newTestServer(t, &stubService{search: service.SearchResult{Query: q}})
	rec := get(t, h, "/search?q="+url.QueryEscape(q))
	if strings.Contains(rec.Body.String(), q) {
		t.Error("user input must be HTML-escaped")
	}
}

func TestWeatherPageRendersReport(t *testing.T) {
	h := newTestServer(t, &stubService{weather: sampleResult(service.FreshLive, "")})
	rec := get(t, h, weatherURL(london))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body:\n%s", rec.Code, rec.Body.String())
	}
	assertContains(t, rec.Body.String(),
		"18°C", "Overcast", "Feels like 17°C", "SW", "Next 24 hours", "1-day forecast",
		"take an umbrella", "Updated", "14:00")
	if strings.Contains(rec.Body.String(), "banner warning") {
		t.Error("live data should not show a warning banner")
	}
}

func TestWeatherPageShowsStaleWarning(t *testing.T) {
	warning := "Live weather data is temporarily unavailable. Showing the last known conditions from 2 hours ago."
	h := newTestServer(t, &stubService{weather: sampleResult(service.FreshStale, warning)})
	rec := get(t, h, weatherURL(london))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertContains(t, rec.Body.String(), "banner warning", "from 2 hours ago", "Saved data")
}

func TestWeatherPageHandlesEmptyForecast(t *testing.T) {
	res := sampleResult(service.FreshLive, "")
	res.Report.Hourly, res.Report.Daily = nil, nil
	h := newTestServer(t, &stubService{weather: res})
	if rec := get(t, h, weatherURL(london)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with partial data", rec.Code)
	}
}

func TestWeatherInvalidParams(t *testing.T) {
	h := newTestServer(t, &stubService{})
	for _, target := range []string{"/weather", "/weather?lat=abc&lon=1", "/weather?lat=91&lon=0&name=X"} {
		if rec := get(t, h, target); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", target, rec.Code)
		}
	}
}

func TestWeatherUpstreamUnavailable(t *testing.T) {
	svc := &stubService{weatherFn: func(weather.Location) (service.WeatherResult, error) {
		return service.WeatherResult{}, fmt.Errorf("%w: dial tcp: connection refused", weather.ErrUpstream)
	}}
	rec := get(t, newTestServer(t, svc), weatherURL(london))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "temporarily unavailable")
	if strings.Contains(body, "connection refused") {
		t.Error("internal error details must not leak to users")
	}
}

func TestAPIWeather(t *testing.T) {
	h := newTestServer(t, &stubService{weather: sampleResult(service.FreshCached, "")})
	rec := get(t, h, "/api/weather?lat=51.5&lon=-0.12&name=London")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body apiWeatherResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Freshness != "cached" || body.Condition != "Overcast" || body.Report.Current.Temperature != 17.6 {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestAPIErrorsAreJSON(t *testing.T) {
	svc := &stubService{searchErr: fmt.Errorf("%w: boom", weather.ErrUpstream)}
	h := newTestServer(t, svc)

	tests := []struct {
		target string
		status int
		code   string
	}{
		{"/api/weather?lat=x&lon=1", http.StatusBadRequest, "invalid_input"},
		{"/api/search?q=London", http.StatusServiceUnavailable, "upstream_unavailable"},
	}
	for _, tt := range tests {
		rec := get(t, h, tt.target)
		if rec.Code != tt.status {
			t.Errorf("GET %s status = %d, want %d", tt.target, rec.Code, tt.status)
		}
		var body struct{ Error apiError }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tt.code {
			t.Errorf("GET %s body = %s, want code %q", tt.target, rec.Body.String(), tt.code)
		}
	}
}

func TestNotFoundAndHealth(t *testing.T) {
	h := newTestServer(t, &stubService{})
	if rec := get(t, h, "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", rec.Code)
	}
	if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, h, "/static/style.css"); rec.Code != http.StatusOK {
		t.Errorf("static asset status = %d", rec.Code)
	}
}

func TestPanicIsRecovered(t *testing.T) {
	svc := &stubService{weatherFn: func(weather.Location) (service.WeatherResult, error) { panic("boom") }}
	rec := get(t, newTestServer(t, svc), weatherURL(london))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestLocationRoundTripsThroughURL(t *testing.T) {
	in := weather.Location{Name: "São Paulo", Region: "São Paulo", Country: "Brazil", CountryCode: "BR",
		Latitude: -23.5475, Longitude: -46.6361, Timezone: "America/Sao_Paulo"}
	u, _ := url.Parse(weatherURL(in))
	out, err := locationFromQuery(u.Query())
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}
