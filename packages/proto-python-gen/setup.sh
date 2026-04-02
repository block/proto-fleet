#!/usr/bin/env bash
#
# setup.sh — Bootstrap a Python venv with pinned gRPC codegen dependencies.
#
# Called by:
#   - `just setup` (in packages/proto-python-gen or root justfile)
#   - `just setup` (root justfile, via _python-gen-init)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="${SCRIPT_DIR}/.venv"
REQUIREMENTS="${SCRIPT_DIR}/requirements.txt"

configure_pip_config() {
  if [[ -f "${SCRIPT_DIR}/pip-config.sh" ]]; then
    # Tarball layout: pip-config.sh is a sibling of setup.sh
    source "${SCRIPT_DIR}/pip-config.sh"
  else
    # Repo layout: pip-config.sh lives in scripts/ at the repo root
    source "${SCRIPT_DIR}/../../scripts/pip-config.sh"
  fi
}

# ── Find Python 3 ────────────────────────────────────────────────────────────
# Search standard system paths first to avoid hermit shims, which can deadlock
# when this script runs inside a hermit on-unpack hook (the shim tries to
# acquire the hermit lock, but hermit install already holds it).
PYTHON=""
SYSTEM_PATHS="/usr/bin /usr/local/bin /opt/homebrew/bin"
for candidate in python3 python; do
  for dir in ${SYSTEM_PATHS}; do
    if [[ -x "${dir}/${candidate}" ]]; then
      PYTHON="${dir}/${candidate}"
      break 2
    fi
  done
done

# Fall back to PATH if no system python found (e.g., developer machines with
# hermit python3 already installed — no deadlock risk outside on-unpack).
if [[ -z "${PYTHON}" ]]; then
  for candidate in python3 python; do
    if command -v "${candidate}" &>/dev/null; then
      PYTHON="${candidate}"
      break
    fi
  done
fi

if [[ -z "${PYTHON}" ]]; then
  echo "Error: python3 not found." >&2
  exit 1
fi

if ! "${PYTHON}" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 8) else 1)' >/dev/null 2>&1; then
  echo "Error: Python 3.8+ is required; found version: $(${PYTHON} --version 2>&1)" >&2
  exit 1
fi

echo "Using Python: $(${PYTHON} --version 2>&1) (${PYTHON})"

# ── Create venv ───────────────────────────────────────────────────────────────
if [[ ! -d "${VENV_DIR}" ]]; then
  echo "Creating venv at ${VENV_DIR} ..."
  "${PYTHON}" -m venv "${VENV_DIR}"
fi

# ── Install dependencies ─────────────────────────────────────────────────────
echo "Installing dependencies from ${REQUIREMENTS} ..."
configure_pip_config
"${VENV_DIR}/bin/pip" install --quiet --upgrade pip
"${VENV_DIR}/bin/pip" install --quiet -r "${REQUIREMENTS}"

# ── Verify ────────────────────────────────────────────────────────────────────
if ! "${VENV_DIR}/bin/python" -c "import grpc_tools" &>/dev/null; then
  echo "Error: grpc_tools not importable after install." >&2
  exit 1
fi

echo "Setup complete. venv: ${VENV_DIR}"
echo "  grpcio-tools: $("${VENV_DIR}/bin/python" -c "from importlib.metadata import version; print(version('grpcio-tools'))")"
echo "  protobuf:     $("${VENV_DIR}/bin/python" -c "from importlib.metadata import version; print(version('protobuf'))")"
