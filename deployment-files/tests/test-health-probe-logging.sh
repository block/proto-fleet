#!/usr/bin/env bash
# Exercise the packaged nginx configs, including the per-response role marker.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAGE=$(mktemp -d)
CONTAINER=
cleanup() {
    if [ -n "$CONTAINER" ]; then docker rm -f "$CONTAINER" >/dev/null; fi
    rm -rf "$STAGE"
}
trap cleanup EXIT

mkdir "$STAGE/ssl"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
    -keyout "$STAGE/ssl/key.pem" -out "$STAGE/ssl/cert.pem" >/dev/null 2>&1
cat >"$STAGE/upstream.conf" <<'EOF'
server {
    listen 4000;
    access_log off;
    location = /health/active {
        add_header X-Proto-Fleet-HA-Role $http_x_test_passive_role always;
        if ($http_x_test_case = active) { return 200; }
        return 503;
    }
}
EOF

for protocol in http https; do
    CONTAINER="fleet-health-log-test-$(basename "$STAGE")-$protocol"
    docker run --rm -d --name "$CONTAINER" \
        -p 127.0.0.1::80 -p 127.0.0.1::443 \
        -v "$REPO_ROOT/deployment-files/client/nginx.$protocol.conf:/etc/nginx/conf.d/default.conf:ro" \
        -v "$STAGE/upstream.conf:/etc/nginx/conf.d/test-upstream.conf:ro" \
        -v "$STAGE/ssl:/etc/nginx/ssl:ro" nginx:1.29.0-alpine >/dev/null
    port=80
    if [ "$protocol" = https ]; then port=443; fi
    binding=$(docker port "$CONTAINER" "$port/tcp")
    url="$protocol://127.0.0.1:${binding##*:}/api-proxy/health/active"
    for attempt in {1..50}; do
        if curl -ksS --max-time 1 "$url" -o /dev/null 2>/dev/null; then break; fi
        sleep 0.1
    done

    probe() {
        local case_id="$1"
        shift
        curl -ksS --max-time 5 -D "$STAGE/headers" -o /dev/null -w '%{http_code}' \
            -A ELB-HealthChecker/2.0 -e "probe-$case_id" "$@" "$url"
    }
    last_log() {
        local line attempt
        for attempt in {1..20}; do
            line=$(docker logs "$CONTAINER" 2>&1 | grep -F '"probe-'"$1"'"' | tail -1) || true
            if [ -n "$line" ]; then printf '%s\n' "$line"; return; fi
            sleep 0.1
        done
        echo "missing access log for probe-$1" >&2
        return 1
    }
    [ "$(probe passive -H 'X-Test-Passive-Role: passive')" = 503 ]
    grep -qi '^X-Proto-Fleet-HA-Role: passive' "$STAGE/headers"
    last_log passive | grep -Eq '"ELB-HealthChecker/2\.0" "-" ha_role="passive"$'

    # A request header cannot forge the marker: nginx logs the upstream response.
    [ "$(probe unknown -H 'X-Proto-Fleet-HA-Role: passive')" = 503 ]
    if grep -qi '^X-Proto-Fleet-HA-Role:' "$STAGE/headers"; then
        echo "unexpected role header" >&2
        exit 1
    fi
    last_log unknown | grep -Eq 'ha_role="-"$'

    [ "$(probe active -H 'X-Test-Case: active')" = 200 ]
    if grep -qi '^X-Proto-Fleet-HA-Role:' "$STAGE/headers"; then
        echo "unexpected role header" >&2
        exit 1
    fi
    last_log active | grep -Eq 'ha_role="-"$'

    [ "$(probe forwarded -H 'X-Test-Passive-Role: passive' -H 'X-Forwarded-For: 203.0.113.9')" = 503 ]
    last_log forwarded | grep -Eq '"ELB-HealthChecker/2\.0" "203\.0\.113\.9" ha_role="passive"$'
    echo "ok: $protocol health logs distinguish passive, unknown, active, and forwarded requests"

    docker rm -f "$CONTAINER" >/dev/null
    CONTAINER=
done
