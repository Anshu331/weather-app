// Package openmeteo implements weather.Provider on top of the free Open-Meteo
// geocoding and forecast APIs (https://open-meteo.com). No API key required.
package openmeteo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Anshu331/weather-app/internal/weather"
)

const (
	DefaultGeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	DefaultForecastURL  = "https://api.open-meteo.com/v1/forecast"

	// maxBodyBytes guards against unexpectedly large (or malicious) responses.
	maxBodyBytes = 2 << 20
	hoursAhead   = 24
	forecastDays = 7

	currentFields = "temperature_2m,relative_humidity_2m,apparent_temperature,is_day,precipitation," +
		"weather_code,cloud_cover,pressure_msl,wind_speed_10m,wind_direction_10m,wind_gusts_10m"
	hourlyFields = "temperature_2m,precipitation_probability,weather_code"
	dailyFields  = "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum," +
		"precipitation_probability_max,wind_speed_10m_max,uv_index_max,sunrise,sunset"
)

// Client talks to Open-Meteo. The zero value is not usable; use New.
type Client struct {
	httpClient   *http.Client
	geocodingURL string
	forecastURL  string
	now          func() time.Time
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (e.g. to change timeouts).
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// WithBaseURLs overrides the API endpoints (used by tests and to point at a
// self-hosted Open-Meteo instance).
func WithBaseURLs(geocoding, forecast string) Option {
	return func(c *Client) {
		c.geocodingURL = geocoding
		c.forecastURL = forecast
	}
}

// WithClock overrides the clock used to stamp Report.FetchedAt.
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// New returns a Client with sensible defaults.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		geocodingURL: DefaultGeocodingURL,
		forecastURL:  DefaultForecastURL,
		now:          time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

var _ weather.Provider = (*Client)(nil)

// Search resolves a place name into up to limit candidate locations.
// It returns an empty slice (not an error) when nothing matches.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]weather.Location, error) {
	q := url.Values{}
	q.Set("name", query)
	q.Set("count", strconv.Itoa(limit))
	q.Set("language", "en")
	q.Set("format", "json")

	var resp geocodeResponse
	if err := c.getJSON(ctx, c.geocodingURL, q, &resp); err != nil {
		return nil, fmt.Errorf("geocoding %q: %w", query, err)
	}

	locs := make([]weather.Location, 0, len(resp.Results))
	for _, r := range resp.Results {
		locs = append(locs, weather.Location{
			Name:        r.Name,
			Region:      r.Admin1,
			Country:     r.Country,
			CountryCode: r.CountryCode,
			Latitude:    r.Latitude,
			Longitude:   r.Longitude,
			Timezone:    r.Timezone,
		})
	}
	return locs, nil
}

// Forecast fetches current conditions, the next 24 hours and a 7-day outlook.
func (c *Client) Forecast(ctx context.Context, loc weather.Location) (weather.Report, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', 4, 64))
	q.Set("current", currentFields)
	q.Set("hourly", hourlyFields)
	q.Set("daily", dailyFields)
	q.Set("timezone", "auto")
	q.Set("forecast_days", strconv.Itoa(forecastDays))

	var resp forecastResponse
	if err := c.getJSON(ctx, c.forecastURL, q, &resp); err != nil {
		return weather.Report{}, fmt.Errorf("forecast for %s: %w", loc.Key(), err)
	}

	report, err := resp.toReport(loc)
	if err != nil {
		return weather.Report{}, fmt.Errorf("forecast for %s: %w: %v", loc.Key(), weather.ErrUpstream, err)
	}
	report.FetchedAt = c.now().UTC()
	return report, nil
}

// getJSON performs a GET and decodes a JSON body into dst. All transport,
// status and decoding failures are wrapped in weather.ErrUpstream so callers
// can treat them uniformly as "the service is unavailable".
func (c *Client) getJSON(ctx context.Context, base string, q url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: building request: %v", weather.ErrUpstream, err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", weather.ErrUpstream, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("%w: reading response: %v", weather.ErrUpstream, err)
	}

	if res.StatusCode != http.StatusOK {
		var apiErr apiError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Reason != "" {
			return fmt.Errorf("%w: status %d: %s", weather.ErrUpstream, res.StatusCode, apiErr.Reason)
		}
		return fmt.Errorf("%w: unexpected status %d", weather.ErrUpstream, res.StatusCode)
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("%w: decoding response: %v", weather.ErrUpstream, err)
	}
	return nil
}

// --- wire types --------------------------------------------------------------

type apiError struct {
	Error  bool   `json:"error"`
	Reason string `json:"reason"`
}

