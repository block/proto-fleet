#!/usr/bin/env bash

set -euo pipefail

echo "Starting Proto Fleet development environment..."

echo "Starting ProtoFleet client..."
cd client
npm run dev:protoFleet & CLIENT_PID=$!
echo "Client PID: $CLIENT_PID"

echo "Waiting for client to be ready on port 5173..."
while ! nc -z localhost 5173 2>/dev/null; do
    sleep 1
done
echo "Client is ready!"

echo "Starting server..."
cd ../server
just dev & SERVER_PID=$!
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