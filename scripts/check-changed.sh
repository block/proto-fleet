#!/usr/bin/env bash
set -euo pipefail

base=origin/main
list_only=false
declare -a explicit_paths=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --base)
      base="$2"
      shift 2
      ;;
    --list)
      list_only=true
      shift
      ;;
    --)
      shift
      explicit_paths=("$@")
      break
      ;;
    *)
      echo "usage: $0 [--base REF] [--list] [-- PATH ...]" >&2
      exit 2
      ;;
  esac
done

declare -a paths=()
if [ "${#explicit_paths[@]}" -gt 0 ]; then
  paths=("${explicit_paths[@]}")
else
  if ! git rev-parse --verify --quiet "$base^{commit}" >/dev/null; then
    echo "Base ref '$base' does not resolve. Fetch it or pass --base REF." >&2
    exit 2
  fi
  merge_base="$(git merge-base "$base" HEAD)"
  while IFS= read -r -d '' path; do
    paths+=("$path")
  done < <(
    {
      git diff --name-only -z "$merge_base"...HEAD
      git diff --name-only -z
      git diff --cached --name-only -z
      git ls-files --others --exclude-standard -z
    } | sort -zu
  )
fi

client=false
server=false
proto=false
plugin_proto=false
plugin_antminer=false
developer_workflows=false

for path in "${paths[@]}"; do
  case "$path" in
    client/*) client=true ;;
  esac
  case "$path" in
    server/*|go.work|go.work.sum) server=true ;;
  esac
  case "$path" in
    proto/*.proto|proto/**/*.proto|buf.yaml|buf.lock|buf.gen.yaml)
      proto=true
      ;;
  esac
  case "$path" in
    plugin/proto/*|go.work|go.work.sum) plugin_proto=true ;;
  esac
  case "$path" in
    plugin/antminer/*|go.work|go.work.sum) plugin_antminer=true ;;
  esac
  case "$path" in
    justfile|lefthook.yml|scripts/check-changed.sh|scripts/check-diff.sh|scripts/check-changed_test.sh)
      developer_workflows=true
      ;;
  esac
done

declare -a labels=()
declare -a commands=()
add_check() {
  labels+=("$1")
  commands+=("$2")
}

add_check "diff whitespace" "scripts/check-diff.sh"
$proto && add_check "protobuf lint" "just _lint-protos"
$client && add_check "client lint + typecheck" "just _check-client"
$server && add_check "server lint" "just _lint-server"
$plugin_proto && add_check "proto plugin lint" "cd plugin/proto && golangci-lint run -c .golangci.yaml"
$plugin_antminer && add_check "antminer plugin lint" "cd plugin/antminer && golangci-lint run -c .golangci.yaml"
$developer_workflows && add_check "developer workflow tests" "just test-developer-workflows"

if [ "$list_only" = true ]; then
  printf '%s\n' "${commands[@]}"
  exit 0
fi

echo "Changed-path checks (${#paths[@]} files against $base):"
for label in "${labels[@]}"; do
  echo "  - $label"
done

declare -a pids=()
export CHECK_CHANGED_BASE="$base"
for i in "${!commands[@]}"; do
  (
    echo "=== ${labels[$i]} ==="
    bash -euo pipefail -c "${commands[$i]}"
  ) &
  pids+=("$!")
done

failed=0
for pid in "${pids[@]}"; do
  if ! wait "$pid"; then
    failed=1
  fi
done
exit "$failed"
