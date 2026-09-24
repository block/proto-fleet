package sqlstores_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeviceClassSurvivesDiscoveryPairingAndRediscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "device-class", Name: "Device class"})
	require.NoError(t, err)
	discovered := sqlstores.NewSQLDiscoveredDeviceStore(db)
	devices := sqlstores.NewSQLDeviceStore(db)
	doi := discoverymodels.DeviceOrgIdentifier{OrgID: orgID, DeviceIdentifier: "container-1"}
	device := &discoverymodels.DiscoveredDevice{
		Device: pairingpb.Device{
			DeviceIdentifier: doi.DeviceIdentifier,
			DeviceClass:      commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
			Model:            "CT1",
			Manufacturer:     "Proto",
			DriverName:       "proto",
			IpAddress:        "192.168.1.10",
			Port:             "80",
			UrlScheme:        "http",
			MacAddress:       "00:11:22:33:44:55",
		},
		OrgID: orgID, IsActive: true,
	}
	_, err = discovered.Save(ctx, doi, device)
	require.NoError(t, err)
	reloaded, err := discovered.GetDevice(ctx, doi)
	require.NoError(t, err)
	require.Equal(t, device.DeviceClass, reloaded.DeviceClass)
	unpaired, _, err := discovered.GetActiveUnpairedDevices(ctx, orgID, "", 10)
	require.NoError(t, err)
	require.Len(t, unpaired, 1)
	require.Equal(t, device.DeviceClass, unpaired[0].DeviceClass)

	rows, _, _, err := devices.ListMinerStateSnapshots(ctx, orgID, "", 10, nil, nil)
	require.NoError(t, err)
	require.Empty(t, rows)

	require.NoError(t, devices.InsertDevice(ctx, &device.Device, orgID, doi.DeviceIdentifier))
	paired, err := devices.GetDeviceByDeviceIdentifier(ctx, doi.DeviceIdentifier, orgID)
	require.NoError(t, err)
	require.Equal(t, device.DeviceClass, paired.DeviceClass)
	rows, _, _, err = devices.ListMinerStateSnapshots(ctx, orgID, "", 10, nil, nil)
	require.NoError(t, err)
	require.Empty(t, rows)

	// A report without class information preserves the known identity.
	device.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED
	_, err = discovered.Save(ctx, doi, device)
	require.NoError(t, err)
	paired, err = devices.GetDeviceByDeviceIdentifier(ctx, doi.DeviceIdentifier, orgID)
	require.NoError(t, err)
	require.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, paired.DeviceClass)

	// A new explicit report replaces the stored class without consulting model.
	device.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_RIG
	_, err = discovered.Save(ctx, doi, device)
	require.NoError(t, err)
	paired, err = devices.GetDeviceByDeviceIdentifier(ctx, doi.DeviceIdentifier, orgID)
	require.NoError(t, err)
	require.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_RIG, paired.DeviceClass)
}
