// Package web exposes the service over HTTP as a server-rendered HTML UI and
// a small JSON API. Handlers are thin: they parse and validate request
// parameters, call the service, and map results/errors to responses.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Anshu331/weather-app/internal/service"
	"github.com/Anshu331/weather-app/internal/weather"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Service is the subset of the application service used by the web layer.
type Service interface {
	Search(ctx context.Context, query string) (service.SearchResult, error)
	Weather(ctx context.Context, loc weather.Location) (service.WeatherResult, error)
	Recent() []weather.Location
}

// Server holds HTTP dependencies.
type Server struct {
	svc   Service
	log   *slog.Logger
	pages map[string]*template.Template
}

// NewServer parses templates and returns a ready-to-use Server.
func NewServer(svc Service, log *slog.Logger) (*Server, error) {
	pages := map[string]*template.Template{}
	for _, name := range []string{"index.html", "search.html", "weather.html", "error.html"} {
		t, err := template.New(name).Funcs(templateFuncs).
			ParseFS(templateFS, "templates/layout.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", name, err)
		}
		pages[name] = t
	}
	return &Server{svc: svc, log: log, pages: pages}, nil
}

// Handler returns the root HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /weather", s.handleWeather)

	mux.HandleFunc("GET /api/search", s.handleAPISearch)
	mux.HandleFunc("GET /api/weather", s.handleAPIWeather)
	mux.HandleFunc("GET /api/recent", s.handleAPIRecent)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	mux.HandleFunc("/", s.handleNotFound)

	return chain(mux, s.recoverPanics, s.logRequests, securityHeaders)
}

// pageData is the view model shared by all HTML pages.
type pageData struct {
	Title   string
	Query   string
	Error   string
	Recent  []weather.Location
	Search  *service.SearchResult
	Weather *service.WeatherResult
	// Highlights are derived notes shown on the weather page.
	Highlights []string
}

// render executes a page into a buffer first so that a template error never
// results in a half-written response with a 200 status.
func (s *Server) render(w http.ResponseWriter, status int, page string, data pageData) {
	t, ok := s.pages[page]
	if !ok {
		s.log.Error("unknown template", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		s.log.Error("rendering template", "page", page, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// errorInfo maps an error to an HTTP status, a stable machine-readable code
// and a message that is safe to show to end users (internal details are
// logged, never displayed).
func errorInfo(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, weather.ErrInvalidInput):
		msg := strings.TrimPrefix(err.Error(), weather.ErrInvalidInput.Error()+": ")
		return http.StatusBadRequest, "invalid_input", msg
	case errors.Is(err, weather.ErrUpstream):
		return http.StatusServiceUnavailable, "upstream_unavailable",
			"The weather service is temporarily unavailable and there is no recent saved data " +
				"for this request. Please try again in a few minutes."
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "The request took too long. Please try again."
	default:
		return http.StatusInternalServerError, "internal", "Something went wrong on our side."
	}
}

var templateFuncs = template.FuncMap{
	"weatherURL": weatherURL,
	"temp":       func(c float64) string { return fmt.Sprintf("%.0f°C", c) },
	"clock": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("15:04")
	},
	"day": func(t time.Time) string { return t.Format("Mon 2 Jan") },
	"firstDay": func(days []weather.DailyForecast) weather.DailyForecast {
		if len(days) == 0 {
			return weather.DailyForecast{}
		}
		return days[0]
	},
}
