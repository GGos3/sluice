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
sluice server user@blocked-host:220 --port 18080
```

The tunnel target is a positional argument in `user@host[:port]` format. SSH port defaults to 22 if omitted.

### 3) Start agent on blocked host (Linux)

```bash
sudo sluice agent --port 18080
```

### 4) Verify

```bash
curl https://github.com
```

## Daemon mode

Both `server` and `agent` support `--daemon` (or `-d`) to run as a background process:

```bash
sluice server user@blocked-host -d --port 18080
sluice server stop

sudo sluice agent -d --port 18080
sluice agent stop
```

PID files are written to `/var/run/sluice/` and logs to `/var/log/sluice/`.

Systemd unit files are also available under `configs/`:

```bash
sudo cp configs/sluice-server.service /etc/systemd/system/
sudo systemctl enable --now sluice-server
```

## Runtime domain management

Control the server's domain rules at runtime (requires the server to be running):

```bash
sluice server deny example.com      # block a domain
sluice server allow example.com     # allow a domain
sluice server remove example.com    # remove a runtime rule
sluice server rules                 # list active rules
```

Runtime rules are in-memory only; `config.yaml` remains the source of truth after restart.

## DNS path

- Agent intercepts DNS (`:53`) and relays upstream using DoH to server `http://127.0.0.1:{port}/dns-query` through tunnel/proxy path.
- Control-plane mark bypass is used to avoid DNS self-interception loops.

## CLI reference

```text
sluice server [start] [user@host[:port]] [flags]   Start the proxy server
sluice server stop                                  Stop the server daemon
sluice server deny <domain>                         Block a domain at runtime
sluice server allow <domain>                        Allow a domain at runtime
sluice server remove <domain>                       Remove a runtime rule
sluice server rules                                 List active rules
sluice agent [start] [flags]                        Start transparent proxy agent (Linux)
sluice agent stop                                   Stop the agent daemon
sluice run [flags] [-- cmd]                         Run a command with proxy env vars
sluice gateway [flags]                              Run as transparent proxy gateway (Linux)
sluice version                                      Show version
```

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
  user@blocked-host:220 --port 18080
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

For offline or air-gapped environments where the one-line install script cannot be used.

### Install

```bash
# Download binary (choose your platform)
curl -fsSL -o sluice https://github.com/ggos3/sluice/releases/download/v0.1.0/sluice-linux-amd64

# Verify checksum
curl -fsSL -o sluice-checksums.txt https://github.com/ggos3/sluice/releases/download/v0.1.0/sluice-checksums.txt
sha256sum -c --ignore-missing sluice-checksums.txt

# Install to /usr/local/bin
sudo install -m 0755 sluice /usr/local/bin/sluice

# Create /usr/bin symlink for sudo compatibility
# (sudo's secure_path typically includes /usr/bin but not /usr/local/bin)
sudo ln -sf /usr/local/bin/sluice /usr/bin/sluice

# Create config directory
sudo mkdir -p /etc/sluice
```

Available binaries: `sluice-linux-amd64`, `sluice-linux-arm64`, `sluice-darwin-amd64`, `sluice-darwin-arm64`

### Uninstall

```bash
sudo rm -f /usr/local/bin/sluice /usr/bin/sluice
```

## Project structure

- `cmd/sluice/` — CLI entrypoint
- `internal/proxy/` — HTTP/HTTPS proxy core
- `internal/tunnel/` — SSH reverse tunnel manager
- `internal/dns/` — DoH handler
- `internal/gateway/` — transparent agent core (TUN + nftables + policy routing)
- `internal/rules/` — client bypass rules
- `internal/acl/` — server whitelist + runtime deny/allow
- `internal/control/` — Unix socket IPC for runtime domain management
- `internal/daemon/` — process daemonization and PID file management

## License

MIT
