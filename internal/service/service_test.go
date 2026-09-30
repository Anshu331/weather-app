package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Anshu331/weather-app/internal/store"
	"github.com/Anshu331/weather-app/internal/weather"
)

// fakeProvider is a controllable weather.Provider.
type fakeProvider struct {
	searchResult  []weather.Location
	searchErr     error
	forecastErr   error
	forecastCalls int
	now           func() time.Time
}

func (f *fakeProvider) Search(_ context.Context, _ string, _ int) ([]weather.Location, error) {
	return f.searchResult, f.searchErr
}

func (f *fakeProvider) Forecast(_ context.Context, loc weather.Location) (weather.Report, error) {
	f.forecastCalls++
	if f.forecastErr != nil {
		return weather.Report{}, f.forecastErr
	}
	now := f.now()
	r := weather.Report{
		Location:  loc,
		Current:   weather.Current{Time: now, Temperature: 20},
		FetchedAt: now,
	}
	for i := 0; i < 24; i++ {
		r.Hourly = append(r.Hourly, weather.HourlyPoint{Time: now.Add(time.Duration(i) * time.Hour)})
	}
	for i := 0; i < 3; i++ {
		r.Daily = append(r.Daily, weather.DailyForecast{Date: now.Truncate(24*time.Hour).AddDate(0, 0, i)})
	}
	return r, nil
}

// clock is a manually advanced clock.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

type fixture struct {
	svc      *Service
	provider *fakeProvider
	clock    *clock
	history  *store.RecentSearches
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	p := &fakeProvider{now: clk.Now}
	cache, _ := store.OpenReportCache("", 100)
	hist, _ := store.OpenRecentSearches("", 10)
	cfg := Config{FreshFor: 10 * time.Minute, MaxStale: 6 * time.Hour, SearchLimit: 5}
	svc := New(p, cache, hist, cfg,
		WithClock(clk.Now),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	return &fixture{svc: svc, provider: p, clock: clk, history: hist}
}

var london = weather.Location{Name: "London", Country: "United Kingdom", Latitude: 51.51, Longitude: -0.13}

var errDown = fmt.Errorf("%w: connection refused", weather.ErrUpstream)

func TestWeatherLiveThenCached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	r, err := f.svc.Weather(ctx, london)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if r.Freshness != FreshLive {
		t.Errorf("first call freshness = %s, want live", r.Freshness)
	}

	f.clock.Advance(5 * time.Minute)
	r, err = f.svc.Weather(ctx, london)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if r.Freshness != FreshCached {
		t.Errorf("second call freshness = %s, want cached", r.Freshness)
	}
	if r.Age != 5*time.Minute {
		t.Errorf("age = %s, want 5m", r.Age)
	}
	if f.provider.forecastCalls != 1 {
		t.Errorf("provider called %d times, want 1 (second call should hit cache)", f.provider.forecastCalls)
	}
}

func TestWeatherRefreshesAfterFreshWindow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Weather(ctx, london)

	f.clock.Advance(11 * time.Minute)
	r, err := f.svc.Weather(ctx, london)
	if err != nil {
		t.Fatal(err)
	}
	if r.Freshness != FreshLive || f.provider.forecastCalls != 2 {
		t.Errorf("expected a live refresh, got freshness=%s calls=%d", r.Freshness, f.provider.forecastCalls)
	}
}

func TestWeatherFallsBackToStaleCacheWhenProviderFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Weather(ctx, london)

	f.clock.Advance(2 * time.Hour)
	f.provider.forecastErr = errDown

	r, err := f.svc.Weather(ctx, london)
	if err != nil {
		t.Fatalf("expected stale data, got error: %v", err)
	}
	if !r.Stale() {
		t.Errorf("freshness = %s, want stale", r.Freshness)
	}
	if !strings.Contains(r.Warning, "2 hours ago") {
		t.Errorf("warning should tell the user how old the data is: %q", r.Warning)
	}
	if r.Report.Current.Temperature != 20 {
		t.Errorf("expected cached report contents, got %+v", r.Report.Current)
	}
}

