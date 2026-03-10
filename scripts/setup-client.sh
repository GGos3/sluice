#!/usr/bin/env bash

set -u

SCRIPT_NAME=$(basename "$0")
DEFAULT_PROXY_PORT=18080
DEFAULT_SSH_PORT=22
DEFAULT_LOCAL_PORT=3128
DEFAULT_NO_PROXY="localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
PROFILE_FILE="/etc/profile.d/sluice.sh"
APT_FILE="/etc/apt/apt.conf.d/99-sluice"
YUM_CONF="/etc/yum.conf"
DNF_CONF="/etc/dnf/dnf.conf"
WGETRC_FILE="/etc/wgetrc"
DOCKER_DROPIN_DIR="/etc/systemd/system/docker.service.d"
DOCKER_PROXY_FILE="${DOCKER_DROPIN_DIR}/http-proxy.conf"
TUNNEL_SERVICE_FILE="/etc/systemd/system/sluice-tunnel.service"
MANAGED_BEGIN="# BEGIN sluice managed block"
MANAGED_END="# END sluice managed block"

ACTION="install"
DRY_RUN=0
USE_SSH_TUNNEL=0
PROXY_HOST=""
PROXY_PORT="$DEFAULT_PROXY_PORT"
PROXY_USER=""
PROXY_PASS=""
SSH_USER="${USER:-$(id -un 2>/dev/null || printf 'root')}"
SSH_HOST=""
SSH_PORT="$DEFAULT_SSH_PORT"
LOCAL_PORT="$DEFAULT_LOCAL_PORT"
NO_PROXY_LIST="$DEFAULT_NO_PROXY"
PROXY_URL=""
LOCAL_HOME="${HOME:-}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

CONFIGURED_ITEMS=""
REMOVED_ITEMS=""
SKIPPED_ITEMS=""

usage() {
    cat <<EOF
Usage: ${SCRIPT_NAME} [OPTIONS]

Options:
  --proxy-host HOST    Proxy server hostname/IP (required for install)
  --proxy-port PORT    Proxy server port (default: 18080)
  --proxy-user USER    Proxy authentication username (optional)
  --proxy-pass PASS    Proxy authentication password (optional)
  --ssh-tunnel         Set up SSH tunnel instead of direct connection
  --ssh-user USER      SSH user for tunnel (default: current user)
  --ssh-host HOST      SSH host (same as proxy-host if not specified)
  --ssh-port PORT      SSH port (default: 22)
  --local-port PORT    Local port for SSH tunnel (default: 3128)
  --no-proxy LIST      Comma-separated no-proxy list (default: ${DEFAULT_NO_PROXY})
  --install            Install/configure proxy settings (default action)
  --uninstall          Remove all proxy settings
  --status             Show current proxy configuration
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

record_configured() {
    CONFIGURED_ITEMS="${CONFIGURED_ITEMS}$1\n"
}

record_removed() {
    REMOVED_ITEMS="${REMOVED_ITEMS}$1\n"
}

record_skipped() {
    SKIPPED_ITEMS="${SKIPPED_ITEMS}$1\n"
}

print_summary_block() {
    title=$1
    body=$2
    [ -n "$body" ] || return 0
    printf "%s\n" "$title"
    printf "%b" "$body" | while IFS= read -r line; do
        [ -n "$line" ] && printf "  - %s\n" "$line"
    done
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

ensure_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "root privileges are required for ${ACTION} mode"
        exit 1
    fi
}

detect_local_home() {
    if [ -n "${SUDO_USER:-}" ] && command_exists getent; then
        detected_home=$(getent passwd "$SUDO_USER" | cut -d: -f6 2>/dev/null || true)
        if [ -n "$detected_home" ]; then
            LOCAL_HOME="$detected_home"
        fi
    fi
    if [ -z "$LOCAL_HOME" ]; then
        LOCAL_HOME="/root"
    fi
}

run_as_local_user() {
    if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ] && command_exists sudo; then
        sudo -u "$SUDO_USER" "$@"
    else
        "$@"
    fi
}

urlencode() {
    input=$1
    output=""
    i=0
    LC_ALL=C
    while [ "$i" -lt "${#input}" ]; do
        char=${input:$i:1}
        case "$char" in
            [a-zA-Z0-9.~_-])
                output="${output}${char}"
                ;;
            *)
                printf -v hex '%02X' "'$char"
                output="${output}%${hex}"
                ;;
        esac
        i=$((i + 1))
    done
    printf '%s' "$output"
}

