#!/bin/bash
set -euo pipefail

# PostgreSQL Import Script for Proto Fleet Migration
# Imports MySQL-exported CSV data into PostgreSQL

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$SCRIPT_DIR/lib.sh"

# Configuration
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-deployment-timescaledb-1}"
POSTGRES_USER="${DB_USERNAME:-fleet}"
POSTGRES_PASSWORD="${DB_PASSWORD:-}"
POSTGRES_DATABASE="${POSTGRES_DATABASE:-${DB_NAME:-fleet}}"
IMPORT_DIR="${IMPORT_DIR:-/tmp/proto-fleet-migration/mysql}"

# Use shared table list from lib.sh
TABLES=("${MIGRATION_TABLES[@]}")

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Import MySQL CSV data into PostgreSQL.

Options:
    -c, --container NAME    PostgreSQL container name (default: deployment-timescaledb-1)
    -u, --user USER         PostgreSQL username (default: fleet)
    -d, --database DB       Database name (default: fleet)
    -i, --input DIR         Input directory with CSV files (default: /tmp/proto-fleet-migration/mysql)
    -h, --help              Show this help message

Environment Variables:
    DB_USERNAME             PostgreSQL username
    DB_PASSWORD             PostgreSQL password
    POSTGRES_CONTAINER      PostgreSQL container name
    POSTGRES_DATABASE       Database name
    IMPORT_DIR              Input directory

EOF
    exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--container) POSTGRES_CONTAINER="$2"; shift 2 ;;
        -u|--user) POSTGRES_USER="$2"; shift 2 ;;
        -d|--database) POSTGRES_DATABASE="$2"; shift 2 ;;
        -i|--input) IMPORT_DIR="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

require_password "$POSTGRES_PASSWORD" "PostgreSQL" "DB_PASSWORD"
require_container "$POSTGRES_CONTAINER" "PostgreSQL"
require_directory "$IMPORT_DIR" "Import directory"

# Function to copy file into container and import
import_table() {
    local table="$1"
    local input_file="$IMPORT_DIR/${table}.csv"

    if [[ ! -f "$input_file" ]]; then
        echo "  $table: SKIPPED (no CSV file)"
        return 0
    fi

    echo -n "  Importing $table... "

    # Get expected row count from export
    local expected_count=0
    if [[ -f "$IMPORT_DIR/${table}.count" ]]; then
        expected_count=$(cat "$IMPORT_DIR/${table}.count")
    fi

    # Skip if file only has header (no data rows)
    local data_lines
    data_lines=$(csv_data_rows "$input_file")
    if [[ "$data_lines" -eq 0 ]]; then
        echo "0 rows (empty table)"
        echo "0" > "$IMPORT_DIR/${table}.imported_count"
        return 0
    fi

    # Get column names from the CSV header (first line)
    local columns
    columns=$(head -n 1 "$input_file")

    # Convert comma-separated columns to PostgreSQL format, quote column names
    # Handle reserved words like "user" by quoting all column names
    local pg_columns
    pg_columns=$(echo "$columns" | tr ',' '\n' | sed 's/^/"/;s/$/"/' | tr '\n' ',' | sed 's/,$//')

    # Handle the table name (quote if reserved word like "user")
    local pg_table="$table"
    if [[ "$table" == "user" ]]; then
        pg_table='"user"'
    fi

    # Disable FK checks and triggers for import (handles FK constraints and seed data from migrations)
    echo -n "preparing... "
    psql_quiet "SET session_replication_role = 'replica';" || true
    psql_optional "disable triggers" "ALTER TABLE $pg_table DISABLE TRIGGER ALL;"

    # Truncate table with CASCADE to handle FK constraints (all tables will be re-imported anyway)
    psql_optional "truncate table" "TRUNCATE TABLE $pg_table CASCADE;"

    echo -n "importing... "
    # Import using COPY FROM STDIN (streams data through psql, avoids container file permission issues)
    # Filter empty lines and pipe through docker exec to psql
    # MySQL's INTO OUTFILE exports NULL as \N by default
    local copy_output
    # Replace MySQL zero dates with Unix epoch (PostgreSQL rejects 0000-00-00 dates)
    if ! copy_output=$(grep -v '^[[:space:]]*$' "$input_file" | sed 's/0000-00-00 00:00:00\.[0-9]*/1970-01-01 00:00:00.000000/g' | docker exec -i -e PGPASSWORD="$POSTGRES_PASSWORD" "$POSTGRES_CONTAINER" \
        psql -U "$POSTGRES_USER" -d "$POSTGRES_DATABASE" -c \
        "COPY $pg_table ($pg_columns) FROM STDIN WITH (FORMAT csv, DELIMITER ',', HEADER true, NULL '\\N', QUOTE '\"');" 2>&1); then
        echo "FAILED"
        echo "  Error: $copy_output"
        psql_quiet "ALTER TABLE $pg_table ENABLE TRIGGER ALL;" || true
        psql_quiet "SET session_replication_role = 'origin';" || true
        return 1
    fi

    # Re-enable triggers and FK checks
    psql_quiet "ALTER TABLE $pg_table ENABLE TRIGGER ALL;" || true
    psql_quiet "SET session_replication_role = 'origin';" || true

    local actual_count
    actual_count=$(psql_or_fail "SELECT COUNT(*) FROM $pg_table;")

    # Verify count
    if [[ "$actual_count" -eq "$expected_count" ]]; then
        echo "$actual_count rows (verified)"
    else
        echo "$actual_count rows (expected: $expected_count) WARNING"
    fi

    echo "$actual_count" > "$IMPORT_DIR/${table}.imported_count"
}

