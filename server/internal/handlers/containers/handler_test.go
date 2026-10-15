package containers_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	inventorypb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	inventorydomain "github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/handlers/containers"
	inventoryhandler "github.com/block/proto-fleet/server/internal/handlers/fleetinventory"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestGetContainerCurrentSite(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	q := sqlc.New(db)
	orgID, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "overview", Name: "Overview"})
	require.NoError(t, err)
	site, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: orgID, Name: "North", Slug: "north"})
	require.NoError(t, err)
	other, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: orgID, Name: "South", Slug: "south"})
	require.NoError(t, err)
	device := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: orgID, IsActive: true}
	_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: orgID, DeviceIdentifier: "ct1"}, device)
	require.NoError(t, err)
	devices := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, devices.InsertDevice(ctx, &device.Device, orgID, "ct1"))
	require.NoError(t, devices.UpsertDevicePairing(ctx, &device.Device, orgID, "PAIRED"))
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE org_id=$2 AND device_identifier='ct1'`, site.ID, orgID)
	require.NoError(t, err)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, store.StoreDeviceMetrics(ctx, telemetry.DeviceMetrics{DeviceIdentifier: "ct1", Timestamp: now, Container: &sdk.ContainerSnapshot{ObservedAt: now, Fans: []sdk.ContainerFan{{ComponentID: "fan-1", ObservedAt: now, SpeedPercent: new(25.0)}}, Controls: &sdk.ContainerControls{ObservedAt: now, Supported: new(true), Controls: []sdk.ContainerControl{{Name: "dry_cooler", Enabled: new(true)}}}}}))
	handler := containers.NewHandler(domain.NewService(sqlstores.NewSQLContainerStore(db)))
	inventory := inventoryhandler.NewHandler(inventorydomain.NewService(q))
	assertInventoryCanAssign := func(t *testing.T, caller context.Context, want bool) {
		t.Helper()
		response, err := inventory.ListInventory(caller, connect.NewRequest(&inventorypb.ListInventoryRequest{
			View:  inventorypb.InventoryView_INVENTORY_VIEW_FACILITIES,
			Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_CONTAINER},
		}))
		require.NoError(t, err)
		require.Len(t, response.Msg.Items, 1)
		container := response.Msg.Items[0].GetContainer()
		require.NotNil(t, container)
		require.Equal(t, want, container.CanAssignSite)
	}
	withPermissions := func(permissions []string) context.Context {
		authenticated := authn.SetInfo(ctx, &session.Info{UserID: 1, OrganizationID: orgID})
		return middleware.WithEffectivePermissions(authenticated, authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site.ID, Permissions: permissions}}))
	}
	read := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead}
	request := connect.NewRequest(&pb.GetContainerRequest{DeviceIdentifier: "ct1"})
	response, err := handler.GetContainer(withPermissions(read), request)
	require.NoError(t, err)
	require.Equal(t, site.ID, response.Msg.Container.Site.Id)
	require.Positive(t, response.Msg.Container.Id)
	require.Len(t, response.Msg.Container.Fans, 1)
	require.Equal(t, 25.0, *response.Msg.Container.Fans[0].SpeedPercent)
	require.Equal(t, true, *response.Msg.Container.Controls.Controls[0].Enabled)
	for _, tc := range []struct {
		name        string
		assignments []authz.Assignment
		canAssign   bool
	}{
		{name: "read only"},
		{name: "current site manager", assignments: []authz.Assignment{handlerstest.SiteAssignment(site.ID, append(append([]string{}, read...), authz.PermSiteManage)...)}, canAssign: true},
		{name: "other site manager", assignments: []authz.Assignment{handlerstest.SiteAssignment(other.ID, authz.PermSiteManage)}},
		{name: "org manager", assignments: []authz.Assignment{handlerstest.OrgAssignment(authz.PermSiteManage)}, canAssign: true},
		{name: "site override", assignments: []authz.Assignment{handlerstest.OrgAssignment(authz.PermSiteManage), handlerstest.SiteAssignment(site.ID, read...)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assignments := append([]authz.Assignment{handlerstest.OrgAssignment(read...)}, tc.assignments...)
			caller := handlerstest.CtxWithAssignments(t, orgID, assignments...)
			detail, err := handler.GetContainer(caller, request)
			require.NoError(t, err)
			require.Equal(t, tc.canAssign, detail.Msg.Container.CanAssignSite)
			list, err := handler.ListContainers(caller, connect.NewRequest(&pb.ListContainersRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Containers, 1)
			require.Equal(t, tc.canAssign, list.Msg.Containers[0].CanAssignSite)
			assertInventoryCanAssign(t, caller, tc.canAssign)
		})
	}
	for i := range read {
		incomplete := append(append([]string{}, read[:i]...), read[i+1:]...)
		_, err = handler.GetContainer(withPermissions(incomplete), request)
		require.True(t, fleeterror.IsNotFoundError(err), "incomplete permissions must read as not found, got %v", err)
	}
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE org_id=$2 AND device_identifier='ct1'`, other.ID, orgID)
	require.NoError(t, err)
	_, err = handler.GetContainer(withPermissions(read), request)
	require.True(t, fleeterror.IsNotFoundError(err), "authorization must follow current placement after a move, got %v", err)
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=NULL WHERE org_id=$1 AND device_identifier='ct1'`, orgID)
	require.NoError(t, err)
	for _, assignment := range []authz.Assignment{
		handlerstest.SiteAssignment(site.ID, authz.PermSiteManage),
		handlerstest.OrgAssignment(authz.PermSiteManage),
	} {
		caller := handlerstest.CtxWithAssignments(t, orgID, handlerstest.OrgAssignment(read...), assignment)
		response, err := handler.GetContainer(caller, request)
		require.NoError(t, err)
		require.Equal(t, assignment.ScopeType == authz.ScopeOrg, response.Msg.Container.CanAssignSite, "unassigned containers require org-wide authority")
		assertInventoryCanAssign(t, caller, assignment.ScopeType == authz.ScopeOrg)
	}
}
