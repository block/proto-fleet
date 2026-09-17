#!/usr/bin/env bash
# Local fixtures only: exercise the standalone installer helpers and the exact
# asset staging path used by release and nightly workflows.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
sed '/^# END INSTALLER TESTABLE HELPERS$/q' "$root/deployment-files/install.sh" > "$scratch/helpers.sh"
source "$scratch/helpers.sh"

for repository in block/proto-fleet example-owner/fleet_fork.test; do
  validate_release_repository "$repository"
done
for repository in '' owner /repo owner/ 'owner/../repo' 'owner/repo/extra' \
  'owner/repo?token=secret' 'owner/repo#fragment' 'owner/repo;id' 'owner/repo$(id)' \
  'https://github.com/owner/repo' ' owner/repo' 'owner-/repo' 'owner/repo..name'; do
  if validate_release_repository "$repository"; then
    echo "accepted invalid repository: $repository" >&2; exit 1
  fi
done

PROTO_FLEET_RELEASE_REPOSITORY=other-owner/fleet
resolve_install_release_repository "$scratch/fresh"
[[ "$RELEASE_REPOSITORY" == block/proto-fleet ]]
PACKAGED_RELEASE_REPOSITORY=example-owner/fleet-fork
resolve_install_release_repository "$scratch/fresh"
[[ "$GITHUB_RELEASES_URL" == https://github.com/example-owner/fleet-fork/releases ]]

# Fresh non-interactive installs may have a preseeded .env without Compose or
# release metadata. Validate it in source preflight, before any release request.
mkdir -p "$scratch/preseeded/deployment"
for repository in block/proto-fleet example-owner/fleet-fork; do
  PACKAGED_RELEASE_REPOSITORY="$repository"
  printf 'DB_PASSWORD=preseeded\n' > "$scratch/preseeded/deployment/.env"
  resolve_install_release_repository "$scratch/preseeded"
  [[ "$RELEASE_REPOSITORY" == "$repository" ]]
  printf 'PROTO_FLEET_RELEASE_REPOSITORY="%s"\n' "$repository" > "$scratch/preseeded/deployment/.env"
  resolve_install_release_repository "$scratch/preseeded"
  [[ "$RELEASE_REPOSITORY" == "$repository" ]]
  for contents in \
    'PROTO_FLEET_RELEASE_REPOSITORY=other-owner/fleet' \
    'PROTO_FLEET_RELEASE_REPOSITORY=' \
    'PROTO_FLEET_RELEASE_REPOSITORY missing-delimiter' \
    "$(printf 'PROTO_FLEET_RELEASE_REPOSITORY=%s\nPROTO_FLEET_RELEASE_REPOSITORY=%s' "$repository" "$repository")"; do
    printf '%s\n' "$contents" > "$scratch/preseeded/deployment/.env"
    if resolve_install_release_repository "$scratch/preseeded"; then
      echo 'invalid preseeded repository configuration was accepted' >&2; exit 1
    fi
  done
done

mkdir -p "$scratch/installed/deployment"
touch "$scratch/installed/deployment/docker-compose.yaml"
if resolve_install_release_repository "$scratch/installed"; then
  echo 'fork installer accepted an official legacy installation' >&2; exit 1
fi
PACKAGED_RELEASE_REPOSITORY=block/proto-fleet
resolve_install_release_repository "$scratch/installed"
[[ "$RELEASE_REPOSITORY" == block/proto-fleet ]]
printf 'version: v1.0.0\nrelease_repository: example-owner/fleet-fork\n' > "$scratch/installed/deployment/version.txt"
printf 'PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\n' > "$scratch/installed/deployment/.env"
PACKAGED_RELEASE_REPOSITORY=block/proto-fleet
if resolve_install_release_repository "$scratch/installed"; then
  echo 'installed metadata overrode the packaged identity' >&2; exit 1
fi
PACKAGED_RELEASE_REPOSITORY=example-owner/fleet-fork
resolve_install_release_repository "$scratch/installed"
[[ "$RELEASE_REPOSITORY" == example-owner/fleet-fork ]]
printf 'PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\n' > "$scratch/installed/deployment/.env"
if resolve_install_release_repository "$scratch/installed"; then
  echo 'conflicting configuration was accepted' >&2; exit 1
fi
if release_repository_from_metadata $'release_repository: example/fleet\nrelease_repository: block/proto-fleet'; then
  echo 'duplicate metadata was accepted' >&2; exit 1
fi
printf 'PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\nPROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\n' > "$scratch/installed/deployment/.env"
if resolve_install_release_repository "$scratch/installed"; then
  echo 'duplicate configuration was accepted' >&2; exit 1
fi
for assignment in 'PROTO_FLEET_RELEASE_REPOSITORY' 'export PROTO_FLEET_RELEASE_REPOSITORY=example/fleet'; do
  if release_source_assignment "$assignment" PROTO_FLEET_RELEASE_REPOSITORY; then
    echo 'malformed updater configuration was accepted' >&2; exit 1
  fi
done

bash "$root/deployment-files/scripts/package-release-installers.sh" example-owner/fleet-fork "$scratch/assets"
for installer in install.sh install-fleet-node.sh; do
  grep -Fxq 'PACKAGED_RELEASE_REPOSITORY="example-owner/fleet-fork"' "$scratch/assets/$installer"
  bash -n "$scratch/assets/$installer"
  if bash "$scratch/assets/$installer" --repo example-owner/fleet-fork > "$scratch/repo-option.out" 2>&1; then
    echo "$installer accepted a repository override" >&2; exit 1
  fi
  grep -Fq "Usage:" "$scratch/repo-option.out"
done
if bash "$root/deployment-files/scripts/package-release-installers.sh" 'example/fleet;id' "$scratch/bad-assets"; then
  echo 'packager accepted shell syntax' >&2; exit 1
fi
[[ ! -e "$scratch/bad-assets" ]]

# Load only channel-resolution functions, then stub curl. Failed alternate
# requests must terminate without trying an official URL.
sed -n '/^resolve_latest_version() {/,/^validate_release_version() {/p' "$root/deployment-files/install.sh" | sed '$d' > "$scratch/channels.sh"
source "$scratch/channels.sh"
RELEASE_REPOSITORY=example-owner/fleet-fork
curl() {
  printf '%s\n' "$*" >> "$scratch/requests"
  case "$*" in
    *https://github.com/example-owner/fleet-fork/releases/latest*) printf '%s' 'https://github.com/example-owner/fleet-fork/releases/tag/v1.2.3' ;;
    *https://raw.githubusercontent.com/example-owner/fleet-fork/nightly-channel/latest.txt*) printf '%s' 'nightly-20260917-0123456789ab' ;;
    *) return 22 ;;
  esac
}
[[ "$(resolve_latest_version)" == v1.2.3 ]]
[[ "$(resolve_latest_nightly_version)" == nightly-20260917-0123456789ab ]]
curl() { printf '%s\n' "$*" >> "$scratch/requests"; return 22; }
if (resolve_latest_version); then echo 'failed lookup was accepted' >&2; exit 1; fi
if (resolve_latest_nightly_version); then echo 'failed nightly lookup was accepted' >&2; exit 1; fi
if grep -q 'block/proto-fleet' "$scratch/requests"; then echo 'lookup fell back to upstream' >&2; exit 1; fi

# Exercise the runner's exact source preflight without reaching Docker or host
# mutations. The source-build path deliberately has no version.txt.
sed -n '/^# Bind manual runs/,/^validate_runner_env_values/p' "$root/deployment-files/run-fleet.sh" | sed '$d' > "$scratch/runner-source.sh"
check_runner_source() (
  PROJECT_ROOT="$scratch/runner"
  ENV_FILE="$PROJECT_ROOT/.env"
  RELEASE_REPOSITORY="${1:-}"
  source "$root/deployment-files/scripts/compose-project.sh"
  source "$scratch/runner-source.sh"
)
mkdir -p "$scratch/runner"
check_runner_source
printf 'release_repository: example-owner/fleet-fork\n' > "$scratch/runner/version.txt"
printf 'PROTO_FLEET_RELEASE_REPOSITORY="example-owner/fleet-fork"\n' > "$scratch/runner/.env"
check_runner_source example-owner/fleet-fork
if check_runner_source block/proto-fleet; then echo 'runner accepted conflicting process identity' >&2; exit 1; fi
printf 'PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\n' >> "$scratch/runner/.env"
if check_runner_source; then echo 'runner accepted duplicate persisted identities' >&2; exit 1; fi
echo 'release repository installer and packaging checks passed'
