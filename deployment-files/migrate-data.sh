#!/bin/bash
set -euo pipefail

# Proto Fleet Data Migration Script
# Migrates data from MySQL/InfluxDB to PostgreSQL/TimescaleDB
#
# This script orchestrates the complete migration process:
# 1. Validates prerequisites and disk space
# 2. Exports data from MySQL and InfluxDB
# 3. Starts new TimescaleDB container
# 4. Imports data into PostgreSQL/TimescaleDB
# 5. Verifies migration success

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
SCRIPTS_DIR="$SCRIPT_DIR/scripts"

source "$SCRIPTS_DIR/lib.sh"

# Configuration
MIGRATION_DIR="${MIGRATION_DIR:-/tmp/proto-fleet-migration-$(date +%Y%m%d-%H%M%S)}"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
ENV_FILE="$PROJECT_ROOT/.env"

# Environment variables will be loaded in preflight_checks after validating .env exists

# Container names (will be prefixed with project name)
# Derive from directory name to match docker compose default behavior
PROJECT_NAME="${PROJECT_NAME:-$(basename "$PROJECT_ROOT")}"
MYSQL_CONTAINER="${PROJECT_NAME}-mysql-1"
INFLUXDB_CONTAINER="${PROJECT_NAME}-influxdb-1"
TIMESCALEDB_CONTAINER="${PROJECT_NAME}-timescaledb-1"

# Migration phase tracking for error recovery guidance
MIGRATION_PHASE="init"

# Error handling with phase-specific recovery guidance
cleanup_on_error() {
    log_error "Migration failed at phase: $MIGRATION_PHASE"
    echo ""
    case "$MIGRATION_PHASE" in
        "init"|"preflight"|"export")
            log_error "Old containers should still be intact."
            log_error "You can safely retry the migration."
            ;;
        "transition")
            log_error "System may be in inconsistent state - old containers stopped but new stack not ready."
            log_error "Run: ./rollback-migration.sh to restore old containers"
            log_error "Or manually: docker compose up -d mysql influxdb"
            ;;
        "import")
            log_error "New database is running but data import incomplete."
            log_error "Options:"
            log_error "  1. Retry import: $0 --skip-export --export-dir $MIGRATION_DIR"
            log_error "  2. Rollback: ./rollback-migration.sh"
            ;;
        "verify")
            log_error "Migration may have succeeded but verification failed."
            log_error "Check the data manually before proceeding."
            ;;
        *)
            log_error "Unknown phase. Check system state manually."
            ;;
    esac
    echo ""
    log_error "Old data volumes have been preserved."
    exit 1
}
trap cleanup_on_error ERR

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Migrate Proto Fleet data from MySQL/InfluxDB to PostgreSQL/TimescaleDB.

Options:
    --skip-export           Skip export phase (use existing exported data)
    --skip-import           Skip import phase (only export data)
    --export-dir DIR        Export/Import directory (default: /tmp/proto-fleet-migration-TIMESTAMP)
    --dry-run               Show what would be done without making changes
    --mysql-only            Only migrate MySQL data (skip InfluxDB)
    --influxdb-only         Only migrate InfluxDB telemetry (skip MySQL)
    -h, --help              Show this help message

This script will:
  1. Export all data from MySQL and InfluxDB
  2. Stop the old MySQL/InfluxDB containers
  3. Start the new TimescaleDB container
  4. Import data into PostgreSQL/TimescaleDB
  5. Verify the migration

Old volumes are preserved until you manually remove them.

EOF
    exit 0
}

# Parse command line arguments
SKIP_EXPORT=false
SKIP_IMPORT=false
DRY_RUN=false
MYSQL_ONLY=false
INFLUXDB_ONLY=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-export) SKIP_EXPORT=true; shift ;;
        --skip-import) SKIP_IMPORT=true; shift ;;
        --export-dir) MIGRATION_DIR="$2"; shift 2 ;;
        --dry-run) DRY_RUN=true; shift ;;
        --mysql-only) MYSQL_ONLY=true; shift ;;
        --influxdb-only) INFLUXDB_ONLY=true; shift ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

