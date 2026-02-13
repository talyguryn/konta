#!/bin/bash
# Konta GitOps Installer
# Installation: /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/talyguryn/konta/main/scripts/install.sh)"

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="talyguryn/konta"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="konta"

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux";;
        Darwin*)    echo "darwin";;
        *)          echo "unsupported";;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64)     echo "amd64";;
        aarch64)    echo "arm64";;
        arm64)      echo "arm64";;
        *)          echo "unsupported";;
    esac
}

# Get the latest release version
get_latest_version() {
    print_info "Fetching latest version..."
    
    version=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4 2>/dev/null)
    
    if [ -z "$version" ]; then
        print_error "Could not fetch latest version"
        exit 1
    fi
    
    # Remove 'v' prefix if present
    version="${version#v}"
    echo "$version"
}

# Main installation
main() {
    print_info "Installing Konta GitOps"
    echo ""
    
    # Detect OS and architecture
    OS=$(detect_os)
    ARCH=$(detect_arch)
    
    if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
        print_error "Unsupported OS/Architecture: $(uname -s) $(uname -m)"
        exit 1
    fi
    
    print_success "Detected: $(uname -s) $(uname -m) ($OS/$ARCH)"
    
    # Get latest version
    VERSION=$(get_latest_version)
    print_success "Latest version: v${VERSION}"
    
    # Determine binary name
    if [ "$OS" = "linux" ]; then
        if [ "$ARCH" = "amd64" ]; then
            BINARY_FILE="konta-linux"
        else
            BINARY_FILE="konta-linux-${ARCH}"
        fi
    elif [ "$OS" = "darwin" ]; then
        BINARY_FILE="konta-darwin-${ARCH}"
    fi
    
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY_FILE}"
    
    # Check if konta is already installed with the same version
    if [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        INSTALLED_VERSION=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null | grep -oP 'v\K[0-9.]+' || echo "unknown")
        if [ "$INSTALLED_VERSION" = "$VERSION" ]; then
            print_success "Konta v${VERSION} is already installed at ${INSTALL_DIR}/${BINARY_NAME}"
            echo ""
            print_info "Next steps:"
            echo "  1. Configure: konta install"
            echo "  2. Run once: konta run"
            echo "  3. Enable daemon: sudo konta daemon enable"
            exit 0
        fi
    fi
    
    # Download the binary
    print_info "Downloading konta v${VERSION}..."
    
    TEMP_FILE=$(mktemp)
    if ! curl -fsSL -o "$TEMP_FILE" "$DOWNLOAD_URL"; then
        print_error "Failed to download from $DOWNLOAD_URL"
        rm -f "$TEMP_FILE"
        exit 1
    fi
    
    # Verify file is not empty
    if [ ! -s "$TEMP_FILE" ]; then
        print_error "Downloaded file is empty"
        rm -f "$TEMP_FILE"
        exit 1
    fi
    
    print_success "Downloaded successfully"
    
    # Check if we need sudo
    if [ ! -w "$INSTALL_DIR" ]; then
        print_info "Need sudo to write to $INSTALL_DIR"
        
        if ! sudo -v 2>/dev/null; then
            print_error "sudo access required but not available"
            rm -f "$TEMP_FILE"
            exit 1
        fi
        
        # Use sudo for installation
        sudo mv "$TEMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    else
        mv "$TEMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    fi
    
    print_success "Installed konta v${VERSION} to ${INSTALL_DIR}/${BINARY_NAME}"
    
    echo ""
    print_info "Installation complete!"
    echo ""
    print_info "Next steps:"
    echo "  1. Configure: konta install"
    echo "  2. Run once: konta run"
    echo "  3. Enable daemon: sudo konta daemon enable"
    echo ""
    echo "For more info: konta help"
}

main "$@"
