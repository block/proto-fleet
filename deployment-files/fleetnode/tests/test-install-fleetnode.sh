#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
FLEETNODE_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'status=$?; if [[ "$status" == "0" ]]; then rm -rf "$TEST_DIR"; else echo "test artifacts: $TEST_DIR" >&2; fi; exit "$status"' EXIT

ASSETS_DIR="$TEST_DIR/assets"
ROOT_PREFIX="$TEST_DIR/root"
SYSTEMCTL_LOG="$TEST_DIR/systemctl.log"
SUDO_LOG="$TEST_DIR/sudo.log"
ACCOUNT_LOG="$TEST_DIR/account.log"
ACCOUNT_DB="$TEST_DIR/account-db"
LINUX_SERVICE_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
mkdir -p "$ASSETS_DIR" "$ROOT_PREFIX/run/systemd/system" "$TEST_DIR/bin" "$ACCOUNT_DB"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file_contains() {
  local path="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$path" || fail "$path does not contain: $expected"
}

file_mode() {
  stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"
}

create_release() {
  local version="$1"
  local extra_entry="${2:-}"
  local unit_marker="${3:-}"
  local release_dir="$ASSETS_DIR/$version"
  local archive_root="fleetnode-${version}-linux-amd64"
  mkdir -p "$release_dir/$archive_root/plugins"

  printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/fleetnode"
  chmod 0755 "$release_dir/$archive_root/fleetnode"
  printf 'version: %s\n' "$version" > "$release_dir/$archive_root/version.txt"
  cp "$FLEETNODE_DIR/fleet-node.service" "$release_dir/$archive_root/fleet-node.service"
  if [[ -n "$unit_marker" ]]; then
    printf '%s\n' "$unit_marker" >> "$release_dir/$archive_root/fleet-node.service"
  fi

  local plugin
  for plugin in proto-plugin antminer-plugin virtual-plugin asicrs-plugin; do
    printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/plugins/$plugin"
    chmod 0755 "$release_dir/$archive_root/plugins/$plugin"
  done
  printf '{}\n' > "$release_dir/$archive_root/plugins/virtual-plugin.json"
  printf 'plugin: {}\nminers: {}\n' > "$release_dir/$archive_root/plugins/asicrs-config.yaml"
  if [[ -n "$extra_entry" ]]; then
    printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/$extra_entry"
    chmod 4755 "$release_dir/$archive_root/$extra_entry" 2>/dev/null || \
      chmod 0755 "$release_dir/$archive_root/$extra_entry"
  fi

  (
    cd "$release_dir"
    tar -czf "$archive_root.tar.gz" "$archive_root"
    sha256sum "$archive_root.tar.gz" > "$archive_root.tar.gz.sha256"
    rm -rf "$archive_root"
  )
}

cat > "$TEST_DIR/bin/id" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "-u" ]]; then
  echo 1000
  exit 0
fi
if [[ "${1:-}" == "-nG" && "${2:-}" == "fleetnode" && -e "$FAKE_ACCOUNT_DB/user" ]]; then
  echo "${FAKE_ACCOUNT_GROUPS:-fleetnode}"
  exit 0
fi
exit 1
EOF
chmod 0755 "$TEST_DIR/bin/id"

cat > "$TEST_DIR/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$SUDO_LOG"
exec "$@"
EOF
chmod 0755 "$TEST_DIR/bin/sudo"

cat > "$TEST_DIR/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
if [[ "${1:-}" == "show" && "${FAKE_SYSTEMCTL_FAIL_SHOW:-0}" == "1" ]]; then
  exit 1
fi
if [[ "${1:-}" == "stop" && -n "${FAKE_FLEETNODE_LOCK_READY:-}" ]]; then
  : > "$FAKE_FLEETNODE_LOCK_READY"
  while [[ -e "${FAKE_FLEETNODE_LOCK_BLOCK:-}" ]]; do
    sleep 0.05
  done
fi
if [[ "${1:-}" == "is-enabled" ]]; then
  [[ "${FAKE_FLEETNODE_ENABLED:-0}" == "1" ]]
  exit
