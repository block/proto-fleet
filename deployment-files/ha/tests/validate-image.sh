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
import io
import json
import logging
import runpy
from unittest.mock import Mock, patch
from patroni.dcs.etcd3 import Etcd3Client, InvalidAuthToken
from patroni.log import PatroniLogger
from patroni.config import Config
from patroni.postgresql.config import ConfigHandler

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = yaml.safe_load(config_file)

assert config["etcd3"]["password"] == "value # still a password"
assert config["restapi"]["authentication"]["password"] == "quoted \"value\": with space"
assert config["postgresql"]["authentication"]["superuser"]["password"] == "true"
assert config["postgresql"]["authentication"]["replication"]["password"] == "colon: space"

# Test the effective limits after a new cluster adopts its bootstrap configuration.
effective = Config(sys.argv[1], validator=None)
effective.set_dynamic_configuration(config["bootstrap"]["dcs"])
assert effective["postgresql"]["parameters"]["max_worker_processes"] == 19
assert effective["postgresql"]["parameters"]["max_connections"] == 1200

# Catch local parameters that Patroni overrides with cluster settings or defaults.
for name, value in config["postgresql"]["parameters"].items():
    if name in ConfigHandler.CMDLINE_OPTIONS:
        assert str(effective["postgresql"]["parameters"][name]) == str(value), name

# Exercise Patroni itself: a missing JSON dependency otherwise silently falls
# back to plain text, even when the configuration validator succeeds.
logger = PatroniLogger()
logger.reload_config(config["log"])
output = io.StringIO()
handler = logging.StreamHandler(output)
handler.setFormatter(logger.log_handler.formatter)
for level in (logging.INFO, logging.WARNING, logging.ERROR):
    handler.handle(logging.LogRecord("patroni", level, "", 0, "test message", (), None))
    record = json.loads(output.getvalue().splitlines()[-1])
    assert record["level"] == logging.getLevelName(level), record
    assert record["message"] == "test message", record
    assert record["timestamp"], record

# Run the packaged launcher, then exercise the upstream logger that otherwise
# includes an expired credential in its authentication-error record.
with patch("patroni.__main__.main") as main:
    runpy.run_path("/usr/local/bin/start-patroni", run_name="__main__")
    main.assert_called_once_with()
etcd_logger = logging.getLogger("patroni.dcs.etcd3")
etcd_logger.addHandler(handler)
etcd_logger.propagate = False
client = object.__new__(Etcd3Client)
client._reauthenticate = False
client._token = "synthetic-token-must-not-be-logged"
client.username = "test-user"
client.password = "test-password"
client.authenticate = Mock()
operation = Mock(side_effect=[InvalidAuthToken(), {"ok": True}])
assert client.handle_auth_errors(operation) == {"ok": True}
client.authenticate.assert_called_once()
record = json.loads(output.getvalue().splitlines()[-1])
assert record["message"] == "Invalid auth token: [redacted]", record
assert record["level"] == "ERROR", record
assert "synthetic-token-must-not-be-logged" not in output.getvalue()
etcd_logger.warning("Unrelated authentication failure: %s", "reason retained")
record = json.loads(output.getvalue().splitlines()[-1])
assert record["message"] == "Unrelated authentication failure: reason retained", record
assert record["level"] == "WARNING", record
PY

    patroni --validate-config --ignore-listen-port "$config"
'
