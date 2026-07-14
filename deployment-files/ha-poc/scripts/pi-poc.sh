#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
poc_dir="$(cd "${script_dir}/.." && pwd)"
env_file="${HA_POC_ENV:-${poc_dir}/.env}"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/pi-poc.sh configure <fleet-a|fleet-b|witness> <fleet-a-ip> <fleet-b-ip> <witness-ip> <vip> [interface]
  ./scripts/pi-poc.sh lan-ip [interface]
  ./scripts/pi-poc.sh install-deps
  ./scripts/pi-poc.sh doctor
  ./scripts/pi-poc.sh start
  ./scripts/pi-poc.sh status
  ./scripts/pi-poc.sh active-host
  ./scripts/pi-poc.sh watch
  ./scripts/pi-poc.sh fail-app
  ./scripts/pi-poc.sh fail-db
  ./scripts/pi-poc.sh restore
  ./scripts/pi-poc.sh stop
  ./scripts/pi-poc.sh reset --yes

Environment:
  HA_POC_ENV       Path to the env file. Defaults to deployment-files/ha-poc/.env.
  HA_POC_PASSWORD  Shared POC password used by configure. Defaults to protofleet-ha-poc.
  HA_POC_PORT_MODE Port preset used by configure: coexist or standard. Defaults to coexist.
  HA_VIP_PREFIX    VIP CIDR prefix used by configure. Defaults to 24.

Coexist port preset:
  etcd client/peer: 12379/12380
  Postgres:         15432
  Patroni API:      18008
  fake Fleet HTTP:  14080

Standard port preset:
  etcd client/peer: 2379/2380
  Postgres:         5432
  Patroni API:      8008
  fake Fleet HTTP:  4080

Examples:
  ./scripts/pi-poc.sh lan-ip eth0
  HA_POC_PASSWORD='change-me' ./scripts/pi-poc.sh configure fleet-a 192.168.2.11 192.168.2.12 192.168.2.13 192.168.2.50 eth0
  ./scripts/pi-poc.sh install-deps
  ./scripts/pi-poc.sh start
  ./scripts/pi-poc.sh status
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

info() {
  echo "== $*"
}

load_env() {
  if [[ ! -f "${env_file}" ]]; then
    die "env file not found: ${env_file}. Run configure first."
  fi

  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
  apply_port_defaults
}

apply_port_defaults() {
  : "${HA_POC_PORT_MODE:=standard}"
  local default_etcd_client_port
  local default_etcd_peer_port
  local default_postgres_port
  local default_patroni_port
  local default_fake_fleet_port

  case "${HA_POC_PORT_MODE}" in
    coexist)
      default_etcd_client_port="12379"
      default_etcd_peer_port="12380"
      default_postgres_port="15432"
      default_patroni_port="18008"
      default_fake_fleet_port="14080"
      ;;
    standard)
      default_etcd_client_port="2379"
      default_etcd_peer_port="2380"
      default_postgres_port="5432"
      default_patroni_port="8008"
      default_fake_fleet_port="4080"
      ;;
    *)
      die "HA_POC_PORT_MODE must be coexist or standard; got ${HA_POC_PORT_MODE}"
      ;;
  esac

  : "${HA_ETCD_CLIENT_PORT:=${default_etcd_client_port}}"
  : "${HA_ETCD_PEER_PORT:=${default_etcd_peer_port}}"
  : "${HA_POSTGRES_PORT:=${default_postgres_port}}"
  : "${HA_PATRONI_PORT:=${default_patroni_port}}"
  : "${HA_FAKE_FLEET_PORT:=${default_fake_fleet_port}}"
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    die "${cmd} is required"
  fi
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    die "missing required environment variable: ${name}"
  fi
}

role_kind() {
  case "${HA_NODE_NAME:-}" in
    fleet-a | fleet-b) echo "fleet" ;;
    witness) echo "witness" ;;
    *) die "HA_NODE_NAME must be fleet-a, fleet-b, or witness; got ${HA_NODE_NAME:-unset}" ;;
  esac
}

compose_file() {
  if [[ "$(role_kind)" == "witness" ]]; then
    echo "${poc_dir}/docker-compose.witness.yaml"
  else
    echo "${poc_dir}/docker-compose.fleet.yaml"
  fi
}

compose() {
  docker compose --env-file "${env_file}" -f "$(compose_file)" "$@"
}

detect_interface() {
  local vip="$1"
  local iface
  iface="$(ip route get "${vip}" 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit }}')"
  if [[ -z "${iface}" ]]; then
    iface="$(ip route show default 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit }}')"
  fi
  printf '%s\n' "${iface:-eth0}"
}

