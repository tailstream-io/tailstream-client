package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestDeviceCode(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/device/code" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		// Verify client_id
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("client_id") != clientID {
			t.Errorf("unexpected client_id: %s", r.FormValue("client_id"))
		}

		// Return mock response
		resp := DeviceCodeResponse{
			DeviceCode:      "test-device-code",
			UserCode:        "TEST-1234",
			VerificationURI: "https://example.com/activate",
			ExpiresIn:       600,
			Interval:        5,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test the function
	result, err := requestDeviceCode(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DeviceCode != "test-device-code" {
		t.Errorf("unexpected device code: %s", result.DeviceCode)
	}
	if result.UserCode != "TEST-1234" {
		t.Errorf("unexpected user code: %s", result.UserCode)
	}
}

func TestRequestDeviceCodeError(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer server.Close()

	// Test the function
	_, err := requestDeviceCode(server.URL)
	if err == nil {
		t.Fatal("expected error for unauthorized response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestPollForTokenSuccess(t *testing.T) {
	// Create test server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Return pending on first call, success on second
		if callCount == 1 {
			resp := TokenResponse{
				Error: "authorization_pending",
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			resp := TokenResponse{
				AccessToken:  "test-access-token",
				RefreshToken: "test-refresh-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	// Test the function with short interval
	result, err := pollForToken(server.URL, "test-device-code", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AccessToken != "test-access-token" {
		t.Errorf("unexpected access token: %s", result.AccessToken)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
}

func TestPollForTokenOAuthError(t *testing.T) {
	// Create test server that returns OAuth error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := TokenResponse{
			Error: "access_denied",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Test the function
	_, err := pollForToken(server.URL, "test-device-code", 0)
	if err == nil {
		t.Fatal("expected error for access_denied")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error should mention access_denied: %v", err)
	}
}

