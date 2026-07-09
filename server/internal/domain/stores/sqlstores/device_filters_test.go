package sqlstores

import (
	"database/sql"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMinerFilterParams_StatusFilter(t *testing.T) {
	filter := &stores.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{
			minermodels.MinerStatusActive,
			minermodels.MinerStatusOffline,
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.Len(t, params.statusValues, 2)
	assert.Contains(t, params.statusValues, "ACTIVE")
	assert.Contains(t, params.statusValues, "OFFLINE")
	assert.False(t, params.needsAttentionFilter)
	assert.True(t, params.includeNullStatus, "OFFLINE filter should include NULL status miners")
}

func TestBuildMinerFilterParams_StatusFilterWithError(t *testing.T) {
	// Tests special behavior: ERROR status triggers needsAttentionFilter
	filter := &stores.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{
			minermodels.MinerStatusError,
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.True(t, params.needsAttentionFilter)
	assert.False(t, params.includeNullStatus, "ERROR filter should not include NULL status")
}

func TestBuildMinerFilterParams_StatusFilterActiveOnly(t *testing.T) {
	filter := &stores.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{
			minermodels.MinerStatusActive,
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.False(t, params.includeNullStatus, "ACTIVE filter should not include NULL status")
	assert.False(t, params.needsAttentionFilter)
}

func TestBuildMinerFilterParams_PairingStatusUnspecifiedOnly(t *testing.T) {
	// Tests edge case: UNSPECIFIED should NOT set the filter (means "return all")
	filter := &stores.MinerFilter{
		PairingStatuses: []fm.PairingStatus{
			fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED,
		},
	}

	params := buildMinerFilterParams(filter)

	assert.False(t, params.pairingStatusFilter.Valid)
	assert.Empty(t, params.pairingStatusValues)
}

func TestBuildMinerFilterParams_CombinedFilters(t *testing.T) {
	filter := &stores.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusActive},
		ModelNames:         []string{"S21 XP"},
		ManufacturerNames:  []string{"Bitmain"},
		PairingStatuses:    []fm.PairingStatus{fm.PairingStatus_PAIRING_STATUS_PAIRED},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.True(t, params.modelFilter.Valid)
	assert.True(t, params.manufacturerFilter.Valid)
	assert.True(t, params.pairingStatusFilter.Valid)
}

func TestAppendFilterSQL_PairingStatusFilter(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		pairingStatusFilter: validNullString(),
		pairingStatusValues: []string{"PAIRED"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)

	assert.Contains(t, sb.String(), "pairing_status")
	assert.Contains(t, sb.String(), "$2")
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

func TestAppendFilterSQL_DeviceIdentifiersFilter(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		deviceIdentifiersFilter: validNullString(),
		deviceIdentifierValues:  []string{"miner-1", "miner-2"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	assert.Contains(t, sb.String(), "device.device_identifier = ANY($2::text[])")
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

func TestAppendFilterSQL_StatusFilter(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		statusFilter: validNullString(),
		statusValues: []string{"ACTIVE"},
	}
	orgID := int64(1)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	assert.Contains(t, sb.String(), "device_status.status::text")
	assert.Len(t, resultArgs, 3) // initial + statusValues + orgID
	assert.Equal(t, 4, resultArgNum)
}

func TestAppendFilterSQL_StatusFilterWithNeedsAttention(t *testing.T) {
	// Tests special OR logic for needs attention (AUTHENTICATION_NEEDED + errors)
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		statusFilter:         validNullString(),
		statusValues:         []string{"ERROR"},
		needsAttentionFilter: true,
	}
	orgID := int64(1)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "AUTHENTICATION_NEEDED")
	assert.Contains(t, sql, "errors")
	assert.Contains(t, sql, "device_status.status IS NULL OR device_status.status != 'OFFLINE'")
	assert.Contains(t, sql, "device_status.status IS NULL OR device_status.status NOT IN")
	// Errors branch excludes NULL paired-like miners (they remain bucketed as offline).
	assert.Contains(t, sql, "NOT (device_status.status IS NULL AND device_pairing.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD'))")
	assert.Len(t, resultArgs, 4) // initial + statusValues + orgID + orgID
	assert.Equal(t, 5, resultArgNum)
}

func TestAppendFilterSQL_StatusFilterWithOfflineIncludesNull(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		statusFilter:      validNullString(),
		statusValues:      []string{"OFFLINE"},
		includeNullStatus: true,
	}
	orgID := int64(1)

	appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device_status.status IS NULL")
	// Narrowed to paired-like statuses (matches CountMinersByState scope); excludes PENDING/FAILED/UNPAIRED.
	assert.Contains(t, sql, "device_pairing.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')")
	assert.NotContains(t, sql, "pairing_status != 'AUTHENTICATION_NEEDED'")
}

func TestAppendFilterSQL_StatusFilterActiveDoesNotIncludeNull(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		statusFilter: validNullString(),
		statusValues: []string{"ACTIVE"},
	}
	orgID := int64(1)

	appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.NotContains(t, sql, "device_status.status IS NULL")
}

func TestAppendFilterSQL_CombinedFilters(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		pairingStatusFilter: validNullString(),
		pairingStatusValues: []string{"PAIRED"},
		modelFilter:         validNullString(),
		modelValues:         []string{"S21 XP"},
		manufacturerFilter:  validNullString(),
		manufacturerValues:  []string{"Bitmain"},
		statusFilter:        validNullString(),
		statusValues:        []string{"ACTIVE"},
	}
	orgID := int64(1)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	assert.Contains(t, sb.String(), "pairing_status")
	assert.Contains(t, sb.String(), "discovered_device.model")
	assert.Contains(t, sb.String(), "discovered_device.manufacturer")
	assert.Contains(t, sb.String(), "device_status.status")
	assert.Len(t, resultArgs, 6) // initial + pairing + model + manufacturer + status + orgID
	assert.Equal(t, 7, resultArgNum)
}

