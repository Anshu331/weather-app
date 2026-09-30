// Command weatherapp runs the weather web application.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Anshu331/weather-app/internal/mock"
	"github.com/Anshu331/weather-app/internal/openmeteo"
	"github.com/Anshu331/weather-app/internal/service"
	"github.com/Anshu331/weather-app/internal/store"
	"github.com/Anshu331/weather-app/internal/weather"
	"github.com/Anshu331/weather-app/internal/web"
)

const (
	maxCachedReports = 200
	maxRecent        = 8
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type config struct {
	addr         string
	dataDir      string
	provider     string
	upstreamTO   time.Duration
	freshFor     time.Duration
	maxStale     time.Duration
	geocodingURL string
	forecastURL  string
	logLevel     string
}

// parseConfig reads flags, falling back to WEATHER_* environment variables,
// then to platform conventions (PORT, VERCEL), then to defaults.
func parseConfig(args []string, getenv func(string) string, stderr io.Writer) (config, error) {
	env := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	envDur := func(key string, def time.Duration) (time.Duration, error) {
		v := getenv(key)
		if v == "" {
			return def, nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		return d, nil
	}

	defaults := service.DefaultConfig()
	upstreamTO, err := envDur("WEATHER_UPSTREAM_TIMEOUT", 5*time.Second)
	if err != nil {
		return config{}, err
	}
	freshFor, err := envDur("WEATHER_FRESH_FOR", defaults.FreshFor)
	if err != nil {
		return config{}, err
	}
	maxStale, err := envDur("WEATHER_MAX_STALE", defaults.MaxStale)
	if err != nil {
		return config{}, err
	}

	// PaaS platforms (Vercel, Render, Cloud Run, Heroku) assign the port via
	// PORT and route external traffic to it, so bind all interfaces there.
	defaultAddr := "localhost:8080"
	if port := getenv("PORT"); port != "" {
		defaultAddr = ":" + port
	}
	// Vercel's filesystem is read-only apart from the temp directory.
	defaultDataDir := "data"
	if getenv("VERCEL") != "" {
		defaultDataDir = filepath.Join(os.TempDir(), "weatherapp")
	}

	var c config
	fs := flag.NewFlagSet("weatherapp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.addr, "addr", env("WEATHER_ADDR", defaultAddr), "HTTP listen address")
	fs.StringVar(&c.dataDir, "data-dir", env("WEATHER_DATA_DIR", defaultDataDir), "directory for cache and recent searches")
	fs.StringVar(&c.provider, "provider", env("WEATHER_PROVIDER", "openmeteo"), "weather provider: openmeteo or mock")
	fs.DurationVar(&c.upstreamTO, "upstream-timeout", upstreamTO, "timeout for calls to the weather API")
	fs.DurationVar(&c.freshFor, "fresh-for", freshFor, "serve cached weather without refreshing for this long")
	fs.DurationVar(&c.maxStale, "max-stale", maxStale, "oldest cached weather to show when the API is unavailable")
	fs.StringVar(&c.geocodingURL, "geocoding-url", env("WEATHER_GEOCODING_URL", openmeteo.DefaultGeocodingURL), "Open-Meteo geocoding endpoint")
	fs.StringVar(&c.forecastURL, "forecast-url", env("WEATHER_FORECAST_URL", openmeteo.DefaultForecastURL), "Open-Meteo forecast endpoint")
	fs.StringVar(&c.logLevel, "log-level", env("WEATHER_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	switch {
	case c.provider != "openmeteo" && c.provider != "mock":
		return config{}, fmt.Errorf("unknown provider %q (want openmeteo or mock)", c.provider)
	case c.upstreamTO <= 0 || c.freshFor < 0 || c.maxStale < 0:
		return config{}, errors.New("durations must be positive")
	case c.maxStale < c.freshFor:
		return config{}, errors.New("max-stale must be at least fresh-for")
	}
	return c, nil
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := parseConfig(args, getenv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.logLevel)); err != nil {
		return fmt.Errorf("invalid log level %q", cfg.logLevel)
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	var provider weather.Provider
	switch cfg.provider {
	case "mock":
		provider = mock.New()
	default:
		provider = openmeteo.New(
			openmeteo.WithHTTPClient(&http.Client{Timeout: cfg.upstreamTO}),
			openmeteo.WithBaseURLs(cfg.geocodingURL, cfg.forecastURL),
		)
	}

	cache, err := store.OpenReportCache(filepath.Join(cfg.dataDir, "cache.json"), maxCachedReports)
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}
	recent, err := store.OpenRecentSearches(filepath.Join(cfg.dataDir, "recent.json"), maxRecent)
	if err != nil {
		return fmt.Errorf("opening recent searches: %w", err)
	}

	svcCfg := service.DefaultConfig()
	svcCfg.FreshFor, svcCfg.MaxStale = cfg.freshFor, cfg.maxStale
	svc := service.New(provider, cache, recent, svcCfg, service.WithLogger(logger))

	webSrv, err := web.NewServer(svc, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.addr, err)
	}
	logger.Info("listening", "url", "http://"+ln.Addr().String(), "provider", cfg.provider, "dataDir", cfg.dataDir)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
