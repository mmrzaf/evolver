#!/usr/bin/env bash
set -e

VERSION="v1.0.0"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; fi
if [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi

BIN_DIR="/usr/local/bin"
BIN_NAME="evolver"

if command -v go >/dev/null 2>&1; then
    echo "Go toolchain found, building from source..."
    cd "${GITHUB_ACTION_PATH:-.}"
    go build -o "$BIN_DIR/$BIN_NAME" ./cmd/evolver
else
    echo "Downloading pre-built binary..."
    URL="https://github.com/mmrzaf/evolver/releases/download/${VERSION}/evolver-${OS}-${ARCH}"
    curl -sSL -o "$BIN_DIR/$BIN_NAME" "$URL"
    chmod +x "$BIN_DIR/$BIN_NAME"
fi

