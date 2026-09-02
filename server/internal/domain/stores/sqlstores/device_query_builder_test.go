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
	assert.Contains(t, query, "LEFT JOIN fleet_node assigned_fleet_node")
	assert.Contains(t, query, "effective_status.status IS NULL OR effective_status.status != 'OFFLINE'")
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
	assert.Contains(t, query, "discovered_device.ip_address_inet <<= ANY(")
	require.Len(t, args, 3)
	assert.EqualValues(t, 1, args[0])
	assert.Equal(t, minValue, args[1])
}

func TestBuildStateCountsQuerySQL_DerivesFleetNodeOutagesAsOffline(t *testing.T) {
	store := &SQLDeviceStore{}
	query, _ := store.buildStateCountsQuerySQL(1, minerFilterParams{})

	assert.Contains(t, query, "THEN 'OFFLINE'")
	assert.Contains(t, query, "assigned_fleet_node.last_seen_at")
	assert.Contains(t, query, "filtered.status = 'OFFLINE'")
	assert.NotContains(t, query, "UNAVAILABLE")
}

func TestBuildDeviceIdentifiersByOrgWithFilterQuerySQL_JoinsFleetNodeForEffectiveStatus(t *testing.T) {
	query, _ := buildDeviceIdentifiersByOrgWithFilterQuerySQL(1, minerFilterParams{
		statusFilter: validNullString(),
		statusValues: []string{"OFFLINE"},
	})

	assert.Contains(t, query, "LEFT JOIN fleet_node assigned_fleet_node")
	assert.Contains(t, query, "THEN 'OFFLINE'")
}

func TestListAndPaginationCountShareEffectiveStatusFilter(t *testing.T) {
	fp := minerFilterParams{
		statusFilter: validNullString(),
		statusValues: []string{"OFFLINE"},
	}
	store := &SQLDeviceStore{}

	listQuery, _ := store.buildListQuerySQL(1, nil, 50, fp, nil)
	countQuery, _ := store.buildCountQuerySQL(1, fp)

	for _, query := range []string{listQuery, countQuery} {
		assert.Contains(t, query, "THEN 'OFFLINE'")
		assert.Contains(t, query, "assigned_fleet_node.last_seen_at")
		assert.NotContains(t, query, "device_status.status::text = ANY")
	}
}
