#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="deployment"
UPDATER_BOOTSTRAP_DIR=""
UPDATER_CLEANUP_FAILED=0
UPDATER_PRIVILEGE=()
UPDATER_PRIVILEGE_AVAILABLE=1

# Probe docker for an existing fleet-api container and return the install
# directory inferred from its bind mount. Echoes the path on success; returns
# 1 on miss.
#
# Takes a privilege-wrapper argv (empty for unprivileged, or `sudo -n` for
# elevated). All docker calls AND the marker validation are run through the
# wrapper at the same privilege level — without that, sudo-detected installs
# at root-only paths (e.g. /root/proto-fleet) would pass the docker probe
# but silently fail the unprivileged `test -f` check and look absent.
probe_install_dir_with() {
  local privilege=("$@")
  # Note: `${arr[@]+"${arr[@]}"}` is the set -u-safe expansion idiom for
  # arrays that may be empty. A bare `"${arr[@]}"` errors out on an empty
  # array under `set -u` in bash 3.2 and (intermittently) bash 4.x.
  local container_id
  container_id=$(${privilege[@]+"${privilege[@]}"} docker ps -a --filter "name=${DEPLOYMENT_DIR}-fleet-api" --filter "name=${DEPLOYMENT_DIR}_fleet-api" --format "{{.ID}}" 2>/dev/null | head -n 1 || true)
  [ -z "$container_id" ] && return 1

  local mount_path
  mount_path=$(${privilege[@]+"${privilege[@]}"} docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/fleet/start"}}{{.Source}}{{end}}{{end}}' "$container_id" 2>/dev/null || true)
  [ -z "$mount_path" ] && return 1

  # Recover the install dir by stripping the trailing /deployment/<...>
  # segment with parameter expansion. `${var%/deployment/*}` strips only the
  # shortest trailing match, so /home/alice/deployment/proto-fleet/deployment/...
  # resolves to /home/alice/deployment/proto-fleet (not /home/alice).
  # Edge cases:
  #   - No /deployment/<...> segment present  -> mount path unchanged -> miss.
  #   - Mount path is /deployment/<...>       -> install root is "/"; expand
  #                                              empty to "/" before returning.
  # `${var%/deployment/*}` requires at least one character after /deployment;
  # a mount source that ends exactly at /deployment (no trailing subpath)
  # wouldn't match, leaving install_dir == mount_path and tripping the miss
  # branch below. Try the trailing-segment form first; if the mount source
  # ends exactly at /deployment, fall back to stripping the bare suffix.
  local install_dir="${mount_path%/${DEPLOYMENT_DIR}/*}"
  if [ "$install_dir" = "$mount_path" ]; then
    install_dir="${mount_path%/${DEPLOYMENT_DIR}}"
  fi
  if [ "$install_dir" = "$mount_path" ]; then
    return 1
  fi
  # Reject install_dir="/" — a mount source like /deployment/<x> would
  # otherwise propose installing into / itself. No supported layout puts
  # ProtoFleet directly at the filesystem root.
  if [ -z "$install_dir" ] || [ "$install_dir" = "/" ]; then
    return 1
  fi

  # Validate the recovered dir actually houses a ProtoFleet install by
  # checking for the bundled docker-compose.yaml marker. This guards against
  # an unrelated container that happens to share the name filter and mounts
  # a path with /deployment/ in it.
  #
  # Run the marker check at the SAME privilege level used for docker — a
  # root-owned install path may be unreadable to the invoking shell, so an
  # unprivileged `[ -f ]` would falsely report missing.
  #
  # When the elevated check exits non-zero, distinguish two cases via stderr:
  #   - empty stderr  -> `test -f` ran and the marker is genuinely missing
  #                       (test is silent on plain misses) -> treat as miss.
  #   - non-empty     -> sudo refused (sudoers permits docker but not test,
  #                       missing askpass, etc.) -> accept the discovery
  #                       conservatively, since docker already confirmed a
  #                       name-matching container at this path and we'd
  #                       rather trip the privilege-mismatch guard than
  #                       silently miss the install.
  local marker="${install_dir%/}/${DEPLOYMENT_DIR}/docker-compose.yaml"
  if [ "${#privilege[@]}" -eq 0 ]; then
    [ -f "$marker" ] || return 1
  else
    local test_err
    if ! test_err=$(${privilege[@]+"${privilege[@]}"} test -f "$marker" 2>&1); then
      [ -z "$test_err" ] && return 1
    fi
  fi
  echo "$install_dir"
}

# Determines the installation directory by detecting previous installations.
# Probes the unprivileged docker first; if that misses, falls back to a
# non-interactive `sudo docker` probe so we can spot installs whose containers
# live in the root daemon. Writes results to globals (rather than stdout) so
# the sudo signal isn't lost across a subshell:
#   PREVIOUS_INSTALL_DIR          — install dir, or empty if none detected
#   PREVIOUS_INSTALL_NEEDS_SUDO   — 1 if the install was only visible via sudo
#   PREVIOUS_INSTALL_SUDO_BLOCKED — 1 if sudo would prompt and we couldn't
#                                   probe the root daemon at all (so a
#                                   "not detected" result might just mean
#                                   "couldn't check"; the caller surfaces
#                                   this in the suggestion text).
detect_previous_install() {
  PREVIOUS_INSTALL_DIR=""
  PREVIOUS_INSTALL_NEEDS_SUDO=0
  PREVIOUS_INSTALL_SUDO_BLOCKED=0

  local install_dir
  if install_dir=$(probe_install_dir_with); then
    PREVIOUS_INSTALL_DIR="$install_dir"
    return 0
  fi

  # Already root — no sudo prompt is possible. Probe via sudo for symmetry
  # (covers rootless-vs-rootful docker daemon splits).
  if [ "$(id -u)" -eq 0 ]; then
    if install_dir=$(probe_install_dir_with sudo -n); then
      PREVIOUS_INSTALL_DIR="$install_dir"
      PREVIOUS_INSTALL_NEEDS_SUDO=1
      return 0
    fi
    return 1
  fi

  # No sudo binary -> nothing to probe; skip to avoid leaking
  # "sudo: command not found" to user stderr on minimal hosts.
  command -v sudo >/dev/null 2>&1 || return 1

  # The sudo fallback is only meaningful when sudo can run docker without
  # prompting. An earlier `sudo -n true` gate was too strict — sudoers configs
  # that NOPASSWD `docker` specifically don't necessarily NOPASSWD arbitrary
  # commands. Probe sudo's view of docker directly and inspect stderr to
  # distinguish "sudo refused (needs password)" from "sudo ran but found
  # no install". Only the refused case sets SUDO_BLOCKED; the rest fall
  # through to the actual probe.
  # `2>&1 >/dev/null` captures stderr only (redirect order matters: dup
  # stderr to current stdout first, then point stdout at /dev/null).
  local sudo_probe_err
  sudo_probe_err=$(sudo -n docker version --format 'x' 2>&1 >/dev/null || true)
  # Anchor each pattern on `sudo:` so docker stderr that happens to mention
  # "password" / "terminal" / "tty" can't false-positive into SUDO_BLOCKED.
  case "$sudo_probe_err" in
    *"sudo: a password is required"*|*"sudo: a terminal is required"*|*"sudo:"*"may not run"*|*"sudo: no tty present"*|*"is not in the sudoers file"*|*"sudo: sorry, you must have a tty to run sudo"*)
      PREVIOUS_INSTALL_SUDO_BLOCKED=1
      return 1
      ;;
  esac

  if install_dir=$(probe_install_dir_with sudo -n); then
    PREVIOUS_INSTALL_DIR="$install_dir"
    PREVIOUS_INSTALL_NEEDS_SUDO=1
    return 0
  fi
  return 1
}

