#!/bin/bash
# Konta installer script
# Usage: curl -sSL https://raw.githubusercontent.com/.../install.sh | bash

set -euo pipefail

REPO_URL="https://github.com/kontacd/konta"
VERSION="0.1.0"
INSTALL_DIR="/usr/local/bin"

echo "=== Konta Installer v$VERSION ==="

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is required but not installed."
    echo "Please install Go from https://golang.org/dl/"
    exit 1
fi

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is required but not installed."
    echo "Please install Docker from https://docker.com"
    exit 1
fi

# Create temporary directory
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "Downloading Konta..."
cd "$TMP_DIR"
git clone --depth 1 --branch main "$REPO_URL.git" konta

echo "Building Konta..."
cd konta
go build -o "/tmp/konta" ./cmd/konta/

echo "Installing to $INSTALL_DIR..."
sudo mv /tmp/konta "$INSTALL_DIR/konta"
sudo chmod +x "$INSTALL_DIR/konta"

echo ""
echo "✅ Konta installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Run: konta install"
echo "  2. Configure your repository"
echo "  3. Run: konta daemon"
echo ""
echo "For help: konta help"
