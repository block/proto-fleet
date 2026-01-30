#!/bin/bash
set -euo pipefail

# InfluxDB 3.x Export Script for Proto Fleet Migration
# Exports telemetry data from InfluxDB 3.x to CSV format for TimescaleDB import

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$SCRIPT_DIR/lib.sh"

# Configuration
INFLUXDB_CONTAINER="${INFLUXDB_CONTAINER:-deployment-influxdb-1}"
INFLUXDB_DATABASE="${INFLUXDB_DATABASE:-fleet}"
INFLUXDB_TOKEN="${INFLUXDB_TOKEN:-}"
EXPORT_DIR="${EXPORT_DIR:-/tmp/proto-fleet-migration/influxdb}"

# Chunk size for large exports (in days)
CHUNK_DAYS="${CHUNK_DAYS:-7}"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Export InfluxDB 3.x telemetry data to CSV files for TimescaleDB migration.

Options:
    -c, --container NAME    InfluxDB container name (default: deployment-influxdb-1)
    -d, --database DB       Database name (default: fleet)
    -t, --token TOKEN       InfluxDB auth token (auto-fetched from container if not provided)
    -o, --output DIR        Output directory (default: /tmp/proto-fleet-migration/influxdb)
    --chunk-days DAYS       Export chunk size in days (default: 7)
    -h, --help              Show this help message

Environment Variables:
    INFLUXDB_CONTAINER      InfluxDB container name
    INFLUXDB_DATABASE       Database name
    INFLUXDB_TOKEN          InfluxDB auth token (auto-fetched from container if not provided)
    EXPORT_DIR              Output directory
    CHUNK_DAYS              Chunk size for exports in days

EOF
    exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--container) INFLUXDB_CONTAINER="$2"; shift 2 ;;
        -d|--database) INFLUXDB_DATABASE="$2"; shift 2 ;;
        -t|--token) INFLUXDB_TOKEN="$2"; shift 2 ;;
        -o|--output) EXPORT_DIR="$2"; shift 2 ;;
        --chunk-days) CHUNK_DAYS="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

require_container "$INFLUXDB_CONTAINER" "InfluxDB"

# Try to get InfluxDB token from container if not provided
if [[ -z "$INFLUXDB_TOKEN" ]]; then
    INFLUXDB_TOKEN=$(docker exec "$INFLUXDB_CONTAINER" grep INFLUXDB3_AUTH_TOKEN /var/lib/influxdb3/start/.env 2>/dev/null | cut -d= -f2 || echo "")
fi

# Validate token was retrieved
if [[ -z "$INFLUXDB_TOKEN" ]]; then
    echo "Warning: No InfluxDB token found. Queries may fail with 401 Unauthorized."
fi

# Create export directory
mkdir -p "$EXPORT_DIR"
echo "Exporting InfluxDB data to: $EXPORT_DIR"

# Function to run InfluxDB 3.x SQL query
# InfluxDB 3.x uses the influxdb3 CLI with SQL queries
influxdb_query() {
    local query="$1"
    local format="${2:-csv}"
    local args=(--database "$INFLUXDB_DATABASE" --format "$format")

    if [[ -n "$INFLUXDB_TOKEN" ]]; then
        args+=(--token "$INFLUXDB_TOKEN")
    fi

    docker exec "$INFLUXDB_CONTAINER" influxdb3 query "${args[@]}" "$query"
}