parse_compose_env_value() {
  local value="$1"
  local double_quoted='^"([^"\\]*)"[[:space:]]*(#.*)?$'
  local single_quoted="^'([^']*)'[[:space:]]*(#.*)?$"

  value=$(printf '%s' "$value" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//')
  case "$value" in
    \"*)
      [[ "$value" =~ $double_quoted ]] || return 2
      value="${BASH_REMATCH[1]}"
      ;;
    \'*)
      [[ "$value" =~ $single_quoted ]] || return 2
      value="${BASH_REMATCH[1]}"
      ;;
    *)
      # Compose treats # as an inline comment only when whitespace separates
      # it from an unquoted value.
      value=$(printf '%s' "$value" | sed -E 's/[[:space:]]+#.*$//; s/[[:space:]]+$//')
      ;;
  esac
  printf '%s' "$value"
}

# Read the last assignment using Compose's documented dotenv delimiters and
# comment rules. Returns 1 when absent and 2 when the file or assignment is
# malformed, so callers never silently infer over an explicit value.
compose_env_last_value() {
  local env_file="$1"
  local key="$2"
  local line normalized parsed found=false
  local assignment_re="^${key}[[:space:]]*([=:])(.*)$"
  local malformed_re="^${key}([[:space:]]|$)"

  [ -e "$env_file" ] || return 1
  [ -f "$env_file" ] && [ -r "$env_file" ] || return 2
  while IFS= read -r line || [ -n "$line" ]; do
    normalized=$(printf '%s' "$line" | sed -E 's/^[[:space:]]+//')
    case "$normalized" in
      export[[:space:]]*)
        normalized="${normalized#export}"
        normalized=$(printf '%s' "$normalized" | sed -E 's/^[[:space:]]+//')
        ;;
    esac
    if [[ "$normalized" =~ $assignment_re ]]; then
      parsed=$(parse_compose_env_value "${BASH_REMATCH[2]}") || return 2
      found=true
    elif [[ "$normalized" =~ $malformed_re ]]; then
      return 2
    fi
  done < "$env_file"

  [ "$found" = true ] || return 1
  printf '%s' "$parsed"
}

# Report an explicit deployment boolean as true/false, or "missing" when the
# key is absent. Invalid explicit values are rejected instead of being
# silently replaced by an inferred container state.
deployment_boolean_state() {
  local env_file="$1"
  local key="$2"
  local value read_status

  if value=$(compose_env_last_value "$env_file" "$key"); then
    :
  else
    read_status=$?
    if [ "$read_status" -eq 1 ]; then
      echo "missing"
      return 0
    fi
    return 2
  fi

  value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
  case "$value" in
    true|false) echo "$value" ;;
    '') echo "false" ;;
    *) return 2 ;;
  esac
}

