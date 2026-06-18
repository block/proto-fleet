#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$script_dir/lib.sh"

if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
  warn "systemd is not available; run ha/fleet-follows-primary.sh manually or install your own scheduler"
  exit 0
fi

sudo_cmd=()
if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || die "sudo is required to install systemd units"
  sudo_cmd=(sudo)
fi

escaped_root=$(printf '%s' "$project_root" | sed 's/[\/&]/\\&/g')

sed "s/__PROJECT_ROOT__/$escaped_root/g" "$script_dir/fleet-follows-primary.service.in" \
  | "${sudo_cmd[@]}" tee /etc/systemd/system/fleet-follows-primary.service >/dev/null

sed "s/__PROJECT_ROOT__/$escaped_root/g" "$script_dir/fleet-follows-primary.timer.in" \
  | "${sudo_cmd[@]}" tee /etc/systemd/system/fleet-follows-primary.timer >/dev/null

"${sudo_cmd[@]}" systemctl daemon-reload
"${sudo_cmd[@]}" systemctl enable --now fleet-follows-primary.timer

log "installed and started fleet-follows-primary.timer"
