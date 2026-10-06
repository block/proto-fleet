#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Refresh Proto Fleet onboarding visual snapshots from a Linux Playwright container.

Usage:
  ./scripts/update-protofleet-visual-snapshots-linux.sh --confirm-overwrite [--project=desktop|mobile|all] [--skip-build]

Examples:
  ./scripts/update-protofleet-visual-snapshots-linux.sh --confirm-overwrite
  ./scripts/update-protofleet-visual-snapshots-linux.sh --confirm-overwrite --project=desktop

What it does:
  1. Builds Proto Fleet locally (unless --skip-build is provided)
  2. Starts a local Vite preview on port 5173
  3. Resets the fake backend before each project
  4. Runs the visual spec inside the official Linux Playwright image
  5. Copies the refreshed screenshots back into the repo

Important:
  - This command overwrites the checked-in expected screenshots.
  - Only run it after reviewing the visual changes and deciding the old screenshots are outdated.
  - Do not run it just to make a failing visual test pass.
  - Review every updated screenshot before committing or pushing.
EOF
}

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
CLIENT_DIR=$(cd -- "$SCRIPT_DIR/.." && pwd)
REPO_ROOT=$(cd -- "$CLIENT_DIR/.." && pwd)

DEFAULT_PREVIEW_PORT=5173
PREVIEW_PORT="${PREVIEW_PORT:-${DEFAULT_PREVIEW_PORT}}"
PLAYWRIGHT_VERSION=$(
  cd "${CLIENT_DIR}"
  node -p 'require("./package.json").devDependencies["@playwright/test"]'
)
PLAYWRIGHT_IMAGE_OVERRIDE="${PLAYWRIGHT_DOCKER_IMAGE:-}"
PLAYWRIGHT_IMAGE=""
PLAYWRIGHT_NODE_MODULES_SOURCE="${PLAYWRIGHT_NODE_MODULES_SOURCE:-}"
JUST_BIN="${JUST_BIN:-}"

CONFIRM_OVERWRITE=0
SKIP_BUILD=0
PROJECTS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project|--project=*)
      project_value=""

      if [[ "$1" == "--project" ]]; then
        if [[ $# -lt 2 ]]; then
          echo "--project requires a value" >&2
          exit 1
        fi
        project_value="$2"
        shift 2
      else
        project_value="${1#--project=}"
        shift
      fi

      if [[ -z "${project_value}" ]]; then
        echo "--project requires a value" >&2
        exit 1
      fi

      case "${project_value}" in
        desktop|mobile)
          PROJECTS+=("${project_value}")
          ;;
        all)
          PROJECTS=(desktop mobile)
          ;;
        *)
          echo "Unsupported project: ${project_value}" >&2
          exit 1
          ;;
      esac
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --confirm-overwrite)
      CONFIRM_OVERWRITE=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ "${CONFIRM_OVERWRITE}" -ne 1 ]]; then
  cat >&2 <<'EOF'
Refusing to overwrite visual snapshots without explicit confirmation.

Re-run with:
  ./scripts/update-protofleet-visual-snapshots-linux.sh --confirm-overwrite

Only do this after reviewing the intended UI change and deciding the existing
expected screenshots are outdated. Review every updated screenshot before
committing or pushing.
EOF
  exit 1
fi

