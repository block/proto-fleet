package fleetmanagement

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// testOrgID is the org used for every parseFilter call in these tests.
// The cross-org bulk check fires when an explicit building_id > 0 is
// present; callers wire the right BuildingStore stub for the case they
// exercise (see stubBuildingStore below).
const testOrgID int64 = 1

// stubBuildingStore is the minimal BuildingStore used by tests that
// exercise the cross-org check. It embeds the interface so unused
// methods fall through to a nil dispatch — those methods must not be
// called by parseFilter.
type stubBuildingStore struct {
	stores.BuildingStore
	owned map[int64]struct{}
	err   error
}

// newOwnedStore returns a stub that recognizes the given IDs as belonging
// to the org. IDs not in the set are treated as cross-org and rejected.
func newOwnedStore(ids ...int64) *stubBuildingStore {
	s := &stubBuildingStore{owned: map[int64]struct{}{}}
	for _, id := range ids {
		s.owned[id] = struct{}{}
	}
	return s
}

func (s *stubBuildingStore) BuildingsByIDs(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := s.owned[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// callParseFilter is the test entry point. Most tests don't exercise
// building_ids/zone_keys so they don't need a real store; pass nil and
// parseFilter short-circuits before calling BuildingsByIDs.
func callParseFilter(t *testing.T, pbFilter *pb.MinerListFilter) (*stores.MinerFilter, error) {
	t.Helper()
	return parseFilter(context.Background(), testOrgID, pbFilter, nil)
}

// callParseFilterWithStore is for tests that exercise cross-org
// validation on building_ids / zone_keys.
func callParseFilterWithStore(t *testing.T, pbFilter *pb.MinerListFilter, store stores.BuildingStore) (*stores.MinerFilter, error) {
	t.Helper()
	return parseFilter(context.Background(), testOrgID, pbFilter, store)
}

func TestParseFilter_NilFilter(t *testing.T) {
	filter, err := callParseFilter(t, nil)

	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.Empty(t, filter.FirmwareVersions)
	assert.Empty(t, filter.ZoneKeys)
	assert.Empty(t, filter.BuildingIDs)
}

func TestParseFilter_FirmwareVersions(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		FirmwareVersions: []string{"v3.5.1", "v3.5.2"},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"v3.5.1", "v3.5.2"}, filter.FirmwareVersions)
}

func TestParseFilter_ZoneKeys_AllWildcard(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 0, Zone: "building-a"},
			{BuildingId: 0, Zone: "building-b"},
		},
	}

	// Wildcard entries don't trigger the cross-org check; nil store is fine.
	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []stores.ZoneKey{
		{BuildingID: 0, Zone: "building-a"},
		{BuildingID: 0, Zone: "building-b"},
	}, filter.ZoneKeys)
}

func TestParseFilter_ZoneKeys_AllScoped(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: "Room 2"},
			{BuildingId: 9, Zone: "Room 2"},
		},
	}
	store := newOwnedStore(7, 9)

	filter, err := callParseFilterWithStore(t, pbFilter, store)

	require.NoError(t, err)
	assert.Equal(t, []stores.ZoneKey{
		{BuildingID: 7, Zone: "Room 2"},
		{BuildingID: 9, Zone: "Room 2"},
	}, filter.ZoneKeys)
}

func TestParseFilter_ZoneKeys_MixedScopedAndWildcard(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: "Room 2"},
			{BuildingId: 0, Zone: "Other Zone"},
		},
	}
	store := newOwnedStore(7)

	filter, err := callParseFilterWithStore(t, pbFilter, store)

	require.NoError(t, err)
	assert.Len(t, filter.ZoneKeys, 2)
	assert.Equal(t, int64(7), filter.ZoneKeys[0].BuildingID)
	assert.Equal(t, int64(0), filter.ZoneKeys[1].BuildingID)
}

func TestParseFilter_ZoneKeys_RejectsNegativeBuildingID(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: -1, Zone: "Room 2"},
		},
	}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone_keys")
}

func TestParseFilter_ZoneKeys_RejectsEmptyZone(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: ""},
		},
	}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone")
}

