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
    [[ "$(grep -c 'restart: on-failure' "$rendered")" -eq 2 ]] ||
        fail "etcd and Patroni must recover process failures without bypassing the systemd start gate"
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
    assert_contains "$template" "synchronous_mode_strict: false"
    assert_contains "$template" "sslmode: verify-full"
    assert_contains "$template" 'post_bootstrap: /usr/local/bin/patroni-post-bootstrap'
    assert_contains "$template" 'hostssl fleet grafana_ha_ro ${HA_DB_A_IP}/32 scram-sha-256'
    assert_not_contains "$template" "0.0.0.0/0"
    assert_contains "$entrypoint" "render-patroni-config"
    assert_contains "$bootstrap" 'psql --dbname="$connection_url" --set=ON_ERROR_STOP=1'
    assert_contains "$bootstrap" 'CREATE ROLE grafana_ha_ro LOGIN PASSWORD'
    assert_not_contains "$bootstrap" 'GRANT SELECT ON ALL TABLES'
    assert_not_contains "$bootstrap" 'ALTER DEFAULT PRIVILEGES'
    assert_not_contains "$bootstrap" 'PGDATABASE="$connection_url"'
    assert_contains "$dockerfile" 'ARG TIMESCALEDB_IMAGE_TAG=latest'
    assert_contains "$dockerfile" 'FROM proto-fleet-timescaledb:${TIMESCALEDB_IMAGE_TAG}'
    [[ "$(grep -E '^[[:space:]]*USER[[:space:]]+' "$dockerfile" | tail -n 1)" == "USER postgres" ]] ||
        fail "Patroni image must default to the postgres user"
    awk '
        /^  patroni:$/ { in_patroni = 1; next }
        in_patroni && /^  [[:alnum:]_-]+:$/ { exit }
        in_patroni && /^[[:space:]]+user: root$/ { found = 1 }
        END { exit !found }
    ' "$compose" || fail "Patroni must start as root before its entrypoint drops privileges"
}

