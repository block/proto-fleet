#!/bin/bash
# Shared library for Proto Fleet migration scripts
# Source this file at the top of each script: source "$(dirname "$0")/lib.sh"

# ============================================================================
# Shared Constants
# ============================================================================

# Tables in dependency order (parents before children)
MIGRATION_TABLES=(
    "role"
    "organization"
    "user"
    "user_organization"
    "session"
    "discovered_device"
    "device"
    "device_status"
    "device_pairing"
    "miner_credentials"
    "pool"
    "command_batch_log"
    "command_on_device_log"
    "queue_message"
    "errors"
)

# ============================================================================
# Error Handling Helpers
# ============================================================================

# Run a command quietly but show output on failure
# Usage: run_quiet command [args...]
# Returns: exit code of the command
run_quiet() {
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo "Command failed (exit $exit_code): $*" >&2
        [[ -n "$output" ]] && echo "$output" >&2
    fi
    return $exit_code
}

# Run a command that may fail (optional/best-effort)
# Shows a note on failure but always returns success
# Usage: run_optional "description" command [args...]
run_optional() {
    local desc="$1"
    shift
    local output
    # Use if ! to prevent set -e from triggering on command failure
    if ! output=$("$@" 2>&1); then
        echo "  ($desc skipped: ${output:-command failed})"
    fi
    return 0
}

# Run a command and capture output, failing the script on error
# Usage: output=$(run_or_fail command [args...])
run_or_fail() {
    local output exit_code
    output=$("$@" 2>&1)
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo "FATAL: Command failed (exit $exit_code): $*" >&2
        [[ -n "$output" ]] && echo "$output" >&2
        exit $exit_code
    fi
    echo "$output"
}

# ============================================================================
# Docker Helpers
# ============================================================================

# Run docker exec command quietly but show output on failure
# Usage: docker_exec_quiet container command [args...]
docker_exec_quiet() {
    local container="$1"
    shift
    run_quiet docker exec "$container" "$@"
}

# Run docker exec command that may fail
# Usage: docker_exec_optional "description" container command [args...]
docker_exec_optional() {
    local desc="$1"
    local container="$2"
    shift 2
    run_optional "$desc" docker exec "$container" "$@"
}

# ============================================================================
# PostgreSQL Helpers
# ============================================================================

# Run a psql query and capture output
# Requires POSTGRES_PASSWORD, POSTGRES_CONTAINER, POSTGRES_USER, POSTGRES_DATABASE to be set
# Usage: result=$(psql_run "SELECT 1;")
psql_run() {
    local query="$1"
    docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" "$POSTGRES_CONTAINER" psql \
        -U "$POSTGRES_USER" \
        -d "$POSTGRES_DATABASE" \
        -t -A -c "$query"
}

# Run a psql query quietly (suppress output on success, show on failure)
# Usage: psql_quiet "CREATE TABLE..."
psql_quiet() {
    local query="$1"
    local output exit_code
    output=$(psql_run "$query" 2>&1)
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo "PostgreSQL query failed:" >&2
        echo "  Query: $query" >&2
        echo "  Error: $output" >&2
    fi
    return $exit_code
}

# Run a psql query that may fail (optional/best-effort)
# Usage: psql_optional "index creation" "CREATE INDEX..."
psql_optional() {
    local desc="$1"
    local query="$2"
    # Combine declaration and assignment to prevent set -e from triggering
    # When local is part of the assignment, its exit status (0) takes precedence
    local output
    if ! output=$(psql_run "$query" 2>&1); then
        echo "  ($desc skipped: ${output:-unknown error})"
    elif [[ "$output" == *"ERROR"* ]]; then
        echo "  ($desc skipped: $output)"
    fi
    return 0
}

# Run a psql query and fail the script on error
# Usage: result=$(psql_or_fail "SELECT COUNT(*) FROM table;")
psql_or_fail() {
    local query="$1"
    local output exit_code
    output=$(psql_run "$query" 2>&1)
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo "FATAL: PostgreSQL query failed:" >&2
        echo "  Query: $query" >&2
        echo "  Error: $output" >&2
        exit $exit_code
    fi
    echo "$output"
}

# ============================================================================
# MySQL Helpers
# ============================================================================

# Run a mysql query and capture output
# Requires MYSQL_PWD (env), MYSQL_CONTAINER, MYSQL_USER, MYSQL_DATABASE to be set
# Usage: result=$(mysql_run "SELECT 1;")
mysql_run() {
    local query="$1"
    docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" "$MYSQL_CONTAINER" mysql \
        -u "$MYSQL_USER" \
        "$MYSQL_DATABASE" \
        -N -B -e "$query"
}

# Run a mysql query quietly (suppress output on success, show on failure)
# Usage: mysql_quiet "CREATE TABLE..."
mysql_quiet() {
    local query="$1"
    local output exit_code
    output=$(mysql_run "$query" 2>&1)
    exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        echo "MySQL query failed:" >&2
        echo "  Query: $query" >&2
        echo "  Error: $output" >&2
    fi
    return $exit_code
}

# ============================================================================
# Logging Helpers
# ============================================================================

# These use ANSI colors if stdout is a terminal
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

# ============================================================================
# Validation Helpers
# ============================================================================

# Require a Docker container to be running
# Usage: require_container "container-name" "MySQL"
require_container() {
    local container="$1"
    local name="${2:-$container}"

    if ! docker ps --format '{{.Names}}' | grep -q "^${container}$"; then
        echo "Error: $name container '${container}' is not running."
        echo "Available containers:"
        docker ps --format '  {{.Names}}'
        exit 1
    fi
}

# Require a password to be set (warns but allows empty for dev environments)
# Usage: require_password "$PASSWORD" "MySQL" "DB_PASSWORD"
require_password() {
    local password="$1"
    local db_name="$2"
    local hint="$3"

    if [[ -z "$password" ]]; then
        log_warn "$db_name password is empty (set $hint if this is not intentional)"
    fi
}

# Require a directory to exist
# Usage: require_directory "$DIR" "Import directory"
require_directory() {
    local dir="$1"
    local desc="${2:-Directory}"

    if [[ ! -d "$dir" ]]; then
        echo "Error: $desc not found: $dir"
        exit 1
    fi
}

# ============================================================================
# Utility Helpers
# ============================================================================

# Count data rows in CSV (excludes header and empty lines)
# Usage: count=$(csv_data_rows "$file")
csv_data_rows() {
    local count
    count=$(tail -n +2 "$1" | grep -c -v '^[[:space:]]*$' 2>/dev/null) || count=0
    echo "$count"
}

# Print a section header with separators
# Usage: print_section "MySQL Data Export"
print_section() {
    echo "=============================================="
    echo "$1"
    echo "=============================================="
}
