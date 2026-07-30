package updates

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	instancev1 "github.com/block/proto-fleet/server/generated/grpc/instance/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// Update status and channel mutation are gated on org-wide
// instance:update before the service is touched (svc is nil).
func TestGetUpdateStatusRequiresInstanceUpdate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermMinerRead)

	_, err := h.GetUpdateStatus(ctx, connect.NewRequest(&instancev1.GetUpdateStatusRequest{}))
	requirePermissionDenied(t, err)
}

func TestSetReleaseChannelRequiresInstanceUpdate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermMinerRead)

	_, err := h.SetReleaseChannel(ctx, connect.NewRequest(&instancev1.SetReleaseChannelRequest{
		Channel: instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE,
	}))
	requirePermissionDenied(t, err)
}

// instance:update narrowed to a site scope must not satisfy the org-wide gate.
func TestUpdateStatusRejectsSiteScopedPermission(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	ctx := handlerstest.CtxWithAssignments(t, 1, handlerstest.SiteAssignment(42, authz.PermInstanceUpdate))

	_, err := h.GetUpdateStatus(ctx, connect.NewRequest(&instancev1.GetUpdateStatusRequest{}))
	requirePermissionDenied(t, err)
}

// RELEASE_CHANNEL_UNSPECIFIED is rejected as invalid argument in the handler,
// before the service is touched (svc is nil).
func TestSetReleaseChannelRejectsUnspecified(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermInstanceUpdate)

	_, err := h.SetReleaseChannel(ctx, connect.NewRequest(&instancev1.SetReleaseChannelRequest{}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
