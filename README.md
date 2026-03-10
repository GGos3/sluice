# sluice

> Korean documentation: **[README.ko.md](./README.ko.md)**

**Use internet from firewalled servers through a single SSH port.**

`sluice` runs a forward proxy on the server side and a Linux transparent agent on the blocked host side.
The agent intercepts HTTP/HTTPS/DNS traffic and forwards it through an SSH reverse tunnel (`-R`) to the proxy.

## Architecture

```text
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│ Blocked Host     │──────────│ Proxy Host       │──────────│ Internet     │
│ (agent)          │  SSH -R  │ (sluice server)  │  HTTP/S  │ targets      │
│                  │  encrypted│ + DoH endpoint   │          │              │
└──────────────────┘          └──────────────────┘          └──────────────┘
```

## Highlights

- SSH reverse tunnel orchestration (`server --tunnel user@host`)
- Transparent Linux agent (TUN/netstack)
- DNS-over-HTTPS over the same sluice port (`/dns-query`)
- Domain whitelist (server ACL) and optional proxy auth
- Client-side exclusion rules (`--no-proxy` with domains/CIDRs)
- Structured access logs

## Quick start

### 1) Start proxy server + reverse tunnel

```bash
./sluice server --tunnel user@remote-host
```

If the remote SSH daemon uses a non-default port (for example `220`):

```bash
./sluice server --tunnel user@remote-host --ssh-port 220
```

You can also change the sluice port:

```bash
./sluice server --tunnel user@remote-host --ssh-port 220 --port 18080
```

### 2) Start agent on the blocked host (Linux, root)

```bash
sudo ./sluice agent --port 18080
```

### 3) Verify

```bash
curl https://github.com
```

## Manual SSH reverse tunnel (optional)

Instead of auto-tunnel mode, you can create the reverse tunnel yourself:

```bash
# Proxy host
./sluice server --port 18080

# From blocked host (SSH port 22)
ssh -R 18080:localhost:18080 user@proxy-host -N

# From blocked host (SSH port 220)
ssh -p 220 -R 18080:localhost:18080 user@proxy-host -N

# Agent
sudo ./sluice agent --port 18080
```

## Docker

### Server image

```bash
docker run -d --name sluice-server \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/ggos3/sluice-server \
  --tunnel user@remote-host --ssh-port 220
```

### Agent image (Linux host)

```bash
docker run -d --name sluice-agent \
  --net=host \
  --cap-add=NET_ADMIN \
  ghcr.io/ggos3/sluice-agent
```

## Config and modes

- Server config: `configs/config.yaml`
- Agent exclusions: `--no-proxy "*.internal.example,10.0.0.0/8"`
- Modes:
  - `server`
  - `gateway` (Linux only)
  - `agent` (Linux only)
  - `run` (command-scoped proxy env)

`run` mode examples:

```bash
./sluice run -- curl https://example.com
./sluice run --port 18080 -- curl https://example.com
./sluice run --proxy-host 127.0.0.1 --port 18080 -- curl https://example.com
```

## Project structure

- `cmd/sluice/` — CLI entrypoint
- `internal/proxy/` — HTTP/HTTPS proxy core
- `internal/tunnel/` — SSH reverse tunnel manager
- `internal/dns/` — DoH handler
- `internal/gateway/` — transparent agent core
- `internal/rules/` — client bypass rules
- `internal/acl/` — server whitelist

## License

MIT