func TestAppendFilterSQL_ArgNumbersIncrement(t *testing.T) {
	// Tests that argument numbering correctly increments across multiple filters
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 5 // Start from a higher number
	fp := minerFilterParams{
		pairingStatusFilter: validNullString(),
		pairingStatusValues: []string{"PAIRED"},
		modelFilter:         validNullString(),
		modelValues:         []string{"S21 XP"},
	}

	_, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)

	assert.Contains(t, sb.String(), "$5") // First filter uses starting argNum
	assert.Contains(t, sb.String(), "$6") // Second filter increments
	assert.Equal(t, 7, resultArgNum)
}

func TestAppendFilterSQL_NoRawSliceArgs(t *testing.T) {
	// Verifies no raw Go slices are passed as query args.
	// database/sql cannot convert []string or []int32 to PostgreSQL arrays —
	// they must be wrapped with pq.Array() (which implements driver.Valuer).
	// Raw slices cause: "sql: converting argument $N type: unsupported type []string"
	var sb strings.Builder
	args := []any{"initial_org_id"}
	fp := minerFilterParams{
		pairingStatusFilter:       validNullString(),
		pairingStatusValues:       []string{"PAIRED"},
		modelFilter:               validNullString(),
		modelValues:               []string{"S21 XP"},
		statusFilter:              validNullString(),
		statusValues:              []string{"ACTIVE"},
		errorComponentTypesFilter: validNullString(),
		errorComponentTypeValues:  []int32{1, 2},
	}

	resultArgs, _ := appendFilterSQL(&sb, args, 2, 1, fp)

	for i, arg := range resultArgs {
		kind := reflect.TypeOf(arg).Kind()
		assert.NotEqual(t, reflect.Slice, kind,
			fmt.Sprintf("arg at position %d is a raw slice (%T); must be wrapped with pq.Array()", i, arg))
	}
}

func TestBuildMinerFilterParams_GroupIDs(t *testing.T) {
	filter := &stores.MinerFilter{
		GroupIDs: []int64{10, 20, 30},
	}

	// Act
	params := buildMinerFilterParams(filter)

	// Assert
	assert.True(t, params.groupIDsFilter.Valid)
	assert.Equal(t, []int64{10, 20, 30}, params.groupIDValues)
}

func TestBuildMinerFilterParams_RackIDs(t *testing.T) {
	filter := &stores.MinerFilter{
		RackIDs: []int64{5},
	}

	// Act
	params := buildMinerFilterParams(filter)

	// Assert
	assert.True(t, params.rackIDsFilter.Valid)
	assert.Equal(t, []int64{5}, params.rackIDValues)
}

func TestAppendFilterSQL_GroupIDsOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		groupIDsFilter: validNullString(),
		groupIDValues:  []int64{10, 20},
	}
	orgID := int64(42)

	// Act
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	// Assert
	sql := sb.String()
	assert.Contains(t, sql, "device_set_membership")
	assert.Contains(t, sql, "device_set_type = 'group'")
	assert.Contains(t, sql, "org_id = $2")
	assert.Contains(t, sql, "device_set_id = ANY($3::bigint[])")
	assert.Len(t, resultArgs, 3) // initial + orgID + groupIDs
	assert.Equal(t, 4, resultArgNum)
}

func TestAppendFilterSQL_RackIDsOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		rackIDsFilter: validNullString(),
		rackIDValues:  []int64{5},
	}
	orgID := int64(42)

	// Act
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	// Assert
	sql := sb.String()
	assert.Contains(t, sql, "device_set_type = 'rack'")
	assert.Contains(t, sql, "org_id = $2")
	assert.Contains(t, sql, "device_set_id = ANY($3::bigint[])")
	assert.Len(t, resultArgs, 3) // initial + orgID + rackIDs
	assert.Equal(t, 4, resultArgNum)
}

