package fleetlistfilter

import (
	"math"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestParseTelemetryAndIssueFilters(t *testing.T) {
	filter, err := Parse(
		[]errorspb.ComponentType{errorspb.ComponentType_COMPONENT_TYPE_FAN},
		[]*commonpb.FleetListTelemetryRangeFilter{{
			Field:        commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_POWER_KW,
			Min:          wrapperspb.Double(10),
			Max:          wrapperspb.Double(20),
			MinInclusive: true,
		}},
	)

	require.NoError(t, err)
	assert.Equal(t, []int32{int32(errorspb.ComponentType_COMPONENT_TYPE_FAN)}, filter.ErrorComponentTypes)
	require.Len(t, filter.TelemetryRanges, 1)
	assert.Equal(t, interfaces.NumericFilterFieldPowerKW, filter.TelemetryRanges[0].Field)
	assert.Equal(t, 10.0, *filter.TelemetryRanges[0].Min)
	assert.Equal(t, 20.0, *filter.TelemetryRanges[0].Max)
	assert.True(t, filter.TelemetryRanges[0].MinInclusive)
	assert.False(t, filter.TelemetryRanges[0].MaxInclusive)
}

func TestParseRejectsInvalidTelemetryRanges(t *testing.T) {
	tests := []struct {
		name   string
		ranges []*commonpb.FleetListTelemetryRangeFilter
	}{
		{
			name:   "nil range",
			ranges: []*commonpb.FleetListTelemetryRangeFilter{nil},
		},
		{
			name: "unspecified field",
			ranges: []*commonpb.FleetListTelemetryRangeFilter{{
				Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_UNSPECIFIED,
				Min:   wrapperspb.Double(1),
			}},
		},
		{
			name: "missing bounds",
			ranges: []*commonpb.FleetListTelemetryRangeFilter{{
				Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS,
			}},
		},
		{
			name: "non-finite min",
			ranges: []*commonpb.FleetListTelemetryRangeFilter{{
				Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS,
				Min:   wrapperspb.Double(math.Inf(1)),
			}},
		},
		{
			name: "min greater than max",
			ranges: []*commonpb.FleetListTelemetryRangeFilter{{
				Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_HASHRATE_THS,
				Min:   wrapperspb.Double(2),
				Max:   wrapperspb.Double(1),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(nil, tt.ranges)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "got %T: %v", err, err)
		})
	}
}

func TestParseRejectsUnsupportedIssueComponentTypes(t *testing.T) {
	tests := []struct {
		name          string
		componentType errorspb.ComponentType
	}{
		{
			name:          "unspecified",
			componentType: errorspb.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
		},
		{
			name:          "unsupported concrete component",
			componentType: errorspb.ComponentType_COMPONENT_TYPE_EEPROM,
		},
		{
			name:          "unknown enum value",
			componentType: errorspb.ComponentType(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]errorspb.ComponentType{tt.componentType}, nil)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "got %T: %v", err, err)
		})
	}
}

func TestMatchesUsesIssueOrTelemetryAnd(t *testing.T) {
	minPower := 10.0
	filter := Filter{
		ErrorComponentTypes: []int32{int32(errorspb.ComponentType_COMPONENT_TYPE_FAN), int32(errorspb.ComponentType_COMPONENT_TYPE_PSU)},
		TelemetryRanges: []interfaces.NumericRange{{
			Field:        interfaces.NumericFilterFieldPowerKW,
			Min:          &minPower,
			MinInclusive: true,
		}},
	}

	assert.True(t, Matches(Stats{
		PowerReportingCount: 1,
		TotalPowerKw:        10,
		PsuIssueCount:       1,
	}, filter))
	assert.False(t, Matches(Stats{
		PowerReportingCount: 1,
		TotalPowerKw:        10,
		HashBoardIssueCount: 1,
	}, filter))
	assert.False(t, Matches(Stats{
		PowerReportingCount: 0,
		TotalPowerKw:        20,
		PsuIssueCount:       1,
	}, filter))
}

func TestMatchesTemperatureComparesMinAndMaxAggregates(t *testing.T) {
	minTemp := 40.0
	maxTemp := 80.0
	filter := Filter{TelemetryRanges: []interfaces.NumericRange{{
		Field:        interfaces.NumericFilterFieldTemperatureC,
		Min:          &minTemp,
		Max:          &maxTemp,
		MinInclusive: true,
		MaxInclusive: true,
	}}}

	assert.True(t, Matches(Stats{
		TemperatureReportingCount: 1,
		MinTemperatureC:           45,
		MaxTemperatureC:           75,
	}, filter))
	assert.False(t, Matches(Stats{
		TemperatureReportingCount: 1,
		MinTemperatureC:           35,
		MaxTemperatureC:           75,
	}, filter), "min filter must compare against aggregate minimum temperature")
	assert.False(t, Matches(Stats{
		TemperatureReportingCount: 1,
		MinTemperatureC:           45,
		MaxTemperatureC:           85,
	}, filter), "max filter must compare against aggregate maximum temperature")
}
