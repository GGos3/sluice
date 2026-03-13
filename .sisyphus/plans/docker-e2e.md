# Docker E2E Tests with Firewall Simulation

## TL;DR

> **Quick Summary**: Add Docker E2E tests that validate sluice's transparent interception in a realistic firewalled environment — 3 containers (server, agent, firewall) on 2 isolated networks, SSH reverse tunnel through firewall, happy-path + negative-case verification.
>
> **Deliverables**:
> - `e2e/docker-compose.yml` — 3-service topology with 2 isolated Docker networks
> - `e2e/Dockerfile.agent` — Agent image with openssh-server for tunnel reception
> - `e2e/entrypoint-*.sh` — Container startup scripts (agent, firewall, server)
> - `e2e/run.sh` — Full E2E test runner (setup, readiness, tests, cleanup)
> - `Makefile` `e2e` target
> - GitHub Actions E2E job in CI workflow
>
> **Estimated Effort**: Medium
> **Parallel Execution**: YES — 3 waves
> **Critical Path**: Task 1 (topology) → Task 5 (test runner) → Task 6 (CI)

---

## Context

### Original Request
Add Docker E2E tests with a firewall container between server and agent. Server→Agent: SSH only allowed. Agent→Server: all blocked. Agent uses SSH reverse tunnel to reach proxy. Verify transparent interception works end-to-end.

### Interview Summary
**Key Discussions**:
- Test runner: Shell script + docker-compose (agreed — matches project's compact style, no new Go dependencies)
- Test scope: Happy path (curl succeeds through tunnel) + negative cases (firewall blocks direct access)
- CI: Add E2E job to GitHub Actions workflow with environment capability probe

**Research Findings**:
- `tunnel.go:254-263`: Server runs external `ssh -R port:localhost:port -N user@host` — server initiates SSH TO agent, agent must run sshd
- `agent_linux.go:33`: Agent hardcodes `ProxyHost = "127.0.0.1"` — reverse tunnel maps localhost:18080 → server's proxy
- `gateway.go:75-86`: nftables marks TCP/UDP → TUN → netstack → proxy dialer. DNS on :53, HTTP on :80, HTTPS on :443
- Dockerfile: Server image has `openssh-client`, agent image has only `ca-certificates` — agent needs sshd added
- seed-labs OSS pattern: Two Docker networks + firewall container with iptables FORWARD rules is battle-tested
- `tunnel.go:254-263` commandArgs: No identity file or StrictHostKeyChecking flags — uses system SSH defaults, needs SSH config file

### Metis Review
**Identified Gaps** (addressed):
- CI environment probe: Added capability check step before full E2E — `/dev/net/tun` + `NET_ADMIN` validation
- Bootstrap race conditions: Added explicit readiness gates (sshd, tunnel, TUN) with bounded retries
- Scope creep lock: No product code changes — E2E is harness-only around current behavior
- Controlled vs real internet: Using `example.com` for HTTP and a normal-chain HTTPS target selected by the user (`google.com`) for TLS verification stability in this environment.
- Firewall mechanism: Locked to iptables in firewall container (well-proven in Docker, no nftables kernel dependency for the firewall itself)
- SSH auth model: Disposable ed25519 keypair generated per test run, lives in `e2e/.ssh/` (gitignored), no changes to production SSH code
- Artifact collection: Container logs saved on failure for CI debugging

---

## Work Objectives

### Core Objective
Prove sluice's full transparent interception chain works end-to-end in a realistic firewalled Docker environment: agent traffic → TUN → nftables → netstack → SSH reverse tunnel → server proxy → internet.

### Concrete Deliverables
- `e2e/docker-compose.yml` — 3-service topology
- `e2e/Dockerfile.agent` — Agent image with sshd
- `e2e/entrypoint-agent.sh` — Agent startup (sshd + sluice agent)
- `e2e/entrypoint-firewall.sh` — Firewall iptables rules
- `e2e/entrypoint-server.sh` — Server routing + sluice server with tunnel
- `e2e/run.sh` — Test orchestration script
- `Makefile` update with `e2e` / `e2e-clean` targets
- `.github/workflows/docker-publish.yml` update with E2E job

### Definition of Done
- [x] `make e2e` exits 0 on a Linux machine with Docker
- [x] Agent curls `http://example.com` successfully through tunnel
- [x] Agent curls `https://google.com` successfully through tunnel
- [x] Agent cannot directly reach server IP (firewall blocks)
- [x] CI E2E job passes on GitHub Actions

### Must Have
- 3 containers: server, agent, firewall
- 2 isolated Docker networks (net-server, net-agent)
- Firewall: blocks agent→server, allows server→agent SSH only
- SSH reverse tunnel from server to agent
- Happy path: curl from agent works through tunnel
- Negative case: direct agent→server access fails
- `make e2e` target
- CI integration with capability probe

### Must NOT Have (Guardrails)
- Do NOT modify any production code (no changes to `internal/`, `cmd/`, existing `Dockerfile`, `docker-compose.yml`)
- Do NOT add Go dependencies (no testcontainers-go or Docker SDK)
- Do NOT commit SSH keys to the repository
- Do NOT add auth/whitelist/reconnect test scenarios (future scope)
- Do NOT add DoH-specific E2E tests (future scope)
- Do NOT replace the tunnel mechanism (`ssh` external binary) in this task
- Do NOT add platform support beyond Linux CI
- Do NOT modify the existing `docker-compose.yml` — the E2E compose file is separate
- `.dockerignore` changes are allowed only when required to exclude generated `e2e/.ssh/` material from Docker build context

---

## Verification Strategy (MANDATORY)

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: NO (this is a new E2E harness)
- **Automated tests**: Shell script E2E tests (not Go unit tests)
- **Framework**: Shell script + docker-compose + curl assertions

### QA Policy
Every task MUST include agent-executed QA scenarios.
Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **Docker infrastructure**: Use Bash — docker compose commands, docker exec, curl assertions
- **Shell scripts**: Use Bash — shellcheck, execution verification
- **CI workflow**: Use Bash — YAML validation, workflow syntax check

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start Immediately — infrastructure scaffolding, 4 parallel):
├── Task 1: Docker Compose topology (docker-compose.yml) [quick]
├── Task 2: Agent E2E image (Dockerfile.agent + entrypoint-agent.sh) [quick]
├── Task 3: Firewall + Server scripts (entrypoint-firewall.sh + entrypoint-server.sh) [quick]
└── Task 4: Makefile + .gitignore updates [quick]

