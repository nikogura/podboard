#!/usr/bin/env bash

set -euo pipefail

# podboard installation script
# Usage: curl -sSL https://raw.githubusercontent.com/nikogura/podboard/main/install.sh | sh

REPO="nikogura/podboard"
BINARY_NAME="podboard"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
GITHUB_API="https://api.github.com"
GITHUB_RAW="https://github.com"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Detect OS and architecture
detect_platform() {
    local os arch

    # Detect OS
    case "$(uname -s)" in
        Linux*)   os="linux" ;;
        Darwin*)  os="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)        print_error "Unsupported operating system: $(uname -s)" ;;
    esac

    # Detect architecture
    case "$(uname -m)" in
        x86_64)   arch="amd64" ;;
        aarch64)  arch="arm64" ;;
        arm64)    arch="arm64" ;;
        *)        print_error "Unsupported architecture: $(uname -m)" ;;
    esac

    if [[ "$os" == "windows" ]]; then
        echo "${BINARY_NAME}-${os}-${arch}.exe"
    else
        echo "${BINARY_NAME}-${os}-${arch}"
    fi
}

# Get the latest release version
get_latest_version() {
    local version
    version=$(curl -sSL "${GITHUB_API}/repos/${REPO}/releases/latest" | \
              grep '"tag_name":' | \
              sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')

    if [[ -z "$version" ]]; then
        print_error "Failed to get latest version"
    fi

    echo "$version"
}

# Check if binary exists in PATH
check_existing_installation() {
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        local existing_version
        existing_version=$("$BINARY_NAME" --version 2>/dev/null | head -n1 || echo "unknown")
        print_warning "Found existing installation: $existing_version"
        print_status "Continuing with installation..."
    fi
}

# Download and install binary
install_binary() {
    local platform version download_url temp_file install_path

    platform=$(detect_platform)
    version=$(get_latest_version)

    print_status "Detected platform: $platform"
    print_status "Latest version: $version"

    download_url="${GITHUB_RAW}/${REPO}/releases/download/${version}/${platform}"
    temp_file=$(mktemp)

    # Use sudo for system-wide installation if needed
    if [[ ! -w "$INSTALL_DIR" ]]; then
        print_status "Installing to system directory, you may be prompted for your password"
        install_path="sudo install"
    else
        install_path="install"
    fi

    print_status "Downloading ${BINARY_NAME} ${version}..."
    if ! curl -sSL -o "$temp_file" "$download_url"; then
        print_error "Failed to download $download_url"
    fi

    print_status "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
    if ! $install_path -m 755 "$temp_file" "${INSTALL_DIR}/${BINARY_NAME}"; then
        print_error "Failed to install binary to ${INSTALL_DIR}"
    fi

    # Clean up
    rm -f "$temp_file"

    print_success "${BINARY_NAME} ${version} installed successfully!"

    # Verify installation
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        print_success "Installation verified: $(command -v "$BINARY_NAME")"
        print_status "Run '${BINARY_NAME}' to get started"
    else
        print_warning "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
        print_warning "Make sure ${INSTALL_DIR} is in your PATH"
        print_status "Add this to your shell profile:"
        print_status "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi

    # Show quick start info
    echo
    print_status "🚀 Quick Start:"
    print_status "  ${BINARY_NAME}                    # Start with default settings"
    print_status "  ${BINARY_NAME} --help             # Show all options"
    print_status "  ${BINARY_NAME} --bind-address=:8080  # Use different port"
    echo
    print_status "Then open http://localhost:9999 in your browser"
}

# Main installation flow
main() {
    echo
    print_status "Installing podboard - Kubernetes Pod Dashboard"
    echo

    # Check for required tools
    for tool in curl uname; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            print_error "Required tool not found: $tool"
        fi
    done

    # Check existing installation
    check_existing_installation

    # Install binary
    install_binary

    echo
    print_success "Installation complete!"
    print_status "Documentation: https://github.com/${REPO}#readme"
    echo
}

# Run main function
main "$@"