func TestAppendFilterSQL_RackIDsAndIncludeNoRack_ORTogether(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		rackIDsFilter: validNullString(),
		rackIDValues:  []int64{5},
		includeNoRack: true,
	}
	orgID := int64(42)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device_set_id = ANY($3::bigint[])")
	assert.Contains(t, sql, " OR ")
	assert.Contains(t, sql, "NOT EXISTS")
	assert.Contains(t, sql, "dcm.org_id = $4")
	assert.Len(t, resultArgs, 4) // initial + rack orgID + rackIDs + no-rack orgID
	assert.Equal(t, 5, resultArgNum)
}

func TestAppendFilterSQL_RackIDsAndIncludeNoRack_DoesNotContradictBuildingIDs(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		rackIDsFilter:     validNullString(),
		rackIDValues:      []int64{5},
		buildingIDsFilter: validNullString(),
		buildingIDValues:  []int64{7},
		includeNoRack:     true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device_set_id = ANY")
	assert.Contains(t, sql, "device.building_id = ANY")
	assert.Equal(t, 1, strings.Count(sql, "NOT EXISTS"))
}

func TestAppendFilterSQL_RackAndBuildingUnassigned_IncludeNoRackWideningPreserved(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		rackIDsFilter:     validNullString(),
		rackIDValues:      []int64{5},
		includeNoBuilding: true,
		includeNoRack:     true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device_set_id = ANY")
	assert.Contains(t, sql, "dsr.building_id IS NULL")
	assert.Equal(t, 2, strings.Count(sql, "NOT EXISTS"))
	// #702: with a building predicate present (include_no_building), the
	// no-rack branch is intersected with "no direct building" so a rackless
	// miner directly placed in another building can't leak through.
	assert.Contains(t, sql, "(device.building_id IS NULL AND NOT EXISTS")
}

// The assignable-only miner-selection filter (issue #702) sends the target
// rack (rack_ids + include_no_rack) AND its building (building_ids +
// include_no_building). The no-rack branch must be gated on
// device.building_id IS NULL so a rackless miner with a direct building_id in
// a different building stays out of the default assignable list.
func TestAppendFilterSQL_AssignableOnlyShape_NoRackBranchGatedOnNoBuilding(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		rackIDsFilter:     validNullString(),
		rackIDValues:      []int64{5},
		buildingIDsFilter: validNullString(),
		buildingIDValues:  []int64{7},
		includeNoBuilding: true,
		includeNoRack:     true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device.building_id = ANY")                   // branch A: direct/rack-derived building
	assert.Contains(t, sql, "dsr.building_id IS NULL")                    // branch B: rack with no building
	assert.Contains(t, sql, "(device.building_id IS NULL AND NOT EXISTS") // branch C: gated no-rack
}

// The fleet "Unassigned" rack bucket sends include_no_rack with no building
// predicate at all. Here the no-rack branch is the SOLE building-group
// predicate and must stay unqualified — otherwise a rackless miner directly
// placed in a building would vanish from the Unassigned view.
func TestAppendFilterSQL_UnassignedRackBucket_NoRackBranchNotGated(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		includeNoRack: true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "NOT EXISTS")
	assert.NotContains(t, sql, "device.building_id IS NULL")
}

// TestBuildMinerFilterParams_SiteFilter exercises the four allowed combos
// of site_ids + include_unassigned (plan §"device/" filter notes).
func TestBuildMinerFilterParams_SiteFilter(t *testing.T) {
	t.Run("specific sites only", func(t *testing.T) {
		fp := buildMinerFilterParams(&stores.MinerFilter{SiteIDs: []int64{1, 2}})
		assert.True(t, fp.siteIDsFilter.Valid)
		assert.Equal(t, []int64{1, 2}, fp.siteIDValues)
		assert.False(t, fp.includeUnassigned)
	})
	t.Run("unassigned only", func(t *testing.T) {
		fp := buildMinerFilterParams(&stores.MinerFilter{IncludeUnassigned: true})
		assert.False(t, fp.siteIDsFilter.Valid)
		assert.True(t, fp.includeUnassigned)
	})
	t.Run("sites plus unassigned", func(t *testing.T) {
		fp := buildMinerFilterParams(&stores.MinerFilter{SiteIDs: []int64{3}, IncludeUnassigned: true})
		assert.True(t, fp.siteIDsFilter.Valid)
		assert.True(t, fp.includeUnassigned)
	})
}

// TestAppendFilterSQL_SiteIDsOnly emits a single AND predicate matching
// device.site_id against the supplied list and binds the array argument
// as a bigint[].
func TestAppendFilterSQL_SiteIDsOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		siteIDsFilter: validNullString(),
		siteIDValues:  []int64{7, 11},
	}
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)
	sql := sb.String()
	assert.Contains(t, sql, "device.site_id = ANY($2::bigint[])")
	assert.NotContains(t, sql, "IS NULL")
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

