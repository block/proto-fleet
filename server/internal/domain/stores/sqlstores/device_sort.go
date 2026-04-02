package sqlstores

import (
	"fmt"
	"strconv"
	"strings"

	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func getSortExpression(field stores.SortField) string {
	if expr, ok := sortExpressions[field]; ok {
		return expr
	}
	return ""
}

// defaultSortExpr is the expression used for default sorting (name field).
// This ensures consistent default sort behavior even when no sort config is provided.
var defaultSortExpr = sortExpressions[stores.SortFieldName]

func buildSortOrderClause(sortConfig *stores.SortConfig) string {
	sortExpr := defaultSortExpr
	direction := "ASC"

	if sortConfig != nil && !sortConfig.IsUnspecified() {
		if expr := getSortExpression(sortConfig.Field); expr != "" {
			sortExpr = expr
			if sortConfig.Direction == stores.SortDirectionDesc {
				direction = "DESC"
			}
		}
	}

	return fmt.Sprintf("ORDER BY (%s) %s NULLS LAST, discovered_device.id %s", sortExpr, direction, direction)
}

// buildKeysetSQL builds the keyset pagination WHERE clause for sorted queries.
func buildKeysetSQL(cursor *sortedCursor, sortConfig *stores.SortConfig, argNum int) (string, []any) {
	if cursor == nil {
		return "", nil
	}

	// Use name sort as default for consistency with buildSortOrderClause
	sortExpr := defaultSortExpr
	direction := stores.SortDirectionAsc
	isNullableSort := false
	if sortConfig != nil && !sortConfig.IsUnspecified() {
		expr := getSortExpression(sortConfig.Field)
		if expr != "" {
			sortExpr = expr
			direction = sortConfig.Direction
			isNullableSort = sortConfig.IsTelemetrySort() || canSortFieldBeNull(sortConfig.Field)
		}
	}

	operator := ">"
	if direction == stores.SortDirectionDesc {
		operator = "<"
	}

	// Handle NULL values for sorts on nullable columns
	if isNullableSort {
		if cursor.SortValue == "" {
			// Cursor row had NULL value - only compare IDs among NULLs
			return fmt.Sprintf("AND (%s IS NULL AND discovered_device.id %s $%d)", sortExpr, operator, argNum), []any{cursor.CursorID}
		}
		// Cursor row had non-NULL value - include NULLs in results (they sort last)
		return fmt.Sprintf("AND ((%s, discovered_device.id) %s ($%d, $%d) OR %s IS NULL)", sortExpr, operator, argNum, argNum+1, sortExpr), []any{cursor.SortValue, cursor.CursorID}
	}

	// IP address sort: tuple comparison with INET cast for numeric ordering
	if sortConfig != nil && sortConfig.Field == stores.SortFieldIPAddress {
		return fmt.Sprintf("AND ((%s), discovered_device.id) %s (INET(COALESCE(NULLIF($%d, ''), '0.0.0.0')), $%d)", sortExpr, operator, argNum, argNum+1), []any{cursor.SortValue, cursor.CursorID}
	}

	// Non-nullable sorts: tuple comparison with text cast for consistent comparison
	return fmt.Sprintf("AND ((%s)::text, discovered_device.id) %s ($%d, $%d)", sortExpr, operator, argNum, argNum+1), []any{cursor.SortValue, cursor.CursorID}
}

// canSortFieldBeNull returns true for non-telemetry fields that can have NULL values.
func canSortFieldBeNull(field stores.SortField) bool {
	return field == stores.SortFieldFirmware || field == stores.SortFieldWorkerName
}

// extractSortValueForCursorFromRow extracts the sort field value from an extended row for cursor encoding.
func extractSortValueForCursorFromRow(row minerStateRow, sortConfig *stores.SortConfig) string {
	field := stores.SortFieldName
	if sortConfig != nil && !sortConfig.IsUnspecified() {
		field = sortConfig.Field
	}

	switch field { //nolint:exhaustive // device_count not applicable to device rows
	case stores.SortFieldUnspecified, stores.SortFieldName:
		if row.CustomName.Valid && row.CustomName.String != "" {
			return strings.TrimSpace(row.CustomName.String)
		}
		manufacturer := ""
		if row.Manufacturer.Valid {
			manufacturer = row.Manufacturer.String
		}
		model := ""
		if row.Model.Valid {
			model = row.Model.String
		}
		return strings.TrimSpace(manufacturer + " " + model)
	case stores.SortFieldIPAddress:
		return row.IpAddress
	case stores.SortFieldMACAddress:
		return row.MacAddress
	case stores.SortFieldModel:
		if row.Model.Valid {
			return row.Model.String
		}
		return ""
	case stores.SortFieldHashrate,
		stores.SortFieldTemperature,
		stores.SortFieldPower,
		stores.SortFieldEfficiency:
		// Telemetry sorts use the sort_value column
		if row.SortValue.Valid {
			return strconv.FormatFloat(row.SortValue.Float64, 'f', -1, 64)
		}
		return ""
	case stores.SortFieldFirmware:
		if row.FirmwareVersion.Valid {
			return row.FirmwareVersion.String
		}
		return ""
	case stores.SortFieldWorkerName:
		if row.WorkerName.Valid {
			return row.WorkerName.String
		}
		return ""
	default:
		return ""
	}
}
