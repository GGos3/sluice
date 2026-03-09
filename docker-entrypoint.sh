#!/bin/sh
set -e

SLUICE_MODE="${SLUICE_MODE:-run}"

case "$SLUICE_MODE" in
  server)
    CONFIG_PATH="${SLUICE_CONFIG:-/etc/sluice/config.yaml}"
    if [ "$#" -eq 0 ] || { [ "$#" -eq 1 ] && [ "$1" = "sh" ]; }; then
      exec sluice -config "$CONFIG_PATH"
    fi
    exec sluice -config "$CONFIG_PATH" "$@"
    ;;
  run)
    PROXY_HOST="${SLUICE_PROXY_HOST:-}"
    PROXY_PORT="${SLUICE_PROXY_PORT:-8080}"
    PROXY_USER="${SLUICE_PROXY_USER:-}"
    PROXY_PASS="${SLUICE_PROXY_PASS:-}"
    NO_PROXY="${SLUICE_NO_PROXY:-localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16}"

    if [ -z "$PROXY_HOST" ]; then
      echo "error: SLUICE_PROXY_HOST is required in run mode" >&2
      echo "usage: docker run -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice [command]" >&2
      exit 1
    fi

    AUTH_PART=""
    if [ -n "$PROXY_USER" ]; then
      AUTH_PART="${PROXY_USER}"
      if [ -n "$PROXY_PASS" ]; then
        AUTH_PART="${AUTH_PART}:${PROXY_PASS}"
      fi
      AUTH_PART="${AUTH_PART}@"
    fi
    PROXY_URL="http://${AUTH_PART}${PROXY_HOST}:${PROXY_PORT}"

    export HTTP_PROXY="$PROXY_URL"
    export HTTPS_PROXY="$PROXY_URL"
    export NO_PROXY="$NO_PROXY"
    export http_proxy="$PROXY_URL"
    export https_proxy="$PROXY_URL"
    export no_proxy="$NO_PROXY"

    if command -v git >/dev/null 2>&1; then
      git config --global http.proxy "$PROXY_URL"
      git config --global https.proxy "$PROXY_URL"
    fi

    echo "sluice: run mode activated"
    echo "sluice: proxy -> $PROXY_URL"
    echo "sluice: no_proxy -> $NO_PROXY"

    if [ "$#" -eq 0 ]; then
      exec sh
    fi

     exec "$@"
     ;;
  gateway)
    gateway_log()  { echo "sluice-gateway: $*"; }
    gateway_warn() { echo "sluice-gateway: WARNING: $*" >&2; }
    gateway_error(){ echo "sluice-gateway: ERROR: $*" >&2; }

    require_gateway_env() {
      SLUICE_PROXY_HOST="${SLUICE_PROXY_HOST:-}"
      SLUICE_PROXY_PORT="${SLUICE_PROXY_PORT:-8080}"
      SLUICE_PROXY_USER="${SLUICE_PROXY_USER:-}"
      SLUICE_PROXY_PASS="${SLUICE_PROXY_PASS:-}"
      SLUICE_PROXY_DOMAINS="${SLUICE_PROXY_DOMAINS:-}"
      SLUICE_REDIRECT_PORTS="${SLUICE_REDIRECT_PORTS:-http}"

      if [ -z "$SLUICE_PROXY_HOST" ]; then
        gateway_error "SLUICE_PROXY_HOST is required in gateway mode"
        exit 1
      fi

      if [ "$SLUICE_REDIRECT_PORTS" = "all" ]; then
        gateway_warn "SLUICE_REDIRECT_PORTS=all is not supported; falling back to http"
        SLUICE_REDIRECT_PORTS="http"
      fi

      PROXY_IP="$(getent hosts "$SLUICE_PROXY_HOST" | while IFS=' ' read -r ip _; do case "$ip" in *.*) printf '%s\n' "$ip"; break ;; esac; done)"
      if [ -z "$PROXY_IP" ]; then
        gateway_error "failed to resolve IPv4 address for $SLUICE_PROXY_HOST"
        exit 1
      fi
    }

    build_redsocks_config() {
      AUTH_LINES=""
      if [ -n "$SLUICE_PROXY_USER" ]; then
        AUTH_LINES="    login = \"$SLUICE_PROXY_USER\";
"
        if [ -n "$SLUICE_PROXY_PASS" ]; then
          AUTH_LINES="${AUTH_LINES}    password = \"$SLUICE_PROXY_PASS\";
"
        fi
      fi

      cat > /tmp/redsocks.conf <<EOF
base {
    log_debug = off;
    log_info = on;
    daemon = on;
    redirector = iptables;
    pidfile = "/tmp/redsocks.pid";
}

redsocks {
    local_ip = 127.0.0.1;
    local_port = 12345;
    ip = $PROXY_IP;
    port = $SLUICE_PROXY_PORT;
    type = http-relay;
${AUTH_LINES}}

redsocks {
    local_ip = 127.0.0.1;
    local_port = 12346;
    ip = $PROXY_IP;
    port = $SLUICE_PROXY_PORT;
    type = http-connect;
${AUTH_LINES}}
EOF
    }

    start_redsocks() {
      rm -f /tmp/redsocks.pid
      redsocks -c /tmp/redsocks.conf
      sleep 1

      if [ -f /tmp/redsocks.pid ]; then
        return 0
      fi

      if pgrep -x redsocks >/dev/null 2>&1; then
        return 0
      fi

      gateway_error "redsocks failed to start"
      exit 1
    }

    create_domain_ipset() {
      ipset create sluice_domains hash:ip -exist
    }

    populate_domain_ipset() {
      OLD_IFS=$IFS
      IFS=,
      set -- $SLUICE_PROXY_DOMAINS
      IFS=$OLD_IFS

      for domain in "$@"; do
        domain=$(printf '%s' "$domain" | tr -d ' ')
        [ -n "$domain" ] || continue

        found_ip=""
        while IFS=' ' read -r ip _; do
          case "$ip" in
            *.*)
              ipset add sluice_domains "$ip" -exist
              gateway_log "resolved $domain -> $ip"
              found_ip=1
              ;;
          esac
        done <<EOF