fi
if [[ "${1:-}" == "is-active" ]]; then
  [[ "${FAKE_FLEETNODE_ACTIVE:-0}" == "1" ]]
  exit
fi
if [[ "${1:-}" == "start" ]]; then
  if [[ "${FAKE_SYSTEMCTL_FAIL_START:-0}" == "1" ]]; then
    exit 1
  fi
  if [[ -n "${FAKE_SYSTEMCTL_FAIL_START_ONCE:-}" && -e "$FAKE_SYSTEMCTL_FAIL_START_ONCE" ]]; then
    rm -f "$FAKE_SYSTEMCTL_FAIL_START_ONCE"
    exit 1
  fi
fi
exit 0
EOF
chmod 0755 "$TEST_DIR/bin/systemctl"

cat > "$TEST_DIR/bin/getent" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}:${2:-}" in
  passwd:fleetnode)
    [[ -e "$FAKE_ACCOUNT_DB/user" ]] || exit 2
    printf 'fleetnode:x:995:%s::%s:%s\n' \
      "${FAKE_ACCOUNT_PRIMARY_GID:-995}" \
      "${FAKE_ACCOUNT_HOME:-/var/lib/fleetnode}" \
      "${FAKE_ACCOUNT_SHELL:-$FAKE_NOLOGIN_PATH}"
    ;;
  group:fleetnode)
    [[ -e "$FAKE_ACCOUNT_DB/group" ]] || exit 2
    printf 'fleetnode:x:%s:%s\n' \
      "${FAKE_GROUP_GID:-995}" "${FAKE_GROUP_MEMBERS:-}"
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$TEST_DIR/bin/getent"

cat > "$TEST_DIR/bin/useradd" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'useradd %s\n' "$*" >> "$ACCOUNT_LOG"
touch "$FAKE_ACCOUNT_DB/user"
if [[ " $* " == *" --user-group "* ]]; then
  touch "$FAKE_ACCOUNT_DB/group"
fi
EOF
chmod 0755 "$TEST_DIR/bin/useradd"

cat > "$TEST_DIR/bin/userdel" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'userdel %s\n' "$*" >> "$ACCOUNT_LOG"
rm -f "$FAKE_ACCOUNT_DB/user"
EOF
chmod 0755 "$TEST_DIR/bin/userdel"

cat > "$TEST_DIR/bin/groupdel" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'groupdel %s\n' "$*" >> "$ACCOUNT_LOG"
rm -f "$FAKE_ACCOUNT_DB/group"
EOF
chmod 0755 "$TEST_DIR/bin/groupdel"

cat > "$TEST_DIR/bin/runuser" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
shift 3
if [[ -n "${FAKE_RUNUSER_DENY:-}" && " $* " == *" $FAKE_RUNUSER_DENY "* ]]; then
  exit 1
fi
exec "$@"
EOF
chmod 0755 "$TEST_DIR/bin/runuser"

cat > "$TEST_DIR/bin/pgrep" <<'EOF'
#!/usr/bin/env bash
[[ "${FAKE_ACCOUNT_BUSY:-0}" == "1" ]]
EOF
chmod 0755 "$TEST_DIR/bin/pgrep"

cat > "$TEST_DIR/bin/nologin" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod 0755 "$TEST_DIR/bin/nologin"

cat > "$TEST_DIR/bin/nmap" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod 0755 "$TEST_DIR/bin/nmap"

cat > "$TEST_DIR/bin/flock" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${REAL_FLOCK:-}" ]]; then
  exec "$REAL_FLOCK" "$@"
fi
if [[ "${FAKE_STATE_LOCK_HELD:-0}" == "1" && " $* " == *"state.lock"* ]]; then
  exit 1
fi
if [[ "${1:-}" == "-n" ]]; then
  shift 2
fi
exec "$@"
EOF
chmod 0755 "$TEST_DIR/bin/flock"

if /bin/bash "$FLEETNODE_DIR/install-fleet-node.sh" > "$TEST_DIR/missing-version.out" 2>&1; then
  fail "installer accepted a missing version"