Wave 2 (After Wave 1 — test orchestration):
└── Task 5: E2E test runner (run.sh) [deep]

Wave 3 (After Wave 2 — CI integration):
└── Task 6: GitHub Actions E2E job [quick]

Wave FINAL (After ALL tasks — 4 parallel reviews):
├── F1: Plan compliance audit (oracle)
├── F2: Code quality review (unspecified-high)
├── F3: Real manual QA — run full E2E (unspecified-high)
└── F4: Scope fidelity check (deep)

Critical Path: Task 1 → Task 5 → Task 6 → F1-F4
Parallel Speedup: Wave 1 runs 4 tasks simultaneously
Max Concurrent: 4 (Wave 1)
```

### Dependency Matrix

| Task | Depends On | Blocks | Wave |
|------|-----------|--------|------|
| 1 | — | 5 | 1 |
| 2 | — | 5 | 1 |
| 3 | — | 5 | 1 |
| 4 | — | 5 | 1 |
| 5 | 1, 2, 3, 4 | 6 | 2 |
| 6 | 5 | F1-F4 | 3 |

### Agent Dispatch Summary

- **Wave 1**: **4** — T1 → `quick`, T2 → `quick`, T3 → `quick`, T4 → `quick`
- **Wave 2**: **1** — T5 → `deep`
- **Wave 3**: **1** — T6 → `quick`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

### Fixed Constants (ALL tasks must use these exact values)

| Constant | Value | Usage |
|----------|-------|-------|
| net-server subnet | `172.23.0.0/24` | Docker network IPAM |
| net-agent subnet | `172.24.0.0/24` | Docker network IPAM |
| server IP | `172.23.0.10` | Server container on net-server |
| firewall IP (server-side) | `172.23.0.11` | Firewall on net-server |
| agent IP | `172.24.0.10` | Agent container on net-agent |
| firewall IP (agent-side) | `172.24.0.11` | Firewall on net-agent |
| sluice proxy port | `18080` | Server proxy + tunnel bind |
| SSH port | `22` | Agent sshd |
| SSH key type | `ed25519` | Generated per test run |
| SSH key path | `e2e/.ssh/` | Gitignored, runtime only |

---

## TODOs

- [x] 1. E2E Docker Compose Topology

  **What to do**:
  - Create `e2e/docker-compose.yml` defining the 3-container firewalled topology
  - Define 2 isolated Docker networks with fixed subnets:
    - `net-server`: `172.20.0.0/24` (server + firewall)
    - `net-agent`: `172.21.0.0/24` (agent + firewall)
  - Define 3 services:
    - `server`: builds from repo root with `target: server`, on `net-server` at `172.20.0.10`, `cap_add: NET_ADMIN` (for `ip route add`), SSH private key + config mounted from `e2e/.ssh/`, override entrypoint to use `e2e/entrypoint-server.sh`
    - `agent`: builds from `e2e/Dockerfile.agent`, on `net-agent` at `172.21.0.10`, `cap_add: [NET_ADMIN, NET_RAW]`, `devices: ["/dev/net/tun:/dev/net/tun"]`, SSH authorized_keys mounted from `e2e/.ssh/`, uses `e2e/entrypoint-agent.sh` as entrypoint
    - `firewall`: Alpine 3.21 image, on BOTH networks (`net-server` at `172.20.0.11`, `net-agent` at `172.21.0.11`), `cap_add: NET_ADMIN`, uses `e2e/entrypoint-firewall.sh` as entrypoint
  - Volume mounts for SSH keys: bind mount `./e2e/.ssh/` into containers at appropriate paths
  - Server depends_on firewall and agent (start order)

  **Must NOT do**:
  - Do NOT modify the root `docker-compose.yml`
  - Do NOT use `network_mode: host` for any service
  - Do NOT hardcode SSH keys in the compose file
  - Do NOT add services beyond server, agent, firewall

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Single YAML file creation with well-defined structure
  - **Skills**: []
    - No special skills needed — standard docker-compose YAML

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 2, 3, 4)
  - **Blocks**: Task 5 (test runner needs topology defined)
  - **Blocked By**: None (can start immediately)

  **References** (CRITICAL):

  **Pattern References**:
  - `docker-compose.yml:1-22` — Existing compose file showing project's build context pattern (`context: .`, `target: server/agent`), service naming, and capability usage
  - `Dockerfile:18-38` — Server target: Alpine 3.21, openssh-client installed, entrypoint `sluice server`
  - `Dockerfile:40-57` — Agent target: Alpine 3.21, minimal, entrypoint `sluice agent`

  **API/Type References**:
  - Fixed Constants table in Execution Strategy above — exact IPs, subnets, ports

  **External References**:
  - `seed-labs/Firewall_Evasion/Labsetup/docker-compose.yml` — Multi-network Docker topology with firewall container pattern using iptables FORWARD rules

  **WHY Each Reference Matters**:
  - Root `docker-compose.yml` shows the project's preferred compose style and build target naming
  - `Dockerfile` targets define what packages are pre-installed in each image (server has openssh-client, agent has only ca-certificates)
  - seed-labs pattern proves the multi-network + firewall container approach works reliably in Docker

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Docker Compose YAML is valid and defines correct topology
    Tool: Bash
    Preconditions: Docker and docker compose available
    Steps:
      1. Run: docker compose -f e2e/docker-compose.yml config --quiet
      2. Parse output: verify 3 services defined (server, agent, firewall)
      3. Parse output: verify 2 networks defined (net-server, net-agent)
    Expected Result: Exit code 0, no validation errors
    Failure Indicators: Non-zero exit code, YAML syntax errors, missing services/networks
    Evidence: .sisyphus/evidence/task-1-compose-valid.txt

  Scenario: Network isolation is correct (services on expected networks only)
    Tool: Bash
    Preconditions: docker compose config output available
    Steps:
      1. Run: docker compose -f e2e/docker-compose.yml config
      2. Verify server is ONLY on net-server (not net-agent)
      3. Verify agent is ONLY on net-agent (not net-server)
      4. Verify firewall is on BOTH net-server AND net-agent
    Expected Result: Network assignments match plan constants
    Evidence: .sisyphus/evidence/task-1-network-isolation.txt
  ```

  **Commit**: YES (groups with Task 2)
  - Message: `test(e2e): add docker-compose topology and agent Dockerfile`
  - Files: `e2e/docker-compose.yml`, `e2e/Dockerfile.agent`
  - Pre-commit: `docker compose -f e2e/docker-compose.yml config --quiet`