// TestAppendFilterSQL_IncludeUnassignedOnly emits a single AND predicate
// matching device.site_id IS NULL with no array argument bound.
func TestAppendFilterSQL_IncludeUnassignedOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{includeUnassigned: true}
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)
	sql := sb.String()
	assert.Contains(t, sql, "device.site_id IS NULL")
	assert.NotContains(t, sql, "= ANY(")
	assert.Len(t, resultArgs, 1)
	assert.Equal(t, 2, resultArgNum)
}

// TestAppendFilterSQL_SiteIDsAndUnassigned joins the two with OR inside
// the outer AND so callers get the "specific sites plus Unassigned" set.
func TestAppendFilterSQL_SiteIDsAndUnassigned(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		siteIDsFilter:     validNullString(),
		siteIDValues:      []int64{3},
		includeUnassigned: true,
	}
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)
	sql := sb.String()
	assert.Contains(t, sql, "device.site_id = ANY($2::bigint[])")
	assert.Contains(t, sql, "device.site_id IS NULL")
	assert.Contains(t, sql, " OR ")
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

// TestAppendFilterSQL_NoSiteFilter emits no site predicate when both
// the list and the flag are empty.
func TestAppendFilterSQL_NoSiteFilter(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{}
	_, _ = appendFilterSQL(&sb, args, argNum, 1, fp)
	sql := sb.String()
	assert.NotContains(t, sql, "device.site_id")
}

func TestAppendFilterSQL_GroupAndRackIDs_ProducesAND(t *testing.T) {
	// Both group and rack filters should produce separate AND clauses (not OR)
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		groupIDsFilter: validNullString(),
		groupIDValues:  []int64{10},
		rackIDsFilter:  validNullString(),
		rackIDValues:   []int64{5},
	}
	orgID := int64(42)

	// Act
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	// Assert
	sql := sb.String()
	assert.Contains(t, sql, "device_set_type = 'group'")
	assert.Contains(t, sql, "device_set_type = 'rack'")
	// Both should be AND-ed as separate membership predicates, with no OR
	// between the group and rack buckets.
	assert.NotContains(t, sql, " OR ")
	assert.Equal(t, 2, strings.Count(sql, "device_set_membership dcm"))
	// 4 new args: orgID + groupIDs + orgID + rackIDs
	assert.Len(t, resultArgs, 5) // initial + 2*orgID + groupIDs + rackIDs
	assert.Equal(t, 6, resultArgNum)
}

func TestAppendFilterSQL_CollectionFiltersWithExistingFilters_ArgNumContinuity(t *testing.T) {
	// Tests that collection filters correctly continue argNum from prior filters
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		modelFilter:    validNullString(),
		modelValues:    []string{"S21 XP"},
		groupIDsFilter: validNullString(),
		groupIDValues:  []int64{10},
	}
	orgID := int64(42)

	// Act
	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	// Assert
	sql := sb.String()
	// Model filter gets $2, group gets $3 (orgID) and $4 (groupIDs)
	assert.Contains(t, sql, "model = ANY($2::text[])")
	assert.Contains(t, sql, "org_id = $3")
	assert.Contains(t, sql, "device_set_id = ANY($4::bigint[])")
	assert.Len(t, resultArgs, 4) // initial + model + orgID + groupIDs
	assert.Equal(t, 5, resultArgNum)
}

func TestAppendFilterSQL_NoRawSliceArgs_WithCollectionFilters(t *testing.T) {
	// Verifies collection filter args are wrapped with pq.Array()
	var sb strings.Builder
	args := []any{"initial_org_id"}
	fp := minerFilterParams{
		groupIDsFilter: validNullString(),
		groupIDValues:  []int64{10, 20},
		rackIDsFilter:  validNullString(),
		rackIDValues:   []int64{5},
	}

	// Act
	resultArgs, _ := appendFilterSQL(&sb, args, 2, 1, fp)

	// Assert
	for i, arg := range resultArgs {
		kind := reflect.TypeOf(arg).Kind()
		assert.NotEqual(t, reflect.Slice, kind,
			fmt.Sprintf("arg at position %d is a raw slice (%T); must be wrapped with pq.Array()", i, arg))
	}
}

func TestBuildMinerFilterParams_FirmwareVersions(t *testing.T) {
	filter := &stores.MinerFilter{
		FirmwareVersions: []string{"v3.5.1", "v3.5.2"},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.firmwareVersionsFilter.Valid)
	assert.Equal(t, []string{"v3.5.1", "v3.5.2"}, params.firmwareVersionValues)
}

func TestBuildMinerFilterParams_ZoneKeys_AllScoped(t *testing.T) {
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 9, Zone: "Room 2"},
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.zoneKeysFilter.Valid)
	assert.Equal(t, []int64{7, 9}, params.scopedBuildingIDs)
	assert.Equal(t, []string{"Room 2", "Room 2"}, params.scopedZones)
	assert.Empty(t, params.wildcardZones)
}

