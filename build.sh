#!/bin/bash

set -e

# Build script for tailstream-client
# 
# Usage:
#   ./build.sh                    # Standard production build
#   LOCAL=1 ./build.sh            # Build for local testing (app.tailstream.test)
#   VERSION=v1.0.0 ./build.sh     # Build with version tag

PROJECT_NAME="tailstream-client"
BUILD_DIR="dist"

# Detect local build mode
if [ "$LOCAL" = "1" ]; then
    echo "🧪 Building for LOCAL testing..."
    PROJECT_NAME="tailstream-client-test"
    BASE_URL="https://app.tailstream.test"
    INSECURE_TLS="true"
else
    echo "Building $PROJECT_NAME..."
    BASE_URL="https://app.tailstream.io"
    INSECURE_TLS="false"
fi

# Clean and create build directory
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# Get build info
VERSION=${VERSION:-"dev"}
BUILD_DATE=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

if [ "$LOCAL" = "1" ]; then
    echo "Base URL: $BASE_URL"
    echo "Insecure TLS: $INSECURE_TLS"
fi
echo "Version: $VERSION"
echo "Build Date: $BUILD_DATE"
echo "Git Commit: $GIT_COMMIT"

# Build ldflags
LDFLAGS="-w -s -X 'main.Version=$VERSION' -X 'main.BuildDate=$BUILD_DATE' -X 'main.GitCommit=$GIT_COMMIT'"
if [ "$LOCAL" = "1" ]; then
    LDFLAGS="$LDFLAGS -X 'main.defaultBaseURL=$BASE_URL' -X 'main.insecureSkipTLSStr=$INSECURE_TLS'"
fi

# Build for Linux x64
cd client
echo "Building for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="$LDFLAGS" \
    -o "../$BUILD_DIR/$PROJECT_NAME-linux-amd64" \
    .

echo "Building for linux/arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -ldflags="$LDFLAGS" \
    -o "../$BUILD_DIR/$PROJECT_NAME-linux-arm64" \
    .

echo "Building for darwin/amd64 (macOS Intel)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build \
    -ldflags="$LDFLAGS" \
    -o "../$BUILD_DIR/$PROJECT_NAME-darwin-amd64" \
    .

echo "Building for darwin/arm64 (macOS Apple Silicon)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
    -ldflags="$LDFLAGS" \
    -o "../$BUILD_DIR/$PROJECT_NAME-darwin-arm64" \
    .

cd ..

# If VERSION is set, create checksums for release
if [ -n "$VERSION" ]; then
    echo "Creating checksums for release $VERSION..."
    cd "$BUILD_DIR"
    sha256sum * > checksums.txt
    echo "✓ Checksums created:"
    cat checksums.txt
    cd ..
fi

echo ""
echo "✓ Build complete!"
ls -lah "$BUILD_DIR"

