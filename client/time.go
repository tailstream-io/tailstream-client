// Package main - time.go
//
// Time parsing utilities for the Tailstream client.
//
// This file provides functions to parse flexible time specifications including:
// - Relative times (e.g., "-1h", "-30m", "-7d")
// - Absolute dates (e.g., "2024-01-02", "2024-01-02 15:04")
// - RFC3339 timestamps
// - Special keywords ("now")
//
// All times are normalized to RFC3339 format in UTC for API consumption.

package main

import (
	"fmt"
	"strings"
	"time"
)

// parseTimeArg parses a time string in various formats and returns RFC3339 format.
// Supports:
// - "now" -> current time
// - Relative durations: "-1h", "-30m", "-2h30m"
// - Dates: "2024-01-01"
// - Date and time: "2024-01-01 15:04"
// - RFC3339: "2024-01-01T15:04:05Z"
func parseTimeArg(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.EqualFold(value, "now") {
		return time.Now().UTC().Format(time.RFC3339), nil
	}
	if strings.HasPrefix(value, "-") {
		dur, err := time.ParseDuration(value)
		if err != nil {
			return "", fmt.Errorf("invalid relative duration %q: %w", value, err)
		}
		return time.Now().Add(dur).UTC().Format(time.RFC3339), nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("could not parse time value %q", value)
}
