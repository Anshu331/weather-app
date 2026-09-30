package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Anshu331/weather-app/internal/weather"
)

func loc(name string, lat, lon float64) weather.Location {
	return weather.Location{Name: name, Latitude: lat, Longitude: lon}
}

func TestReportCachePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	fetched := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	c, err := OpenReportCache(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	l := loc("London", 51.5, -0.12)
	if err := c.Put(l.Key(), weather.Report{Location: l, FetchedAt: fetched}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	reopened, err := OpenReportCache(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(l.Key())
	if !ok {
		t.Fatal("expected report to survive reopen")
	}
	if got.Location.Name != "London" || !got.FetchedAt.Equal(fetched) {
		t.Errorf("unexpected report after reopen: %+v", got)
	}
}

func TestReportCacheEvictsOldest(t *testing.T) {
	c, _ := OpenReportCache("", 2)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	_ = c.Put("a", weather.Report{FetchedAt: base.Add(2 * time.Hour)})
	_ = c.Put("b", weather.Report{FetchedAt: base})
	_ = c.Put("c", weather.Report{FetchedAt: base.Add(time.Hour)})

	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
	if _, ok := c.Get("b"); ok {
		t.Error("oldest entry b should have been evicted")
	}
}

func TestCorruptFileIsMovedAsideAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := OpenReportCache(path, 10)
	if err != nil {
		t.Fatalf("corrupt file should not prevent startup: %v", err)
	}
	if c.Len() != 0 {
		t.Errorf("expected empty cache, got %d entries", c.Len())
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Errorf("expected corrupt file to be preserved for inspection: %v", err)
	}
}

func TestRecentSearchesOrderingDedupAndLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	r, err := OpenRecentSearches(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Add(loc("London", 51.5, -0.12))
	_ = r.Add(loc("Paris", 48.85, 2.35))
	_ = r.Add(loc("Tokyo", 35.68, 139.69))
	_ = r.Add(loc("London (again)", 51.501, -0.121)) // same key as London -> moves to front
	_ = r.Add(loc("Berlin", 52.52, 13.40))           // exceeds limit -> Paris drops off

	got := names(r.List())
	want := []string{"Berlin", "London (again)", "Tokyo"}
	if !slices.Equal(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}

	reopened, _ := OpenRecentSearches(path, 3)
	if !slices.Equal(names(reopened.List()), want) {
		t.Errorf("after reopen = %v, want %v", names(reopened.List()), want)
	}
}

func TestRecentSearchesListReturnsCopy(t *testing.T) {
	r, _ := OpenRecentSearches("", 5)
	_ = r.Add(loc("London", 51.5, -0.12))
	l := r.List()
	l[0].Name = "mutated"
	if r.List()[0].Name != "London" {
		t.Error("List() must not expose internal state")
	}
}

func names(ls []weather.Location) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}