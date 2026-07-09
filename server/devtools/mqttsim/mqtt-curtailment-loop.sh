#!/bin/bash
set -euo pipefail

usage() {
    cat <<EOF
Usage: $0 <on|off> [interval_seconds]

Start two local Mosquitto brokers and publish Proto Fleet MQTT curtailment
signals until the script is stopped. Stopping the script removes both broker
containers.

Arguments:
    on                  Publish target=100, which means ON/full power.
    off                 Publish target=0, which means OFF/curtail.
    interval_seconds    Publish interval in seconds (default: 30).

Environment overrides:
    TOPIC               MQTT topic (default: maestro/target)
    NETWORK_NAME        Docker network to use. Defaults to server_fleet-network
                        when it exists; otherwise creates a temporary network.
    NETWORK_SUBNET      Subnet to use for a newly-created network
                        (default: first available private /24 candidate)
    PRIMARY_HOST        Primary broker container IP. Defaults to the selected
                        network's .240 address when static assignment is possible.
    SECONDARY_HOST      Secondary broker container IP. Defaults to the selected
                        network's .241 address when static assignment is possible.
    BROKER_PORT         MQTT port inside the Docker network (default: 1883)
    PUBLISH_TIMEOUT     Seconds before a broker publish/check is killed
                        (default: 10)
    IMAGE               Mosquitto image (default: eclipse-mosquitto:2)
    NAME_PREFIX         Docker container name prefix
                        (default: protofleet-mqtt-curtailment)

Configure Proto Fleet with:
    The script prints the exact broker hosts after Docker assigns or confirms
    them.
    Broker port:           $BROKER_PORT
    Broker transport:      tcp
    Topic:                 $TOPIC
    Payload format:        target_timestamp
    MQTT username:         proto-fleet
    MQTT password:         proto-fleet
EOF
}

MODE="${1:-}"
INTERVAL="${2:-30}"
TOPIC="${TOPIC:-maestro/target}"
NETWORK_NAME="${NETWORK_NAME:-}"
NETWORK_SUBNET="${NETWORK_SUBNET:-}"
PRIMARY_HOST="${PRIMARY_HOST:-}"
SECONDARY_HOST="${SECONDARY_HOST:-}"
BROKER_PORT="${BROKER_PORT:-1883}"
PUBLISH_TIMEOUT="${PUBLISH_TIMEOUT:-10}"
IMAGE="${IMAGE:-eclipse-mosquitto:2}"
NAME_PREFIX="${NAME_PREFIX:-protofleet-mqtt-curtailment}"
PRIMARY_CONTAINER="${NAME_PREFIX}-primary"
SECONDARY_CONTAINER="${NAME_PREFIX}-secondary"
WORK_DIR=""
NETWORK_CREATED=false

case "$MODE" in
    on|ON)
        MODE="on"
        WIRE_TARGET=100
        ;;
    off|OFF)
        MODE="off"
        WIRE_TARGET=0
        ;;
    -h|--help|"")
        usage
        exit 0
        ;;
    *)
        echo "Error: first argument must be 'on' or 'off'." >&2
        usage >&2
        exit 1
        ;;
esac

if ! [[ "$INTERVAL" =~ ^[0-9]+$ ]] || [[ "$INTERVAL" -le 0 ]]; then
    echo "Error: interval_seconds must be a positive integer." >&2
    exit 1
fi

if ! [[ "$BROKER_PORT" =~ ^[0-9]+$ ]] || [[ "$BROKER_PORT" -le 0 ]] || [[ "$BROKER_PORT" -gt 65535 ]]; then
    echo "Error: BROKER_PORT must be between 1 and 65535." >&2
    exit 1
fi

if ! [[ "$PUBLISH_TIMEOUT" =~ ^[0-9]+$ ]] || [[ "$PUBLISH_TIMEOUT" -le 0 ]]; then
    echo "Error: PUBLISH_TIMEOUT must be a positive integer." >&2
    exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
    echo "Error: docker is required." >&2
    exit 1
