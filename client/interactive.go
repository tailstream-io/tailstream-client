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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
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
	expandedScrollOffset := make(map[int]int)   // Track vertical scroll offset within expanded entries
	horizontalScrollOffset := make(map[int]int) // Track horizontal scroll offset for each entry
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

	// Set up signal handling for terminal resize
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)

	// Get terminal dimensions using ioctl (more reliable than tput)
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	getTerminalSize := func() (int, int) {
		ws := &winsize{}
		retCode, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
			uintptr(syscall.Stdin),
			uintptr(syscall.TIOCGWINSZ),
			uintptr(unsafe.Pointer(ws)))

		if int(retCode) == -1 {
			// Fallback to tput if ioctl fails
			cmd := exec.Command("tput", "lines")
			output, err := cmd.Output()
			height := 40
			if err == nil {
				if h, e := strconv.Atoi(strings.TrimSpace(string(output))); e == nil {
					height = h
				}
			}

			cmd = exec.Command("tput", "cols")
			output, err = cmd.Output()
			width := 80
			if err == nil {
				if w, e := strconv.Atoi(strings.TrimSpace(string(output))); e == nil {
					width = w
				}
			}
			_ = errno // avoid unused variable
			return height, width
		}
		return int(ws.Row), int(ws.Col)
	}

	getTerminalHeight := func() int {
		h, _ := getTerminalSize()
		return h
	}

	getTerminalWidth := func() int {
		_, w := getTerminalSize()
		return w
	}

	termHeight := getTerminalHeight()
	termWidth := getTerminalWidth()
	// Reserve space for: header (1) + status (1) + separator (1) + separator (1) + footer (1) = 5 lines
	// The remaining space is for log content
	viewportHeight := termHeight - 5
	if viewportHeight < 1 {
		viewportHeight = 1 // Absolute minimum
	}

	// Helper to truncate a line to fit terminal width
	truncateLine := func(line string, maxWidth int) string {
		if len(line) <= maxWidth {
			return line
		}
		if maxWidth <= 3 {
			return "..."
		}
		return line[:maxWidth-3] + "..."
	}

	// Helper to extract a horizontal window from a line with scroll offset
	horizontalWindow := func(line string, offset int, maxWidth int) string {
		lineLen := len(line)

		// If line fits entirely, no scrolling needed
		if lineLen <= maxWidth {
			return line
		}

		// Clamp offset
		if offset < 0 {
			offset = 0
		}
		maxOffset := lineLen - maxWidth
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset > maxOffset {
			offset = maxOffset
		}

		// Extract window
		end := offset + maxWidth
		if end > lineLen {
			end = lineLen
		}

		result := line[offset:end]

		// Add indicators for scrolled content
		if offset > 0 && end < lineLen {
			// Content on both sides - show both indicators
			if len(result) > 2 {
				result = "<" + result[1:len(result)-1] + ">"
			}
		} else if offset > 0 {
			// Content on left only
			if len(result) > 0 {
				result = "<" + result[1:]
			}
		} else if end < lineLen {
			// Content on right only
			if len(result) > 0 {
				result = result[:len(result)-1] + ">"
			}
		}

		return result
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
		// Update terminal dimensions in case of resize
		termHeight = getTerminalHeight()
		termWidth = getTerminalWidth()
		// Calculate viewport height: we need to reserve exactly 5 lines for UI elements:
		// - Header line (1)
		// - Status line (1)
		// - Top separator (1)
		// - Bottom separator (1)
		// - Footer line (1)
		// Everything else is content
		viewportHeight = termHeight - 5
		if viewportHeight < 1 {
			viewportHeight = 1 // Absolute minimum
		}

		// Build entire screen content in a buffer to avoid tearing
		var screen strings.Builder

		// Save cursor, hide cursor, move to home
		screen.WriteString("\033[?25l")  // Hide cursor
		screen.WriteString("\033[H")     // Move to top-left
		screen.WriteString("\033[J")     // Clear from cursor to end

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

		// Print header with line truncation
		headerLine1 := headerText + " - Use j/k or ↓/↑ to navigate, Space/Enter to expand/collapse, q to quit"
		screen.WriteString(truncateLine(headerLine1, termWidth))
		screen.WriteString("\033[K\n")  // Clear to end of line

		if status != "" {
			screen.WriteString(truncateLine(style(status, "33", withColor), termWidth))
		}
		screen.WriteString("\033[K\n")  // Clear to end of line

		separatorLine := strings.Repeat("─", termWidth)
		screen.WriteString(separatorLine)
		screen.WriteString("\033[K\n")  // Clear to end of line

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

			// Get horizontal scroll offset for this entry
			hOffset := horizontalScrollOffset[i]

			if expanded[i] {
				// Show full JSON when expanded - with scrolling support
				jsonBytes, _ := json.MarshalIndent(entry, "  ", "  ")
				jsonLines := strings.Split(string(jsonBytes), "\n")

				// Get vertical scroll offset for this entry
				scrollOffset := expandedScrollOffset[i]
				if scrollOffset < 0 {
					scrollOffset = 0
				}
				if scrollOffset >= len(jsonLines) {
					scrollOffset = len(jsonLines) - 1
				}
				expandedScrollOffset[i] = scrollOffset

				// Render visible portion of expanded JSON with horizontal scrolling
				for lineIdx := scrollOffset; lineIdx < len(jsonLines) && linesRendered < viewportHeight; lineIdx++ {
					prefix := "  "
					if lineIdx == scrollOffset {
						prefix = cursor // Show cursor on first visible line
					}
					line := fmt.Sprintf("%s%s", prefix, jsonLines[lineIdx])
					// Apply horizontal scrolling
					screen.WriteString(horizontalWindow(line, hOffset, termWidth))
					screen.WriteString("\033[0m\033[K\n")  // Reset formatting and clear to end of line
					linesRendered++
				}

				// Show scroll indicator if there's more content
				if scrollOffset > 0 || scrollOffset+linesRendered < len(jsonLines) {
					scrollInfo := fmt.Sprintf("  [Lines %d-%d of %d]", scrollOffset+1, scrollOffset+linesRendered, len(jsonLines))
					if linesRendered < viewportHeight {
						screen.WriteString(horizontalWindow(style(scrollInfo, "90", withColor), hOffset, termWidth))
						screen.WriteString("\033[0m\033[K\n")  // Reset formatting and clear to end of line
						linesRendered++
					}
				}
			} else {
				// Show formatted log line with horizontal scrolling
				line := fmt.Sprintf("%s%s", cursor, formatEntry(entry, withColor))
				screen.WriteString(horizontalWindow(line, hOffset, termWidth))
				screen.WriteString("\033[0m\033[K\n")  // Reset formatting and clear to end of line
				linesRendered++
			}
		}

		// Fill remaining viewport space if needed
		for i := linesRendered; i < viewportHeight; i++ {
			screen.WriteString("\033[K\n")  // Clear empty lines
		}

		screen.WriteString(separatorLine)
		screen.WriteString("\033[K\n")  // Clear to end of line

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

		footerLine := fmt.Sprintf("Entry %d/%d%s%s | %s | Space: expand | q: quit", currentIdx+1, len(allEntries), viewportInfo, moreInfo, helpText)
		screen.WriteString(truncateLine(footerLine, termWidth))
		screen.WriteString("\033[0m\033[K")  // Reset formatting and clear to end of line (NO newline!)

		// Clear any remaining lines below footer to prevent artifacts
		screen.WriteString("\033[J")  // Clear from cursor to end of screen

		// Show cursor and write entire buffer at once
		screen.WriteString("\033[?25h")  // Show cursor
		fmt.Print(screen.String())
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

	// Handle resize signals in background
	go func() {
		for range sigwinch {
			renderScreen()
		}
	}()

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
					oldIdx := currentIdx
					currentIdx++
					// Reset horizontal scroll when changing entries
					if _, exists := horizontalScrollOffset[currentIdx]; !exists {
						horizontalScrollOffset[currentIdx] = 0
					}
					delete(horizontalScrollOffset, oldIdx) // Clean up old entry to save memory
					if currentIdx >= len(allEntries)-5 && hasNextPage && !loading {
						loadNextPage()
					}
					renderScreen()
				}
			} else {
				// Normal navigation
				if currentIdx < len(allEntries)-1 {
					oldIdx := currentIdx
					currentIdx++
					// Reset horizontal scroll when changing entries
					if _, exists := horizontalScrollOffset[currentIdx]; !exists {
						horizontalScrollOffset[currentIdx] = 0
					}
					delete(horizontalScrollOffset, oldIdx) // Clean up old entry to save memory

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
					oldIdx := currentIdx
					currentIdx--
					// Reset horizontal scroll when changing entries
					if _, exists := horizontalScrollOffset[currentIdx]; !exists {
						horizontalScrollOffset[currentIdx] = 0
					}
					delete(horizontalScrollOffset, oldIdx) // Clean up old entry to save memory
					renderScreen()
				}
			} else {
				// Normal navigation
				if currentIdx > 0 {
					oldIdx := currentIdx
					currentIdx--
					// Reset horizontal scroll when changing entries
					if _, exists := horizontalScrollOffset[currentIdx]; !exists {
						horizontalScrollOffset[currentIdx] = 0
					}
					delete(horizontalScrollOffset, oldIdx) // Clean up old entry to save memory
					renderScreen()
				}
			}

		case n == 3 && input[0] == 27 && input[1] == 91 && input[2] == 67:
			// Right arrow - scroll right horizontally
			// Get the actual line content to calculate max offset
			var lineContent string
			if expanded[currentIdx] {
				jsonBytes, _ := json.MarshalIndent(allEntries[currentIdx], "  ", "  ")
				jsonLines := strings.Split(string(jsonBytes), "\n")
				if len(jsonLines) > 0 {
					// Use the longest line in expanded view
					for _, jsonLine := range jsonLines {
						if len(jsonLine) > len(lineContent) {
							lineContent = jsonLine
						}
					}
				}
			} else {
				lineContent = fmt.Sprintf("%s%s", style("▶ ", "36", withColor), formatEntry(allEntries[currentIdx], withColor))
			}

			// Calculate max offset
			maxOffset := len(lineContent) - termWidth
			if maxOffset < 0 {
				maxOffset = 0
			}

			// Only scroll if we haven't reached the end
			newOffset := horizontalScrollOffset[currentIdx] + 10
			if newOffset > maxOffset {
				newOffset = maxOffset
			}
			horizontalScrollOffset[currentIdx] = newOffset
			renderScreen()

		case n == 3 && input[0] == 27 && input[1] == 91 && input[2] == 68:
			// Left arrow - scroll left horizontally
			horizontalScrollOffset[currentIdx] -= 10
			if horizontalScrollOffset[currentIdx] < 0 {
				horizontalScrollOffset[currentIdx] = 0
			}
			renderScreen()

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
