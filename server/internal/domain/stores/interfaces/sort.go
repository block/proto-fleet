package interfaces

import "fmt"

// SortField represents a field to sort miners by.
type SortField int32

// Sort field constants matching proto SortField enum values.
// Note: Status (4) and Issues (10) are reserved/removed - sorting by these fields is not supported.
const (
	SortFieldUnspecified SortField = 0
	SortFieldName        SortField = 1
	SortFieldIPAddress   SortField = 2
	SortFieldMACAddress  SortField = 3
	SortFieldModel       SortField = 5
	SortFieldHashrate    SortField = 6
	SortFieldTemperature SortField = 7
	SortFieldPower       SortField = 8
	SortFieldEfficiency  SortField = 9
	SortFieldFirmware    SortField = 11
	SortFieldDeviceCount SortField = 12
)

// SortDirection represents the direction to sort results.
type SortDirection int32

// Sort direction constants matching proto SortDirection enum values.
const (
	SortDirectionUnspecified SortDirection = 0
	SortDirectionAsc         SortDirection = 1
	SortDirectionDesc        SortDirection = 2
)

// SortConfig holds sorting configuration extracted from proto messages.
type SortConfig struct {
	Field     SortField
	Direction SortDirection
}

// IsValid returns true if the sort config has a valid field and direction.
func (c *SortConfig) IsValid() bool {
	if c == nil {
		return false
	}

	validField := c.Field == SortFieldName ||
		c.Field == SortFieldIPAddress ||
		c.Field == SortFieldMACAddress ||
		c.Field == SortFieldModel ||
		c.Field == SortFieldHashrate ||
		c.Field == SortFieldTemperature ||
		c.Field == SortFieldPower ||
		c.Field == SortFieldEfficiency ||
		c.Field == SortFieldFirmware ||
		c.Field == SortFieldDeviceCount

	validDirection := c.Direction == SortDirectionAsc || c.Direction == SortDirectionDesc

	return validField && validDirection
}

// IsTelemetrySort returns true if sorting by a telemetry-derived field.
// These fields require the latest_metrics CTE and telemetry join.
func (c *SortConfig) IsTelemetrySort() bool {
	if c == nil {
		return false
	}
	return c.Field == SortFieldHashrate ||
		c.Field == SortFieldTemperature ||
		c.Field == SortFieldPower ||
		c.Field == SortFieldEfficiency
}

// IsUnspecified returns true if no sort is specified (use default).
func (c *SortConfig) IsUnspecified() bool {
	return c == nil || c.Field == SortFieldUnspecified
}

// String returns a string representation for logging.
func (c *SortConfig) String() string {
	if c == nil {
		return "SortConfig{nil}"
	}
	return fmt.Sprintf("SortConfig{Field:%d, Direction:%d}", c.Field, c.Direction)
}
