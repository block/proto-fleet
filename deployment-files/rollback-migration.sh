#!/bin/bash
set -euo pipefail

# Proto Fleet Migration Rollback Script
# Restores the old MySQL/InfluxDB setup if migration failed
#
# This script requires that the old docker-compose.yaml has been backed up
# and the old MySQL/InfluxDB volumes still exist.

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yaml"
ENV_FILE="$PROJECT_ROOT/.env"

source "$PROJECT_ROOT/scripts/lib.sh"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Rollback Proto Fleet migration and restore MySQL/InfluxDB setup.

Options:
    --backup-dir DIR    Directory containing backup docker-compose.yaml
    --force             Skip confirmation prompts
    -h, --help          Show this help message

IMPORTANT: This script requires:
  1. A backup of the old docker-compose.yaml
  2. The old MySQL and InfluxDB volumes still exist

If you don't have a backup, you'll need to:
  1. Re-download the old version of Proto Fleet
  2. Restore from a database backup

EOF
    exit 0
}

# Parse command line arguments
BACKUP_DIR=""
FORCE=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --backup-dir) BACKUP_DIR="$2"; shift 2 ;;
        --force) FORCE=true; shift ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

echo ""
echo "=============================================="
echo "Proto Fleet Migration Rollback"
echo "=============================================="
echo ""

# Check for old volumes
# Derive from directory name to match docker compose default behavior
PROJECT_NAME="${PROJECT_NAME:-$(basename "$PROJECT_ROOT")}"
MYSQL_VOL=$(docker volume ls -q | grep -E "^${PROJECT_NAME}[-_]mysql$" || true)
INFLUXDB_VOL=$(docker volume ls -q | grep -E "^${PROJECT_NAME}[-_]influxdb" | head -1 || true)

if [[ -z "$MYSQL_VOL" ]]; then
    log_error "MySQL volume not found. Cannot rollback."
    log_error "The old data may have been deleted."
    exit 1
fi
log_success "Found MySQL volume: $MYSQL_VOL"

if [[ -n "$INFLUXDB_VOL" ]]; then
    log_success "Found InfluxDB volume: $INFLUXDB_VOL"
else
    log_warn "InfluxDB volume not found. Telemetry data may be lost."
fi

# Check for backup docker-compose.yaml
if [[ -n "$BACKUP_DIR" ]]; then
    if [[ ! -f "$BACKUP_DIR/docker-compose.yaml" ]]; then
        log_error "Backup docker-compose.yaml not found at $BACKUP_DIR/docker-compose.yaml"
        exit 1
    fi
else
    log_warn "No backup directory specified."
    log_warn "You will need to provide the old docker-compose.yaml manually."
    echo ""
    log_info "Options:"
    echo "  1. Re-download the old version of Proto Fleet"
    echo "  2. Provide a backup directory with --backup-dir"
    echo ""

    if [[ "$FORCE" != true ]]; then
        read -p "Continue without backup docker-compose.yaml? (y/N): " continue_choice
        if [[ ! "$continue_choice" =~ ^[Yy]$ ]]; then
            exit 0
        fi
    fi
fi

# Confirm rollback
if [[ "$FORCE" != true ]]; then
    echo ""
    log_warn "This will:"
    echo "  1. Stop all current Proto Fleet containers"
    echo "  2. Remove the TimescaleDB volume (if it exists)"
    echo "  3. Restore the old docker-compose.yaml (if backup provided)"
    echo "  4. Start MySQL/InfluxDB containers with old data"
    echo ""
    read -p "Proceed with rollback? (y/N): " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        log_info "Rollback cancelled."
        exit 0
    fi
fi

# Stop current services
log_info "Stopping current services..."
stop_output=""
if ! stop_output=$(docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" down 2>&1); then
    log_warn "docker compose down had issues: $stop_output"
fi

# Remove TimescaleDB volume if it exists
TSDB_VOL=$(docker volume ls -q | grep -E "^${PROJECT_NAME}[-_]timescaledb" | head -1 || true)
if [[ -n "$TSDB_VOL" ]]; then
    log_info "Removing TimescaleDB volume: $TSDB_VOL"
    docker volume rm "$TSDB_VOL" || log_warn "Failed to remove TimescaleDB volume"
fi

# Restore backup docker-compose.yaml if provided
if [[ -n "$BACKUP_DIR" ]] && [[ -f "$BACKUP_DIR/docker-compose.yaml" ]]; then
    log_info "Restoring docker-compose.yaml from backup..."
    cp "$BACKUP_DIR/docker-compose.yaml" "$COMPOSE_FILE"
    log_success "docker-compose.yaml restored"
else
    log_warn "No backup docker-compose.yaml to restore."
    log_warn "You must manually restore the old docker-compose.yaml before starting services."
    echo ""
    log_info "The old MySQL/InfluxDB volumes are preserved:"
    echo "  MySQL:    $MYSQL_VOL"
    [[ -n "$INFLUXDB_VOL" ]] && echo "  InfluxDB: $INFLUXDB_VOL"
    exit 0
fi

# Update environment file for MySQL
if [[ -f "$ENV_FILE" ]]; then
    if ! grep -q "^MYSQL_ROOT_PASSWORD=" "$ENV_FILE"; then
        log_warn "MYSQL_ROOT_PASSWORD not found in .env file."
        log_warn "You may need to add it manually before starting services."
    fi
fi

# Start old services
log_info "Starting MySQL/InfluxDB services..."
if docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up -d; then
    # Wait and verify services are healthy
    log_info "Waiting for services to be healthy..."
    sleep 10
    ps_output=""
    if ps_output=$(docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" ps 2>&1); then
        if echo "$ps_output" | grep -qE "unhealthy|Exit"; then
            log_warn "Some services may not be healthy. Check with: docker compose ps"
        else
            log_success "Services are running"
        fi
    else
        log_warn "Could not check service status: $ps_output"
    fi

    log_success "Rollback complete!"
    echo ""
    log_info "Check status with:"
    echo "  docker compose -p $PROJECT_NAME -f $COMPOSE_FILE ps"
    echo ""
    log_info "View logs with:"
    echo "  docker compose -p $PROJECT_NAME -f $COMPOSE_FILE logs -f"
else
    log_error "Failed to start services."
    log_error "Check docker-compose.yaml and .env file for issues."
    exit 1
fi
