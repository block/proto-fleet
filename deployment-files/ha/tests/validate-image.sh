#!/usr/bin/env bash
set -euo pipefail

# Validate the built Patroni image without booting a real HA cluster.
#
# Usage:
#   validate-image.sh IMAGE [EXPECTED_SOURCE_COMMIT]
#
# The optional commit check binds the image to the release source. The
# in-container checks then render a representative config, prove password
# escaping survives YAML parsing, and run Patroni's own config validator.

image="${1:?pass the Patroni image tag to validate}"
expected_source_commit="${2:-}"

# Qualification later compares this OCI label with the checkout and deployment
# bundle. Catch a missing or stale label while the image is still in CI.
if [[ -n "$expected_source_commit" ]]; then
    actual_source_commit="$(
        docker image inspect \
            --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
            "$image"
    )"
    [[ "$actual_source_commit" == "$expected_source_commit" ]] || {
        echo "image source commit is ${actual_source_commit}, expected ${expected_source_commit}" >&2
        exit 1
    }
fi

# Patroni's validator checks that configured DCS endpoints are reachable.
# Provide a local listener so CI can validate the image without a live cluster.
docker run --rm --entrypoint sh "$image" -c '
    set -eu
    set -- $(hostname -i)
    host_ip=$1
    export HA_NODE_NAME=ha-a
    export HA_NODE_IP=$host_ip
    export HA_DB_A_IP=$host_ip
    export HA_DB_B_IP=$host_ip
    export HA_DCS_C_IP=$host_ip
    python3 -c "
import socket
listener = socket.socket()
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind((\"0.0.0.0\", 2379))
listener.listen()
while True:
    connection, _ = listener.accept()
    connection.close()
" &
    listener_pid=$!
    trap "kill $listener_pid 2>/dev/null || true" EXIT

    python3 - "$host_ip" <<"PY"
import socket
import sys
import time

host = sys.argv[1]
for attempt in range(50):
    try:
        with socket.create_connection((host, 2379), timeout=0.2):
            break
    except OSError:
        if attempt == 49:
            raise SystemExit(
                f"TCP stub listener did not become ready at {host}:2379"
            )
        time.sleep(0.1)
PY

    # These values cover YAML-sensitive characters and scalar-looking strings.
    # If rendering loses quoting, the parsed values below will change type or
    # content even though the generated file still looks plausible.
    secrets=$(mktemp -d)
    printf "%s\n" "value # still a password" >"$secrets/patroni-etcd-password"
    printf "%s\n" "quoted \"value\": with space" >"$secrets/patroni-rest-password"
    printf "%s\n" "true" >"$secrets/postgres-superuser-password"
    printf "%s\n" "colon: space" >"$secrets/postgres-replication-password"

    config=$(mktemp)
    render-patroni-config /etc/patroni/patroni.yml.tmpl "$secrets" "$config"

    # Parse the rendered file with the same YAML implementation shipped in the
    # image before asking Patroni to validate the complete schema.
    python3 - "$config" <<"PY"
import sys
import yaml

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = yaml.safe_load(config_file)

assert config["etcd3"]["password"] == "value # still a password"
assert config["restapi"]["authentication"]["password"] == "quoted \"value\": with space"
assert config["postgresql"]["authentication"]["superuser"]["password"] == "true"
assert config["postgresql"]["authentication"]["replication"]["password"] == "colon: space"
PY

    patroni --validate-config --ignore-listen-port "$config"
'
