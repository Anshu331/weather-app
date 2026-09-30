package openmeteo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Anshu331/weather-app/internal/weather"
)

// newTestClient starts a server that serves the given handler for both the
// geocoding and forecast endpoints.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	fixed := time.Date(2026, 9, 30, 21, 5, 0, 0, time.UTC)
	return New(
		WithBaseURLs(srv.URL+"/geo", srv.URL+"/forecast"),
		WithHTTPClient(srv.Client()),
		WithClock(func() time.Time { return fixed }),
	)
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func TestSearchParsesResults(t *testing.T) {
	var gotQuery string
	fixture := serveFile(t, "testdata/geocode_london.json")
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fixture(w, r)
	})

	locs, err := c.Search(context.Background(), "London", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(gotQuery, "name=London") || !strings.Contains(gotQuery, "count=3") {
		t.Errorf("unexpected query string %q", gotQuery)
	}
	if len(locs) != 3 {
		t.Fatalf("got %d locations, want 3", len(locs))
	}
	want := weather.Location{
		Name: "London", Region: "England", Country: "United Kingdom", CountryCode: "GB",
		Latitude: 51.50853, Longitude: -0.12574, Timezone: "Europe/London",
	}
	if locs[0] != want {
		t.Errorf("first location = %+v, want %+v", locs[0], want)
	}
	if locs[1].Country != "Canada" {
		t.Errorf("second location country = %q, want Canada", locs[1].Country)
	}
}

func TestSearchNoResultsIsNotAnError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"generationtime_ms":0.4}`))
	})
	locs, err := c.Search(context.Background(), "zzzzqqq", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected no locations, got %d", len(locs))
	}
}

func TestForecastParsesReport(t *testing.T) {
	c := newTestClient(t, serveFile(t, "testdata/forecast_london.json"))
	loc := weather.Location{Name: "London", Latitude: 51.51, Longitude: -0.13}

	r, err := c.Forecast(context.Background(), loc)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}

	if r.Location.Timezone != "Europe/London" {
		t.Errorf("timezone should be filled from response, got %q", r.Location.Timezone)
	}
	if r.Current.Temperature != 19.4 || r.Current.Humidity != 63 || r.Current.WeatherCode != 2 {
		t.Errorf("unexpected current conditions: %+v", r.Current)
	}
	if r.Current.IsDay {
		t.Error("expected night-time (is_day=0)")
	}
	if _, off := r.Current.Time.Zone(); off != 3600 {
		t.Errorf("current time offset = %d, want 3600", off)
	}
	if got := r.Current.Time.Format(localTimeLayout); got != "2026-09-30T22:00" {
		t.Errorf("current time = %s", got)
	}

	if len(r.Hourly) != hoursAhead {
		t.Fatalf("got %d hourly points, want %d", len(r.Hourly), hoursAhead)
	}
	if !r.Hourly[0].Time.Equal(r.Current.Time) {
		t.Errorf("hourly should start at the current hour, starts at %s", r.Hourly[0].Time)
	}

	if len(r.Daily) != 3 {
		t.Fatalf("got %d days, want 3", len(r.Daily))
	}
	if r.Daily[0].Sunrise.IsZero() || !r.Daily[0].Sunset.After(r.Daily[0].Sunrise) {
		t.Errorf("unexpected sunrise/sunset: %s / %s", r.Daily[0].Sunrise, r.Daily[0].Sunset)
	}
	if !r.FetchedAt.Equal(time.Date(2026, 9, 30, 21, 5, 0, 0, time.UTC)) {
		t.Errorf("FetchedAt = %s, want injected clock", r.FetchedAt)
	}
}

func TestUpstreamFailuresWrapErrUpstream(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantMsg string
	}{
		{
			name: "api error with reason",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":true,"reason":"Latitude must be in range"}`))
			},
			wantMsg: "Latitude must be in range",
		},
		{
			name: "server error without body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			wantMsg: "unexpected status 502",
		},
		{
			name: "invalid json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`<html>not json</html>`))
			},
			wantMsg: "decoding response",
		},
		{
			name: "malformed arrays",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"current":{"time":"2026-09-30T22:00"},
					"hourly":{"time":["2026-09-30T22:00"],"temperature_2m":[]}}`))
			},
			wantMsg: "hourly arrays differ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.handler)
			_, err := c.Forecast(context.Background(), weather.Location{Name: "X", Latitude: 1, Longitude: 1})
			if !errors.Is(err, weather.ErrUpstream) {
				t.Fatalf("expected ErrUpstream, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should mention %q", err, tt.wantMsg)
			}
		})
	}
}

func TestTimeoutIsReportedAsUpstreamError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Search(ctx, "London", 1)
	if !errors.Is(err, weather.ErrUpstream) {
		t.Fatalf("expected ErrUpstream on timeout, got %v", err)
	}
}
