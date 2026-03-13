# Go-Native Packet Marking: iptables → nftables Migration

## TODOs

- [x] 1. Create nftables.go — NftablesManager with google/nftables
  - Deliverables: `internal/gateway/nftables.go`, `go.mod`, `go.sum`
  - Preserve current iptables semantics: loopback RETURN, ESTABLISHED/RELATED RETURN, SSH-port bypass, NEW TCP MARK, NEW UDP MARK
  - Keep CGO disabled compatibility

- [x] 2. Wire NftablesManager into gateway.Run() and remove iptables.go
  - Update `internal/gateway/gateway.go` to call `NewNftablesManager(cfg.Fwmark).Setup()` after route setup
  - Add deferred cleanup mirroring route cleanup
  - Delete `internal/gateway/iptables.go`
  - Verify no `exec.Command("iptables")` or `IPTablesManager` references remain

- [x] 3. Write nftables_test.go with fake implementation
  - Add `internal/gateway/nftables_test.go`
  - Cover setup ordering, rollback, cleanup, no-table, SSH ports, default fwmark, idempotent reconcile

- [x] 4. Full build + test verification
  - Run `go test ./... -count=1`
  - Run `go vet ./...`
  - Run `CGO_ENABLED=0 go build ./cmd/sluice/`
  - Verify no `exec.Command.*iptables` remains

- [x] 5. Delete all GitHub releases/tags and re-release as v0.1.0
  - Delete v0.1.0-v0.1.3 releases and tags
  - Create new v0.1.0 tag and GitHub release

## Final Verification Wave

- [x] F1. Plan compliance audit
- [x] F2. Code quality review
- [x] F3. Real manual QA
- [x] F4. Scope fidelity check
