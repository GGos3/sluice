#!/usr/bin/env bash

set -u

SCRIPT_NAME=$(basename "$0")
GITHUB_REPO="ggos3/sluice"
BINARY_NAME="sluice"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/sluice"
ACTION="install"
DRY_RUN=0
PURGE=0
VERSION=""
INSTALL_VERSION=""
DETECTED_OS=""
DETECTED_ARCH=""
TMP_DIR=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

usage() {
    cat <<EOF
Usage: ${SCRIPT_NAME} [OPTIONS]

Options:
  --install            Install ${BINARY_NAME} (default action)
  --uninstall          Remove installed ${BINARY_NAME}
  --purge              Remove ${CONFIG_DIR} during uninstall
  --version VERSION    Install a specific release tag (default: latest)
  --dry-run            Show what would be done without making changes
  --help               Show this help
EOF
}

supports_color() {
    [ -t 1 ] || return 1
    [ "${TERM:-}" != "dumb" ]
}

init_colors() {
    if ! supports_color; then
        RED=''
        GREEN=''
        YELLOW=''
        BLUE=''
        NC=''
    fi
}

log_info() {
    printf "%b[INFO]%b %s\n" "$BLUE" "$NC" "$*"
}

log_success() {
    printf "%b[OK]%b %s\n" "$GREEN" "$NC" "$*"
}

log_warn() {
    printf "%b[WARN]%b %s\n" "$YELLOW" "$NC" "$*"
}

log_error() {
    printf "%b[ERROR]%b %s\n" "$RED" "$NC" "$*" >&2
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf "$TMP_DIR"
    fi
}

fail() {
    log_error "$*"
    cleanup
    exit 1
}

run_as_root() {
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] $*"
        return 0
    fi

    if [ "${EUID:-$(id -u)}" -eq 0 ]; then
        "$@"
        return $?
    fi

    if command_exists sudo; then
        sudo "$@"
        return $?
    fi

    fail "root privileges are required and sudo is not installed"
}

detect_os() {
    os=$(uname 2>/dev/null | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux|darwin)
            DETECTED_OS="$os"
            ;;
        *)
            fail "unsupported operating system: ${os:-unknown}; supported: linux, darwin"
            ;;
    esac
}

detect_arch() {
    arch=$(uname -m 2>/dev/null)
    case "$arch" in
        x86_64|amd64)
            DETECTED_ARCH="amd64"
            ;;
        arm64|aarch64)
            DETECTED_ARCH="arm64"
            ;;
        *)
            fail "unsupported architecture: ${arch:-unknown}; supported: amd64, arm64"
            ;;
    esac
}

download_file() {
    url=$1
    destination=$2

    if command_exists curl; then
        curl -fsSL "$url" -o "$destination"
        return $?
    fi

    if command_exists wget; then
        wget -qO "$destination" "$url"
        return $?
    fi

    fail "curl or wget is required"
}

download_to_stdout() {
    url=$1

    if command_exists curl; then
        curl -fsSL "$url"
        return $?
    fi

    if command_exists wget; then
        wget -qO- "$url"
        return $?
    fi

    fail "curl or wget is required"
}

get_latest_version() {
    api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    response=$(download_to_stdout "$api_url") || fail "failed to fetch latest release from GitHub API"
    latest=$(printf '%s\n' "$response" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')
    [ -n "$latest" ] || fail "failed to determine latest release version"
    printf '%s' "$latest"
}

create_temp_dir() {
    if [ "$DRY_RUN" -eq 1 ]; then
        TMP_DIR="/tmp/${BINARY_NAME}-install-dry-run"
        return 0
    fi

    TMP_DIR=$(mktemp -d 2>/dev/null) || fail "failed to create temporary directory"
}

sha256_file() {
    file=$1

    if command_exists sha256sum; then
        sha256sum "$file" | cut -d' ' -f1
        return $?
    fi

    if command_exists shasum; then
        shasum -a 256 "$file" | cut -d' ' -f1
        return $?
    fi

    fail "sha256sum or shasum is required for checksum verification"
}

verify_checksum() {
    file=$1
    checksum_file=$2
    filename=$(basename "$file")
    expected=$(awk -v target="$filename" 'NF >= 2 && $2 == target { print $1; exit }' "$checksum_file")
    [ -n "$expected" ] || fail "checksum entry not found for ${filename}"

    actual=$(sha256_file "$file") || fail "failed to calculate checksum for ${filename}"
    [ -n "$actual" ] || fail "failed to calculate checksum for ${filename}"

    if [ "$actual" != "$expected" ]; then
        fail "checksum verification failed for ${filename}"
    fi
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --install)
                ACTION="install"
                shift
                ;;
            --uninstall)
                ACTION="uninstall"
                shift
                ;;
            --purge)
                PURGE=1
                shift
                ;;
            --version)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --version"
                    exit 1
                }
                VERSION=${2:-}
                shift 2
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            --help)
                usage
                exit 0
                ;;
            *)
                log_error "unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

