package fleetmanagement

import (
	"math"
	"net/netip"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestParseFilter_NilFilter(t *testing.T) {
	filter, err := parseFilter(nil)

	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.Empty(t, filter.FirmwareVersions)
	assert.Empty(t, filter.Zones)
}

func TestParseFilter_FirmwareVersions(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		FirmwareVersions: []string{"v3.5.1", "v3.5.2"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"v3.5.1", "v3.5.2"}, filter.FirmwareVersions)
}

func TestParseFilter_Zones(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Zones: []string{"building-a", "building-b"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"building-a", "building-b"}, filter.Zones)
}

func TestParseFilter_FirmwareAndZonesEmpty(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		FirmwareVersions: []string{},
		Zones:            []string{},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Empty(t, filter.FirmwareVersions)
	assert.Empty(t, filter.Zones)
}

func TestParseFilter_NewFiltersCombineWithExisting(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Models:           []string{"S21 XP"},
		FirmwareVersions: []string{"v3.5.1"},
		Zones:            []string{"building-a"},
		RackIds:          []int64{42},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"S21 XP"}, filter.ModelNames)
	assert.Equal(t, []string{"v3.5.1"}, filter.FirmwareVersions)
	assert.Equal(t, []string{"building-a"}, filter.Zones)
	assert.Equal(t, []int64{42}, filter.RackIDs)
}

func TestParseFilter_FreeFormZoneWithSpecialChars(t *testing.T) {
	// Zone is free-form text. Server passes it through unchanged; URL/value
	// encoding is the client's responsibility.
	pbFilter := &pb.MinerListFilter{
		Zones: []string{"Austin, Building 1"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"Austin, Building 1"}, filter.Zones)
}

func TestParseFilter_FirmwareVersions_RejectsOversizedArray(t *testing.T) {
	// Cap protects Postgres planner from `= ANY(huge_array)` blowup.
	values := make([]string, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware_versions")
}

func TestParseFilter_Zones_RejectsOversizedArray(t *testing.T) {
	values := make([]string, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = "z"
	}
	pbFilter := &pb.MinerListFilter{Zones: values}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "zones")
}

func TestParseFilter_FirmwareVersions_AcceptsMaxSizedArray(t *testing.T) {
	// Boundary: exactly maxFreeFormFilterValues is allowed.
	values := make([]string, maxFreeFormFilterValues)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.FirmwareVersions, maxFreeFormFilterValues)
}

// TestParseFilter_OversizedArrays_ReturnInvalidArgument guards against the bug
// where an oversized free-form array would be wrapped by callers
// (buildSnapshot, GetMinerModelGroups, ExportMinerListCsv) with
// NewInternalErrorf, converting a 400-style client validation failure into a
// 500. parseFilter must return an InvalidArgument FleetError so callers can
// pass it through unchanged.
func TestParseFilter_OversizedArrays_ReturnInvalidArgument(t *testing.T) {
	cases := []struct {
		name   string
		filter *pb.MinerListFilter
	}{
		{
			name:   "firmware_versions",
			filter: &pb.MinerListFilter{FirmwareVersions: make([]string, maxFreeFormFilterValues+1)},
		},
		{
			name:   "zones",
			filter: &pb.MinerListFilter{Zones: make([]string, maxFreeFormFilterValues+1)},
		},
		{
			name: "numeric_ranges",
			filter: &pb.MinerListFilter{
				NumericRanges: oversizedNumericRanges(),
			},
		},
		{
			name: "ip_cidrs",
			filter: &pb.MinerListFilter{
				IpCidrs: make([]string, maxFreeFormFilterValues+1),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFilter(tc.filter)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err),
				"oversized %s must surface as InvalidArgument, not Internal — got %v",
				tc.name, err)
		})
	}
}

func TestParseFilter_NumericRange_HashrateGreaterThan(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		NumericRanges: []*pb.NumericRangeFilter{
			{
				Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS,
				Min:   wrapperspb.Double(90),
			},
		},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.NumericRanges, 1)
	r := filter.NumericRanges[0]
	assert.Equal(t, stores.NumericFilterFieldHashrateTHs, r.Field)
	require.NotNil(t, r.Min)
	assert.Equal(t, 90.0, *r.Min)
	assert.Nil(t, r.Max)
	assert.False(t, r.MinInclusive, "default operator is strict > to match issue copy")
}

func TestParseFilter_NumericRange_BetweenInclusive(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		NumericRanges: []*pb.NumericRangeFilter{
			{
				Field:        pb.NumericField_NUMERIC_FIELD_POWER_KW,
				Min:          wrapperspb.Double(1500),
				Max:          wrapperspb.Double(3000),
				MinInclusive: true,
				MaxInclusive: true,
			},
		},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.NumericRanges, 1)
	r := filter.NumericRanges[0]
	assert.Equal(t, stores.NumericFilterFieldPowerKW, r.Field)
	require.NotNil(t, r.Min)
	require.NotNil(t, r.Max)
	assert.Equal(t, 1500.0, *r.Min)
	assert.Equal(t, 3000.0, *r.Max)
	assert.True(t, r.MinInclusive)
	assert.True(t, r.MaxInclusive)
}

