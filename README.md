# Tailstream Client

[![Go](https://github.com/tailstream-io/tailstream-client/actions/workflows/go.yml/badge.svg)](https://github.com/tailstream-io/tailstream-client/actions/workflows/go.yml)

A powerful command-line client for querying and analyzing logs from the Tailstream API with an interactive viewer, real-time search, and flexible filtering.

## Quick Start

```bash
# 1. Login with OAuth
tailstream-client --login

# 2. View your logs interactively
tailstream-client --start "-1h"

# 3. That's it! Use j/k to navigate, / to search, f to filter by date
```

## Features

- 🔐 **OAuth Device Flow** - Secure authentication, no manual token management
- 🎯 **Interactive Mode** - Navigate, search, and filter logs in real-time
- 🔍 **Powerful Search** - Server-side and client-side search with highlighting
- ⏰ **Flexible Time Ranges** - Relative (`-1h`, `-30m`) or absolute dates
- 🎨 **Syntax Highlighting** - Color-coded log levels (ERROR, WARN, INFO, DEBUG)
- 📊 **Stream Selection** - Pick from your streams with smart defaults
- ⚡ **Fast** - Cursor-based pagination, lazy loading
- 💾 **Config Storage** - Remembers your preferences in `~/.tailstream-client.yaml`

## Installation

### Download Binary (Recommended)

Download the latest release for your platform:
- [Linux (x86_64)](https://github.com/tailstream-io/tailstream-client/releases/latest/download/tailstream-client-linux-amd64)
- [Linux (ARM64)](https://github.com/tailstream-io/tailstream-client/releases/latest/download/tailstream-client-linux-arm64)
- [macOS (Apple Silicon)](https://github.com/tailstream-io/tailstream-client/releases/latest/download/tailstream-client-darwin-arm64)
- [macOS (Intel)](https://github.com/tailstream-io/tailstream-client/releases/latest/download/tailstream-client-darwin-amd64)

```bash
# Make it executable
chmod +x tailstream-client-*

# Move to your PATH
mv tailstream-client-* /usr/local/bin/tailstream-client
```

### Build from Source

```bash
git clone https://github.com/tailstream-io/tailstream-client.git
cd tailstream-client
./build.sh
```

Binaries will be in `dist/`.

**Local Testing Build:**

To build for local testing against `app.tailstream.test`:

```bash
LOCAL=1 ./build.sh
```

This creates `dist/tailstream-client-test-*` binaries configured for local development.

## Authentication

### First Time Setup

```bash
tailstream-client --login
```

This will:
1. Show you a URL and code
2. Open your browser for authorization
3. Save credentials to `~/.tailstream-client.yaml`

You only need to login once!

### Logout

```bash
tailstream-client --logout
```

### Manual Token (Optional)

For scripts or CI/CD:

```bash
tailstream-client --token "your-token" --stream-id "stream-id" --from "-1h"
```

## Usage

### Basic Queries

```bash
# Last hour (interactive mode)
tailstream-client --from "-1h"

# Last 30 minutes
tailstream-client --from "-30m"

# Last 24 hours
tailstream-client --from "-24h"

# Specific date range
tailstream-client --from "2024-01-01" --to "2024-01-02"
```

### Interactive Mode

Interactive mode is enabled by default. Use these keys:

| Key | Action |
|-----|--------|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `d` / `PgDn` | Page down |
| `u` / `PgUp` | Page up |
| `g` / `Home` | Go to top |
| `G` / `End` | Go to bottom |
| `Space` / `Enter` | Expand/collapse entry (show full JSON) |
| `/` | Search |
| `f` | Filter by date range |
| `Esc` | Clear search/filter |
| `q` | Quit |

```bash
# Start interactive mode
tailstream-client --from "-1h"

# Disable interactive mode (pipe to file, etc.)
tailstream-client --from "-1h" --no-interactive

# JSON output (automatically disables interactive)
tailstream-client --from "-1h" --json
```

### Filtering & Search

```bash
# Filter by log level (server-side)
tailstream-client --from "-24h" --level ERROR

# Multiple levels
tailstream-client --from "-24h" --level ERROR --level WARN

# Filter by HTTP method
tailstream-client --from "-24h" --method POST

# Client-side search (case-insensitive)
tailstream-client --from "-1h" --search "database" --search "timeout"

# Combine filters
tailstream-client --from "-24h" --level ERROR --method POST --search "api"
```

### Output Formats

```bash
# Formatted output (default)
tailstream-client --from "-1h"

# Raw JSON
tailstream-client --from "-1h" --json

# No colors (for piping)
tailstream-client --from "-1h" --no-color

# Quiet mode (no spinner)
tailstream-client --from "-1h" --quiet
```

### Working with Multiple Streams

```bash
# Interactive stream selection (if no default set)
tailstream-client --from "-1h"

# Use specific stream
tailstream-client --stream-id "my-stream-id" --from "-1h"

# The selected stream becomes your default
```

## Command Reference

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--login` | Run OAuth login flow | - |
| `--logout` | Remove stored credentials | - |
| `--version` | Show version information | - |
| `--token` | API token (overrides config) | From config |
| `--stream-id` | Stream ID (overrides default) | From config |
| `--base-url` | API host (overrides config) | `https://app.tailstream.io` |
| `--from` | Start time (RFC3339, date, or relative) | - |
| `--to` | End time (RFC3339, date, or relative) | - |
| `--level` | Filter by log level (repeatable, e.g., ERROR, WARN, INFO) | - |
| `--method` | Filter by HTTP method (repeatable, e.g., GET, POST) | - |
| `--search` | Search query (repeatable, case-insensitive) | - |
| `--sort` | Sort direction (`asc` or `desc`) | `desc` |
| `--limit` | Max number of entries to display | `200` |
| `--per-page` | Entries per page | `200` |
| `--timeout` | HTTP request timeout | `15s` |
| `--json` | Output raw JSON | `false` |
| `--no-color` | Disable color output | `false` |
| `--quiet` | Disable progress indicator | `false` |
| `--interactive` | Enable interactive mode | `true` |
| `--no-interactive` | Disable interactive mode | `false` |

### Time Formats

The `--from` and `--to` flags accept various formats:

```bash
# Relative duration
tailstream-client --from "-1h"        # 1 hour ago
tailstream-client --from "-30m"       # 30 minutes ago
tailstream-client --from "-24h"       # 24 hours ago

# Date only (assumes 00:00:00 local time)
tailstream-client --from "2024-01-01"

# Date and time
tailstream-client --from "2024-01-01 15:04"

# RFC3339 format
tailstream-client --from "2024-01-01T15:04:05Z"

# Now
tailstream-client --from "now"
```

## Examples

### Find Recent Errors

```bash
tailstream-client --from "-1h" --level ERROR
```

### Filter by HTTP Method

```bash
# Find all POST requests in the last hour
tailstream-client --from "-1h" --method POST

# Find all GET and DELETE requests
tailstream-client --from "-1h" --method GET --method DELETE
```

### Debug Specific Request

```bash
tailstream-client --from "-24h" --search "request_id:abc123"
```

### Export Logs to File

```bash
# Formatted text
tailstream-client --from "-1h" --no-color > logs.txt

# JSON for processing
tailstream-client --from "-1h" --json > logs.json
```

### Process with jq

```bash
tailstream-client --from "-1h" --json --quiet | \
  jq '.data[] | select(.level == "error") | .message'
```

### Monitor Errors in Real-Time

```bash
# Watch for new errors (using watch command)
watch -n 5 'tailstream-client --from "-5m" --level ERROR --no-interactive'
```

## Configuration

Configuration is stored in `~/.tailstream-client.yaml`:

```yaml
base_url: https://app.tailstream.io
access_token: <your-oauth-token>
refresh_token: <your-refresh-token>
default_stream: <your-default-stream-id>
updated_at: "2024-01-01T12:00:00Z"
```

You typically don't need to edit this manually - use `--login` to authenticate.

## Development

### Project Structure

```
tailstream-client/
├── client/              # Source code
│   ├── main.go         # Entry point & CLI
│   ├── config.go       # Configuration management
│   ├── oauth.go        # OAuth authentication
│   ├── api.go          # API client
│   ├── display.go      # Formatting & colors
│   ├── interactive.go  # Interactive mode
│   ├── time.go         # Time parsing
│   └── *_test.go       # Tests
├── build.sh            # Multi-platform build script
├── test-client.sh      # Integration tests
└── README.md           # This file
```

### Building

```bash
# Build all platforms (production)
./build.sh

# Build for local testing (targets app.tailstream.test)
LOCAL=1 ./build.sh

# Build for current platform only
cd client && go build -o ../tailstream-client

# Run tests
cd client && go test ./...
```

### Build Environment Variables

- `LOCAL=1` - Build for local testing (sets base URL to `app.tailstream.test`, enables TLS skip)
- `VERSION=v1.0.0` - Set version string (used for releases)

Example:
```bash
# Build v1.0.0 for production
VERSION=v1.0.0 ./build.sh

# Build test version for local dev
LOCAL=1 VERSION=test ./build.sh
```

### Testing

```bash
# Run all unit tests
cd client && go test -v ./...

# Run tests with coverage
cd client && go test -cover ./...
```

### Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests (`cd client && go test ./...`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## Troubleshooting

### Authentication Errors

```bash
# Try logging in again
tailstream-client --logout
tailstream-client --login

# Check config file
cat ~/.tailstream-client.yaml
```

### No Logs Returned

- Check your time range is correct
- Try a wider time range: `--from "-24h"`
- Remove filters temporarily
- Use `--json` to see raw API response

### Timeout Errors

```bash
# Increase timeout for large queries
tailstream-client --from "-7d" --timeout 60s
```

### No Streams Found

1. Go to your Tailstream dashboard
2. Create a new stream
3. Run `tailstream-client --from "-1h"` again

### Interactive Mode Not Working

- Ensure your terminal supports ANSI escape codes
- Try `--no-interactive` for direct output
- Check terminal size: `tput lines` should return > 10

## License

MIT License - see [LICENSE](LICENSE) for details.

## Links

- [Tailstream](https://tailstream.io) - Log analysis platform
- [Issues](https://github.com/tailstream-io/tailstream-client/issues) - Bug reports & feature requests
- [Releases](https://github.com/tailstream-io/tailstream-client/releases) - Download binaries
