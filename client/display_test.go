package main

import (
	"strings"
	"testing"
)

func TestFormatEntry(t *testing.T) {
	// Test with raw_message (new API format)
	entry := map[string]any{
		"id":           12345,
		"timestamp":    "2024-01-02T03:04:05Z",
		"timestamp_ms": 1735732800000,
		"raw_message":  "GET /api/orders 200 142ms",
		"fields": map[string]any{
			"level":  "INFO",
			"method": "GET",
		},
	}
	out := formatEntry(entry, false)
	if !strings.Contains(out, "GET /api/orders 200 142ms") {
		t.Fatalf("formatted output missing raw message: %s", out)
	}

	// Test with plain log (no fields)
	entryPlain := map[string]any{
		"id":           12346,
		"timestamp":    "2024-01-02T03:04:06Z",
		"timestamp_ms": 1735732801000,
		"raw_message":  "Plain log message",
	}
	outPlain := formatEntry(entryPlain, false)
	if !strings.Contains(outPlain, "Plain log message") {
		t.Fatalf("formatted output missing raw message: %s", outPlain)
	}

	// Test fallback format (when raw_message is missing)
	entryFallback := map[string]any{
		"timestamp": "2024-01-02T03:04:05Z",
		"message":   "Application started",
		"fields": map[string]any{
			"level": "info",
		},
	}
	outFallback := formatEntry(entryFallback, false)
	if !strings.Contains(outFallback, "Application started") {
		t.Fatalf("formatted output missing message: %s", outFallback)
	}
	if !strings.Contains(outFallback, "INFO") {
		t.Fatalf("formatted output missing level: %s", outFallback)
	}
}

func TestFirstString(t *testing.T) {
	entry := map[string]any{
		"message": "test message",
		"level":   "ERROR",
		"empty":   "",
	}

	// Should find message
	result := firstString(entry, "message")
	if result != "test message" {
		t.Errorf("expected 'test message', got '%s'", result)
	}

	// Should find level
	result = firstString(entry, "level")
	if result != "ERROR" {
		t.Errorf("expected 'ERROR', got '%s'", result)
	}

	// Should skip empty and find next
	result = firstString(entry, "empty", "message")
	if result != "test message" {
		t.Errorf("expected 'test message', got '%s'", result)
	}

	// Should return empty for non-existent keys
	result = firstString(entry, "nonexistent")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestStringify(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil", nil, ""},
		{"string", "test", "test"},
		{"int-like float", 42.0, "42"},
		{"float", 42.5, "42.500000"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringify(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestColorForLevel(t *testing.T) {
	tests := []struct {
		level    string
		expected string
	}{
		{"ERROR", "31"},
		{"error", "31"},
		{"WARN", "33"},
		{"WARNING", "33"},
		{"INFO", "36"},
		{"DEBUG", "35"},
		{"TRACE", "90"},
		{"UNKNOWN", "37"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			result := colorForLevel(tt.level)
			if result != tt.expected {
				t.Errorf("expected color code '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestStyle(t *testing.T) {
	// With color enabled
	result := style("test", "31", true)
	expected := "\x1b[31mtest\x1b[0m"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}

	// With color disabled
	result = style("test", "31", false)
	if result != "test" {
		t.Errorf("expected 'test', got '%s'", result)
	}

	// With empty color
	result = style("test", "", true)
	if result != "test" {
		t.Errorf("expected 'test', got '%s'", result)
	}
}

