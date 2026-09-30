package weather

import "math"

// wmoCodes maps WMO weather interpretation codes (as used by Open-Meteo and
// most numerical weather models) to short descriptions.
// See https://open-meteo.com/en/docs ("WMO Weather interpretation codes").
var wmoCodes = map[int]string{
	0:  "Clear sky",
	1:  "Mainly clear",
	2:  "Partly cloudy",
	3:  "Overcast",
	45: "Fog",
	48: "Depositing rime fog",
	51: "Light drizzle",
	53: "Moderate drizzle",
	55: "Dense drizzle",
	56: "Light freezing drizzle",
	57: "Dense freezing drizzle",
	61: "Slight rain",
	63: "Moderate rain",
	65: "Heavy rain",
	66: "Light freezing rain",
	67: "Heavy freezing rain",
	71: "Slight snowfall",
	73: "Moderate snowfall",
	75: "Heavy snowfall",
	77: "Snow grains",
	80: "Slight rain showers",
	81: "Moderate rain showers",
	82: "Violent rain showers",
	85: "Slight snow showers",
	86: "Heavy snow showers",
	95: "Thunderstorm",
	96: "Thunderstorm with slight hail",
	99: "Thunderstorm with heavy hail",
}

// Describe returns a description for a WMO weather code.
func Describe(code int) string {
	if d, ok := wmoCodes[code]; ok {
		return d
	}
	return "Unknown conditions"
}

var compassPoints = [...]string{
	"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
	"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW",
}

// Compass converts a bearing in degrees to a 16-point compass direction.
func Compass(deg int) string {
	d := math.Mod(float64(deg), 360)
	if d < 0 {
		d += 360
	}
	idx := int(math.Round(d/22.5)) % len(compassPoints)
	return compassPoints[idx]
}

// UVCategory returns the WHO category for a UV index value.
func UVCategory(uv float64) string {
	switch {
	case uv < 3:
		return "Low"
	case uv < 6:
		return "Moderate"
	case uv < 8:
		return "High"
	case uv < 11:
		return "Very high"
	default:
		return "Extreme"
	}
}
