package weather

import "errors"

var (
	// ErrInvalidInput indicates the caller supplied bad input (maps to 4xx).
	ErrInvalidInput = errors.New("invalid input")

	// ErrUpstream indicates the external data source failed or returned
	// something we could not understand (maps to 5xx / degraded mode).
	ErrUpstream = errors.New("weather service unavailable")
)