$(getent hosts "$domain" 2>/dev/null || true)
EOF

        if [ -z "$found_ip" ]; then
          gateway_warn "no IPv4 addresses resolved for $domain"
        fi
      done
    }

    flush_domain_ipset() {
      ipset flush sluice_domains 2>/dev/null || true
    }

    delete_domain_ipset() {
      ipset destroy sluice_domains 2>/dev/null || true
    }

    ensure_nat_chain() {
      iptables -t nat -N SLUICE 2>/dev/null || true
      iptables -t nat -F SLUICE
    }

    append_bypass_rules() {
      iptables -t nat -A SLUICE -d 127.0.0.0/8 -j RETURN
      iptables -t nat -A SLUICE -d 10.0.0.0/8 -j RETURN
      iptables -t nat -A SLUICE -d 172.16.0.0/12 -j RETURN
      iptables -t nat -A SLUICE -d 192.168.0.0/16 -j RETURN
      iptables -t nat -A SLUICE -d 224.0.0.0/4 -j RETURN
      iptables -t nat -A SLUICE -d "$PROXY_IP" -j RETURN
    }

    append_selective_match_rules() {
      iptables -t nat -A SLUICE -m set --match-set sluice_domains dst -p tcp --dport 80 -j REDIRECT --to-ports 12345
      iptables -t nat -A SLUICE -m set --match-set sluice_domains dst -p tcp --dport 443 -j REDIRECT --to-ports 12346
    }

    append_full_match_rules() {
      iptables -t nat -A SLUICE -p tcp --dport 80 -j REDIRECT --to-ports 12345
      iptables -t nat -A SLUICE -p tcp --dport 443 -j REDIRECT --to-ports 12346
    }

    attach_output_chain() {
      detach_output_chain
      iptables -t nat -A OUTPUT -j SLUICE
    }

    detach_output_chain() {
      iptables -t nat -D OUTPUT -j SLUICE 2>/dev/null || true
    }

    setup_gateway_iptables() {
      ensure_nat_chain
      append_bypass_rules

      if [ -n "$SLUICE_PROXY_DOMAINS" ]; then
        create_domain_ipset
        populate_domain_ipset
        append_selective_match_rules
      else
        append_full_match_rules
      fi

      attach_output_chain
    }

    cleanup_gateway() {
      gateway_log "shutting down..."
      detach_output_chain
      iptables -t nat -F SLUICE 2>/dev/null || true
      iptables -t nat -X SLUICE 2>/dev/null || true
      if ipset list sluice_domains >/dev/null 2>&1; then
        flush_domain_ipset
        delete_domain_ipset
      fi
      kill "$(cat /tmp/redsocks.pid 2>/dev/null)" 2>/dev/null || true
      gateway_log "cleanup complete"
    }

    run_gateway_mode() {
      gateway_log "initializing gateway mode..."
      require_gateway_env
      build_redsocks_config
      start_redsocks
      setup_gateway_iptables
      gateway_log "proxy target -> $SLUICE_PROXY_HOST:$SLUICE_PROXY_PORT ($PROXY_IP)"
      gateway_log "redirect ports -> $SLUICE_REDIRECT_PORTS"
      if [ -n "$SLUICE_PROXY_DOMAINS" ]; then
        gateway_log "selective domains -> $SLUICE_PROXY_DOMAINS"
      fi
      gateway_log "gateway active — all HTTP/HTTPS traffic is being proxied"
      trap cleanup_gateway EXIT INT TERM
      tail -f /dev/null
    }

    run_gateway_mode
    ;;
  *)
    echo "error: unknown SLUICE_MODE: $SLUICE_MODE (use 'server', 'run', or 'gateway')" >&2
    exit 1
    ;;
esac
