package weather

import (
	"errors"
	"math"
	"testing"
)

func TestLocationValidate(t *testing.T) {
	tests := []struct {
		name    string
		loc     Location
		wantErr bool
	}{
		{"valid", Location{Name: "London", Latitude: 51.5, Longitude: -0.12}, false},
		{"poles and antimeridian are valid", Location{Name: "Edge", Latitude: -90, Longitude: 180}, false},
		{"missing name", Location{Name: "  ", Latitude: 1, Longitude: 1}, true},
		{"latitude too high", Location{Name: "X", Latitude: 90.1, Longitude: 0}, true},
		{"longitude too low", Location{Name: "X", Latitude: 0, Longitude: -180.5}, true},
		{"NaN latitude", Location{Name: "X", Latitude: math.NaN(), Longitude: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.loc.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error should wrap ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestLocationKeyMergesNearbyCoordinates(t *testing.T) {
	a := Location{Latitude: 51.50853, Longitude: -0.12574}
	b := Location{Latitude: 51.5071, Longitude: -0.1261}
	if a.Key() != b.Key() {
		t.Errorf("expected nearby coordinates to share a key: %q vs %q", a.Key(), b.Key())
	}
}

func TestLocationLabel(t *testing.T) {
	tests := []struct {
		loc  Location
		want string
	}{
		{Location{Name: "London", Region: "England", Country: "United Kingdom"}, "London, England, United Kingdom"},
		{Location{Name: "Singapore", Region: "Singapore", Country: "Singapore"}, "Singapore, Singapore"},
		{Location{Name: "Nowhere"}, "Nowhere"},
	}
	for _, tt := range tests {
		if got := tt.loc.Label(); got != tt.want {
			t.Errorf("Label() = %q, want %q", got, tt.want)
		}
	}
}

func TestCompass(t *testing.T) {
	tests := map[int]string{0: "N", 11: "N", 12: "NNE", 90: "E", 180: "S", 270: "W", 349: "N", 360: "N", -90: "W"}
	for deg, want := range tests {
		if got := Compass(deg); got != want {
			t.Errorf("Compass(%d) = %q, want %q", deg, got, want)
		}
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe(0); got != "Clear sky" {
		t.Errorf("Describe(0) = %q", got)
	}
	if got := Describe(1234); got != "Unknown conditions" {
		t.Errorf("Describe(unknown) = %q", got)
	}
}

func TestUVCategory(t *testing.T) {
	tests := map[float64]string{0: "Low", 2.9: "Low", 3: "Moderate", 6.5: "High", 10: "Very high", 11: "Extreme"}
	for uv, want := range tests {
		if got := UVCategory(uv); got != want {
			t.Errorf("UVCategory(%v) = %q, want %q", uv, got, want)
		}
	}
}
