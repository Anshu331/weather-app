package web

import (
	"encoding/json"
	"net/http"

	"github.com/Anshu331/weather-app/internal/weather"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiWeatherResponse struct {
	Report     weather.Report `json:"report"`
	Freshness  string         `json:"freshness"`
	Age        string         `json:"age"`
	Warning    string         `json:"warning,omitempty"`
	Condition  string         `json:"condition"`
	Highlights []string       `json:"highlights"`
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	if res.Locations == nil {
		res.Locations = []weather.Location{}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAPIWeather(w http.ResponseWriter, r *http.Request) {
	loc, err := locationFromQuery(r.URL.Query())
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	res, err := s.svc.Weather(r.Context(), loc)
	if err != nil {
		s.writeAPIError(w, r, err)
		return
	}
	highlights := weather.Highlights(res.Report)
	if highlights == nil {
		highlights = []string{}
	}
	writeJSON(w, http.StatusOK, apiWeatherResponse{
		Report:     res.Report,
		Freshness:  string(res.Freshness),
		Age:        res.AgeText,
		Warning:    res.Warning,
		Condition:  res.Report.Current.Condition(),
		Highlights: highlights,
	})
}

func (s *Server) handleAPIRecent(w http.ResponseWriter, _ *http.Request) {
	recent := s.svc.Recent()
	if recent == nil {
		recent = []weather.Location{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": recent})
}

func (s *Server) writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := errorInfo(err)
	if status >= 500 {
		s.log.Error("api request failed", "path", r.URL.Path, "code", code, "err", err)
	}
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "60")
	}
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