func TestParseFilter_NumericRange_AllSupportedFields(t *testing.T) {
	cases := []struct {
		proto pb.NumericField
		want  stores.NumericFilterField
	}{
		{pb.NumericField_NUMERIC_FIELD_HASHRATE_THS, stores.NumericFilterFieldHashrateTHs},
		{pb.NumericField_NUMERIC_FIELD_EFFICIENCY_JTH, stores.NumericFilterFieldEfficiencyJTH},
		{pb.NumericField_NUMERIC_FIELD_POWER_KW, stores.NumericFilterFieldPowerKW},
		{pb.NumericField_NUMERIC_FIELD_TEMPERATURE_C, stores.NumericFilterFieldTemperatureC},
		{pb.NumericField_NUMERIC_FIELD_VOLTAGE_V, stores.NumericFilterFieldVoltageV},
		{pb.NumericField_NUMERIC_FIELD_CURRENT_A, stores.NumericFilterFieldCurrentA},
	}
	for _, tc := range cases {
		t.Run(tc.proto.String(), func(t *testing.T) {
			pbFilter := &pb.MinerListFilter{
				NumericRanges: []*pb.NumericRangeFilter{
					{Field: tc.proto, Min: wrapperspb.Double(1)},
				},
			}
			filter, err := parseFilter(pbFilter)
			require.NoError(t, err)
			require.Len(t, filter.NumericRanges, 1)
			assert.Equal(t, tc.want, filter.NumericRanges[0].Field)
		})
	}
}

func TestParseFilter_NumericRange_RejectsUnspecifiedField(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		NumericRanges: []*pb.NumericRangeFilter{
			{Field: pb.NumericField_NUMERIC_FIELD_UNSPECIFIED, Min: wrapperspb.Double(1)},
		},
	}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "field")
}

func TestParseFilter_NumericRange_RejectsNoBounds(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		NumericRanges: []*pb.NumericRangeFilter{
			{Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS},
		},
	}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestParseFilter_NumericRange_RejectsNonFiniteBounds(t *testing.T) {
	cases := []struct {
		name string
		min  *wrapperspb.DoubleValue
		max  *wrapperspb.DoubleValue
	}{
		{"NaN min", wrapperspb.Double(math.NaN()), nil},
		{"+Inf min", wrapperspb.Double(math.Inf(1)), nil},
		{"-Inf min", wrapperspb.Double(math.Inf(-1)), nil},
		{"NaN max", nil, wrapperspb.Double(math.NaN())},
		{"+Inf max", nil, wrapperspb.Double(math.Inf(1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pbFilter := &pb.MinerListFilter{
				NumericRanges: []*pb.NumericRangeFilter{
					{Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS, Min: tc.min, Max: tc.max},
				},
			}
			_, err := parseFilter(pbFilter)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
}

func TestParseFilter_NumericRange_RejectsMinGreaterThanMax(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		NumericRanges: []*pb.NumericRangeFilter{
			{
				Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS,
				Min:   wrapperspb.Double(100),
				Max:   wrapperspb.Double(90),
			},
		},
	}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestParseFilter_NumericRange_AcceptsMaxSizedArray(t *testing.T) {
	values := make([]*pb.NumericRangeFilter, maxFreeFormFilterValues)
	for i := range values {
		values[i] = &pb.NumericRangeFilter{
			Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS,
			Min:   wrapperspb.Double(1),
		}
	}
	pbFilter := &pb.MinerListFilter{NumericRanges: values}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.NumericRanges, maxFreeFormFilterValues)
}

func TestParseFilter_IPCIDRs_HappyPathIPv4(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"192.168.1.0/24", "10.0.0.0/8"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 2)
	assert.Equal(t, netip.MustParsePrefix("192.168.1.0/24"), filter.IPCIDRs[0])
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/8"), filter.IPCIDRs[1])
}

func TestParseFilter_IPCIDRs_BareIPv4NormalizedTo32(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"10.0.0.5"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.5/32"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_BareIPv6NormalizedTo128(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"2001:db8::1"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("2001:db8::1/128"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_NonCanonicalNormalized(t *testing.T) {
	// 192.168.1.5/24 is not a valid network — host bits set. We accept and
	// normalize to 192.168.1.0/24 so `inet <<= cidr` semantics are obvious.
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"192.168.1.5/24"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("192.168.1.0/24"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_IPv6Network(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"2001:db8::/32"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("2001:db8::/32"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_RejectsMalformed(t *testing.T) {
	cases := []string{"", "foo", "192.168.1.0/33", "192.168.1.0/-1", "999.999.999.999/24"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			pbFilter := &pb.MinerListFilter{IpCidrs: []string{c}}
			_, err := parseFilter(pbFilter)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
}

func TestParseFilter_IPCIDRs_AcceptsMaxSizedArray(t *testing.T) {
	values := make([]string, maxFreeFormFilterValues)
	for i := range values {
		values[i] = "10.0.0.0/8"
	}
	pbFilter := &pb.MinerListFilter{IpCidrs: values}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.IPCIDRs, maxFreeFormFilterValues)
}

func TestParseFilter_NumericAndCIDR_CombineWithExistingFilters(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Models:           []string{"S21 XP"},
		FirmwareVersions: []string{"v3.5.1"},
		NumericRanges: []*pb.NumericRangeFilter{
			{Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS, Min: wrapperspb.Double(90)},
			{Field: pb.NumericField_NUMERIC_FIELD_POWER_KW, Max: wrapperspb.Double(3000), MaxInclusive: true},
		},
		IpCidrs: []string{"192.168.1.0/24"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"S21 XP"}, filter.ModelNames)
	assert.Equal(t, []string{"v3.5.1"}, filter.FirmwareVersions)
	assert.Len(t, filter.NumericRanges, 2)
	assert.Len(t, filter.IPCIDRs, 1)
}

func oversizedNumericRanges() []*pb.NumericRangeFilter {
	out := make([]*pb.NumericRangeFilter, maxFreeFormFilterValues+1)
	for i := range out {
		out[i] = &pb.NumericRangeFilter{
			Field: pb.NumericField_NUMERIC_FIELD_HASHRATE_THS,
			Min:   wrapperspb.Double(1),
		}
	}
	return out
}
