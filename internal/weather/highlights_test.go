package weather

import (
	"strings"
	"testing"
	"time"
)

func TestHighlights(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	hours := func(probs ...int) []HourlyPoint {
		out := make([]HourlyPoint, len(probs))
		for i, p := range probs {
			out[i] = HourlyPoint{Time: start.Add(time.Duration(i) * time.Hour), PrecipProbability: p}
		}
		return out
	}

	t.Run("calm day has no highlights", func(t *testing.T) {
		r := Report{Current: Current{Temperature: 15, FeelsLike: 14}, Hourly: hours(0, 10, 20)}
		if got := Highlights(r); len(got) != 0 {
			t.Errorf("expected no highlights, got %v", got)
		}
	})

	t.Run("rain within lookahead", func(t *testing.T) {
		r := Report{Hourly: hours(0, 10, 70)}
		got := Highlights(r)
		if len(got) != 1 || !strings.Contains(got[0], "11:00") || !strings.Contains(got[0], "70%") {
			t.Errorf("unexpected highlights: %v", got)
		}
	})

	t.Run("rain beyond lookahead is ignored", func(t *testing.T) {
		probs := make([]int, rainLookahead+1)
		probs[rainLookahead] = 90
		if got := Highlights(Report{Hourly: hours(probs...)}); len(got) != 0 {
			t.Errorf("expected no highlights, got %v", got)
		}
	})

	t.Run("uv, wind and feels-like", func(t *testing.T) {
		r := Report{
			Current: Current{Temperature: 10, FeelsLike: 4, WindGusts: 65},
			Daily:   []DailyForecast{{UVIndexMax: 8.2}},
		}
		got := strings.Join(Highlights(r), "\n")
		for _, want := range []string{"Very high", "65 km/h", "Feels colder"} {
			if !strings.Contains(got, want) {
				t.Errorf("highlights missing %q:\n%s", want, got)
			}
		}
	})
}
