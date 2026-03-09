#!/bin/sh
set -e

SLUICE_MODE="${SLUICE_MODE:-client}"

case "$SLUICE_MODE" in
  server)
    CONFIG_PATH="${SLUICE_CONFIG:-/etc/sluice/config.yaml}"
    if [ "$#" -eq 0 ] || { [ "$#" -eq 1 ] && [ "$1" = "sh" ]; }; then
      exec sluice -config "$CONFIG_PATH"
    fi
    exec sluice -config "$CONFIG_PATH" "$@"
    ;;
  client)
    PROXY_HOST="${SLUICE_PROXY_HOST:-}"
    PROXY_PORT="${SLUICE_PROXY_PORT:-8080}"
    PROXY_USER="${SLUICE_PROXY_USER:-}"
    PROXY_PASS="${SLUICE_PROXY_PASS:-}"
    NO_PROXY="${SLUICE_NO_PROXY:-localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16}"

    if [ -z "$PROXY_HOST" ]; then
      echo "error: SLUICE_PROXY_HOST is required in client mode" >&2
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

    echo "sluice: client mode activated"
    echo "sluice: proxy -> $PROXY_URL"
    echo "sluice: no_proxy -> $NO_PROXY"

    if [ "$#" -eq 0 ]; then
      exec sh
    fi

    exec "$@"
    ;;
  *)
    echo "error: unknown SLUICE_MODE: $SLUICE_MODE (use 'server' or 'client')" >&2
    exit 1
    ;;
esac