backup_file() {
    file=$1
    [ -e "$file" ] || return 0
    backup="${file}.bak.$(date +%Y%m%d%H%M%S)"
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] back up ${file} to ${backup}"
        return 0
    fi
    cp -a "$file" "$backup"
    log_info "backed up ${file} to ${backup}"
}

write_file_content() {
    file=$1
    content=$2
    mode=${3:-0644}
    dir=$(dirname "$file")

    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] write ${file}"
        return 0
    fi

    mkdir -p "$dir" || return 1
    tmp=$(mktemp)
    printf '%s\n' "$content" > "$tmp" || {
        rm -f "$tmp"
        return 1
    }
    chmod "$mode" "$tmp" || {
        rm -f "$tmp"
        return 1
    }
    mv "$tmp" "$file"
}

remove_file_if_exists() {
    file=$1
    label=$2
    if [ ! -e "$file" ]; then
        log_warn "${label} not present: ${file}"
        record_skipped "$label (not present)"
        return 0
    fi
    backup_file "$file" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] remove ${file}"
    else
        rm -f "$file"
    fi
    log_success "removed ${label}"
    record_removed "$label"
}

strip_managed_block() {
    file=$1
    if [ ! -f "$file" ]; then
        return 0
    fi

    tmp=$(mktemp)
    awk -v begin="$MANAGED_BEGIN" -v end="$MANAGED_END" '
        $0 == begin { skip=1; next }
        $0 == end { skip=0; next }
        !skip { print }
    ' "$file" > "$tmp"

    if [ "$DRY_RUN" -eq 1 ]; then
        rm -f "$tmp"
        log_info "[dry-run] remove managed block from ${file}"
        return 0
    fi

    mv "$tmp" "$file"
}

append_managed_block() {
    file=$1
    block_body=$2
    label=$3
    dir=$(dirname "$file")
    block=$(cat <<EOF
${MANAGED_BEGIN}
${block_body}
${MANAGED_END}
EOF
)

    if [ ! -e "$file" ]; then
        if [ "$DRY_RUN" -eq 0 ]; then
            mkdir -p "$dir" || return 1
            : > "$file" || return 1
        fi
    fi

    if [ -e "$file" ]; then
        backup_file "$file" || return 1
    fi
    strip_managed_block "$file" || return 1

    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] append managed block to ${file}"
    else
        printf '\n%s\n' "$block" >> "$file"
    fi

    log_success "configured ${label}"
    record_configured "$label"
}

prepare_proxy_url() {
    if [ "$USE_SSH_TUNNEL" -eq 1 ]; then
        PROXY_URL="http://127.0.0.1:${LOCAL_PORT}"
        return 0
    fi

    auth_part=""
    if [ -n "$PROXY_USER" ]; then
        encoded_user=$(urlencode "$PROXY_USER")
        auth_part="$encoded_user"
        if [ -n "$PROXY_PASS" ]; then
            encoded_pass=$(urlencode "$PROXY_PASS")
            auth_part="${auth_part}:${encoded_pass}"
        fi
        auth_part="${auth_part}@"
    fi
    PROXY_URL="http://${auth_part}${PROXY_HOST}:${PROXY_PORT}"
}

validate_args() {
    case "$ACTION" in
        install)
            if [ -z "$PROXY_HOST" ]; then
                log_error "--proxy-host is required for install mode"
                exit 1
            fi
            if [ "$USE_SSH_TUNNEL" -eq 1 ] && [ -z "$SSH_HOST" ]; then
                SSH_HOST="$PROXY_HOST"
            fi
            prepare_proxy_url
            ;;
        uninstall|status)
            if [ -z "$SSH_HOST" ] && [ -n "$PROXY_HOST" ]; then
                SSH_HOST="$PROXY_HOST"
            fi
            ;;
        *)
            log_error "unsupported action: ${ACTION}"
            exit 1
            ;;
    esac
}

profile_content() {
    cat <<EOF
# sluice environment configuration
export HTTP_PROXY="${PROXY_URL}"
export HTTPS_PROXY="${PROXY_URL}"
export NO_PROXY="${NO_PROXY_LIST}"
export http_proxy="${PROXY_URL}"
export https_proxy="${PROXY_URL}"
export no_proxy="${NO_PROXY_LIST}"
EOF
}

apt_content() {
    cat <<EOF
Acquire::http::Proxy "${PROXY_URL}";
Acquire::https::Proxy "${PROXY_URL}";
EOF
}

