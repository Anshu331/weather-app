// Package store provides small, file-backed persistence for the report cache
// and recent searches. Data is held in memory and flushed to a JSON file on
// every write, which is plenty for a single-user app with tiny data volumes.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// loadJSON decodes the file at path into v. A missing file is not an error.
// A corrupt file is moved aside (path + ".corrupt") so the app can start with
// empty state rather than refusing to run.
func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		aside := path + ".corrupt"
		slog.Warn("store: discarding unreadable data file", "path", path, "movedTo", aside, "err", err)
		if rerr := os.Rename(path, aside); rerr != nil {
			return fmt.Errorf("moving corrupt file %s aside: %w", path, rerr)
		}
	}
	return nil
}

// saveJSON writes v to path atomically: it writes a temp file in the same
// directory and renames it over the target, so a crash mid-write never leaves
// a truncated file behind.
func saveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}