lan_ip() {
  local iface="${1:-}"
  if [[ -z "${iface}" ]]; then
    iface="$(detect_interface "${HA_VIP:-1.1.1.1}")"
  fi
  ip -o -4 addr show dev "${iface}" scope global \
    | awk '{ sub(/\/.*/, "", $4); print $4; exit }'
}

is_tailscale_ip() {
  local ip="$1"
  local first
  local second
  IFS=. read -r first second _ _ <<<"${ip}"
  [[ "${first}" == "100" && "${second}" =~ ^[0-9]+$ && "${second}" -ge 64 && "${second}" -le 127 ]]
}

reject_tailscale_ip() {
  local label="$1"
  local ip="$2"
  if is_tailscale_ip "${ip}"; then
    die "${label}=${ip} looks like a Tailscale/CGNAT address. Use the Pi's LAN IP on eth0 for HA internals."
  fi
}

configure() {
  if [[ "$#" -lt 5 || "$#" -gt 6 ]]; then
    usage
    exit 2
  fi

  local role="$1"
  local fleet_a_ip="$2"
  local fleet_b_ip="$3"
  local witness_ip="$4"
  local vip="$5"
  local iface="${6:-}"
  local node_ip
  local priority
  local peer_ip

  reject_tailscale_ip "fleet-a-ip" "${fleet_a_ip}"
  reject_tailscale_ip "fleet-b-ip" "${fleet_b_ip}"
  reject_tailscale_ip "witness-ip" "${witness_ip}"
  reject_tailscale_ip "vip" "${vip}"

  case "${role}" in
    fleet-a)
      node_ip="${fleet_a_ip}"
      priority="110"
      peer_ip="${fleet_b_ip}"
      ;;
    fleet-b)
      node_ip="${fleet_b_ip}"
      priority="100"
      peer_ip="${fleet_a_ip}"
      ;;
    witness)
      node_ip="${witness_ip}"
      priority="0"
      peer_ip="${fleet_a_ip}"
      ;;
    *)
      die "role must be fleet-a, fleet-b, or witness"
      ;;
  esac

  if [[ -z "${iface}" ]]; then
    iface="$(detect_interface "${vip}")"
  fi

  local password="${HA_POC_PASSWORD:-protofleet-ha-poc}"
  local vip_prefix="${HA_VIP_PREFIX:-24}"
  local vip_router_id="${HA_VIP_ROUTER_ID:-74}"
  local port_mode="${HA_POC_PORT_MODE:-coexist}"
  local etcd_client_port="${HA_ETCD_CLIENT_PORT:-}"
  local etcd_peer_port="${HA_ETCD_PEER_PORT:-}"
  local postgres_port="${HA_POSTGRES_PORT:-}"
  local patroni_port="${HA_PATRONI_PORT:-}"
  local fake_fleet_port="${HA_FAKE_FLEET_PORT:-}"

  case "${port_mode}" in
    coexist)
      : "${etcd_client_port:=12379}"
      : "${etcd_peer_port:=12380}"
      : "${postgres_port:=15432}"
      : "${patroni_port:=18008}"
      : "${fake_fleet_port:=14080}"
      ;;
    standard)
      : "${etcd_client_port:=2379}"
      : "${etcd_peer_port:=2380}"
      : "${postgres_port:=5432}"
      : "${patroni_port:=8008}"
      : "${fake_fleet_port:=4080}"
      ;;
    *)
      die "HA_POC_PORT_MODE must be coexist or standard; got ${port_mode}"
      ;;
  esac

  cat > "${env_file}" <<EOF
# Generated by scripts/pi-poc.sh configure.
# POC-only credentials. Do not reuse this file for production Fleet.
HA_POC_PORT_MODE=${port_mode}
HA_CLUSTER_NAME=proto-fleet-ha-poc
HA_NODE_NAME=${role}
HA_NODE_IP=${node_ip}
HA_FLEET_A_IP=${fleet_a_ip}
HA_FLEET_B_IP=${fleet_b_ip}
HA_WITNESS_IP=${witness_ip}

HA_POSTGRES_SUPERUSER_PASSWORD=${password}
HA_REPLICATION_USER=replicator
HA_REPLICATION_PASSWORD=${password}
HA_REWIND_USER=rewind
HA_REWIND_PASSWORD=${password}

HA_ETCD_CLIENT_PORT=${etcd_client_port}
HA_ETCD_PEER_PORT=${etcd_peer_port}
HA_POSTGRES_PORT=${postgres_port}
HA_PATRONI_PORT=${patroni_port}
HA_FAKE_FLEET_PORT=${fake_fleet_port}

HA_POC_DB_DSN='postgres://postgres:${password}@${fleet_a_ip}:${postgres_port},${fleet_b_ip}:${postgres_port}/postgres?sslmode=disable&target_session_attrs=read-write'
HA_POC_HTTP_ADDR=0.0.0.0:${fake_fleet_port}
HA_POC_LEASE_TTL=6s
HA_POC_RENEW_INTERVAL=2s
HA_POC_ACQUIRE_INTERVAL=1s
HA_POC_HEARTBEAT_INTERVAL=1s
HA_POC_REQUIRE_MULTI_HOST_DSN=true
HA_POC_STATUS_TOKEN=

