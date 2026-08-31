#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
FLEETNODE_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

ASSETS_DIR="$TEST_DIR/assets"
ROOT_PREFIX="$TEST_DIR/root"
SYSTEMCTL_LOG="$TEST_DIR/systemctl.log"
SUDO_LOG="$TEST_DIR/sudo.log"
LINUX_SERVICE_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
mkdir -p "$ASSETS_DIR" "$ROOT_PREFIX" "$TEST_DIR/bin"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file_contains() {
  local path="$1"
  local expected="$2"
  grep -Fq "$expected" "$path" || fail "$path does not contain: $expected"
}

create_release() {
  local version="$1"
  local extra_entry="${2:-}"
  local release_dir="$ASSETS_DIR/$version"
  local archive_root="fleetnode-${version}-linux-amd64"
  mkdir -p "$release_dir/$archive_root/plugins"

  printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/fleetnode"
  chmod 0755 "$release_dir/$archive_root/fleetnode"
  printf 'version: %s\n' "$version" > "$release_dir/$archive_root/version.txt"
  cp "$FLEETNODE_DIR/fleet-node.service" "$release_dir/$archive_root/fleet-node.service"

  local plugin
  for plugin in proto-plugin antminer-plugin virtual-plugin asicrs-plugin; do
    printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/plugins/$plugin"
    chmod 0755 "$release_dir/$archive_root/plugins/$plugin"
  done
  printf '{}\n' > "$release_dir/$archive_root/plugins/virtual-plugin.json"
  printf 'plugin: {}\nminers: {}\n' > "$release_dir/$archive_root/plugins/asicrs-config.yaml"
  if [[ -n "$extra_entry" ]]; then
    printf '#!/usr/bin/env bash\nexit 0\n' > "$release_dir/$archive_root/$extra_entry"
    chmod 4755 "$release_dir/$archive_root/$extra_entry"
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
exit 0
EOF
chmod 0755 "$TEST_DIR/bin/systemctl"

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
if grep -Fq 'is-active' "$FLEETNODE_DIR/install-fleet-node.sh"; then
  fail "installer checks active state before restarting"
fi
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

mkdir -p "$TEST_DIR/curl-home"
printf 'proto = "=https"\n' > "$TEST_DIR/curl-home/.curlrc"

run_installer() {
  local version="$1"
  FAKE_FLEETNODE_ENABLED="${FAKE_FLEETNODE_ENABLED:-0}" \
  FAKE_FLEETNODE_LOCK_READY="${FAKE_FLEETNODE_LOCK_READY:-}" \
  FAKE_FLEETNODE_LOCK_BLOCK="${FAKE_FLEETNODE_LOCK_BLOCK:-}" \
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

: > "$SYSTEMCTL_LOG"
: > "$SUDO_LOG"
mkdir -p "$ROOT_PREFIX/etc/systemd/system"
printf 'legacy unit\n' > "$ROOT_PREFIX/etc/systemd/system/fleetnode.service"
CURL_HOME="$TEST_DIR/curl-home" FAKE_FLEETNODE_ENABLED=1 run_installer v1.0.0

[[ -x "$ROOT_PREFIX/opt/fleetnode/fleetnode" ]] || fail "Fleet Node binary was not installed"
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.0.0"
[[ -f "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" ]] || fail "systemd unit was not installed"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "Environment=PATH=$LINUX_SERVICE_PATH"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "ExecStart=/usr/bin/env PATH=$LINUX_SERVICE_PATH /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode run"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "Restart=on-failure"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleet-node.service" "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK"
assert_file_contains "$ROOT_PREFIX/etc/systemd/system/fleetnode.service" "legacy unit"
if grep -Eq '(^| )enable( |$)|(^| )stop( |$)' "$SYSTEMCTL_LOG"; then
  fail "fresh install enabled or stopped the service"
fi
if grep -Fq 'fleetnode.service' "$SYSTEMCTL_LOG"; then
  fail "installer operated on the old systemd unit"
fi
assert_file_contains "$SYSTEMCTL_LOG" "disable fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "start fleet-node.service"
assert_file_contains "$SUDO_LOG" "$TEST_DIR/bin/systemctl start fleet-node.service"
if grep -Fq 'install-fleet-node.sh' "$SUDO_LOG"; then
  fail "installer asked sudo to rerun the whole script"
fi

printf 'operator config\n' > "$ROOT_PREFIX/etc/fleetnode/config.yaml"
printf 'identity material\n' > "$ROOT_PREFIX/var/lib/fleetnode/state.yaml"
printf 'stale program file\n' > "$ROOT_PREFIX/opt/fleetnode/stale.txt"

: > "$SYSTEMCTL_LOG"
run_installer v1.1.0

assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
[[ ! -e "$ROOT_PREFIX/opt/fleetnode/stale.txt" ]] || fail "upgrade retained stale program files"
assert_file_contains "$ROOT_PREFIX/etc/fleetnode/config.yaml" "operator config"
assert_file_contains "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" "identity material"
assert_file_contains "$SYSTEMCTL_LOG" "stop fleet-node.service"
assert_file_contains "$SYSTEMCTL_LOG" "start fleet-node.service"

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
  SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  SUDO_LOG="$SUDO_LOG" \
  PATH="$TEST_DIR/bin:$PATH" \
    bash <(curl --disable --fail --silent --show-error "file://$FLEETNODE_DIR/install-fleet-node.sh") v1.1.0 2> "$TEST_DIR/bad-checksum.err"; then
  fail "installer accepted a checksum mismatch"
fi
assert_file_contains "$TEST_DIR/bad-checksum.err" "checksum sidecar is not bound"
assert_file_contains "$ROOT_PREFIX/opt/fleetnode/version.txt" "version: v1.1.0"
assert_file_contains "$ROOT_PREFIX/var/lib/fleetnode/state.yaml" "identity material"

echo "Fleet Node installer tests passed"
