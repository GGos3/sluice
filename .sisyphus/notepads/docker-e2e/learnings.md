# Learnings

## Task 1: E2E Docker Compose Topology (Updated)

### Key Findings
- When docker-compose.yml is in a subdirectory (e2e/), the build context paths need to be relative to that subdirectory
- Use `context: ../` to reference the repo root from within e2e/
- Use `../e2e/.ssh` for volume mounts relative to the compose file location
- Fixed IPs are set via `ipv4_address` in the network config
- The compose file validates successfully with `docker compose -f e2e/docker-compose.yml config --quiet`

### Network Topology
- net-server: 172.23.0.0/24 (server + firewall) - ADJUSTED from 172.20.0.0/24 due to environment conflict
- net-agent: 172.24.0.0/24 (agent + firewall) - ADJUSTED from 172.21.0.0/24 due to environment conflict
- firewall bridges both networks at 172.23.0.11 and 172.24.0.11

### Service Configuration
- server: builds from repo root (../) with Dockerfile target: server, IP 172.23.0.10
- agent: builds from repo root with e2e/Dockerfile.agent, IP 172.24.0.10
- firewall: Alpine 3.21, on both networks

### Runtime Fix Details
- Original plan subnets (172.20.0.0/24, 172.21.0.0/24) conflicted with pre-existing Docker network `oh-my-openclaw_default` (172.20.0.0/16)
- Pool overlap error: "invalid pool request: Pool overlaps with other one on this address space"
- Fixed by using non-conflicting subnets: 172.23.0.0/24 and 172.24.0.0/24
- Also added explicit network names (e2e-net-server, e2e-net-agent) to avoid naming conflicts
- Added entrypoint script mounts for server and agent (previously missing)
- Changed SSH volume from read-only to read-write so entrypoint scripts can create config files

### Firewall IP Forwarding Fix
- Original error: `sysctl: error setting key 'net.ipv4.ip_forward': Read-only file system`
- Root cause: entrypoint-firewall.sh runs `sysctl -w net.ipv4.ip_forward=1` but proc is read-only in container
- Solution: Added compose-level `sysctls: net.ipv4.ip_forward=1` and `privileged: true` to firewall service
- The sysctl is now set BEFORE the entrypoint runs, so the kernel parameter is already enabled
- Verified: container logs show `net.ipv4.ip_forward = 1` before the entrypoint script runs

### Firewall iptables Availability Fix
- Original error: `/entrypoint-firewall.sh: line 8: iptables: not found`
- Root cause: Alpine 3.21 base image doesn't include iptables by default
- Solution: Changed entrypoint to `["/bin/sh", "-c", "apk add --no-cache iptables && exec /entrypoint-firewall.sh"]`
- This installs iptables BEFORE the mounted script runs, then execs into the script
- Verified: container logs show iptables package installation succeeded, container stays running

## Task 3: Firewall and Server Entrypoint Scripts

### Key Findings
- Firewall script uses iptables (not nftables) for simplicity and universal availability in Alpine
- IP forwarding enabled via `sysctl -w net.ipv4.ip_forward=1`
- Default FORWARD policy set to DROP for security
- ESTABLISHED,RELATED connections always allowed for return traffic
- SSH (TCP port 22) allowed from server net (172.20.0.0/24) to agent net (172.21.0.0/24)
- Server script adds route to agent network via firewall at 172.20.0.11
- SSH config sets StrictHostKeyChecking=no, UserKnownHostsFile=/dev/null, LogLevel=ERROR for non-interactive operation
- Server launches sluice with `--tunnel root@172.21.0.10 --ssh-port 22 --port 18080`

### Script Validation
- Both scripts pass `bash -n` syntax check
- Both scripts have executable bit set (chmod +x)
- Scripts use bash shebang (#!/bin/bash) consistent with project conventions

### SSH Config Compatibility
- Tunnel manager (tunnel.go:254-263) runs external ssh with `-R`, `-o ServerAliveInterval=60`, etc.
- Server entrypoint SSH config adds StrictHostKeyChecking options without conflicting with tunnel manager flags
- No identity file specified in config (uses system default ~/.ssh/id_rsa or similar)

## Task 2: Agent E2E Image with SSH Server

### Key Findings
- Dockerfile.agent uses multi-stage build: builder stage replicates root Dockerfile lines 1-16
- Builder stage uses golang:1.24-alpine, copies go.mod/go.sum, runs go mod download, then builds sluice binary
- Final agent stage uses alpine:3.21, installs ca-certificates openssh-server bash iproute2
- SSH host keys generated at build time with `ssh-keygen -A`
- sshd_config configured with PermitRootLogin yes, PubkeyAuthentication yes, PasswordAuthentication no
- Added HostKeyAlgorithms and PubkeyAcceptedKeyTypes for RSA compatibility
- Entrypoint script copied to /e2e/entrypoint-agent.sh and made executable
- Build context is repo root (../), so COPY paths use e2e/ prefix

### Entrypoint Script Details
- Uses bash shebang (#!/bin/bash)
- Creates /root/.ssh directory with chmod 700
- Copies authorized_keys if mounted (chmod 600)
- Adds route to server network: `ip route add 172.20.0.0/24 via 172.21.0.11`
- Starts sshd in background
- Executes `sluice agent --port 18080`

### Verification Results
- Image builds successfully: `docker build -f e2e/Dockerfile.agent -t sluice-e2e-agent-test .`
- sluice binary functional: `docker run --rm --entrypoint sluice sluice-e2e-agent-test version` returns "sluice dev"
- sshd exists at /usr/sbin/sshd
- entrypoint-agent.sh has correct bash shebang and executable bit