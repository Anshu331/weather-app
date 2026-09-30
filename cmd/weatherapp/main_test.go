package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseConfigDefaults(t *testing.T) {
	c, err := parseConfig(nil, envMap(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.addr != "localhost:8080" || c.provider != "openmeteo" || c.freshFor != 10*time.Minute {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestParseConfigFlagsOverrideEnv(t *testing.T) {
	env := envMap(map[string]string{"WEATHER_ADDR": ":9000", "WEATHER_PROVIDER": "mock", "WEATHER_MAX_STALE": "2h"})
	c, err := parseConfig([]string{"-addr", ":7000"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.addr != ":7000" {
		t.Errorf("flag should win over env, got addr %q", c.addr)
	}
	if c.provider != "mock" || c.maxStale != 2*time.Hour {
		t.Errorf("env values not applied: %+v", c)
	}
}

func TestParseConfigPlatformConventions(t *testing.T) {
	c, err := parseConfig(nil, envMap(map[string]string{"PORT": "3000", "VERCEL": "1"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.addr != ":3000" {
		t.Errorf("addr = %q, want :3000 from PORT", c.addr)
	}
	if want := filepath.Join(os.TempDir(), "weatherapp"); c.dataDir != want {
		t.Errorf("dataDir = %q, want %q on Vercel", c.dataDir, want)
	}

	c, _ = parseConfig(nil, envMap(map[string]string{"PORT": "3000", "WEATHER_ADDR": ":9000"}), io.Discard)
	if c.addr != ":9000" {
		t.Errorf("WEATHER_ADDR should win over PORT, got %q", c.addr)
	}
}

func TestParseConfigRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{"unknown provider", []string{"-provider", "acme"}, nil},
		{"stale shorter than fresh", []string{"-fresh-for", "1h", "-max-stale", "10m"}, nil},
		{"bad env duration", nil, map[string]string{"WEATHER_FRESH_FOR": "soon"}},
		{"zero timeout", []string{"-upstream-timeout", "0s"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseConfig(tt.args, envMap(tt.env), io.Discard); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
