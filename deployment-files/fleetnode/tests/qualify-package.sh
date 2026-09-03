#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: qualify-package.sh VERSION PACKAGE_DIR" >&2
  exit 2
fi

VERSION="$1"
PACKAGE_DIR="$2"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
INSTALLER="$SCRIPT_DIR/../install-fleet-node.sh"
DOWNLOAD_URL="file://$PACKAGE_DIR"
CLEANUP_ALLOWED=0
WORK_DIR=""

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported qualification architecture: $(uname -m)" >&2; exit 1 ;;
esac
ARCHIVE="fleetnode-${VERSION}-linux-${ARCH}.tar.gz"

fail() {
  echo "Fleet Node package qualification failed: $*" >&2
  exit 1
}

run_installer() {
  env \
    FLEETNODE_TEST_MODE=systemd \
    FLEETNODE_DOWNLOAD_BASE_URL="$DOWNLOAD_URL" \
    bash "$INSTALLER" "$@"
}

cleanup() {
  status=$?
  trap - EXIT
  if [[ "$status" -ne 0 ]]; then
    sudo systemctl status --no-pager fleet-node.service || true
    sudo journalctl --no-pager -u fleet-node.service || true
  fi
  if [[ "$CLEANUP_ALLOWED" == "1" ]]; then
    run_installer uninstall >/dev/null 2>&1 || true
  fi
  [[ -z "$WORK_DIR" ]] || rm -rf "$WORK_DIR"
  exit "$status"
}
trap cleanup EXIT

for path in /opt/fleetnode /etc/fleetnode /var/lib/fleetnode /etc/systemd/system/fleet-node.service /usr/local/bin/fleetnode-enroll; do
  [[ ! -e "$path" ]] || fail "runner is not clean: $path already exists"
done
getent passwd fleetnode >/dev/null 2>&1 && fail "runner already has a fleetnode account"

sudo chmod go-w /opt
CLEANUP_ALLOWED=1
run_installer "$VERSION"
sudo systemctl is-active --quiet fleet-node.service && fail "fresh install started the unenrolled service"
sudo systemctl is-enabled --quiet fleet-node.service && fail "fresh install enabled the service"
[[ -x /usr/local/bin/fleetnode-enroll ]] || fail "enrollment helper was not installed"
/usr/local/bin/fleetnode-enroll --help | grep -Fq "sudo fleetnode-enroll --server-url=URL" || \
  fail "enrollment helper did not report its supported command"
sudo systemctl is-active --quiet fleet-node.service && fail "enrollment helper help started the service"
sudo systemctl is-enabled --quiet fleet-node.service && fail "enrollment helper help enabled the service"

sudo tee /var/lib/fleetnode/state.yaml >/dev/null <<'EOF'
server_url: http://127.0.0.1:1
allow_insecure_transport: true
fleet_node_id: 1
identity_fingerprint: qualification
identity_private_key_hex: 00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000
identity_public_key_hex: 0000000000000000000000000000000000000000000000000000000000000000
encryption_private_key_hex: 0000000000000000000000000000000000000000000000000000000000000000
encryption_public_key_hex: 0000000000000000000000000000000000000000000000000000000000000000
credential_key_hex: 0000000000000000000000000000000000000000000000000000000000000000
api_key: qualification
session_token: qualification
session_expires_at: 2099-01-01T00:00:00Z
EOF
sudo chown fleetnode:fleetnode /var/lib/fleetnode/state.yaml
sudo chmod 0600 /var/lib/fleetnode/state.yaml

sudo fleetnode-enroll --server-url=http://127.0.0.1:1 --allow-insecure-transport
sudo systemctl is-active --quiet fleet-node.service || fail "installed service did not report ready"

original_plugin_hash=$(sha256sum /opt/fleetnode/plugins/antminer-plugin | awk '{print $1}')
run_installer "$VERSION"
sudo systemctl is-active --quiet fleet-node.service || fail "known-good upgrade did not leave the service active"

WORK_DIR=$(mktemp -d)
tar -xzf "$PACKAGE_DIR/$ARCHIVE" -C "$WORK_DIR"
printf '#!/usr/bin/env bash\nexit 1\n' > "$WORK_DIR/${ARCHIVE%.tar.gz}/plugins/antminer-plugin"
(
  cd "$WORK_DIR"
  tar -czf "$ARCHIVE" "${ARCHIVE%.tar.gz}"
  sha256sum "$ARCHIVE" > "$ARCHIVE.sha256"
)
if DOWNLOAD_URL="file://$WORK_DIR" run_installer "$VERSION"; then
  fail "upgrade accepted a plugin that exited during startup"
fi
sudo systemctl is-active --quiet fleet-node.service || fail "failed upgrade did not restore the running service"
restored_plugin_hash=$(sha256sum /opt/fleetnode/plugins/antminer-plugin | awk '{print $1}')
[[ "$restored_plugin_hash" == "$original_plugin_hash" ]] || fail "failed upgrade did not restore the previous payload"
rm -rf "$WORK_DIR"
WORK_DIR=""

run_installer uninstall
[[ ! -e /opt/fleetnode ]] || fail "uninstall retained the program"
[[ ! -e /etc/systemd/system/fleet-node.service ]] || fail "uninstall retained the unit"
[[ ! -e /usr/local/bin/fleetnode-enroll ]] || fail "uninstall retained the enrollment helper"
sudo test -f /var/lib/fleetnode/state.yaml || fail "uninstall removed state"
getent passwd fleetnode >/dev/null || fail "uninstall removed the service account"

trap - EXIT
echo "Fleet Node package qualification passed"