if [[ ${#PROJECTS[@]} -eq 0 ]]; then
  PROJECTS=(desktop mobile)
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if [[ -z "${JUST_BIN}" ]]; then
  if command -v just >/dev/null 2>&1; then
    JUST_BIN=$(command -v just)
  elif [[ -x "${REPO_ROOT}/bin/just" ]]; then
    JUST_BIN="${REPO_ROOT}/bin/just"
  else
    cat >&2 <<EOF
Missing 'just'.

Expected one of:
  - just on PATH
  - ${REPO_ROOT}/bin/just

If repo tools are not installed yet, run:
  cd ${REPO_ROOT}
  source ./bin/activate-hermit
EOF
    exit 1
  fi
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

if [[ ! -x "${CLIENT_DIR}/node_modules/.bin/vite" ]]; then
  cat >&2 <<EOF
Missing local client dependency: vite

Reinstall client dependencies first, then rerun this script:
  cd ${CLIENT_DIR}
  npm install
EOF
  exit 1
fi

find_cached_playwright_node_modules() {
  local requested_version="$1"
  local fallback=""
  local pkg_json=""

  while IFS= read -r pkg_json; do
    local pkg_dir
    local cached_version

    pkg_dir=$(dirname "${pkg_json}")
    cached_version=$(node -p 'require(process.argv[1]).version' "${pkg_json}")

    if [[ "${cached_version}" == "${requested_version}" ]]; then
      dirname "${pkg_dir}"
      return 0
    fi

    if [[ -z "${fallback}" ]]; then
      fallback=$(dirname "${pkg_dir}")
    fi
  done < <(find "${HOME}/.npm/_npx" -path '*/node_modules/playwright/package.json' 2>/dev/null)

  if [[ -n "${fallback}" ]]; then
    echo "${fallback}"
    return 0
  fi

  return 1
}

if [[ -z "${PLAYWRIGHT_NODE_MODULES_SOURCE}" ]] && \
  [[ -f "${CLIENT_DIR}/node_modules/playwright/package.json" ]] && \
  [[ -f "${CLIENT_DIR}/node_modules/playwright-core/package.json" ]]; then
  PLAYWRIGHT_NODE_MODULES_SOURCE="${CLIENT_DIR}/node_modules"
fi

if [[ -z "${PLAYWRIGHT_NODE_MODULES_SOURCE}" ]]; then
  if ! PLAYWRIGHT_NODE_MODULES_SOURCE=$(find_cached_playwright_node_modules "${PLAYWRIGHT_VERSION}"); then
    cat >&2 <<EOF
No usable Playwright package source was found.

Preferred source:
  ${CLIENT_DIR}/node_modules

Fallback source:
  ~/.npm/_npx

Install client dependencies first, then rerun this script:
  cd ${CLIENT_DIR}
  npm install

If you intentionally want to use the fallback cache source, you can also seed it with:
  cd ${CLIENT_DIR}
  npx playwright@${PLAYWRIGHT_VERSION} --version

If the packages live somewhere else, you can also set:
  PLAYWRIGHT_NODE_MODULES_SOURCE=/absolute/path/to/node_modules
EOF
    exit 1
  fi
fi

if [[ ! -f "${PLAYWRIGHT_NODE_MODULES_SOURCE}/playwright/package.json" ]]; then
  echo "PLAYWRIGHT_NODE_MODULES_SOURCE does not contain playwright/package.json: ${PLAYWRIGHT_NODE_MODULES_SOURCE}" >&2
  exit 1
fi

CACHED_PLAYWRIGHT_VERSION=$(node -p 'require(process.argv[1]).version' "${PLAYWRIGHT_NODE_MODULES_SOURCE}/playwright/package.json")

if [[ -n "${PLAYWRIGHT_IMAGE_OVERRIDE}" ]]; then
  PLAYWRIGHT_IMAGE="${PLAYWRIGHT_IMAGE_OVERRIDE}"
elif [[ "${CACHED_PLAYWRIGHT_VERSION}" == "${PLAYWRIGHT_VERSION}" ]]; then
  PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v${PLAYWRIGHT_VERSION}-noble"
else
  PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v${CACHED_PLAYWRIGHT_VERSION}-noble"
  echo "Using Playwright ${CACHED_PLAYWRIGHT_VERSION} from ${PLAYWRIGHT_NODE_MODULES_SOURCE} and matching Docker image ${PLAYWRIGHT_IMAGE} because ${PLAYWRIGHT_VERSION} is not available there." >&2
fi

find_available_preview_port() {
  local port="$1"
  local max_port=$((port + 20))

  while [[ "${port}" -le "${max_port}" ]]; do
    if ! lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "${port}"
      return 0
    fi
    port=$((port + 1))
  done

  return 1
}

if lsof -iTCP:"${PREVIEW_PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
  if [[ "${PREVIEW_PORT}" == "${DEFAULT_PREVIEW_PORT}" ]]; then
    if ! PREVIEW_PORT=$(find_available_preview_port "${DEFAULT_PREVIEW_PORT}"); then
      echo "No free preview port found in ${DEFAULT_PREVIEW_PORT}-$((DEFAULT_PREVIEW_PORT + 20)). Set PREVIEW_PORT explicitly." >&2
      exit 1
    fi

    echo "Port ${DEFAULT_PREVIEW_PORT} is already in use. Falling back to port ${PREVIEW_PORT}."
  else
    echo "Port ${PREVIEW_PORT} is already in use. Stop the existing server or set PREVIEW_PORT to a free port." >&2
    exit 1
  fi
fi

HOST_PREVIEW_URL="http://127.0.0.1:${PREVIEW_PORT}"
CONTAINER_BASE_URL="${E2E_BASE_URL_FOR_CONTAINER:-http://host.docker.internal:${PREVIEW_PORT}}"

BACKEND_MANAGED=0
PREVIEW_PID=""
PREVIEW_LOG="${REPO_ROOT}/.tmp/protofleet-visual-preview.log"
mkdir -p "${REPO_ROOT}/.tmp"

cleanup() {
  if [[ "${BACKEND_MANAGED}" -eq 1 ]]; then
    (
      cd "${REPO_ROOT}/server"
      "${JUST_BIN}" stop
    ) >/dev/null 2>&1 || true
  fi

  if [[ -n "${PREVIEW_PID}" ]] && kill -0 "${PREVIEW_PID}" >/dev/null 2>&1; then
    kill "${PREVIEW_PID}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

wait_for_fleet_api_ready() {
  for _ in {1..120}; do
    if curl --fail --silent --show-error "http://127.0.0.1:4000/health/ready" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done

  echo "fleet-api did not become ready on 127.0.0.1:4000" >&2
  return 1
}

if [[ "${SKIP_BUILD}" -eq 0 ]]; then
  echo "Building Proto Fleet..."
  (
    cd "${CLIENT_DIR}"
    npm run build:protoFleet
  )
fi

echo "Building Linux plugin binaries for the fake backend..."
(
  cd "${REPO_ROOT}"
  "${JUST_BIN}" build-plugins-docker
)

echo "Starting Proto Fleet preview on ${HOST_PREVIEW_URL}..."
(
  cd "${CLIENT_DIR}"
  npm run preview:protoFleet -- --host 0.0.0.0 --port "${PREVIEW_PORT}"
) >"${PREVIEW_LOG}" 2>&1 &
PREVIEW_PID=$!

for _ in {1..30}; do
  if curl -sf "${HOST_PREVIEW_URL}" >/dev/null; then
    break
  fi
  sleep 1
done

if ! curl -sf "${HOST_PREVIEW_URL}" >/dev/null; then
  echo "Preview server did not become ready. See ${PREVIEW_LOG}" >&2
  exit 1
fi

run_linux_playwright() {
  local project="$1"

  echo
  echo "Resetting fake backend for ${project}..."
  BACKEND_MANAGED=1
  (
    cd "${REPO_ROOT}/server"
    "${JUST_BIN}" rebuild-services
  )

  echo "Waiting for fleet-api readiness..."
  wait_for_fleet_api_ready

  echo "Refreshing ${project} snapshots in ${PLAYWRIGHT_IMAGE}..."
  docker run --rm --init --ipc=host \
    --add-host=host.docker.internal:host-gateway \
    -e CI=1 \
    -e HOST_UID="$(id -u)" \
    -e HOST_GID="$(id -g)" \
    -e PW_UI_NO_DEPS=1 \
    -e PROTOFLEET_VISUAL_ONLY=1 \
    -e PROTOFLEET_VISUAL_BASE_URL="${CONTAINER_BASE_URL}" \
    -e E2E_TARGET=fake \
    -e PROTOFLEET_VISUAL_OVERWRITE=1 \
    -e PROJECT_NAME="${project}" \
    -e CACHED_PLAYWRIGHT_VERSION="${CACHED_PLAYWRIGHT_VERSION}" \
    -v "${REPO_ROOT}:/repo" \
    -v "${PLAYWRIGHT_NODE_MODULES_SOURCE}:/pw-cache-node_modules:ro" \
    "${PLAYWRIGHT_IMAGE}" \
    bash -lc '
      set -euo pipefail
      WORKDIR=/tmp/proto-fleet-linux
      rm -rf "$WORKDIR"
      mkdir -p "$WORKDIR/client"
      tar \
        --exclude="node_modules" \
        --exclude="playwright-report" \
        --exclude="test-results" \
        -cf - -C /repo/client . | tar -xf - -C "$WORKDIR/client"

      mkdir -p "$WORKDIR/client/node_modules" "$WORKDIR/client/node_modules/@playwright/test"
      cp -R /pw-cache-node_modules/playwright /pw-cache-node_modules/playwright-core \
        "$WORKDIR/client/node_modules/"

      cat > "$WORKDIR/client/node_modules/@playwright/test/package.json" <<EOF
{
  "name": "@playwright/test",
  "version": "${CACHED_PLAYWRIGHT_VERSION}-shim",
  "type": "module",
  "main": "./index.cjs",
  "exports": {
    ".": {
      "import": "./index.mjs",
      "require": "./index.cjs",
      "default": "./index.cjs"
    }
  }
}
EOF

      cat > "$WORKDIR/client/node_modules/@playwright/test/index.cjs" <<EOF
module.exports = require("playwright/test");
EOF

      cat > "$WORKDIR/client/node_modules/@playwright/test/index.mjs" <<EOF
export * from "playwright/test";
import pkg from "playwright/test";
export default pkg;
EOF

      cd "$WORKDIR/client"
      node node_modules/playwright/cli.js test -c e2eTests/protoFleet/playwright.config.ts --project="$PROJECT_NAME" \
        e2eTests/protoFleet/spec/onboardingVisual.spec.ts

      cp -a \
        "$WORKDIR/client/e2eTests/protoFleet/spec/onboardingVisual.spec.ts-snapshots/visual/." \
        /repo/client/e2eTests/protoFleet/spec/onboardingVisual.spec.ts-snapshots/visual/
      chown -R "$HOST_UID:$HOST_GID" \
        /repo/client/e2eTests/protoFleet/spec/onboardingVisual.spec.ts-snapshots/visual
    '
}

for project in "${PROJECTS[@]}"; do
  run_linux_playwright "${project}"
done

echo
echo "Done. Updated snapshots are in:"
echo "  ${REPO_ROOT}/client/e2eTests/protoFleet/spec/onboardingVisual.spec.ts-snapshots/visual"
