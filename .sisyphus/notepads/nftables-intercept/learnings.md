2026-03-11
- Preserved iptables rule semantics in nftables by creating an IPv4 `route` base chain on `output` with mangle priority and rule order: loopback return, ESTABLISHED|RELATED return, SSH-port return, NEW+TCP mark, NEW+UDP mark.
- `expr.MetaKeyOIFNAME` comparisons require IFNAMSIZ-sized byte payloads for reliable interface-name matching (`lo` padded in helper).
- Conntrack state matches were implemented with `expr.Ct` + `expr.Bitwise` masks against `CtStateBit*`, then `expr.Cmp` non-zero checks.
- Task 3 added a package-local fake nftables seam that records `AddTable`, `AddChain`, `AddRule`, `DelTable`, `Flush`, and list calls so setup ordering, cleanup, and idempotent reconcile stay kernel-free.
- The rollback test needs three fake connections because `Setup` reconciles before programming rules and opens a fresh connection for rollback when the setup flush fails.

2026-03-11 (Task 2)
- Wired NftablesManager into gateway.Run() after route setup and before dialer creation, following existing defer+closeErr pattern.
- Deferred cleanup uses LIFO order so packet marking cleanup runs before route cleanup.
- Error wrapping uses `setup packet marking: %w` and `cleanup packet marking: %w`.
- iptables.go does not exist in the codebase (likely already removed in Task 1 or never created).
