# INTERNAL PACKAGE GUIDE

## OVERVIEW
`internal/` holds all private application logic; each subpackage owns one narrow concern and is wired together only from `cmd/proxy/main.go`.

## STRUCTURE
```text
internal/
├── acl/      # whitelist rule parsing and host matching
├── config/   # config schema, defaults, validation
├── logger/   # slog setup and access log formatting
└── proxy/    # HTTP proxying and CONNECT tunnels
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Domain matching bug | `acl/whitelist.go` | Normalize first, then exact/wildcard compare |
| Config shape change | `config/config.go` | Update normalize/default/validate together |
| Logging field change | `logger/logger.go` | Keep `proxy` group field names stable |
| Request flow/auth/header handling | `proxy/handler.go` | Main HTTP code path |
| CONNECT tunnel behavior | `proxy/tunnel.go` | Socket hijack + relay logic |

## CONVENTIONS
- Keep packages independent; `proxy` is the integration point that depends on `acl` and `logger`.
- When adding config fields, preserve the existing order: struct field, normalize, defaults, validation, tests.
- Prefer table-driven tests; current packages use co-located tests with focused helpers.
- Nil handling is deliberate in several APIs (`Whitelist`, logger access path); preserve those call contracts.

## ANTI-PATTERNS
- Do not create cross-package shortcuts that bypass `config.Load`, `acl.New`, or `logger.Setup`; `main.go` is the composition root.
- Do not split reusable helpers into a new shared package unless more than one internal package genuinely needs them; current layout is intentionally compact.
- Do not add broad package-level state; current packages are mostly constructor-driven and request-scoped.

## NOTES
- `internal/proxy/` is the complexity hotspot: most functions in the repo live there, plus both HTTP and CONNECT tests.
- `internal/config/` and `internal/acl/` encode user-facing validation rules; changes there usually require README/config example updates.