compose_config_files_has() {
  local config_files="$1"
  local expected_file="$2"
  case "$expected_file" in
    *,*) return 2 ;;
  esac

  local expected_dir expected_name canonical_expected
  expected_dir=$(cd "$(dirname "$expected_file")" 2>/dev/null && pwd -P) || return 2
  expected_name=$(basename "$expected_file")
  canonical_expected="${expected_dir%/}/${expected_name}"

  local config_file config_dir config_name canonical_config
  while IFS= read -r config_file; do
    config_file=$(printf '%s' "$config_file" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    case "$config_file" in
      /*) ;;
      *) continue ;;
    esac
    config_dir=$(cd "$(dirname "$config_file")" 2>/dev/null && pwd -P) || continue
    config_name=$(basename "$config_file")
    canonical_config="${config_dir%/}/${config_name}"
    [ "$canonical_config" = "$canonical_expected" ] && return 0
  done <<< "$(printf '%s' "$config_files" | tr ',' '\n')"
  return 1
}

# Return the one Compose-managed fleet-api container whose recorded base
# Compose file belongs to the selected installation. Exact service labels and
# canonical paths avoid guessing from container-name substrings.
find_install_container_with() {
  local install_dir="$1"
  shift
  local privilege=("$@")
  local base_file="${install_dir%/}/${DEPLOYMENT_DIR}/docker-compose.yaml"
  local container_ids
  if ! container_ids=$(${privilege[@]+"${privilege[@]}"} docker ps -a \
    --filter "label=com.docker.compose.service=fleet-api" \
    --format "{{.ID}}" 2>/dev/null); then
    return 3
  fi

  local container_id config_files selected_container="" match_count=0 inspect_failed=0
  while IFS= read -r container_id; do
    [ -n "$container_id" ] || continue
    if ! config_files=$(${privilege[@]+"${privilege[@]}"} docker inspect \
      --format '{{with index .Config.Labels "com.docker.compose.project.config_files"}}{{.}}{{end}}' \
      "$container_id" 2>/dev/null); then
      inspect_failed=1
      continue
    fi
    if compose_config_files_has "$config_files" "$base_file"; then
      selected_container="$container_id"
      match_count=$((match_count + 1))
    fi
  done <<< "$container_ids"

  [ "$inspect_failed" -eq 0 ] || return 3
  [ "$match_count" -le 1 ] || return 2
  [ "$match_count" -eq 1 ] || return 1
  echo "$selected_container"
}

# Migrate only overlay settings absent from the existing deployment .env.
# The Compose config-files label records the old model without trusting
# mutable service environment values. PREVIOUS_* is set only for a missing
# setting whose legacy overlay was active.
capture_previous_run_options() {
  local install_dir="$1"
  local existing_deployment="$2"
  shift 2
  local privilege=("$@")
  local env_file="${install_dir%/}/${DEPLOYMENT_DIR}/.env"

  PREVIOUS_BETA_ALERTS=0
  PREVIOUS_SYSTEM_MONITORING=0
  PREVIOUS_TRACING=0

  local beta_state system_state tracing_state
  beta_state=$(deployment_boolean_state "$env_file" ENABLE_BETA_ALERTS) || {
    echo "❌ Existing ENABLE_BETA_ALERTS in $env_file must be true or false." >&2
    return 1
  }
  system_state=$(deployment_boolean_state "$env_file" ENABLE_SYSTEM_MONITORING) || {
    echo "❌ Existing ENABLE_SYSTEM_MONITORING in $env_file must be true or false." >&2
    return 1
  }
  tracing_state=$(deployment_boolean_state "$env_file" ENABLE_TRACING) || {
    echo "❌ Existing ENABLE_TRACING in $env_file must be true or false." >&2
    return 1
  }

  if [ "$beta_state" != "missing" ] \
    && [ "$system_state" != "missing" ] \
    && [ "$tracing_state" != "missing" ]; then
    if [ "$system_state" = "true" ] && [ "$beta_state" != "true" ]; then
      echo "❌ Existing overlay state enables system monitoring without beta alerts; correct the ENABLE_* values in ${env_file} before upgrading." >&2
      return 1
    fi
    return 0
  fi
  # Fresh installs have no legacy state to migrate and retain false defaults.
  [ "$existing_deployment" = "1" ] || return 0

  local container_id find_status
  if container_id=$(find_install_container_with "$install_dir" \
    ${privilege[@]+"${privilege[@]}"}); then
    :
  else
    find_status=$?
    case "$find_status" in
      2) echo "❌ Multiple fleet-api containers match ${install_dir}; remove the stale container before upgrading." >&2 ;;
      3) echo "❌ Could not inspect Docker to migrate missing deployment overlay settings." >&2 ;;
      *) echo "❌ No existing fleet-api container matches ${install_dir}; add explicit ENABLE_BETA_ALERTS, ENABLE_SYSTEM_MONITORING, and ENABLE_TRACING values to ${env_file} before upgrading." >&2 ;;
    esac
    return 1
  fi

  local config_files
  if ! config_files=$(${privilege[@]+"${privilege[@]}"} docker inspect \
    --format '{{with index .Config.Labels "com.docker.compose.project.config_files"}}{{.}}{{end}}' \
    "$container_id" 2>/dev/null); then
    echo "❌ Could not inspect the old fleet-api Compose config-files label." >&2
    return 1
  fi
  [ -n "$config_files" ] || {
    echo "❌ The old fleet-api container has no Compose config-files label; set all ENABLE_* overlay values explicitly in ${env_file} before upgrading." >&2
    return 1
  }

  local deployment_path
  deployment_path=$(cd "${install_dir%/}/${DEPLOYMENT_DIR}" 2>/dev/null && pwd -P) || return 1
  local base_file="${deployment_path}/docker-compose.yaml"
  local alerts_file="${deployment_path}/docker-compose.alerts.yaml"
  local system_file="${deployment_path}/docker-compose.system-monitoring.yaml"
  local tracing_file="${deployment_path}/docker-compose.tracing.yaml"
  if ! compose_config_files_has "$config_files" "$base_file"; then
    echo "❌ The old fleet-api Compose config-files label does not belong to ${deployment_path}." >&2
    return 1
  fi

  local inferred_beta=false inferred_system=false inferred_tracing=false
  if compose_config_files_has "$config_files" "$alerts_file"; then inferred_beta=true; fi
  if compose_config_files_has "$config_files" "$system_file"; then inferred_system=true; fi
  if compose_config_files_has "$config_files" "$tracing_file"; then inferred_tracing=true; fi

  local effective_beta="$beta_state" effective_system="$system_state"
  [ "$effective_beta" != "missing" ] || effective_beta="$inferred_beta"
  [ "$effective_system" != "missing" ] || effective_system="$inferred_system"
  if [ "$effective_system" = "true" ] && [ "$effective_beta" != "true" ]; then
    echo "❌ Existing overlay state enables system monitoring without beta alerts; correct the ENABLE_* values in ${env_file} before upgrading." >&2
    return 1
  fi

  [ "$beta_state" != "missing" ] || [ "$inferred_beta" != "true" ] || PREVIOUS_BETA_ALERTS=1
  [ "$system_state" != "missing" ] || [ "$inferred_system" != "true" ] || PREVIOUS_SYSTEM_MONITORING=1
  [ "$tracing_state" != "missing" ] || [ "$inferred_tracing" != "true" ] || PREVIOUS_TRACING=1
}

resolve_selected_install_path() {
  local selected="$1"
  local discovered="${2:-}"
  local canonical_selected canonical_discovered

  [ -n "$selected" ] || {
    echo "❌ Installation path cannot be empty." >&2
    return 1
  }
  # A fresh path has nothing to compare yet and is made absolute after
  # extraction. When Docker or the on-disk fallback found an installation,
  # both paths already exist and can be canonicalized without filesystem
  # mutation or non-portable realpath flags.
  if [ -z "$discovered" ]; then
    printf '%s' "$selected"
    return 0
  fi
  canonical_discovered=$(cd "$discovered" 2>/dev/null && pwd -P) || {
    echo "❌ Discovered installation path cannot be accessed: $discovered" >&2
    return 1
  }
  canonical_selected=$(cd "$selected" 2>/dev/null && pwd -P) || {
    echo "❌ An existing Proto Fleet installation was discovered at ${canonical_discovered}." >&2
    echo "   The selected path ${selected} does not resolve to that installation." >&2
    echo "   Relocation is not supported; use the discovered installation path or uninstall it first." >&2
    return 1
  }
  if [ "$canonical_selected" != "$canonical_discovered" ]; then
    echo "❌ An existing Proto Fleet installation was discovered at ${canonical_discovered}." >&2
    echo "   Installing to ${canonical_selected} would target the same default Compose project from a different directory." >&2
    echo "   Relocation is not supported; use the discovered installation path or uninstall it first." >&2
    return 1
  fi
  printf '%s' "$canonical_selected"
}

# Keep the privileged bootstrap payload inside the private, checksum-verified
# download directory. The deployment tree is operator-controlled after
# extraction, so it must never be the source for files copied into /etc or
# /usr/local and then executed by systemd as root.
extract_updater_bootstrap() {
  local tar_path="$1"
  local target_dir="$2"
  local binary="$target_dir/proto-fleet-updater"
  local unit="$target_dir/proto-fleet-updater.service"

  mkdir -m 0700 "$target_dir" || return 1
  if ! tar --no-same-owner --strip-components=2 -xzf "$tar_path" -C "$target_dir" \
    deployment/updater/proto-fleet-updater \
    deployment/updater/proto-fleet-updater.service; then
    return 1
  fi
  if [ -L "$binary" ] || [ ! -f "$binary" ] || [ ! -x "$binary" ] \
    || [ -L "$unit" ] || [ ! -f "$unit" ]; then
    echo "⚠️  The release host-updater bootstrap payload is invalid." >&2
    return 1
  fi
}

disable_updater_service_with() {
  local privilege=("$@")
  local load_state active_state unit_file_state
  command -v systemctl >/dev/null 2>&1 || return 0

  # A missing unit is already safe. For a known unit, query final state with
  # checked commands so a sudo/systemctl failure cannot masquerade as
  # inactive merely because `is-active` returned nonzero.
  if ! load_state=$(${privilege[@]+"${privilege[@]}"} systemctl show \
      --property=LoadState --value proto-fleet-updater.service 2>/dev/null); then
    echo "❌ Could not inspect the host updater while disabling it." >&2
    UPDATER_CLEANUP_FAILED=1
    return 1
  fi
  if [ "$load_state" = "not-found" ]; then
    return 0
  fi

  ${privilege[@]+"${privilege[@]}"} systemctl disable --now \
    proto-fleet-updater.service >/dev/null 2>&1 || true
  if ! active_state=$(${privilege[@]+"${privilege[@]}"} systemctl show \
      --property=ActiveState --value proto-fleet-updater.service 2>/dev/null) \
    || ! unit_file_state=$(${privilege[@]+"${privilege[@]}"} systemctl show \
      --property=UnitFileState --value proto-fleet-updater.service 2>/dev/null); then
    echo "❌ Could not stop and disable the host updater safely." >&2
    UPDATER_CLEANUP_FAILED=1
    return 1
  fi
  case "$active_state" in
    inactive|failed) ;;
    *)
      echo "❌ Could not stop and disable the host updater safely." >&2
      UPDATER_CLEANUP_FAILED=1
      return 1
      ;;
  esac
  case "$unit_file_state" in
    disabled|masked|masked-runtime|static) ;;
    *)
      echo "❌ Could not stop and disable the host updater safely." >&2
      UPDATER_CLEANUP_FAILED=1
      return 1
      ;;
  esac
  return 0
}

resolve_updater_privilege() {
  UPDATER_PRIVILEGE=()
  UPDATER_PRIVILEGE_AVAILABLE=1
  if [ "$(id -u)" -eq 0 ]; then
    return 0
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    UPDATER_PRIVILEGE_AVAILABLE=0
    return 0
  fi
  if [ "$NON_INTERACTIVE" = "1" ]; then
    UPDATER_PRIVILEGE=(sudo -n)
  else
    UPDATER_PRIVILEGE=(sudo)
  fi
}

# Stop the existing updater before the release tree can be replaced. systemd's
# inactive state proves the supervised process has exited, which also releases
# the updater's lifetime flock. A missing unit needs no privilege or prompt.
prepare_existing_updater_service() {
  if [ "$(uname -s)" != "Linux" ] \
    || ! command -v systemctl >/dev/null 2>&1 \
    || [ ! -d /run/systemd/system ]; then
    return 0
  fi

  local load_state
  if ! load_state=$(systemctl show --property=LoadState --value \
      proto-fleet-updater.service 2>/dev/null); then
    echo "❌ Could not inspect the existing host updater before installation." >&2
    UPDATER_CLEANUP_FAILED=1
    return 1
  fi
  if [ "$load_state" = "not-found" ]; then
    return 0
  fi
  if [ "$UPDATER_PRIVILEGE_AVAILABLE" != "1" ]; then
    echo "❌ The existing host updater must be stopped, but sudo is unavailable." >&2
    UPDATER_CLEANUP_FAILED=1
    return 1
  fi
  disable_updater_service_with ${UPDATER_PRIVILEGE[@]+"${UPDATER_PRIVILEGE[@]}"}
}

# Match the Docker CLI environment the system service is guaranteed to use.
# The unit also unsets these variables, so neither an invoking root shell nor
# the systemd manager can redirect upgrades to a different daemon.
service_docker_id_with() {
  local privilege=("$@")
  (
    unset DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS DOCKER_TLS_VERIFY DOCKER_CERT_PATH
    ${privilege[@]+"${privilege[@]}"} docker info --format '{{.ID}}' 2>/dev/null || true
  )
}

# END INSTALLER TESTABLE HELPERS

# Function to extract files to the installation directory and cd to it
extract_and_cd() {
  local tar_path="$1"
  local target_dir="$2"
  local env_file="${target_dir}/${DEPLOYMENT_DIR}/server/influx_config/.env"
  
  echo "📦 Extracting to ${target_dir}..."
  
  # Create target directory if it doesn't exist
  mkdir -p "$target_dir"
  
  # Check if we need to preserve existing .env file
  # Never preserve the release builder's numeric UID/GID. Files should belong
  # to the invoking install administrator (or root for a root-run install),
  # matching the updater's root-plus-one-admin ownership trust model.
  if [ -f "$env_file" ]; then
    echo "📦 Preserving existing $env_file file"
    tar --no-same-owner -xzvf "$tar_path" -C "$target_dir" --exclude="${DEPLOYMENT_DIR}/server/influx_config/.env"
  else
    tar --no-same-owner -xzvf "$tar_path" -C "$target_dir"
  fi
  
  # Clean up the tarball
  rm "$tar_path"
  
  # Change to the deployment directory
  cd "${target_dir}/${DEPLOYMENT_DIR}"
  echo "📍 Working in $(pwd)"
}

usage() {
  cat <<EOF
Usage: install.sh [options] [VERSION]

If you omit VERSION or pass "latest", installs the latest GitHub release.
Pass "nightly" to install the latest successful nightly prerelease.
Options:
  --install-dir PATH       Use PATH without prompting.
  --non-interactive        Fail instead of prompting; for an existing install
                           with a complete deployment .env.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
  install.sh nightly
  install.sh nightly-20260424-68712dfabc12
EOF
  exit 1
}

resolve_latest_version() {
  local latest_release_url effective_url curl_stderr

  latest_release_url="https://github.com/block/proto-fleet/releases/latest"
  echo "🛰  Determining latest version from ${latest_release_url}" >&2

  curl_stderr=$(mktemp)
  if ! effective_url=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "${latest_release_url}" 2>"${curl_stderr}"); then
    echo "❌ Failed to query GitHub Releases." >&2
    echo "   URL: ${latest_release_url}" >&2
    echo "   curl error: $(cat "${curl_stderr}")" >&2
    rm -f "${curl_stderr}"
    exit 1
  fi
  rm -f "${curl_stderr}"

  if [[ "${effective_url}" =~ /releases/tag/([^/?#]+)/?$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return 0
  fi

  echo "❌ Failed to determine the latest version from GitHub Releases." >&2
  echo "   Resolved URL: ${effective_url}" >&2
  exit 1
}

resolve_latest_nightly_version() {
  local nightly_channel_url nightly_version curl_stderr

  nightly_channel_url="https://raw.githubusercontent.com/block/proto-fleet/nightly-channel/latest.txt"
  echo "🛰  Determining latest nightly version from ${nightly_channel_url}" >&2

  curl_stderr=$(mktemp)
  if ! nightly_version=$(curl -fsSL "${nightly_channel_url}" 2>"${curl_stderr}"); then
    echo "❌ Failed to query the nightly channel pointer." >&2
    echo "   URL: ${nightly_channel_url}" >&2
    echo "   curl error: $(cat "${curl_stderr}")" >&2
    rm -f "${curl_stderr}"
    exit 1
  fi
  rm -f "${curl_stderr}"

  nightly_version=$(printf '%s' "${nightly_version}" | tr -d '[:space:]')
  if [[ ! "${nightly_version}" =~ ^nightly-[0-9]{8}-[0-9a-f]{12}$ ]]; then
    echo "❌ Nightly channel pointer returned an invalid version: ${nightly_version}" >&2
    exit 1
  fi

  echo "${nightly_version}"
}

validate_release_version() {
  local version="$1"
  if [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z]+)*$ ]] \
    || [[ "$version" =~ ^nightly-[0-9]{8}-[0-9a-f]{12}$ ]]; then
    return 0
  fi
  echo "❌ Invalid Proto Fleet release version: $version" >&2
  exit 1
}

check_page_size() {
  local page_size=$(getconf PAGE_SIZE)
  local os_type=$(uname -s)
  
  if [ "$os_type" != "Darwin" ] && [ "$page_size" -ne 4096 ]; then
    echo "❌ Error: Your system page size is $page_size bytes, but 4096 bytes (4K) is required."
    echo "This is common on Raspberry Pi devices with 16K pages and can cause issues with installation."
    echo ""
    echo "To fix this issue on Raspberry Pi:"
    echo "1. Run: sudo nano /boot/firmware/config.txt"
    echo "2. Add this line at the top: kernel=kernel8.img"
    echo "3. Save and exit (CTRL+X, then Y, then Enter)"
    echo "4. Reboot: sudo reboot"
    echo "5. Verify with: getconf PAGESIZE (should show 4096)"
    echo "6. Run this installation script again"
    if [ "$NON_INTERACTIVE" = "1" ]; then
      echo "Installation aborted because non-interactive mode cannot accept this unsafe host prerequisite."
      exit 1
    fi
    read -p "Do you want to continue anyway? (y/N): " continue_anyway < /dev/tty
      
    if [[ ! "$continue_anyway" =~ ^[Yy]$ ]]; then
      echo "Installation aborted."
      exit 1
    fi
      
    echo "Continuing installation with $page_size byte page size..."
  fi
}

NON_INTERACTIVE=0
REQUESTED_INSTALL_DIR=""
REQUESTED_VERSION=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      [ "$#" -ge 2 ] || { echo "Error: --install-dir requires a path." >&2; usage; }
      REQUESTED_INSTALL_DIR="$2"
      shift 2
      ;;
    --non-interactive)
      NON_INTERACTIVE=1
      shift
      ;;
    -h|--help)
      usage
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "Error: unknown option $1" >&2
      usage
      ;;
    *)
      if [ -n "$REQUESTED_VERSION" ]; then
        echo "Error: only one VERSION may be supplied." >&2
        usage
      fi
      REQUESTED_VERSION="$1"
      shift
      ;;
  esac
done

if [ "$#" -gt 0 ]; then
  echo "Error: unexpected arguments: $*" >&2
  usage
fi

check_page_size

GITHUB_RELEASES_URL="https://github.com/block/proto-fleet/releases"

# determine version and tarball name
case "${REQUESTED_VERSION:-latest}" in
  latest)
    VERSION=$(resolve_latest_version)
    echo "🔖 Latest version is ${VERSION}"
    ;;
  nightly)
    VERSION=$(resolve_latest_nightly_version)
    echo "🔖 Latest nightly version is ${VERSION}"
    ;;
  *)
    VERSION="$REQUESTED_VERSION"
    ;;
esac
validate_release_version "$VERSION"

# Detect architecture
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

TAR_NAME="proto-fleet-${VERSION}-${ARCH}.tar.gz"
URL="${GITHUB_RELEASES_URL}/download/${VERSION}/${TAR_NAME}"
DOWNLOAD_DIR=$(mktemp -d /tmp/proto-fleet-install.XXXXXX) || {
  echo "❌ Could not create a private release-download directory." >&2
  exit 1
}
trap 'rm -rf -- "$DOWNLOAD_DIR"' EXIT
chmod 700 "$DOWNLOAD_DIR" || {
  echo "❌ Could not secure the release-download directory." >&2
  exit 1
}
TAR_PATH="${DOWNLOAD_DIR}/${TAR_NAME}"

echo "🛰  Fetching proto-fleet ${VERSION} from ${URL}"
if ! curl -fsSL "${URL}" -o "$TAR_PATH"; then
  echo "❌ Failed to download ${TAR_NAME} from GitHub Releases — does that release asset exist?"
  usage
fi

CHECKSUM_PATH="${DOWNLOAD_DIR}/${TAR_NAME}.sha256"
echo "🔐 Fetching and verifying ${TAR_NAME}.sha256"
if ! curl -fsSL "${URL}.sha256" -o "${CHECKSUM_PATH}"; then
  echo "❌ This release does not provide the required SHA-256 integrity file."
  echo "   For a legacy release, run the installer published with that exact tag:"
  echo "   bash <(curl -fsSL ${GITHUB_RELEASES_URL}/download/${VERSION}/install.sh) ${VERSION}"
  exit 1
fi
checksum_fields=$(wc -w < "${CHECKSUM_PATH}" | tr -d '[:space:]')
checksum_name=$(awk '{print $2}' "${CHECKSUM_PATH}" | sed 's/^\*//')
if [ "$checksum_fields" != "2" ] || [ "$checksum_name" != "$TAR_NAME" ]; then
  echo "❌ Invalid checksum file for ${TAR_NAME}."
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$DOWNLOAD_DIR" && sha256sum -c "${TAR_NAME}.sha256")
elif command -v shasum >/dev/null 2>&1; then
  expected_checksum=$(awk '{print $1}' "${CHECKSUM_PATH}")
  actual_checksum=$(shasum -a 256 "$TAR_PATH" | awk '{print $1}')
  [ "$expected_checksum" = "$actual_checksum" ] || { echo "❌ Release checksum verification failed."; exit 1; }
else
  echo "❌ Neither sha256sum nor shasum is available to verify the release."
  exit 1
fi
rm -f "${CHECKSUM_PATH}"

UPDATER_BOOTSTRAP_DIR="${DOWNLOAD_DIR}/updater-bootstrap"
if ! extract_updater_bootstrap "$TAR_PATH" "$UPDATER_BOOTSTRAP_DIR"; then
  echo "ℹ️  This release has no valid host-updater bootstrap payload; one-click upgrades will remain disabled."
  UPDATER_BOOTSTRAP_DIR=""
fi

# Function to determine default installation directory based on OS.
# When invoked under sudo on Linux, prefer the invoking user's home over
# /root — fleet is normally installed under the user account, and falling
# back to /root/proto-fleet would silently miss the user's on-disk install.
get_default_install_dir() {
  local os_type
  os_type=$(uname -s)

  if [ "$os_type" = "Darwin" ]; then
    echo "$HOME/Applications/ProtoFleet"
    return
  fi

  if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    # `|| true` neutralizes set -e / pipefail so a missing getent or failed
    # NSS lookup falls through to $HOME instead of aborting.
    local sudo_home
    sudo_home=$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6 || true)
    if [ -n "$sudo_home" ]; then
      echo "$sudo_home/proto-fleet"
      return
    fi
    # SUDO_USER set but resolution failed — warn so the operator knows the
    # default below is /root, not their home. Direct >&2 because this
    # function's stdout is captured by the `$(...)` caller.
    echo "⚠️  SUDO_USER='$SUDO_USER' set but home lookup returned empty;" >&2
    echo "    default install dir will fall back to \$HOME ($HOME/proto-fleet)." >&2
  fi

  echo "$HOME/proto-fleet"
}

