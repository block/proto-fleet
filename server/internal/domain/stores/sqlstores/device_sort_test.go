package sqlstores

import (
	"database/sql"
	"fmt"
	"testing"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestGetSortExpression(t *testing.T) {
	tests := []struct {
		name     string
		field    stores.SortField
		expected string
	}{
		{"name field", stores.SortFieldName, "TRIM(COALESCE(NULLIF(device.custom_name, ''), COALESCE(discovered_device.manufacturer, '') || ' ' || COALESCE(discovered_device.model, '')))"},
		{"ip address field", stores.SortFieldIPAddress, "INET(COALESCE(NULLIF(discovered_device.ip_address, ''), '0.0.0.0'))"},
		{"mac address field", stores.SortFieldMACAddress, "COALESCE(device.mac_address, '')"},
		{"model field", stores.SortFieldModel, "discovered_device.model"},
		{"hashrate field", stores.SortFieldHashrate, "latest_metrics.sort_value"},
		{"temperature field", stores.SortFieldTemperature, "latest_metrics.sort_value"},
		{"power field", stores.SortFieldPower, "latest_metrics.sort_value"},
		{"efficiency field", stores.SortFieldEfficiency, "latest_metrics.sort_value"},
		{"firmware field", stores.SortFieldFirmware, "discovered_device.firmware_version"},
		{"worker name field", stores.SortFieldWorkerName, "device.worker_name"},
		{"unspecified field", stores.SortFieldUnspecified, ""},
		{"unknown field", stores.SortField(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSortExpression(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildSortOrderClause(t *testing.T) {
	t.Run("nil config defaults to name ASC", func(t *testing.T) {
		// Arrange & Act
		result := buildSortOrderClause(nil)

		// Assert
		assert.Contains(t, result, "ORDER BY")
		assert.Contains(t, result, "ASC")
		assert.Contains(t, result, "NULLS LAST")
		assert.Contains(t, result, "COALESCE(discovered_device.manufacturer")
	})

	t.Run("ascending sort by name", func(t *testing.T) {
		// Arrange
		config := &stores.SortConfig{
			Field:     stores.SortFieldName,
			Direction: stores.SortDirectionAsc,
		}

		// Act
		result := buildSortOrderClause(config)

		// Assert
		assert.Contains(t, result, "ORDER BY")
		assert.Contains(t, result, "ASC")
		assert.Contains(t, result, "NULLS LAST")
		assert.Contains(t, result, "discovered_device.id ASC")
	})

	t.Run("descending sort by hashrate", func(t *testing.T) {
		config := &stores.SortConfig{
			Field:     stores.SortFieldHashrate,
			Direction: stores.SortDirectionDesc,
		}

		result := buildSortOrderClause(config)

		assert.Contains(t, result, "ORDER BY")
		assert.Contains(t, result, "DESC")
		assert.Contains(t, result, "NULLS LAST")
		assert.Contains(t, result, "discovered_device.id DESC")
	})
}

func TestBuildKeysetSQL(t *testing.T) {
	t.Run("nil config uses name sort ASC", func(t *testing.T) {
		// Arrange
		cursor := &sortedCursor{
			SortValue: "Bitmain S21",
			CursorID:  50,
		}

		// Act
		sql, args := buildKeysetSQL(cursor, nil, 2)

		// Assert - uses name expression with ASC (> operator)
		assert.Contains(t, sql, "> ($2, $3)")
		assert.Contains(t, sql, "::text")
		assert.Contains(t, sql, "COALESCE(discovered_device.manufacturer")
		assert.Equal(t, []any{"Bitmain S21", int64(50)}, args)
	})

	t.Run("ascending non-telemetry sort", func(t *testing.T) {
		// Arrange
		cursor := &sortedCursor{
			SortValue: "Bitmain",
			CursorID:  50,
		}
		config := &stores.SortConfig{
			Field:     stores.SortFieldName,
			Direction: stores.SortDirectionAsc,
		}

		// Act
		sql, args := buildKeysetSQL(cursor, config, 2)

		// Assert
		assert.Contains(t, sql, "> ($2, $3)")
		assert.Contains(t, sql, "::text")
		assert.Equal(t, []any{"Bitmain", int64(50)}, args)
	})

	t.Run("descending non-telemetry sort", func(t *testing.T) {
		cursor := &sortedCursor{
			SortValue: "AA:BB:CC:DD:EE:FF",
			CursorID:  75,
		}
		config := &stores.SortConfig{
			Field:     stores.SortFieldMACAddress,
			Direction: stores.SortDirectionDesc,
		}

		sql, args := buildKeysetSQL(cursor, config, 2)

		assert.Contains(t, sql, "< ($2, $3)")
		assert.Equal(t, []any{"AA:BB:CC:DD:EE:FF", int64(75)}, args)
	})

	t.Run("telemetry sort with NULL value", func(t *testing.T) {
		cursor := &sortedCursor{
			SortValue: "", // NULL telemetry
			CursorID:  25,
		}
		config := &stores.SortConfig{
			Field:     stores.SortFieldHashrate,
			Direction: stores.SortDirectionAsc,
		}

		sql, args := buildKeysetSQL(cursor, config, 2)

		assert.Contains(t, sql, "IS NULL")
		assert.Contains(t, sql, "discovered_device.id > $2")
		assert.Equal(t, []any{int64(25)}, args)
	})

	t.Run("telemetry sort with value includes NULL fallback", func(t *testing.T) {
		cursor := &sortedCursor{
			SortValue: "123.5",
			CursorID:  30,
		}
		config := &stores.SortConfig{
			Field:     stores.SortFieldTemperature,
			Direction: stores.SortDirectionDesc,
		}

		sql, args := buildKeysetSQL(cursor, config, 2)

		assert.Contains(t, sql, "< ($2, $3)")
		assert.Contains(t, sql, "OR")
		assert.Contains(t, sql, "IS NULL")
		assert.Equal(t, []any{"123.5", int64(30)}, args)
	})
}

func TestGetTelemetryMetricExpression(t *testing.T) {
	tests := []struct {
		name     string
		field    stores.SortField
		expected string
	}{
		{"hashrate", stores.SortFieldHashrate, "device_metrics.hash_rate_hs"},
		{"temperature", stores.SortFieldTemperature, "device_metrics.temp_c"},
		{"power", stores.SortFieldPower, "device_metrics.power_w"},
		{"efficiency", stores.SortFieldEfficiency, "device_metrics.efficiency_jh"},
		{"non-telemetry field", stores.SortFieldName, "NULL"},
		{"unspecified", stores.SortFieldUnspecified, "NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTelemetryMetricExpression(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBuildKeysetSQL_TelemetrySortNullHandling verifies NULL handling in telemetry sorts.
func TestBuildKeysetSQL_TelemetrySortNullHandling(t *testing.T) {
	telemetryFields := []stores.SortField{
		stores.SortFieldHashrate,
		stores.SortFieldTemperature,
		stores.SortFieldPower,
		stores.SortFieldEfficiency,
	}

	for _, field := range telemetryFields {
		t.Run(fmt.Sprintf("%v with NULL value ascending", field), func(t *testing.T) {
			// Cursor from a row with NULL telemetry
			cursor := &sortedCursor{
				SortField:     field,
				SortDirection: stores.SortDirectionAsc,
				SortValue:     "", // NULL
				CursorID:      100,
			}
			config := &stores.SortConfig{
				Field:     field,
				Direction: stores.SortDirectionAsc,
			}

			sql, args := buildKeysetSQL(cursor, config, 2)

			// Should only compare IDs among NULL rows
			assert.Contains(t, sql, "IS NULL")
			assert.Contains(t, sql, "discovered_device.id > $2")
			assert.Equal(t, []any{int64(100)}, args)
		})

		t.Run(fmt.Sprintf("%v with NULL value descending", field), func(t *testing.T) {
			cursor := &sortedCursor{
				SortField:     field,
				SortDirection: stores.SortDirectionDesc,
				SortValue:     "", // NULL
				CursorID:      100,
			}
			config := &stores.SortConfig{
				Field:     field,
				Direction: stores.SortDirectionDesc,
			}

			sql, args := buildKeysetSQL(cursor, config, 2)

			// Descending uses < operator
			assert.Contains(t, sql, "IS NULL")
			assert.Contains(t, sql, "discovered_device.id < $2")
			assert.Equal(t, []any{int64(100)}, args)
		})

		t.Run(fmt.Sprintf("%v with non-NULL value includes NULL fallback", field), func(t *testing.T) {
			// Cursor from row with actual telemetry value
			cursor := &sortedCursor{
				SortField:     field,
				SortDirection: stores.SortDirectionDesc,
				SortValue:     "123.45",
				CursorID:      100,
			}
			config := &stores.SortConfig{
				Field:     field,
				Direction: stores.SortDirectionDesc,
			}

			sql, args := buildKeysetSQL(cursor, config, 2)

			// Should include OR clause for NULL values (they sort last)
			assert.Contains(t, sql, "< ($2, $3)")
			assert.Contains(t, sql, "OR")
			assert.Contains(t, sql, "IS NULL")
			assert.Equal(t, []any{"123.45", int64(100)}, args)
		})
	}
}

func TestExtractSortValueForCursorFromRow_NameField(t *testing.T) {
	t.Run("custom name takes precedence over manufacturer+model", func(t *testing.T) {
		row := minerStateRow{
			ListMinerStateSnapshotsRow: sqlc.ListMinerStateSnapshotsRow{
				CustomName:   sql.NullString{String: "My Miner", Valid: true},
				Manufacturer: sql.NullString{String: "Bitmain", Valid: true},
				Model:        sql.NullString{String: "S21", Valid: true},
			},
		}
		config := &stores.SortConfig{Field: stores.SortFieldName, Direction: stores.SortDirectionAsc}

		result := extractSortValueForCursorFromRow(row, config)

		assert.Equal(t, "My Miner", result)
	})

	t.Run("falls back to manufacturer+model when no custom name", func(t *testing.T) {
		row := minerStateRow{
			ListMinerStateSnapshotsRow: sqlc.ListMinerStateSnapshotsRow{
				CustomName:   sql.NullString{Valid: false},
				Manufacturer: sql.NullString{String: "Bitmain", Valid: true},
				Model:        sql.NullString{String: "S21", Valid: true},
			},
		}
		config := &stores.SortConfig{Field: stores.SortFieldName, Direction: stores.SortDirectionAsc}

		result := extractSortValueForCursorFromRow(row, config)

		assert.Equal(t, "Bitmain S21", result)
	})

	t.Run("nil sort config defaults to name field and respects custom name", func(t *testing.T) {
		row := minerStateRow{
			ListMinerStateSnapshotsRow: sqlc.ListMinerStateSnapshotsRow{
				CustomName:   sql.NullString{String: "Renamed Miner", Valid: true},
				Manufacturer: sql.NullString{String: "MicroBT", Valid: true},
				Model:        sql.NullString{String: "M60", Valid: true},
			},
		}

		result := extractSortValueForCursorFromRow(row, nil)

		assert.Equal(t, "Renamed Miner", result)
	})
}