# Validate mutually exclusive options
if [[ "$MYSQL_ONLY" == true ]] && [[ "$INFLUXDB_ONLY" == true ]]; then
    echo "Error: --mysql-only and --influxdb-only are mutually exclusive"
    exit 1
fi

# ============================================================================
# Pre-flight Checks
# ============================================================================

preflight_checks() {
    MIGRATION_PHASE="preflight"
    log_info "Running pre-flight checks..."

    # Check Docker is running
    if ! docker info > /dev/null 2>&1; then
        log_error "Docker daemon is not running."
        exit 1
    fi
    log_success "  Docker is running"

    # Check for required scripts
    for script in export-mysql.sh export-influxdb.sh import-postgres.sh import-timescaledb.sh; do
        if [[ ! -f "$SCRIPTS_DIR/$script" ]]; then
            log_error "Required script not found: $SCRIPTS_DIR/$script"
            exit 1
        fi
    done
    log_success "  Migration scripts found"

    # Check for .env file
    if [[ ! -f "$ENV_FILE" ]]; then
        log_error "Environment file not found: $ENV_FILE"
        exit 1
    fi
    log_success "  Environment file found"

    # Load environment variables
    set -a
    source "$ENV_FILE"
    set +a

    # Validate environment variables
    if [[ -z "${DB_PASSWORD:-}" ]]; then
        log_warn "  DB_PASSWORD is empty (this may be intentional for dev environments)"
    else
        log_success "  Database password configured"
    fi

    # Check for old MySQL volume
    local mysql_vol
    mysql_vol=$(docker volume ls -q | grep -E "${PROJECT_NAME}[-_]mysql$" || true)
    if [[ -z "$mysql_vol" ]]; then
        log_warn "No MySQL volume found. Nothing to migrate."
        exit 0
    fi
    log_success "  MySQL volume found: $mysql_vol"

    # Check for old InfluxDB volume
    local influx_vol
    influx_vol=$(docker volume ls -q | grep -E "${PROJECT_NAME}[-_]influxdb" | head -1 || true)
    if [[ -n "$influx_vol" ]]; then
        log_success "  InfluxDB volume found: $influx_vol"
    else
        log_warn "  No InfluxDB volume found (telemetry will not be migrated)"
    fi

    # Check disk space (require at least 10GB free)
    # Check parent directory since MIGRATION_DIR may not exist yet
    local check_dir free_space
    check_dir=$(dirname "$MIGRATION_DIR")
    [[ ! -d "$check_dir" ]] && check_dir="."
    # Use -BG on Linux, -g on macOS (BSD)
    free_space=$(df -BG "$check_dir" 2>/dev/null | tail -1 | awk '{print $4}' | tr -d 'G' || \
                 df -g "$check_dir" 2>/dev/null | tail -1 | awk '{print $4}' || \
                 echo "0")
    if [[ "$free_space" -lt 10 ]]; then
        log_warn "Low disk space: ${free_space}GB free. Recommended: 10GB+"
        read -p "Continue anyway? (y/N): " continue_anyway
        if [[ ! "$continue_anyway" =~ ^[Yy]$ ]]; then
            exit 1
        fi
    else
        log_success "  Disk space OK: ${free_space}GB free"
    fi

    echo ""
}

# ============================================================================
# Export Phase
# ============================================================================

