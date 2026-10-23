package sqlstores_test

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestDiscoveryListingSkipsInvalidEndpointsWithoutLosingPages(t *testing.T) {
	db, store, org := ct1HAStore(t)
	for i := range 6 {
		identifier := fmt.Sprintf("device-%d", i)
		device := ct1HADiscovery(org, identifier, fmt.Sprintf("10.0.0.%d", i+1))
		device.Endpoints = nil
		saveCT1HA(t, store, identifier, device)
		if i != 2 && i != 4 {
			_, err := db.ExecContext(t.Context(), `UPDATE discovered_device SET endpoints = '{"invalid":{}}' WHERE org_id = $1 AND device_identifier = $2`, org, identifier)
			require.NoError(t, err)
		}
	}
	first, cursor, err := store.GetActiveUnpairedDevices(t.Context(), org, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "device-2", first[0].DeviceIdentifier)
	require.NotEmpty(t, cursor)
	count, err := store.CountActiveUnpairedDevices(t.Context(), org)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	second, cursor, err := store.GetActiveUnpairedDevices(t.Context(), org, cursor, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "device-4", second[0].DeviceIdentifier)
	require.Empty(t, cursor)
	_, err = store.GetDevice(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "device-0"})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "invalid endpoints must never become a usable route")

	_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET endpoints = '{"invalid":{}}' WHERE org_id = $1`, org)
	require.NoError(t, err)
	devices, cursor, err := store.GetActiveUnpairedDevices(t.Context(), org, "", 1)
	require.NoError(t, err)
	require.Empty(t, devices)
	require.Empty(t, cursor)
	count, err = store.CountActiveUnpairedDevices(t.Context(), org)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDirectDiscoveryRejectsMissingGroupWithClearPrecondition(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(fmt.Sprintf("paired=%t", paired), func(t *testing.T) {
			db, store, org := ct1HAStore(t)
			before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
			if paired {
				pairCT1HA(t, db, before)
			}
			incoming := ct1HADiscovery(org, "a", "10.0.0.11")
			incoming.Endpoints = nil
			_, err := store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), incoming)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.NotContains(t, err.Error(), "no rows")
			after, err := store.GetDevice(t.Context(), before.GetDeviceOrgIdentifier())
			require.NoError(t, err)
			require.True(t, proto.Equal(&before.Device, &after.Device))
		})
	}
}

func TestPairingMetadataSaveKeepsCurrentEndpointGroup(t *testing.T) {
	for _, legacySnapshot := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_snapshot=%t", legacySnapshot), func(t *testing.T) {
			testPairingMetadataSaveKeepsCurrentEndpointGroup(t, legacySnapshot)
		})
	}
}

func testPairingMetadataSaveKeepsCurrentEndpointGroup(t *testing.T, legacySnapshot bool) {
	t.Helper()
	_, store, org := ct1HAStore(t)
	initial := ct1HADiscovery(org, "a", "10.0.0.11")
	if legacySnapshot {
		initial.Endpoints = nil
	}
	before := saveCT1HA(t, store, "container", initial)
	incoming := ct1HADiscovery(org, "a", "10.0.0.11")
	incoming.Endpoints[0].DeviceKey = "different-group"
	current := saveCT1HA(t, store, "scan-a", incoming)
	before.FirmwareVersion = "pair-v2"
	saved, err := store.Save(t.Context(), before.GetDeviceOrgIdentifier(), before)
	require.NoError(t, err)
	require.Equal(t, "pair-v2", saved.FirmwareVersion)
	require.True(t, proto.Equal(current.Endpoints[0], saved.Endpoints[0]))
	before.DriverName = "unrelated-driver"
	_, err = store.Save(t.Context(), before.GetDeviceOrgIdentifier(), before)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.NotContains(t, err.Error(), "no rows")
}
