package sqlstores

import (
	"fmt"
	"strconv"
	"strings"

	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

func getSortExpression(field stores.SortField) string {
	if expr, ok := sortExpressions[field]; ok {
		return expr
	}
	return ""
}

func buildSortOrderClause(sortConfig *stores.SortConfig) string {
	if sortConfig == nil || sortConfig.IsUnspecified() {
		return "ORDER BY discovered_device.id ASC"
	}

	sortExpr := getSortExpression(sortConfig.Field)
	if sortExpr == "" {
		return "ORDER BY discovered_device.id ASC"
	}

	direction := "ASC"
	if sortConfig.Direction == stores.SortDirectionDesc {
		direction = "DESC"
	}

	return fmt.Sprintf("ORDER BY (%s) %s NULLS LAST, discovered_device.id %s", sortExpr, direction, direction)
}

// buildKeysetSQL builds the keyset pagination WHERE clause for sorted queries.
func buildKeysetSQL(cursor *sortedCursor, sortConfig *stores.SortConfig, argNum int) (string, []any) {
	if cursor == nil {
		return "", nil
	}

	if sortConfig == nil || sortConfig.IsUnspecified() {
		return fmt.Sprintf("AND discovered_device.id > $%d", argNum), []any{cursor.CursorID}
	}

	sortExpr := getSortExpression(sortConfig.Field)
	if sortExpr == "" {
		return fmt.Sprintf("AND discovered_device.id > $%d", argNum), []any{cursor.CursorID}
	}

	operator := ">"
	if sortConfig.Direction == stores.SortDirectionDesc {
		operator = "<"
	}

	// Handle NULL values for sorts on nullable columns
	if sortConfig.IsTelemetrySort() || sortConfig.IsIssuesSort() || canSortFieldBeNull(sortConfig.Field) {
		if cursor.SortValue == "" {
			// Cursor row had NULL value - only compare IDs among NULLs
			return fmt.Sprintf("AND (%s IS NULL AND discovered_device.id %s $%d)", sortExpr, operator, argNum), []any{cursor.CursorID}
		}
		// Cursor row had non-NULL value - include NULLs in results (they sort last)
		return fmt.Sprintf("AND ((%s, discovered_device.id) %s ($%d, $%d) OR %s IS NULL)", sortExpr, operator, argNum, argNum+1, sortExpr), []any{cursor.SortValue, cursor.CursorID}
	}

	// Non-nullable sorts: tuple comparison with text cast for consistent comparison
	return fmt.Sprintf("AND ((%s)::text, discovered_device.id) %s ($%d, $%d)", sortExpr, operator, argNum, argNum+1), []any{cursor.SortValue, cursor.CursorID}
}

// canSortFieldBeNull returns true for non-telemetry fields that can have NULL values.
func canSortFieldBeNull(field stores.SortField) bool {
	return field == stores.SortFieldStatus || field == stores.SortFieldFirmware
}

// extractSortValueForCursorFromRow extracts the sort field value from an extended row for cursor encoding.
func extractSortValueForCursorFromRow(row minerStateRow, sortConfig *stores.SortConfig) string {
	if sortConfig == nil {
		return ""
	}
	switch sortConfig.Field {
	case stores.SortFieldName:
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
	case stores.SortFieldStatus:
		if row.DeviceStatus.Valid {
			return string(row.DeviceStatus.DeviceStatusEnum)
		}
		return ""
	case stores.SortFieldModel:
		if row.Model.Valid {
			return row.Model.String
		}
		return ""
	case stores.SortFieldHashrate,
		stores.SortFieldTemperature,
		stores.SortFieldPower,
		stores.SortFieldEfficiency,
		stores.SortFieldIssues:
		// These sorts use the sort_value column
		if row.SortValue.Valid {
			return strconv.FormatFloat(row.SortValue.Float64, 'f', -1, 64)
		}
		return ""
	case stores.SortFieldFirmware:
		if row.FirmwareVersion.Valid {
			return row.FirmwareVersion.String
		}
		return ""
	case stores.SortFieldUnspecified:
		return ""
	}
	return ""
}
