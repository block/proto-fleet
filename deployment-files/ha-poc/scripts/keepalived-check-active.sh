#!/usr/bin/env bash
set -euo pipefail

port="${1:-${HA_FAKE_FLEET_PORT:-4080}}"
curl -fsS --max-time 1 "http://127.0.0.1:${port}/health/active" >/dev/null
