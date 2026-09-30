// Package mock provides a deterministic, offline weather.Provider. It is used
// for demos without network access and for manual UI testing. Data is
// synthetic but plausible, and stable for a given location and hour.
package mock

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/Anshu331/weather-app/internal/weather"
)

var places = []weather.Location{
	{Name: "London", Region: "England", Country: "United Kingdom", CountryCode: "GB", Latitude: 51.5085, Longitude: -0.1257, Timezone: "Europe/London"},
	{Name: "London", Region: "Ontario", Country: "Canada", CountryCode: "CA", Latitude: 42.9834, Longitude: -81.2330, Timezone: "America/Toronto"},
	{Name: "Manchester", Region: "England", Country: "United Kingdom", CountryCode: "GB", Latitude: 53.4809, Longitude: -2.2374, Timezone: "Europe/London"},
	{Name: "Edinburgh", Region: "Scotland", Country: "United Kingdom", CountryCode: "GB", Latitude: 55.9521, Longitude: -3.1965, Timezone: "Europe/London"},
	{Name: "Paris", Region: "Île-de-France", Country: "France", CountryCode: "FR", Latitude: 48.8534, Longitude: 2.3488, Timezone: "Europe/Paris"},
	{Name: "New York", Region: "New York", Country: "United States", CountryCode: "US", Latitude: 40.7143, Longitude: -74.0060, Timezone: "America/New_York"},
	{Name: "Springfield", Region: "Illinois", Country: "United States", CountryCode: "US", Latitude: 39.8017, Longitude: -89.6437, Timezone: "America/Chicago"},
	{Name: "Springfield", Region: "Missouri", Country: "United States", CountryCode: "US", Latitude: 37.2153, Longitude: -93.2982, Timezone: "America/Chicago"},
	{Name: "Tokyo", Region: "Tokyo", Country: "Japan", CountryCode: "JP", Latitude: 35.6895, Longitude: 139.6917, Timezone: "Asia/Tokyo"},
	{Name: "Sydney", Region: "New South Wales", Country: "Australia", CountryCode: "AU", Latitude: -33.8679, Longitude: 151.2073, Timezone: "Australia/Sydney"},
	{Name: "Mumbai", Region: "Maharashtra", Country: "India", CountryCode: "IN", Latitude: 19.0728, Longitude: 72.8826, Timezone: "Asia/Kolkata"},
}

// Provider is an offline weather.Provider.
type Provider struct {
	now func() time.Time
}

// New returns a mock provider using the real clock.
func New() *Provider { return &Provider{now: time.Now} }

var _ weather.Provider = (*Provider)(nil)

// Search matches the query against a small built-in list of places.
func (p *Provider) Search(_ context.Context, query string, limit int) ([]weather.Location, error) {
	q := strings.ToLower(query)
	var out []weather.Location
	for _, pl := range places {
		if strings.HasPrefix(strings.ToLower(pl.Name), q) {
			out = append(out, pl)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// Forecast synthesises a report whose values vary smoothly with the hour of
// day and are seeded by the location, so each place looks different.
func (p *Provider) Forecast(_ context.Context, loc weather.Location) (weather.Report, error) {
	zone := time.FixedZone("local", int(math.Round(loc.Longitude/15))*3600)
	now := p.now().In(zone)
	hour := now.Truncate(time.Hour)
	seed := seedFor(loc)

	base := 25 - math.Abs(loc.Latitude)*0.35 + float64(seed%7) - 3
	tempAt := func(t time.Time) float64 {
		// Warmest around 15:00, coolest around 03:00.
		phase := (float64(t.Hour()) - 9) / 24 * 2 * math.Pi
		return round1(base + 5*math.Sin(phase) + float64(t.YearDay()%5) - 2)
	}
	codeAt := func(t time.Time) int {
		codes := []int{0, 1, 2, 3, 3, 61, 80, 2, 45, 63}
		return codes[(int(seed)+t.YearDay()*3+t.Hour()/6)%len(codes)]
	}
	rainAt := func(t time.Time) int {
		switch c := codeAt(t); {
		case c >= 61:
			return 60 + int(seed%30)
		case c == 3 || c == 45:
			return 20
		default:
			return 5
		}
	}

	t0 := tempAt(hour)
	r := weather.Report{
		Location: loc,
		Current: weather.Current{
			Time:          hour,
			Temperature:   t0,
			FeelsLike:     round1(t0 - 1.5),
			Humidity:      55 + int(seed%35),
			Precipitation: 0,
			CloudCover:    int(seed % 100),
			Pressure:      1005 + float64(seed%20),
			WindSpeed:     float64(5 + seed%25),
			WindGusts:     float64(12 + seed%40),
			WindDirection: int(seed % 360),
			WeatherCode:   codeAt(hour),
			IsDay:         hour.Hour() >= 7 && hour.Hour() < 19,
		},
		FetchedAt: p.now().UTC(),
	}

	for i := 0; i < 24; i++ {
		t := hour.Add(time.Duration(i) * time.Hour)
		r.Hourly = append(r.Hourly, weather.HourlyPoint{
			Time: t, Temperature: tempAt(t), PrecipProbability: rainAt(t), WeatherCode: codeAt(t),
		})
	}

	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	for i := 0; i < 7; i++ {
		d := day.AddDate(0, 0, i)
		noon := d.Add(12 * time.Hour)
		r.Daily = append(r.Daily, weather.DailyForecast{
			Date:              d,
			TempMax:           tempAt(d.Add(15 * time.Hour)),
			TempMin:           tempAt(d.Add(3 * time.Hour)),
			PrecipSum:         float64(rainAt(noon)) / 20,
			PrecipProbability: rainAt(noon),
			WindSpeedMax:      float64(10 + (seed+uint32(i)*7)%30),
			UVIndexMax:        math.Max(0, round1(8-math.Abs(loc.Latitude)/10+float64(i%3))),
			Sunrise:           d.Add(6*time.Hour + 30*time.Minute),
			Sunset:            d.Add(18*time.Hour + 45*time.Minute),
			WeatherCode:       codeAt(noon),
		})
	}
	return r, nil
}

func seedFor(loc weather.Location) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(loc.Key()))
	return h.Sum32()
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
