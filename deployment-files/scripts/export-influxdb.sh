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

# Chunk size for large exports (in hours, default 6 hours)
CHUNK_HOURS="${CHUNK_HOURS:-6}"

# Manual date range (if provided, skips auto-detection)
START_DATE="${START_DATE:-}"
END_DATE="${END_DATE:-}"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Export InfluxDB 3.x telemetry data to CSV files for TimescaleDB migration.

Options:
    -c, --container NAME    InfluxDB container name (default: deployment-influxdb-1)
    -d, --database DB       Database name (default: fleet)
    -t, --token TOKEN       InfluxDB auth token (auto-fetched from container if not provided)
    -o, --output DIR        Output directory (default: /tmp/proto-fleet-migration/influxdb)
    --chunk-hours HOURS     Export chunk size in hours (default: 6)
    --start-date DATE       Start date for export (YYYY-MM-DD), skips auto-detection
    --end-date DATE         End date for export (YYYY-MM-DD), defaults to today
    -h, --help              Show this help message

Environment Variables:
    INFLUXDB_CONTAINER      InfluxDB container name
    INFLUXDB_DATABASE       Database name
    INFLUXDB_TOKEN          InfluxDB auth token (auto-fetched from container if not provided)
    EXPORT_DIR              Output directory
    CHUNK_HOURS             Chunk size for exports in hours
    START_DATE              Start date for export (YYYY-MM-DD)
    END_DATE                End date for export (YYYY-MM-DD)

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
        --chunk-hours) CHUNK_HOURS="$2"; shift 2 ;;
        --start-date) START_DATE="$2"; shift 2 ;;
        --end-date) END_DATE="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

# Validate argument combinations
if [[ -n "$END_DATE" ]] && [[ -z "$START_DATE" ]]; then
    echo "Error: --end-date requires --start-date"
    exit 1
fi

# Validate CHUNK_HOURS is a positive integer
if ! [[ "$CHUNK_HOURS" =~ ^[0-9]+$ ]]; then
    echo "Error: --chunk-hours must be a positive integer"
    exit 1
fi

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
# Returns: 0 on success, 1 on failure
# Output is written to stdout, errors to stderr
influxdb_query() {
    local query="$1"
    local format="${2:-csv}"
    local args=(--database "$INFLUXDB_DATABASE" --format "$format")
    local result
    local exit_code

    if [[ -n "$INFLUXDB_TOKEN" ]]; then
        args+=(--token "$INFLUXDB_TOKEN")
    fi

    # Capture both stdout and exit code, allow command to fail without triggering set -e
    result=$(docker exec "$INFLUXDB_CONTAINER" influxdb3 query "${args[@]}" "$query" 2>&1) || exit_code=$?
    exit_code=${exit_code:-0}

    # Check for error patterns (InfluxDB sometimes returns errors with exit code 0)
    # Match common error indicators - be careful not to match data containing these words
    if echo "$result" | grep -qiE "^(error:|Error |Query command failed|Query would|unauthorized|401|500 Internal)"; then
        echo "$result" >&2
        return 1
    fi

    if [[ $exit_code -ne 0 ]]; then
        echo "$result" >&2
        return 1
    fi

    echo "$result"
    return 0
}

# Function to prompt user for date
prompt_for_date() {
    local prompt="$1"
    local default="$2"
    local result

    echo -n "$prompt [default: $default]: " >&2
    read -r result
    if [[ -z "$result" ]]; then
        result="$default"
    fi
    # Return only the result value
    echo "$result"
}

