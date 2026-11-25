package telemetry

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/require"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

type discoverFunc func(ctx context.Context, ipAddress, port string) (*discovery.DiscoveredDevice, error)

func (f discoverFunc) Discover(ctx context.Context, ipAddress, port string) (*discovery.DiscoveredDevice, error) {
	return f(ctx, ipAddress, port)
}

type noCapabilities struct{}

func (noCapabilities) GetMinerCapabilitiesForDevice(context.Context, *pb.Device) *capabilitiespb.MinerCapabilities {
	return nil
}
func (noCapabilities) GetDefaultDiscoveryPorts(context.Context) []string { return nil }
func (noCapabilities) GetDiscoveryPorts(context.Context) []string        { return nil }

// A direct scan saves each device after its probe. Thus telemetry can store a
// new version after the probe read the old version and before the scan saves it.
func TestLateDirectScanResultKeepsFirmwareVersionFromTelemetry(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, tc := range []struct{ name, scanned string }{
		{"the scan has the old version", "1.0.0"},
		{"the scan has no version", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const ip, port, mac, oldVersion, newVersion = "10.0.0.21", "8080", "AA:BB:CC:DD:EE:21", "1.0.0", "2.0.0"
			db := dbtest.GetTestDB(t)
			org, err := sqlc.New(db).CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: "late-direct-scan", Name: "late-direct-scan"})
			require.NoError(t, err)
			ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: org})
			discoveryStore := sqlstores.NewSQLDiscoveredDeviceStore(db)
			deviceStore := sqlstores.NewSQLDeviceStore(db)
			scan := func(version string, duringProbe func()) string {
				probe := discoverFunc(func(context.Context, string, string) (*discovery.DiscoveredDevice, error) {
					duringProbe()
					return &discovery.DiscoveredDevice{Device: pb.Device{
						IpAddress: ip, Port: port, UrlScheme: "http", DriverName: "proto", MacAddress: mac, FirmwareVersion: version,
					}}, nil
				})
				service := pairing.NewService(discoveryStore, deviceStore, sqlstores.NewSQLTransactor(db), nil, nil, probe, noCapabilities{}, nil, nil)
				results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{ip}, Ports: []string{port}})
				require.NoError(t, err)
				var devices []*pb.Device
				for result := range results {
					devices = append(devices, result.Devices...)
				}
				require.Len(t, devices, 1)
				return devices[0].DeviceIdentifier
			}

			identifier := scan(oldVersion, func() {})
			doi := discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: identifier}
			discovered, err := discoveryStore.GetDevice(ctx, doi)
			require.NoError(t, err)
			require.NoError(t, deviceStore.InsertDevice(ctx, &discovered.Device, org, identifier))
			require.NoError(t, deviceStore.UpsertDevicePairing(ctx, &discovered.Device, org, "PAIRED"))

			telemetry := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, deviceStore, nil)
			poll := func(version string) {
				telemetry.persistFirmwareVersionIfChanged(ctx, models.DeviceIdentifier(identifier), version)
			}
			poll(oldVersion)

			require.Equal(t, identifier, scan(tc.scanned, func() { poll(newVersion) }))
			poll(newVersion)

			stored, err := discoveryStore.GetDevice(ctx, doi)
			require.NoError(t, err)
			require.Equal(t, newVersion, stored.FirmwareVersion)
		})
	}
}
