package alerts

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	alertsv1 "github.com/block/proto-fleet/server/generated/grpc/alerts/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Channel payloads carry device identity (miner data), so create/update must require miner:read
// in addition to alert:manage; alert:manage alone must be rejected before the service is touched.
func TestCreateChannelRequiresMinerRead(t *testing.T) {
	h := NewHandler(nil, nil) // svc is never reached: the permission check precedes it.

	_, err := h.CreateChannel(ctxWithPerms(authz.PermAlertManage),
		connect.NewRequest(&alertsv1.CreateChannelRequest{Name: "x", Kind: alertsv1.ChannelKind_CHANNEL_KIND_SLACK}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

func TestUpdateChannelRequiresMinerRead(t *testing.T) {
	h := NewHandler(nil, nil)

	_, err := h.UpdateChannel(ctxWithPerms(authz.PermAlertManage),
		connect.NewRequest(&alertsv1.UpdateChannelRequest{Id: "1", Name: "x", Kind: alertsv1.ChannelKind_CHANNEL_KIND_SLACK}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}