echo "🔍 Checking for previous ProtoFleet installations via Docker..."
# An explicit target controls the destination, not the Docker ownership
# boundary. Always probe both daemon contexts before replacing files so
# --install-dir cannot bypass the root-daemon mismatch guard.
detect_previous_install || true
DEFAULT_INSTALL_DIR=$(get_default_install_dir)

# If the existing containers were only visible via `sudo docker`, this script
# is running as a user who can't manage them. Bail out loudly rather than
# silently extracting on top of an install we can't control — continuing
# would orphan the root-owned containers and likely leave the user with two
# competing stacks. (Process substitution + sudo is a footgun, so tell them
# the pipe form that actually works.)
# Shell-escape VERSION so the suggested copy-paste commands below stay safe
# even when the user-supplied version arg contains spaces or metachars.
QUOTED_VERSION=$(printf '%q' "${VERSION}")

if [ "${PREVIOUS_INSTALL_NEEDS_SUDO:-0}" = "1" ] && [ "$(id -u)" -ne 0 ]; then
  echo "❌ Existing fleet containers were detected, but only via sudo."
  echo "   They are managed by the root Docker daemon, and this script is running as $(id -un)."
  echo "   Re-run the installer as root so the upgrade targets the same daemon:"
  echo ""
  echo "     curl -fsSL https://fleet.proto.xyz/install.sh | sudo bash -s -- ${QUOTED_VERSION}"
  echo ""
  echo "   Or, if your user account is already in the 'docker' group but the current"
  echo "   shell hasn't picked it up yet, log out and back in (or run 'newgrp docker')"
  echo "   and re-run the original install command without sudo."
  echo ""
  echo "   (The 'sudo bash <(curl ...)' form does not work — process substitution"
  echo "   opens an FD that sudo cannot access.)"
  exit 1
