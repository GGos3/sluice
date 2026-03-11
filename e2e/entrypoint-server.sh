#!/bin/sh
set -e

# Add route to agent network via firewall
ip route add 172.24.0.0/24 via 172.23.0.11

# Create SSH config for non-interactive host key behavior
mkdir -p /root/.ssh
cat > /root/.ssh/config << 'EOF'
Host *
    IdentityFile /e2e-ssh/id_ed25519
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
EOF
chmod 600 /root/.ssh/config

# Start sluice server with SSH reverse tunnel
exec sluice server --tunnel root@172.24.0.10 --ssh-port 22 --port 18080