type geocodeResponse struct {
	Results []struct {
		Name        string  `json:"name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		CountryCode string  `json:"country_code"`
		Country     string  `json:"country"`
		Admin1      string  `json:"admin1"`
		Timezone    string  `json:"timezone"`
	} `json:"results"`
}

type forecastResponse struct {
	Timezone         string `json:"timezone"`
	UTCOffsetSeconds int    `json:"utc_offset_seconds"`
	Current          struct {
		Time                string  `json:"time"`
		Temperature2m       float64 `json:"temperature_2m"`
		RelativeHumidity2m  float64 `json:"relative_humidity_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		IsDay               int     `json:"is_day"`
		Precipitation       float64 `json:"precipitation"`
		WeatherCode         int     `json:"weather_code"`
		CloudCover          float64 `json:"cloud_cover"`
		PressureMSL         float64 `json:"pressure_msl"`
		WindSpeed10m        float64 `json:"wind_speed_10m"`
		WindDirection10m    float64 `json:"wind_direction_10m"`
		WindGusts10m        float64 `json:"wind_gusts_10m"`
	} `json:"current"`
	Hourly struct {
		Time                     []string  `json:"time"`
		Temperature2m            []float64 `json:"temperature_2m"`
		PrecipitationProbability []float64 `json:"precipitation_probability"`
		WeatherCode              []int     `json:"weather_code"`
	} `json:"hourly"`
	Daily struct {
		Time                        []string  `json:"time"`
		WeatherCode                 []int     `json:"weather_code"`
		Temperature2mMax            []float64 `json:"temperature_2m_max"`
		Temperature2mMin            []float64 `json:"temperature_2m_min"`
		PrecipitationSum            []float64 `json:"precipitation_sum"`
		PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
		WindSpeed10mMax             []float64 `json:"wind_speed_10m_max"`
		UVIndexMax                  []float64 `json:"uv_index_max"`
		Sunrise                     []string  `json:"sunrise"`
		Sunset                      []string  `json:"sunset"`
	} `json:"daily"`
}

const (
	localTimeLayout = "2006-01-02T15:04"
	localDateLayout = "2006-01-02"
)

var errMalformed = errors.New("malformed forecast data")

// toReport converts the wire format to the domain model. Open-Meteo returns
// parallel arrays and local times without an offset, so we attach the
// location's UTC offset to every timestamp.
func (r forecastResponse) toReport(loc weather.Location) (weather.Report, error) {
	zone := time.FixedZone(r.Timezone, r.UTCOffsetSeconds)
	if loc.Timezone == "" {
		loc.Timezone = r.Timezone
	}

	now, err := time.ParseInLocation(localTimeLayout, r.Current.Time, zone)
	if err != nil {
		return weather.Report{}, fmt.Errorf("%w: current time %q", errMalformed, r.Current.Time)
	}

	c := r.Current
	report := weather.Report{
		Location: loc,
		Current: weather.Current{
			Time:          now,
			Temperature:   c.Temperature2m,
			FeelsLike:     c.ApparentTemperature,
			Humidity:      int(c.RelativeHumidity2m),
			Precipitation: c.Precipitation,
			CloudCover:    int(c.CloudCover),
			Pressure:      c.PressureMSL,
			WindSpeed:     c.WindSpeed10m,
			WindGusts:     c.WindGusts10m,
			WindDirection: int(c.WindDirection10m),
			WeatherCode:   c.WeatherCode,
			IsDay:         c.IsDay == 1,
		},
	}

	h := r.Hourly
	if !sameLen(len(h.Time), len(h.Temperature2m), len(h.PrecipitationProbability), len(h.WeatherCode)) {
		return weather.Report{}, fmt.Errorf("%w: hourly arrays differ in length", errMalformed)
	}
	// The hourly series starts at local midnight; keep only the next 24 hours.
	currentHour := now.Truncate(time.Hour)
	for i, ts := range h.Time {
		t, err := time.ParseInLocation(localTimeLayout, ts, zone)
		if err != nil {
			return weather.Report{}, fmt.Errorf("%w: hourly time %q", errMalformed, ts)
		}
		if t.Before(currentHour) {
			continue
		}
		report.Hourly = append(report.Hourly, weather.HourlyPoint{
			Time:              t,
			Temperature:       h.Temperature2m[i],
			PrecipProbability: int(h.PrecipitationProbability[i]),
			WeatherCode:       h.WeatherCode[i],
		})
		if len(report.Hourly) == hoursAhead {
			break
		}
	}

	d := r.Daily
	if !sameLen(len(d.Time), len(d.WeatherCode), len(d.Temperature2mMax), len(d.Temperature2mMin),
		len(d.PrecipitationSum), len(d.PrecipitationProbabilityMax), len(d.WindSpeed10mMax),
		len(d.UVIndexMax), len(d.Sunrise), len(d.Sunset)) {
		return weather.Report{}, fmt.Errorf("%w: daily arrays differ in length", errMalformed)
	}
	for i, ds := range d.Time {
		date, err := time.ParseInLocation(localDateLayout, ds, zone)
		if err != nil {
			return weather.Report{}, fmt.Errorf("%w: daily date %q", errMalformed, ds)
		}
		// Sunrise/sunset can legitimately be absent (polar day/night); a
		// zero time is rendered as "—" by the UI.
		sunrise, _ := time.ParseInLocation(localTimeLayout, d.Sunrise[i], zone)
		sunset, _ := time.ParseInLocation(localTimeLayout, d.Sunset[i], zone)
		report.Daily = append(report.Daily, weather.DailyForecast{
			Date:              date,
			TempMax:           d.Temperature2mMax[i],
			TempMin:           d.Temperature2mMin[i],
			PrecipSum:         d.PrecipitationSum[i],
			PrecipProbability: int(d.PrecipitationProbabilityMax[i]),
			WindSpeedMax:      d.WindSpeed10mMax[i],
			UVIndexMax:        d.UVIndexMax[i],
			Sunrise:           sunrise,
			Sunset:            sunset,
			WeatherCode:       d.WeatherCode[i],
		})
	}

	return report, nil
}

func sameLen(n int, rest ...int) bool {
	for _, m := range rest {
		if m != n {
			return false
		}
	}
	return true
}