fi

# Marker check: docker-compose.yaml ships in every install tarball, so its
# presence inside a 'deployment/' directory is a strong positive signal that
# this really is a ProtoFleet install (and not some unrelated 'deployment/'
# tree the user happened to create).
if [ -z "${PREVIOUS_INSTALL_DIR:-}" ] \
  && [ -d "${DEFAULT_INSTALL_DIR}/${DEPLOYMENT_DIR}" ] \
  && [ -f "${DEFAULT_INSTALL_DIR}/${DEPLOYMENT_DIR}/docker-compose.yaml" ]; then
  PREVIOUS_INSTALL_DIR="$DEFAULT_INSTALL_DIR"
  echo "📁 No running fleet containers, but found existing install on disk at: ${PREVIOUS_INSTALL_DIR}"
fi

if [ -n "$REQUESTED_INSTALL_DIR" ]; then
  SUGGESTED_DIR="$REQUESTED_INSTALL_DIR"
  echo "📌 Using requested installation location: ${SUGGESTED_DIR}"
elif [ -n "${PREVIOUS_INSTALL_DIR:-}" ]; then
  SUGGESTED_DIR="$PREVIOUS_INSTALL_DIR"
  echo "📌 Found previous installation at: ${SUGGESTED_DIR}"
else
  SUGGESTED_DIR="$DEFAULT_INSTALL_DIR"
  echo "📌 No previous installation detected."
  echo "   Suggested installation location: ${SUGGESTED_DIR}"
