package pairing_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeDiscoveryListingSkipsInvalidEndpointsWithoutLosingPages(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "node-invalid-endpoints")
	for _, id := range []string{"bad-first", "bad-second", "good-first", "bad-middle", "good-second", "bad-last"} {
		upsertNodeDiscovered(t, svc, org, node, id)
		if id != "good-first" && id != "good-second" {
			_, err := db.ExecContext(t.Context(), `UPDATE discovered_device SET endpoints = '{"invalid":{}}' WHERE org_id = $1 AND device_identifier = $2`, org, id)
			require.NoError(t, err)
		}
	}
	limit := int64(1)
	first, cursor, err := svc.ListDiscoveredDevicesForFleetNode(t.Context(), org, &node, nil, &limit)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "good-first", first[0].DeviceIdentifier)
	require.NotNil(t, cursor)
	second, cursor, err := svc.ListDiscoveredDevicesForFleetNode(t.Context(), org, &node, cursor, &limit)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "good-second", second[0].DeviceIdentifier)
	require.NotNil(t, cursor)
	last, cursor, err := svc.ListDiscoveredDevicesForFleetNode(t.Context(), org, &node, cursor, &limit)
	require.NoError(t, err)
	require.Empty(t, last)
	require.Nil(t, cursor)
	all, _, err := svc.ListDiscoveredDevicesForFleetNode(t.Context(), org, &node, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"good-first", "good-second"}, discoveredIdentifiers(all))
}