- [x] 2. Agent E2E Image with SSH Server

  **What to do**:
  - Create `e2e/Dockerfile.agent` that extends the main Dockerfile's agent build:
    - Use multi-stage: `FROM golang:1.24-alpine AS builder` with same build steps as root `Dockerfile:1-16`
    - Final stage: `FROM alpine:3.21`, install `ca-certificates openssh-server`, copy sluice binary
    - Do NOT use `FROM ... AS agent` from root Dockerfile — the E2E Dockerfile is standalone to avoid coupling
  - Create `e2e/entrypoint-agent.sh`:
    - Generate SSH host keys: `ssh-keygen -A`
    - Configure sshd: `PermitRootLogin yes`, `PubkeyAuthentication yes`, `PasswordAuthentication no`
    - Create `/root/.ssh/` directory, copy authorized_keys from mounted volume
    - Set correct permissions: `chmod 700 /root/.ssh`, `chmod 600 /root/.ssh/authorized_keys`
    - Add route to server network: `ip route add 172.20.0.0/24 via 172.21.0.11`
    - Start sshd in background: `/usr/sbin/sshd`
    - Exec sluice agent: `exec sluice agent --port 18080`
  - Make `entrypoint-agent.sh` executable (`chmod +x`)

  **Must NOT do**:
  - Do NOT modify the root `Dockerfile`
  - Do NOT install unnecessary packages
  - Do NOT hardcode SSH keys in the Dockerfile
  - Do NOT use `EXPOSE` for SSH (internal only)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Dockerfile + shell script, well-defined patterns from existing Dockerfile
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 3, 4)
  - **Blocks**: Task 5
  - **Blocked By**: None

  **References** (CRITICAL):

  **Pattern References**:
  - `Dockerfile:1-16` — Builder stage pattern: `golang:1.24-alpine`, workdir, go mod download, CGO_ENABLED=0 build
  - `Dockerfile:40-57` — Agent target pattern: Alpine 3.21 base, copy binary, entrypoint
  - `cmd/sluice/agent_linux.go:15-46` — Agent CLI: `NewConfigFromFlags(fs)`, `--port` flag defaults to 18080, hardcodes `ProxyHost = "127.0.0.1"`, calls `gateway.Run(ctx, cfg, log)`

  **API/Type References**:
  - `internal/gateway/config.go:36-51` — Config struct: ProxyHost, ProxyPort, TUNName, RouteTable, RulePriority, Fwmark — all have defaults, only `--port` needs explicit flag

  **WHY Each Reference Matters**:
  - Root Dockerfile's builder stage must be replicated exactly for the E2E agent image (same Go version, same build flags)
  - `agent_linux.go` shows the only flag needed is `--port 18080`, and ProxyHost is always 127.0.0.1 (reverse tunnel endpoint)

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Agent E2E image builds successfully
    Tool: Bash
    Preconditions: Docker available, repo root is build context
    Steps:
      1. Run: docker build -f e2e/Dockerfile.agent -t sluice-e2e-agent-test .
      2. Verify exit code 0
      3. Run: docker run --rm sluice-e2e-agent-test sluice version
      4. Verify output contains version string
    Expected Result: Image builds without errors, sluice binary is functional
    Failure Indicators: Build failure, missing binary, wrong architecture
    Evidence: .sisyphus/evidence/task-2-image-build.txt

  Scenario: Agent image includes openssh-server
    Tool: Bash
    Preconditions: Agent E2E image built
    Steps:
      1. Run: docker run --rm sluice-e2e-agent-test which sshd
      2. Verify output shows /usr/sbin/sshd
      3. Run: docker run --rm sluice-e2e-agent-test sshd -t 2>&1 || true
    Expected Result: sshd binary exists at /usr/sbin/sshd
    Evidence: .sisyphus/evidence/task-2-sshd-exists.txt

  Scenario: Entrypoint script is valid shell
    Tool: Bash
    Preconditions: entrypoint-agent.sh exists
    Steps:
      1. Run: shellcheck e2e/entrypoint-agent.sh || true (note warnings)
      2. Run: head -1 e2e/entrypoint-agent.sh (verify shebang)
      3. Run: test -x e2e/entrypoint-agent.sh (verify executable)
    Expected Result: Valid shell script with proper shebang and executable bit
    Evidence: .sisyphus/evidence/task-2-entrypoint-valid.txt
  ```

  **Commit**: YES (groups with Task 1)
  - Message: `test(e2e): add docker-compose topology and agent Dockerfile`
  - Files: `e2e/Dockerfile.agent`, `e2e/entrypoint-agent.sh`

- [x] 3. Firewall and Server Entrypoint Scripts

  **What to do**:
  - Create `e2e/entrypoint-firewall.sh`:
    - Enable IP forwarding: `sysctl -w net.ipv4.ip_forward=1`
    - Set default FORWARD policy to DROP: `iptables -P FORWARD DROP`
    - Allow established/related connections: `iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT`
    - Allow SSH from server network to agent network: `iptables -A FORWARD -s 172.20.0.0/24 -d 172.21.0.0/24 -p tcp --dport 22 -j ACCEPT`
    - All other forwarded traffic is dropped by policy (agent→server blocked)
    - Keep container alive: `exec tail -f /dev/null`
  - Create `e2e/entrypoint-server.sh`:
    - Add route to agent network via firewall: `ip route add 172.21.0.0/24 via 172.20.0.11`
    - Create SSH config for non-interactive operation: write `/root/.ssh/config` with `StrictHostKeyChecking no`, `UserKnownHostsFile /dev/null`, `LogLevel ERROR`
    - Start sluice server with tunnel: `exec sluice server --tunnel root@172.21.0.10 --ssh-port 22 --port 18080`
  - Make both scripts executable (`chmod +x`)

  **Must NOT do**:
  - Do NOT use nftables for the firewall container (iptables is simpler and universally available in Alpine)
  - Do NOT add NAT/MASQUERADE rules (direct routing through firewall is sufficient for this topology)
  - Do NOT allow any traffic from agent network to server network except ESTABLISHED/RELATED responses
  - Do NOT modify production tunnel code

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Two small shell scripts with well-defined iptables and routing commands
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 2, 4)
  - **Blocks**: Task 5
  - **Blocked By**: None

  **References** (CRITICAL):

  **Pattern References**:
  - `internal/tunnel/tunnel.go:254-263` — SSH commandArgs: `-R 18080:localhost:18080 -o ServerAliveInterval=60 -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes -p 22 -N root@172.21.0.10`. The server's SSH config must not conflict with these options.
  - `Dockerfile:29-30` — Server image has `openssh-client` pre-installed (`apk add --no-cache bind-tools ca-certificates openssh-client`)

  **External References**:
  - `seed-labs/Firewall_Evasion/Labsetup/docker-compose.yml:122` — Proven iptables pattern: `ESTABLISHED,RELATED ACCEPT → port-specific ACCEPT → default DROP`

  **WHY Each Reference Matters**:
  - `tunnel.go` commandArgs shows exact SSH flags the server will use — the SSH config in entrypoint-server.sh must add StrictHostKeyChecking=no and identity file path without conflicting
  - seed-labs pattern is the proven Docker firewall isolation technique with iptables FORWARD chain

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Firewall script has correct iptables rule structure
    Tool: Bash
    Preconditions: entrypoint-firewall.sh exists
    Steps:
      1. Run: shellcheck e2e/entrypoint-firewall.sh || true
      2. Run: grep -c "iptables" e2e/entrypoint-firewall.sh
      3. Verify script contains: FORWARD DROP policy, ESTABLISHED,RELATED ACCEPT, SSH port 22 ACCEPT
      4. Verify script does NOT contain rules allowing agent→server non-SSH traffic
    Expected Result: Valid shell with exactly the required iptables rules
    Failure Indicators: Missing rules, overly permissive rules, syntax errors
    Evidence: .sisyphus/evidence/task-3-firewall-rules.txt

  Scenario: Server script routes through firewall and starts tunnel
    Tool: Bash
    Preconditions: entrypoint-server.sh exists
    Steps:
      1. Run: shellcheck e2e/entrypoint-server.sh || true
      2. Verify script adds route: ip route add 172.21.0.0/24 via 172.20.0.11
      3. Verify script starts sluice with: --tunnel root@172.21.0.10 --ssh-port 22 --port 18080
      4. Verify SSH config sets StrictHostKeyChecking no
    Expected Result: Valid shell with correct routing and tunnel command
    Evidence: .sisyphus/evidence/task-3-server-script.txt
  ```

  **Commit**: YES
  - Message: `test(e2e): add container entrypoint scripts`
  - Files: `e2e/entrypoint-firewall.sh`, `e2e/entrypoint-server.sh`