validate_args() {
    if [ "$ACTION" != "uninstall" ] && [ "$PURGE" -eq 1 ]; then
        log_warn "--purge only applies to --uninstall; ignoring for install"
    fi
}

print_install_summary() {
    printf "\nSummary:\n"
    printf "  action: install\n"
    printf "  version: %s\n" "$INSTALL_VERSION"
    printf "  binary: %s/%s\n" "$INSTALL_DIR" "$BINARY_NAME"
    printf "  config dir: %s\n" "$CONFIG_DIR"
}

print_uninstall_summary() {
    printf "\nSummary:\n"
    printf "  action: uninstall\n"
    printf "  binary removed: %s/%s\n" "$INSTALL_DIR" "$BINARY_NAME"
    if [ "$PURGE" -eq 1 ]; then
        printf "  config dir removed: %s\n" "$CONFIG_DIR"
    else
        printf "  config dir preserved: %s\n" "$CONFIG_DIR"
    fi
}

install_binary() {
    detect_os
    detect_arch

    if [ -n "$VERSION" ]; then
        INSTALL_VERSION="$VERSION"
    else
        INSTALL_VERSION=$(get_latest_version)
    fi

    asset_name="${BINARY_NAME}-${DETECTED_OS}-${DETECTED_ARCH}"
    release_base_url="https://github.com/${GITHUB_REPO}/releases/download/${INSTALL_VERSION}"
    binary_url="${release_base_url}/${asset_name}"
    checksum_url="${release_base_url}/${BINARY_NAME}-checksums.txt"

    log_info "installing ${BINARY_NAME} ${INSTALL_VERSION} for ${DETECTED_OS}/${DETECTED_ARCH}"
    create_temp_dir

    binary_tmp="${TMP_DIR}/${asset_name}"
    checksum_tmp="${TMP_DIR}/${BINARY_NAME}-checksums.txt"

    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] download ${binary_url}"
        log_info "[dry-run] download ${checksum_url}"
        log_info "[dry-run] verify checksum for ${asset_name}"
        log_info "[dry-run] create ${CONFIG_DIR}"
        log_info "[dry-run] install ${asset_name} to ${INSTALL_DIR}/${BINARY_NAME}"
        print_install_summary
        cleanup
        return 0
    fi

    download_file "$binary_url" "$binary_tmp" || fail "failed to download ${binary_url}"
    download_file "$checksum_url" "$checksum_tmp" || fail "failed to download ${checksum_url}"
    verify_checksum "$binary_tmp" "$checksum_tmp"
    log_success "verified checksum for ${asset_name}"
    chmod 0755 "$binary_tmp" || fail "failed to set executable permissions on downloaded binary"
    run_as_root mkdir -p "$CONFIG_DIR" || fail "failed to create ${CONFIG_DIR}"
    run_as_root install -m 0755 "$binary_tmp" "$INSTALL_DIR/$BINARY_NAME" || fail "failed to install ${BINARY_NAME} to ${INSTALL_DIR}"

    log_success "installed ${BINARY_NAME} ${INSTALL_VERSION} to ${INSTALL_DIR}/${BINARY_NAME}"
    if [ -d "$CONFIG_DIR" ]; then
        log_success "ensured config directory exists at ${CONFIG_DIR}"
    fi
    print_install_summary
    cleanup
}

uninstall_binary() {
    target_path="${INSTALL_DIR}/${BINARY_NAME}"

    if [ -e "$target_path" ]; then
        run_as_root rm -f "$target_path" || fail "failed to remove ${target_path}"
        log_success "removed ${target_path}"
    else
        log_warn "binary not present: ${target_path}"
    fi

    if [ "$PURGE" -eq 1 ]; then
        if [ -d "$CONFIG_DIR" ]; then
            run_as_root rm -rf "$CONFIG_DIR" || fail "failed to remove ${CONFIG_DIR}"
            log_success "removed ${CONFIG_DIR}"
        else
            log_warn "config directory not present: ${CONFIG_DIR}"
        fi
    else
        log_info "preserved ${CONFIG_DIR}; use --purge to remove it"
    fi

    print_uninstall_summary
    cleanup
}

main() {
    init_colors
    parse_args "$@"
    validate_args

    case "$ACTION" in
        install)
            install_binary
            ;;
        uninstall)
            uninstall_binary
            ;;
    esac
}

main "$@"
