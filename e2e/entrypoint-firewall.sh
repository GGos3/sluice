#!/bin/sh
set -e

# Enable IP forwarding
sysctl -w net.ipv4.ip_forward=1

# Set default FORWARD policy to DROP
iptables -P FORWARD DROP

# Allow established/related connections
iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

# Allow SSH from server network (172.23.0.0/24) to agent network (172.24.0.0/24)
iptables -A FORWARD -s 172.23.0.0/24 -d 172.24.0.0/24 -p tcp --dport 22 -j ACCEPT

# Keep container alive
exec tail -f /dev/null