fi
assert_file_contains "$TEST_DIR/missing-version.out" "Usage: install-fleet-node.sh VERSION"
if /bin/bash "$FLEETNODE_DIR/install-fleet-node.sh" --version v1.0.0 > "$TEST_DIR/version-flag.out" 2>&1; then
  fail "installer accepted --version"
fi
assert_file_contains "$TEST_DIR/version-flag.out" "Usage: install-fleet-node.sh VERSION"

NO_NMAP_BIN="$TEST_DIR/no-nmap-bin"
mkdir -p "$NO_NMAP_BIN"
for command in uname curl sha256sum tar install; do
  ln -s "$(command -v "$command")" "$NO_NMAP_BIN/$command"
done
for command in flock id sudo; do
  ln -s "$TEST_DIR/bin/$command" "$NO_NMAP_BIN/$command"
done
if PATH="$NO_NMAP_BIN" \
  FLEETNODE_TEST_MODE=1 \
  FLEETNODE_ROOT_PREFIX="$ROOT_PREFIX" \
  FLEETNODE_ARCH=amd64 \
  FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
    /bin/bash "$FLEETNODE_DIR/install-fleet-node.sh" v1.0.0 2> "$TEST_DIR/missing-nmap.err"; then
  fail "installer accepted a host without nmap on PATH"
fi
assert_file_contains "$TEST_DIR/missing-nmap.err" "required command not found: nmap"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "LINUX_SERVICE_PATH=\"$LINUX_SERVICE_PATH\""
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "PATH=\"\$LINUX_SERVICE_PATH\""
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "as_root flock -n \"\$INSTALL_LOCK_PATH\""
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" 'is-active --quiet fleet-node.service'
if grep -Fq 'fleetnode.service' "$FLEETNODE_DIR/install-fleet-node.sh"; then
  fail "installer contains a migration path for the old systemd unit"
fi

if FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
    /bin/bash "$FLEETNODE_DIR/install-fleet-node.sh" v1.0.0 2> "$TEST_DIR/systemctl-override.err"; then
  fail "installer accepted a systemctl override outside test mode"
fi
assert_file_contains "$TEST_DIR/systemctl-override.err" "installer overrides are restricted to automated tests"

create_release v1.0.0
create_release v1.1.0
create_release v1.2.0 unexpected-setuid
create_release v1.3.0 "" "# candidate unit v1.3.0"

mkdir -p "$TEST_DIR/curl-home"
printf 'proto = "=https"\n' > "$TEST_DIR/curl-home/.curlrc"

