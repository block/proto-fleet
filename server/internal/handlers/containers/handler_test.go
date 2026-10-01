package containers_test

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/containers"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
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
	handler := containers.NewHandler(domain.NewService(sqlstores.NewSQLContainerStore(db)))
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
	for i := range read {
		incomplete := append(append([]string{}, read[:i]...), read[i+1:]...)
		_, err = handler.GetContainer(withPermissions(incomplete), request)
		require.True(t, fleeterror.IsNotFoundError(err), "incomplete permissions must read as not found, got %v", err)
	}
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE org_id=$2 AND device_identifier='ct1'`, other.ID, orgID)
	require.NoError(t, err)
	_, err = handler.GetContainer(withPermissions(read), request)
	require.True(t, fleeterror.IsNotFoundError(err), "authorization must follow current placement after a move, got %v", err)
}
