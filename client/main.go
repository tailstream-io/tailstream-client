// Package main - Tailstream Client
//
// A command-line client for querying and analyzing logs from the Tailstream API.
//
// The client supports:
// - OAuth device flow authentication (--login/--logout)
// - Interactive terminal UI for browsing logs (default mode)
// - Direct query mode with JSON output (--json)
// - Flexible time ranges (relative like "-1h" or absolute dates)
// - Log filtering by level, search terms, and custom queries
// - Real-time streaming and pagination
//
// Architecture:
// - main.go: Entry point, flag parsing, and flow control
// - config.go: Configuration file management
// - oauth.go: OAuth device flow and stream selection
// - api.go: HTTP client and API interactions
// - time.go: Time parsing utilities
// - display.go: Log formatting and styling
// - interactive.go: Interactive terminal UI
//
// Usage examples:
//   tailstream-client --login              # Authenticate via OAuth
//   tailstream-client --start "-1h"        # View last hour (interactive)
//   tailstream-client --start "-24h" --json  # JSON output, last 24h
//   tailstream-client --level ERROR --start "-1h"  # Filter by log level

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	// Version information (set during build)
	Version   = "dev"
	BuildDate = "unknown"
	GitCommit = "unknown"

	defaultBaseURL     = "https://app.tailstream.io"
	insecureSkipTLSStr = "false" // Set to "true" for local testing with self-signed certs
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	// Handle version command
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v" || os.Args[1] == "version") {
		fmt.Printf("tailstream-client %s\n", Version)
		fmt.Printf("Build date: %s\n", BuildDate)
		fmt.Printf("Git commit: %s\n", GitCommit)
		return
	}

	var (
		baseURL       = flag.String("base-url", "", "Tailstream API host (overrides config)")
		token         = flag.String("token", "", "API token for Authorization header (overrides config)")
		streamID      = flag.String("stream-id", "", "Stream ID (overrides config default)")
		from          = flag.String("from", "", "Start date/time (RFC3339, YYYY-MM-DD, or relative like -1h)")
		to            = flag.String("to", "", "End date/time (RFC3339, YYYY-MM-DD, or relative like -5m)")
		limit         = flag.Int("limit", 200, "Maximum number of log entries to display")
		perPage       = flag.Int("per-page", 200, "Number of results per page (uses 'limit' parameter)")
		sortDir       = flag.String("sort", "desc", "Sort direction: asc or desc (uses 'direction' parameter)")
		timeout       = flag.Duration("timeout", 15*time.Second, "HTTP request timeout")
		rawJSON       = flag.Bool("json", false, "Output raw JSON response")
		noColor       = flag.Bool("no-color", false, "Disable ANSI color output")
		quiet         = flag.Bool("quiet", false, "Disable progress indicator")
		login         = flag.Bool("login", false, "Run OAuth login flow")
		logout        = flag.Bool("logout", false, "Remove stored credentials")
		interactive   = flag.Bool("interactive", true, "Interactive mode with navigation (use --interactive=false to disable)")
		noInteractive = flag.Bool("no-interactive", false, "Disable interactive mode and output directly")
	)

	var levels stringSliceFlag
	var methods stringSliceFlag
	var searches stringSliceFlag
	flag.Var(&levels, "level", "Log level filter (repeatable, e.g., ERROR, WARN, INFO)")
	flag.Var(&methods, "method", "HTTP method filter (repeatable, e.g., GET, POST)")
	flag.Var(&searches, "search", "Search query (repeatable, case-insensitive)")

	flag.Parse()

	// Determine if we should use interactive mode
	useInteractive := *interactive && !*noInteractive && !*rawJSON

	// If filters or searches are provided, assume non-interactive output is desired
	if len(levels) > 0 || len(methods) > 0 || len(searches) > 0 {
		useInteractive = false
	}

	// Handle login command
	if *login {
		if err := runLogin(*baseURL); err != nil {
			fatal(err)
		}
		return
	}

	// Handle logout command
	if *logout {
		if err := runLogout(); err != nil {
			fatal(err)
		}
		return
	}

	// Load config
	config, err := loadConfig()
	if err != nil && !os.IsNotExist(err) {
		fatal(fmt.Errorf("failed to load config: %v", err))
	}

	// Determine base URL (flag > config > default)
	finalBaseURL := determineBaseURL(*baseURL, config)

	// Determine token (flag > config)
	finalToken := *token
	if finalToken == "" && config != nil {
		finalToken = config.AccessToken
	}

	// If no token available, prompt for login
	if finalToken == "" {
		fmt.Println("No authentication found. Please run:")
		fmt.Println("  tailstream-client --login")
		os.Exit(1)
	}

	// Determine stream ID
	finalStreamID := *streamID

	// If no explicit stream ID was provided via flag, show interactive selector
	if finalStreamID == "" {
		selectedStream, err := selectStreamInteractive(finalBaseURL, finalToken, config)
		if err != nil {
			fatal(fmt.Errorf("stream selection failed: %v", err))
		}
		finalStreamID = selectedStream

		// Update config with selected stream as default
		if config != nil {
			config.DefaultStream = finalStreamID
			config.UpdatedAt = time.Now().Format(time.RFC3339)
			if err := saveConfig(config); err != nil {
				// Non-fatal, just warn
				fmt.Fprintf(os.Stderr, "Warning: could not save default stream: %v\n", err)
			}
		}
	}

	query := url.Values{}
	if v := strings.TrimSpace(*from); v != "" {
		parsed, err := parseTimeArg(v)
		if err != nil {
			fatal(err)
		}
		// Convert RFC3339 to millisecond timestamp
		t, err := time.Parse(time.RFC3339, parsed)
		if err != nil {
			fatal(fmt.Errorf("failed to parse from time: %w", err))
		}
		query.Set("start_time", strconv.FormatInt(t.UnixMilli(), 10))
	}
	if v := strings.TrimSpace(*to); v != "" {
		parsed, err := parseTimeArg(v)
		if err != nil {
			fatal(err)
		}
		// Convert RFC3339 to millisecond timestamp
		t, err := time.Parse(time.RFC3339, parsed)
		if err != nil {
			fatal(fmt.Errorf("failed to parse to time: %w", err))
		}
		query.Set("end_time", strconv.FormatInt(t.UnixMilli(), 10))
	}
	// Build filters for levels and methods
	if len(levels) > 0 || len(methods) > 0 {
		filters := make([]map[string]any, 0, len(levels)+len(methods))
		for _, level := range levels {
			filters = append(filters, map[string]any{
				"field":    "level",
				"operator": "=",
				"value":    level,
			})
		}
		for _, method := range methods {
			filters = append(filters, map[string]any{
				"field":    "method",
				"operator": "=",
				"value":    method,
			})
		}
		if filterJSON, err := json.Marshal(filters); err == nil {
			query.Set("filters", string(filterJSON))
		}
	}
	// Backend uses cursor-based pagination with limit and direction
	if *perPage > 0 {
		query.Set("limit", strconv.Itoa(*perPage))
	}
	if *sortDir != "" {
		query.Set("direction", *sortDir) // Backend uses 'direction' not 'sort'
	}

	endpoint := strings.TrimRight(finalBaseURL, "/") + "/api/streams/" + url.PathEscape(strings.TrimSpace(finalStreamID)) + "/logs"

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+finalToken)

	client := getHTTPClient(*timeout)

	stopSpinner := func() {}
	if !*quiet {
		stopSpinner = startSpinner("Fetching logs")
		defer stopSpinner()
	}

	resp, err := client.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	stopSpinner()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		fatal(fmt.Errorf("request failed: %s\n%s", resp.Status, strings.TrimSpace(string(body))))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fatal(err)
	}

	if *rawJSON {
		os.Stdout.Write(body)
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
		return
	}

	var payload logResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		fatal(fmt.Errorf("unable to parse response JSON: %w", err))
	}

	entries := payload.Data

	if len(entries) == 0 {
		fmt.Println("No logs matched your filters.")
		return
	}

	// Filter entries based on search terms
	terms := normalizeQueries(searches)
	filtered := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if len(terms) > 0 && !entryMatches(entry, terms) {
			continue
		}
		filtered = append(filtered, entry)
		if *limit > 0 && len(filtered) >= *limit {
			break
		}
	}

	if len(filtered) == 0 {
		fmt.Println("No logs matched your filters.")
		return
	}

	// Create a fetcher function for pagination
	fetcher := createFetcher(finalBaseURL, finalToken, finalStreamID, query, terms)

	// Get initial cursor for pagination
	initialCursor := ""
	if payload.Meta.NextCursor != nil {
		initialCursor = *payload.Meta.NextCursor
	}

	// Display logs
	if useInteractive {
		// Pass context needed for dynamic filtering
		interactiveCtx := &InteractiveContext{
			BaseURL:   finalBaseURL,
			Token:     finalToken,
			StreamID:  finalStreamID,
			PerPage:   *perPage,
			SortDir:   *sortDir,
			Client:    client,
			Endpoint:  endpoint,
			BaseQuery: query, // Original query params (without filters)
		}
		runInteractiveMode(filtered, !*noColor, payload.Meta.HasMore, payload.Meta.Total, initialCursor, fetcher, interactiveCtx)
	} else {
		// Direct output mode - print current page and continue if there are more
		for _, entry := range filtered {
			fmt.Println(formatEntry(entry, !*noColor))
		}

		// If there are more pages and we're not limiting output, fetch and display them
		cursor := initialCursor
		if payload.Meta.HasMore && (*limit <= 0 || len(filtered) < *limit) {
			remainingLimit := *limit - len(filtered)

			for cursor != "" {
				moreEntries, hasMore, _, nextCursor, err := fetcher(cursor, "") // No search in direct mode
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to fetch next page: %v\n", err)
					break
				}

				if len(moreEntries) == 0 {
					break
				}

				// Print entries from this page
				for _, entry := range moreEntries {
					fmt.Println(formatEntry(entry, !*noColor))
					remainingLimit--
					if *limit > 0 && remainingLimit <= 0 {
						return
					}
				}

				if !hasMore {
					break
				}

				cursor = nextCursor
			}
		}
	}
}