run_installer() {
  local version="$1"
  FAKE_FLEETNODE_ENABLED="${FAKE_FLEETNODE_ENABLED:-0}" \
  FAKE_FLEETNODE_ACTIVE="${FAKE_FLEETNODE_ACTIVE:-1}" \
  FAKE_FLEETNODE_LOCK_READY="${FAKE_FLEETNODE_LOCK_READY:-}" \
  FAKE_FLEETNODE_LOCK_BLOCK="${FAKE_FLEETNODE_LOCK_BLOCK:-}" \
  FAKE_SYSTEMCTL_FAIL_START="${FAKE_SYSTEMCTL_FAIL_START:-0}" \
  FAKE_SYSTEMCTL_FAIL_START_ONCE="${FAKE_SYSTEMCTL_FAIL_START_ONCE:-}" \
  FAKE_SYSTEMCTL_FAIL_SHOW="${FAKE_SYSTEMCTL_FAIL_SHOW:-0}" \
  FAKE_ACCOUNT_DB="$ACCOUNT_DB" \
  FAKE_NOLOGIN_PATH="$TEST_DIR/bin/nologin" \
  FAKE_ACCOUNT_HOME="${FAKE_ACCOUNT_HOME:-/var/lib/fleetnode}" \
  FAKE_ACCOUNT_SHELL="${FAKE_ACCOUNT_SHELL:-$TEST_DIR/bin/nologin}" \
  FAKE_ACCOUNT_GROUPS="${FAKE_ACCOUNT_GROUPS:-fleetnode}" \
  FAKE_ACCOUNT_PRIMARY_GID="${FAKE_ACCOUNT_PRIMARY_GID:-995}" \
  FAKE_GROUP_GID="${FAKE_GROUP_GID:-995}" \
  FAKE_GROUP_MEMBERS="${FAKE_GROUP_MEMBERS:-}" \
  FAKE_RUNUSER_DENY="${FAKE_RUNUSER_DENY:-}" \
  FAKE_ACCOUNT_BUSY="${FAKE_ACCOUNT_BUSY:-0}" \
  FAKE_STATE_LOCK_HELD="${FAKE_STATE_LOCK_HELD:-0}" \
  ACCOUNT_LOG="$ACCOUNT_LOG" \
  REAL_FLOCK="${REAL_FLOCK:-}" \
  SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  SUDO_LOG="$SUDO_LOG" \
  PATH="$TEST_DIR/bin:$PATH" \
  FLEETNODE_TEST_MODE=1 \
  FLEETNODE_ROOT_PREFIX="$ROOT_PREFIX" \
  FLEETNODE_ARCH=amd64 \
  FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
  FLEETNODE_DOWNLOAD_BASE_URL="file://$ASSETS_DIR/$version" \
    bash <(curl --disable --fail --silent --show-error "file://$FLEETNODE_DIR/install-fleet-node.sh") "$version"
}

run_uninstaller() {
  FAKE_FLEETNODE_ENABLED="${FAKE_FLEETNODE_ENABLED:-0}" \
  FAKE_FLEETNODE_ACTIVE="${FAKE_FLEETNODE_ACTIVE:-0}" \
  FAKE_ACCOUNT_DB="$ACCOUNT_DB" \
  FAKE_NOLOGIN_PATH="$TEST_DIR/bin/nologin" \
  FAKE_ACCOUNT_HOME="${FAKE_ACCOUNT_HOME:-/var/lib/fleetnode}" \
  FAKE_ACCOUNT_SHELL="${FAKE_ACCOUNT_SHELL:-$TEST_DIR/bin/nologin}" \
  FAKE_ACCOUNT_GROUPS="${FAKE_ACCOUNT_GROUPS:-fleetnode}" \
  FAKE_ACCOUNT_PRIMARY_GID="${FAKE_ACCOUNT_PRIMARY_GID:-995}" \
  FAKE_GROUP_GID="${FAKE_GROUP_GID:-995}" \
  FAKE_GROUP_MEMBERS="${FAKE_GROUP_MEMBERS:-}" \
  FAKE_ACCOUNT_BUSY="${FAKE_ACCOUNT_BUSY:-0}" \
  FAKE_STATE_LOCK_HELD="${FAKE_STATE_LOCK_HELD:-0}" \
  ACCOUNT_LOG="$ACCOUNT_LOG" \
  SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  SUDO_LOG="$SUDO_LOG" \
  PATH="$TEST_DIR/bin:$PATH" \
  FLEETNODE_TEST_MODE=1 \
  FLEETNODE_ROOT_PREFIX="$ROOT_PREFIX" \
  FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
    /bin/bash "$FLEETNODE_DIR/install-fleet-node.sh" uninstall "$@"
}

