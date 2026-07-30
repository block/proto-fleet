#!/usr/bin/env bash
set -euo pipefail

DEPLOYMENT_DIR="deployment"

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

# Function to extract files to the installation directory and cd to it
extract_and_cd() {
  local tar_path="$1"
  local target_dir="$2"
  local env_file="${target_dir}/${DEPLOYMENT_DIR}/server/influx_config/.env"
  
  echo "📦 Extracting to ${target_dir}..."
  
  # Create target directory if it doesn't exist
  mkdir -p "$target_dir"
  
  # Check if we need to preserve existing .env file
  if [ -f "$env_file" ]; then
    echo "📦 Preserving existing $env_file file"
    tar -xzvf "$tar_path" -C "$target_dir" --exclude="${DEPLOYMENT_DIR}/server/influx_config/.env"
  else
    tar -xzvf "$tar_path" -C "$target_dir"
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
  --skip-updater-service   Do not install/reconfigure the host updater.
                           Reserved for updater-driven internal workflows.
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
SKIP_UPDATER_SERVICE=0
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
    --skip-updater-service)
      SKIP_UPDATER_SERVICE=1
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

# Detect architecture
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

TAR_NAME="proto-fleet-${VERSION}-${ARCH}.tar.gz"
URL="${GITHUB_RELEASES_URL}/download/${VERSION}/${TAR_NAME}"

# Clean up the downloaded tarball on any exit path. extract_and_cd also rm's
# it on the happy path, but early-exit paths (download retry, sudo-mismatch
# abort, future guards added below) would otherwise leak release-sized files
# into /tmp on every aborted attempt.
trap 'rm -f "/tmp/${TAR_NAME}"' EXIT

echo "🛰  Fetching proto-fleet ${VERSION} from ${URL}"
if ! curl -fsSL "${URL}" -o "/tmp/${TAR_NAME}"; then
  echo "❌ Failed to download ${TAR_NAME} from GitHub Releases — does that release asset exist?"
  usage
fi

CHECKSUM_PATH="/tmp/${TAR_NAME}.sha256"
trap 'rm -f "/tmp/${TAR_NAME}" "${CHECKSUM_PATH}"' EXIT
echo "🔐 Fetching and verifying ${TAR_NAME}.sha256"
if ! curl -fsSL "${URL}.sha256" -o "${CHECKSUM_PATH}"; then
  echo "❌ This release does not provide the required SHA-256 integrity file."
  exit 1
fi
checksum_fields=$(wc -w < "${CHECKSUM_PATH}" | tr -d '[:space:]')
checksum_name=$(awk '{print $2}' "${CHECKSUM_PATH}" | sed 's/^\*//')
if [ "$checksum_fields" != "2" ] || [ "$checksum_name" != "$TAR_NAME" ]; then
  echo "❌ Invalid checksum file for ${TAR_NAME}."
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd /tmp && sha256sum -c "${TAR_NAME}.sha256")
elif command -v shasum >/dev/null 2>&1; then
  expected_checksum=$(awk '{print $1}' "${CHECKSUM_PATH}")
  actual_checksum=$(shasum -a 256 "/tmp/${TAR_NAME}" | awk '{print $1}')
  [ "$expected_checksum" = "$actual_checksum" ] || { echo "❌ Release checksum verification failed."; exit 1; }
else
  echo "❌ Neither sha256sum nor shasum is available to verify the release."
  exit 1
fi
rm -f "${CHECKSUM_PATH}"

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
if [ -z "$REQUESTED_INSTALL_DIR" ]; then
  detect_previous_install || true
else
  PREVIOUS_INSTALL_DIR=""
  PREVIOUS_INSTALL_NEEDS_SUDO=0
  PREVIOUS_INSTALL_SUDO_BLOCKED=0
fi
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

if [ "$NON_INTERACTIVE" = "1" ] && [ ! -f "${INSTALL_DIR}/${DEPLOYMENT_DIR}/.env" ]; then
  echo "❌ Non-interactive mode requires an existing configured deployment at ${INSTALL_DIR}/${DEPLOYMENT_DIR}." >&2
  exit 1
fi
case "$INSTALL_DIR" in
  *$'\n'*|*$'\r'*)
    echo "❌ Installation paths cannot contain newline characters." >&2
    exit 1
    ;;
esac

echo "📌 Will install to: ${INSTALL_DIR}"

