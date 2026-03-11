#!/bin/bash
set -euo pipefail

# Generate SSH host keys at runtime
ssh-keygen -A

# Ensure CA certificates are available for curl/OpenSSL
# Download Mozilla CA bundle if current one is outdated (fixes SSL verification issues)
if [ ! -f /etc/ssl/certs/cacert.pem ] || [ ! -s /etc/ssl/certs/cacert.pem ]; then
    curl -sfS --max-time 30 -o /etc/ssl/certs/cacert.pem https://curl.se/ca/cacert.pem 2>/dev/null || true
fi
export SSL_CERT_FILE=/etc/ssl/certs/cacert.pem
export SSL_CERT_DIR=/etc/ssl/certs

# Ensure SSH directory exists and has correct permissions
mkdir -p /root/.ssh
chmod 700 /root/.ssh

# Copy authorized_keys from mounted volume to root's .ssh
if [ -f /e2e-ssh/authorized_keys ]; then
    cp /e2e-ssh/authorized_keys /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
fi

# Add route to server network via firewall (replace if exists for idempotency)
# This may fail in standalone mode - that's OK, compose will handle it
ip route replace 172.23.0.0/24 via 172.24.0.11 2>/dev/null || true

# Start SSH server in background
/usr/sbin/sshd

# Execute sluice agent
exec sluice agent --port 18080
