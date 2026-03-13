#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
SSH_DIR="$SCRIPT_DIR/.ssh"
ARTIFACT_DIR="$SCRIPT_DIR/../e2e-artifacts"
SERVER_IP="172.23.0.10"

COMPOSE=(docker compose -f "$COMPOSE_FILE")
MAX_RETRIES=30
SLEEP_SECONDS=2

if [[ -t 1 ]]; then
  C_GREEN='\033[0;32m'
  C_RED='\033[0;31m'
  C_YELLOW='\033[1;33m'
  C_RESET='\033[0m'
else
  C_GREEN=''
  C_RED=''
  C_YELLOW=''
  C_RESET=''
fi

log() {
  printf '%b[INFO]%b %s\n' "$C_YELLOW" "$C_RESET" "$*"
}

pass() {
  printf '%b[PASS]%b %s\n' "$C_GREEN" "$C_RESET" "$*"
}

fail() {
  printf '%b[FAIL]%b %s\n' "$C_RED" "$C_RESET" "$*" >&2
}

cleanup() {
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$SSH_DIR" >/dev/null 2>&1 || true
  if [[ -d "$SSH_DIR" ]]; then
    docker run --rm -v "$SCRIPT_DIR:/e2e" alpine:3.21 sh -c 'rm -rf /e2e/.ssh' >/dev/null 2>&1 || true
    rm -rf "$SSH_DIR" >/dev/null 2>&1 || true
  fi
}

dump_logs() {
  fail "E2E failed; dumping container logs"
  "${COMPOSE[@]}" logs >&2 || true
}

persist_failure_artifacts() {
  mkdir -p "$ARTIFACT_DIR"
  "${COMPOSE[@]}" logs >"$ARTIFACT_DIR/compose.log" 2>&1 || true
  "${COMPOSE[@]}" ps -a >"$ARTIFACT_DIR/compose-ps.log" 2>&1 || true
}

on_exit() {
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    persist_failure_artifacts
    dump_logs
  fi
  cleanup
  exit "$rc"
}

wait_for() {
  local name="$1"
  local cmd="$2"
  local i
  for ((i=1; i<=MAX_RETRIES; i++)); do
    if bash -c "$cmd" >/dev/null 2>&1; then
      pass "readiness: $name"
      return 0
    fi
    log "waiting ($i/$MAX_RETRIES): $name"
    sleep "$SLEEP_SECONDS"
  done
  fail "timeout: $name"
  return 1
}

assert_success() {
  local name="$1"
  local cmd="$2"
  if bash -c "$cmd"; then
    pass "$name"
    return 0
  fi
  fail "$name"
  return 1
}

assert_failure() {
  local name="$1"
  local cmd="$2"
  if bash -c "$cmd"; then
    fail "$name"
    return 1
  fi
  pass "$name"
  return 0
}

prepare_ssh_material() {
  rm -rf "$SSH_DIR"
  mkdir -p "$SSH_DIR"
  chmod 700 "$SSH_DIR"

  ssh-keygen -t ed25519 -f "$SSH_DIR/id_ed25519" -N "" -q
  cp "$SSH_DIR/id_ed25519.pub" "$SSH_DIR/authorized_keys"

  chmod 600 "$SSH_DIR/id_ed25519"
  chmod 644 "$SSH_DIR/id_ed25519.pub"
  chmod 600 "$SSH_DIR/authorized_keys"
}

if [[ "${1:-}" == "--cleanup" ]]; then
  cleanup
  pass "cleanup complete"
  exit 0
fi

if [[ $# -gt 0 ]]; then
  fail "unknown argument: $1"
  exit 1
fi

trap on_exit EXIT

log "preparing disposable SSH key material"
prepare_ssh_material

log "building E2E images"
"${COMPOSE[@]}" build

log "starting compose stack"
"${COMPOSE[@]}" up -d

wait_for "firewall FORWARD policy/rules queryable" "docker compose -f '$COMPOSE_FILE' exec -T firewall sh -c 'iptables -L FORWARD -n | grep -q \"Chain FORWARD (policy DROP)\"'"
wait_for "agent sshd listens on 22" "docker compose -f '$COMPOSE_FILE' exec -T agent sh -c 'ss -lnt | grep -Eq \"[:.]22\\s\"'"
wait_for "server reports ssh reverse tunnel connected" "docker compose -f '$COMPOSE_FILE' logs server 2>&1 | grep -q 'ssh reverse tunnel connected'"
wait_for "agent HTTP path through tunnel is reachable" "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 10 http://example.com >/dev/null"
wait_for "agent TUN device sluice0 exists" "docker compose -f '$COMPOSE_FILE' exec -T agent ip link show sluice0"

assert_failure "negative: direct access to server ${SERVER_IP}:18080 is blocked" "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 30 'http://${SERVER_IP}:18080' >/dev/null"
assert_success "http: curl -fsS --max-time 30 http://example.com" "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 30 http://example.com >/dev/null"
assert_success "https: curl -fsS --max-time 30 https://google.com" "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 30 https://google.com >/dev/null"

log "testing runtime domain management via control socket"

assert_success "rules: initially shows 'no rules'" \
  "docker compose -f '$COMPOSE_FILE' exec -T server sluice server rules 2>&1 | grep -q 'no rules'"

assert_success "deny: sluice server deny example.com succeeds" \
  "docker compose -f '$COMPOSE_FILE' exec -T server sluice server deny example.com"

sleep 1

assert_failure "deny: agent cannot reach denied example.com" \
  "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 15 http://example.com >/dev/null 2>&1"

assert_success "rules: lists denied example.com" \
  "docker compose -f '$COMPOSE_FILE' exec -T server sluice server rules 2>&1 | grep -q 'deny.*example.com.*runtime'"

assert_success "remove: sluice server remove example.com succeeds" \
  "docker compose -f '$COMPOSE_FILE' exec -T server sluice server remove example.com"

assert_success "remove: agent can reach example.com again" \
  "docker compose -f '$COMPOSE_FILE' exec -T agent curl -fsS --max-time 30 http://example.com >/dev/null"

assert_success "rules: no rules after remove" \
  "docker compose -f '$COMPOSE_FILE' exec -T server sluice server rules 2>&1 | grep -q 'no rules'"

pass "all E2E assertions passed"
