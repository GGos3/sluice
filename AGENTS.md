# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-09T07:58:53Z
**Commit:** 8873b42
**Branch:** main

## OVERVIEW
`sluice` is a single-binary Go forward proxy for firewalled environments. Core concerns: whitelist enforcement, SSH reverse tunneling, transparent intercept (agent), and structured access logging.

## STRUCTURE
```text
sluice/
├── cmd/sluice/         # binary entrypoint (server, agent, run commands)
├── internal/           # private application packages
│   ├── acl/            # server-side whitelist matching
│   ├── config/         # YAML load/default/validation path
│   ├── dns/            # DoH (DNS-over-HTTPS) server
│   ├── gateway/        # TUN/netstack intercept logic (agent)
│   ├── logger/         # slog setup + access log shaping
│   ├── proxy/          # HTTP forwarding and CONNECT tunneling
│   ├── rules/          # client-side proxy exclusion rules
│   └── tunnel/         # SSH reverse tunnel management
├── configs/            # sample runtime config
├── Dockerfile          # multi-stage build for server/agent
├── docker-compose.yml
└── Makefile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Start the server | `cmd/sluice/main.go` | Subcommand entrypoint for server/agent/run |
| Change request handling | `internal/proxy/handler.go` | Plain HTTP proxy path, header stripping, auth gating |
| Change CONNECT tunneling | `internal/proxy/tunnel.go` | HTTPS tunnel handshake and bidirectional copy |
| Change whitelist behavior | `internal/acl/whitelist.go` | Wildcard and host normalization rules |
| Change SSH tunneling | `internal/tunnel/tunnel.go` | Reverse tunnel management and reconnection |
| Change agent intercept | `internal/gateway/gateway.go` | TUN device and netstack integration |
| Change DNS handling | `internal/dns/server.go` | DoH server implementation |
| Change access logs | `internal/logger/logger.go` | `slog` setup and `proxy` log group shape |
| Change example runtime settings | `configs/config.yaml` | Sample domains and logging/auth defaults |
| Change dev workflow | `Makefile` | Canonical build/test/fmt/cross-build targets |
| Change CI/CD pipeline | `.github/workflows/docker-publish.yml` | Test → multi-platform build → GHCR push |

## CODE MAP
| Unit | Kind | Location | Role |
|------|------|----------|------|
| `main`, `run` | entrypoint | `cmd/sluice/main.go` | Parse flags, dispatch subcommands |
| `NewHandler`, `ServeHTTP` | core API | `internal/proxy/handler.go` | Dispatch HTTP vs CONNECT, enforce auth + ACL |
| `handleConnect`, `bidirectionalCopy` | tunnel path | `internal/proxy/tunnel.go` | Hijack connection and relay bytes |
| `Load`, `Config.Address` | config API | `internal/config/config.go` | Read YAML, normalize, default, validate |
| `New`, `Whitelist.IsAllowed` | ACL API | `internal/acl/whitelist.go` | Default-deny domain filtering |
| `Setup`, `LogAccess` | logging API | `internal/logger/logger.go` | Build `slog` logger and structured access logs |
| `ReverseTunnel` | tunnel API | `internal/tunnel/tunnel.go` | SSH reverse tunnel lifecycle |
| `NewGateway` | agent API | `internal/gateway/gateway.go` | TUN-based traffic capture |


## CONVENTIONS
- Keep application code under `internal/`; this repo does not use `pkg/`.
- Prefer Makefile targets over raw commands when documenting or validating work.
- Tests are package-local `*_test.go` files beside the code they exercise.
- Logging is expected to stay `slog`-based with `proxy` grouped access fields.
- Runtime config is YAML at `configs/config.yaml`; defaults live in code, not only in docs.

## ANTI-PATTERNS (THIS PROJECT)
- Do not treat `*.example.com` as matching the apex domain; whitelist both `example.com` and `*.example.com` when both are needed.
- Do not enable `whitelist.enabled` or `auth.enabled` without non-empty `domains` / `credentials`; config validation fails hard.
- Do not bypass `removeHopByHopHeaders`; proxy code is explicitly stripping hop-by-hop headers on request and response paths.
- Do not assume gateway mode is a safe default; it requires `--net=host`, `NET_ADMIN`, `NET_RAW`, and mutates host iptables.
- Do not assume shell helpers are unprivileged; install/uninstall flows require root and some SSH-tunnel paths require `ssh` plus `systemctl`.

## UNIQUE STYLES
- Small focused packages with explicit constructors and little indirection.
- Errors are surfaced with short wrapped messages (`load config: ...`, `server shutdown: ...`).
- Access log reasons are stable string codes such as `ok`, `domain_not_allowed`, `proxy_auth_required`, `target_dial_failed`.

## COMMANDS
```bash
make build
make run
make test
make test-coverage
make lint
make fmt
make cross-build
```

## NOTES
- Tooling in this environment may lack `go`; if verification fails due to missing toolchain, treat that as environment-specific rather than repo breakage.
- CI runs via `.github/workflows/docker-publish.yml`: tests on every push, multi-platform Docker build + GHCR push on `main` (`:dev` tag) and on release (semver tags + `:latest`).
- Docker mode names are operationally important: `server`, `run`, `gateway`.
