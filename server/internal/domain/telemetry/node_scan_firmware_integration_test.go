package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	nodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

// A Fleet Node sends its scan reports when the scan ends. Thus a report can
// carry the version that a device had before a firmware update and arrive
// after telemetry stored the new version.
func TestLateNodeScanReportKeepsFirmwareVersionFromTelemetry(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, tc := range []struct{ name, reported string }{
		{"the report has the old version", "1.0.0"},
		{"the report has no version", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const identifier, oldVersion, newVersion = "mac:AA:BB:CC:DD:EE:01", "1.0.0", "2.0.0"
			db := dbtest.GetTestDB(t)
			org, err := sqlc.New(db).CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: "late-scan", Name: "late-scan"})
			require.NoError(t, err)
			var node int64
			require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'node',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
			nodeStore := sqlstores.NewSQLFleetNodePairingStore(db)
			discoveryStore := sqlstores.NewSQLDiscoveredDeviceStore(db)
			deviceStore := sqlstores.NewSQLDeviceStore(db)
			doi := discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: identifier}
			stored := func() string {
				row, err := discoveryStore.GetDevice(t.Context(), doi)
				require.NoError(t, err)
				return row.FirmwareVersion
			}
			scanReport := func(version string) {
				report := nodepairing.DiscoveredDeviceReport{DeviceIdentifier: identifier, IPAddress: "10.0.0.11", Port: "8080", URLScheme: "http",
					DriverName: "proto", Model: "Rig", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG, FirmwareVersion: version}
				_, ok, err := nodeStore.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, report)
				require.NoError(t, err)
				require.True(t, ok)
			}

			scanReport(oldVersion)
			discovered, err := discoveryStore.GetDevice(t.Context(), doi)
			require.NoError(t, err)
			require.NoError(t, deviceStore.InsertDevice(t.Context(), &discovered.Device, org, identifier))
			require.NoError(t, deviceStore.UpsertDevicePairing(t.Context(), &discovered.Device, org, "PAIRED"))
			device, err := sqlc.New(db).GetDeviceByDeviceIdentifier(t.Context(), sqlc.GetDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: identifier})
			require.NoError(t, err)
			_, err = nodeStore.PairDeviceToFleetNode(t.Context(), node, device.ID, org, nil)
			require.NoError(t, err)

			telemetry := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, deviceStore, nil)
			poll := func(version string) {
				telemetry.persistFirmwareVersionIfChanged(t.Context(), models.DeviceIdentifier(identifier), version)
			}
			poll(oldVersion)
			poll(newVersion)
			require.Equal(t, newVersion, stored())

			scanReport(tc.reported)
			poll(newVersion)

			require.Equal(t, newVersion, stored())
		})
	}
}