- [x] 4. Makefile and .gitignore Updates

  **What to do**:
  - Add to `Makefile` (after the `clean` target, before `install`):
    ```makefile
    # E2E tests (requires Docker + Linux)
    .PHONY: e2e
    e2e:
    	./e2e/run.sh

    .PHONY: e2e-clean
    e2e-clean:
    	./e2e/run.sh --cleanup
    ```
  - Add to `.gitignore` (create if doesn't exist, or append):
    ```
    # E2E test artifacts
    e2e/.ssh/
    ```
  - Update `Makefile` help target to include e2e entries

  **Must NOT do**:
  - Do NOT remove or modify existing Makefile targets
  - Do NOT add complex logic to the Makefile — it just delegates to run.sh

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Trivial additions to existing files
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 2, 3)
  - **Blocks**: Task 5
  - **Blocked By**: None

  **References** (CRITICAL):

  **Pattern References**:
  - `Makefile:12-100` — Existing Makefile style: `.PHONY` declarations, tab indentation, help target at bottom with `@echo` lines. Follow exact same conventions.

  **WHY Each Reference Matters**:
  - Makefile must match existing style exactly — `.PHONY` before target, consistent formatting, help text added

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Makefile e2e target exists and is syntactically correct
    Tool: Bash
    Preconditions: Makefile exists
    Steps:
      1. Run: make -n e2e (dry run)
      2. Verify output shows: ./e2e/run.sh
      3. Run: make -n e2e-clean
      4. Verify output shows: ./e2e/run.sh --cleanup
      5. Run: make help
      6. Verify output includes e2e entries
    Expected Result: Both targets defined, dry-run shows correct commands
    Failure Indicators: make: *** No rule to make target 'e2e'
    Evidence: .sisyphus/evidence/task-4-makefile-targets.txt

  Scenario: .gitignore excludes SSH keys
    Tool: Bash
    Preconditions: .gitignore exists
    Steps:
      1. Run: grep "e2e/.ssh/" .gitignore
      2. Verify match found
    Expected Result: e2e/.ssh/ is in .gitignore
    Evidence: .sisyphus/evidence/task-4-gitignore.txt
  ```

  **Commit**: YES (groups with Task 5)
  - Message: `test(e2e): add E2E test runner and build integration`
  - Files: `Makefile`, `.gitignore`

- [x] 5. E2E Test Runner Script

  **What to do**:
  - Create `e2e/run.sh` — the main orchestration script that handles the full E2E lifecycle:
  - **Argument handling**:
    - No args: run full E2E test suite
    - `--cleanup`: only clean up (remove containers, networks, SSH keys)
  - **Setup phase**:
    - Set `SCRIPT_DIR` to the directory containing the script
    - Set `COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"`
    - Set `SSH_DIR="$SCRIPT_DIR/.ssh"`
    - Register `cleanup` trap on EXIT (always clean up)
    - Generate SSH keypair: `ssh-keygen -t ed25519 -f "$SSH_DIR/id_ed25519" -N "" -q`
    - Copy public key to `$SSH_DIR/authorized_keys`
  - **Build and start**:
    - `docker compose -f "$COMPOSE_FILE" build`
    - `docker compose -f "$COMPOSE_FILE" up -d`
  - **Readiness gates** (with bounded retries, max 60s each):
    - Wait for firewall: `docker compose exec -T firewall iptables -L FORWARD -n` succeeds and shows DROP policy
    - Wait for agent sshd: `docker compose exec -T agent ss -lnt` shows port 22 listening
    - Wait for SSH tunnel: `docker compose exec -T agent ss -lnt` shows port 18080 listening (reverse tunnel active)
    - Wait for agent TUN: `docker compose exec -T agent ip link show sluice0` succeeds
    - Each gate: retry loop with 2s sleep, print status, fail with timeout error after max retries
  - **Negative test** (run BEFORE happy path to avoid TUN interference):
    - "Firewall blocks agent→server": `docker compose exec -T agent sh -c "wget --spider --timeout=3 http://172.20.0.10:18080 2>&1"` — expect non-zero exit code (port 18080 is not intercepted by TUN/nftables since it's not port 80/443/53, so it goes through normal routing → firewall drops it)
  - **Happy path tests**:
    - "HTTP through tunnel": `docker compose exec -T agent curl -fsS --max-time 30 http://example.com` — expect exit 0
    - "HTTPS through tunnel": `docker compose exec -T agent curl -fsS --max-time 30 https://google.com` — expect exit 0
  - **Result reporting**:
    - Print pass/fail for each test with clear formatting
    - On failure: dump container logs to stderr for debugging
    - Exit 0 if all pass, exit 1 if any fail
  - **Cleanup phase** (in trap):
    - `docker compose -f "$COMPOSE_FILE" down -v --remove-orphans`
    - `rm -rf "$SSH_DIR"`
  - Make script executable (`chmod +x`)
  - Use `set -euo pipefail` at top
  - Use colored output for pass/fail (green/red) with fallback for non-terminal

  **Must NOT do**:
  - Do NOT add tests beyond the 3 defined scenarios (HTTP, HTTPS, firewall block)
  - Do NOT add DoH-specific tests
  - Do NOT require internet for the negative test (firewall test is local only)
  - Do NOT leave containers running on failure (trap ensures cleanup)
  - Do NOT use `docker compose exec` without `-T` flag (no TTY in CI)

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Most complex task — orchestration logic, readiness gates with retries, multiple test scenarios, error handling, cleanup. Needs careful sequencing and edge case handling.
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 2 (solo)
  - **Blocks**: Task 6
  - **Blocked By**: Tasks 1, 2, 3, 4

  **References** (CRITICAL):

  **Pattern References**:
  - `e2e/docker-compose.yml` (Task 1 output) — Service names: `server`, `agent`, `firewall`. Compose file path relative to `e2e/`
  - `e2e/entrypoint-agent.sh` (Task 2 output) — Agent starts sshd then sluice agent. SSH keys mounted at `/e2e-ssh/`
  - `e2e/entrypoint-firewall.sh` (Task 3 output) — Firewall applies iptables FORWARD rules
  - `e2e/entrypoint-server.sh` (Task 3 output) — Server adds route, starts sluice with tunnel

  **API/Type References**:
  - Fixed Constants table — All IPs, ports, and paths used in tests
  - `internal/tunnel/tunnel.go:221-226` — Server log message on tunnel connect: `"ssh reverse tunnel connected"` with fields `remote`, `ssh_port`, `remote_bind_port`, `local_port`. Can grep for this in server logs.
  - `internal/gateway/gateway.go:133` — Agent log on startup: `"gateway started"` with fields `tun`, `proxy_ip`. Can grep for this in agent logs.

  **External References**:
  - `internal/gateway/nftables.go` — nftables rules only mark TCP ports 80/443 and UDP port 53. Traffic to port 18080 is NOT intercepted (goes through normal routing). This is WHY the negative test works: `curl http://172.20.0.10:18080` from agent hits normal routing → firewall → DROP.

  **WHY Each Reference Matters**:
  - Service names from docker-compose are needed for `docker compose exec` commands
  - Tunnel log message is the readiness signal — grep for it to know tunnel is up
  - Gateway log message confirms TUN is ready
  - Nftables port scope (80/443/53 only) is critical for understanding why the negative test (port 18080) works — it's not intercepted

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Full E2E test suite passes
    Tool: Bash
    Preconditions: Docker available, Linux host with /dev/net/tun and nf_tables kernel module
    Steps:
      1. Run: ./e2e/run.sh
      2. Observe output for each test result
      3. Verify exit code is 0
    Expected Result: All 3 tests pass (HTTP, HTTPS, firewall block), exit 0
    Failure Indicators: Non-zero exit, test failure output, timeout in readiness gates
    Evidence: .sisyphus/evidence/task-5-e2e-full-run.txt

  Scenario: Cleanup removes all resources
    Tool: Bash
    Preconditions: E2E test completed (pass or fail)
    Steps:
      1. Run: ./e2e/run.sh
      2. After completion, verify: docker ps -a --filter "name=e2e" shows no containers
      3. Verify: ls e2e/.ssh/ fails (directory removed)
      4. Run: docker network ls --filter "name=e2e" shows no networks
    Expected Result: No leftover containers, networks, or SSH keys
    Evidence: .sisyphus/evidence/task-5-cleanup-verify.txt

  Scenario: Script handles --cleanup flag
    Tool: Bash
    Preconditions: No running E2E containers
    Steps:
      1. Run: ./e2e/run.sh --cleanup
      2. Verify no errors (idempotent cleanup)
    Expected Result: Clean exit even when nothing to clean
    Evidence: .sisyphus/evidence/task-5-cleanup-flag.txt
  ```

  **Commit**: YES (groups with Task 4)
  - Message: `test(e2e): add E2E test runner and build integration`
  - Files: `e2e/run.sh`
  - Pre-commit: `shellcheck e2e/run.sh || true`

- [x] 6. GitHub Actions CI E2E Job

  **What to do**:
  - Update `.github/workflows/docker-publish.yml` to add an `e2e` job:
  - Add new job `e2e` that runs after `test` job (same as `build-and-push` dependency):
    ```yaml
    e2e:
      needs: test
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4

        - name: Check E2E prerequisites
          id: prereq
          run: |
            echo "Checking /dev/net/tun..."
            ls -la /dev/net/tun && echo "tun=true" >> "$GITHUB_OUTPUT" || echo "tun=false" >> "$GITHUB_OUTPUT"
            echo "Checking kernel modules..."
            lsmod | grep nf_tables && echo "nft=true" >> "$GITHUB_OUTPUT" || echo "nft=false" >> "$GITHUB_OUTPUT"

        - name: Run E2E tests
          if: steps.prereq.outputs.tun == 'true'
          run: make e2e

        - name: Collect logs on failure
          if: failure()
          run: |
            mkdir -p e2e-artifacts
            docker compose -f e2e/docker-compose.yml logs > e2e-artifacts/compose.log 2>&1 || true
            docker compose -f e2e/docker-compose.yml ps > e2e-artifacts/compose-ps.log 2>&1 || true

        - name: Upload failure artifacts
          if: failure()
          uses: actions/upload-artifact@v4
          with:
            name: e2e-logs
            path: e2e-artifacts/
            retention-days: 7

        - name: Skip notice
          if: steps.prereq.outputs.tun == 'false'
          run: echo "::warning::E2E tests skipped — /dev/net/tun not available on this runner"
    ```
  - Do NOT make `build-and-push` depend on `e2e` — keep E2E independent (can fail without blocking Docker push)
  - Ensure `e2e` job runs on every push to main and on releases (same trigger as existing jobs)

  **Must NOT do**:
  - Do NOT remove or modify existing jobs (test, build-release, build-and-push)
  - Do NOT make build-and-push depend on e2e (E2E is additive, not blocking)
  - Do NOT add self-hosted runner configuration
  - Do NOT skip E2E silently — use `::warning::` annotation when skipping

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: YAML additions to existing workflow file, following established patterns
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 3 (solo)
  - **Blocks**: F1-F4 (final verification)
  - **Blocked By**: Task 5

  **References** (CRITICAL):

  **Pattern References**:
  - `.github/workflows/docker-publish.yml:17-29` — Existing `test` job pattern: `runs-on: ubuntu-latest`, `actions/checkout@v4`, `actions/setup-go@v5`. Follow same structure for `e2e` job.
  - `.github/workflows/docker-publish.yml:63-66` — Existing `build-and-push` job: `needs: test`. E2E should also `needs: test` but NOT be a dependency of `build-and-push`.

  **WHY Each Reference Matters**:
  - Existing workflow style must be matched — same action versions, same job structure
  - E2E must not block the Docker publish pipeline — it's additive verification, not a gate

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: CI workflow YAML is valid after E2E job addition
    Tool: Bash
    Preconditions: Updated workflow file exists
    Steps:
      1. Run: python3 -c "import yaml; yaml.safe_load(open('.github/workflows/docker-publish.yml'))" or equivalent YAML validation
      2. Verify e2e job exists in the workflow
      3. Verify e2e job has needs: test
      4. Verify build-and-push does NOT have needs: e2e
    Expected Result: Valid YAML, correct job dependencies
    Failure Indicators: YAML syntax errors, missing e2e job, incorrect dependencies
    Evidence: .sisyphus/evidence/task-6-ci-yaml-valid.txt

  Scenario: E2E job has prerequisite check and skip logic
    Tool: Bash
    Preconditions: Updated workflow file
    Steps:
      1. Read workflow file
      2. Verify "Check E2E prerequisites" step exists with /dev/net/tun check
      3. Verify "Run E2E tests" step has if condition on prereq output
      4. Verify "Skip notice" step prints warning when skipped
      5. Verify failure artifact collection steps exist
    Expected Result: Conditional E2E execution with proper skip handling
    Evidence: .sisyphus/evidence/task-6-ci-prereq-check.txt
  ```

  **Commit**: YES
  - Message: `ci: add E2E test job to GitHub Actions`
  - Files: `.github/workflows/docker-publish.yml`

---

## Final Verification Wave (MANDATORY — after ALL implementation tasks)

> 4 review agents run in PARALLEL. ALL must APPROVE. Rejection → fix → re-run.

- [x] F1. **Plan Compliance Audit** — `oracle`
  Read the plan end-to-end. For each "Must Have": verify implementation exists (read files, check docker-compose services, verify scripts). For each "Must NOT Have": search codebase for forbidden patterns (production code changes, committed SSH keys, Go dependencies added). Check evidence files exist in `.sisyphus/evidence/`. Compare deliverables against plan.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [x] F2. **Code Quality Review** — `unspecified-high`
  Run shellcheck on all `.sh` files in `e2e/`. Validate docker-compose YAML syntax (`docker compose -f e2e/docker-compose.yml config`). Check Dockerfile best practices (no `latest` tags, proper layer caching). Review `.github/workflows/docker-publish.yml` for valid YAML and correct job dependencies. Check for hardcoded secrets, debug prints, commented-out code.
  Output: `Shellcheck [PASS/FAIL] | Compose [PASS/FAIL] | Dockerfile [PASS/FAIL] | CI YAML [PASS/FAIL] | VERDICT`

- [x] F3. **Real Manual QA** — `unspecified-high`
  Run `make e2e` from the repo root on a Linux machine with Docker. Verify all tests pass. Check container logs for errors. Verify cleanup removes all containers and networks. Run twice to confirm idempotency. Save full output to `.sisyphus/evidence/final-qa/`.
  Output: `Run 1 [PASS/FAIL] | Run 2 [PASS/FAIL] | Cleanup [CLEAN/DIRTY] | VERDICT`

- [x] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", verify actual files match spec. Check that NO files outside `e2e/`, `Makefile`, `.gitignore`, `.dockerignore`, and `.github/workflows/` were modified. Verify no production code was changed (`internal/`, `cmd/`, root `Dockerfile`, root `docker-compose.yml`). Flag any unaccounted changes.
  Output: `Tasks [N/N compliant] | Scope [CLEAN/N violations] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

- **1**: `test(e2e): add docker-compose topology and agent Dockerfile` — `e2e/docker-compose.yml`, `e2e/Dockerfile.agent`
- **2**: `test(e2e): add container entrypoint scripts` — `e2e/entrypoint-agent.sh`, `e2e/entrypoint-firewall.sh`, `e2e/entrypoint-server.sh`
- **3**: `test(e2e): add E2E test runner and build integration` — `e2e/run.sh`, `Makefile`, `.gitignore`
- **4**: `ci: add E2E test job to GitHub Actions` — `.github/workflows/docker-publish.yml`

---

## Success Criteria

### Verification Commands
```bash
make e2e                    # Expected: all tests pass, exit 0
shellcheck e2e/*.sh         # Expected: no errors
docker compose -f e2e/docker-compose.yml config  # Expected: valid YAML
```

### Final Checklist
- [x] All "Must Have" present
- [x] All "Must NOT Have" absent
- [x] `make e2e` passes on Linux with Docker
- [x] CI E2E job succeeds on GitHub Actions
- [x] No production code modified
- [x] SSH keys not committed
