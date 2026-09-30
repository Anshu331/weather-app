package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Anshu331/weather-app/internal/weather"
)

// Locations are passed between pages as query parameters rather than an ID.
// That keeps URLs bookmarkable and shareable, and lets the weather page work
// (from cache) even when the geocoding service is down, because no lookup is
// needed to turn an ID back into coordinates.

const maxParamLen = 100

// weatherURL builds a link to the weather page for loc.
func weatherURL(loc weather.Location) string {
	return "/weather?" + locationValues(loc).Encode()
}

func locationValues(loc weather.Location) url.Values {
	v := url.Values{}
	v.Set("name", loc.Name)
	v.Set("lat", strconv.FormatFloat(loc.Latitude, 'f', 4, 64))
	v.Set("lon", strconv.FormatFloat(loc.Longitude, 'f', 4, 64))
	setIf(v, "region", loc.Region)
	setIf(v, "country", loc.Country)
	setIf(v, "cc", loc.CountryCode)
	setIf(v, "tz", loc.Timezone)
	return v
}

func setIf(v url.Values, k, val string) {
	if val != "" {
		v.Set(k, val)
	}
}

// locationFromQuery parses and validates a location from query parameters.
func locationFromQuery(q url.Values) (weather.Location, error) {
	lat, err := parseCoord(q.Get("lat"), "lat")
	if err != nil {
		return weather.Location{}, err
	}
	lon, err := parseCoord(q.Get("lon"), "lon")
	if err != nil {
		return weather.Location{}, err
	}
	loc := weather.Location{
		Name:        param(q, "name"),
		Region:      param(q, "region"),
		Country:     param(q, "country"),
		CountryCode: param(q, "cc"),
		Timezone:    param(q, "tz"),
		Latitude:    lat,
		Longitude:   lon,
	}
	if loc.Name == "" {
		loc.Name = fmt.Sprintf("%.2f, %.2f", lat, lon)
	}
	return loc, loc.Validate()
}

func parseCoord(s, name string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: %s is required", weather.ErrInvalidInput, name)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a number", weather.ErrInvalidInput, name)
	}
	return f, nil
}

// param returns a trimmed, length-limited query parameter.
func param(q url.Values, key string) string {
	s := strings.TrimSpace(q.Get(key))
	if r := []rune(s); len(r) > maxParamLen {
		s = string(r[:maxParamLen])
	}
	return s
}
