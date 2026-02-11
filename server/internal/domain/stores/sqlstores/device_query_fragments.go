package sqlstores

import (
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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

// minerBaseQuery is the base SELECT/FROM/JOIN/WHERE for miner state queries.
// Uses discovered_device as the base table since it contains all devices (paired and unpaired).
// Parameter: $1 = org_id
const minerBaseQuery = `SELECT
    discovered_device.device_identifier,
    COALESCE(device.mac_address, '') as mac_address,
    device.serial_number,
    discovered_device.model,
    discovered_device.manufacturer,
    discovered_device.type,
    discovered_device.firmware_version,
    device_status.status as device_status,
    device_status.status_timestamp,
    device_status.status_details,
    discovered_device.ip_address,
    discovered_device.port,
    discovered_device.url_scheme,
    ` + pairingStatusExpr + ` as pairing_status,
    discovered_device.id as cursor_id,
    COALESCE(device.id, 0) as device_id
FROM discovered_device
LEFT JOIN device ON discovered_device.id = device.discovered_device_id
    AND device.deleted_at IS NULL
    AND device.org_id = $1
LEFT JOIN device_pairing ON device.id = device_pairing.device_id
LEFT JOIN device_status ON device.id = device_status.device_id
WHERE discovered_device.org_id = $1
    AND discovered_device.is_active = TRUE
    AND discovered_device.deleted_at IS NULL`

// minerBaseQueryWithTelemetry is the base query with an additional sort_telemetry_value column.
// Used when sorting by telemetry fields (hashrate, temperature, power, efficiency, issues).
// Parameter: $1 = org_id
const minerBaseQueryWithTelemetry = `SELECT
    discovered_device.device_identifier,
    COALESCE(device.mac_address, '') as mac_address,
    device.serial_number,
    discovered_device.model,
    discovered_device.manufacturer,
    discovered_device.type,
    discovered_device.firmware_version,
    device_status.status as device_status,
    device_status.status_timestamp,
    device_status.status_details,
    discovered_device.ip_address,
    discovered_device.port,
    discovered_device.url_scheme,
    ` + pairingStatusExpr + ` as pairing_status,
    discovered_device.id as cursor_id,
    COALESCE(device.id, 0) as device_id,
    latest_metrics.sort_telemetry_value
FROM discovered_device
LEFT JOIN device ON discovered_device.id = device.discovered_device_id
    AND device.deleted_at IS NULL
    AND device.org_id = $1
LEFT JOIN device_pairing ON device.id = device_pairing.device_id
LEFT JOIN device_status ON device.id = device_status.device_id`

// actionableErrorSeverities defines which error severities trigger "needs attention" state.
// Values: 1=CRITICAL, 2=ERROR, 3=WARNING. Excludes INFO=4 and UNSPECIFIED=0.
const actionableErrorSeverities = "errors.severity IN (1, 2, 3)"

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
	stores.SortFieldName:        "TRIM(COALESCE(discovered_device.manufacturer, '') || ' ' || COALESCE(discovered_device.model, ''))",
	stores.SortFieldIPAddress:   "discovered_device.ip_address",
	stores.SortFieldMACAddress:  "COALESCE(device.mac_address, '')",
	stores.SortFieldStatus:      "device_status.status",
	stores.SortFieldDeviceType:  "discovered_device.type",
	stores.SortFieldHashrate:    "latest_metrics.sort_telemetry_value",
	stores.SortFieldTemperature: "latest_metrics.sort_telemetry_value",
	stores.SortFieldPower:       "latest_metrics.sort_telemetry_value",
	stores.SortFieldEfficiency:  "latest_metrics.sort_telemetry_value",
	stores.SortFieldIssues:      "latest_metrics.sort_telemetry_value",
	stores.SortFieldFirmware:    "discovered_device.firmware_version",
}

// latestMetricsCTE is the Common Table Expression that fetches the latest telemetry
// values for sorting. It extracts the appropriate metric based on the sort field.
// Parameter: $1 = org_id, uses sort_metric_type placeholder for the metric selector
var latestMetricsCTE = `WITH latest_metrics AS (
    SELECT DISTINCT ON (m.device_id)
        m.device_id,
        %s as sort_telemetry_value
    FROM metrics m
    INNER JOIN device d2 ON m.device_id = d2.id
        AND d2.deleted_at IS NULL
        AND d2.org_id = $1
    WHERE m.timestamp > NOW() - INTERVAL '` + telemetryFreshnessWindow + `'
    ORDER BY m.device_id, m.timestamp DESC
)`

// minerTelemetryJoin is the LEFT JOIN clause for telemetry sorting queries.
const minerTelemetryJoin = `LEFT JOIN latest_metrics ON device.id = latest_metrics.device_id`

// getTelemetryMetricExpression returns the SQL expression for extracting
// the sort value from the metrics table for the given sort field.
// Only telemetry-based fields have metric expressions; all others return "NULL".
func getTelemetryMetricExpression(field stores.SortField) string {
	//nolint:exhaustive // Non-telemetry fields intentionally return "NULL" via default
	switch field {
	case stores.SortFieldHashrate:
		return "m.hashrate"
	case stores.SortFieldTemperature:
		return "m.temperature"
	case stores.SortFieldPower:
		return "m.power"
	case stores.SortFieldEfficiency:
		return "CASE WHEN m.hashrate > 0 THEN m.power / m.hashrate ELSE NULL END"
	case stores.SortFieldIssues:
		return "(m.hashboard_errors + m.fan_errors + m.psu_errors)"
	default:
		return "NULL"
	}
}
