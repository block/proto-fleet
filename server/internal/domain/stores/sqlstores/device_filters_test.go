package sqlstores

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minermodels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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
		MinerType:          []minermodels.Type{minermodels.TypeProto},
		PairingStatuses:    []fm.PairingStatus{fm.PairingStatus_PAIRING_STATUS_PAIRED},
	}

	params := buildMinerFilterParams(filter)

	assert.True(t, params.statusFilter.Valid)
	assert.True(t, params.typeFilter.Valid)
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

	assert.Contains(t, sb.String(), "AUTHENTICATION_NEEDED")
	assert.Contains(t, sb.String(), "errors")
	assert.Len(t, resultArgs, 4) // initial + statusValues + orgID + orgID
	assert.Equal(t, 5, resultArgNum)
}

func TestAppendFilterSQL_CombinedFilters(t *testing.T) {
	var sb strings.Builder
	args := []any{"initial"}
	argNum := 2
	fp := minerFilterParams{
		pairingStatusFilter: validNullString(),
		pairingStatusValues: []string{"PAIRED"},
		typeFilter:          validNullString(),
		typeValues:          []string{"proto"},
		statusFilter:        validNullString(),
		statusValues:        []string{"ACTIVE"},
	}
	orgID := int64(1)

	resultArgs, resultArgNum := appendFilterSQL(&sb, args, argNum, orgID, fp)

	assert.Contains(t, sb.String(), "pairing_status")
	assert.Contains(t, sb.String(), "discovered_device.type")
	assert.Contains(t, sb.String(), "device_status.status")
	assert.Len(t, resultArgs, 5) // initial + pairing + type + status + orgID
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
		typeFilter:          validNullString(),
		typeValues:          []string{"proto"},
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
		typeFilter:                validNullString(),
		typeValues:                []string{"proto"},
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

// validNullString creates a valid sql.NullString for testing.
func validNullString() sql.NullString {
	return sql.NullString{Valid: true}
}
