// Package main - display.go
//
// Display and formatting utilities for the Tailstream client.
//
// This file handles:
// - Log entry formatting with color-coded log levels
// - Text styling and ANSI color codes
// - Query normalization and entry matching for search
// - Loading spinners for async operations
// - Type conversion utilities for displaying structured log data
//
// The formatting is optimized for terminal output with support for both
// colored and plain text modes.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// formatEntry formats a log entry for display
func formatEntry(entry map[string]any, withColor bool) string {
	// Prioritize raw_message - this is the actual log line
	rawMessage := firstString(entry, "raw_message", "message", "msg", "body", "description")

	// Helper to get parsed field from 'fields' object or top-level
	getField := func(name string) string {
		// First check if there's a 'fields' object with parsed data
		if fields, ok := entry["fields"].(map[string]any); ok {
			if val, exists := fields[name]; exists {
				return stringify(val)
			}
		}
		// Fallback to top-level (for backwards compatibility)
		return firstString(entry, name, strings.ToLower(name))
	}

	// If we have raw_message, just return it (it's already formatted)
	if rawMsg, ok := entry["raw_message"].(string); ok && rawMsg != "" {
		// Use level for styling if available (check fields object first)
		level := strings.ToUpper(getField("level"))
		if level != "" && withColor {
			// Apply subtle color based on level
			return style(rawMsg, colorForLevel(level), withColor)
		}
		return rawMsg
	}

	// Fallback to structured format if no raw_message
	timestamp := firstString(entry, "timestamp", "time", "created_at", "datetime", "logged_at")
	level := strings.ToUpper(getField("level"))
	message := rawMessage

	var builder strings.Builder
	if timestamp != "" {
		builder.WriteString(style(timestamp, "90", withColor))
		builder.WriteString(" ")
	}
	if level != "" {
		builder.WriteString(style(level, colorForLevel(level), withColor))
		builder.WriteString(" ")
	}
	if message != "" {
		builder.WriteString(message)
	}

	if builder.Len() == 0 {
		raw, _ := json.Marshal(entry)
		return string(raw)
	}
	return builder.String()
}

// firstString returns the first non-empty string value from the entry for the given keys
func firstString(entry map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := entry[k]; ok {
			if s := stringify(v); s != "" {
				return s
			}
		}
		if v, ok := entry[strings.ToLower(k)]; ok {
			if s := stringify(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// stringify converts a value to a string representation
func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case time.Time:
		return v.Format(time.RFC3339)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%f", v)
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return ""
}

// colorForLevel returns the ANSI color code for a log level
func colorForLevel(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "CRITICAL", "FATAL":
		return "31"
	case "WARN", "WARNING":
		return "33"
	case "INFO":
		return "36"
	case "DEBUG":
		return "35"
	case "TRACE":
		return "90"
	default:
		return "37"
	}
}

// style applies ANSI color codes to text
func style(text, color string, enabled bool) string {
	if !enabled || color == "" {
		return text
	}
	return "\x1b[" + color + "m" + text + "\x1b[0m"
}

// startSpinner starts a visual spinner with a message
func startSpinner(message string) func() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	stop := make(chan struct{})
	stopped := false
	var mu sync.Mutex

	go func() {
		ticker := time.NewTicker(90 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprintf(os.Stderr, "\r%s ✔\n", message)
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", message, frames[i%len(frames)])
				i++
			}
		}
	}()

	return func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopped = true
			close(stop)
		}
	}
}

// fatal prints an error message and exits
func fatal(err error) {
	if err == nil {
		os.Exit(0)
	}
	var e *url.Error
	if errors.As(err, &e) && e.Timeout() {
		fmt.Fprintf(os.Stderr, "Error: request timed out (%v)\n", e)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(1)
}
