#!/bin/bash
set -euo pipefail

# MySQL Export Script for Proto Fleet Migration
# Exports all MySQL tables to CSV format for PostgreSQL import

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$SCRIPT_DIR/lib.sh"

# Configuration
MYSQL_CONTAINER="${MYSQL_CONTAINER:-deployment-mysql-1}"
MYSQL_USER="${DB_USERNAME:-fleet_user}"
MYSQL_PASSWORD="${DB_PASSWORD:-}"
MYSQL_DATABASE="${MYSQL_DATABASE:-fleet}"
EXPORT_DIR="${EXPORT_DIR:-/tmp/proto-fleet-migration/mysql}"

# Use shared table list from lib.sh
TABLES=("${MIGRATION_TABLES[@]}")

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Export MySQL data to CSV files for PostgreSQL migration.

Options:
    -c, --container NAME    MySQL container name (default: deployment-mysql-1)
    -u, --user USER         MySQL username (default: fleet_user)
    -d, --database DB       Database name (default: fleet)
    -o, --output DIR        Output directory (default: /tmp/proto-fleet-migration/mysql)
    -h, --help              Show this help message

Environment Variables:
    DB_USERNAME             MySQL username
    DB_PASSWORD             MySQL password
    MYSQL_CONTAINER         MySQL container name
    MYSQL_DATABASE          Database name
    EXPORT_DIR              Output directory

EOF
    exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--container) MYSQL_CONTAINER="$2"; shift 2 ;;
        -u|--user) MYSQL_USER="$2"; shift 2 ;;
        -d|--database) MYSQL_DATABASE="$2"; shift 2 ;;
        -o|--output) EXPORT_DIR="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

require_password "$MYSQL_PASSWORD" "MySQL" "DB_PASSWORD"
require_container "$MYSQL_CONTAINER" "MySQL"

# Create export directory
mkdir -p "$EXPORT_DIR"
echo "Exporting MySQL data to: $EXPORT_DIR"

# Function to export a table to CSV
export_table() {
    local table="$1"
    local output_file="$EXPORT_DIR/${table}.csv"
    local container_tmp="/tmp/${table}_export.csv"

    echo -n "  Exporting $table... "

    local columns
    columns=$(mysql_run "SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY ORDINAL_POSITION) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA='$MYSQL_DATABASE' AND TABLE_NAME='$table';" 2>&1) || {
        echo "FAILED (could not get columns)"
        return 1
    }

    if [[ -z "$columns" ]]; then
        echo "SKIPPED (table not found)"
        return 0
    fi

    # Write header (comma-separated for proper CSV)
    echo "$columns" > "$output_file"

    # Export data with proper CSV escaping using MySQL's CSV format
    # Uses MYSQL_PWD to avoid password exposure in process list
    # OPTIONALLY ENCLOSED BY ensures NULL values are written as unquoted \N
    # Delete any existing temp file first (MySQL INTO OUTFILE won't overwrite)
    docker_exec_quiet "$MYSQL_CONTAINER" rm -f "$container_tmp"

    local outfile_output
    if outfile_output=$(docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" "$MYSQL_CONTAINER" mysql \
        -u "$MYSQL_USER" \
        "$MYSQL_DATABASE" \
        -N -e "SELECT * INTO OUTFILE '$container_tmp'
               FIELDS TERMINATED BY ',' OPTIONALLY ENCLOSED BY '\"' ESCAPED BY '\"'
               LINES TERMINATED BY '\n'
               FROM \`$table\`;" 2>&1); then
        :
    else
        # Fallback to basic export if INTO OUTFILE fails (e.g., missing FILE privilege)
        # MySQL batch mode outputs tab-separated with "NULL" for null values
        # Use awk to convert TSV to proper CSV:
        # - Convert literal "NULL" to \N (PostgreSQL null format)
        # - Quote fields containing commas, quotes, or newlines
        local fallback_output
        if ! fallback_output=$(docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" "$MYSQL_CONTAINER" mysql \
            -u "$MYSQL_USER" \
            "$MYSQL_DATABASE" \
            -N -B -e "SELECT * FROM \`$table\`;" 2>&1); then
            echo "FAILED"
            echo "  INTO OUTFILE error: $outfile_output"
            echo "  Fallback error: $fallback_output"
            return 1
        fi

        echo "$fallback_output" | awk -F'\t' '{
            for (i=1; i<=NF; i++) {
                # Convert MySQL NULL representation to PostgreSQL format
                if ($i == "NULL") {
                    printf "\\N"
                } else {
                    # Escape existing quotes by doubling them
                    gsub(/"/, "\"\"", $i);
                    # Quote the field if it contains comma, quote, or newline
                    if ($i ~ /[,"\n]/) {
                        $i = "\"" $i "\""
                    }
                    printf "%s", $i
                }
                if (i < NF) printf ","
            }
            print ""
        }' >> "$output_file"

        local count
        count=$(csv_data_rows "$output_file")
        echo "$count rows (basic export)"
        echo "$count" > "$EXPORT_DIR/${table}.count"
        return 0
    fi

    # Copy CSV file from container to host
    # Capture stderr separately to avoid corrupting tar stream
    local cp_stderr
    cp_stderr="$(mktemp)"
    if ! docker cp "$MYSQL_CONTAINER:$container_tmp" - 2>"$cp_stderr" | tar -xO >> "$output_file"; then
        echo "FAILED (docker cp or tar error)"
        cat "$cp_stderr" 2>/dev/null || true
        rm -f "$cp_stderr"
        docker_exec_quiet "$MYSQL_CONTAINER" rm -f "$container_tmp"
        return 1
    fi
    rm -f "$cp_stderr"

    docker_exec_quiet "$MYSQL_CONTAINER" rm -f "$container_tmp"

    local count
    count=$(csv_data_rows "$output_file")
    echo "$count rows"

    # Save row count for verification
    echo "$count" > "$EXPORT_DIR/${table}.count"
}

# Start export
echo "=============================================="
echo "MySQL Data Export"
echo "=============================================="
echo "Container: $MYSQL_CONTAINER"
echo "Database:  $MYSQL_DATABASE"
echo "Output:    $EXPORT_DIR"
echo "=============================================="
echo ""

# Test connection
echo "Testing MySQL connection..."
if ! mysql_quiet "SELECT 1;"; then
    echo "Error: Failed to connect to MySQL."
    exit 1
fi
echo "Connection successful!"
echo ""

# Export each table
echo "Exporting tables:"
for table in "${TABLES[@]}"; do
    export_table "$table"
done

echo ""
echo "=============================================="
echo "Export Summary"
echo "=============================================="

total_rows=0
for table in "${TABLES[@]}"; do
    count_file="$EXPORT_DIR/${table}.count"
    if [[ -f "$count_file" ]]; then
        count=$(cat "$count_file")
        total_rows=$((total_rows + count))
        printf "  %-25s %10s rows\n" "$table" "$count"
    fi
done

echo "----------------------------------------------"
printf "  %-25s %10s rows\n" "TOTAL" "$total_rows"
echo "=============================================="

# Create manifest file
cat > "$EXPORT_DIR/manifest.json" <<EOF
{
    "export_time": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "source": "mysql",
    "database": "$MYSQL_DATABASE",
    "tables": [$(printf '"%s",' "${TABLES[@]}" | sed 's/,$//')]
}
EOF

echo ""
echo "Export complete. Files saved to: $EXPORT_DIR"
