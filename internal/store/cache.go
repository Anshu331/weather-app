package store

import (
	"sync"

	"github.com/Anshu331/weather-app/internal/weather"
)

// ReportCache stores the most recent successful report per location key.
// It deliberately does not expire entries itself: freshness and "how stale is
// too stale" are business rules owned by the service layer, which inspects
// Report.FetchedAt. The cache only bounds its size.
type ReportCache struct {
	mu         sync.RWMutex
	path       string
	maxEntries int
	entries    map[string]weather.Report
}

// OpenReportCache loads (or creates) a cache persisted at path. If path is
// empty the cache is memory-only.
func OpenReportCache(path string, maxEntries int) (*ReportCache, error) {
	c := &ReportCache{path: path, maxEntries: maxEntries, entries: map[string]weather.Report{}}
	if path != "" {
		if err := loadJSON(path, &c.entries); err != nil {
			return nil, err
		}
		if c.entries == nil { // file contained "null"
			c.entries = map[string]weather.Report{}
		}
	}
	return c, nil
}

// Get returns the cached report for key, if any.
func (c *ReportCache) Get(key string) (weather.Report, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[key]
	return r, ok
}

// Put stores r under key, evicting the oldest entries if the cache is full,
// and persists the cache. The in-memory update happens even if persisting
// fails, so the returned error only means "won't survive a restart".
func (c *ReportCache) Put(key string, r weather.Report) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = r
	for c.maxEntries > 0 && len(c.entries) > c.maxEntries {
		c.evictOldestLocked()
	}
	if c.path == "" {
		return nil
	}
	return saveJSON(c.path, c.entries)
}

func (c *ReportCache) evictOldestLocked() {
	var oldestKey string
	var oldest weather.Report
	first := true
	for k, r := range c.entries {
		if first || r.FetchedAt.Before(oldest.FetchedAt) {
			oldestKey, oldest, first = k, r, false
		}
	}
	delete(c.entries, oldestKey)
}

// Len returns the number of cached reports.
func (c *ReportCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
