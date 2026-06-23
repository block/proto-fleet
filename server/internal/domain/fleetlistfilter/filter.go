package fleetlistfilter

import (
	"math"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const MaxFilterValues = 1024

type Filter struct {
	ErrorComponentTypes []int32
	TelemetryRanges     []interfaces.NumericRange
}

type Stats struct {
	HashrateReportingCount    int32
	EfficiencyReportingCount  int32
	PowerReportingCount       int32
	TemperatureReportingCount int32
	TotalHashrateThs          float64
	AvgEfficiencyJth          float64
	TotalPowerKw              float64
	MinTemperatureC           float64
	MaxTemperatureC           float64
	ControlBoardIssueCount    int32
	FanIssueCount             int32
	HashBoardIssueCount       int32
	PsuIssueCount             int32
}

func Parse(errorComponentTypes []errorspb.ComponentType, ranges []*commonpb.FleetListTelemetryRangeFilter) (Filter, error) {
	if len(errorComponentTypes) > MaxFilterValues {
		return Filter{}, fleeterror.NewInvalidArgumentErrorf(
			"error_component_types exceeds maximum of %d values", MaxFilterValues)
	}
	if len(ranges) > MaxFilterValues {
		return Filter{}, fleeterror.NewInvalidArgumentErrorf(
			"telemetry_ranges exceeds maximum of %d values", MaxFilterValues)
	}
	out := Filter{
		ErrorComponentTypes: make([]int32, 0, len(errorComponentTypes)),
		TelemetryRanges:     make([]interfaces.NumericRange, 0, len(ranges)),
	}
	for i, ct := range errorComponentTypes {
		if err := validateIssueComponentType(ct); err != nil {
			return Filter{}, fleeterror.NewInvalidArgumentErrorf(
				"error_component_types[%d]: %v", i, err)
		}
		out.ErrorComponentTypes = append(out.ErrorComponentTypes, int32(ct))
	}
	for i, r := range ranges {
		parsed, err := parseTelemetryRange(i, r)
		if err != nil {
			return Filter{}, err
		}
		out.TelemetryRanges = append(out.TelemetryRanges, parsed)
	}
	return out, nil
}

func validateIssueComponentType(ct errorspb.ComponentType) error {
	switch ct {
	case errorspb.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		errorspb.ComponentType_COMPONENT_TYPE_FAN,
		errorspb.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		errorspb.ComponentType_COMPONENT_TYPE_PSU:
		return nil
	case errorspb.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
		errorspb.ComponentType_COMPONENT_TYPE_EEPROM,
		errorspb.ComponentType_COMPONENT_TYPE_IO_MODULE:
		return fleeterror.NewInvalidArgumentErrorf("unsupported component type %v", ct)
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported component type %v", ct)
	}
}

func HasFilters(filter Filter) bool {
	return len(filter.ErrorComponentTypes) > 0 || len(filter.TelemetryRanges) > 0
}

func Matches(stats Stats, filter Filter) bool {
	if len(filter.ErrorComponentTypes) > 0 && !matchesIssues(stats, filter.ErrorComponentTypes) {
		return false
	}
	for _, r := range filter.TelemetryRanges {
		if !matchesRange(stats, r) {
			return false
		}
	}
	return true
}

func parseTelemetryRange(idx int, r *commonpb.FleetListTelemetryRangeFilter) (interfaces.NumericRange, error) {
	if r == nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"telemetry_ranges[%d] is nil", idx)
	}
	field, err := convertTelemetryField(r.GetField())
	if err != nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"telemetry_ranges[%d].field: %v", idx, err)
	}
	if r.GetMin() == nil && r.GetMax() == nil {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"telemetry_ranges[%d]: at least one of min or max must be set", idx)
	}
	out := interfaces.NumericRange{
		Field:        field,
		MinInclusive: r.GetMinInclusive(),
		MaxInclusive: r.GetMaxInclusive(),
	}
	if r.GetMin() != nil {
		v := r.GetMin().GetValue()
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
				"telemetry_ranges[%d].min must be finite", idx)
		}
		out.Min = &v
	}
	if r.GetMax() != nil {
		v := r.GetMax().GetValue()
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
				"telemetry_ranges[%d].max must be finite", idx)
		}
		out.Max = &v
	}
	if out.Min != nil && out.Max != nil && *out.Min > *out.Max {
		return interfaces.NumericRange{}, fleeterror.NewInvalidArgumentErrorf(
			"telemetry_ranges[%d]: min (%v) must not exceed max (%v)", idx, *out.Min, *out.Max)
	}
	return out, nil
}

