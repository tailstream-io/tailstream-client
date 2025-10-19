// Package main - interactive.go
//
// Interactive terminal UI for browsing and searching Tailstream logs.
//
// This file implements a full-featured interactive mode with:
// - Real-time log streaming and pagination
// - Keyboard navigation (j/k, page up/down, home/end)
// - Live search with query highlighting
// - Date range filtering (f key)
// - Auto-refresh mode (a key)
// - Terminal resize handling
// - Viewport management with smooth scrolling
//
// The interactive mode uses raw terminal input (stty -echo) and ANSI escape
// codes for cursor control and screen clearing. It provides a less(1)-like
// experience for log exploration.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// InteractiveContext holds the context needed for dynamic operations in interactive mode
type InteractiveContext struct {
	BaseURL   string
	Token     string
	StreamID  string
	PerPage   int
	SortDir   string
	Client    *http.Client
	Endpoint  string
	BaseQuery url.Values
}

// runInteractiveMode displays logs in an interactive viewer with navigation and pagination
func runInteractiveMode(entries []map[string]any, withColor bool, hasMore bool, totalCount *int, nextCursor string, fetcher func(string, string) ([]map[string]any, bool, *int, string, error), ctx *InteractiveContext) {
	if len(entries) == 0 {
		return
	}

	currentIdx := 0
	expanded := make(map[int]bool)
	expandedScrollOffset := make(map[int]int) // Track scroll offset within expanded entries
	loading := false
	status := ""
	searchQuery := ""          // Current server-side search query
	searchMatches := []int{}   // Indices of entries that match search (for n/N navigation)
	searchActive := false      // Whether we're in search mode
	searchCursor := ""         // Cursor for search pagination
	searchHasMore := false     // Whether search results have more pages
	searchTotal := (*int)(nil) // Total search results (can be nil)

	// Date filter state
	activeStartTime := ""
	activeEndTime := ""

	// Pagination state - cursor-based
	allEntries := entries
	currentCursor := nextCursor // Cursor for loading next page
	hasNextPage := hasMore
	totalAvailable := totalCount // Can be nil in tail mode

	// Disable input buffering
	runCmd := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Save terminal state
	runCmd("stty", "-echo", "-icanon")
	defer runCmd("stty", "echo", "icanon")

	// Get terminal height
	getTerminalHeight := func() int {
		cmd := exec.Command("tput", "lines")
		output, err := cmd.Output()
		if err != nil {
			return 40 // Default fallback
		}
		height, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil {
			return 40
		}
		return height
	}

	termHeight := getTerminalHeight()
	// Reserve space for header (3 lines) and footer (2 lines)
	viewportHeight := termHeight - 5
	if viewportHeight < 10 {
		viewportHeight = 10 // Minimum viewport
	}

	// Forward declare functions
	var renderScreen func()
	var loadNextPage func()
	var performSearch func(query string)
	var reloadWithDateFilter func(start, end string)

	// Reload data with date filter
	reloadWithDateFilter = func(start, end string) {
		loading = true
		status = "Loading logs with date filter..."
		renderScreen()

		go func() {
			// Build query with date filters
			queryParams := url.Values{}
			for k, v := range ctx.BaseQuery {
				queryParams[k] = v
			}

			// Add date filters
			if start != "" {
				parsed, err := parseTimeArg(start)
				if err != nil {
					status = fmt.Sprintf("Invalid start time: %v", err)
					loading = false
					renderScreen()
					return
				}
				t, err := time.Parse(time.RFC3339, parsed)
				if err != nil {
					status = fmt.Sprintf("Failed to parse start time: %v", err)
					loading = false
					renderScreen()
					return
				}
				queryParams.Set("start_time", strconv.FormatInt(t.UnixMilli(), 10))
			}

			if end != "" {
				parsed, err := parseTimeArg(end)
				if err != nil {
					status = fmt.Sprintf("Invalid end time: %v", err)
					loading = false
					renderScreen()
					return
				}
				t, err := time.Parse(time.RFC3339, parsed)
				if err != nil {
					status = fmt.Sprintf("Failed to parse end time: %v", err)
					loading = false
					renderScreen()
					return
				}
				queryParams.Set("end_time", strconv.FormatInt(t.UnixMilli(), 10))
			}

			// Make API request
			fullURL := ctx.Endpoint + "?" + queryParams.Encode()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
			if err != nil {
				status = fmt.Sprintf("Request error: %v", err)
				loading = false
				renderScreen()
				return
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer "+ctx.Token)

			resp, err := ctx.Client.Do(req)
			if err != nil {
				status = fmt.Sprintf("Request error: %v", err)
				loading = false
				renderScreen()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				status = fmt.Sprintf("Request failed: %s", resp.Status)
				loading = false
				renderScreen()
				return
			}

			var payload logResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				status = fmt.Sprintf("Parse error: %v", err)
				loading = false
				renderScreen()
				return
			}

			// Update state
			allEntries = payload.Data
			hasNextPage = payload.Meta.HasMore
			totalAvailable = payload.Meta.Total
			if payload.Meta.NextCursor != nil {
				currentCursor = *payload.Meta.NextCursor
			} else {
				currentCursor = ""
			}
			currentIdx = 0
			expanded = make(map[int]bool)
			expandedScrollOffset = make(map[int]int)
			searchActive = false
			searchQuery = ""
			activeStartTime = start
			activeEndTime = end

			loading = false
			if len(payload.Data) == 0 {
				status = "No logs found for the specified date range"
			} else {
				filterMsg := ""
				if start != "" || end != "" {
					filterMsg = " (filtered)"
				}
				status = fmt.Sprintf("Loaded %d entries%s", len(payload.Data), filterMsg)
			}
			renderScreen()

			// Clear status after 3 seconds
			go func() {
				time.Sleep(3 * time.Second)
				status = ""
				renderScreen()
			}()
		}()
	}

	// Search function - performs server-side search
	performSearch = func(query string) {
		if query == "" {
			// Clear search - restore to normal browsing mode
			searchQuery = ""
			searchActive = false
			searchMatches = []int{}
			currentIdx = 0
			status = "Search cleared - back to normal mode"
			renderScreen()
			return
		}

		searchQuery = query
		searchActive = true
		searchCursor = "" // Start from beginning
		currentIdx = 0
		loading = true
		status = fmt.Sprintf("Searching for '%s'...", query)
		renderScreen()

		// Fetch search results from server
		go func() {
			results, hasMore, total, cursor, err := fetcher("", query) // Empty cursor for first search
			if err != nil {
				status = fmt.Sprintf("Search error: %v", err)
				loading = false
				renderScreen()
				return
			}

			allEntries = results
			searchHasMore = hasMore
			searchTotal = total
			searchCursor = cursor
			loading = false

			if len(results) > 0 {
				// Build searchMatches for n/N navigation
				searchMatches = make([]int, len(results))
				for i := range results {
					searchMatches[i] = i
				}
				moreMsg := ""
				if hasMore {
					moreMsg = " - scroll down to load more"
				}
				// Use actual result count, not the (broken) total from backend
				totalMsg := fmt.Sprintf("%d", len(results))
				if total != nil && *total > 0 {
					totalMsg = fmt.Sprintf("%d of %d", len(results), *total)
				} else if hasMore {
					totalMsg = fmt.Sprintf("%d+", len(results))
				}
				status = fmt.Sprintf("Found %s results%s - Esc to clear", totalMsg, moreMsg)
			} else {
				searchMatches = []int{}
				status = fmt.Sprintf("No matches for '%s' (Esc: clear)", query)
			}
			renderScreen()
		}()
	}

	renderScreen = func() {
		// Clear screen
		fmt.Print("\033[2J\033[H")

		// Header shows different info for search vs normal mode
		headerText := ""
		loadingText := ""
		if loading {
			loadingText = " (loading...)"
		}

		// Show active date filter if any
		dateFilterText := ""
		if activeStartTime != "" || activeEndTime != "" {
			if activeStartTime != "" && activeEndTime != "" {
				dateFilterText = fmt.Sprintf(" [%s to %s]", activeStartTime, activeEndTime)
			} else if activeStartTime != "" {
				dateFilterText = fmt.Sprintf(" [from %s]", activeStartTime)
			} else {
				dateFilterText = fmt.Sprintf(" [until %s]", activeEndTime)
			}
		}

		if searchActive {
			totalInfo := ""
			if searchTotal != nil && *searchTotal > 0 {
				totalInfo = fmt.Sprintf(" of %d total", *searchTotal)
			} else if searchHasMore {
				totalInfo = " (more available)"
			}
			headerText = fmt.Sprintf("Search Results for '%s' (%d loaded%s)%s%s", searchQuery, len(allEntries), totalInfo, dateFilterText, loadingText)
		} else {
			totalInfo := ""
			if totalAvailable != nil && *totalAvailable > 0 {
				totalInfo = fmt.Sprintf(" of %d total", *totalAvailable)
			} else if hasNextPage {
				totalInfo = " (more available)"
			}
			headerText = fmt.Sprintf("Logs (%d loaded%s)%s%s", len(allEntries), totalInfo, dateFilterText, loadingText)
		}

		fmt.Printf("%s - Use j/k or ↓/↑ to navigate, Space/Enter to expand/collapse, q to quit\n", headerText)
		if status != "" {
			fmt.Printf("%s\n", style(status, "33", withColor))
		}
		fmt.Println(strings.Repeat("─", 80))

		// Calculate viewport window
		// Center the current index in the viewport when possible
		viewportStart := currentIdx - (viewportHeight / 2)
		if viewportStart < 0 {
			viewportStart = 0
		}
		viewportEnd := viewportStart + viewportHeight
		if viewportEnd > len(allEntries) {
			viewportEnd = len(allEntries)
			viewportStart = viewportEnd - viewportHeight
			if viewportStart < 0 {
				viewportStart = 0
			}
		}

		// Render only visible entries
		linesRendered := 0
		for i := viewportStart; i < viewportEnd && i < len(allEntries) && linesRendered < viewportHeight; i++ {
			entry := allEntries[i]
			cursor := "  "
			if i == currentIdx {
				cursor = style("▶ ", "36", withColor)
			}

			if expanded[i] {
				// Show full JSON when expanded - with scrolling support
				jsonBytes, _ := json.MarshalIndent(entry, "  ", "  ")
				jsonLines := strings.Split(string(jsonBytes), "\n")

				// Get scroll offset for this entry
				scrollOffset := expandedScrollOffset[i]
				if scrollOffset < 0 {
					scrollOffset = 0
				}
				if scrollOffset >= len(jsonLines) {
					scrollOffset = len(jsonLines) - 1
				}
				expandedScrollOffset[i] = scrollOffset

				// Render visible portion of expanded JSON
				for lineIdx := scrollOffset; lineIdx < len(jsonLines) && linesRendered < viewportHeight; lineIdx++ {
					prefix := "  "
					if lineIdx == scrollOffset {
						prefix = cursor // Show cursor on first visible line
					}
					fmt.Printf("%s%s\n", prefix, jsonLines[lineIdx])
					linesRendered++
				}

				// Show scroll indicator if there's more content
				if scrollOffset > 0 || scrollOffset+linesRendered < len(jsonLines) {
					scrollInfo := fmt.Sprintf("  [Lines %d-%d of %d]", scrollOffset+1, scrollOffset+linesRendered, len(jsonLines))
					if linesRendered < viewportHeight {
						fmt.Println(style(scrollInfo, "90", withColor))
						linesRendered++
					}
				}
			} else {
				// Show formatted log line
				fmt.Printf("%s%s\n", cursor, formatEntry(entry, withColor))
				linesRendered++
			}
		}

		// Fill remaining viewport space if needed
		for i := linesRendered; i < viewportHeight; i++ {
			fmt.Println()
		}

		fmt.Println(strings.Repeat("─", 80))

		// Footer with navigation info
		moreInfo := ""
		if searchActive {
			if searchHasMore {
				moreInfo = " | More results (will auto-load)"
			}
		} else {
			if hasNextPage {
				moreInfo = " | More available (will auto-load)"
			}
		}

		// Show viewport position indicator
		viewportInfo := ""
		if len(allEntries) > viewportHeight {
			percent := int(float64(currentIdx) / float64(len(allEntries)) * 100)
			viewportInfo = fmt.Sprintf(" [%d%%]", percent)
		}

		helpText := "/: search | f: date filter"
		if searchActive {
			helpText = "Esc: clear search | f: date filter"
		}

		fmt.Printf("Entry %d/%d%s%s | %s | Space: expand | q: quit\n", currentIdx+1, len(allEntries), viewportInfo, moreInfo, helpText)
	}

	// Load next page in background when approaching end
	loadNextPage = func() {
		// In search mode, use search pagination
		if searchActive {
			if loading || !searchHasMore || searchCursor == "" {
				return
			}
			loading = true
			status = "Loading more search results..."
			renderScreen()

			go func() {
				newEntries, more, total, cursor, err := fetcher(searchCursor, searchQuery)
				if err != nil {
					status = fmt.Sprintf("Error loading: %v", err)
				} else {
					allEntries = append(allEntries, newEntries...)
					searchHasMore = more
					searchTotal = total
					searchCursor = cursor
					// Update searchMatches
					startIdx := len(searchMatches)
					for i := range newEntries {
						searchMatches = append(searchMatches, startIdx+i)
					}
					totalMsg := ""
					if searchTotal != nil {
						totalMsg = fmt.Sprintf(" (%d total)", *searchTotal)
					}
					status = fmt.Sprintf("Loaded %d more results%s", len(newEntries), totalMsg)
				}
				loading = false
				renderScreen()

				// Clear status after 2 seconds
				go func() {
					time.Sleep(2 * time.Second)
					status = ""
					renderScreen()
				}()
			}()
			return
		}

		// Normal mode pagination
		if loading || !hasNextPage || currentCursor == "" {
			return
		}

		loading = true
		status = "Loading more..."
		renderScreen()

		go func() {
			newEntries, more, total, cursor, err := fetcher(currentCursor, "")
			if err != nil {
				status = fmt.Sprintf("Error loading: %v", err)
			} else {
				allEntries = append(allEntries, newEntries...)
				hasNextPage = more
				totalAvailable = total
				currentCursor = cursor
				status = fmt.Sprintf("Loaded %d new entries", len(newEntries))
			}
			loading = false
			renderScreen()

			// Clear status after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				status = ""
				renderScreen()
			}()
		}()
	}

	renderScreen()

	// Read input
	buf := make([]byte, 6)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}

		input := buf[:n]

		// Handle different key codes
		switch {
		case input[0] == 'q' || input[0] == 'Q':
			// Quit
			fmt.Print("\033[2J\033[H") // Clear screen
			return

		case input[0] == 27 && n == 1:
			// Escape key (plain, not part of arrow sequence) - clear search
			if searchQuery != "" {
				performSearch("") // Empty search clears filter
				renderScreen()
			}

		case input[0] == '/':
			// Search mode - read search query
			fmt.Print("\033[2J\033[H") // Clear screen
			// Restore terminal for input
			runCmd("stty", "echo", "icanon")
			fmt.Print("Search: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				query := scanner.Text()
				performSearch(query)
			}
			// Restore raw mode
			runCmd("stty", "-echo", "-icanon")
			renderScreen()

		case input[0] == 'f' || input[0] == 'F':
			// Filter by date range
			fmt.Print("\033[2J\033[H") // Clear screen
			// Restore terminal for input
			runCmd("stty", "echo", "icanon")
			fmt.Println("Date Range Filter")
			fmt.Println("Examples: -1h, -30m, -24h, 2025-01-01")
			fmt.Println("Leave both blank to clear filters")
			fmt.Print("Start time: ")
			startScanner := bufio.NewScanner(os.Stdin)
			startTime := ""
			if startScanner.Scan() {
				startTime = strings.TrimSpace(startScanner.Text())
			}
			fmt.Print("End time (optional): ")
			endScanner := bufio.NewScanner(os.Stdin)
			endTime := ""
			if endScanner.Scan() {
				endTime = strings.TrimSpace(endScanner.Text())
			}
			// Restore raw mode
			runCmd("stty", "-echo", "-icanon")

			// Apply the filter dynamically
			reloadWithDateFilter(startTime, endTime)

		case input[0] == 'n':
			// Next entry (when filtered, just go down)
			if searchQuery != "" && currentIdx < len(allEntries)-1 {
				currentIdx++
				renderScreen()
			}

		case input[0] == 'N':
			// Previous entry (when filtered, just go up)
			if searchQuery != "" && currentIdx > 0 {
				currentIdx--
				renderScreen()
			}

		case input[0] == 'j' || (n == 3 && input[0] == 27 && input[1] == 91 && input[2] == 66):
			// Down (j or down arrow)
			if expanded[currentIdx] {
				// Scroll within expanded content
				jsonBytes, _ := json.MarshalIndent(allEntries[currentIdx], "  ", "  ")
				jsonLines := strings.Split(string(jsonBytes), "\n")
				if expandedScrollOffset[currentIdx] < len(jsonLines)-1 {
					expandedScrollOffset[currentIdx]++
					renderScreen()
				} else if currentIdx < len(allEntries)-1 {
					// At bottom of expanded content, move to next entry
					currentIdx++
					if currentIdx >= len(allEntries)-5 && hasNextPage && !loading {
						loadNextPage()
					}
					renderScreen()
				}
			} else {
				// Normal navigation
				if currentIdx < len(allEntries)-1 {
					currentIdx++

					// Auto-load next page when near the end (within 5 entries)
					if currentIdx >= len(allEntries)-5 && hasNextPage && !loading {
						loadNextPage()
					}

					renderScreen()
				}
			}

		case input[0] == 'k' || (n == 3 && input[0] == 27 && input[1] == 91 && input[2] == 65):
			// Up (k or up arrow)
			if expanded[currentIdx] {
				// Scroll within expanded content
				if expandedScrollOffset[currentIdx] > 0 {
					expandedScrollOffset[currentIdx]--
					renderScreen()
				} else if currentIdx > 0 {
					// At top of expanded content, move to previous entry
					currentIdx--
					renderScreen()
				}
			} else {
				// Normal navigation
				if currentIdx > 0 {
					currentIdx--
					renderScreen()
				}
			}

		case input[0] == 'd' || input[0] == 'D':
			// Page Down (d key) - jump down by viewport height
			newIdx := currentIdx + viewportHeight
			if newIdx >= len(allEntries) {
				newIdx = len(allEntries) - 1
			}
			if newIdx != currentIdx {
				currentIdx = newIdx
				renderScreen()

				// Auto-load next page when near the end
				if searchActive {
					if currentIdx >= len(allEntries)-viewportHeight && searchHasMore && !loading {
						loadNextPage()
					}
				} else {
					if currentIdx >= len(allEntries)-viewportHeight && hasNextPage && !loading {
						loadNextPage()
					}
				}
			}

		case input[0] == 'u' || input[0] == 'U':
			// Page Up (u key) - jump up by viewport height
			newIdx := currentIdx - viewportHeight
			if newIdx < 0 {
				newIdx = 0
			}
			if newIdx != currentIdx {
				currentIdx = newIdx
				renderScreen()
			}

		case input[0] == 'g' || input[0] == 'G':
			// Go to top (g) or bottom (G)
			if input[0] == 'g' {
				currentIdx = 0
			} else {
				currentIdx = len(allEntries) - 1
			}
			renderScreen()

		case n >= 4 && input[0] == 27 && input[1] == 91:
			// Extended escape sequences
			switch {
			case n >= 4 && input[2] == 53 && input[3] == 126: // Page Up
				newIdx := currentIdx - viewportHeight
				if newIdx < 0 {
					newIdx = 0
				}
				if newIdx != currentIdx {
					currentIdx = newIdx
					renderScreen()
				}

			case n >= 4 && input[2] == 54 && input[3] == 126: // Page Down
				newIdx := currentIdx + viewportHeight
				if newIdx >= len(allEntries) {
					newIdx = len(allEntries) - 1
				}
				if newIdx != currentIdx {
					currentIdx = newIdx
					renderScreen()

					// Auto-load next page when near the end
					if searchActive {
						if currentIdx >= len(allEntries)-viewportHeight && searchHasMore && !loading {
							loadNextPage()
						}
					} else {
						if currentIdx >= len(allEntries)-viewportHeight && hasNextPage && !loading {
							loadNextPage()
						}
					}
				}

			case input[2] == 72: // Home
				currentIdx = 0
				renderScreen()

			case input[2] == 70: // End
				currentIdx = len(allEntries) - 1
				renderScreen()
			}

		case input[0] == 13 || input[0] == 10 || input[0] == 32:
			// Enter or Space - toggle expanded
			expanded[currentIdx] = !expanded[currentIdx]
			// Reset scroll offset when toggling
			if !expanded[currentIdx] {
				delete(expandedScrollOffset, currentIdx)
			} else {
				expandedScrollOffset[currentIdx] = 0
			}
			renderScreen()
		}
	}
}