# Function to get date range of data
get_date_range() {
    echo "Determining data date range..."

    # Get the earliest and latest timestamps with error checking
    local min_result max_result min_time max_time

    if ! min_result=$(influxdb_query "SELECT MIN(time) as min_time FROM device_metrics" 2>&1); then
        echo "Failed to query minimum time from device_metrics: $min_result"
        return 1
    fi
    if [[ -z "$min_result" ]]; then
        echo "Empty result when querying minimum time from device_metrics"
        return 1
    fi
    # Validate result has at least 2 lines (header + data)
    if [[ $(echo "$min_result" | wc -l) -lt 2 ]]; then
        echo "Invalid query result format for minimum time"
        return 1
    fi
    min_time=$(echo "$min_result" | tail -n 1 | cut -d',' -f1)

    if ! max_result=$(influxdb_query "SELECT MAX(time) as max_time FROM device_metrics" 2>&1); then
        echo "Failed to query maximum time from device_metrics: $max_result"
        return 1
    fi
    if [[ -z "$max_result" ]]; then
        echo "Empty result when querying maximum time from device_metrics"
        return 1
    fi
    # Validate result has at least 2 lines (header + data)
    if [[ $(echo "$max_result" | wc -l) -lt 2 ]]; then
        echo "Invalid query result format for maximum time"
        return 1
    fi
    max_time=$(echo "$max_result" | tail -n 1 | cut -d',' -f1)

    if [[ -z "$min_time" || "$min_time" == "min_time" ]]; then
        echo "No data found in device_metrics table"
        return 1
    fi

    echo "  Data range: $min_time to $max_time"
    echo "$min_time" > "$EXPORT_DIR/min_time.txt"
    echo "$max_time" > "$EXPORT_DIR/max_time.txt"

    # Export global variables for chunked export
    EXPORT_MIN_TIME="$min_time"
    EXPORT_MAX_TIME="$max_time"
}

# Start export
echo "=============================================="
echo "InfluxDB 3.x Data Export"
echo "=============================================="
echo "Container: $INFLUXDB_CONTAINER"
echo "Database:  $INFLUXDB_DATABASE"
echo "Output:    $EXPORT_DIR"
echo "=============================================="
echo ""

# Test connection
echo "Testing InfluxDB connection..."
conn_output=""
if ! conn_output=$(influxdb_query "SELECT 1" 2>&1); then
    # Try alternative command for older versions
    help_output=""
    if ! help_output=$(docker exec "$INFLUXDB_CONTAINER" influxdb3 --help 2>&1); then
        echo "Error: Failed to connect to InfluxDB or influxdb3 CLI not available."
        echo "  Connection error: $conn_output"
        echo "  Help check error: $help_output"
        exit 1
    fi
fi
echo "Connection successful!"
echo ""

# Get date range
if ! get_date_range; then
    echo "No telemetry data to export."
    echo "0" > "$EXPORT_DIR/total_count.txt"
    exit 0
fi

# Get total row count first
echo "Counting total rows..."
if ! count_result=$(influxdb_query "SELECT COUNT(*) as cnt FROM device_metrics" 2>&1); then
    echo "Warning: Failed to get row count estimate: $count_result"
    total_estimate="unknown"
elif [[ -z "$count_result" ]]; then
    echo "Warning: Empty result when getting row count"
    total_estimate="unknown"
else
    total_estimate=$(echo "$count_result" | tail -n 1 | tr -d ' ')
fi
echo "  Estimated total rows: $total_estimate"
echo ""

# Large dataset threshold (1 million rows)
LARGE_DATASET_THRESHOLD=1000000

# Use SELECT * to export all columns dynamically (schema may vary between deployments)

# Check if we should use chunked export
use_chunked=false
if [[ "$total_estimate" != "unknown" ]] && [[ "$total_estimate" -gt "$LARGE_DATASET_THRESHOLD" ]]; then
    use_chunked=true
    echo "Large dataset detected. Using chunked export (${CHUNK_DAYS}-day chunks)..."
fi

echo "Exporting device_metrics table..."
output_file="$EXPORT_DIR/device_metrics.csv"

