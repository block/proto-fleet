#!/usr/bin/env bash

set -euo pipefail

echo "Starting Proto Fleet development environment..."
GIT_VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "development")

echo "Starting ProtoFleet client..."
# The Alerts settings nav is behind VITE_ALERTS_ENABLED; surface it
# only when the server is started with the alerts sidecar.
ALERTS_ENABLED=$([[ "${ENABLE_BETA_ALERTS:-}" = "true" ]] && echo "true" || echo "false")
(
  cd client
  VITE_VERSION="$GIT_VERSION" \
  VITE_BUILD_DATE="$BUILD_DATE" \
  VITE_COMMIT="$GIT_COMMIT" \
  VITE_ALERTS_ENABLED="$ALERTS_ENABLED" \
  npm run dev:protoFleet
) & CLIENT_PID=$!
echo "Client PID: $CLIENT_PID"

function start_server() {
  if [[ "${ENABLE_BETA_ALERTS:-}" = "true" ]]; then
    just dev-alerts
  else
    just dev
  fi
}

echo "Starting server..."
(cd server && start_server) & SERVER_PID=$!
echo "Server PID: $SERVER_PID"

echo "Both processes started. Press Ctrl+C to stop both processes"

cleanup() {
    echo "Stopping processes..."
    kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
    wait
    echo "All processes stopped"
}

trap cleanup EXIT INT TERM

wait 