fi

# When sudo would have prompted for a password, we couldn't probe the root
# daemon at all — a parallel root-managed install would otherwise stay
# invisible. Print this whenever the sudo probe was blocked, even if the
# on-disk fallback found an unprivileged install (the two installs could
# coexist on the same host).
if [ "${PREVIOUS_INSTALL_SUDO_BLOCKED:-0}" = "1" ]; then
  echo ""
  echo "   (Note: sudo required a password, so we couldn't check whether a"
  echo "    root-managed fleet install also exists. If one might, re-run as root:"
  echo "      curl -fsSL https://fleet.proto.xyz/install.sh | sudo bash -s -- ${QUOTED_VERSION})"
fi

if [ -n "$REQUESTED_INSTALL_DIR" ]; then
  INSTALL_DIR="$REQUESTED_INSTALL_DIR"
elif [ "$NON_INTERACTIVE" = "1" ]; then
  if [ -z "${PREVIOUS_INSTALL_DIR:-}" ]; then
    echo "❌ A fresh non-interactive install requires --install-dir." >&2
    exit 1
  fi
  INSTALL_DIR="$SUGGESTED_DIR"
else
  # Read from /dev/tty so prompts work under `curl ... | sudo bash -s --`.
  read -p "   Use this location? (Y/n): " use_suggested < /dev/tty
  if [[ "$use_suggested" =~ ^[Nn]$ ]]; then
    read -p "   Enter installation directory [${DEFAULT_INSTALL_DIR}]: " custom_dir < /dev/tty
    INSTALL_DIR="${custom_dir:-$DEFAULT_INSTALL_DIR}"
  else
    INSTALL_DIR="$SUGGESTED_DIR"
  fi
