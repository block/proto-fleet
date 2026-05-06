package sqlstores

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

func TestBuildDeviceIdentifiersByOrgWithFilterQuerySQL_NumericRanges_UsesTelemetryJoin(t *testing.T) {
	minValue := 90.0
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &minValue, MinInclusive: true},
		},
		pairingStatusFilter: validNullString(),
		pairingStatusValues: []string{"PAIRED"},
	}

	query, args := buildDeviceIdentifiersByOrgWithFilterQuerySQL(1, fp)

	assert.True(t, strings.HasPrefix(query, "WITH latest_metrics AS"), "numeric filters should prepend the telemetry CTE")
	assert.Contains(t, query, minerTelemetryInnerJoin)
	assert.Contains(t, query, "latest_metrics.hash_rate_hs / 1e12 >=")
	require.Len(t, args, 3)
	assert.EqualValues(t, 1, args[0])
	assert.Equal(t, minValue, args[2])
}

func TestBuildStateCountsQuerySQL_NumericAndCIDRFilters_UsesDynamicPredicates(t *testing.T) {
	minValue := 90.0
	fp := minerFilterParams{
		numericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &minValue},
		},
		ipCIDRsFilter: validNullString(),
		ipCIDRValues:  []string{"192.168.1.0/24"},
	}

	store := &SQLDeviceStore{}
	query, args := store.buildStateCountsQuerySQL(1, fp)

	assert.True(t, strings.HasPrefix(query, "WITH latest_metrics AS"), "numeric filters should prepend the telemetry CTE")
	assert.Contains(t, query, "open_errors AS")
	assert.Contains(t, query, minerTelemetryInnerJoin)
	assert.Contains(t, query, "filtered.has_open_error")
	assert.Contains(t, query, "NULLIF(discovered_device.ip_address, '')::inet <<=")
	require.Len(t, args, 3)
	assert.EqualValues(t, 1, args[0])
	assert.Equal(t, minValue, args[1])
}