func TestBuildMinerFilterParams_ZoneKeys_AllWildcard(t *testing.T) {
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: 0, Zone: "Room 2"},
			{BuildingID: 0, Zone: "Cold Aisle"},
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.zoneKeysFilter.Valid)
	assert.Empty(t, params.scopedBuildingIDs)
	assert.Empty(t, params.scopedZones)
	assert.Equal(t, []string{"Room 2", "Cold Aisle"}, params.wildcardZones)
}

func TestBuildMinerFilterParams_ZoneKeys_Mixed(t *testing.T) {
	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
			{BuildingID: 0, Zone: "Wildcard Zone"},
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.zoneKeysFilter.Valid)
	assert.Equal(t, []int64{7}, params.scopedBuildingIDs)
	assert.Equal(t, []string{"Room 2"}, params.scopedZones)
	assert.Equal(t, []string{"Wildcard Zone"}, params.wildcardZones)
}

func TestBuildMinerFilterParams_BuildingIDs(t *testing.T) {
	filter := &stores.MinerFilter{
		BuildingIDs:       []int64{7, 9},
		IncludeNoBuilding: true,
		IncludeNoRack:     true,
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.buildingIDsFilter.Valid)
	assert.Equal(t, []int64{7, 9}, params.buildingIDValues)
	assert.True(t, params.includeNoBuilding)
	assert.True(t, params.includeNoRack)
}

func TestBuildMinerFilterParams_FirmwareAndZones_Empty(t *testing.T) {
	// Empty slices should leave the filter unset (valid=false).
	filter := &stores.MinerFilter{
		FirmwareVersions: []string{},
		ZoneKeys:         []stores.ZoneKey{},
	}

	params := buildMinerFilterParams(filter)

	assert.False(t, params.firmwareVersionsFilter.Valid)
	assert.False(t, params.zoneKeysFilter.Valid)
}

func TestAppendFilterSQL_FirmwareVersionsOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		firmwareVersionsFilter: validNullString(),
		firmwareVersionValues:  []string{"v3.5.1", "v3.5.2"},
	}
	orgID := int64(42)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "discovered_device.firmware_version = ANY($2::text[])")
	assert.Len(t, resultArgs, 2) // initial + firmware values
	assert.Equal(t, 3, resultArgNum)
}

func TestAppendFilterSQL_ZoneKeys_WildcardOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		zoneKeysFilter: validNullString(),
		wildcardZones:  []string{"Room 2"},
	}
	orgID := int64(42)

	resultArgs, _ := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device_set_membership dcm")
	assert.Contains(t, sql, "dsr.zone = ANY($3::text[])")
	assert.Contains(t, sql, "dcm.org_id = $2",
		"single-layer org defense: every zone_keys EXISTS must carry dcm.org_id")
	assert.NotContains(t, sql, "UNNEST",
		"wildcard-only path should not emit the scoped UNNEST branch")
	assert.Len(t, resultArgs, 3)
}

func TestAppendFilterSQL_ZoneKeys_ScopedOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		zoneKeysFilter:    validNullString(),
		scopedBuildingIDs: []int64{7, 9},
		scopedZones:       []string{"Room 2", "Room 2"},
	}
	orgID := int64(42)

	resultArgs, _ := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "(dsr.building_id, dsr.zone) IN (")
	assert.Contains(t, sql, "UNNEST($3::bigint[], $4::text[])")
	assert.Contains(t, sql, "dcm.org_id = $2",
		"single-layer org defense: every zone_keys EXISTS must carry dcm.org_id")
	assert.NotContains(t, sql, "dsr.zone = ANY(",
		"scoped-only path should not emit the wildcard ANY branch")
	assert.Len(t, resultArgs, 4) // initial + orgID + buildingIDs + zones
}

func TestAppendFilterSQL_ZoneKeys_MixedScopedAndWildcard(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		zoneKeysFilter:    validNullString(),
		scopedBuildingIDs: []int64{7},
		scopedZones:       []string{"Room 2"},
		wildcardZones:     []string{"Cold Aisle"},
	}
	orgID := int64(42)

	resultArgs, _ := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	// Both branches present, OR'd inside one EXISTS so org_id only fires once.
	assert.Contains(t, sql, "(dsr.building_id, dsr.zone) IN (")
	assert.Contains(t, sql, "dsr.zone = ANY(")
	assert.Contains(t, sql, " OR ")
	assert.Contains(t, sql, "dcm.org_id = $2")
	assert.Equal(t, 1, strings.Count(sql, "dcm.org_id"),
		"both branches share one EXISTS so org_id appears exactly once")
	assert.Len(t, resultArgs, 5) // initial + orgID + buildingIDs + scopedZones + wildcardZones
}

