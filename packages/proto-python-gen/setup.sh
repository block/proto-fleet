#!/usr/bin/env bash
#
# setup.sh — Bootstrap a Python venv with pinned gRPC codegen dependencies.
#
# Called by:
#   - Hermit on-unpack hook
#   - `just setup`
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="${SCRIPT_DIR}/.venv"
REQUIREMENTS="${SCRIPT_DIR}/requirements.txt"

# ── Find Python 3 ────────────────────────────────────────────────────────────
PYTHON=""
for candidate in python3 python; do
  if command -v "${candidate}" &>/dev/null; then
    PYTHON="${candidate}"
    break
  fi
done

if [[ -z "${PYTHON}" ]]; then
  echo "Error: python3 not found on PATH." >&2
  exit 1
fi

if ! "${PYTHON}" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 8) else 1)' >/dev/null 2>&1; then
  echo "Error: Python 3.8+ is required; found version: $(${PYTHON} --version 2>&1)" >&2
  exit 1
fi

echo "Using Python: $(${PYTHON} --version 2>&1) ($(command -v ${PYTHON}))"

# ── Create venv ───────────────────────────────────────────────────────────────
if [[ ! -d "${VENV_DIR}" ]]; then
  echo "Creating venv at ${VENV_DIR} ..."
  "${PYTHON}" -m venv "${VENV_DIR}"
fi

# ── Install dependencies ─────────────────────────────────────────────────────
echo "Installing dependencies from ${REQUIREMENTS} ..."
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