func TestParseFilter_ZoneKeys_RejectsCrossOrgScoped(t *testing.T) {
	// Building 99 not in caller's org; building 7 is. The bulk check
	// rejects when len(found) < len(requested).
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: "Room 2"},
			{BuildingId: 99, Zone: "Cold Aisle"},
		},
	}
	store := newOwnedStore(7)

	_, err := callParseFilterWithStore(t, pbFilter, store)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "building_ids")
	// Error message must not echo the rejected ID — finding 10.
	assert.NotContains(t, err.Error(), "99")
}

func TestParseFilter_ZoneKeys_WildcardSkipsCrossOrgCheck(t *testing.T) {
	// All-wildcard request: store is never consulted. Pass nil to prove
	// parseFilter doesn't call into it.
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 0, Zone: "Room 2"},
		},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.ZoneKeys, 1)
	assert.Equal(t, int64(0), filter.ZoneKeys[0].BuildingID)
}

func TestParseFilter_ZoneKeys_RejectsOversizedArray(t *testing.T) {
	keys := make([]*commonpb.ZoneKey, maxFreeFormFilterValues+1)
	for i := range keys {
		keys[i] = &commonpb.ZoneKey{BuildingId: 0, Zone: "z"}
	}
	pbFilter := &pb.MinerListFilter{ZoneKeys: keys}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone_keys")
}

func TestParseFilter_BuildingIDs_HappyPath(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		BuildingIds: []int64{7, 9},
	}
	store := newOwnedStore(7, 9)

	filter, err := callParseFilterWithStore(t, pbFilter, store)

	require.NoError(t, err)
	assert.Equal(t, []int64{7, 9}, filter.BuildingIDs)
}

func TestParseFilter_BuildingIDs_RejectsZeroAndNegative(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
	}{
		{"zero", []int64{1, 0, 3}},
		{"negative", []int64{-5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callParseFilter(t, &pb.MinerListFilter{BuildingIds: tc.ids})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "building_ids")
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
}

func TestParseFilter_BuildingIDs_RejectsCrossOrg(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		BuildingIds: []int64{7, 99},
	}
	store := newOwnedStore(7) // 99 missing

	_, err := callParseFilterWithStore(t, pbFilter, store)

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.NotContains(t, err.Error(), "99")
}

func TestParseFilter_BuildingIDs_StoreError(t *testing.T) {
	pbFilter := &pb.MinerListFilter{BuildingIds: []int64{7}}
	store := &stubBuildingStore{err: errors.New("db down")}

	_, err := callParseFilterWithStore(t, pbFilter, store)

	require.Error(t, err)
	// Internal errors (not InvalidArgument) when the bulk check itself fails.
	assert.False(t, fleeterror.IsInvalidArgumentError(err))
}

func TestParseFilter_BuildingIDs_NilStoreWithExplicitIDs(t *testing.T) {
	// Defensive: nil store with explicit IDs is a server misconfiguration,
	// must surface as Internal so it's caught in production logs.
	pbFilter := &pb.MinerListFilter{BuildingIds: []int64{7}}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.False(t, fleeterror.IsInvalidArgumentError(err))
}

func TestParseFilter_BuildingIDs_RejectsOversizedArray(t *testing.T) {
	values := make([]int64, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = int64(i + 1)
	}
	pbFilter := &pb.MinerListFilter{BuildingIds: values}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "building_ids")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestParseFilter_IncludeNoRackPlusZoneKeysRejected covers the
// combinatorial bug surfaced in PR #249 review: the zone_keys EXISTS
// predicate requires a rack-membership row, so include_no_rack silently
// drops every unracked device the caller asked to include. parseFilter
// rejects the combination explicitly instead.
func TestParseFilter_IncludeNoRackPlusZoneKeysRejected(t *testing.T) {
	cases := []struct {
		name   string
		filter *pb.MinerListFilter
	}{
		{
			name: "with zone_keys",
			filter: &pb.MinerListFilter{
				IncludeNoRack: true,
				ZoneKeys:      []*commonpb.ZoneKey{{BuildingId: 0, Zone: "Room 2"}},
			},
		},
		{
			name: "with legacy zones shim",
			filter: &pb.MinerListFilter{
				IncludeNoRack: true,
				Zones:         []string{"Room 2"}, //nolint:staticcheck // SA1019 — testing the deprecated path
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callParseFilter(t, tc.filter)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), "include_no_rack")
		})
	}
}