export_data() {
    MIGRATION_PHASE="export"
    log_info "Starting data export..."
    echo ""

    # Create migration directory
    mkdir -p "$MIGRATION_DIR"
    log_info "Export directory: $MIGRATION_DIR"

    # Export MySQL data (unless --influxdb-only)
    if [[ "$INFLUXDB_ONLY" != true ]]; then
        # Ensure MySQL container is running
        if ! docker ps --format '{{.Names}}' | grep -q "^${MYSQL_CONTAINER}$"; then
            log_info "Starting MySQL container..."
            docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up -d mysql
            # Wait for MySQL to be ready using mysqladmin ping
            log_info "Waiting for MySQL to be ready..."
            local retries=30
            while [[ $retries -gt 0 ]]; do
                if docker exec -e MYSQL_PWD="${DB_PASSWORD}" "$MYSQL_CONTAINER" \
                    mysqladmin -u "${DB_USERNAME:-fleet_user}" ping > /dev/null 2>&1; then
                    break
                fi
                retries=$((retries - 1))
                sleep 2
            done
            if [[ $retries -eq 0 ]]; then
                log_error "MySQL failed to start within timeout."
                exit 1
            fi
        fi

        echo ""
        log_info "Exporting MySQL data..."
        EXPORT_DIR="$MIGRATION_DIR/mysql" \
        MYSQL_CONTAINER="$MYSQL_CONTAINER" \
        "$SCRIPTS_DIR/export-mysql.sh"
    else
        log_info "Skipping MySQL export (--influxdb-only)"
    fi

    # Export InfluxDB data (unless --mysql-only)
    if [[ "$MYSQL_ONLY" != true ]]; then
        if docker ps -a --format '{{.Names}}' | grep -q "^${INFLUXDB_CONTAINER}$"; then
            # Ensure InfluxDB container is running
            if ! docker ps --format '{{.Names}}' | grep -q "^${INFLUXDB_CONTAINER}$"; then
                log_info "Starting InfluxDB container..."
                docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up -d influxdb
                sleep 5  # Wait for InfluxDB to be ready
            fi

            echo ""
            log_info "Exporting InfluxDB telemetry data..."
            EXPORT_DIR="$MIGRATION_DIR/influxdb" \
            INFLUXDB_CONTAINER="$INFLUXDB_CONTAINER" \
            "$SCRIPTS_DIR/export-influxdb.sh"
        else
            log_warn "InfluxDB container not found. Skipping telemetry export."
        fi
    else
        log_info "Skipping InfluxDB export (--mysql-only)"
    fi

    log_success "Export phase complete!"
}

# ============================================================================
# Transition Phase
# ============================================================================

transition_to_new_stack() {
    MIGRATION_PHASE="transition"
    log_info "Transitioning to new database stack..."
    echo ""

    # Stop old database containers (not the entire stack, to avoid disrupting other services)
    # Use docker compose stop to properly handle network cleanup
    log_info "Stopping old database containers..."
    docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" stop mysql influxdb 2>/dev/null || true

    # The docker-compose.yaml should already be updated for TimescaleDB
    # Start new TimescaleDB container (uses migration profile since it's not started by default)
    log_info "Starting TimescaleDB container..."
    docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" --profile migration up -d timescaledb

    # Wait for TimescaleDB to be healthy
    # pg_isready only checks if PostgreSQL accepts connections, not if it's fully initialized
    # so we also verify we can actually run a query
    log_info "Waiting for TimescaleDB to be ready..."
    local retries=30
    while [[ $retries -gt 0 ]]; do
        if docker exec -e PGPASSWORD="${DB_PASSWORD}" "$TIMESCALEDB_CONTAINER" \
            pg_isready -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" > /dev/null 2>&1; then
            # Also verify we can actually query
            if docker exec -e PGPASSWORD="${DB_PASSWORD}" "$TIMESCALEDB_CONTAINER" \
                psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" -c "SELECT 1;" > /dev/null 2>&1; then
                break
            fi
        fi
        retries=$((retries - 1))
        sleep 2
    done

    if [[ $retries -eq 0 ]]; then
        log_error "TimescaleDB failed to start within timeout."
        exit 1
    fi

    log_success "TimescaleDB is ready!"
    echo ""

    # Run database migrations directly via psql
    log_info "Running database migrations..."

    # Check if migrations directory exists in the deployment
    # Primary: server/migrations (post-migration), Fallback: server/migrations-pg (during transition)
    local migrations_dir="$PROJECT_ROOT/server/migrations"
    if [[ ! -d "$migrations_dir" ]]; then
        migrations_dir="$SCRIPT_DIR/../server/migrations"
    fi
    if [[ ! -d "$migrations_dir" ]]; then
        migrations_dir="$PROJECT_ROOT/server/migrations-pg"
    fi
    if [[ ! -d "$migrations_dir" ]]; then
        migrations_dir="$SCRIPT_DIR/../server/migrations-pg"
    fi

    if [[ -d "$migrations_dir" ]]; then
        # Run each migration file in order (sorted by version number)
        while IFS= read -r migration_file; do
            if [[ -f "$migration_file" ]]; then
                local filename
                filename=$(basename "$migration_file")
                log_info "  Running migration: $filename"
                local output
                if output=$(docker exec -i -e PGPASSWORD="${DB_PASSWORD}" "$TIMESCALEDB_CONTAINER" \
                    psql -U "${DB_USERNAME:-fleet}" -d "${DB_NAME:-fleet}" \
                    -v ON_ERROR_STOP=1 -f "/dev/stdin" < "$migration_file" 2>&1); then
                    # Check for specific errors in output even if command succeeded
                    if echo "$output" | grep -qiE "ERROR:|FATAL:"; then
                        log_warn "  Migration $filename had errors: $output"
                    fi
                else
                    # Migration command failed - check if it's a "relation already exists" type error
                    if echo "$output" | grep -qiE "already exists|duplicate"; then
                        log_info "    (already applied)"
                    else
                        log_error "  Migration $filename failed: $output"
                        exit 1
                    fi
                fi
            fi
        done < <(find "$migrations_dir" -name "*.up.sql" -type f | sort -V)
        log_success "Database migrations complete!"
    else
        log_warn "Migrations directory not found. Tables may need to be created manually."
        log_warn "Looked in: $migrations_dir"
    fi
    echo ""
}

