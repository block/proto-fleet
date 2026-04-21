#!/bin/bash
set -euo pipefail

# TimescaleDB Telemetry Import Script for Proto Fleet Migration
# Imports InfluxDB-exported CSV data into TimescaleDB hypertable
# Optimized for months of telemetry data with streaming import

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$SCRIPT_DIR/lib.sh"

# Configuration
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-deployment-timescaledb-1}"
POSTGRES_USER="${DB_USERNAME:-fleet}"
POSTGRES_PASSWORD="${DB_PASSWORD:-}"
POSTGRES_DATABASE="${POSTGRES_DATABASE:-fleet}"
IMPORT_DIR="${IMPORT_DIR:-/tmp/proto-fleet-migration/influxdb}"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Import InfluxDB CSV telemetry data into TimescaleDB.

Options:
    -c, --container NAME    PostgreSQL container name (default: deployment-timescaledb-1)
    -u, --user USER         PostgreSQL username (default: fleet)
    -d, --database DB       Database name (default: fleet)
    -i, --input DIR         Input directory with CSV files (default: /tmp/proto-fleet-migration/influxdb)
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

INPUT_FILE="$IMPORT_DIR/device_metrics.csv"
if [[ ! -f "$INPUT_FILE" ]]; then
    echo "No telemetry data to import (device_metrics.csv not found)."
    exit 0
fi

# Start import
echo "=============================================="
echo "TimescaleDB Telemetry Import"
echo "=============================================="
echo "Container:  $POSTGRES_CONTAINER"
echo "Database:   $POSTGRES_DATABASE"
echo "Input:      $INPUT_FILE"
echo "=============================================="
echo ""

# Test connection
echo "Testing PostgreSQL connection..."
if ! psql_quiet "SELECT 1;"; then
    echo "Error: Failed to connect to PostgreSQL."
    exit 1
fi
echo "Connection successful!"
echo ""

# Get expected row count from export
expected_count=0
if [[ -f "$IMPORT_DIR/device_metrics.count" ]]; then
    expected_count=$(cat "$IMPORT_DIR/device_metrics.count")
    echo "Expected rows to import: $expected_count"
fi

# Get current row count
current_count=$(psql_or_fail "SELECT COUNT(*) FROM device_metrics;")
echo "Current rows in table: $current_count"
echo ""

# Read CSV header and convert to column array
echo "Analyzing CSV structure..."
csv_header=$(head -n 1 "$INPUT_FILE")
if [[ -z "$csv_header" ]]; then
    echo "Error: CSV file is empty or has no header"
    exit 1
fi
echo "  CSV columns: $csv_header"

IFS=',' read -ra CSV_COLUMNS <<< "$csv_header"
echo "  Found ${#CSV_COLUMNS[@]} columns"

# Create a temporary table for import (staging)
echo ""
echo "Creating staging table..."
psql_quiet "DROP TABLE IF EXISTS device_metrics_staging;" || true

# Build CREATE TABLE statement dynamically (all columns as TEXT for staging)
create_cols=""
for col in "${CSV_COLUMNS[@]}"; do
    # Clean column name (remove quotes/whitespace)
    col=$(echo "$col" | tr -d '"' | xargs)
    [[ -n "$create_cols" ]] && create_cols="$create_cols, "
    create_cols="$create_cols\"$col\" TEXT"
done

psql_or_fail "CREATE TABLE device_metrics_staging ($create_cols);" > /dev/null

# Rename device_id to device_identifier in staging table to match target schema
psql_optional "rename device_id column" "ALTER TABLE device_metrics_staging RENAME COLUMN device_id TO device_identifier;"

# Update CSV_COLUMNS array to reflect the rename
for i in "${!CSV_COLUMNS[@]}"; do
    csv_col_clean=$(echo "${CSV_COLUMNS[$i]}" | tr -d '"' | xargs)
    if [[ "$csv_col_clean" == "device_id" ]]; then
        CSV_COLUMNS[$i]="device_identifier"
        break
    fi
done

echo "  Staging table created with dynamic schema."

# Copy CSV file into container (filter out empty lines that can cause COPY to fail)
echo ""
echo "Copying data to container..."
# Use grep to filter out empty lines, then copy to container
grep -v '^[[:space:]]*$' "$INPUT_FILE" > "/tmp/device_metrics_clean.csv"
docker cp "/tmp/device_metrics_clean.csv" "${POSTGRES_CONTAINER}:/tmp/device_metrics.csv"
rm -f "/tmp/device_metrics_clean.csv"
echo "  File copied."

# Import into staging table
echo ""
echo "Importing data into staging table..."
start_time=$(date +%s)

# Use SQL COPY command (server-side) for reliable import
copy_output=""
if ! copy_output=$(psql_run "COPY device_metrics_staging FROM '/tmp/device_metrics.csv' WITH (FORMAT csv, HEADER true);" 2>&1); then
    echo "  ERROR: COPY failed"
    echo "  $copy_output"
    exit 1
fi

staging_count=$(psql_or_fail "SELECT COUNT(*) FROM device_metrics_staging;")
end_time=$(date +%s)
elapsed=$((end_time - start_time))
echo "  Imported $staging_count rows into staging table in ${elapsed}s"