// TestParseFilter_LegacyZones_RejectsEmpty covers the empty-zone bug
// surfaced in PR #249 review: empty strings used to silently drop and
// effectively un-filter the result. parseFilter now rejects them for
// parity with the zone_keys.zone non-empty rule.
func TestParseFilter_LegacyZones_RejectsEmpty(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Zones: []string{""}, //nolint:staticcheck // SA1019 — testing the deprecated path
	}

	_, err := callParseFilter(t, pbFilter)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zones[0]")
}

func TestParseFilter_IncludeNoBuildingAndIncludeNoRack(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IncludeNoBuilding: true,
		IncludeNoRack:     true,
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.True(t, filter.IncludeNoBuilding)
	assert.True(t, filter.IncludeNoRack)
}

func TestParseFilter_BuildingAndZoneCombined(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		BuildingIds:       []int64{7},
		IncludeNoBuilding: true,
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: "Room 2"},
			{BuildingId: 0, Zone: "Wildcard Zone"},
		},
	}
	store := newOwnedStore(7)

	filter, err := callParseFilterWithStore(t, pbFilter, store)

	require.NoError(t, err)
	assert.Equal(t, []int64{7}, filter.BuildingIDs)
	assert.True(t, filter.IncludeNoBuilding)
	assert.Len(t, filter.ZoneKeys, 2)
}

func TestParseFilter_NewFiltersCombineWithExisting(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Models:           []string{"S21 XP"},
		FirmwareVersions: []string{"v3.5.1"},
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 0, Zone: "building-a"},
		},
		RackIds: []int64{42},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"S21 XP"}, filter.ModelNames)
	assert.Equal(t, []string{"v3.5.1"}, filter.FirmwareVersions)
	assert.Len(t, filter.ZoneKeys, 1)
	assert.Equal(t, []int64{42}, filter.RackIDs)
}

// TestParseFilter_SiteIDs covers the multi-site filter split: site_ids
// is a repeated list (OR logic) and include_unassigned is an independent
// bool. Plan §"device/" filter notes — the four allowed combos are
// (none), site_ids only, include_unassigned only, both.
func TestParseFilter_SiteIDs(t *testing.T) {
	t.Run("specific sites", func(t *testing.T) {
		filter, err := callParseFilter(t, &pb.MinerListFilter{SiteIds: []int64{1, 2}})
		require.NoError(t, err)
		assert.Equal(t, []int64{1, 2}, filter.SiteIDs)
		assert.False(t, filter.IncludeUnassigned)
	})
	t.Run("unassigned only", func(t *testing.T) {
		filter, err := callParseFilter(t, &pb.MinerListFilter{IncludeUnassigned: true})
		require.NoError(t, err)
		assert.Nil(t, filter.SiteIDs)
		assert.True(t, filter.IncludeUnassigned)
	})
	t.Run("specific sites plus unassigned", func(t *testing.T) {
		filter, err := callParseFilter(t, &pb.MinerListFilter{
			SiteIds:           []int64{1, 2},
			IncludeUnassigned: true,
		})
		require.NoError(t, err)
		assert.Equal(t, []int64{1, 2}, filter.SiteIDs)
		assert.True(t, filter.IncludeUnassigned)
	})
	t.Run("no site filter", func(t *testing.T) {
		filter, err := callParseFilter(t, &pb.MinerListFilter{})
		require.NoError(t, err)
		assert.Nil(t, filter.SiteIDs)
		assert.False(t, filter.IncludeUnassigned)
	})
}

func TestParseFilter_FreeFormZoneWithSpecialChars(t *testing.T) {
	// Zone is free-form text. Server passes it through unchanged; URL/value
	// encoding is the client's responsibility.
	pbFilter := &pb.MinerListFilter{
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 0, Zone: "Austin, Building 1"},
		},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.ZoneKeys, 1)
	assert.Equal(t, "Austin, Building 1", filter.ZoneKeys[0].Zone)
}

