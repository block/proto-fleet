package fleetinventory_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	containerdomain "github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	containerhandler "github.com/block/proto-fleet/server/internal/handlers/containers"
	"github.com/block/proto-fleet/server/internal/handlers/fleetinventory"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestContainerReadIncludesChildrenWithoutRackPermission(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	orgID, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "child-access", Name: "Child access"})
	require.NoError(t, err)
	site, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: orgID, Name: "North", Slug: "north"})
	require.NoError(t, err)
	other, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: orgID, Name: "South", Slug: "south"})
	require.NoError(t, err)
	d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: orgID, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: orgID, DeviceIdentifier: d.DeviceIdentifier}, d)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &d.Device, orgID, d.DeviceIdentifier))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, orgID, "PAIRED"))
	_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE device_identifier=$2", site.ID, d.DeviceIdentifier)
	require.NoError(t, err)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hashrate := 300e12
	require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: now, Container: &sdk.ContainerSnapshot{
		ObservedAt: now, TopologyComplete: true, Tanks: []sdk.ContainerTank{
			{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, State: sdk.ModuleHashing, ObservedAt: now, Metrics: &sdk.ContainerModuleMetrics{ObservedAt: now, HashrateHS: &hashrate}}}},
			{Position: 2},
		},
	}}))
	var rackID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO device_set (org_id,type,label) VALUES ($1,'rack','Rack') RETURNING id", orgID).Scan(&rackID))
	_, err = db.ExecContext(ctx, "INSERT INTO device_set_rack (device_set_id,org_id,site_id,rows,columns) VALUES ($1,$2,$3,1,1)", rackID, orgID, site.ID)
	require.NoError(t, err)
	withPermissions := func(permissions []string, permissionSite int64) context.Context {
		authenticated := authn.SetInfo(ctx, &session.Info{UserID: 1, OrganizationID: orgID})
		return middleware.WithEffectivePermissions(authenticated, authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &permissionSite, Permissions: permissions}}))
	}
	read := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead}
	authorized := withPermissions(read, site.ID)
	handler := fleetinventory.NewHandler(domain.NewService(q))
	containers := containerhandler.NewHandler(containerdomain.NewService(sqlstores.NewSQLContainerStore(db)))
	container, err := containers.GetContainer(authorized, connect.NewRequest(&containerpb.GetContainerRequest{DeviceIdentifier: d.DeviceIdentifier}))
	require.NoError(t, err)

	for _, kinds := range [][]pb.EntityKind{nil, {pb.EntityKind_ENTITY_KIND_TANK}, {pb.EntityKind_ENTITY_KIND_RACK, pb.EntityKind_ENTITY_KIND_TANK}} {
		request := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: kinds, PageSize: 1}
		var positions []int32
		for {
			response, err := handler.ListInventory(authorized, connect.NewRequest(request))
			require.NoError(t, err)
			require.EqualValues(t, 2, response.Msg.TotalCount, "rack permission must not restrict tank counts")
			require.Len(t, response.Msg.Items, 1)
			item := response.Msg.Items[0]
			require.Equal(t, pb.EntityKind_ENTITY_KIND_TANK, item.Ref.Kind, "racks must remain hidden")
			positions = append(positions, item.Position)
			if item.Position == 1 {
				require.NotNil(t, item.HashrateThs)
				require.Equal(t, 300.0, *item.HashrateThs, "tank telemetry uses container permissions")
			}
			request.PageToken = response.Msg.NextPageToken
			if request.PageToken == "" {
				break
			}
			require.Less(t, len(positions), 2)
		}
		require.Equal(t, []int32{1, 2}, positions)
	}
	for _, tt := range []struct {
		permissions []string
		kinds       []pb.EntityKind
		count       int64
	}{
		{read, []pb.EntityKind{pb.EntityKind_ENTITY_KIND_RACK}, 0},
		{append(append([]string{}, read...), authz.PermRackRead), nil, 3},
	} {
		response, err := handler.ListInventory(withPermissions(tt.permissions, site.ID), connect.NewRequest(&pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, Kinds: tt.kinds}))
		require.NoError(t, err)
		require.Equal(t, tt.count, response.Msg.TotalCount)
	}
	children := &pb.ListInventoryRequest{View: pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Kinds: []pb.EntityKind{pb.EntityKind_ENTITY_KIND_MODULE}, Ancestors: []*pb.EntityRef{{Kind: pb.EntityKind_ENTITY_KIND_CONTAINER, Id: container.Msg.Container.Id}}}
	modules, err := handler.ListInventory(authorized, connect.NewRequest(children))
	require.NoError(t, err)
	require.Len(t, modules.Msg.Items, 1)
	historyRequest := connect.NewRequest(&pb.ListModuleHistoryRequest{ModuleId: modules.Msg.Items[0].Ref.Id, StartTime: timestamppb.New(now.Add(-time.Minute)), EndTime: timestamppb.New(now.Add(time.Minute))})
	history, err := handler.ListModuleHistory(authorized, historyRequest)
	require.NoError(t, err)
	require.Len(t, history.Msg.Samples, 1)

	for _, view := range []pb.InventoryView{pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS} {
		response, err := handler.ListInventory(withPermissions(read, other.ID), connect.NewRequest(&pb.ListInventoryRequest{View: view}))
		require.NoError(t, err)
		require.Zero(t, response.Msg.TotalCount)
		for i := range read {
			incomplete := append(append([]string{}, read[:i]...), read[i+1:]...)
			response, err = handler.ListInventory(withPermissions(incomplete, site.ID), connect.NewRequest(&pb.ListInventoryRequest{View: view}))
			require.NoError(t, err)
			require.Zero(t, response.Msg.TotalCount, "all container read permissions remain required")
		}
	}
	_, err = db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE device_identifier=$2", other.ID, d.DeviceIdentifier)
	require.NoError(t, err)
	for _, view := range []pb.InventoryView{pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS, pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS} {
		response, err := handler.ListInventory(authorized, connect.NewRequest(&pb.ListInventoryRequest{View: view}))
		require.NoError(t, err)
		require.Zero(t, response.Msg.TotalCount, "child permissions must follow the container after a site move")
	}
	_, err = handler.ListModuleHistory(authorized, historyRequest)
	require.True(t, fleeterror.IsForbiddenError(err), "history must follow the container's current site, got %v", err)
}
