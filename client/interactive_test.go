package main

import (
	"testing"
)

// TestInteractiveContext verifies the InteractiveContext structure
func TestInteractiveContext(t *testing.T) {
	ctx := &InteractiveContext{
		BaseURL:  "https://test.example.com",
		Token:    "test-token",
		StreamID: "test-stream-id",
		PerPage:  200,
		SortDir:  "desc",
	}

	if ctx.BaseURL != "https://test.example.com" {
		t.Errorf("unexpected BaseURL: %s", ctx.BaseURL)
	}
	if ctx.Token != "test-token" {
		t.Errorf("unexpected Token: %s", ctx.Token)
	}
	if ctx.StreamID != "test-stream-id" {
		t.Errorf("unexpected StreamID: %s", ctx.StreamID)
	}
	if ctx.PerPage != 200 {
		t.Errorf("unexpected PerPage: %d", ctx.PerPage)
	}
	if ctx.SortDir != "desc" {
		t.Errorf("unexpected SortDir: %s", ctx.SortDir)
	}
}

// Note: Full integration testing of runInteractiveMode requires terminal interaction
// and is better suited for manual testing or end-to-end tests with terminal emulation.
// The core logic is tested through the other component tests (display, api, etc.)

