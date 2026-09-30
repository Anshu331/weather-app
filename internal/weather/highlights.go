package weather

import "fmt"

const (
	rainLikelyPct  = 50
	rainLookahead  = 12 // hours
	strongGustsKmh = 50
	highUVIndex    = 6
	feelsLikeDelta = 3 // °C
)

// Highlights derives short, practical notes from a report, e.g. whether to
// take an umbrella. The rules are intentionally simple and conservative.
func Highlights(r Report) []string {
	var out []string

	for i, h := range r.Hourly {
		if i >= rainLookahead {
			break
		}
		if h.PrecipProbability >= rainLikelyPct {
			out = append(out, fmt.Sprintf("Rain likely around %s (%d%% chance) - take an umbrella.",
				h.Time.Format("15:04"), h.PrecipProbability))
			break
		}
	}

	if len(r.Daily) > 0 && r.Daily[0].UVIndexMax >= highUVIndex {
		out = append(out, fmt.Sprintf("UV index reaches %.0f (%s) today - use sun protection.",
			r.Daily[0].UVIndexMax, UVCategory(r.Daily[0].UVIndexMax)))
	}

	if r.Current.WindGusts >= strongGustsKmh {
		out = append(out, fmt.Sprintf("Strong gusts of up to %.0f km/h.", r.Current.WindGusts))
	}

	if diff := r.Current.FeelsLike - r.Current.Temperature; diff <= -feelsLikeDelta || diff >= feelsLikeDelta {
		word := "colder"
		if diff > 0 {
			word = "warmer"
		}
		out = append(out, fmt.Sprintf("Feels %s than the actual temperature (%.0f°C vs %.0f°C).",
			word, r.Current.FeelsLike, r.Current.Temperature))
	}

	return out
}