# Function to reset sequences after import
reset_sequences() {
    echo ""
    echo "Resetting sequences..."

    for table in "${TABLES[@]}"; do
        local pg_table="$table"
        if [[ "$table" == "user" ]]; then
            pg_table='"user"'
        fi

        # Get actual sequence name for this table's id column
        local seq_name
        seq_name=$(psql_run "SELECT pg_get_serial_sequence('$pg_table', 'id');" 2>&1 | tr -d ' ') || continue

        # Skip if no sequence found (table may not have auto-increment id)
        if [[ -z "$seq_name" ]]; then
            continue
        fi

        # Get max id value
        local max_id
        max_id=$(psql_run "SELECT COALESCE(MAX(id), 0) FROM $pg_table;" 2>&1) || continue

        if [[ "$max_id" != "0" && -n "$max_id" ]]; then
            # Reset the sequence to max(id)
            psql_optional "setval $table" "SELECT setval('$seq_name', $max_id, true);"
            echo "  $table: sequence reset to $max_id"
        fi
    done
}

# Function to validate ENUM values before import
validate_enum_values() {
    echo ""
    echo "Validating ENUM values..."

    # Check device_status values (column 3 in CSV format)
    if [[ -f "$IMPORT_DIR/device_status.csv" ]]; then
        local invalid_status
        invalid_status=$(tail -n +2 "$IMPORT_DIR/device_status.csv" | cut -d',' -f3 | tr -d '"' | sort -u | grep -v -E '^(ACTIVE|INACTIVE|OFFLINE|MAINTENANCE|ERROR|UNKNOWN|NEEDS_MINING_POOL)$' || true)
        if [[ -n "$invalid_status" ]]; then
            echo "  WARNING: Invalid device status values found: $invalid_status"
        fi
    fi

    # Check pairing_status values (column 3 in CSV format)
    if [[ -f "$IMPORT_DIR/device_pairing.csv" ]]; then
        local invalid_pairing
        invalid_pairing=$(tail -n +2 "$IMPORT_DIR/device_pairing.csv" | cut -d',' -f3 | tr -d '"' | sort -u | grep -v -E '^(PENDING|PAIRED|UNPAIRED|FAILED|AUTHENTICATION_NEEDED)$' || true)
        if [[ -n "$invalid_pairing" ]]; then
            echo "  WARNING: Invalid pairing status values found: $invalid_pairing"
        fi
    fi

    # Check batch_status values (column 5 in CSV format)
    if [[ -f "$IMPORT_DIR/command_batch_log.csv" ]]; then
        local invalid_batch
        invalid_batch=$(tail -n +2 "$IMPORT_DIR/command_batch_log.csv" | cut -d',' -f5 | tr -d '"' | sort -u | grep -v -E '^(PENDING|PROCESSING|FINISHED)$' || true)
        if [[ -n "$invalid_batch" ]]; then
            echo "  WARNING: Invalid batch status values found: $invalid_batch"
        fi
    fi

    echo "  Validation complete."
}

# Start import
echo "=============================================="
echo "PostgreSQL Data Import"
echo "=============================================="
echo "Container: $POSTGRES_CONTAINER"
echo "Database:  $POSTGRES_DATABASE"
echo "Input:     $IMPORT_DIR"
echo "=============================================="
echo ""

# Test connection
echo "Testing PostgreSQL connection..."
if ! psql_quiet "SELECT 1;"; then
    echo "Error: Failed to connect to PostgreSQL."
    exit 1
fi
echo "Connection successful!"

# Validate ENUM values
validate_enum_values

# Import each table
echo ""
echo "Importing tables:"
for table in "${TABLES[@]}"; do
    import_table "$table"
done

# Reset sequences
reset_sequences

echo ""
echo "=============================================="
echo "Import Summary"
echo "=============================================="

total_imported=0
total_expected=0
warnings=0

for table in "${TABLES[@]}"; do
    expected_file="$IMPORT_DIR/${table}.count"
    imported_file="$IMPORT_DIR/${table}.imported_count"

    if [[ -f "$imported_file" ]]; then
        imported=$(cat "$imported_file")
        total_imported=$((total_imported + imported))

        if [[ -f "$expected_file" ]]; then
            expected=$(cat "$expected_file")
            total_expected=$((total_expected + expected))

            if [[ "$imported" -ne "$expected" ]]; then
                printf "  %-25s %10s / %s rows (MISMATCH)\n" "$table" "$imported" "$expected"
                warnings=$((warnings + 1))
            else
                printf "  %-25s %10s rows\n" "$table" "$imported"
            fi
        else
            printf "  %-25s %10s rows\n" "$table" "$imported"
        fi
    fi
done

echo "----------------------------------------------"
printf "  %-25s %10s rows\n" "TOTAL IMPORTED" "$total_imported"
if [[ "$total_expected" -gt 0 ]]; then
    printf "  %-25s %10s rows\n" "TOTAL EXPECTED" "$total_expected"
fi
echo "=============================================="

if [[ $warnings -gt 0 ]]; then
    echo ""
    echo "WARNING: $warnings table(s) had row count mismatches."
    echo "Please verify the data integrity manually."
fi

echo ""
echo "Import complete."