func convertTelemetryField(field commonpb.FleetListTelemetryField) (interfaces.NumericFilterField, error) {
	switch field {
	case commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS:
		return interfaces.NumericFilterFieldHashrateTHs, nil
	case commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_EFFICIENCY_JTH:
		return interfaces.NumericFilterFieldEfficiencyJTH, nil
	case commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_POWER_KW:
		return interfaces.NumericFilterFieldPowerKW, nil
	case commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_TEMPERATURE_C:
		return interfaces.NumericFilterFieldTemperatureC, nil
	case commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_UNSPECIFIED:
		return interfaces.NumericFilterFieldUnspecified, fleeterror.NewInvalidArgumentError("must be specified")
	default:
		return interfaces.NumericFilterFieldUnspecified, fleeterror.NewInvalidArgumentErrorf("unsupported field %v", field)
	}
}

func matchesIssues(stats Stats, componentTypes []int32) bool {
	for _, ct := range componentTypes {
		switch errorspb.ComponentType(ct) {
		case errorspb.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
			if stats.ControlBoardIssueCount > 0 {
				return true
			}
		case errorspb.ComponentType_COMPONENT_TYPE_FAN:
			if stats.FanIssueCount > 0 {
				return true
			}
		case errorspb.ComponentType_COMPONENT_TYPE_HASH_BOARD:
			if stats.HashBoardIssueCount > 0 {
				return true
			}
		case errorspb.ComponentType_COMPONENT_TYPE_PSU:
			if stats.PsuIssueCount > 0 {
				return true
			}
		case errorspb.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
			errorspb.ComponentType_COMPONENT_TYPE_EEPROM,
			errorspb.ComponentType_COMPONENT_TYPE_IO_MODULE:
		}
	}
	return false
}

func matchesRange(stats Stats, r interfaces.NumericRange) bool {
	switch r.Field {
	case interfaces.NumericFilterFieldHashrateTHs:
		return stats.HashrateReportingCount > 0 && valueMatchesRange(stats.TotalHashrateThs, stats.TotalHashrateThs, r)
	case interfaces.NumericFilterFieldEfficiencyJTH:
		return stats.EfficiencyReportingCount > 0 && valueMatchesRange(stats.AvgEfficiencyJth, stats.AvgEfficiencyJth, r)
	case interfaces.NumericFilterFieldPowerKW:
		return stats.PowerReportingCount > 0 && valueMatchesRange(stats.TotalPowerKw, stats.TotalPowerKw, r)
	case interfaces.NumericFilterFieldTemperatureC:
		return stats.TemperatureReportingCount > 0 && valueMatchesRange(stats.MinTemperatureC, stats.MaxTemperatureC, r)
	case interfaces.NumericFilterFieldUnspecified,
		interfaces.NumericFilterFieldVoltageV,
		interfaces.NumericFilterFieldCurrentA:
		return false
	default:
		return false
	}
}

func valueMatchesRange(minValue, maxValue float64, r interfaces.NumericRange) bool {
	if r.Min != nil {
		if r.MinInclusive {
			if minValue < *r.Min {
				return false
			}
		} else if minValue <= *r.Min {
			return false
		}
	}
	if r.Max != nil {
		if r.MaxInclusive {
			if maxValue > *r.Max {
				return false
			}
		} else if maxValue >= *r.Max {
			return false
		}
	}
	return true
}