# Releases before one-click support treated optional Compose overlays as
# process-only flags. Capture the effective fleet-api environment while the
# old stack is still running so the bootstrap upgrade can persist those
# choices into the new deployment's .env.
PREVIOUS_BETA_ALERTS=0
PREVIOUS_SYSTEM_MONITORING=0
PREVIOUS_TRACING=0
capture_previous_run_options() {
  local container_id container_env
  container_id=$(docker ps -a \
    --filter "name=${DEPLOYMENT_DIR}-fleet-api" \
    --filter "name=${DEPLOYMENT_DIR}_fleet-api" \
    --format "{{.ID}}" 2>/dev/null | head -n 1 || true)
  [ -n "$container_id" ] || return 0
  container_env=$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container_id" 2>/dev/null || true)
  printf '%s\n' "$container_env" | grep -Eqi '^FLEET_ALERTS_ENABLED=true$' && PREVIOUS_BETA_ALERTS=1
  printf '%s\n' "$container_env" | grep -Eqi '^FLEET_SYSTEM_MONITORING_ENABLED=true$' && PREVIOUS_SYSTEM_MONITORING=1
  printf '%s\n' "$container_env" | grep -Eqi '^FLEET_TELEMETRY_ENABLED=true$' && PREVIOUS_TRACING=1
}
capture_previous_run_options

extract_and_cd "/tmp/${TAR_NAME}" "$INSTALL_DIR"
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
  if [ "$SKIP_UPDATER_SERVICE" = "1" ] || [ "$(uname -s)" != "Linux" ]; then
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
  if [ ! -x "updater/proto-fleet-updater" ] || [ ! -f "updater/proto-fleet-updater.service" ]; then
    echo "⚠️  This release does not contain the host updater payload."
    return 1
  fi

  local privilege=()
  if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null 2>&1 || {
      echo "⚠️  sudo is unavailable; one-click upgrades cannot be enabled."
      return 1
    }
    if [ "$NON_INTERACTIVE" = "1" ]; then
      privilege=(sudo -n)
    else
      privilege=(sudo)
    fi
  fi

  # The service runs as root. Only enable it when root sees the same Docker
  # daemon as the current installation; rootless Docker needs a future
  # user-service adapter and must keep the manual fallback.
  local current_docker_id root_docker_id
  current_docker_id=$(docker info --format '{{.ID}}' 2>/dev/null || true)
  root_docker_id=$(${privilege[@]+"${privilege[@]}"} docker info --format '{{.ID}}' 2>/dev/null || true)
  if [ -z "$current_docker_id" ] || [ "$current_docker_id" != "$root_docker_id" ]; then
    echo "ℹ️  Root does not manage this installation's Docker daemon; keeping copy-command upgrades."
    return 1
  fi

  local env_temp escaped_install escaped_download
  env_temp=$(mktemp)
  escaped_install=${INSTALL_DIR//\\/\\\\}
  escaped_install=${escaped_install//\"/\\\"}
  escaped_download=${GITHUB_RELEASES_URL//\\/\\\\}
  escaped_download=${escaped_download//\"/\\\"}
  {
    printf 'PROTO_FLEET_INSTALL_ROOT="%s"\n' "$escaped_install"
    printf 'PROTO_FLEET_DOWNLOAD_BASE_URL="%s/download"\n' "$escaped_download"
    printf 'PROTO_FLEET_UPDATER_STATE_DIR="/var/lib/proto-fleet-updater"\n'
    printf 'PROTO_FLEET_UPDATER_SOCKET_PATH="/run/proto-fleet-updater/updater.sock"\n'
    printf 'PROTO_FLEET_UPDATER_BINARY_PATH="/usr/local/libexec/proto-fleet/proto-fleet-updater"\n'
  } > "$env_temp"

  if ! ${privilege[@]+"${privilege[@]}"} install -d -m 0755 /usr/local/libexec/proto-fleet /etc/proto-fleet; then
    rm -f "$env_temp"
    return 1
  fi
  if ! ${privilege[@]+"${privilege[@]}"} install -m 0755 updater/proto-fleet-updater /usr/local/libexec/proto-fleet/proto-fleet-updater \
    || ! ${privilege[@]+"${privilege[@]}"} install -m 0644 updater/proto-fleet-updater.service /etc/systemd/system/proto-fleet-updater.service \
    || ! ${privilege[@]+"${privilege[@]}"} install -m 0600 "$env_temp" /etc/proto-fleet/updater.env; then
    rm -f "$env_temp"
    return 1
  fi
  rm -f "$env_temp"
  ${privilege[@]+"${privilege[@]}"} systemctl daemon-reload
  ${privilege[@]+"${privilege[@]}"} systemctl enable proto-fleet-updater.service
  # A manual install can safely restart an already-running updater; the
  # updater-driven path never invokes install.sh and refreshes its on-disk
  # binary for the next service start instead.
  ${privilege[@]+"${privilege[@]}"} systemctl restart proto-fleet-updater.service
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
  echo "ℹ️  One-click upgrades are unavailable; the in-product copy command remains usable."
fi

echo "🔧 Running deployment script..."
if [ "$NON_INTERACTIVE" = "1" ]; then
  RUN_FLEET_ARGS+=(--non-interactive)
fi
./run-fleet.sh "${RUN_FLEET_ARGS[@]}"
