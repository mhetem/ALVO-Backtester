package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func intParam(r *http.Request, name string, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("%s must be at least 1", name)
	}

	return parsed, nil
}

func dayParam(r *http.Request, name string, loc *time.Location, fallback time.Time) (time.Time, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseInLocation(time.DateOnly, value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a YYYY-MM-DD date", name)
	}

	return parsed, nil
}

func timestampParam(r *http.Request, name string) (time.Time, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp, as in 2026-06-22T13:00:00Z", name)
	}

	return parsed.UTC(), nil
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func int32Of(value int) int32 {
	if value < 0 {
		return 0
	}
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(value)
}
