package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchUserStreams(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/streams" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		// Verify authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("unexpected authorization header: %s", auth)
		}

		// Return mock streams
		resp := struct {
			Streams []Stream `json:"streams"`
		}{
			Streams: []Stream{
				{ID: 1, Name: "Test Stream 1", StreamID: "stream-1", Description: "First stream"},
				{ID: 2, Name: "Test Stream 2", StreamID: "stream-2", Description: "Second stream"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test the function
	streams, err := fetchUserStreams(server.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}

	if streams[0].Name != "Test Stream 1" {
		t.Errorf("unexpected stream name: %s", streams[0].Name)
	}
	if streams[1].StreamID != "stream-2" {
		t.Errorf("unexpected stream ID: %s", streams[1].StreamID)
	}
}

func TestFetchUserStreamsError(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer server.Close()

	// Test the function
	_, err := fetchUserStreams(server.URL, "invalid-token")
	if err == nil {
		t.Fatal("expected error for unauthorized response")
	}
}

func TestNormalizeQueries(t *testing.T) {
	terms := normalizeQueries([]string{" Error ", "", "Warn"})
	if len(terms) != 2 {
		t.Fatalf("expected 2 terms got %d", len(terms))
	}
	if terms[0] != "error" || terms[1] != "warn" {
		t.Fatalf("unexpected values: %#v", terms)
	}
}

func TestEntryMatches(t *testing.T) {
	entry := map[string]any{"message": "Server error", "level": "error"}
	if !entryMatches(entry, []string{"error"}) {
		t.Fatal("expected match")
	}
	if entryMatches(entry, []string{"warning"}) {
		t.Fatal("did not expect match")
	}
}

func TestEntryMatchesMultipleTerms(t *testing.T) {
	entry := map[string]any{
		"message": "Database connection error",
		"level":   "error",
		"service": "api",
	}

	// All terms match
	if !entryMatches(entry, []string{"database", "error"}) {
		t.Fatal("expected match for all terms")
	}

	// One term doesn't match
	if entryMatches(entry, []string{"database", "warning"}) {
		t.Fatal("did not expect match when one term doesn't match")
	}

	// Empty terms should match everything
	if !entryMatches(entry, []string{}) {
		t.Fatal("expected match for empty terms")
	}
}

