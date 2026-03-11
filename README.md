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
│                  │ encrypted│ + DoH endpoint   │          │              │
└──────────────────┘          └──────────────────┘          └──────────────┘
```

## Highlights

- SSH reverse tunnel orchestration (`server --tunnel user@host`)
- Transparent Linux agent (TUN/netstack)
- DNS-over-HTTPS over the same sluice port (`/dns-query`)
- Domain whitelist (server ACL) and optional proxy auth
- Client-side exclusion rules (`--no-proxy` with domains/CIDRs)
- Structured access logs

## Install (one-shot)

Install the prebuilt `sluice` release binary with a single command — no local Go toolchain required:

```bash
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash
```

After installation, use the same `sluice` binary for both modes:

```bash
sluice server --tunnel user@remote-host --ssh-port 220
sudo sluice agent --port 18080
```

The installer downloads the matching GitHub Release binary for your OS/architecture and verifies its checksum.

On Linux, the installer also creates `/usr/bin/sluice -> /usr/local/bin/sluice` so `sudo sluice ...` works even when sudo `secure_path` does not include `/usr/local/bin`.

Linux release binaries are built with `CGO_ENABLED=0` to maximize compatibility across older/newer distributions.

Optional installer flags:

```bash
# install a specific release tag
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash -s -- --version v0.1.0

# uninstall
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash -s -- uninstall
```

## Uninstall (Manual)

If you installed manually (or want to remove it without the install script):

```bash
sudo rm -f /usr/local/bin/sluice
if [ -L /usr/bin/sluice ] && [ "$(readlink /usr/bin/sluice)" = "/usr/local/bin/sluice" ]; then sudo rm -f /usr/bin/sluice; fi
```

## Install (Manual)

If the target host cannot reach GitHub directly, download the prebuilt release binary on another machine and transfer it manually.

Choose the binary that matches your CPU architecture:

- `x86_64` / `amd64` → `sluice-linux-amd64`
- `aarch64` / `arm64` → `sluice-linux-arm64`

You can check with:

```bash
uname -m
```

Compatibility note:

- `linux-amd64` and `linux-arm64` release binaries are built without cgo (`CGO_ENABLED=0`) for broad distro compatibility.
- For Linux agent mode, kernel capabilities/permissions are still required (`NET_ADMIN`, root).

```bash
# on a machine with internet access
# amd64
curl -fsSL https://github.com/ggos3/sluice/releases/latest/download/sluice-linux-amd64 -o sluice-linux-amd64

# arm64
curl -fsSL https://github.com/ggos3/sluice/releases/latest/download/sluice-linux-arm64 -o sluice-linux-arm64

# checksums (latest)
curl -fsSL https://github.com/ggos3/sluice/releases/latest/download/sluice-checksums.txt -o sluice-checksums.txt

# verify amd64 binary
grep " sluice-linux-amd64$" sluice-checksums.txt | sha256sum -c -

# verify arm64 binary
grep " sluice-linux-arm64$" sluice-checksums.txt | sha256sum -c -

# transfer the selected binary to the firewalled host
scp sluice-linux-amd64 user@firewalled-host:/tmp/sluice
# or
scp sluice-linux-arm64 user@firewalled-host:/tmp/sluice

# install on the firewalled host
ssh user@firewalled-host 'sudo install -m 0755 /tmp/sluice /usr/local/bin/sluice'
```

After installation, use the same `sluice` binary as the one-shot installer:

```bash
sluice server --tunnel user@remote-host --ssh-port 220
sudo sluice agent --port 18080
```

## Quick start

### 1) Start proxy server + reverse tunnel

```bash
sluice server --tunnel user@remote-host
```

If the remote SSH daemon uses a non-default port (for example `220`):

```bash
sluice server --tunnel user@remote-host --ssh-port 220
```

You can also change the sluice port:

```bash
sluice server --tunnel user@remote-host --ssh-port 220 --port 18080
```

### 2) Start agent on the blocked host (Linux, root)

```bash
sudo sluice agent --port 18080
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