HA_VIP=${vip}
HA_VIP_CIDR=${vip}/${vip_prefix}
HA_VIP_INTERFACE=${iface}
HA_VIP_ROUTER_ID=${vip_router_id}
HA_VIP_AUTH_PASS=protofleet
HA_KEEPALIVED_PRIORITY=${priority}
HA_KEEPALIVED_PEER_IP=${peer_ip}

HA_ETCD_IMAGE=quay.io/coreos/etcd:v3.6.5
EOF
  chmod 0600 "${env_file}"
  info "wrote ${env_file}"
  info "role=${role} node_ip=${node_ip} vip=${vip} interface=${iface}"
  info "port_mode=${port_mode} etcd=${etcd_client_port}/${etcd_peer_port} postgres=${postgres_port} patroni=${patroni_port} fake_fleet=${fake_fleet_port}"
}

install_deps() {
  local include_keepalived="yes"
  if [[ -f "${env_file}" ]]; then
    load_env
    if [[ "$(role_kind)" == "witness" ]]; then
      include_keepalived="no"
    fi
  fi

  require_cmd sudo
  if ! command -v apt-get >/dev/null 2>&1; then
    die "install-deps supports Debian/Raspberry Pi OS hosts with apt-get"
  fi

  local packages=(git curl jq netcat-openbsd gettext-base tmux iproute2)
  if [[ "${include_keepalived}" == "yes" ]]; then
    packages+=(keepalived)
  fi

  info "installing OS packages: ${packages[*]}"
  sudo apt-get update
  sudo apt-get install -y "${packages[@]}"

  if ! command -v docker >/dev/null 2>&1; then
    cat >&2 <<'EOF'

Docker is not installed. Install it on each Pi, then reconnect so group changes apply:
  curl -fsSL https://get.docker.com | sh
  sudo usermod -aG docker "$USER"
  newgrp docker
EOF
    exit 2
  fi

  docker version >/dev/null
  docker compose version >/dev/null
  info "dependencies look present"
}

port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -H -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "(^|[.:])${port}$"
  else
    nc -z 127.0.0.1 "${port}" >/dev/null 2>&1
  fi
}

poc_has_containers() {
  compose ps -q 2>/dev/null | grep -q .
}

local_ports() {
  printf '%s\n' "${HA_ETCD_CLIENT_PORT}" "${HA_ETCD_PEER_PORT}"
  if [[ "$(role_kind)" == "fleet" ]]; then
    printf '%s\n' "${HA_POSTGRES_PORT}" "${HA_PATRONI_PORT}" "${HA_FAKE_FLEET_PORT}"
  fi
}

check_local_ports() {
  local fail_on_conflict="$1"
  local conflicts=()
  local port

  while IFS= read -r port; do
    if port_in_use "${port}"; then
      conflicts+=("${port}")
    fi
  done < <(local_ports)

  if [[ "${#conflicts[@]}" -eq 0 ]]; then
    info "local POC ports are free"
    return 0
  fi

  if poc_has_containers; then
    info "local POC ports already in use by this compose project: ${conflicts[*]}"
    return 0
  fi

  echo "warn: local ports already in use before POC start: ${conflicts[*]}" >&2
  echo "      This usually means an existing Fleet/Postgres/etcd/Patroni process is running." >&2
  if [[ "${fail_on_conflict}" == "yes" ]]; then
    die "stop the conflicting services or use dedicated Pis before starting this POC"
  fi
}