func TestParseFilter_FirmwareVersions_RejectsOversizedArray(t *testing.T) {
	// Cap protects Postgres planner from `= ANY(huge_array)` blowup.
	values := make([]string, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware_versions")
}

func TestParseFilter_SiteIDs_RejectsOversizedArray(t *testing.T) {
	values := make([]int64, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = int64(i + 1)
	}
	pbFilter := &pb.MinerListFilter{SiteIds: values}

	_, err := callParseFilter(t, pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "site_ids")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestParseFilter_SiteIDs_RejectsZeroAndNegative(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
	}{
		{name: "zero", ids: []int64{1, 0, 3}},
		{name: "negative", ids: []int64{-5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callParseFilter(t, &pb.MinerListFilter{SiteIds: tc.ids})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "site_ids")
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
}

func TestParseFilter_FirmwareVersions_AcceptsMaxSizedArray(t *testing.T) {
	// Boundary: exactly maxFreeFormFilterValues is allowed.
	values := make([]string, maxFreeFormFilterValues)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	filter, err := callParseFilter(t, pbFilter)

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
	oversizedZoneKeys := make([]*commonpb.ZoneKey, maxFreeFormFilterValues+1)
	for i := range oversizedZoneKeys {
		oversizedZoneKeys[i] = &commonpb.ZoneKey{BuildingId: 0, Zone: "z"}
	}
	cases := []struct {
		name   string
		filter *pb.MinerListFilter
	}{
		{
			name:   "firmware_versions",
			filter: &pb.MinerListFilter{FirmwareVersions: make([]string, maxFreeFormFilterValues+1)},
		},
		{
			name:   "zone_keys",
			filter: &pb.MinerListFilter{ZoneKeys: oversizedZoneKeys},
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
		{
			name:   "building_ids",
			filter: &pb.MinerListFilter{BuildingIds: oversizedInt64s()},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callParseFilter(t, tc.filter)
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

	filter, err := callParseFilter(t, pbFilter)

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

	filter, err := callParseFilter(t, pbFilter)

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
			filter, err := callParseFilter(t, pbFilter)
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

	_, err := callParseFilter(t, pbFilter)

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

	_, err := callParseFilter(t, pbFilter)

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
			_, err := callParseFilter(t, pbFilter)
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

	_, err := callParseFilter(t, pbFilter)

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

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.NumericRanges, maxFreeFormFilterValues)
}

func TestParseFilter_IPCIDRs_HappyPathIPv4(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"192.168.1.0/24", "10.0.0.0/8"},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 2)
	assert.Equal(t, netip.MustParsePrefix("192.168.1.0/24"), filter.IPCIDRs[0])
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/8"), filter.IPCIDRs[1])
}

func TestParseFilter_IPCIDRs_BareIPv4NormalizedTo32(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"10.0.0.5"},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.5/32"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_BareIPv6NormalizedTo128(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"2001:db8::1"},
	}

	filter, err := callParseFilter(t, pbFilter)

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

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("192.168.1.0/24"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_IPv6Network(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		IpCidrs: []string{"2001:db8::/32"},
	}

	filter, err := callParseFilter(t, pbFilter)

	require.NoError(t, err)
	require.Len(t, filter.IPCIDRs, 1)
	assert.Equal(t, netip.MustParsePrefix("2001:db8::/32"), filter.IPCIDRs[0])
}

func TestParseFilter_IPCIDRs_RejectsMalformed(t *testing.T) {
	cases := []string{"", "foo", "192.168.1.0/33", "192.168.1.0/-1", "999.999.999.999/24"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			pbFilter := &pb.MinerListFilter{IpCidrs: []string{c}}
			_, err := callParseFilter(t, pbFilter)
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

	filter, err := callParseFilter(t, pbFilter)

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

	filter, err := callParseFilter(t, pbFilter)

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

func oversizedInt64s() []int64 {
	out := make([]int64, maxFreeFormFilterValues+1)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}