fi

cleanup() {
    set +e
    if [[ -n "${PRIMARY_CONTAINER:-}" || -n "${SECONDARY_CONTAINER:-}" ]]; then
        docker rm -f "$PRIMARY_CONTAINER" "$SECONDARY_CONTAINER" >/dev/null 2>&1 || true
    fi
    if [[ -n "${WORK_DIR:-}" ]]; then
        rm -rf "$WORK_DIR"
    fi
    if [[ "${NETWORK_CREATED:-false}" == true && -n "${NETWORK_NAME:-}" ]]; then
        docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
    fi
}

trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT

write_config() {
    WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/protofleet-mqtt.XXXXXX")"
    cat >"$WORK_DIR/mosquitto.conf" <<EOF
listener $BROKER_PORT 0.0.0.0
allow_anonymous true
persistence true
persistence_location /mosquitto/data/
log_dest stdout
connection_messages true
EOF
}

network_exists() {
    docker network inspect "$1" >/dev/null 2>&1
}

network_subnet() {
    docker network inspect -f '{{range .IPAM.Config}}{{if .Subnet}}{{.Subnet}}{{end}}{{end}}' "$1" 2>/dev/null
}

static_ip_for_subnet() {
    local subnet="$1"
    local last_octet="$2"
    local network bits oct1 oct2 oct3 oct4

    network="${subnet%/*}"
    bits="${subnet#*/}"
    IFS=. read -r oct1 oct2 oct3 oct4 <<EOF
$network
EOF

    if [[ "$bits" == "24" && -n "$oct1" && -n "$oct2" && -n "$oct3" ]]; then
        echo "$oct1.$oct2.$oct3.$last_octet"
        return 0
    fi

    return 1
}

run_with_timeout() {
    local timeout_seconds="$1"
    local command_pid timer_pid status
    shift

    "$@" &
    command_pid=$!
    (
        sleep "$timeout_seconds"
        kill -TERM "$command_pid" >/dev/null 2>&1 || true
        sleep 1
        kill -KILL "$command_pid" >/dev/null 2>&1 || true
    ) &
    timer_pid=$!

    set +e
    wait "$command_pid"
    status=$?
    set -e
    kill "$timer_pid" >/dev/null 2>&1 || true
    wait "$timer_pid" >/dev/null 2>&1 || true
    return "$status"
}

set_default_hosts_from_subnet() {
    local subnet="$1"
    local primary secondary

    primary="$(static_ip_for_subnet "$subnet" 240 || true)"
    secondary="$(static_ip_for_subnet "$subnet" 241 || true)"

    if [[ -z "$PRIMARY_HOST" && -n "$primary" ]]; then
        PRIMARY_HOST="$primary"
    fi
    if [[ -z "$SECONDARY_HOST" && -n "$secondary" ]]; then
        SECONDARY_HOST="$secondary"
    fi
}

create_network() {
    local subnet candidates

    candidates="${NETWORK_SUBNET:-192.168.2.0/24 172.31.240.0/24 172.31.241.0/24 10.255.240.0/24}"
    for subnet in $candidates; do
        if docker network create --driver bridge --subnet "$subnet" "$NETWORK_NAME" >/dev/null 2>&1; then
            NETWORK_CREATED=true
            NETWORK_SUBNET="$subnet"
            set_default_hosts_from_subnet "$subnet"
            return 0
        fi
    done

    echo "Error: could not create Docker network $NETWORK_NAME with an available private subnet." >&2
    echo "Set NETWORK_SUBNET to an unused /24, for example NETWORK_SUBNET=172.31.250.0/24." >&2
    return 1
}

