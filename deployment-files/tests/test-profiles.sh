#!/bin/bash
# Validates the host profile env files against the compose files and the Go
# config defaults, then smoke-tests compose interpolation end to end.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROFILES_DIR="$REPO_ROOT/deployment-files/profiles"
BASE_COMPOSE="$REPO_ROOT/server/docker-compose.base.yaml"
PROD_COMPOSE="$REPO_ROOT/deployment-files/docker-compose.yaml"

FAILURES=0

fail() {
    echo "FAIL: $*" >&2
    FAILURES=$((FAILURES + 1))
}

pass() {
    echo "ok: $*"
}

# Last value wins, matching docker compose env-file semantics; falls back to
# the compose default when the profile does not set the key.
profile_get() { # file key default
    local v
    v=$(grep -E "^${2}=" "$1" 2>/dev/null | tail -1 | cut -d= -f2-)
    if [ -n "$v" ]; then echo "$v"; else echo "$3"; fi
}

to_mb() { # 512MB / 4GB -> megabytes
    local v="$1"
    case "$v" in
        *GB) echo $(( ${v%GB} * 1024 )) ;;
        *MB) echo "${v%MB}" ;;
        *) echo "unparseable-$v" ;;
    esac
}

secs() { # 10s -> 10
    echo "${1%s}"
}

is_int() {
    case "$1" in ''|*[!0-9]*) return 1 ;; *) return 0 ;; esac
}

# ----------------------------------------------------------------------------
# 1. Every profile key must be interpolated by a compose file
# ----------------------------------------------------------------------------

allowed_keys=$(grep -ohE '\$\{[A-Z_]+:-' "$BASE_COMPOSE" "$PROD_COMPOSE" | sed -E 's/^\$\{([A-Z_]+):-$/\1/' | sort -u)