docker_proxy_content() {
    cat <<EOF
[Service]
Environment="HTTP_PROXY=${PROXY_URL}" "HTTPS_PROXY=${PROXY_URL}" "NO_PROXY=${NO_PROXY_LIST}"
EOF
}

ssh_tunnel_service_content() {
    cat <<EOF
[Unit]
Description=sluice SSH tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=/usr/bin/ssh -N -L ${LOCAL_PORT}:localhost:${PROXY_PORT} -o ServerAliveInterval=60 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes ${SSH_USER}@${SSH_HOST} -p ${SSH_PORT}

[Install]
WantedBy=multi-user.target
EOF
}

configure_profile() {
    if [ -e "$PROFILE_FILE" ]; then
        backup_file "$PROFILE_FILE" || return 1
    fi
    write_file_content "$PROFILE_FILE" "$(profile_content)" 0644 || return 1
    log_success "configured shell environment"
    record_configured "environment variables (${PROFILE_FILE})"
}

configure_apt() {
    if ! command_exists apt-get && ! command_exists apt; then
        log_warn "apt not found; skipping"
        record_skipped "apt"
        return 0
    fi
    if [ -e "$APT_FILE" ]; then
        backup_file "$APT_FILE" || return 1
    fi
    write_file_content "$APT_FILE" "$(apt_content)" 0644 || return 1
    log_success "configured apt"
    record_configured "apt (${APT_FILE})"
}

configure_yum_like() {
    manager=$1
    conf_file=$2

    if ! command_exists "$manager"; then
        log_warn "${manager} not found; skipping"
        record_skipped "$manager"
        return 0
    fi

    append_managed_block "$conf_file" "proxy=${PROXY_URL}" "$manager (${conf_file})"
}

backup_user_file() {
    file=$1
    [ -n "$file" ] || return 0
    [ -e "$file" ] || return 0
    backup_file "$file"
}

configure_git() {
    if ! command_exists git; then
        log_warn "git not found; skipping"
        record_skipped "git"
        return 0
    fi
    backup_user_file "${LOCAL_HOME}/.gitconfig" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] git config --global http.proxy ${PROXY_URL}"
        log_info "[dry-run] git config --global https.proxy ${PROXY_URL}"
    else
        run_as_local_user git config --global http.proxy "$PROXY_URL" || return 1
        run_as_local_user git config --global https.proxy "$PROXY_URL" || return 1
    fi
    log_success "configured git"
    record_configured "git global proxy"
}

configure_npm() {
    if ! command_exists npm; then
        log_warn "npm not found; skipping"
        record_skipped "npm"
        return 0
    fi
    backup_user_file "${LOCAL_HOME}/.npmrc" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] npm config set proxy ${PROXY_URL}"
        log_info "[dry-run] npm config set https-proxy ${PROXY_URL}"
    else
        run_as_local_user npm config set proxy "$PROXY_URL" || return 1
        run_as_local_user npm config set https-proxy "$PROXY_URL" || return 1
    fi
    log_success "configured npm"
    record_configured "npm proxy"
}

pip_command() {
    if command_exists pip; then
        printf '%s' "pip"
    elif command_exists pip3; then
        printf '%s' "pip3"
    else
        return 1
    fi
}

configure_pip() {
    pip_cmd=$(pip_command 2>/dev/null || true)
    if [ -z "$pip_cmd" ]; then
        log_warn "pip not found; skipping"
        record_skipped "pip"
        return 0
    fi
    backup_user_file "/etc/pip.conf" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] ${pip_cmd} config --global set global.proxy ${PROXY_URL}"
    else
        "$pip_cmd" config --global set global.proxy "$PROXY_URL" || return 1
    fi
    log_success "configured pip"
    record_configured "pip global proxy"
}

configure_docker() {
    if ! command_exists docker && [ ! -d "/etc/systemd/system" ]; then
        log_warn "docker/systemd not found; skipping docker proxy configuration"
        record_skipped "docker"
        return 0
    fi
    if [ -e "$DOCKER_PROXY_FILE" ]; then
        backup_file "$DOCKER_PROXY_FILE" || return 1
    fi
    write_file_content "$DOCKER_PROXY_FILE" "$(docker_proxy_content)" 0644 || return 1
    if command_exists systemctl; then
        if [ "$DRY_RUN" -eq 1 ]; then
            log_info "[dry-run] systemctl daemon-reload"
            log_info "[dry-run] systemctl restart docker"
        else
            systemctl daemon-reload || return 1
            if systemctl list-unit-files docker.service >/dev/null 2>&1; then
                systemctl restart docker || log_warn "docker service restart failed"
            else
                log_warn "docker service not installed; config written only"
            fi
        fi
    fi
    log_success "configured docker"
    record_configured "docker systemd proxy override"
}