func TestStaleReportDropsElapsedForecastPeriods(t *testing.T) {
	f := newFixture(t) // clock starts at 12:00
	ctx := context.Background()
	_, _ = f.svc.Weather(ctx, london)

	f.clock.Advance(5*time.Hour + 30*time.Minute) // 17:30
	f.provider.forecastErr = errDown

	r, err := f.svc.Weather(ctx, london)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Report.Hourly[0].Time.Hour(); got != 17 {
		t.Errorf("first hourly point should be the current hour (17:00), got %02d:00", got)
	}
	if len(r.Report.Hourly) != 19 {
		t.Errorf("got %d hourly points, want 19", len(r.Report.Hourly))
	}
	if len(r.Report.Daily) != 3 {
		t.Errorf("today has not ended, so all 3 days should remain; got %d", len(r.Report.Daily))
	}
}

func TestWeatherRefusesDataOlderThanMaxStale(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _ = f.svc.Weather(ctx, london)

	f.clock.Advance(7 * time.Hour)
	f.provider.forecastErr = errDown

	_, err := f.svc.Weather(ctx, london)
	if !errors.Is(err, weather.ErrUpstream) {
		t.Fatalf("expected ErrUpstream, got %v", err)
	}
}

func TestWeatherWithNoCacheReturnsProviderError(t *testing.T) {
	f := newFixture(t)
	f.provider.forecastErr = errDown

	_, err := f.svc.Weather(context.Background(), london)
	if !errors.Is(err, weather.ErrUpstream) {
		t.Fatalf("expected ErrUpstream, got %v", err)
	}
	if len(f.history.List()) != 0 {
		t.Error("failed lookups should not be recorded as recent searches")
	}
}

func TestWeatherValidatesLocationBeforeCallingProvider(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Weather(context.Background(), weather.Location{Name: "Bad", Latitude: 123})
	if !errors.Is(err, weather.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if f.provider.forecastCalls != 0 {
		t.Error("provider should not be called for invalid input")
	}
}

func TestWeatherRecordsRecentSearches(t *testing.T) {
	f := newFixture(t)
	paris := weather.Location{Name: "Paris", Latitude: 48.85, Longitude: 2.35}
	_, _ = f.svc.Weather(context.Background(), london)
	_, _ = f.svc.Weather(context.Background(), paris)

	recent := f.svc.Recent()
	if len(recent) != 2 || recent[0].Name != "Paris" || recent[1].Name != "London" {
		t.Errorf("unexpected recent searches: %+v", recent)
	}
}

func TestSearchValidation(t *testing.T) {
	f := newFixture(t)
	for _, q := range []string{"", "   ", "a", strings.Repeat("x", MaxQueryLen+1), "Lon\x00don"} {
		if _, err := f.svc.Search(context.Background(), q); !errors.Is(err, weather.ErrInvalidInput) {
			t.Errorf("Search(%q) error = %v, want ErrInvalidInput", q, err)
		}
	}
}

func TestNormalizeQueryCollapsesWhitespace(t *testing.T) {
	got, err := NormalizeQuery("  New    York \t")
	if err != nil || got != "New York" {
		t.Errorf("NormalizeQuery = %q, %v; want \"New York\"", got, err)
	}
}

func TestSearchReturnsProviderResults(t *testing.T) {
	f := newFixture(t)
	f.provider.searchResult = []weather.Location{london}

	res, err := f.svc.Search(context.Background(), "London")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Locations) != 1 || res.Warning != "" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestSearchFallsBackToRecentSearchesWhenProviderFails(t *testing.T) {
	f := newFixture(t)
	_, _ = f.svc.Weather(context.Background(), london)
	f.provider.searchErr = errDown

	res, err := f.svc.Search(context.Background(), "lond")
	if err != nil {
		t.Fatalf("expected fallback results, got %v", err)
	}
	if len(res.Locations) != 1 || res.Locations[0].Name != "London" {
		t.Errorf("unexpected fallback locations: %+v", res.Locations)
	}
	if res.Warning == "" {
		t.Error("fallback results must carry a warning")
	}

	if _, err := f.svc.Search(context.Background(), "Tokyo"); !errors.Is(err, weather.ErrUpstream) {
		t.Errorf("no fallback match should surface the upstream error, got %v", err)
	}
}

func TestFormatAge(t *testing.T) {
	tests := map[time.Duration]string{
		10 * time.Second: "less than a minute",
		90 * time.Second: "1 minute",
		45 * time.Minute: "45 minutes",
		61 * time.Minute: "1 hour",
		5 * time.Hour:    "5 hours",
		72 * time.Hour:   "3 days",
	}
	for d, want := range tests {
		if got := FormatAge(d); got != want {
			t.Errorf("FormatAge(%s) = %q, want %q", d, got, want)
		}
	}
}
