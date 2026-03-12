# sluice

> Korean documentation: **[README.ko.md](./README.ko.md)**

**Use internet from firewalled Linux hosts through a single SSH port.**

`sluice` is a single-binary Go proxy system:
- `server`: forward proxy + optional DoH endpoint
- `agent` (Linux): transparent interception with TUN/netstack

The blocked host sends traffic through an SSH reverse tunnel (`ssh -R`) to the proxy host.

## Core idea (how it works)

`sluice agent` is **nftables mark-based transparent interception**.

At startup, the agent:
1. Creates a TUN device and userspace TCP/IP stack.
2. Installs nftables output-chain rules to mark outbound traffic.
3. Installs policy routing (`ip rule` + custom route table) for packets with that fwmark.
4. Sends marked traffic into TUN, where netstack serves:
   - DNS on `:53` (relayed as DoH)
   - HTTP on `:80`
   - HTTPS on `:443`
5. Forwards upstream via proxy endpoint `127.0.0.1:{port}` (the SSH reverse-tunnel bind).

To avoid loops, control-plane traffic uses a separate `control-fwmark` bypass path.

## Architecture

```text
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│ Blocked Host     │──────────│ Proxy Host       │──────────│ Internet     │
│ (agent, Linux)   │  SSH -R  │ (sluice server)  │  HTTP/S  │ targets      │
│ nft mark + TUN   │ encrypted│ + /dns-query     │          │              │
└──────────────────┘          └──────────────────┘          └──────────────┘
```

## Requirements

- Agent mode: Linux + root privileges
- Kernel networking privileges/capabilities (`NET_ADMIN`; containerized runs may also need `NET_RAW`)
- TUN device available (`/dev/net/tun`)
- nftables support (`nf_tables`)
- SSH access from proxy host to blocked host

## Quick start

### 1) Install

```bash
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash
```

### 2) Start proxy server + reverse tunnel orchestration

```bash
sluice server --tunnel user@blocked-host --ssh-port 220 --port 18080
```

### 3) Start agent on blocked host (Linux)

```bash
sudo sluice agent --port 18080
```

### 4) Verify

```bash
curl https://github.com
```

## DNS path

- Agent intercepts DNS (`:53`) and relays upstream using DoH to server `http://127.0.0.1:{port}/dns-query` through tunnel/proxy path.
- Control-plane mark bypass is used to avoid DNS self-interception loops.

## Modes

- `server` — forward proxy server (and optional auto SSH reverse tunnel)
- `agent` — Linux transparent interception mode
- `gateway` — Linux gateway mode
- `run` — command-scoped proxy environment

`run` examples:

```bash
sluice run -- curl https://example.com
sluice run --port 18080 -- curl https://example.com
sluice run --proxy-host 127.0.0.1 --port 18080 -- curl https://example.com
```

## Configuration

- Server config: `configs/config.yaml`
- Agent exclusions: `--no-proxy "*.internal.example,10.0.0.0/8"`
- Agent mark controls:
  - `--fwmark` for intercepted data path
  - `--control-fwmark` for control-plane bypass traffic

## Docker

### Server image

```bash
docker run -d --name sluice-server \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/ggos3/sluice-server \
  --tunnel user@blocked-host --ssh-port 220 --port 18080
```

### Agent image (Linux host)

```bash
docker run -d --name sluice-agent \
  --net=host \
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --device /dev/net/tun:/dev/net/tun \
  ghcr.io/ggos3/sluice-agent \
  --port 18080
```

## E2E test

Run Docker-based firewall + tunnel end-to-end validation:

```bash
make e2e
```

This verifies:
- firewall blocks direct agent -> server access
- reverse tunnel is up
- intercepted HTTP/HTTPS succeed through sluice

## Manual install / uninstall

If one-shot install is not possible (offline transfer, air-gapped workflow), see manual binary install steps in release assets:
- Download `sluice-linux-amd64` or `sluice-linux-arm64`
- Verify `sluice-checksums.txt`
- Install to `/usr/local/bin/sluice`

Manual uninstall:

```bash
sudo rm -f /usr/local/bin/sluice
if [ -L /usr/bin/sluice ] && [ "$(readlink /usr/bin/sluice)" = "/usr/local/bin/sluice" ]; then sudo rm -f /usr/bin/sluice; fi
```

## Project structure

- `cmd/sluice/` — CLI entrypoint
- `internal/proxy/` — HTTP/HTTPS proxy core
- `internal/tunnel/` — SSH reverse tunnel manager
- `internal/dns/` — DoH handler
- `internal/gateway/` — transparent agent core (TUN + nftables + policy routing)
- `internal/rules/` — client bypass rules
- `internal/acl/` — server whitelist

## License

MIT