# ============================================================================
# Import Phase
# ============================================================================

import_data() {
    MIGRATION_PHASE="import"
    log_info "Starting data import..."
    echo ""

    # Import MySQL data into PostgreSQL (unless --influxdb-only)
    if [[ "$INFLUXDB_ONLY" != true ]]; then
        log_info "Importing core data into PostgreSQL..."
        IMPORT_DIR="$MIGRATION_DIR/mysql" \
        POSTGRES_CONTAINER="$TIMESCALEDB_CONTAINER" \
        "$SCRIPTS_DIR/import-postgres.sh"
    else
        log_info "Skipping PostgreSQL import (--influxdb-only)"
    fi

    # Import InfluxDB data into TimescaleDB (unless --mysql-only)
    if [[ "$MYSQL_ONLY" != true ]]; then
        if [[ -d "$MIGRATION_DIR/influxdb" ]] && [[ -f "$MIGRATION_DIR/influxdb/device_metrics.csv" ]]; then
            echo ""
            log_info "Importing telemetry data into TimescaleDB..."
            IMPORT_DIR="$MIGRATION_DIR/influxdb" \
            POSTGRES_CONTAINER="$TIMESCALEDB_CONTAINER" \
            "$SCRIPTS_DIR/import-timescaledb.sh"
        else
            log_warn "No telemetry data to import."
        fi
    else
        log_info "Skipping TimescaleDB import (--mysql-only)"
    fi

    log_success "Import phase complete!"
}

# ============================================================================
# Verification Phase
# ============================================================================

