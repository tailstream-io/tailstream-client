// Package main - api.go
//
// API client for the Tailstream log query service.
//
// This file handles:
// - HTTP client configuration with optional TLS verification skip
// - Fetching user streams from the Tailstream API
// - Streaming log entries with pagination support
// - Query parameter construction for log filtering
// - Error handling and fatal error reporting
//
// The API uses cursor-based pagination and supports various filters
// including time ranges, log levels, and text search.

package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Stream represents a user's stream from the API
type Stream struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	StreamID    string `json:"stream_id"`
	Description string `json:"description"`
}

// logResponse represents the API response structure
type logResponse struct {
	Data []map[string]any `json:"data"`
	Meta struct {
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
		Total      *int    `json:"total"` // null in tail mode (no time range)
	} `json:"meta"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

// getHTTPClient returns an HTTP client with appropriate timeout and TLS settings
func getHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

	// Check if we should skip TLS verification (for local testing)
	if insecureSkipTLSStr == "true" {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return client
}

// selectStreamInteractive fetches user streams and lets them choose
func selectStreamInteractive(baseURL, accessToken string, config *ClientConfig) (string, error) {
	fmt.Println("Fetching your streams...")

	streams, err := fetchUserStreams(baseURL, accessToken)
	if err != nil {
		return "", err
	}

	if len(streams) == 0 {
		return "", fmt.Errorf("no streams found. Please create a stream first at %s", baseURL)
	}

	// Find default stream index if it exists
	defaultIdx := -1
	if config != nil && config.DefaultStream != "" {
		for i, stream := range streams {
			if stream.StreamID == config.DefaultStream {
				defaultIdx = i
				break
			}
		}
	}

	fmt.Println()
	fmt.Println("Available streams:")
	for i, stream := range streams {
		desc := stream.Description
		if desc == "" {
			desc = stream.StreamID
		}

		marker := ""
		if i == defaultIdx {
			marker = " (default)"
		}

		fmt.Printf("[%d] %s (%s)%s\n", i+1, stream.Name, desc, marker)
	}
	fmt.Println()

	// Show which is default if it exists
	prompt := "Select stream (enter number"
	if defaultIdx >= 0 {
		prompt += fmt.Sprintf(", or press Enter for default [%d]", defaultIdx+1)
	}
	prompt += "): "

	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	selection := strings.TrimSpace(scanner.Text())

	// If empty and there's a default, use it
	if selection == "" && defaultIdx >= 0 {
		selectedStream := streams[defaultIdx]
		fmt.Printf("Using default: %s\n", selectedStream.Name)
		fmt.Println()
		return selectedStream.StreamID, nil
	}

	idx, err := strconv.Atoi(selection)
	if err != nil || idx < 1 || idx > len(streams) {
		return "", fmt.Errorf("invalid selection")
	}

	selectedStream := streams[idx-1]
	fmt.Printf("Selected: %s\n", selectedStream.Name)
	fmt.Println()

	return selectedStream.StreamID, nil
}

// fetchUserStreams retrieves the user's streams
func fetchUserStreams(baseURL, accessToken string) ([]Stream, error) {
	client := getHTTPClient(10 * time.Second)

	req, err := http.NewRequest("GET", baseURL+"/api/user/streams", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch streams: %s - %s", resp.Status, string(body))
	}

	var streamsResp struct {
		Streams []Stream `json:"streams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
		return nil, err
	}

	return streamsResp.Streams, nil
}

// createFetcher creates a fetcher function for pagination
func createFetcher(baseURL, token, streamID string, baseQuery url.Values, terms []string) func(string, string) ([]map[string]any, bool, *int, string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/streams/" + url.PathEscape(strings.TrimSpace(streamID)) + "/logs"
	client := getHTTPClient(15 * time.Second)

	return func(cursor string, searchQuery string) ([]map[string]any, bool, *int, string, error) {
		queryParams := url.Values{}
		// Copy original query params
		for k, v := range baseQuery {
			queryParams[k] = v
		}

		// Set cursor if provided
		if cursor != "" {
			queryParams.Set("cursor", cursor)
		}

		// Add server-side search filter if provided
		if searchQuery != "" {
			filters := []map[string]any{}
			// Parse existing filters if any
			if existingFilters := baseQuery.Get("filters"); existingFilters != "" {
				json.Unmarshal([]byte(existingFilters), &filters)
			}
			// Add search filter
			filters = append(filters, map[string]any{
				"field": "q",
				"value": searchQuery,
			})
			filtersJSON, _ := json.Marshal(filters)
			queryParams.Set("filters", string(filtersJSON))
		}

		fullURL := endpoint + "?" + queryParams.Encode()

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, false, nil, "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, false, nil, "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, false, nil, "", fmt.Errorf("request failed: %s", resp.Status)
		}

		var pagePayload logResponse
		if err := json.NewDecoder(resp.Body).Decode(&pagePayload); err != nil {
			return nil, false, nil, "", err
		}

		// Filter entries based on client-side search terms (from --search flag)
		pageFiltered := make([]map[string]any, 0)
		for _, entry := range pagePayload.Data {
			if len(terms) > 0 && !entryMatches(entry, terms) {
				continue
			}
			pageFiltered = append(pageFiltered, entry)
		}

		hasMore := pagePayload.Meta.HasMore
		nextCursor := ""
		if pagePayload.Meta.NextCursor != nil {
			nextCursor = *pagePayload.Meta.NextCursor
		}

		return pageFiltered, hasMore, pagePayload.Meta.Total, nextCursor, nil
	}
}

// normalizeQueries converts search terms to lowercase and trims whitespace
func normalizeQueries(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	terms := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		terms = append(terms, strings.ToLower(v))
	}
	return terms
}

// entryMatches checks if an entry matches all search terms
func entryMatches(entry map[string]any, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	blob, err := json.Marshal(entry)
	if err != nil {
		return false
	}
	haystack := strings.ToLower(string(blob))
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}
