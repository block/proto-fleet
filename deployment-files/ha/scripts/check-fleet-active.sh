#!/usr/bin/env sh
set -eu

# Keepalived runs this once per second. A successful local active-health check
# refreshes the heartbeat that fleetd uses to prove the endpoint adapter is alive.
heartbeat_file="$1"
# The deployment certificate names the public endpoint, while this probe stays
# on loopback so it cannot accidentally follow the VIP to the peer.
curl --fail --silent \
    --insecure \
    --connect-timeout 1 \
    --max-time 1 \
    https://localhost/api-proxy/health/active >/dev/null || exit 1
touch "$heartbeat_file"