verify_migration() {
    MIGRATION_PHASE="verify"
    log_info "Verifying migration..."
    echo ""

    local errors=0

    # Verify MySQL data counts (unless --influxdb-only)
    if [[ "$INFLUXDB_ONLY" != true ]] && [[ -d "$MIGRATION_DIR/mysql" ]]; then
        log_info "Checking imported data counts:"
        for csv_file in "$MIGRATION_DIR/mysql"/*.csv; do
            if [[ -f "$csv_file" ]]; then
                local table
                table=$(basename "$csv_file" .csv)
                local expected_file="$MIGRATION_DIR/mysql/${table}.count"
                local imported_file="$MIGRATION_DIR/mysql/${table}.imported_count"

                if [[ -f "$expected_file" ]] && [[ -f "$imported_file" ]]; then
                    local expected imported
                    expected=$(cat "$expected_file")
                    imported=$(cat "$imported_file")

                    if [[ "$expected" -eq "$imported" ]]; then
                        printf "  %-25s ${GREEN}%s / %s${NC}\n" "$table" "$imported" "$expected"
                    else
                        printf "  %-25s ${RED}%s / %s MISMATCH${NC}\n" "$table" "$imported" "$expected"
                        errors=$((errors + 1))
                    fi
                fi
            fi
        done
    fi

    # Verify telemetry data (unless --mysql-only)
    if [[ "$MYSQL_ONLY" != true ]] && [[ -f "$MIGRATION_DIR/influxdb/device_metrics.count" ]]; then
        local expected imported
        expected=$(cat "$MIGRATION_DIR/influxdb/device_metrics.count")
        imported=$(cat "$MIGRATION_DIR/influxdb/device_metrics.imported_count" 2>/dev/null || echo "0")
        printf "  %-25s %s / %s (telemetry)\n" "device_metrics" "$imported" "$expected"
    fi

    echo ""

    if [[ $errors -gt 0 ]]; then
        log_warn "Migration completed with $errors warning(s)."
        log_warn "Please verify the data manually."
        return 1
    else
        log_success "Migration verification complete!"
        return 0
    fi
}

# ============================================================================
# Main
# ============================================================================

main() {
    echo ""
    echo "=============================================="
    echo "Proto Fleet Data Migration"
    echo "MySQL/InfluxDB → PostgreSQL/TimescaleDB"
    echo "=============================================="
    echo ""

    if [[ "$DRY_RUN" == true ]]; then
        log_warn "DRY RUN MODE - No changes will be made"
        echo ""
    fi

    # Pre-flight checks
    preflight_checks

    # Export phase
    if [[ "$SKIP_EXPORT" != true ]]; then
        if [[ "$DRY_RUN" == true ]]; then
            log_info "[DRY RUN] Would export data from MySQL and InfluxDB"
        else
            export_data
        fi
    else
        log_info "Skipping export phase (using existing data)"
        if [[ ! -d "$MIGRATION_DIR/mysql" ]]; then
            log_error "No exported data found at $MIGRATION_DIR/mysql"
            exit 1
        fi
    fi

    # Transition to new stack
    if [[ "$SKIP_IMPORT" != true ]]; then
        if [[ "$DRY_RUN" == true ]]; then
            log_info "[DRY RUN] Would stop old containers and start TimescaleDB"
        else
            transition_to_new_stack
        fi
    fi

    # Import phase
    if [[ "$SKIP_IMPORT" != true ]]; then
        if [[ "$DRY_RUN" == true ]]; then
            log_info "[DRY RUN] Would import data into PostgreSQL/TimescaleDB"
        else
            import_data
        fi
    else
        log_info "Skipping import phase"
    fi

    # Verification (failures are warnings only, don't abort the script)
    if [[ "$SKIP_IMPORT" != true ]] && [[ "$DRY_RUN" != true ]]; then
        verify_migration || true
    fi

    echo ""
    echo "=============================================="
    echo "Migration Summary"
    echo "=============================================="
    echo ""
    echo "  Export directory: $MIGRATION_DIR"
    echo ""
    log_success "Migration complete!"
    echo ""
    log_info "Old volumes have been preserved. Once you verify the migration,"
    log_info "you can remove them with:"
    echo ""
    echo "  docker volume rm ${PROJECT_NAME}_mysql"
    echo "  docker volume rm ${PROJECT_NAME}_influxdb-data"
    echo "  docker volume rm ${PROJECT_NAME}_influxdb-config"
    echo ""
    log_info "You can also remove the export directory:"
    echo "  rm -rf $MIGRATION_DIR"
    echo ""
}

main "$@"