start_installer() {
  local version="$1"
  local output_path="$2"
  FAKE_FLEETNODE_ENABLED="${FAKE_FLEETNODE_ENABLED:-0}" \
  FAKE_FLEETNODE_ACTIVE="${FAKE_FLEETNODE_ACTIVE:-1}" \
  FAKE_FLEETNODE_LOCK_READY="${FAKE_FLEETNODE_LOCK_READY:-}" \
  FAKE_FLEETNODE_LOCK_BLOCK="${FAKE_FLEETNODE_LOCK_BLOCK:-}" \
  FAKE_ACCOUNT_DB="$ACCOUNT_DB" \
  FAKE_NOLOGIN_PATH="$TEST_DIR/bin/nologin" \
  FAKE_ACCOUNT_HOME="${FAKE_ACCOUNT_HOME:-/var/lib/fleetnode}" \
  FAKE_ACCOUNT_SHELL="${FAKE_ACCOUNT_SHELL:-$TEST_DIR/bin/nologin}" \
  FAKE_ACCOUNT_GROUPS="${FAKE_ACCOUNT_GROUPS:-fleetnode}" \
  FAKE_ACCOUNT_PRIMARY_GID="${FAKE_ACCOUNT_PRIMARY_GID:-995}" \
  FAKE_GROUP_GID="${FAKE_GROUP_GID:-995}" \
  FAKE_GROUP_MEMBERS="${FAKE_GROUP_MEMBERS:-}" \
  FAKE_RUNUSER_DENY="${FAKE_RUNUSER_DENY:-}" \
  ACCOUNT_LOG="$ACCOUNT_LOG" \
  REAL_FLOCK="${REAL_FLOCK:-}" \
  SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  SUDO_LOG="$SUDO_LOG" \
  PATH="$TEST_DIR/bin:$PATH" \
  FLEETNODE_TEST_MODE=1 \
  FLEETNODE_ROOT_PREFIX="$ROOT_PREFIX" \
  FLEETNODE_ARCH=amd64 \
  FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
  FLEETNODE_DOWNLOAD_BASE_URL="file://$ASSETS_DIR/$version" \
    bash <(curl --disable --fail --silent --show-error "file://$FLEETNODE_DIR/install-fleet-node.sh") "$version" \
      > "$output_path" 2>&1 &
  STARTED_INSTALLER_PID=$!
}

: > "$SYSTEMCTL_LOG"
: > "$SUDO_LOG"
: > "$ACCOUNT_LOG"
mkdir -p "$ROOT_PREFIX/etc/systemd/system"
mkdir -p "$ROOT_PREFIX/opt"
chmod 0711 "$ROOT_PREFIX/opt"
chmod 0751 "$ROOT_PREFIX/etc/systemd/system"
printf 'legacy unit\n' > "$ROOT_PREFIX/etc/systemd/system/fleetnode.service"
CURL_HOME="$TEST_DIR/curl-home" FAKE_FLEETNODE_ENABLED=1 run_installer v1.0.0

[[ "$(file_mode "$ROOT_PREFIX/opt")" == "711" ]] || fail "installer changed /opt mode"
[[ "$(file_mode "$ROOT_PREFIX/etc/systemd/system")" == "751" ]] || fail "installer changed systemd directory mode"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "--connect-timeout 10"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "--max-time 300"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "--retry 3"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "--retry-delay 2"
assert_file_contains "$FLEETNODE_DIR/install-fleet-node.sh" "--retry-connrefused"

[[ -x "$ROOT_PREFIX/opt/fleetnode/fleetnode" ]] || fail "Fleet Node binary was not installed"
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.0.0"
[[ -f "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" ]] || fail "systemd unit was not installed"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "Environment=PATH=$LINUX_SERVICE_PATH"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "ExecStart=/usr/bin/env PATH=$LINUX_SERVICE_PATH /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode run"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "Type=notify"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "NotifyAccess=main"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "Restart=on-failure"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "RestartPreventExitStatus=78"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "TimeoutStartSec=300s"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleetnode.service" "legacy unit"
if grep -Eq '(^| )enable( |$)|(^| )stop( |$)' "$SYSTEMCTL_LOG"; then
  fail "fresh install enabled or stopped the service"
fi
if grep -Fq 'fleetnode.service' "$SYSTEMCTL_LOG"; then
  fail "installer operated on the old systemd unit"
fi
assert_file_contains "$SYSTEMCTL_LOG" "disable fleet-node.service"
if grep -Fq 'start fleet-node.service' "$SYSTEMCTL_LOG"; then
  fail "fresh install started the unenrolled service"
fi
if grep -Fq "$TEST_DIR/bin/systemctl start fleet-node.service" "$SUDO_LOG"; then
  fail "fresh install asked sudo to start the unenrolled service"