doctor() {
  load_env
  for name in HA_NODE_NAME HA_NODE_IP HA_FLEET_A_IP HA_FLEET_B_IP HA_WITNESS_IP; do
    require_env "${name}"
  done

  require_cmd curl
  require_cmd jq
  require_cmd nc
  require_cmd ip

  if command -v docker >/dev/null 2>&1; then
    docker compose version >/dev/null
  else
    echo "warn: docker is not installed" >&2
  fi

  if [[ "$(role_kind)" == "fleet" ]] && ! command -v keepalived >/dev/null 2>&1; then
    echo "warn: keepalived is not installed" >&2
  fi

  if [[ -n "${HA_VIP_INTERFACE:-}" ]]; then
    ip addr show dev "${HA_VIP_INTERFACE}" >/dev/null 2>&1 \
      && info "interface ${HA_VIP_INTERFACE} exists" \
      || echo "warn: interface ${HA_VIP_INTERFACE} not found" >&2
  fi

  for pair in \
    "HA_NODE_IP=${HA_NODE_IP}" \
    "HA_FLEET_A_IP=${HA_FLEET_A_IP}" \
    "HA_FLEET_B_IP=${HA_FLEET_B_IP}" \
    "HA_WITNESS_IP=${HA_WITNESS_IP}" \
    "HA_VIP=${HA_VIP:-}"; do
    local name="${pair%%=*}"
    local value="${pair#*=}"
    if [[ -n "${value}" ]] && is_tailscale_ip "${value}"; then
      echo "warn: ${name}=${value} looks like a Tailscale/CGNAT address; HA internals should use LAN IPs" >&2
    fi
  done

  if ! ip -o -4 addr show scope global | awk '{ sub(/\/.*/, "", $4); print $4 }' | grep -Fxq "${HA_NODE_IP}"; then
    echo "warn: HA_NODE_IP=${HA_NODE_IP} is not assigned on this host; run './scripts/pi-poc.sh lan-ip ${HA_VIP_INTERFACE:-eth0}' to confirm the LAN IP" >&2
  fi

  check_local_ports no

  if command -v docker >/dev/null 2>&1; then
    local possible
    possible="$(docker ps --format '{{.Names}} {{.Image}} {{.Ports}}' \
      | grep -Ei 'fleet|timescale|postgres|etcd|patroni|keepalived' || true)"
    if [[ -n "${possible}" ]]; then
      echo "warn: Docker containers that may conflict or confuse this POC:" >&2
      echo "${possible}" >&2
    fi
  fi

  "${script_dir}/preflight.sh" "${env_file}"
}

start() {
  load_env
  require_cmd docker
  require_cmd nc
  check_local_ports yes
  "${script_dir}/preflight.sh" "${env_file}"

  if [[ "$(role_kind)" == "witness" ]]; then
    compose up -d
  else
    compose up -d --build
    sudo "${script_dir}/install-keepalived.sh" "${env_file}"
  fi
}

status() {
  load_env
  if [[ "$(role_kind)" == "witness" ]]; then
    compose ps
    return 0
  fi
  "${script_dir}/ha-status.sh" "${env_file}"
}

active_host() {
  load_env
  require_env HA_VIP
  curl -fsS --max-time 2 "http://${HA_VIP}:${HA_FAKE_FLEET_PORT}/health/active" \
    | jq '{host_id, holder_id, active_healthy, lease_epoch, lease_expires_at}'
}

watch() {
  load_env
  "${script_dir}/watch-failover.sh" "${env_file}"
}

fail_app() {
  load_env
  [[ "$(role_kind)" == "fleet" ]] || die "fail-app only runs on fleet-a or fleet-b"
  if curl -fsS --max-time 1 "http://127.0.0.1:${HA_FAKE_FLEET_PORT}/health/active" >/dev/null 2>&1; then
    info "local fake-fleet is active; stopping it"
    compose stop fake-fleet
  else
    echo "local fake-fleet is not active. Current VIP active holder:" >&2
    active_host >&2 || true
    exit 1
  fi
}

fail_db() {
  load_env
  [[ "$(role_kind)" == "fleet" ]] || die "fail-db only runs on fleet-a or fleet-b"
  local role
  role="$(curl -fsS --max-time 2 "http://127.0.0.1:${HA_PATRONI_PORT}/patroni" | jq -r '.role // "unknown"')"
  info "local Patroni role is ${role}; stopping local patroni"
  compose stop patroni
}

restore() {
  load_env
  require_cmd docker
  if [[ "$(role_kind)" == "witness" ]]; then
    compose up -d
  else
    compose up -d
    sudo "${script_dir}/install-keepalived.sh" "${env_file}"
  fi
}

stop() {
  load_env
  if [[ "$(role_kind)" == "fleet" ]] && command -v systemctl >/dev/null 2>&1; then
    sudo systemctl stop keepalived || true
  fi
  compose down
}

reset() {
  load_env
  if [[ "${1:-}" != "--yes" ]]; then
    die "reset deletes local POC Docker volumes. Re-run as: ./scripts/pi-poc.sh reset --yes"
  fi
  if [[ "$(role_kind)" == "fleet" ]] && command -v systemctl >/dev/null 2>&1; then
    sudo systemctl stop keepalived || true
  fi
  compose down -v
}

cmd="${1:-}"
shift || true

case "${cmd}" in
  lan-ip) lan_ip "$@" ;;
  configure) configure "$@" ;;
  install-deps) install_deps ;;
  doctor) doctor ;;
  start) start ;;
  status) status ;;
  active-host) active_host ;;
  watch) watch ;;
  fail-app) fail_app ;;
  fail-db) fail_db ;;
  restore) restore ;;
  stop) stop ;;
  reset) reset "$@" ;;
  -h | --help | help | "") usage ;;
  *) usage; exit 2 ;;
esac