configure_wget() {
    if ! command_exists wget; then
        log_warn "wget not found; skipping"
        record_skipped "wget"
        return 0
    fi
    append_managed_block "$WGETRC_FILE" "use_proxy = on
http_proxy = ${PROXY_URL}
https_proxy = ${PROXY_URL}
no_proxy = ${NO_PROXY_LIST}" "wget (${WGETRC_FILE})"
}

configure_ssh_tunnel() {
    if [ "$USE_SSH_TUNNEL" -ne 1 ]; then
        return 0
    fi
    if ! command_exists ssh; then
        log_error "ssh is required for --ssh-tunnel mode"
        return 1
    fi
    if ! command_exists systemctl; then
        log_error "systemctl is required for --ssh-tunnel mode"
        return 1
    fi
    if [ -e "$TUNNEL_SERVICE_FILE" ]; then
        backup_file "$TUNNEL_SERVICE_FILE" || return 1
    fi
    write_file_content "$TUNNEL_SERVICE_FILE" "$(ssh_tunnel_service_content)" 0644 || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] systemctl daemon-reload"
        log_info "[dry-run] systemctl enable --now sluice-tunnel.service"
    else
        systemctl daemon-reload || return 1
        systemctl enable --now sluice-tunnel.service || return 1
    fi
    log_success "configured SSH tunnel service"
    record_configured "SSH tunnel service"
}

install_proxy() {
    ensure_root
    detect_local_home
    configure_ssh_tunnel || return 1
    configure_profile || return 1
    configure_apt || return 1
    configure_yum_like yum "$YUM_CONF" || return 1
    configure_yum_like dnf "$DNF_CONF" || return 1
    configure_git || return 1
    configure_npm || return 1
    configure_pip || return 1
    configure_docker || return 1
    configure_wget || return 1

    printf '\n'
    log_success "install complete"
    print_summary_block "Configured:" "$CONFIGURED_ITEMS"
    print_summary_block "Skipped:" "$SKIPPED_ITEMS"
    printf "\nProxy URL: %s\n" "$PROXY_URL"
    [ "$USE_SSH_TUNNEL" -eq 1 ] && printf "SSH tunnel: %s@%s:%s -> localhost:%s\n" "$SSH_USER" "$SSH_HOST" "$SSH_PORT" "$LOCAL_PORT"
}

remove_yum_like() {
    manager=$1
    conf_file=$2

    if [ ! -f "$conf_file" ]; then
        log_warn "${conf_file} not present; skipping ${manager} cleanup"
        record_skipped "$manager"
        return 0
    fi

    backup_file "$conf_file" || return 1
    strip_managed_block "$conf_file" || return 1
    log_success "removed ${manager} proxy configuration"
    record_removed "$manager proxy configuration"
}

remove_git() {
    if ! command_exists git; then
        log_warn "git not found; skipping"
        record_skipped "git"
        return 0
    fi
    backup_user_file "${LOCAL_HOME}/.gitconfig" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] git config --global --unset-all http.proxy"
        log_info "[dry-run] git config --global --unset-all https.proxy"
    else
        run_as_local_user git config --global --unset-all http.proxy >/dev/null 2>&1 || true
        run_as_local_user git config --global --unset-all https.proxy >/dev/null 2>&1 || true
    fi
    log_success "removed git proxy configuration"
    record_removed "git global proxy"
}

remove_npm() {
    if ! command_exists npm; then
        log_warn "npm not found; skipping"
        record_skipped "npm"
        return 0
    fi
    backup_user_file "${LOCAL_HOME}/.npmrc" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] npm config delete proxy"
        log_info "[dry-run] npm config delete https-proxy"
    else
        run_as_local_user npm config delete proxy >/dev/null 2>&1 || true
        run_as_local_user npm config delete https-proxy >/dev/null 2>&1 || true
    fi
    log_success "removed npm proxy configuration"
    record_removed "npm proxy"
}

