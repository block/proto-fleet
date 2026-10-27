package sqlstores_test

import (
	"fmt"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestDirectContainerLookupRequiresMatchingOrganizationAndDevice(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	org, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "container-org", Name: "Containers"})
	require.NoError(t, err)
	otherOrg, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "other-container-org", Name: "Other"})
	require.NoError(t, err)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	for i, modify := range []func(*discovery.DiscoveredDevice){
		func(d *discovery.DiscoveredDevice) { d.OrgID = otherOrg },
		func(d *discovery.DiscoveredDevice) { d.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_RIG },
		func(d *discovery.DiscoveredDevice) { d.DriverName = "other" },
		func(d *discovery.DiscoveredDevice) { d.Model = "R2" },
		func(d *discovery.DiscoveredDevice) { d.Manufacturer = "other" },
		func(d *discovery.DiscoveredDevice) { d.IpAddress = "10.0.0.13" },
		func(d *discovery.DiscoveredDevice) { d.Model = "ct1"; d.Manufacturer = "proto" },
	} {
		d := &discovery.DiscoveredDevice{OrgID: org, Device: pb.Device{DeviceIdentifier: fmt.Sprintf("container-%d", i), DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", IpAddress: "10.0.0.12", Port: "8181", UrlScheme: "http"}}
		modify(d)
		_, err = store.SaveDirect(ctx, d.GetDeviceOrgIdentifier(), d)
		require.NoError(t, err)
	}
	got, err := store.GetDirectContainerByIP(ctx, org, "10.0.0.12", "proto", "CT1", "Proto")
	require.NoError(t, err)
	require.Equal(t, "container-6", got.DeviceIdentifier)
	require.NoError(t, store.SoftDelete(ctx, got.GetDeviceOrgIdentifier()))
	_, err = store.GetDirectContainerByIP(ctx, org, "10.0.0.12", "proto", "CT1", "Proto")
	require.Error(t, err, "deleted containers must not be reused")
}

func TestDiscoveredDeviceRPCEndpointRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	org, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "rpc-endpoint-org", Name: "RPC endpoints"})
	require.NoError(t, err)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	id := discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "serial:rpc-controller"}
	input := &discovery.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: id.DeviceIdentifier, DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", IpAddress: "10.0.0.12", Port: "8181", UrlScheme: "http", RpcPort: 9443, RpcTls: true, SerialNumber: "rpc-controller-serial", MacAddress: "02:11:22:33:44:55"}, OrgID: org}
	_, err = store.SaveDirect(ctx, id, input)
	require.NoError(t, err)
	row, err := q.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: id.DeviceIdentifier})
	require.NoError(t, err)
	require.Equal(t, int32(9443), row.RpcPort)
	require.True(t, row.RpcTls)
	require.Equal(t, input.SerialNumber, row.SerialNumber)
	require.Equal(t, input.MacAddress, row.MacAddress)
	require.Equal(t, "8181", row.Port)
	require.Equal(t, "http", row.UrlScheme)
	for _, class := range []commonpb.DeviceClass{commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, commonpb.DeviceClass_DEVICE_CLASS_RIG, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER} {
		t.Run(class.String(), func(t *testing.T) {
			incomplete := &discovery.DiscoveredDevice{OrgID: org}
			proto.Merge(&incomplete.Device, &input.Device)
			incomplete.DeviceClass, incomplete.RpcPort, incomplete.RpcTls = class, 0, false
			_, err := store.SaveDirect(ctx, id, incomplete)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			retained, err := q.GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{OrgID: org, ID: row.ID})
			require.NoError(t, err)
			require.Equal(t, row, retained, "rejected web scans must preserve native transport and identity")
		})
	}
	input.Port = "8080" // discovery default must not replace the stored custom web port
	input.UrlScheme = "https"
	input.RpcPort = 9095
	input.RpcTls = false
	_, err = store.SaveDirect(ctx, id, input)
	require.NoError(t, err)
	updated, err := q.GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{OrgID: org, ID: row.ID})
	require.NoError(t, err)
	require.Equal(t, int32(9095), updated.RpcPort)
	require.False(t, updated.RpcTls)
	require.Equal(t, input.SerialNumber, updated.SerialNumber)
	require.Equal(t, input.MacAddress, updated.MacAddress)
	require.Equal(t, "8181", updated.Port)
	require.Equal(t, "http", updated.UrlScheme)
	require.Equal(t, row.ID, updated.ID, "rediscovery must not create a second controller")
}