func TestAppendFilterSQL_BuildingIDsOnly(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		buildingIDsFilter: validNullString(),
		buildingIDValues:  []int64{7, 9},
	}
	orgID := int64(42)

	resultArgs, _ := appendFilterSQL(&sb, args, argNum, orgID, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device.building_id = ANY($3::bigint[])")
	assert.Contains(t, sql, "dsr.building_id = ANY($3::bigint[])")
	assert.Contains(t, sql, "dcm.org_id = $2")
	assert.Len(t, resultArgs, 3) // initial + orgID + buildingIDs
}

func TestAppendFilterSQL_IncludeNoBuilding_Alone(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{includeNoBuilding: true}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "dsr.building_id IS NULL")
	assert.Contains(t, sql, "dcm.org_id = $2")
}

func TestAppendFilterSQL_IncludeNoRack_Alone(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{includeNoRack: true}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "NOT EXISTS")
	assert.Contains(t, sql, "device_set_type = 'rack'")
	assert.Contains(t, sql, "dcm.org_id = $2")
}

func TestAppendFilterSQL_BuildingFiltersORTogether(t *testing.T) {
	// building_ids + include_no_building + include_no_rack should be OR'd
	// inside one outer AND so devices in any of the three populations match.
	// An explicit rack filter keeps the "rack has no building" branch (it's
	// only dropped for the rack-less new-rack shape).
	var sb strings.Builder
	fp := minerFilterParams{
		buildingIDsFilter: validNullString(),
		buildingIDValues:  []int64{7},
		rackIDsFilter:     validNullString(),
		rackIDValues:      []int64{5},
		includeNoBuilding: true,
		includeNoRack:     true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device.building_id = ANY")
	assert.Contains(t, sql, "dsr.building_id = ANY")
	assert.Contains(t, sql, "dsr.building_id IS NULL")
	assert.Contains(t, sql, "NOT EXISTS")
	assert.GreaterOrEqual(t, strings.Count(sql, " OR "), 2)
}

// The assignable-only filter for a NEW rack sends include_no_rack with no
// rack_ids and no building. Its "no building" pin must NOT emit the "rack has
// no building" branch — there are no target members to keep, and that branch
// would otherwise match miners in OTHER building-less racks. The no-rack
// branch (device.building_id IS NULL AND NOT EXISTS rack) still admits
// rackless miners with no direct building.
func TestAppendFilterSQL_NewRackShape_DropsNoBuildingRackBranch(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		includeNoBuilding: true,
		includeNoRack:     true,
		includeUnassigned: true,
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 42, fp)

	sql := sb.String()
	assert.Contains(t, sql, "device.site_id IS NULL")
	assert.Contains(t, sql, "(device.building_id IS NULL AND NOT EXISTS")
	assert.NotContains(t, sql, "dsr.building_id IS NULL")
}

// TestAppendFilterSQL_ZoneKeys_OrgIDDefenseInDepth guards against the
// regression mode flagged by finding 2: a refactor that drops the
// dcm.org_id clause from the wildcard branch would silently expose
// cross-org data. The wildcard branch skips the parseFilter cross-org
// check so the SQL clause is the only defense.
func TestAppendFilterSQL_ZoneKeys_OrgIDDefenseInDepth(t *testing.T) {
	wildcardFP := minerFilterParams{
		zoneKeysFilter: validNullString(),
		wildcardZones:  []string{"Room 2"},
	}
	scopedFP := minerFilterParams{
		zoneKeysFilter:    validNullString(),
		scopedBuildingIDs: []int64{7},
		scopedZones:       []string{"Room 2"},
	}
	for name, fp := range map[string]minerFilterParams{"wildcard": wildcardFP, "scoped": scopedFP} {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			appendFilterSQL(&sb, []any{}, 1, 42, fp)
			assert.Contains(t, sb.String(), "dcm.org_id = $1",
				"%s branch must include dcm.org_id — single-layer defense", name)
		})
	}
}

func TestAppendFilterSQL_NewFilters_NoRawSliceArgs(t *testing.T) {
	// Every slice-shaped arg must be pq.Array-wrapped so Postgres receives a
	// proper array literal. A raw Go slice as a query arg means the driver
	// silently fails or sends the wrong shape.
	var sb strings.Builder
	args := []any{"initial_org_id"}
	fp := minerFilterParams{
		firmwareVersionsFilter: validNullString(),
		firmwareVersionValues:  []string{"v3.5.1"},
		zoneKeysFilter:         validNullString(),
		scopedBuildingIDs:      []int64{7},
		scopedZones:            []string{"Room 2"},
		wildcardZones:          []string{"Cold Aisle"},
		buildingIDsFilter:      validNullString(),
		buildingIDValues:       []int64{7, 9},
	}

	resultArgs, _ := appendFilterSQL(&sb, args, 2, 1, fp)

	for i, arg := range resultArgs {
		kind := reflect.TypeOf(arg).Kind()
		assert.NotEqual(t, reflect.Slice, kind,
			fmt.Sprintf("arg at position %d is a raw slice (%T); must be wrapped with pq.Array()", i, arg))
	}
}