if [[ "$use_chunked" == true ]]; then
    # Chunked export for large datasets
    # Parse timestamps (assuming ISO format)
    # Convert to Unix timestamp for easier arithmetic
    if command -v gdate &> /dev/null; then
        DATE_CMD="gdate"  # macOS with coreutils
    else
        DATE_CMD="date"
    fi

    # Export header first
    header_query="SELECT * FROM device_metrics LIMIT 0"
    influxdb_query "$header_query" > "$output_file"

    current_time="$EXPORT_MIN_TIME"
    chunk_num=0
    total_rows=0
    max_chunks=1000  # Safety limit to prevent infinite loops

    while [[ "$current_time" < "$EXPORT_MAX_TIME" ]] && [[ $chunk_num -lt $max_chunks ]]; do
        chunk_num=$((chunk_num + 1))

        # Calculate end time for this chunk with proper error handling
        end_time=$($DATE_CMD -d "$current_time + $CHUNK_DAYS days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || \
        end_time=$($DATE_CMD -j -v+${CHUNK_DAYS}d -f "%Y-%m-%dT%H:%M:%SZ" "$current_time" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || {
            echo "Error: Failed to calculate chunk end time. Install GNU coreutils or check date format."
            exit 1
        }

        # Don't go past max time
        if [[ "$end_time" > "$EXPORT_MAX_TIME" ]]; then
            end_time="$EXPORT_MAX_TIME"
        fi

        echo -n "  Chunk $chunk_num ($current_time to $end_time)... "

        chunk_query="SELECT * FROM device_metrics WHERE time >= '$current_time' AND time < '$end_time' ORDER BY time"

        # Export chunk (skip header by using tail)
        chunk_result=""
        if chunk_result=$(influxdb_query "$chunk_query" 2>&1); then
            if [[ -n "$chunk_result" ]]; then
                echo "$chunk_result" | tail -n +2 >> "$output_file"
                chunk_rows=$(echo "$chunk_result" | tail -n +2 | wc -l)
                total_rows=$((total_rows + chunk_rows))
                echo "$chunk_rows rows"
            else
                echo "0 rows"
            fi
        else
            echo "FAILED"
            echo "  Chunk query error: $chunk_result"
            echo "  Cleaning up partial export file..."
            rm -f "$output_file"
            # Record failure in manifest for debugging
            cat > "$EXPORT_DIR/manifest.json" <<FAILEOF
{
    "export_time": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "source": "influxdb3",
    "database": "$INFLUXDB_DATABASE",
    "status": "FAILED",
    "failed_chunk": $chunk_num,
    "error": "Chunk export failed at $current_time"
}
FAILEOF
            exit 1
        fi

        current_time="$end_time"
    done

    if [[ $chunk_num -ge $max_chunks ]]; then
        echo "Error: Exceeded maximum chunk limit ($max_chunks). Check date range."
        exit 1
    fi
else
    # Single export for smaller datasets
    query="SELECT * FROM device_metrics ORDER BY time"

    echo "  Running export query (this may take a while for large datasets)..."
    if ! influxdb_query "$query" > "$output_file" 2>&1; then
        echo "  ERROR: Query failed"
        cat "$output_file"
        exit 1
    fi

    # Check if file has content
    if [[ ! -s "$output_file" ]]; then
        echo "  ERROR: Query returned empty result"
        exit 1
    fi

    total_rows=$(wc -l < "$output_file")
    total_rows=$((total_rows - 1))  # Subtract header row
    echo "  Exported $total_rows rows"
fi

echo ""
echo "=============================================="
echo "Export Summary"
echo "=============================================="
printf "  %-25s %10s rows\n" "device_metrics" "$total_rows"
echo "=============================================="

# Save count for verification
echo "$total_rows" > "$EXPORT_DIR/device_metrics.count"

# Create manifest file
cat > "$EXPORT_DIR/manifest.json" <<EOF
{
    "export_time": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "source": "influxdb3",
    "database": "$INFLUXDB_DATABASE",
    "tables": ["device_metrics"],
    "total_rows": $total_rows
}
EOF

echo ""
echo "Export complete. Files saved to: $EXPORT_DIR"
