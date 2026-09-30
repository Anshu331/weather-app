// Package weather defines the provider-agnostic domain model used across the
// application. Nothing in this package knows about HTTP, JSON wire formats of
// any particular API, or persistence.
package weather

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// Location is a named place with coordinates.
type Location struct {
	Name        string  `json:"name"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"countryCode,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Timezone    string  `json:"timezone,omitempty"`
}

// Key identifies a location for caching and de-duplication. Coordinates are
// rounded to two decimal places (~1km), which is finer than the resolution of
// the forecast models while still merging near-identical searches.
func (l Location) Key() string {
	return fmt.Sprintf("%.2f,%.2f", l.Latitude, l.Longitude)
}

// Label is a human-readable description, e.g. "London, England, United Kingdom".
func (l Location) Label() string {
	parts := []string{l.Name}
	if l.Region != "" && l.Region != l.Name {
		parts = append(parts, l.Region)
	}
	if l.Country != "" {
		parts = append(parts, l.Country)
	}
	return strings.Join(parts, ", ")
}

// Validate checks that the location has a name and plausible coordinates.
func (l Location) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("%w: location name is required", ErrInvalidInput)
	}
	if math.IsNaN(l.Latitude) || l.Latitude < -90 || l.Latitude > 90 {
		return fmt.Errorf("%w: latitude must be between -90 and 90", ErrInvalidInput)
	}
	if math.IsNaN(l.Longitude) || l.Longitude < -180 || l.Longitude > 180 {
		return fmt.Errorf("%w: longitude must be between -180 and 180", ErrInvalidInput)
	}
	return nil
}

// Current holds the conditions observed/modelled right now.
type Current struct {
	Time          time.Time `json:"time"`
	Temperature   float64   `json:"temperatureC"`
	FeelsLike     float64   `json:"feelsLikeC"`
	Humidity      int       `json:"humidityPct"`
	Precipitation float64   `json:"precipitationMm"`
	CloudCover    int       `json:"cloudCoverPct"`
	Pressure      float64   `json:"pressureHpa"`
	WindSpeed     float64   `json:"windSpeedKmh"`
	WindGusts     float64   `json:"windGustsKmh"`
	WindDirection int       `json:"windDirectionDeg"`
	WeatherCode   int       `json:"weatherCode"`
	IsDay         bool      `json:"isDay"`
}

// Condition returns a text description of the current weather code.
func (c Current) Condition() string { return Describe(c.WeatherCode) }

// WindCompass returns the wind direction as a 16-point compass bearing.
func (c Current) WindCompass() string { return Compass(c.WindDirection) }

// HourlyPoint is a single hour of forecast.
type HourlyPoint struct {
	Time              time.Time `json:"time"`
	Temperature       float64   `json:"temperatureC"`
	PrecipProbability int       `json:"precipProbabilityPct"`
	WeatherCode       int       `json:"weatherCode"`
}

// Condition returns a text description of the hour's weather code.
func (h HourlyPoint) Condition() string { return Describe(h.WeatherCode) }

// DailyForecast summarises a single day.
type DailyForecast struct {
	Date              time.Time `json:"date"`
	TempMax           float64   `json:"tempMaxC"`
	TempMin           float64   `json:"tempMinC"`
	PrecipSum         float64   `json:"precipSumMm"`
	PrecipProbability int       `json:"precipProbabilityPct"`
	WindSpeedMax      float64   `json:"windSpeedMaxKmh"`
	UVIndexMax        float64   `json:"uvIndexMax"`
	Sunrise           time.Time `json:"sunrise"`
	Sunset            time.Time `json:"sunset"`
	WeatherCode       int       `json:"weatherCode"`
}

// Condition returns a text description of the day's weather code.
func (d DailyForecast) Condition() string { return Describe(d.WeatherCode) }

// UVLevel returns the WHO UV index category for the day's maximum.
func (d DailyForecast) UVLevel() string { return UVCategory(d.UVIndexMax) }

// Report is everything we know about the weather at a location at FetchedAt.
type Report struct {
	Location  Location        `json:"location"`
	Current   Current         `json:"current"`
	Hourly    []HourlyPoint   `json:"hourly"`
	Daily     []DailyForecast `json:"daily"`
	FetchedAt time.Time       `json:"fetchedAt"`
}

// Geocoder resolves free-text queries into candidate locations.
type Geocoder interface {
	Search(ctx context.Context, query string, limit int) ([]Location, error)
}

// Forecaster fetches a weather report for a location.
type Forecaster interface {
	Forecast(ctx context.Context, loc Location) (Report, error)
}

// Provider is a complete weather data source.
type Provider interface {
	Geocoder
	Forecaster
}