select_network() {
    local subnet

    if [[ -z "$NETWORK_NAME" ]]; then
        if network_exists server_fleet-network; then
            NETWORK_NAME="server_fleet-network"
        else
            NETWORK_NAME="${NAME_PREFIX}-network"
        fi
    fi

    if network_exists "$NETWORK_NAME"; then
        subnet="$(network_subnet "$NETWORK_NAME")"
        if [[ -n "$subnet" ]]; then
            set_default_hosts_from_subnet "$subnet"
        fi
        return 0
    fi

    create_network
}

start_broker() {
    local container="$1"
    local host="$2"
    local args

    docker rm -f "$container" >/dev/null 2>&1 || true
    args=(
        docker run -d
        --name "$container" \
        --restart no \
        --network "$NETWORK_NAME"
    )
    if [[ -n "$host" ]]; then
        args+=(--ip "$host")
    fi
    args+=(
        -v "$WORK_DIR/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro" \
        "$IMAGE"
    )

    "${args[@]}" >/dev/null
}

wait_for_broker() {
    local container="$1"
    local attempt

    for attempt in $(seq 1 60); do
        if run_with_timeout "$PUBLISH_TIMEOUT" \
            docker exec "$container" mosquitto_pub -h 127.0.0.1 -p "$BROKER_PORT" -q 1 -t "__protofleet_probe" -m "ready" >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.25
    done

    echo "Error: MQTT broker $container did not become ready." >&2
    docker logs --tail=120 "$container" >&2 || true
    return 1
}

container_ip() {
    docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$1"
}

publish_once() {
    local ts payload
    ts="$(date -u +%s)"
    payload='{"target":'"$WIRE_TARGET"',"timestamp":'"$ts"'}'

    run_with_timeout "$PUBLISH_TIMEOUT" docker exec "$PRIMARY_CONTAINER" \
        mosquitto_pub -h 127.0.0.1 -p "$BROKER_PORT" -q 1 -r -t "$TOPIC" -m "$payload"
    run_with_timeout "$PUBLISH_TIMEOUT" docker exec "$SECONDARY_CONTAINER" \
        mosquitto_pub -h 127.0.0.1 -p "$BROKER_PORT" -q 1 -r -t "$TOPIC" -m "$payload"

    echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') published $MODE payload=$payload topic=$TOPIC"
}

write_config
select_network
if [[ -n "$PRIMARY_HOST" && -n "$SECONDARY_HOST" && "$PRIMARY_HOST" == "$SECONDARY_HOST" ]]; then
    echo "Error: PRIMARY_HOST and SECONDARY_HOST must be distinct." >&2
    exit 1
fi
start_broker "$PRIMARY_CONTAINER" "$PRIMARY_HOST"
start_broker "$SECONDARY_CONTAINER" "$SECONDARY_HOST"
wait_for_broker "$PRIMARY_CONTAINER"
wait_for_broker "$SECONDARY_CONTAINER"

if [[ -z "$PRIMARY_HOST" ]]; then
    PRIMARY_HOST="$(container_ip "$PRIMARY_CONTAINER")"
fi
if [[ -z "$SECONDARY_HOST" ]]; then
    SECONDARY_HOST="$(container_ip "$SECONDARY_CONTAINER")"
fi

if [[ -z "$PRIMARY_HOST" || -z "$SECONDARY_HOST" || "$PRIMARY_HOST" == "$SECONDARY_HOST" ]]; then
    echo "Error: could not determine distinct broker IPs." >&2
    exit 1
fi

cat <<EOF
MQTT curtailment loop started.

Proto Fleet source configuration:
  Primary broker host:   $PRIMARY_HOST
  Secondary broker host: $SECONDARY_HOST
  Broker port:           $BROKER_PORT
  Broker transport:      tcp
  Topic:                 $TOPIC
  Payload format:        target_timestamp
  MQTT username:         proto-fleet
  MQTT password:         proto-fleet
  Docker network:         $NETWORK_NAME

Publishing '$MODE' every ${INTERVAL}s. Press Ctrl-C to stop and remove containers.
EOF

while true; do
    publish_once
    sleep "$INTERVAL"
done
