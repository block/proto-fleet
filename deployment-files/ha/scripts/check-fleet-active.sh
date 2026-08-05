#!/usr/bin/env sh
set -eu

# Keepalived runs this once per second. A successful local active-health check
# refreshes the heartbeat that fleetd uses to prove the endpoint adapter is alive.
heartbeat_file="$1"
if curl --fail --silent \
    --connect-timeout 1 \
    --max-time 1 \
    http://127.0.0.1:4000/health/active >/dev/null; then
    touch "$heartbeat_file"
    exit 0
fi
exit 1
