package mock

import (
	"context"
	"testing"
	"time"
)

func TestSearchMatchesByPrefix(t *testing.T) {
	p := New()
	locs, _ := p.Search(context.Background(), "spring", 10)
	if len(locs) != 2 {
		t.Fatalf("expected two Springfields, got %d", len(locs))
	}
}

func TestForecastIsDeterministicAndComplete(t *testing.T) {
	fixed := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	p := &Provider{now: func() time.Time { return fixed }}
	loc := places[0]

	a, _ := p.Forecast(context.Background(), loc)
	b, _ := p.Forecast(context.Background(), loc)
	// Compare fields rather than structs: each call builds its own *time.Location.
	if !a.Current.Time.Equal(b.Current.Time) || a.Current.Temperature != b.Current.Temperature ||
		a.Current.WeatherCode != b.Current.WeatherCode {
		t.Error("forecast should be deterministic for the same location and time")
	}
	if len(a.Hourly) != 24 || len(a.Daily) != 7 {
		t.Errorf("got %d hours and %d days, want 24 and 7", len(a.Hourly), len(a.Daily))
	}
	for _, d := range a.Daily {
		if d.TempMin > d.TempMax {
			t.Errorf("min %.1f > max %.1f on %s", d.TempMin, d.TempMax, d.Date)
		}
	}
}
