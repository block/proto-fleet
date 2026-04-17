package interfaces

import "fmt"

// SortField represents a field to sort miners by.
type SortField int32

// Sort field constants matching proto SortField enum values.
// Note: Status (4) is reserved/removed - sorting by this field is not supported.
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
	SortFieldIssueCount  SortField = 15
	SortFieldFirmware    SortField = 11
	SortFieldDeviceCount SortField = 12
	SortFieldLocation    SortField = 13
	SortFieldWorkerName  SortField = 14
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

	switch c.Field { //nolint:exhaustive // collection-only and unspecified fields are invalid here
	case SortFieldName,
		SortFieldIPAddress,
		SortFieldMACAddress,
		SortFieldModel,
		SortFieldHashrate,
		SortFieldTemperature,
		SortFieldPower,
		SortFieldEfficiency,
		SortFieldFirmware,
		SortFieldDeviceCount,
		SortFieldWorkerName:
	default:
		return false
	}

	switch c.Direction { //nolint:exhaustive // unspecified direction is invalid here
	case SortDirectionAsc, SortDirectionDesc:
		return true
	default:
		return false
	}
}

// IsTelemetrySort returns true if sorting by a telemetry-derived field.
// These fields require the latest_metrics CTE and telemetry join.
func (c *SortConfig) IsTelemetrySort() bool {
	if c == nil {
		return false
	}

	switch c.Field { //nolint:exhaustive // only telemetry-derived fields return true
	case SortFieldHashrate, SortFieldTemperature, SortFieldPower, SortFieldEfficiency:
		return true
	default:
		return false
	}
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