// validNullString creates a valid sql.NullString for testing.
func validNullString() sql.NullString {
	return sql.NullString{Valid: true}
}

func ptr[T any](v T) *T { return &v }

func TestBuildMinerFilterParams_NumericRanges(t *testing.T) {
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: ptr(90.0)},
			{Field: stores.NumericFilterFieldPowerKW, Max: ptr(3000.0), MaxInclusive: true},
		},
	}

	params := buildMinerFilterParams(filter)

	require.Len(t, params.numericRanges, 2)
	assert.Equal(t, stores.NumericFilterFieldHashrateTHs, params.numericRanges[0].Field)
	require.NotNil(t, params.numericRanges[0].Min)
	assert.Equal(t, 90.0, *params.numericRanges[0].Min)
	assert.Equal(t, stores.NumericFilterFieldPowerKW, params.numericRanges[1].Field)
	assert.True(t, params.numericRanges[1].MaxInclusive)
}

func TestBuildMinerFilterParams_IPCIDRs(t *testing.T) {
	filter := &stores.MinerFilter{
		IPCIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.ipCIDRsFilter.Valid)
	assert.Equal(t, []string{"192.168.1.0/24", "10.0.0.0/8"}, params.ipCIDRValues)
}

func TestBuildMinerFilterParams_NoNumeric_NoIPCIDR(t *testing.T) {
	filter := &stores.MinerFilter{}

	params := buildMinerFilterParams(filter)

	assert.Empty(t, params.numericRanges)
	assert.False(t, params.ipCIDRsFilter.Valid)
	assert.Empty(t, params.ipCIDRValues)
	assert.Empty(t, params.ipRangeStarts)
	assert.Empty(t, params.ipRangeEnds)
}

func TestBuildMinerFilterParams_IPRanges(t *testing.T) {
	filter := &stores.MinerFilter{
		IPRanges: []stores.IPRange{
			{Start: netip.MustParseAddr("10.0.0.10"), End: netip.MustParseAddr("10.0.0.20")},
			{Start: netip.MustParseAddr("192.168.1.1"), End: netip.MustParseAddr("192.168.1.5")},
		},
	}

	params := buildMinerFilterParams(filter)

	assert.Equal(t, []string{"10.0.0.10", "192.168.1.1"}, params.ipRangeStarts)
	assert.Equal(t, []string{"10.0.0.20", "192.168.1.5"}, params.ipRangeEnds)
}

func TestAppendFilterSQL_NumericRange_LowerBoundExclusive(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: ptr(90.0)},
		},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, 1, fp)

	sql := sb.String()
	assert.Contains(t, sql, "latest_metrics.hash_rate_hs / 1e12 > $2")
	assert.Contains(t, sql, "device_status.status != 'OFFLINE'", "numeric filter must exclude OFFLINE miners")
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

func TestAppendFilterSQL_NumericRange_LowerBoundInclusive(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldEfficiencyJTH, Min: ptr(20.0), MinInclusive: true},
		},
	}

	appendFilterSQL(&sb, args, 2, 1, fp)

	assert.Contains(t, sb.String(), "latest_metrics.efficiency_jh * 1e12 >= $2")
}

func TestAppendFilterSQL_NumericRange_UpperBoundInclusive(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldPowerKW, Max: ptr(3000.0), MaxInclusive: true},
		},
	}

	appendFilterSQL(&sb, args, 2, 1, fp)

	assert.Contains(t, sb.String(), "latest_metrics.power_w / 1e3 <= $2")
}

func TestAppendFilterSQL_NumericRange_BetweenEmitsTwoPredicates(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{
				Field:        stores.NumericFilterFieldHashrateTHs,
				Min:          ptr(90.0),
				Max:          ptr(100.0),
				MinInclusive: true,
				MaxInclusive: true,
			},
		},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	sql := sb.String()
	assert.Contains(t, sql, "latest_metrics.hash_rate_hs / 1e12 >= $2")
	assert.Contains(t, sql, "latest_metrics.hash_rate_hs / 1e12 <= $3")
	assert.Len(t, resultArgs, 3)
	assert.Equal(t, 4, resultArgNum)
}

func TestAppendFilterSQL_NumericRange_FieldToColumnMapping(t *testing.T) {
	cases := []struct {
		field          stores.NumericFilterField
		expectedColumn string
	}{
		{stores.NumericFilterFieldHashrateTHs, "latest_metrics.hash_rate_hs / 1e12"},
		{stores.NumericFilterFieldEfficiencyJTH, "latest_metrics.efficiency_jh * 1e12"},
		{stores.NumericFilterFieldPowerKW, "latest_metrics.power_w / 1e3"},
		{stores.NumericFilterFieldTemperatureC, "latest_metrics.temp_c"},
		{stores.NumericFilterFieldVoltageV, "latest_metrics.voltage_v"},
		{stores.NumericFilterFieldCurrentA, "latest_metrics.current_a"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("field-%d", tc.field), func(t *testing.T) {
			var sb strings.Builder
			fp := minerFilterParams{
				numericRanges: []stores.NumericRange{
					{Field: tc.field, Min: ptr(1.0)},
				},
			}
			appendFilterSQL(&sb, []any{"initial"}, 2, 1, fp)
			assert.Contains(t, sb.String(), tc.expectedColumn)
		})
	}
}