fi

case "$INSTALL_DIR" in
  *$'\n'*|*$'\r'*)
    echo "❌ Installation paths cannot contain newline characters." >&2
    exit 1
    ;;
esac

if ! INSTALL_DIR=$(resolve_selected_install_path "$INSTALL_DIR" "${PREVIOUS_INSTALL_DIR:-}"); then
  exit 1
fi
if [ "$NON_INTERACTIVE" = "1" ] && [ ! -f "${INSTALL_DIR}/${DEPLOYMENT_DIR}/.env" ]; then
  echo "❌ Non-interactive mode requires an existing configured deployment at ${INSTALL_DIR}/${DEPLOYMENT_DIR}." >&2
  exit 1
fi

echo "📌 Will install to: ${INSTALL_DIR}"

# Releases before one-click support treated optional Compose overlays as
# process-only flags. Capture only missing values while the validated old
# stack still exists, and abort before extraction if its state is ambiguous.
EXISTING_DEPLOYMENT=0
if [ -f "${INSTALL_DIR%/}/${DEPLOYMENT_DIR}/docker-compose.yaml" ]; then
  EXISTING_DEPLOYMENT=1
fi
CAPTURE_PRIVILEGE=()
if [ "${PREVIOUS_INSTALL_NEEDS_SUDO:-0}" = "1" ]; then
  CAPTURE_PRIVILEGE=(sudo -n)
fi
if ! capture_previous_run_options "$INSTALL_DIR" "$EXISTING_DEPLOYMENT" \
  ${CAPTURE_PRIVILEGE[@]+"${CAPTURE_PRIVILEGE[@]}"}; then
  echo "❌ Existing deployment options could not be migrated safely; no files were replaced." >&2
  exit 1
fi

# A manual install and the privileged updater both replace the deployment tree.
# Drain and disable the service before extraction so those writers can never
# overlap. Any failure here leaves the existing Fleet containers untouched.
resolve_updater_privilege
if ! prepare_existing_updater_service; then
  echo "❌ Existing host updater could not be stopped safely; no files were replaced." >&2
  exit 1
fi

extract_and_cd "$TAR_PATH" "$INSTALL_DIR"
# The system service starts with / as its working directory, so persist the
# canonical absolute install root even when the interactive installer was
# given a relative path.
INSTALL_DIR=$(cd .. && pwd -P)

# Validate plugin binaries exist
echo "🔌 Validating plugin binaries..."
PLUGIN_DIR="server"
REQUIRED_PLUGINS=("proto-plugin" "antminer-plugin" "asicrs-plugin")
MISSING_PLUGINS=()

for plugin in "${REQUIRED_PLUGINS[@]}"; do
  if [ ! -f "${PLUGIN_DIR}/${plugin}" ]; then
    MISSING_PLUGINS+=("$plugin")
  fi
done

