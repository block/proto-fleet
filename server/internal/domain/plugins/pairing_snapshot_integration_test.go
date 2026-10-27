package plugins_test

import (
	"context"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkmocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDirectPairingRejectsDiscoveryChangedDuringProbe(t *testing.T) {
	for _, change := range []string{"route", "owner"} {
		t.Run(change, func(t *testing.T) {
			db := testutil.GetTestDB(t)
			org, err := sqlc.New(db).CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: "pair-race", Name: "Pair race"})
			require.NoError(t, err)
			store := sqlstores.NewSQLDiscoveredDeviceStore(db)
			fixture := func(id, host string) *discovery.DiscoveredDevice {
				return &discovery.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "container", DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
					IpAddress: host, Port: "8080", UrlScheme: "http", RpcPort: 9095, SerialNumber: id, Model: "CT1", Manufacturer: "Proto",
					Endpoints: []*commonpb.DeviceEndpoint{{Id: id, Host: host, Port: 8080, UrlScheme: "http", RpcPort: 9095, DeviceKey: "a-b"}}}, OrgID: org, IsActive: true}
			}
			initial := fixture("b", "10.0.0.12")
			selected, err := store.SaveDirect(t.Context(), initial.GetDeviceOrgIdentifier(), initial)
			require.NoError(t, err)
			ctrl := gomock.NewController(t)
			driver := sdkmocks.NewMockDriver(ctrl)
			started, release := make(chan struct{}), make(chan struct{})
			unlock := sync.OnceFunc(func() { close(release) })
			defer unlock()
			driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
				close(started)
				<-release
				info.SerialNumber = "b"
				return info, nil
			})
			manager := plugins.NewManager(&plugins.Config{})
			require.NoError(t, manager.RegisterPluginForTest(&plugins.LoadedPlugin{Name: "snapshot", Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}))
			// Any device, pairing-status, or credentials write fails this test.
			devices := mocks.NewMockDeviceStore(ctrl)
			pairer := plugins.NewPairer(manager, sqlstores.NewSQLTransactor(db), store, devices, nil)
			done := make(chan error, 1)
			go func() { done <- pairer.PairDevice(t.Context(), selected, nil) }()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("probe failed early: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("probe did not start")
			}
			incoming := fixture("b", "10.0.0.12")
			switch change {
			case "route":
				incoming.IpAddress, incoming.Endpoints[0].Host = "10.0.0.99", "10.0.0.99"
			case "owner":
				var node int64
				require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status) VALUES ($1,'pair-race',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
				_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET discovered_by_fleet_node_id=$2 WHERE org_id=$1`, org, node)
				require.NoError(t, err)
			}
			if change != "owner" {
				_, err = store.SaveDirect(t.Context(), selected.GetDeviceOrgIdentifier(), incoming)
				require.NoError(t, err)
			}
			unlock()
			require.True(t, fleeterror.IsFailedPreconditionError(<-done), "stale verified inventory must not reach persistence")
		})
	}
}
