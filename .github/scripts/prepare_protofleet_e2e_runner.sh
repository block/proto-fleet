#!/usr/bin/env bash
set -euo pipefail

readonly INSTALL_ROOT="${PROTOFLEET_E2E_INSTALL_ROOT:-/opt/protofleet-e2e}"
readonly REPOSITORY_ROOT="${GITHUB_WORKSPACE:-$(pwd)}"
readonly JQ="${REPOSITORY_ROOT}/bin/jq"
readonly NPM="${REPOSITORY_ROOT}/bin/npm"
readonly NPX="${REPOSITORY_ROOT}/bin/npx"

require_hash() {
  local name="$1"
  local value="$2"
  if [[ ! "${value}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "${name} must be a non-empty GitHub hashFiles SHA-256 digest." >&2
    exit 1
  fi
}

as_root() {
  if [[ "${EUID}" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

if [[ "$(uname -s)" != "Linux" ]] || [[ "$(uname -m)" != "x86_64" ]]; then
  echo "ProtoFleet E2E runner images must be generated on Linux x64." >&2
  exit 1
fi

: "${PROTOFLEET_SERVER_HASH:?PROTOFLEET_SERVER_HASH is required}"
: "${PROTOFLEET_CLIENT_LOCK_HASH:?PROTOFLEET_CLIENT_LOCK_HASH is required}"
: "${PROTOFLEET_IMAGE_SOURCE_SHA:?PROTOFLEET_IMAGE_SOURCE_SHA is required}"
require_hash PROTOFLEET_SERVER_HASH "${PROTOFLEET_SERVER_HASH}"
require_hash PROTOFLEET_CLIENT_LOCK_HASH "${PROTOFLEET_CLIENT_LOCK_HASH}"
if [[ ! "${PROTOFLEET_IMAGE_SOURCE_SHA}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "PROTOFLEET_IMAGE_SOURCE_SHA must be a Git commit SHA." >&2
  exit 1
fi

if [[ -e "${INSTALL_ROOT}" ]]; then
  echo "${INSTALL_ROOT} already exists; generate from a clean GitHub-owned base image." >&2
  exit 1
fi

for command in docker "${JQ}" "${NPM}" "${NPX}"; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "Required command is unavailable: ${command}" >&2
    exit 1
  fi
done

staging_root="$(mktemp -d)"
cleanup() {
  rm -rf -- "${staging_root}"
  rm -rf -- "${REPOSITORY_ROOT}/client/node_modules"
}
trap cleanup EXIT

cd "${REPOSITORY_ROOT}"

echo "Installing the lockfile-exact client dependency tree..."
"${NPM}" --prefix client ci --include=optional

echo "Installing Chromium and its operating-system dependencies..."
(
  cd client/e2eTests/protoFleet
  PLAYWRIGHT_BROWSERS_PATH="${staging_root}/ms-playwright" \
    "${NPX}" playwright install --with-deps chromium
)

echo "Building and pulling the Docker images used by ProtoFleet E2E..."
(
  cd server
  docker compose pull --ignore-buildable
  docker compose build --pull
)

docker_images=()
while IFS= read -r image; do
  docker_images+=("${image}")
done < <(cd server && docker compose config --images | sort -u)
if [[ "${#docker_images[@]}" -eq 0 ]]; then
  echo "Docker Compose did not report any E2E images." >&2
  exit 1
fi
for image in "${docker_images[@]}"; do
  docker image inspect "${image}" >/dev/null
done

printf '%s\n' "${docker_images[@]}" | "${JQ}" -Rn '[inputs]' > "${staging_root}/docker-images.json"
"${JQ}" -n \
  --arg source_sha "${PROTOFLEET_IMAGE_SOURCE_SHA}" \
  --arg generated_at "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
  --arg server_hash "${PROTOFLEET_SERVER_HASH}" \
  --arg client_lock_hash "${PROTOFLEET_CLIENT_LOCK_HASH}" \
  --argjson docker_images "$(< "${staging_root}/docker-images.json")" \
  '{
    schema_version: 1,
    source_sha: $source_sha,
    generated_at: $generated_at,
    server_hash: $server_hash,
    client_lock_hash: $client_lock_hash,
    docker_images: $docker_images
  }' > "${staging_root}/manifest.json"

echo "Installing reusable content at ${INSTALL_ROOT}..."
as_root install -d -m 0755 "${INSTALL_ROOT}"
as_root cp -R "${REPOSITORY_ROOT}/client/node_modules" "${INSTALL_ROOT}/client-node-modules"
as_root cp -R "${staging_root}/ms-playwright" "${INSTALL_ROOT}/ms-playwright"
as_root install -m 0644 "${staging_root}/manifest.json" "${INSTALL_ROOT}/manifest.json"
as_root chmod -R a+rX "${INSTALL_ROOT}"

echo "Pre-baked $("${JQ}" '.docker_images | length' "${INSTALL_ROOT}/manifest.json") Docker images for source ${PROTOFLEET_IMAGE_SOURCE_SHA}."