remove_pip() {
    pip_cmd=$(pip_command 2>/dev/null || true)
    if [ -z "$pip_cmd" ]; then
        log_warn "pip not found; skipping"
        record_skipped "pip"
        return 0
    fi
    backup_user_file "/etc/pip.conf" || return 1
    if [ "$DRY_RUN" -eq 1 ]; then
        log_info "[dry-run] ${pip_cmd} config --global unset global.proxy"
    else
        "$pip_cmd" config --global unset global.proxy >/dev/null 2>&1 || true
    fi
    log_success "removed pip proxy configuration"
    record_removed "pip global proxy"
}

remove_docker() {
    if [ -e "$DOCKER_PROXY_FILE" ]; then
        backup_file "$DOCKER_PROXY_FILE" || return 1
        if [ "$DRY_RUN" -eq 1 ]; then
            log_info "[dry-run] remove ${DOCKER_PROXY_FILE}"
        else
            rm -f "$DOCKER_PROXY_FILE"
        fi
        if command_exists systemctl; then
            if [ "$DRY_RUN" -eq 1 ]; then
                log_info "[dry-run] systemctl daemon-reload"
                log_info "[dry-run] systemctl restart docker"
            else
                systemctl daemon-reload || return 1
                if systemctl list-unit-files docker.service >/dev/null 2>&1; then
                    systemctl restart docker || log_warn "docker service restart failed"
                fi
            fi
        fi
        log_success "removed docker proxy configuration"
        record_removed "docker systemd proxy override"
    else
        log_warn "docker proxy override not present"
        record_skipped "docker"
    fi
}

remove_ssh_tunnel() {
    if command_exists systemctl; then
        if [ "$DRY_RUN" -eq 1 ]; then
            log_info "[dry-run] systemctl disable --now sluice-tunnel.service"
        else
            systemctl disable --now sluice-tunnel.service >/dev/null 2>&1 || true
        fi
    fi

    if [ -e "$TUNNEL_SERVICE_FILE" ]; then
        backup_file "$TUNNEL_SERVICE_FILE" || return 1
        if [ "$DRY_RUN" -eq 1 ]; then
            log_info "[dry-run] remove ${TUNNEL_SERVICE_FILE}"
            log_info "[dry-run] systemctl daemon-reload"
        else
            rm -f "$TUNNEL_SERVICE_FILE"
            command_exists systemctl && systemctl daemon-reload || true
        fi
        log_success "removed SSH tunnel service"
        record_removed "SSH tunnel service"
    else
        log_warn "SSH tunnel service not present"
        record_skipped "SSH tunnel service"
    fi
}

uninstall_proxy() {
    ensure_root
    detect_local_home
    remove_file_if_exists "$PROFILE_FILE" "environment variables" || return 1
    remove_file_if_exists "$APT_FILE" "apt proxy configuration" || return 1
    remove_yum_like yum "$YUM_CONF" || return 1
    remove_yum_like dnf "$DNF_CONF" || return 1
    remove_git || return 1
    remove_npm || return 1
    remove_pip || return 1
    remove_docker || return 1
    if [ -f "$WGETRC_FILE" ]; then
        backup_file "$WGETRC_FILE" || return 1
        strip_managed_block "$WGETRC_FILE" || return 1
        log_success "removed wget proxy configuration"
        record_removed "wget proxy configuration"
    else
        log_warn "wget configuration file not present"
        record_skipped "wget"
    fi
    remove_ssh_tunnel || return 1

    printf '\n'
    log_success "uninstall complete"
    print_summary_block "Removed:" "$REMOVED_ITEMS"
    print_summary_block "Skipped:" "$SKIPPED_ITEMS"
}

show_file_if_exists() {
    label=$1
    file=$2
    if [ -f "$file" ]; then
        printf "\n%s (%s):\n" "$label" "$file"
        cat "$file"
    else
        printf "\n%s: not configured\n" "$label"
    fi
}

show_setting() {
    label=$1
    value=$2
    if [ -n "$value" ]; then
        printf "%s: %s\n" "$label" "$value"
    else
        printf "%s: not set\n" "$label"
    fi
}

current_proxy_for_status() {
    if [ -n "${HTTP_PROXY:-}" ]; then
        printf '%s' "$HTTP_PROXY"
        return 0
    fi
    if [ -f "$PROFILE_FILE" ]; then
        awk -F'"' '/export HTTP_PROXY=/{print $2; exit}' "$PROFILE_FILE"
    fi
}

status_git() {
    if command_exists git; then
        show_setting "git http.proxy" "$(run_as_local_user git config --global --get http.proxy 2>/dev/null || true)"
        show_setting "git https.proxy" "$(run_as_local_user git config --global --get https.proxy 2>/dev/null || true)"
    else
        printf "git: not installed\n"
    fi
}