fi
if grep -Fq 'install-fleet-node.sh' "$SUDO_LOG"; then
  fail "installer asked sudo to rerun the whole script"
fi

printf 'operator config\n' > "$ROOT_PREFIX/etc/fleetnode/config.yaml"
printf 'identity material\n' > "$ROOT_PREFIX/var/lib/fleetnode/state.yaml"
printf 'stale program file\n' > "$ROOT_PREFIX/opt/fleetnode/stale.txt"

: > "$SYSTEMCTL_LOG"
if FAKE_ACCOUNT_HOME=/tmp/not-fleetnode run_installer v1.1.0 > "$TEST_DIR/bad-account.out" 2>&1; then
  fail "installer reused an account with the wrong home"
fi
assert_file_contains "$TEST_DIR/bad-account.out" "fleetnode account must use home /var/lib/fleetnode"
if grep -Fq 'stop fleet-node.service' "$SYSTEMCTL_LOG"; then
  fail "installer stopped the service before validating its account"
fi

: > "$SYSTEMCTL_LOG"
if FAKE_RUNUSER_DENY="$ROOT_PREFIX/var/lib/fleetnode/state.yaml" \
    run_installer v1.1.0 > "$TEST_DIR/unreadable-state.out" 2>&1; then
  fail "installer accepted unreadable preserved state"
fi
assert_file_contains "$TEST_DIR/unreadable-state.out" "fleetnode cannot read preserved file"
if grep -Fq 'stop fleet-node.service' "$SYSTEMCTL_LOG"; then
  fail "installer stopped the service before validating preserved state"
fi

: > "$SYSTEMCTL_LOG"
run_installer v1.1.0

assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
[[ ! -e "$ROOT_PREFIX/opt/fleetnode/stale.txt" ]] || fail "upgrade retained stale program files"
assert_file_contains "$ROOT_PREFIX/etc/fleetnode/config.yaml" "operator config"
assert_file_contains "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" "identity material"
assert_file_contains "$SYSTEMCTL_LOG" "stop fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "start fleet-node.service"

FAIL_START_ONCE="$TEST_DIR/fail-upgrade-start-once"
: > "$FAIL_START_ONCE"
: > "$SYSTEMCTL_LOG"
if FAKE_SYSTEMCTL_FAIL_START_ONCE="$FAIL_START_ONCE" run_installer v1.3.0 > "$TEST_DIR/failed-upgrade.out" 2>&1; then
  fail "installer accepted an upgrade whose service did not become ready"
fi
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
if grep -Fq '# candidate unit v1.3.0' "$ROOT_PREFIX/etc/systemd/system/fleet-node.service"; then
  fail "failed upgrade retained the candidate systemd unit"
fi
[[ "$(grep -Fc 'start fleet-node.service' "$SYSTEMCTL_LOG")" == "2" ]] || \
  fail "failed upgrade did not start the candidate and then restart the previous service"
[[ "$(grep -E '^(is-active --quiet|stop|start) fleet-node.service$' "$SYSTEMCTL_LOG")" == \
  $'is-active --quiet fleet-node.service\nstop fleet-node.service\nstart fleet-node.service\nstop fleet-node.service\nstart fleet-node.service' ]] || \
  fail "failed upgrade did not stop the candidate before restarting the previous service"

: > "$SYSTEMCTL_LOG"
if FAKE_SYSTEMCTL_FAIL_START=1 run_installer v1.3.0 > "$TEST_DIR/failed-rollback.out" 2>&1; then
  fail "installer accepted an upgrade whose candidate and rollback restart both failed"
fi
assert_file_contains "$TEST_DIR/failed-rollback.out" "rollback failed: restart previous Fleet Node service"
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"

