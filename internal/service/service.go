// Package service contains the application's business rules: input
// validation, caching policy, graceful degradation when the upstream weather
// provider is unavailable, and recording recent searches.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Anshu331/weather-app/internal/weather"
)

const (
	MinQueryLen = 2 // Open-Meteo returns nothing for single-character queries
	MaxQueryLen = 100
)

// Cache stores the last good report per location key.
type Cache interface {
	Get(key string) (weather.Report, bool)
	Put(key string, r weather.Report) error
}

// History records recently viewed locations.
type History interface {
	Add(loc weather.Location) error
	List() []weather.Location
}

// Config holds the caching policy.
type Config struct {
	// FreshFor is how long a cached report is served without contacting the
	// provider. Forecast models update hourly at best, so a few minutes of
	// caching costs nothing in accuracy and protects the upstream.
	FreshFor time.Duration
	// MaxStale is the oldest cached report we will show when the provider is
	// unavailable. Beyond this, "current conditions" would be misleading.
	MaxStale time.Duration
	// SearchLimit caps the number of geocoding candidates returned.
	SearchLimit int
}

// DefaultConfig returns the policy used when no overrides are supplied.
func DefaultConfig() Config {
	return Config{FreshFor: 10 * time.Minute, MaxStale: 24 * time.Hour, SearchLimit: 8}
}

// Freshness describes where a weather result came from.
type Freshness string

const (
	FreshLive   Freshness = "live"   // fetched from the provider for this request
	FreshCached Freshness = "cached" // served from cache within FreshFor
	FreshStale  Freshness = "stale"  // provider failed; older cached data shown
)

// WeatherResult is a report plus metadata about how trustworthy it is.
type WeatherResult struct {
	Report    weather.Report `json:"report"`
	Freshness Freshness      `json:"freshness"`
	Age       time.Duration  `json:"-"`
	AgeText   string         `json:"age"`
	Warning   string         `json:"warning,omitempty"`
}

// Stale reports whether the data is being shown because the provider failed.
func (r WeatherResult) Stale() bool { return r.Freshness == FreshStale }

// SearchResult is the outcome of a location search.
type SearchResult struct {
	Query     string             `json:"query"`
	Locations []weather.Location `json:"locations"`
	// Warning is set when results come from a fallback rather than the
	// geocoding service.
	Warning string `json:"warning,omitempty"`
}

// Service orchestrates providers, cache and history.
type Service struct {
	provider weather.Provider
	cache    Cache
	history  History
	cfg      Config
	now      func() time.Time
	log      *slog.Logger
}

// Option customises a Service.
type Option func(*Service)

// WithClock overrides the service clock (for tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger used for degraded-mode diagnostics.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// New builds a Service.
func New(p weather.Provider, c Cache, h History, cfg Config, opts ...Option) *Service {
	s := &Service{provider: p, cache: c, history: h, cfg: cfg, now: time.Now, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// NormalizeQuery trims and collapses whitespace and validates a search query.
func NormalizeQuery(q string) (string, error) {
	q = strings.Join(strings.Fields(q), " ")
	n := utf8.RuneCountInString(q)
	switch {
	case n == 0:
		return "", fmt.Errorf("%w: please enter a location to search for", weather.ErrInvalidInput)
	case n < MinQueryLen:
		return "", fmt.Errorf("%w: search must be at least %d characters", weather.ErrInvalidInput, MinQueryLen)
	case n > MaxQueryLen:
		return "", fmt.Errorf("%w: search must be at most %d characters", weather.ErrInvalidInput, MaxQueryLen)
	}
	for _, r := range q {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: search contains invalid characters", weather.ErrInvalidInput)
		}
	}
	return q, nil
}

// Search finds candidate locations for a free-text query. If the geocoding
// service is down, it falls back to matching the user's recent searches so
// that previously viewed places remain reachable.
func (s *Service) Search(ctx context.Context, query string) (SearchResult, error) {
	q, err := NormalizeQuery(query)
	if err != nil {
		return SearchResult{}, err
	}

	locs, err := s.provider.Search(ctx, q, s.cfg.SearchLimit)
	if err == nil {
		return SearchResult{Query: q, Locations: locs}, nil
	}

	s.log.Warn("location search failed; trying recent searches", "query", q, "err", err)
	if matches := s.matchRecent(q); len(matches) > 0 {
		return SearchResult{
			Query:     q,
			Locations: matches,
			Warning:   "Location search is temporarily unavailable. Showing matches from your recent searches.",
		}, nil
	}
	return SearchResult{}, err
}

func (s *Service) matchRecent(q string) []weather.Location {
	needle := strings.ToLower(q)
	var out []weather.Location
	for _, l := range s.history.List() {
		if strings.Contains(strings.ToLower(l.Label()), needle) {
			out = append(out, l)
		}
	}
	return out
}

// Weather returns the weather for loc, applying the caching policy:
//
//  1. A cached report younger than FreshFor is returned without an upstream call.
//  2. Otherwise the provider is called; on success the cache is refreshed.
//  3. If the provider fails, a cached report younger than MaxStale is returned
//     and flagged as stale, with a warning suitable for display.
//  4. If there is no usable cached report, the provider error is returned.
func (s *Service) Weather(ctx context.Context, loc weather.Location) (WeatherResult, error) {
	if err := loc.Validate(); err != nil {
		return WeatherResult{}, err
	}

	key := loc.Key()
	now := s.now()
	cached, haveCached := s.cache.Get(key)

	if haveCached && now.Sub(cached.FetchedAt) < s.cfg.FreshFor {
		return s.result(loc, cached, FreshCached, now, ""), nil
	}

	report, err := s.provider.Forecast(ctx, loc)
	if err == nil {
		if perr := s.cache.Put(key, report); perr != nil {
			s.log.Warn("failed to persist weather cache", "key", key, "err", perr)
		}
		return s.result(loc, report, FreshLive, now, ""), nil
	}

	if errors.Is(err, context.Canceled) {
		return WeatherResult{}, err // the client went away; nothing to degrade to
	}
	s.log.Warn("forecast failed", "location", loc.Label(), "key", key, "err", err, "haveCached", haveCached)

	if haveCached {
		age := now.Sub(cached.FetchedAt)
		if age <= s.cfg.MaxStale {
			warning := fmt.Sprintf(
				"Live weather data is temporarily unavailable. Showing the last known conditions from %s ago.",
				FormatAge(age))
			return s.result(loc, cached, FreshStale, now, warning), nil
		}
		return WeatherResult{}, fmt.Errorf(
			"%w: live data unavailable and the last saved report is %s old", weather.ErrUpstream, FormatAge(age))
	}
	return WeatherResult{}, err
}

func (s *Service) result(loc weather.Location, r weather.Report, f Freshness, now time.Time, warning string) WeatherResult {
	// Show the location as the user chose it (the cache is keyed by rounded
	// coordinates, so a nearby place with a different name may share a report).
	if loc.Timezone == "" {
		loc.Timezone = r.Location.Timezone
	}
	r.Location = loc

	if err := s.history.Add(loc); err != nil {
		s.log.Warn("failed to persist recent searches", "err", err)
	}

	age := now.Sub(r.FetchedAt)
	if age < 0 {
		age = 0
	}
	return WeatherResult{Report: r, Freshness: f, Age: age, AgeText: FormatAge(age), Warning: warning}
}

// Recent returns recently viewed locations, most recent first.
func (s *Service) Recent() []weather.Location { return s.history.List() }

// FormatAge renders a duration as a short human-readable age.
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < 2*time.Minute:
		return "1 minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		return "1 hour"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