test_fleet_ha_contract() {
    local rendered release_dir secret_mount_count
    rendered="$(mktemp)"
    release_dir="$(mktemp -d)"
    trap 'rm -f "$rendered"; rm -rf "$release_dir"' RETURN

    mkdir -p "${release_dir}/ha" "${release_dir}/server"
    cp "${HA_DIR}/../docker-compose.yaml" "${release_dir}/docker-compose.yaml"
    cp "${HA_DIR}/../docker-compose.alerts.yaml" "${release_dir}/docker-compose.alerts.yaml"
    cp "${HA_DIR}/fleet-compose.yaml" "${release_dir}/ha/fleet-compose.yaml"
    cp "${HA_DIR}/../../server/docker-compose.base.yaml" "${release_dir}/server/docker-compose.base.yaml"
    cp -r "${HA_DIR}/../../server/monitoring" "${release_dir}/server/monitoring"

    AUTH_CLIENT_SECRET_KEY=test-auth-secret \
    DB_USERNAME=fleet \
    DB_PASSWORD=test-db-password \
    DB_DSN=postgresql://fleet:test@10.40.0.11:5432/fleet \
    ENCRYPT_SERVICE_MASTER_KEY=test-master-key \
    GRAFANA_ADMIN_PASSWORD=test-grafana-admin-password \
    GRAFANA_DB_PASSWORD=test-grafana-db-password \
    GRAFANA_SECRET_KEY=test-grafana-secret-key \
    FLEET_ALERTS_WEBHOOK_TOKEN=test-alert-webhook-token \
    UPDATES_ENABLED=true \
    HA_NODE_IP=10.40.0.11 \
    HA_DB_A_IP=10.40.0.11 \
    HA_DB_B_IP=10.40.0.12 \
    HA_DCS_C_IP=10.40.0.13 \
    HA_VIRTUAL_IP=10.40.0.100 \
    HA_NETWORK_INTERFACE=eth0 \
    HA_SECRETS_DIR=/etc/proto-fleet/ha \
        docker compose \
        --file "${release_dir}/docker-compose.yaml" \
        --file "${release_dir}/docker-compose.alerts.yaml" \
        --file "${release_dir}/ha/fleet-compose.yaml" \
        config fleet-api fleet-client grafana >"$rendered"

    if grep -q '^  timescaledb:$' "$rendered"; then
        fail "HA Fleet targets must not include the standalone database service"
    fi
    assert_contains "$rendered" "HTTP_LISTEN_ADDRESS: 127.0.0.1:4000"
    assert_contains "$rendered" "UPDATES_ENABLED: \"false\""
    assert_contains "$rendered" "FLEET_HA_ENABLED: \"true\""
    assert_contains "$rendered" "https://10.40.0.11:2379,https://10.40.0.12:2379,https://10.40.0.13:2379"
    assert_contains "$rendered" "FLEET_HA_ENDPOINT_IP: 10.40.0.100"
    assert_contains "$rendered" "FLEET_HA_ENDPOINT_INTERFACE: eth0"
    assert_contains "$rendered" "FLEET_ALERTS_ENABLED: \"true\""
    assert_contains "$rendered" "FLEET_PUBLIC_URL: https://10.40.0.100"
    assert_contains "$rendered" "sleep 15; exec /app/fleetd"
    [[ "$(grep -c 'restart: on-failure' "$rendered")" -eq 3 ]] ||
        fail "Fleet services must restart process failures without bypassing the systemd start gate"
    assert_contains "$rendered" "NET_RAW"
    assert_not_contains "$rendered" "NET_ADMIN"
    assert_not_contains "$rendered" "/app/dlv"
    assert_contains "$rendered" "source: /etc/proto-fleet/ha/service-ca.crt"
    assert_contains "$rendered" "source: /etc/proto-fleet/ha/fleet-etcd-password"
    assert_contains "$rendered" "source: /etc/proto-fleet/ha/node.env"
    assert_contains "$rendered" "source: /etc/proto-fleet/ha/fleet-client.crt"
    assert_contains "$rendered" "target: /etc/nginx/ssl/cert.pem"
    assert_contains "$rendered" "source: /etc/proto-fleet/ha/fleet-client.key"
    assert_contains "$rendered" "target: /etc/nginx/ssl/key.pem"
    assert_contains "$rendered" "GF_SERVER_HTTP_ADDR: 127.0.0.1"
    assert_contains "$rendered" "GF_SERVER_HTTP_PORT: \"3030\""
    assert_contains "$rendered" "GRAFANA_DB_USERNAME: grafana_ha_ro"
    assert_contains "$rendered" "FLEET_ALERTS_GRAFANA_PASSWORD: test-grafana-admin-password"
    assert_contains "$rendered" "target: /etc/grafana/provisioning/alerting/proto-fleet-ha-rules.yaml"
    assert_not_contains "$rendered" "target: /etc/grafana/provisioning/alerting/proto-fleet-rules.yaml"
    assert_contains "$rendered" "target: /etc/grafana/proto-fleet-ha/service-ca.crt"
    assert_contains "$rendered" "/api/v1/provisioning/alert-rules/protofleet-ha-readiness"
    secret_mount_count="$(grep -c 'source: /etc/proto-fleet/ha/' "$rendered")"
    [[ "$secret_mount_count" -eq 6 ]] || fail "Fleet services must mount only their required HA secret files"
    assert_not_contains "$rendered" "source: ${release_dir}/ssl"
    assert_not_contains "$rendered" "/run/proto-fleet-updater"

    assert_contains "${HA_DIR}/scripts/check-fleet-active.sh" '--cacert "$service_ca"'
    assert_contains "${HA_DIR}/scripts/check-fleet-active.sh" '--connect-to "${virtual_ip}:443:127.0.0.1:443"'
    assert_contains "${HA_DIR}/scripts/check-fleet-active.sh" "--noproxy '*'"
    assert_not_contains "${HA_DIR}/scripts/check-fleet-active.sh" "--insecure"
    assert_contains "${HA_DIR}/keepalived-systemd.conf.tmpl" "Restart=on-failure"
    assert_contains "${HA_DIR}/keepalived-systemd.conf.tmpl" "After=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/keepalived-systemd.conf.tmpl" "PartOf=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/proto-fleet-ha-keepalived.conf" "Wants=keepalived.service"
    assert_contains "${HA_DIR}/keepalived-systemd.conf.tmpl" 'ExecStopPost=/usr/sbin/ip address flush to ${HA_VIRTUAL_IP}/32 dev ${HA_NETWORK_INTERFACE}'
    assert_not_contains "${HA_DIR}/firewall.nft.tmpl" "destroy table"
    assert_contains "${HA_DIR}/firewall.nft.tmpl" "tcp dport 40000 drop"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "ExecStartPre=-/usr/sbin/nft add table inet proto_fleet_ha"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "ExecStartPre=/usr/sbin/nft -c -f /etc/proto-fleet/ha/firewall-replace.nft"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "ExecStart=/usr/sbin/nft -f /etc/proto-fleet/ha/firewall-replace.nft"
    assert_contains "${HA_DIR}/firewall-replace.nft" "delete table inet proto_fleet_ha"
    assert_contains "${HA_DIR}/firewall-replace.nft" 'include "/etc/proto-fleet/ha/firewall.nft"'
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "BindsTo=nftables.service"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "After=nftables.service"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "PartOf=nftables.service"
    assert_contains "${HA_DIR}/proto-fleet-ha-firewall.service" "Before=docker.service proto-fleet-ha.service"
    assert_contains "${HA_DIR}/nftables-systemd.conf" "ExecReload="
    assert_contains "${HA_DIR}/nftables-systemd.conf" "ExecReload=/usr/sbin/nft -f /etc/proto-fleet/ha/nftables-reload.conf"
    assert_not_contains "${HA_DIR}/nftables-systemd.conf" "ExecStart"
    assert_not_contains "${HA_DIR}/nftables-systemd.conf" "ExecStop"
    assert_contains "${HA_DIR}/nftables-reload.conf" 'include "/etc/nftables.conf"'
    assert_contains "${HA_DIR}/nftables-reload.conf" 'include "/etc/proto-fleet/ha/firewall.nft"'
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "Requires=proto-fleet-ha-firewall.service docker.service"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "BindsTo=docker.service"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "PartOf=docker.service"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "ExecStart=/opt/proto-fleet/deployment/ha/fleet-ha start"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "ExecStopPost=/opt/proto-fleet/deployment/ha/fleet-ha stop"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "StartLimitIntervalSec=5min"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "StartLimitBurst=3"
    assert_contains "${HA_DIR}/proto-fleet-ha.service" "Restart=on-failure"
    assert_contains "${HA_DIR}/docker-systemd.conf" "Requires=proto-fleet-ha-firewall.service"
    assert_contains "${HA_DIR}/docker-systemd.conf" "PartOf=nftables.service"
    assert_not_contains "${HA_DIR}/docker-systemd.conf" "Wants=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/docker-ha-recovery-systemd.conf" "Wants=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/updater-systemd.conf" "ReadWritePaths=/etc/proto-fleet/ha"
    assert_contains "${HA_DIR}/updater-systemd.conf" "After=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/updater-systemd.conf" "PartOf=proto-fleet-ha.service"
    assert_contains "${HA_DIR}/updater-systemd.conf" "StartLimitIntervalSec=0"
    assert_contains "${HA_DIR}/ha-updater-systemd.conf" "EnvironmentFile=-/etc/proto-fleet/updater.env"
    assert_contains "${HA_DIR}/ha-updater-systemd.conf" "Wants=proto-fleet-updater.service"
    assert_contains "${HA_DIR}/ha-updater-systemd.conf" "ExecStartPre=/usr/local/libexec/proto-fleet/proto-fleet-updater --deployment-mode ha --self-update-path /usr/local/libexec/proto-fleet/proto-fleet-updater --repair-startup"
    assert_not_contains "${HA_DIR}/ha-updater-systemd.conf" "Requires=proto-fleet-updater.service"

    for nginx_config in "${HA_DIR}/../client/nginx.http.conf" "${HA_DIR}/../client/nginx.https.conf"; do
        assert_contains "$nginx_config" "location ^~ /api-proxy/health/ha"
        assert_contains "$nginx_config" "return 404;"
    done
}

test_compose_uses_one_host_identity
test_patroni_contract
test_fleet_ha_contract

echo "HA deployment profile checks passed"