for profile in "$PROFILES_DIR"/*.env; do
    while IFS= read -r line; do
        case "$line" in ''|'#'*) continue ;; esac
        key="${line%%=*}"
        if ! printf '%s\n' "$allowed_keys" | grep -qx "$key"; then
            fail "$(basename "$profile"): key $key is not interpolated by any compose file"
        fi
    done < "$profile"
done
pass "profile keys all resolve to compose interpolations"

# ----------------------------------------------------------------------------
# 2. Per-profile invariants
# ----------------------------------------------------------------------------

check_profile() { # name assumed_ram_mb
    local name="$1" ram_mb="$2" p="$PROFILES_DIR/$1.env"
    local bg par workers conns pool fetch staleness shared shared_mb

    [ -f "$p" ] || { fail "missing profile file $p"; return; }

    bg=$(profile_get "$p" PG_TS_MAX_BACKGROUND_WORKERS 8)
    par=$(profile_get "$p" PG_MAX_PARALLEL_WORKERS 8)
    workers=$(profile_get "$p" PG_MAX_WORKER_PROCESSES 19)
    conns=$(profile_get "$p" PG_MAX_CONNECTIONS 300)
    pool=$(profile_get "$p" DB_MAX_OPEN_CONNS 250)
    fetch=$(secs "$(profile_get "$p" TELEMETRY_FETCH_INTERVAL 5s)")
    staleness=$(secs "$(profile_get "$p" TELEMETRY_STALENESS_THRESHOLD 20s)")
    shared=$(profile_get "$p" PG_SHARED_BUFFERS 256MB)
    shared_mb=$(to_mb "$shared")

    # A malformed value would otherwise error inside [ ] and fail open
    local v
    for v in "$bg" "$par" "$workers" "$conns" "$pool" "$fetch" "$staleness" "$shared_mb"; do
        if ! is_int "$v"; then
            fail "$name: tunable value '$v' is not a plain integer"
            return
        fi
    done

    if [ "$workers" -ne $((bg + par + 3)) ]; then
        fail "$name: max_worker_processes=$workers != background($bg) + parallel($par) + 3"
    fi

    # ~20 connections reserved for grafana_ro, psql sessions, and superuser
    if [ $((pool + 20)) -ge "$conns" ]; then
        fail "$name: DB_MAX_OPEN_CONNS=$pool + 20 must stay below PG_MAX_CONNECTIONS=$conns"
    fi

    if [ "$staleness" -le "$fetch" ]; then
        fail "$name: STALENESS_THRESHOLD=${staleness}s must exceed FETCH_INTERVAL=${fetch}s"
    fi

    # The database shares the host with fleet-api, nginx, otel, and grafana
    if [ $((shared_mb * 4)) -gt "$ram_mb" ]; then
        fail "$name: shared_buffers=$shared exceeds 25% of the assumed ${ram_mb}MB host RAM"
    fi

    pass "$name invariants hold"
}

check_profile mini 4096
check_profile standard 16384
check_profile max 32768

# ----------------------------------------------------------------------------
# 3. Compose fleet-api defaults must match the Go config defaults
# ----------------------------------------------------------------------------

check_go_default() { # compose_key go_file go_env_name
    local compose_key="$1" go_file="$REPO_ROOT/$2" go_env="$3"
    local compose_default go_default

    compose_default=$(grep -ohE "\\$\\{${compose_key}:-[^}]*\\}" "$PROD_COMPOSE" | head -1 | sed -E 's/^.*:-([^}]*)\}$/\1/')
    go_default=$(grep -E "env:\"${go_env}\"" "$go_file" | grep -oE 'default:"[^"]*"' | head -1 | sed -E 's/^default:"([^"]*)"$/\1/')

    if [ -z "$compose_default" ] || [ -z "$go_default" ]; then
        fail "$compose_key: could not extract defaults (compose='$compose_default' go='$go_default')"
    elif [ "$compose_default" != "$go_default" ]; then
        fail "$compose_key: compose default '$compose_default' != Go default '$go_default' in $2"
    fi
}

check_go_default DB_MAX_OPEN_CONNS server/internal/infrastructure/db/config.go MAX_OPEN_CONNS
check_go_default DB_MAX_IDLE_CONNS server/internal/infrastructure/db/config.go MAX_IDLE_CONNS
check_go_default TELEMETRY_FETCH_INTERVAL server/internal/domain/telemetry/config.go FETCH_INTERVAL
check_go_default TELEMETRY_STALENESS_THRESHOLD server/internal/domain/telemetry/config.go STALENESS_THRESHOLD
check_go_default TELEMETRY_CONCURRENCY_LIMIT server/internal/domain/telemetry/config.go CONCURRENCY_LIMIT
check_go_default TELEMETRY_METRIC_TIMEOUT server/internal/domain/telemetry/config.go METRIC_TIMEOUT
check_go_default TIMESCALEDB_QUERY_TIMEOUT server/internal/infrastructure/timescaledb/config.go QUERY_TIMEOUT
check_go_default TIMESCALEDB_MAX_TIME_SERIES_ROWS server/internal/infrastructure/timescaledb/config.go MAX_TIME_SERIES_ROWS
check_go_default TIMESCALEDB_ASYNC_METRIC_COMMIT server/internal/infrastructure/timescaledb/config.go ASYNC_METRIC_COMMIT
check_go_default FLEET_COMMAND_MAX_WORKERS server/internal/domain/command/config.go MAX_WORKERS
pass "compose defaults checked against Go config defaults"

# ----------------------------------------------------------------------------
# 4. refresh_compose_env_args must survive strict-mode callers
# ----------------------------------------------------------------------------

# migrate-data.sh and rollback-migration.sh run under set -euo pipefail and
# call this on pre-profile .env files (no FLEET_PROFILE line)
STRICT_TMP=$(mktemp -d)
trap 'rm -rf "$STRICT_TMP"' EXIT
printf 'DB_USERNAME=fleet\n' > "$STRICT_TMP/.env"
if (
    set -euo pipefail
    PROJECT_ROOT="$STRICT_TMP"
    ENV_FILE="$STRICT_TMP/.env"
    source "$REPO_ROOT/deployment-files/scripts/lib.sh"
    refresh_compose_env_args
    [ "${#COMPOSE_ENV_ARGS[@]}" -eq 2 ] && [ "${COMPOSE_ENV_ARGS[1]}" = "$STRICT_TMP/.env" ]
) >/dev/null 2>&1; then
    pass "refresh_compose_env_args survives set -euo pipefail without FLEET_PROFILE"
else
    fail "refresh_compose_env_args breaks under set -euo pipefail when FLEET_PROFILE is missing"
fi

# Compose-accepted .env syntax (CRLF, quotes, case) must still resolve
mkdir -p "$STRICT_TMP/profiles"
: > "$STRICT_TMP/profiles/mini.env"
printf 'FLEET_PROFILE="MINI" \r\n' > "$STRICT_TMP/.env"
if (
    set -euo pipefail
    PROJECT_ROOT="$STRICT_TMP"
    ENV_FILE="$STRICT_TMP/.env"
    source "$REPO_ROOT/deployment-files/scripts/lib.sh"
    refresh_compose_env_args
    [ "${#COMPOSE_ENV_ARGS[@]}" -eq 4 ] && [ "${COMPOSE_ENV_ARGS[1]}" = "$STRICT_TMP/profiles/mini.env" ]
) >/dev/null 2>&1; then
    pass "quoted/CRLF/uppercase FLEET_PROFILE values normalize and resolve"
else
    fail "FLEET_PROFILE normalization: quoted/CRLF/uppercase value did not resolve to the profile"
fi

# An unresolvable profile must fall back to .env only, warn, and not abort
printf 'FLEET_PROFILE=bogus\n' > "$STRICT_TMP/.env"
if (
    set -euo pipefail
    PROJECT_ROOT="$STRICT_TMP"
    ENV_FILE="$STRICT_TMP/.env"
    source "$REPO_ROOT/deployment-files/scripts/lib.sh"
    refresh_compose_env_args 2>"$STRICT_TMP/warn.out"
    [ "${#COMPOSE_ENV_ARGS[@]}" -eq 2 ] && grep -q "FLEET_PROFILE='bogus'" "$STRICT_TMP/warn.out"
) >/dev/null 2>&1; then
    pass "invalid FLEET_PROFILE warns and falls back to defaults"
else
    fail "invalid FLEET_PROFILE handling: expected .env-only args plus a warning"
fi

# ----------------------------------------------------------------------------
# 5. Compose render smoke test (staged tarball layout)
# ----------------------------------------------------------------------------

if ! docker compose version >/dev/null 2>&1; then
    fail "docker compose is required for the render smoke test"
    echo "$FAILURES failure(s)"
    exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STRICT_TMP" "$STAGE"' EXIT
mkdir -p "$STAGE/server" "$STAGE/client"
cp "$PROD_COMPOSE" "$STAGE/"
cp "$BASE_COMPOSE" "$STAGE/server/"
cp -r "$PROFILES_DIR" "$STAGE/profiles"
printf 'FROM scratch\n' > "$STAGE/server/Dockerfile"
printf 'FROM scratch\n' > "$STAGE/client/Dockerfile"
printf 'DB_USERNAME=fleet\nDB_PASSWORD=test\nAUTH_CLIENT_SECRET_KEY=test\nENCRYPT_SERVICE_MASTER_KEY=test\n' > "$STAGE/base-secrets.env"
printf 'DB_USERNAME=fleet\nDB_PASSWORD=test\nAUTH_CLIENT_SECRET_KEY=test\nENCRYPT_SERVICE_MASTER_KEY=test\nPG_SHARED_BUFFERS=999MB\n' > "$STAGE/override.env"

render() { # env-file args...
    (cd "$STAGE" && docker compose "$@" -f docker-compose.yaml config 2>"$STAGE/render.err")
}

assert_rendered() { # description rendered_output expected...
    local desc="$1" out="$2"
    shift 2
    local expected
    for expected in "$@"; do
        if ! printf '%s\n' "$out" | grep -qF "$expected"; then
            fail "$desc: expected '$expected' in rendered compose"
            if [ -s "$STAGE/render.err" ]; then
                sed 's/^/    compose: /' "$STAGE/render.err" >&2
            fi
            return
        fi
    done
    pass "$desc"
}

out=$(render --env-file base-secrets.env)
assert_rendered "no-profile render keeps defaults" "$out" \
    "shared_buffers=256MB" "max_worker_processes=19" "wal_compression=off" \
    "shared_preload_libraries=timescaledb,pg_stat_statements" \
    "pg_stat_statements.track_utility=off" \
    "track_io_timing=on" "log_min_duration_statement=5000" \
    "log_parameter_max_length=0" "log_parameter_max_length_on_error=0" \
    'shm_size: "268435456"'

out=$(render --env-file profiles/mini.env --env-file base-secrets.env)
assert_rendered "mini render" "$out" \
    "max_connections=100" "max_worker_processes=9" "random_page_cost=2.0" \
    'shm_size: "134217728"'

out=$(render --env-file profiles/standard.env --env-file base-secrets.env)
assert_rendered "standard render" "$out" \
    "shared_buffers=4GB" "max_worker_processes=13" "wal_compression=lz4" \
    "FLEET_COMMAND_MAX_WORKERS: \"250\"" 'shm_size: "536870912"'

out=$(render --env-file profiles/max.env --env-file base-secrets.env)
assert_rendered "max render" "$out" \
    "shared_buffers=8GB" "max_worker_processes=23" "max_connections=300" \
    'shm_size: "2147483648"'

out=$(render --env-file profiles/standard.env --env-file override.env)
assert_rendered "operator .env overrides the profile" "$out" \
    "shared_buffers=999MB" "max_worker_processes=13"

# ----------------------------------------------------------------------------

if [ "$FAILURES" -gt 0 ]; then
    echo "$FAILURES failure(s)" >&2
    exit 1
fi
echo "all profile checks passed"
