#!/usr/bin/env bash
set -euo pipefail

readonly INSTALL_ROOT="${PROTOFLEET_E2E_INSTALL_ROOT:-/opt/protofleet-e2e}"
readonly MANIFEST="${INSTALL_ROOT}/manifest.json"
readonly JQ="${PROTOFLEET_JQ:-${GITHUB_WORKSPACE}/bin/jq}"

emit_output() {
  printf '%s=%s\n' "$1" "$2" >> "${GITHUB_OUTPUT}"
}

image_version=unavailable
client_dependencies=false
playwright_browsers=false
docker_images_available=false

finish() {
  emit_output image-version "${image_version}"
  emit_output client-dependencies "${client_dependencies}"
  emit_output playwright-browsers "${playwright_browsers}"
  emit_output docker-images "${docker_images_available}"
  echo "ProtoFleet E2E runner image ${image_version}: client dependencies=${client_dependencies}, Playwright=${playwright_browsers}, Docker=${docker_images_available}"
}

if [[ ! -f "${MANIFEST}" ]]; then
  echo "No ProtoFleet E2E runner manifest found; using normal setup paths."
  finish
  exit 0
fi

if ! "${JQ}" -e '
  .schema_version == 1 and
  (.source_sha | type == "string") and
  (.server_hash | type == "string") and
  (.client_lock_hash | type == "string") and
  (.docker_images | type == "array")
' "${MANIFEST}" >/dev/null; then
  echo "::warning::Ignoring an invalid ProtoFleet E2E runner manifest."
  finish
  exit 0
fi

image_version="$("${JQ}" -r '.source_sha' "${MANIFEST}")"
manifest_server_hash="$("${JQ}" -r '.server_hash' "${MANIFEST}")"
manifest_client_lock_hash="$("${JQ}" -r '.client_lock_hash' "${MANIFEST}")"

if [[ "${manifest_client_lock_hash}" == "${PROTOFLEET_CURRENT_CLIENT_LOCK_HASH}" ]]; then
  dependency_source="${INSTALL_ROOT}/client-node-modules"
  dependency_target="${GITHUB_WORKSPACE}/client/node_modules"
  if [[ -d "${dependency_source}" ]] &&
    [[ -n "$(find "${dependency_source}" -mindepth 1 -print -quit)" ]]; then
    if [[ -e "${dependency_target}" ]]; then
      echo "::error::Refusing to overlay an existing client/node_modules directory."
      exit 1
    fi
    cp -R "${dependency_source}" "${dependency_target}"
    client_dependencies=true
  else
    echo "::warning::The runner manifest matches the client lockfile, but its dependency tree is missing."
  fi

  browser_root="${INSTALL_ROOT}/ms-playwright"
  if [[ -d "${browser_root}" ]] &&
    [[ -n "$(find "${browser_root}" -mindepth 1 -print -quit)" ]]; then
    printf 'PLAYWRIGHT_BROWSERS_PATH=%s\n' "${browser_root}" >> "${GITHUB_ENV}"
    playwright_browsers=true
  else
    echo "::warning::The runner manifest matches the client lockfile, but its Playwright browsers are missing."
  fi
fi

if [[ "${PROTOFLEET_ALLOW_PREBAKED_DOCKER}" == "true" ]] &&
  [[ "${manifest_server_hash}" == "${PROTOFLEET_CURRENT_SERVER_HASH}" ]]; then
  docker_images=()
  while IFS= read -r image; do
    docker_images+=("${image}")
  done < <("${JQ}" -r '.docker_images[]' "${MANIFEST}")
  docker_images_available=true
  if [[ "${#docker_images[@]}" -eq 0 ]]; then
    docker_images_available=false
  fi
  for image in "${docker_images[@]}"; do
    if ! docker image inspect "${image}" >/dev/null 2>&1; then
      echo "::warning::Pre-baked Docker image is missing: ${image}"
      docker_images_available=false
    fi
  done
fi

finish
