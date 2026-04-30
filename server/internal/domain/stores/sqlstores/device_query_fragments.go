package sqlstores

import (
	"fmt"

	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// SQL fragments for dynamically building miner queries.
//
// We use a query builder instead of sqlc because:
// - sqlc generates static queries - you can't parameterize ORDER BY direction
// - sqlc doesn't support dynamic column selection for sorting
// - Keyset pagination requires dynamic comparison operators based on sort direction
//
// Instead, we use sqlc solely for type generation (see device.sql WHERE FALSE query)
// and scan results into sqlc-generated ListMinerStateSnapshotsRow.

// pairingStatusExpr returns 'UNPAIRED' for devices without a device record
// or the actual pairing status for paired devices.
const pairingStatusExpr = "CASE WHEN device.id IS NOT NULL THEN COALESCE(device_pairing.pairing_status::text, 'UNPAIRED') ELSE 'UNPAIRED' END"

// minerSelectColumns contains the common SELECT columns for miner state queries.
const minerSelectColumns = `SELECT
    discovered_device.device_identifier,
    COALESCE(device.mac_address, '') as mac_address,
    device.serial_number,
    discovered_device.model,
    discovered_device.manufacturer,
    discovered_device.firmware_version,
    device.worker_name,
    device_status.status as device_status,
    device_status.status_timestamp,
    device_status.status_details,
    discovered_device.ip_address,
    discovered_device.port,
    discovered_device.url_scheme,
    ` + pairingStatusExpr + ` as pairing_status,
    discovered_device.id as cursor_id,
    COALESCE(device.id, 0) as device_id,
    discovered_device.driver_name,
    device.custom_name`

// minerFromJoins contains the FROM clause and LEFT JOINs for miner state queries.
// Parameter: $1 = org_id (used in device join condition)
const minerFromJoins = `
FROM discovered_device
LEFT JOIN device ON discovered_device.id = device.discovered_device_id
    AND device.deleted_at IS NULL
    AND device.org_id = $1
LEFT JOIN device_pairing ON device.id = device_pairing.device_id
LEFT JOIN device_status ON device.id = device_status.device_id`

// minerWhereClause constrains results to the org's active, non-deleted devices.
// Parameter: $1 = org_id
const minerWhereClause = `
WHERE discovered_device.org_id = $1
    AND discovered_device.is_active = TRUE
    AND discovered_device.deleted_at IS NULL`

// minerBaseQuery is the base SELECT/FROM/JOIN/WHERE for miner state queries.
// Uses discovered_device as the base table since it contains all devices (paired and unpaired).
// Includes NULL sort_value column for consistent column count across all query variants.
// Parameter: $1 = org_id
const minerBaseQuery = minerSelectColumns + `,
    NULL::float8 as sort_value` + minerFromJoins + minerWhereClause

// minerBaseQueryWithSortValue builds the base query with a custom sort value column.
// Used for telemetry and issues sorting where we need an additional sort_value column.
// Does NOT include WHERE clause - caller must add minerWhereClause after any additional JOINs.
// Parameter: $1 = org_id
func minerBaseQueryWithSortValue(sortValueExpr string) string {
	return minerSelectColumns + `,
    ` + sortValueExpr + ` as sort_value` + minerFromJoins
}

// actionableErrorSeverities defines which error severities trigger "needs attention" state.
// 1=CRITICAL, 2=MAJOR, 3=MINOR, 4=INFO. Excludes UNSPECIFIED (0), which is normalized
// to a real severity at ingestion by miner_error_mapper.
const (
	actionableErrorSeverityList      = "(1, 2, 3, 4)"
	actionableErrorComponentTypeList = "(1, 2, 3, 4)"
	actionablePairingStatusList      = "('PAIRED', 'AUTHENTICATION_NEEDED')"
	actionableErrorSeverities        = "errors.severity IN " + actionableErrorSeverityList
)

func actionableErrorSeveritiesExpr(alias string) string {
	return fmt.Sprintf("%s.severity IN %s", alias, actionableErrorSeverityList)
}

func actionableErrorComponentTypesExpr(alias string) string {
	return fmt.Sprintf("%s.component_type IN %s", alias, actionableErrorComponentTypeList)
}

func actionablePairingStatusesExpr(alias string) string {
	return fmt.Sprintf("%s.pairing_status IN %s", alias, actionablePairingStatusList)
}

// nonActionableStatuses defines device statuses where errors should not trigger
// the "needs attention" state. These statuses take precedence.
const nonActionableStatuses = "('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')"

// telemetryFreshnessWindow defines how recent telemetry data must be to be included in sorts.
// Devices without metrics within this window will have NULL sort values.
const telemetryFreshnessWindow = "10 minutes"

// sortExpressions maps sort fields to their SQL expressions.
// These expressions are used in ORDER BY clauses and keyset pagination conditions.
// SAFETY: All expressions come from this fixed map; user input only selects the map key.
var sortExpressions = map[stores.SortField]string{
	stores.SortFieldName:        "TRIM(COALESCE(NULLIF(device.custom_name, ''), COALESCE(discovered_device.manufacturer, '') || ' ' || COALESCE(discovered_device.model, '')))",
	stores.SortFieldIPAddress:   "INET(COALESCE(NULLIF(discovered_device.ip_address, ''), '0.0.0.0'))",
	stores.SortFieldMACAddress:  "COALESCE(device.mac_address, '')",
	stores.SortFieldModel:       "discovered_device.model",
	stores.SortFieldHashrate:    "latest_metrics.sort_value",
	stores.SortFieldTemperature: "latest_metrics.sort_value",
	stores.SortFieldPower:       "latest_metrics.sort_value",
	stores.SortFieldEfficiency:  "latest_metrics.sort_value",
	stores.SortFieldFirmware:    "discovered_device.firmware_version",
	stores.SortFieldWorkerName:  "device.worker_name",
}

// latestMetricsCTE is the Common Table Expression that fetches the latest
// telemetry values within telemetryFreshnessWindow. All comparable telemetry
// columns are projected so numeric range filters can reference them via
// latest_metrics.<col>; sort_value carries the (possibly NULL) sort expression.
// Parameter: $1 = org_id, uses sort_metric_type placeholder for the metric selector
var latestMetricsCTE = `WITH latest_metrics AS (
    SELECT DISTINCT ON (device_metrics.device_identifier)
        device_metrics.device_identifier,
        device_metrics.hash_rate_hs,
        device_metrics.efficiency_jh,
        device_metrics.power_w,
        device_metrics.temp_c,
        device_metrics.voltage_v,
        device_metrics.current_a,
        %s as sort_value
    FROM device_metrics
    INNER JOIN device d2 ON device_metrics.device_identifier = d2.device_identifier
        AND d2.deleted_at IS NULL
        AND d2.org_id = $1
    WHERE device_metrics.time > NOW() - INTERVAL '` + telemetryFreshnessWindow + `'
    ORDER BY device_metrics.device_identifier, device_metrics.time DESC
)`

// minerTelemetryJoin is the LEFT JOIN used when telemetry is needed for sort
// only; rows without recent metrics still appear with NULL sort values.
const minerTelemetryJoin = `LEFT JOIN latest_metrics ON device.device_identifier = latest_metrics.device_identifier`

// minerTelemetryInnerJoin is the INNER JOIN used when a numeric filter
// references latest_metrics columns. Missing telemetry naturally excludes
// the row, matching the UI's em-dash rendering for stale cells.
const minerTelemetryInnerJoin = `INNER JOIN latest_metrics ON device.device_identifier = latest_metrics.device_identifier`

// getTelemetryMetricExpression returns the SQL expression for extracting
// the sort value from the device_metrics table for the given sort field.
// Only telemetry-based fields have metric expressions; all others return "NULL".
func getTelemetryMetricExpression(field stores.SortField) string {
	//nolint:exhaustive // Non-telemetry fields intentionally return "NULL" via default
	switch field {
	case stores.SortFieldHashrate:
		return "device_metrics.hash_rate_hs"
	case stores.SortFieldTemperature:
		return "device_metrics.temp_c"
	case stores.SortFieldPower:
		return "device_metrics.power_w"
	case stores.SortFieldEfficiency:
		return "device_metrics.efficiency_jh"
	default:
		return "NULL"
	}
}