if REAL_FLOCK_BINARY=$(command -v flock 2>/dev/null); then
  LOCK_READY="$TEST_DIR/installer-lock.ready"
  LOCK_BLOCK="$TEST_DIR/installer-lock.block"
  : > "$LOCK_BLOCK"
  REAL_FLOCK="$REAL_FLOCK_BINARY" \
    FAKE_FLEETNODE_LOCK_READY="$LOCK_READY" \
    FAKE_FLEETNODE_LOCK_BLOCK="$LOCK_BLOCK" \
    run_installer v1.1.0 > "$TEST_DIR/first-installer.log" 2>&1 &
  first_installer_pid=$!

  for _ in {1..100}; do
    [[ -e "$LOCK_READY" ]] && break
    sleep 0.05
  done
  if [[ ! -e "$LOCK_READY" ]]; then
    rm -f "$LOCK_BLOCK"
    wait "$first_installer_pid" || true
    fail "first installer did not reach the lock test barrier"
  fi

  if REAL_FLOCK="$REAL_FLOCK_BINARY" run_installer v1.1.0 > "$TEST_DIR/second-installer.log" 2>&1; then
    rm -f "$LOCK_BLOCK"
    wait "$first_installer_pid" || true
    fail "installer accepted an overlapping invocation"
  fi
  assert_file_contains "$TEST_DIR/second-installer.log" "another Fleet Node installer is running"

  rm -f "$LOCK_BLOCK"
  wait "$first_installer_pid"
  assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"

  ORPHAN_READY="$TEST_DIR/orphan-lock.ready"
  ORPHAN_BLOCK="$TEST_DIR/orphan-lock.block"
  : > "$ORPHAN_BLOCK"
  REAL_FLOCK="$REAL_FLOCK_BINARY" \
    FAKE_FLEETNODE_LOCK_READY="$ORPHAN_READY" \
    FAKE_FLEETNODE_LOCK_BLOCK="$ORPHAN_BLOCK" \
    start_installer v1.1.0 "$TEST_DIR/orphaned-installer.log"
  orphaned_installer_pid=$STARTED_INSTALLER_PID

  for _ in {1..100}; do
    [[ -e "$ORPHAN_READY" ]] && break
    sleep 0.05
  done
  if [[ ! -e "$ORPHAN_READY" ]]; then
    rm -f "$ORPHAN_BLOCK"
    wait "$orphaned_installer_pid" || true
    fail "installer did not reach the orphaned-lock test barrier"
  fi

  kill -9 "$orphaned_installer_pid"
  rm -f "$ORPHAN_BLOCK"
  wait "$orphaned_installer_pid" 2>/dev/null || true

  lock_released=0
  for _ in {1..100}; do
    if REAL_FLOCK="$REAL_FLOCK_BINARY" run_installer v1.1.0 > "$TEST_DIR/orphan-lock-recovery.log" 2>&1; then
      lock_released=1
      break
    fi
    sleep 0.05
  done
  [[ "$lock_released" == "1" ]] || fail "installer lock survived after its parent was killed"
fi

if run_installer v1.2.0; then
  fail "installer accepted an archive entry outside the Fleet Node manifest"
fi
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
[[ ! -e "$ROOT_PREFIX/opt/fleetnode/unexpected-setuid" ]] || fail "installer persisted an unexpected archive entry"

cp -R "$ASSETS_DIR/v1.1.0" "$ASSETS_DIR/bad-checksum"
printf 'tampered\n' >> "$ASSETS_DIR/bad-checksum/fleetnode-v1.1.0-linux-amd64.tar.gz"
if FLEETNODE_TEST_MODE=1 \
  FLEETNODE_ROOT_PREFIX="$ROOT_PREFIX" \
  FLEETNODE_ARCH=amd64 \
  FLEETNODE_SYSTEMCTL="$TEST_DIR/bin/systemctl" \
  FLEETNODE_DOWNLOAD_BASE_URL="file://$ASSETS_DIR/bad-checksum" \
  FAKE_ACCOUNT_DB="$ACCOUNT_DB" \
  FAKE_NOLOGIN_PATH="$TEST_DIR/bin/nologin" \
  FAKE_ACCOUNT_SHELL="$TEST_DIR/bin/nologin" \
  FAKE_ACCOUNT_GROUPS=fleetnode \
  ACCOUNT_LOG="$ACCOUNT_LOG" \
  SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  SUDO_LOG="$SUDO_LOG" \
  PATH="$TEST_DIR/bin:$PATH" \
    bash <(curl --disable --fail --silent --show-error "file://$FLEETNODE_DIR/install-fleet-node.sh") v1.1.0 2> "$TEST_DIR/bad-checksum.err"; then
  fail "installer accepted a checksum mismatch"
