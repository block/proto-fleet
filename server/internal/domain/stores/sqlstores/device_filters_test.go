package sqlstores

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
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
		PairingStatuses:    []fm.PairingStatus{fm.PairingStatus_PAIRING_STATUS_PAIRED},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.True(t, params.modelFilter.Valid)
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
	// Errors branch excludes NULL+paired miners (they remain bucketed as offline).
	assert.Contains(t, sql, "NOT (device_status.status IS NULL AND device_pairing.pairing_status = 'PAIRED')")
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
	// Narrowed to PAIRED only (matches CountMinersByState scope); excludes PENDING/FAILED/UNPAIRED.
	assert.Contains(t, sql, "device_pairing.pairing_status = 'PAIRED'")
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
		statusFilter:        validNullString(),
		statusValues:        []string{"ACTIVE"},
	}
	orgID := int64(1)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	assert.Contains(t, sb.String(), "pairing_status")
	assert.Contains(t, sb.String(), "discovered_device.model")
	assert.Contains(t, sb.String(), "device_status.status")
	assert.Len(t, resultArgs, 5) // initial + pairing + model + status + orgID
	assert.Equal(t, 6, resultArgNum)
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
	// Both should be AND-ed (separate AND EXISTS clauses, no OR between them)
	assert.NotContains(t, sql, " OR ")
	assert.Equal(t, strings.Count(sql, " AND EXISTS"), 2)
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

// validNullString creates a valid sql.NullString for testing.
func validNullString() sql.NullString {
	return sql.NullString{Valid: true}
}
