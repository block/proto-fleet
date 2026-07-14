#!/usr/bin/env bash
set -euo pipefail

curl -fsS --max-time 1 http://127.0.0.1:4080/health/active >/dev/null