fi
assert_file_contains "$TEST_DIR/bad-checksum.err" "checksum sidecar is not bound"
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
assert_file_contains "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" "identity material"

: > "$SYSTEMCTL_LOG"
FAKE_FLEETNODE_ACTIVE=0 run_installer v1.3.0
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.3.0"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "# candidate unit v1.3.0"
assert_file_contains "$SYSTEMCTL_LOG" "is-active --quiet fleet-node.service"
if grep -Eq '^(stop|start) fleet-node.service$' "$SYSTEMCTL_LOG"; then
  fail "inactive upgrade changed the Fleet Node service state"
fi

run_uninstaller
[[ ! -e "$ROOT_PREFIX/opt/fleetnode" ]] || fail "uninstall retained the program"
[[ ! -e "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" ]] || fail "uninstall retained the unit"
[[ -e "$ROOT_PREFIX/etc/fleetnode/config.yaml" ]] || fail "uninstall removed configuration"
[[ -e "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" ]] || fail "uninstall removed state"
[[ -e "$ACCOUNT_DB/user" && -e "$ACCOUNT_DB/group" ]] || fail "uninstall removed the service account"
assert_file_contains "$SYSTEMCTL_LOG" "stop fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "disable fleet-node.service"

run_uninstaller
run_installer v1.1.0

: > "$ROOT_PREFIX/var/lib/fleetnode/state.lock"
: > "$SYSTEMCTL_LOG"
if FAKE_FLEETNODE_ACTIVE=1 FAKE_FLEETNODE_ENABLED=1 FAKE_STATE_LOCK_HELD=1 run_uninstaller --purge > "$TEST_DIR/locked-purge.out" 2>&1; then
  fail "purge accepted an active state lock"
fi
assert_file_contains "$TEST_DIR/locked-purge.out" "Fleet Node state is in use"
[[ -e "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" ]] || fail "failed purge removed state"
assert_file_contains "$SYSTEMCTL_LOG" "stop fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "start fleet-node.service"
if grep -Fq "disable fleet-node.service" "$SYSTEMCTL_LOG"; then
  fail "refused locked purge disabled the service"
fi

: > "$SYSTEMCTL_LOG"
if FAKE_FLEETNODE_ACTIVE=1 FAKE_FLEETNODE_ENABLED=1 FAKE_ACCOUNT_BUSY=1 run_uninstaller --purge > "$TEST_DIR/busy-purge.out" 2>&1; then
  fail "purge accepted a service account with running processes"
fi
assert_file_contains "$TEST_DIR/busy-purge.out" "fleetnode account still has running processes"
[[ -e "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" ]] || fail "busy purge removed state"
assert_file_contains "$SYSTEMCTL_LOG" "stop fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "start fleet-node.service"
if grep -Fq "disable fleet-node.service" "$SYSTEMCTL_LOG"; then
  fail "refused busy purge disabled the service"
fi

run_uninstaller --purge
[[ ! -e "$ROOT_PREFIX/opt/fleetnode" ]] || fail "purge retained the program"
[[ ! -e "$ROOT_PREFIX/etc/fleetnode" ]] || fail "purge retained configuration"
[[ ! -e "$ROOT_PREFIX/var/lib/fleetnode" ]] || fail "purge retained state"
[[ ! -e "$ACCOUNT_DB/user" && ! -e "$ACCOUNT_DB/group" ]] || fail "purge retained the service account"
assert_file_contains "$ACCOUNT_LOG" "userdel fleetnode"
assert_file_contains "$ACCOUNT_LOG" "groupdel fleetnode"
run_uninstaller --purge

echo "Fleet Node installer tests passed"
