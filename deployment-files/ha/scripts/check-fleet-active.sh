#!/usr/bin/env sh
set -eu

# Keepalived runs this once per second. A successful local active-health check
# refreshes the heartbeat that fleetd uses to prove the endpoint adapter is alive.
heartbeat_file="$1"
virtual_ip="$2"
service_ca="$3"

# Verify the public endpoint identity while pinning the connection to this host.
curl --fail --silent \
    --cacert "$service_ca" \
    --connect-to "${virtual_ip}:443:127.0.0.1:443" \
    --noproxy '*' \
    --connect-timeout 1 \
    --max-time 1 \
    "https://${virtual_ip}/api-proxy/health/active" >/dev/null || exit 1
touch "$heartbeat_file"
