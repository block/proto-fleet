#!/usr/bin/env bash
set -euo pipefail

# Fast contract checks for the declarative HA deployment files. Host setup
# behavior is covered by focused Go tests for the fleet-ha utility; this suite
# only checks the cross-file Compose and Patroni boundaries.

HA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_contains() {
    local file="$1"
    local text="$2"
    grep -Fq -- "$text" "$file" || fail "${file} does not contain: ${text}"
}

assert_not_contains() {
    local file="$1"
    local text="$2"
    if grep -Fq -- "$text" "$file"; then
        fail "${file} unexpectedly contains: ${text}"
    fi
}

test_compose_uses_one_host_identity() {
    local rendered
    rendered="$(mktemp)"
    trap 'rm -f "$rendered"' RETURN

    HA_NODE_NAME=ha-a \
    HA_NODE_IP=10.40.0.11 \
    HA_DB_A_IP=10.40.0.11 \
    HA_DB_B_IP=10.40.0.12 \
    HA_DCS_C_IP=10.40.0.13 \
    HA_DATA_DIR=/var/lib/proto-fleet/ha \
    HA_SECRETS_DIR=/etc/proto-fleet/ha \
        docker compose \
        --file "${HA_DIR}/compose.yaml" \
        --profile database \
        config >"$rendered"

    [[ "$(grep -c 'network_mode: host' "$rendered")" -eq 2 ]] ||
        fail "etcd and Patroni must both use host networking"
    assert_not_contains "$rendered" "restart:"
    assert_not_contains "$rendered" "ports:"
    assert_not_contains "$rendered" "127.0.0.1"
    assert_contains "$rendered" "https://10.40.0.11:2379"
    assert_contains "$rendered" "https://10.40.0.11:2380"
    assert_contains "$rendered" "auth-token=jwt"
    assert_contains "$rendered" "--peer-client-cert-auth=true"
}

test_patroni_contract() {
    local template="${HA_DIR}/patroni.yml.tmpl"
    local dockerfile="${HA_DIR}/patroni.Dockerfile"
    local compose="${HA_DIR}/compose.yaml"
    local entrypoint="${HA_DIR}/scripts/patroni-entrypoint.sh"
    local bootstrap="${HA_DIR}/scripts/patroni-post-bootstrap.sh"

    assert_contains "$template" 'connect_address: ${HA_NODE_IP}:5432'
    assert_contains "$template" 'connect_address: ${HA_NODE_IP}:8008'
    assert_contains "$template" "synchronous_mode: true"
    assert_contains "$template" "synchronous_mode_strict: true"
    assert_contains "$template" "sslmode: verify-full"
    assert_contains "$template" 'post_bootstrap: /usr/local/bin/patroni-post-bootstrap'
    assert_not_contains "$template" "0.0.0.0/0"
    assert_contains "$entrypoint" "render-patroni-config"
    assert_contains "$bootstrap" 'PGOPTIONS="-c synchronous_commit=local"'
    assert_contains "$bootstrap" 'psql --dbname="$connection_url" --set=ON_ERROR_STOP=1'
    assert_not_contains "$bootstrap" 'PGDATABASE="$connection_url"'

    [[ "$(grep -E '^[[:space:]]*USER[[:space:]]+' "$dockerfile" | tail -n 1)" == "USER postgres" ]] ||
        fail "Patroni image must default to the postgres user"
    awk '
        /^  patroni:$/ { in_patroni = 1; next }
        in_patroni && /^  [[:alnum:]_-]+:$/ { exit }
        in_patroni && /^[[:space:]]+user: root$/ { found = 1 }
        END { exit !found }
    ' "$compose" || fail "Patroni must start as root before its entrypoint drops privileges"
}

test_compose_uses_one_host_identity
test_patroni_contract

echo "HA deployment profile checks passed"