# Function to get date range of data
get_date_range() {
    echo "Determining data date range..."

    # If manual dates provided, use them directly
    if [[ -n "$START_DATE" ]]; then
        # Convert date format to ISO timestamp
        EXPORT_MIN_TIME="${START_DATE}T00:00:00Z"
        if [[ -n "$END_DATE" ]]; then
            EXPORT_MAX_TIME="${END_DATE}T23:59:59Z"
        else
            # Default end date to today
            EXPORT_MAX_TIME="$(date -u +"%Y-%m-%dT23:59:59Z")"
        fi
        echo "  Using provided date range: $EXPORT_MIN_TIME to $EXPORT_MAX_TIME"
        echo "$EXPORT_MIN_TIME" > "$EXPORT_DIR/min_time.txt"
        echo "$EXPORT_MAX_TIME" > "$EXPORT_DIR/max_time.txt"
        return 0
    fi

    # Get the earliest and latest timestamps with error checking
    local min_result max_result min_time max_time

    if ! min_result=$(influxdb_query "SELECT MIN(time) as min_time FROM device_metrics" 2>&1); then
        # Check if this is the parquet file limit error
        if echo "$min_result" | grep -qi "parquet file"; then
            echo ""
            echo "Query exceeded parquet file limit (large dataset detected)."
            echo "Please provide a start date for the export."
            echo ""
            local start_input end_input
            start_input=$(prompt_for_date "Enter start date (YYYY-MM-DD)" "2024-01-01")
            end_input=$(prompt_for_date "Enter end date (YYYY-MM-DD)" "$(date -u +"%Y-%m-%d")")

            EXPORT_MIN_TIME="${start_input}T00:00:00Z"
            EXPORT_MAX_TIME="${end_input}T23:59:59Z"
            echo ""
            echo "  Using date range: $EXPORT_MIN_TIME to $EXPORT_MAX_TIME"
            echo "$EXPORT_MIN_TIME" > "$EXPORT_DIR/min_time.txt"
            echo "$EXPORT_MAX_TIME" > "$EXPORT_DIR/max_time.txt"
            return 0
        fi
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
        # Check if this is the parquet file limit error
        if echo "$max_result" | grep -qi "parquet file"; then
            echo ""
            echo "Query exceeded parquet file limit (large dataset detected)."
            echo "Please provide an end date for the export."
            echo ""
            local end_input
            end_input=$(prompt_for_date "Enter end date (YYYY-MM-DD)" "$(date -u +"%Y-%m-%d")")

            EXPORT_MIN_TIME="$min_time"
            EXPORT_MAX_TIME="${end_input}T23:59:59Z"
            echo ""
            echo "  Using date range: $EXPORT_MIN_TIME to $EXPORT_MAX_TIME"
            echo "$EXPORT_MIN_TIME" > "$EXPORT_DIR/min_time.txt"
            echo "$EXPORT_MAX_TIME" > "$EXPORT_DIR/max_time.txt"
            return 0
        fi
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

# Get total row count first (skip if using manual dates to avoid hitting parquet limit)
total_estimate="unknown"
if [[ -z "$START_DATE" ]]; then
    echo "Counting total rows..."
    if ! count_result=$(influxdb_query "SELECT COUNT(*) as cnt FROM device_metrics" 2>&1); then
        # Check for parquet file limit error
        if echo "$count_result" | grep -qi "parquet file"; then
            echo "  Row count query exceeded parquet file limit, using chunked export"
        else
            echo "Warning: Failed to get row count estimate: $count_result"
        fi
    elif [[ -z "$count_result" ]]; then
        echo "Warning: Empty result when getting row count"
    else
        total_estimate=$(echo "$count_result" | tail -n 1 | tr -d ' ')
        echo "  Estimated total rows: $total_estimate"
    fi
else
    echo "Using manual date range, skipping row count query"
fi
echo ""

# Large dataset threshold (1 million rows)
LARGE_DATASET_THRESHOLD=1000000

# Use SELECT * to export all columns dynamically (schema may vary between deployments)

# Check if we should use chunked export
# Always use chunked export when manual dates provided (to avoid hitting limits)
use_chunked=false
if [[ -n "$START_DATE" ]]; then
    use_chunked=true
    echo "Using chunked export with manual date range (${CHUNK_HOURS}-hour chunks)..."
elif [[ "$total_estimate" == "unknown" ]]; then
    use_chunked=true
    echo "Using chunked export (row count unknown, ${CHUNK_HOURS}-hour chunks)..."
elif [[ "$total_estimate" -gt "$LARGE_DATASET_THRESHOLD" ]]; then
    use_chunked=true
    echo "Large dataset detected. Using chunked export (${CHUNK_HOURS}-hour chunks)..."
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

    # Initialize output file (header will be written from first chunk)
    > "$output_file"
    header_written=false

    current_time="$EXPORT_MIN_TIME"
    chunk_num=0
    total_rows=0
    max_chunks=5000  # Safety limit to prevent infinite loops (allows ~208 days with 1-hour chunks)

    while [[ "$current_time" < "$EXPORT_MAX_TIME" ]] && [[ $chunk_num -lt $max_chunks ]]; do
        chunk_num=$((chunk_num + 1))

        # Calculate end time for this chunk with proper error handling (using hours)
        # GNU date: understands ISO 8601 with Z suffix natively
        # BSD date (macOS): needs TZ=UTC and Z stripped from input since -f treats Z as literal
        stripped_time="${current_time%Z}"  # Remove trailing Z for BSD date

        end_time=$($DATE_CMD -u -d "$current_time + $CHUNK_HOURS hours" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || \
        end_time=$(TZ=UTC $DATE_CMD -j -v+${CHUNK_HOURS}H -f "%Y-%m-%dT%H:%M:%S" "$stripped_time" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null) || {
            echo "Error: Failed to calculate chunk end time. Install GNU coreutils or check date format."
            exit 1
        }

        # Don't go past max time
        if [[ "$end_time" > "$EXPORT_MAX_TIME" ]]; then
            end_time="$EXPORT_MAX_TIME"
        fi

        echo -n "  Chunk $chunk_num ($current_time to $end_time)... "

        chunk_query="SELECT * FROM device_metrics WHERE time >= '$current_time' AND time < '$end_time' ORDER BY time"

        # Export chunk with explicit error handling
        chunk_result=""
        query_failed=false

        # Run query and capture result, preventing set -e from triggering
        if ! chunk_result=$(influxdb_query "$chunk_query" 2>&1); then
            query_failed=true
        fi

        # Check for query failure or error patterns in result
        if [[ "$query_failed" == true ]] || echo "$chunk_result" | grep -qiE "^(error:|Error |Query command failed|Query would|unauthorized|401|500 Internal)"; then
            echo "FAILED"
            echo "  Error: $chunk_result"
            rm -f "$output_file"
            # Record failure in manifest for debugging
            cat > "$EXPORT_DIR/manifest.json" <<FAILEOF
{
    "export_time": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "source": "influxdb3",
    "database": "$INFLUXDB_DATABASE",
    "status": "FAILED",
    "failed_chunk": $chunk_num,
    "failed_time_range": "$current_time to $end_time",
    "error": "Chunk export failed"
}
FAILEOF
            exit 1
        fi

        # Process successful result
        if [[ -n "$chunk_result" ]]; then
            # Write result to temp file to avoid SIGPIPE issues with large data in pipes
            chunk_file=$(mktemp)
            printf '%s\n' "$chunk_result" > "$chunk_file"

            # Validate result looks like CSV (should have a header with commas)
            first_line=$(head -n 1 "$chunk_file")
            if [[ ! "$first_line" =~ , ]] || [[ "$first_line" =~ ^[Ee]rror ]] || [[ "$first_line" =~ ^Query ]]; then
                echo "FAILED (invalid response: $first_line)"
                rm -f "$output_file" "$chunk_file"
                exit 1
            fi

            # Write header from first chunk that has data
            if [[ "$header_written" == false ]]; then
                head -n 1 "$chunk_file" > "$output_file"
                header_written=true
            fi
            # Append data (skip header line)
            tail -n +2 "$chunk_file" >> "$output_file"
            chunk_rows=$(tail -n +2 "$chunk_file" | wc -l)
            total_rows=$((total_rows + chunk_rows))
            rm -f "$chunk_file"
            echo "$chunk_rows rows"
        else
            echo "0 rows"
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
