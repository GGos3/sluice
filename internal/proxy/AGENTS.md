# PROXY PACKAGE GUIDE

## OVERVIEW
`internal/proxy` owns the live request path: auth gate, whitelist enforcement, HTTP forwarding, CONNECT hijack, and access logging.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Auth behavior | `handler.go` | `authorized`, `WithAuth`, `Proxy-Authenticate` challenge |
| Standard HTTP proxy flow | `handler.go` | `handleHTTP`, transport round-trip, header filtering |
| CONNECT tunnel flow | `tunnel.go` | `handleConnect`, `bidirectionalCopy`, `closeWrite` |
| Access log reason/status changes | `handler.go`, `tunnel.go` | Keep reason codes stable and update README examples if they change |
| Header semantics | `handler.go` | `removeHopByHopHeaders`, `copyHeaders` |

## CONVENTIONS
- `ServeHTTP` is the only public request entry; branch by method, not by separate handlers registered elsewhere.
- Record access logs for both success and failure paths; denial/error returns should still emit stable `reason` codes.
- Preserve defensive cloning on outbound HTTP requests (`Clone`, `cloneURL`, `RequestURI = ""`).
- Keep transport configuration centralized in `NewHandler`; do not scatter dial or timeout tuning.
- Treat `context.Canceled` during body copy as non-fatal in access logging semantics.

## ANTI-PATTERNS
- Do not forward hop-by-hop headers upstream or back downstream.
- Do not skip whitelist checks for CONNECT; both HTTP and CONNECT paths enforce the same gate.
- Do not compare credentials with plain string equality; this package uses constant-time comparisons.
- Do not emit ad-hoc access log field names; consumers expect the current `proxy.*` keys.
- Do not collapse `bytesIn`/`bytesOut` meaning: HTTP uses request/response body counts, CONNECT uses directional tunnel copy totals.

## TESTING
- Start with `handler_test.go` for plain HTTP behavior and `tunnel_test.go` for CONNECT behavior.
- Existing tests cover auth, deny/allow paths, hop-by-hop header stripping, unreachable upstreams, and access logging. Extend those patterns instead of inventing new harnesses.

## NOTES
- `handleConnect` depends on response hijacking; changes here are easy to break with apparently harmless abstractions.
- `bidirectionalCopy` intentionally half-closes writable sides when possible; preserve that shutdown behavior when refactoring.
