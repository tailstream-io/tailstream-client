package main

import (
	"testing"
	"time"
)

func TestParseTimeArg(t *testing.T) {
	got, err := parseTimeArg("2024-01-02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Parse the result to validate it's a valid time in UTC
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("result is not valid RFC3339: %s", got)
	}
	// Check that the date portion matches (day might shift due to timezone conversion)
	if parsed.Year() != 2024 || parsed.Month() != 1 || (parsed.Day() != 1 && parsed.Day() != 2) {
		t.Fatalf("unexpected date: %s (parsed: %v)", got, parsed)
	}

	got, err = parseTimeArg("2024-01-02 15:04")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 1, 2, 15, 4, 0, 0, time.Local).UTC().Format(time.RFC3339)
	if got != expected {
		t.Fatalf("expected %s got %s", expected, got)
	}

	if _, err := parseTimeArg("not-a-date"); err == nil {
		t.Fatal("expected error for invalid time")
	}
}

func TestParseTimeArgRelative(t *testing.T) {
	// Test relative time
	got, err := parseTimeArg("-1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("result is not valid RFC3339: %s", got)
	}
	// Should be approximately 1 hour ago
	diff := time.Since(parsed)
	if diff < 55*time.Minute || diff > 65*time.Minute {
		t.Fatalf("expected time around 1 hour ago, got diff: %v", diff)
	}
}

func TestParseTimeArgNow(t *testing.T) {
	got, err := parseTimeArg("now")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("result is not valid RFC3339: %s", got)
	}
	// Should be very recent
	diff := time.Since(parsed)
	if diff > 1*time.Second {
		t.Fatalf("expected current time, got diff: %v", diff)
	}
}

func TestParseTimeArgEmpty(t *testing.T) {
	got, err := parseTimeArg("")
	if err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got: %s", got)
	}
}
