package store

import (
	"slices"
	"sync"

	"github.com/Anshu331/weather-app/internal/weather"
)

// RecentSearches is a bounded, most-recent-first list of locations the user
// has viewed. Re-viewing a location moves it to the front.
type RecentSearches struct {
	mu    sync.RWMutex
	path  string
	limit int
	items []weather.Location
}

// OpenRecentSearches loads (or creates) a list persisted at path. If path is
// empty the list is memory-only.
func OpenRecentSearches(path string, limit int) (*RecentSearches, error) {
	r := &RecentSearches{path: path, limit: limit}
	if path != "" {
		if err := loadJSON(path, &r.items); err != nil {
			return nil, err
		}
	}
	if limit > 0 && len(r.items) > limit {
		r.items = r.items[:limit]
	}
	return r, nil
}

// Add records loc as the most recent search and persists the list.
func (r *RecentSearches) Add(loc weather.Location) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := loc.Key()
	r.items = slices.DeleteFunc(r.items, func(l weather.Location) bool { return l.Key() == key })
	r.items = slices.Insert(r.items, 0, loc)
	if r.limit > 0 && len(r.items) > r.limit {
		r.items = r.items[:r.limit]
	}
	if r.path == "" {
		return nil
	}
	return saveJSON(r.path, r.items)
}

// List returns a copy of the recent searches, most recent first.
func (r *RecentSearches) List() []weather.Location {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.items)
}
