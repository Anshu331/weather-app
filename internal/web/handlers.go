package web

import (
	"net/http"

	"github.com/Anshu331/weather-app/internal/weather"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index.html", pageData{Title: "Weather", Recent: s.svc.Recent()})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	res, err := s.svc.Search(r.Context(), query)
	if err != nil {
		s.renderError(w, r, err, query)
		return
	}

	// A single unambiguous match goes straight to the weather page.
	if len(res.Locations) == 1 && res.Warning == "" {
		http.Redirect(w, r, weatherURL(res.Locations[0]), http.StatusSeeOther)
		return
	}

	s.render(w, http.StatusOK, "search.html", pageData{
		Title:  "Search: " + res.Query,
		Query:  res.Query,
		Search: &res,
		Recent: s.svc.Recent(),
	})
}

func (s *Server) handleWeather(w http.ResponseWriter, r *http.Request) {
	loc, err := locationFromQuery(r.URL.Query())
	if err != nil {
		s.renderError(w, r, err, "")
		return
	}
	res, err := s.svc.Weather(r.Context(), loc)
	if err != nil {
		s.renderError(w, r, err, loc.Name)
		return
	}
	s.render(w, http.StatusOK, "weather.html", pageData{
		Title:      "Weather in " + res.Report.Location.Name,
		Query:      loc.Name,
		Weather:    &res,
		Highlights: weather.Highlights(res.Report),
		Recent:     s.svc.Recent(),
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, "error.html", pageData{
		Title:  "Not found",
		Error:  "That page doesn't exist.",
		Recent: s.svc.Recent(),
	})
}

// renderError shows a friendly error page, keeping the search box populated
// and recent searches visible so the user always has a way forward.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, err error, query string) {
	status, code, msg := errorInfo(err)
	if status >= 500 {
		s.log.Error("request failed", "path", r.URL.Path, "code", code, "err", err)
	}
	s.render(w, status, "error.html", pageData{
		Title:  "Weather",
		Query:  query,
		Error:  msg,
		Recent: s.svc.Recent(),
	})
}