# Add index on staging table for faster filtering (only if component_type column exists)
echo ""
echo "Creating index on staging table for faster filtering..."
# component_type may not exist in all deployments - skip index if column doesn't exist
psql_optional "index on component_type" "CREATE INDEX idx_staging_component ON device_metrics_staging(component_type);"
echo "  Index step complete."

# Transform and insert into hypertable
echo ""
echo "Transforming and inserting into device_metrics hypertable..."
echo "  (This may take several minutes for large datasets)"
start_time=$(date +%s)

# For fresh migration, truncate and use direct INSERT (much faster than ON CONFLICT for bulk data)
echo "  Truncating target table for fresh import..."
psql_quiet "TRUNCATE device_metrics;" || true

# Get target table column info (name and type) for columns that exist in both staging and target
echo "  Building dynamic column mapping..."
target_cols=$(psql_or_fail "
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_name = 'device_metrics'
  AND table_schema = 'public'
ORDER BY ordinal_position;
")

# Build INSERT and SELECT clauses dynamically
insert_cols=""
select_cols=""

while IFS='|' read -r col_name col_type; do
    col_name=$(echo "$col_name" | xargs)  # trim whitespace
    col_type=$(echo "$col_type" | xargs)

    # Check if this column exists in our CSV/staging table
    col_exists=false
    for csv_col in "${CSV_COLUMNS[@]}"; do
        csv_col_clean=$(echo "$csv_col" | tr -d '"' | xargs)
        if [[ "$csv_col_clean" == "$col_name" ]]; then
            col_exists=true
            break
        fi
    done

    if [[ "$col_exists" == true ]]; then
        [[ -n "$insert_cols" ]] && insert_cols="$insert_cols, "
        [[ -n "$select_cols" ]] && select_cols="$select_cols, "

        insert_cols="$insert_cols\"$col_name\""

        # Apply type casting based on target column type
        case "$col_type" in
            "timestamp with time zone"|"timestamp without time zone")
                select_cols="${select_cols}\"$col_name\"::TIMESTAMPTZ"
                ;;
            "double precision"|"real"|"numeric")
                select_cols="${select_cols}NULLIF(\"$col_name\", '')::DOUBLE PRECISION"
                ;;
            "integer"|"bigint"|"smallint")
                select_cols="${select_cols}NULLIF(\"$col_name\", '')::INTEGER"
                ;;
            *)
                # TEXT, VARCHAR, etc. - just use NULLIF for empty strings
                select_cols="${select_cols}NULLIF(\"$col_name\", '')"
                ;;
        esac
    fi
done <<< "$target_cols"

if [[ -z "$insert_cols" ]]; then
    echo "Error: No matching columns found between CSV and target table"
    exit 1
fi

echo "  Mapped columns: $insert_cols"

# Check if component_type column exists in staging (for filtering)
has_component_type=false
for csv_col in "${CSV_COLUMNS[@]}"; do
    csv_col_clean=$(echo "$csv_col" | tr -d '"' | xargs)
    if [[ "$csv_col_clean" == "component_type" ]]; then
        has_component_type=true
        break
    fi
done

# Build WHERE clause (filter out component-level metrics if that column exists)
where_clause=""
if [[ "$has_component_type" == true ]]; then
    where_clause="WHERE component_type IS NULL OR component_type = ''"
fi

# Execute the dynamic INSERT (ON CONFLICT skips duplicate time+device_identifier rows)
insert_output=""
if ! insert_output=$(psql_run "
INSERT INTO device_metrics ($insert_cols)
SELECT $select_cols
FROM device_metrics_staging
$where_clause
ON CONFLICT (time, device_identifier) DO NOTHING;
" 2>&1); then
    echo "  ERROR: INSERT failed"
    echo "  $insert_output"
    exit 1
fi

end_time=$(date +%s)
elapsed=$((end_time - start_time))
echo "  Transform complete in ${elapsed}s"

# Get final count (equals imported count since we truncated)
final_count=$(psql_or_fail "SELECT COUNT(*) FROM device_metrics;")
imported_count=$final_count

# Clean up
echo ""
echo "Cleaning up..."
psql_quiet "DROP TABLE IF EXISTS device_metrics_staging;" || true
docker_exec_quiet "$POSTGRES_CONTAINER" rm -f "/tmp/device_metrics.csv" || true
echo "  Cleanup complete."

echo ""
echo "=============================================="
echo "Import Summary"
echo "=============================================="
echo "  Rows in staging:    $staging_count"
echo "  Rows imported:      $imported_count"
echo "  Total rows in table: $final_count"
if [[ "$expected_count" -gt 0 ]]; then
    echo "  Expected rows:      $expected_count"
    if [[ "$imported_count" -ne "$expected_count" ]]; then
        echo ""
        echo "  NOTE: Row count differs from expected. This may be due to:"
        echo "        - Duplicate timestamps being merged"
        echo "        - Component-level metrics being filtered out"
        echo "        - Invalid data being skipped"
    fi
fi
echo "=============================================="

# Save imported count for verification
echo "$imported_count" > "$IMPORT_DIR/device_metrics.imported_count"

echo ""
echo "Telemetry import complete."