if [ ${#MISSING_PLUGINS[@]} -ne 0 ]; then
  echo "❌ Error: Missing plugin binaries:"
  printf '   - %s\n' "${MISSING_PLUGINS[@]}"
  echo "The installation package may be incomplete. Please contact support."
  exit 1
fi

# Set executable permissions on validated plugin binaries
for plugin in "${REQUIRED_PLUGINS[@]}"; do
  chmod +x "${PLUGIN_DIR}/${plugin}"
done
echo "✅ Plugin binaries validated"

install_updater_service() {
  if [ "$(uname -s)" != "Linux" ]; then
    return 1
  fi
  if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    echo "ℹ️  systemd is unavailable; keeping copy-command upgrades on this host."
    return 1
  fi
  case "$INSTALL_DIR" in
    /usr|/usr/*|/boot|/boot/*|/efi|/efi/*|/etc|/etc/*)
      echo "ℹ️  The hardened updater cannot write to ${INSTALL_DIR}; keeping copy-command upgrades."
      return 1
      ;;
  esac
  if [ -z "$UPDATER_BOOTSTRAP_DIR" ] \
    || [ -L "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater" ] \
    || [ ! -x "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater" ] \
    || [ -L "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater.service" ] \
    || [ ! -f "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater.service" ]; then
    echo "⚠️  This release does not contain the host updater payload."
    return 1
  fi

  local privilege=()
  if [ "$UPDATER_PRIVILEGE_AVAILABLE" != "1" ]; then
    echo "⚠️  sudo is unavailable; one-click upgrades cannot be enabled."
    return 1
  fi
  if (( ${#UPDATER_PRIVILEGE[@]} )); then
    privilege=("${UPDATER_PRIVILEGE[@]}")
  fi

  # The service runs with Docker selector variables removed. Only enable it
  # when that exact environment sees the same daemon as the current install;
  # rootless/custom-daemon support needs a future service adapter.
  local current_docker_id service_docker_id attempt
  current_docker_id=$(docker info --format '{{.ID}}' 2>/dev/null || true)
  if [ -z "$current_docker_id" ] && command -v docker >/dev/null 2>&1; then
    # Docker is an installer prerequisite, but tolerate an installed rootful
    # daemon that is merely stopped so a single manual install still enables
    # one-click upgrades. A rootless DOCKER_HOST remains distinct and will
    # fail the daemon-identity comparison below.
    ${privilege[@]+"${privilege[@]}"} systemctl start docker.service >/dev/null 2>&1 || true
    for attempt in {1..20}; do
      current_docker_id=$(docker info --format '{{.ID}}' 2>/dev/null || true)
      [ -z "$current_docker_id" ] || break
      sleep 0.25
    done
  fi
  service_docker_id=$(service_docker_id_with ${privilege[@]+"${privilege[@]}"})
  if [ -z "$current_docker_id" ] || [ "$current_docker_id" != "$service_docker_id" ]; then
    echo "ℹ️  The host updater service does not manage this installation's Docker daemon; keeping copy-command upgrades."
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi

  local env_temp escaped_install escaped_download
  if ! env_temp=$(mktemp); then
    echo "⚠️  Could not create the host updater environment file." >&2
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi
  escaped_install=${INSTALL_DIR//\\/\\\\}
  escaped_install=${escaped_install//\"/\\\"}
  escaped_download=${GITHUB_RELEASES_URL//\\/\\\\}
  escaped_download=${escaped_download//\"/\\\"}
  if ! {
    printf 'PROTO_FLEET_INSTALL_ROOT="%s"\n' "$escaped_install"
    printf 'PROTO_FLEET_DOWNLOAD_BASE_URL="%s/download"\n' "$escaped_download"
    printf 'PROTO_FLEET_UPDATER_STATE_DIR="/var/lib/proto-fleet-updater"\n'
    printf 'PROTO_FLEET_UPDATER_SOCKET_PATH="/run/proto-fleet-updater/updater.sock"\n'
    printf 'PROTO_FLEET_UPDATER_BINARY_PATH="/usr/local/libexec/proto-fleet/proto-fleet-updater"\n'
  } > "$env_temp"; then
    echo "⚠️  Could not write the host updater environment file." >&2
    rm -f "$env_temp"
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi

  if ! ${privilege[@]+"${privilege[@]}"} install -d -m 0755 /usr/local/libexec/proto-fleet /etc/proto-fleet; then
    rm -f "$env_temp"
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi
  if ! ${privilege[@]+"${privilege[@]}"} install -m 0755 "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater" /usr/local/libexec/proto-fleet/proto-fleet-updater \
    || ! ${privilege[@]+"${privilege[@]}"} install -m 0644 "$UPDATER_BOOTSTRAP_DIR/proto-fleet-updater.service" /etc/systemd/system/proto-fleet-updater.service \
    || ! ${privilege[@]+"${privilege[@]}"} install -m 0600 "$env_temp" /etc/proto-fleet/updater.env; then
    rm -f "$env_temp"
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi
  rm -f "$env_temp"
  if ! ${privilege[@]+"${privilege[@]}"} systemctl daemon-reload \
    || ! ${privilege[@]+"${privilege[@]}"} systemctl enable proto-fleet-updater.service; then
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi
  # The old updater was drained before extraction. Start the newly installed
  # binary only after its unit and environment have been replaced atomically.
  if ! ${privilege[@]+"${privilege[@]}"} systemctl restart proto-fleet-updater.service; then
    disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
    return 1
  fi

  # Type=simple reports the restart as soon as the process is spawned. Wait
  # for the updater to finish validating its privileged configuration and
  # bind the secured API socket before exposing that socket to fleet-api.
  local ready_deadline=$((SECONDS + 60))
  while [ "$SECONDS" -lt "$ready_deadline" ]; do
    if ${privilege[@]+"${privilege[@]}"} curl -fsS --max-time 1 \
      --unix-socket /run/proto-fleet-updater/updater.sock \
      http://localhost/v1/status >/dev/null 2>&1 \
      && ${privilege[@]+"${privilege[@]}"} systemctl is-active --quiet proto-fleet-updater.service; then
      return 0
    fi
    sleep 1
  done
  echo "⚠️  Host updater did not become ready; keeping copy-command upgrades." >&2
  disable_updater_service_with ${privilege[@]+"${privilege[@]}"} || true
  return 1
}

RUN_FLEET_ARGS=()
if [ "$PREVIOUS_BETA_ALERTS" = "1" ] || [ "$PREVIOUS_SYSTEM_MONITORING" = "1" ]; then
  RUN_FLEET_ARGS+=(--enable-beta-alerts)
fi
if [ "$PREVIOUS_SYSTEM_MONITORING" = "1" ]; then
  RUN_FLEET_ARGS+=(--enable-system-monitoring)
fi
if [ "$PREVIOUS_TRACING" = "1" ]; then
  RUN_FLEET_ARGS+=(--enable-tracing)
fi
if install_updater_service; then
  echo "✅ Host updater installed; one-click upgrades are enabled."
  RUN_FLEET_ARGS+=(--enable-one-click-updates)
else
  # Keep every fallback fail-closed, including returns that happen before the
  # service payload is copied. Unsupported non-systemd hosts have no system
  # updater to reconcile.
  if [ "$(uname -s)" = "Linux" ] \
    && command -v systemctl >/dev/null 2>&1 \
    && [ -d /run/systemd/system ]; then
    disable_updater_service_with ${UPDATER_PRIVILEGE[@]+"${UPDATER_PRIVILEGE[@]}"} || true
  fi
  if [ "$UPDATER_CLEANUP_FAILED" = "1" ]; then
    echo "❌ Installation stopped because the host updater could not be left in a safe state." >&2
    exit 1
  fi
  echo "ℹ️  One-click upgrades are unavailable; the in-product copy command remains usable."
  RUN_FLEET_ARGS+=(--disable-one-click-updates)
fi

echo "🔧 Running deployment script..."
if [ "$NON_INTERACTIVE" = "1" ]; then
  RUN_FLEET_ARGS+=(--non-interactive)
fi
# run-fleet persists the selected overlay before later build/start work. If a
# later deployment step fails, keep the already-verified updater service ready
# so that persisted true state never points fleet-api at a dead socket. A fresh
# installation cannot expose the socket until run-fleet successfully starts
# the overlay; an existing socket-enabled deployment already owned this
# service before the manual retry.
./run-fleet.sh "${RUN_FLEET_ARGS[@]}"
