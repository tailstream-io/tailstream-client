// Package main - oauth.go
//
// OAuth device flow authentication for the Tailstream client.
//
// This file implements the OAuth 2.0 device authorization grant flow:
// 1. Request a device code from the authorization server
// 2. Display the verification URL and user code to the user
// 3. Poll the token endpoint until the user completes authorization
// 4. Store the access and refresh tokens in the config file
//
// It also handles:
// - Interactive stream selection from user's available streams
// - Logout functionality (clearing stored credentials)
// - Opening the verification URL in the user's browser

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	clientID = "tailstream-client"
)

// DeviceCodeResponse represents the response from the device code request
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse represents the response from the token exchange
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

// runLogin executes the OAuth device flow
func runLogin(baseURL string) error {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	fmt.Println("🚀 Tailstream Client Login")
	fmt.Println()

	// Step 1: Request device code
	deviceResp, err := requestDeviceCode(baseURL)
	if err != nil {
		return fmt.Errorf("failed to request device code: %v", err)
	}

	// Step 2: Show user instructions
	fmt.Printf("Visit: %s\n", deviceResp.VerificationURI)
	fmt.Printf("Enter code: %s\n", deviceResp.UserCode)
	fmt.Println()
	fmt.Print("Waiting for authorization... ⏳")

	// Step 3: Poll for token
	token, err := pollForToken(baseURL, deviceResp.DeviceCode, deviceResp.Interval)
	if err != nil {
		return fmt.Errorf("authorization failed: %v", err)
	}

	fmt.Println("\n✅ Logged in successfully!")

	// Step 4: Save config
	config := &ClientConfig{
		BaseURL:      baseURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}

	if err := saveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %v", err)
	}

	configPath, _ := getConfigPath()
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Println()
	fmt.Println("You can now run: tailstream-client --start \"-1h\"")

	return nil
}

// runLogout removes stored credentials
func runLogout() error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No stored credentials found.")
			return nil
		}
		return err
	}

	fmt.Println("✅ Logged out successfully. Credentials removed.")
	return nil
}

// requestDeviceCode initiates the OAuth Device Code Flow
func requestDeviceCode(baseURL string) (*DeviceCodeResponse, error) {
	// Ensure the base URL doesn't have trailing slash for consistent URL construction
	baseURL = strings.TrimRight(baseURL, "/")

	data := url.Values{
		"client_id": {clientID},
		"scope":     {"stream:read"},
	}

	client := getHTTPClient(10 * time.Second)
	endpoint := baseURL + "/api/oauth/device/code"

	resp, err := client.PostForm(endpoint, data)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed: %s\nEndpoint: %s\nResponse: %s", resp.Status, endpoint, string(body))
	}

	var deviceResp DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &deviceResp, nil
}

// pollForToken polls the token endpoint until authorization is complete
func pollForToken(baseURL, deviceCode string, interval int) (*TokenResponse, error) {
	// Ensure the base URL doesn't have trailing slash for consistent URL construction
	baseURL = strings.TrimRight(baseURL, "/")

	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}

	timeout := time.Now().Add(10 * time.Minute)
	client := getHTTPClient(10 * time.Second)
	endpoint := baseURL + "/api/oauth/device/token"

	for time.Now().Before(timeout) {
		resp, err := client.PostForm(endpoint, data)
		if err != nil {
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		var tokenResp TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
			resp.Body.Close()
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}
		resp.Body.Close()

		if tokenResp.Error == "authorization_pending" {
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		if tokenResp.Error != "" {
			return nil, fmt.Errorf("oauth error: %s", tokenResp.Error)
		}

		return &tokenResp, nil
	}

	return nil, fmt.Errorf("authorization timeout")
}
