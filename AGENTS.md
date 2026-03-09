# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-09T07:58:53Z
**Commit:** 8873b42
**Branch:** main

## OVERVIEW
`sluice` is a single-binary Go forward proxy for firewalled environments. Core concerns: whitelist enforcement, optional proxy auth, structured access logging, Docker server/run/gateway operation.

## STRUCTURE
```text
sluice/
├── cmd/proxy/          # binary entrypoint, flag parsing, graceful shutdown
├── internal/           # private application packages
│   ├── acl/            # whitelist matching and host normalization
│   ├── config/         # YAML load/default/validation path
│   ├── logger/         # slog setup + access log shaping
│   └── proxy/          # HTTP forwarding and CONNECT tunneling
├── configs/            # sample runtime config
├── scripts/            # host-side client setup helpers
├── docker-entrypoint.sh
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Start the server | `cmd/proxy/main.go` | Builds `bin/sluice`, wires config/logger/auth/handler |
| Change request handling | `internal/proxy/handler.go` | Plain HTTP proxy path, header stripping, auth gating |
| Change CONNECT tunneling | `internal/proxy/tunnel.go` | HTTPS tunnel handshake and bidirectional copy |
| Change whitelist behavior | `internal/acl/whitelist.go` | Wildcard and host normalization rules |
| Change config schema/validation | `internal/config/config.go` | Defaults first, strict validation after normalize |
| Change access logs | `internal/logger/logger.go` | `slog` setup and `proxy` log group shape |
| Change example runtime settings | `configs/config.yaml` | Sample domains and logging/auth defaults |
| Change dev workflow | `Makefile` | Canonical build/test/fmt/cross-build targets |
| Change CI/CD pipeline | `.github/workflows/docker-publish.yml` | Test → multi-platform build → GHCR push |
| Change run/gateway shell flow | `scripts/setup-client.sh`, `docker-entrypoint.sh` | Root/system tool requirements live here |

## CODE MAP
| Unit | Kind | Location | Role |
|------|------|----------|------|
| `main`, `run` | entrypoint | `cmd/proxy/main.go` | Parse flags, boot server, graceful shutdown |
| `NewHandler`, `ServeHTTP` | core API | `internal/proxy/handler.go` | Dispatch HTTP vs CONNECT, enforce auth + ACL |
| `handleConnect`, `bidirectionalCopy` | tunnel path | `internal/proxy/tunnel.go` | Hijack connection and relay bytes |
| `Load`, `Config.Address` | config API | `internal/config/config.go` | Read YAML, normalize, default, validate |
| `New`, `Whitelist.IsAllowed` | ACL API | `internal/acl/whitelist.go` | Default-deny domain filtering |
| `Setup`, `LogAccess` | logging API | `internal/logger/logger.go` | Build `slog` logger and structured access logs |

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
- `docker-entrypoint.sh` rejects missing `SLUICE_PROXY_HOST` in run/gateway mode and downgrades unsupported `SLUICE_REDIRECT_PORTS=all`.