status_npm() {
    if command_exists npm; then
        show_setting "npm proxy" "$(run_as_local_user npm config get proxy 2>/dev/null | sed 's/^null$//')"
        show_setting "npm https-proxy" "$(run_as_local_user npm config get https-proxy 2>/dev/null | sed 's/^null$//')"
    else
        printf "npm: not installed\n"
    fi
}

status_docker() {
    show_file_if_exists "Docker proxy" "$DOCKER_PROXY_FILE"
}

status_ssh_tunnel() {
    if [ -f "$TUNNEL_SERVICE_FILE" ]; then
        printf "\nSSH tunnel service:\n"
        if command_exists systemctl; then
            printf "enabled: %s\n" "$(systemctl is-enabled sluice-tunnel.service 2>/dev/null || printf 'unknown')"
            printf "active: %s\n" "$(systemctl is-active sluice-tunnel.service 2>/dev/null || printf 'unknown')"
        else
            printf "systemctl: not available\n"
        fi
        cat "$TUNNEL_SERVICE_FILE"
    else
        printf "\nSSH tunnel service: not configured\n"
    fi
}

status_connectivity() {
    proxy_for_test=$(current_proxy_for_status)
    printf "\nConnectivity test:\n"
    if [ -z "$proxy_for_test" ]; then
        printf "proxy URL not detected; skipping test\n"
        return 0
    fi
    if ! command_exists curl; then
        printf "curl not installed; skipping test\n"
        return 0
    fi
    if curl --proxy "$proxy_for_test" --silent --show-error --fail --max-time 10 --head https://example.com >/dev/null 2>&1; then
        log_success "proxy connectivity test succeeded via ${proxy_for_test}"
    else
        log_warn "proxy connectivity test failed via ${proxy_for_test}"
    fi
}

show_status() {
    detect_local_home
    printf "Current environment variables:\n"
    show_setting "HTTP_PROXY" "${HTTP_PROXY:-}"
    show_setting "HTTPS_PROXY" "${HTTPS_PROXY:-}"
    show_setting "NO_PROXY" "${NO_PROXY:-}"
    show_setting "http_proxy" "${http_proxy:-}"
    show_setting "https_proxy" "${https_proxy:-}"
    show_setting "no_proxy" "${no_proxy:-}"
    show_file_if_exists "Profile configuration" "$PROFILE_FILE"
    show_file_if_exists "APT proxy" "$APT_FILE"
    printf "\nGit configuration:\n"
    status_git
    printf "\nNPM configuration:\n"
    status_npm
    status_docker
    status_ssh_tunnel
    status_connectivity
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --proxy-host)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --proxy-host"
                    exit 1
                }
                PROXY_HOST=${2:-}
                shift 2
                ;;
            --proxy-port)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --proxy-port"
                    exit 1
                }
                PROXY_PORT=${2:-}
                shift 2
                ;;
            --proxy-user)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --proxy-user"
                    exit 1
                }
                PROXY_USER=${2:-}
                shift 2
                ;;
            --proxy-pass)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --proxy-pass"
                    exit 1
                }
                PROXY_PASS=${2:-}
                shift 2
                ;;
            --ssh-tunnel)
                USE_SSH_TUNNEL=1
                shift
                ;;
            --ssh-user)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --ssh-user"
                    exit 1
                }
                SSH_USER=${2:-}
                shift 2
                ;;
            --ssh-host)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --ssh-host"
                    exit 1
                }
                SSH_HOST=${2:-}
                shift 2
                ;;
            --ssh-port)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --ssh-port"
                    exit 1
                }
                SSH_PORT=${2:-}
                shift 2
                ;;
            --local-port)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --local-port"
                    exit 1
                }
                LOCAL_PORT=${2:-}
                shift 2
                ;;
            --no-proxy)
                [ "$#" -ge 2 ] || {
                    log_error "missing value for --no-proxy"
                    exit 1
                }
                NO_PROXY_LIST=${2:-}
                shift 2
                ;;
            --install)
                ACTION="install"
                shift
                ;;
            --uninstall)
                ACTION="uninstall"
                shift
                ;;
            --status)
                ACTION="status"
                shift
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

main() {
    init_colors
    parse_args "$@"
    validate_args

    case "$ACTION" in
        install)
            install_proxy
            ;;
        uninstall)
            uninstall_proxy
            ;;
        status)
            show_status
            ;;
    esac
}

main "$@"
