#!/usr/bin/env bash
set -euo pipefail

check_list() {
  local expected="$1"
  shift
  local actual
  actual="$(scripts/check-changed.sh --list -- "$@")"
  if [ "$actual" != "$expected" ]; then
    echo "unexpected checks for: $*" >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
    exit 1
  fi
}

check_list "scripts/check-diff.sh" docs/README.md
check_list $'scripts/check-diff.sh\njust _check-client' client/src/App.tsx
check_list $'scripts/check-diff.sh\njust _lint-server' server/internal/example.go
check_list $'scripts/check-diff.sh\njust _lint-protos' proto/example/v1/example.proto
check_list $'scripts/check-diff.sh\ncd plugin/proto && golangci-lint run -c .golangci.yaml' plugin/proto/main.go
check_list $'scripts/check-diff.sh\njust _lint-server\ncd plugin/proto && golangci-lint run -c .golangci.yaml\ncd plugin/antminer && golangci-lint run -c .golangci.yaml' go.work
check_list $'scripts/check-diff.sh\njust test-developer-workflows' justfile

echo "check-changed routing tests passed"