func TestAppendFilterSQL_NoNumericRange_DoesNotExcludeOffline(t *testing.T) {
	// Sanity: status-filter exclusion only fires when a numeric range is set.
	var sb strings.Builder
	fp := minerFilterParams{
		modelFilter: validNullString(),
		modelValues: []string{"S21 XP"},
	}

	appendFilterSQL(&sb, []any{"initial"}, 2, 1, fp)

	assert.NotContains(t, sb.String(), "device_status.status != 'OFFLINE'")
}

func TestAppendFilterSQL_IPCIDRs_UsesInetAnyPredicate(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		ipCIDRsFilter: validNullString(),
		ipCIDRValues:  []string{"192.168.1.0/24", "10.0.0.0/8"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	sql := sb.String()
	assert.Contains(t, sql, "discovered_device.ip_address_inet <<= ANY($2::cidr[])")
	// Single param regardless of CIDR count.
	assert.Len(t, resultArgs, 2)
	assert.Equal(t, 3, resultArgNum)
}

func TestAppendFilterSQL_IPRanges_UsesBetweenPredicate(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		ipRangeStarts: []string{"10.0.0.10", "192.168.1.1"},
		ipRangeEnds:   []string{"10.0.0.20", "192.168.1.5"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	sql := sb.String()
	assert.Contains(t, sql, "discovered_device.ip_address_inet BETWEEN $2::inet AND $3::inet")
	assert.Contains(t, sql, "discovered_device.ip_address_inet BETWEEN $4::inet AND $5::inet")
	assert.Contains(t, sql, " OR ", "multiple ranges must be OR'd within the subnet group")
	// initial + 2 bounds per range.
	assert.Len(t, resultArgs, 5)
	assert.Equal(t, 6, resultArgNum)
}

func TestAppendFilterSQL_IPCIDRsAndRanges_OrGroupedTogether(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		ipCIDRsFilter: validNullString(),
		ipCIDRValues:  []string{"192.168.1.0/24"},
		ipRangeStarts: []string{"10.0.0.10"},
		ipRangeEnds:   []string{"10.0.0.20"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	sql := sb.String()
	// CIDR containment and range are OR'd inside one parenthesized group.
	assert.Contains(t, sql, "(discovered_device.ip_address_inet <<= ANY($2::cidr[]) OR discovered_device.ip_address_inet BETWEEN $3::inet AND $4::inet)")
	// initial + cidr array + 2 range bounds.
	assert.Len(t, resultArgs, 4)
	assert.Equal(t, 5, resultArgNum)
}

func TestAppendFilterSQL_IPCIDRs_NoRawSliceArgs(t *testing.T) {
	var sb strings.Builder
	fp := minerFilterParams{
		ipCIDRsFilter: validNullString(),
		ipCIDRValues:  []string{"192.168.1.0/24"},
	}

	resultArgs, _ := appendFilterSQL(&sb, []any{"initial"}, 2, 1, fp)

	for i, arg := range resultArgs {
		kind := reflect.TypeOf(arg).Kind()
		assert.NotEqual(t, reflect.Slice, kind,
			fmt.Sprintf("arg at position %d is a raw slice (%T); must be wrapped with pq.Array()", i, arg))
	}
}

func TestAppendFilterSQL_NumericAndCIDRWithExistingFilters_ArgContinuity(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	fp := minerFilterParams{
		modelFilter: validNullString(),
		modelValues: []string{"S21 XP"},
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: ptr(90.0)},
			{Field: stores.NumericFilterFieldPowerKW, Max: ptr(3000.0)},
		},
		ipCIDRsFilter: validNullString(),
		ipCIDRValues:  []string{"192.168.1.0/24"},
	}

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, 2, 1, fp)

	sql := sb.String()
	// model gets $2; first numeric bound $3; second numeric bound $4; cidrs $5.
	assert.Contains(t, sql, "discovered_device.model = ANY($2::text[])")
	assert.Contains(t, sql, "latest_metrics.hash_rate_hs / 1e12 > $3")
	assert.Contains(t, sql, "latest_metrics.power_w / 1e3 < $4")
	assert.Contains(t, sql, "discovered_device.ip_address_inet <<= ANY($5::cidr[])")
	assert.Len(t, resultArgs, 5) // initial + model + 2 numeric + cidrs
	assert.Equal(t, 6, resultArgNum)
}
