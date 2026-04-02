#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lefthook-lib.sh"

if (("$#" == 0)); then
  exit 0
fi

existing_files=()
while IFS= read -r -d '' file; do
  existing_files+=("$file")
done < <(collect_existing_files "$@")

if ((${#existing_files[@]} == 0)); then
  exit 0
fi

require_fully_staged "Ruff" "${existing_files[@]}"

run_ruff() {
  local label="$1"
  local candidate="$2"
  local allow_path_fallback="$3"
  local missing_hint="$4"
  shift 4
  local files=("$@")

  if ((${#files[@]} == 0)); then
    return 0
  fi

  local ruff_bin=""
  if [[ -n "${PROTO_FLEET_RUFF:-}" ]]; then
    if [[ ! -x "${PROTO_FLEET_RUFF}" ]]; then
      echo "PROTO_FLEET_RUFF must point to an executable Ruff binary: ${PROTO_FLEET_RUFF}" >&2
      exit 1
    fi
    ruff_bin="${PROTO_FLEET_RUFF}"
  elif [[ -n "$candidate" && -x "$candidate" ]]; then
    ruff_bin="$candidate"
  elif [[ "$allow_path_fallback" == "true" ]] && command -v ruff >/dev/null 2>&1; then
    ruff_bin="$(command -v ruff)"
  fi

  if [[ -z "$ruff_bin" ]]; then
    echo "Ruff is required for staged files in ${label}, but no executable was found." >&2
    if [[ -n "$candidate" ]]; then
      echo "  Expected: ${candidate}" >&2
    fi
    echo "  ${missing_hint}" >&2
    exit 1
  fi

  "$ruff_bin" check --force-exclude --fix -- "${files[@]}"
  "$ruff_bin" format --force-exclude -- "${files[@]}"
}

example_plugin_files=()
sdk_files=()
generator_files=()
other_files=()

for file in "${existing_files[@]}"; do
  case "$file" in
    plugin/example-python/*)
      example_plugin_files+=("$file")
      ;;
    server/sdk/v1/python/*)
      sdk_files+=("$file")
      ;;
    packages/proto-python-gen/*)
      generator_files+=("$file")
      ;;
    *)
      other_files+=("$file")
      ;;
  esac
done

if ((${#example_plugin_files[@]} > 0)); then
  run_ruff \
    "plugin/example-python" \
    "plugin/example-python/.venv/bin/ruff" \
    "true" \
    "Install ruff in PATH, or set PROTO_FLEET_RUFF=/path/to/ruff before committing plugin/example-python changes." \
    "${example_plugin_files[@]}"
fi

if ((${#sdk_files[@]} > 0)); then
  run_ruff \
    "server/sdk/v1/python" \
    "server/sdk/v1/python/.venv/bin/ruff" \
    "true" \
    "Run 'cd server/sdk/v1/python && just setup', install ruff in PATH, or set PROTO_FLEET_RUFF=/path/to/ruff." \
    "${sdk_files[@]}"
fi

if ((${#generator_files[@]} > 0)); then
  run_ruff \
    "packages/proto-python-gen" \
    "packages/proto-python-gen/.venv/bin/ruff" \
    "true" \
    "Run 'cd packages/proto-python-gen && just setup-dev', install ruff in PATH, or set PROTO_FLEET_RUFF=/path/to/ruff." \
    "${generator_files[@]}"
fi

if ((${#other_files[@]} > 0)); then
  run_ruff \
    "other Python paths" \
    "" \
    "true" \
    "Install ruff in PATH, or set PROTO_FLEET_RUFF=/path/to/ruff before committing Python changes outside the managed project directories." \
    "${other_files[@]}"
fi
