package collection

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestLegacyChannelSurfaceUsesChannelPermissions(t *testing.T) {
	t.Parallel()

	rackRead := collectionPermissionContext(t, authz.PermRackRead)
	_, err := requireCollectionListRead(
		rackRead,
		pb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	requirePermissionDenied(t, err)

	channelRead := collectionPermissionContext(t, authz.PermChannelRead)
	_, err = requireCollectionListRead(
		channelRead,
		pb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	require.NoError(t, err)

	handler := NewHandler(nil)
	rackManage := collectionPermissionContext(t, authz.PermRackManage)
	_, err = handler.CreateCollection(
		rackManage,
		connect.NewRequest(&pb.CreateCollectionRequest{
			Type: pb.CollectionType_COLLECTION_TYPE_CHANNEL,
		}),
	)
	requirePermissionDenied(t, err)

	assert.Equal(
		t,
		authz.PermChannelRead,
		collectionReadPermission(pb.CollectionType_COLLECTION_TYPE_CHANNEL),
	)
	assert.Equal(
		t,
		authz.PermChannelManage,
		collectionManagePermission(pb.CollectionType_COLLECTION_TYPE_CHANNEL),
	)
}

func collectionPermissionContext(t *testing.T, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		OrganizationID: 42,
		UserID:         9,
	})
	return middleware.WithEffectivePermissions(
		ctx,
		authz.NewEffectivePermissions([]authz.Assignment{{
			AssignmentID: 1,
			ScopeType:    authz.ScopeOrg,
			Permissions:  permissions,
		}}),
	)
}

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}
