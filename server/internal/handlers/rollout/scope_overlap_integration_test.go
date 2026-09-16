package rollout

import (
	"os"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestScopeOverlapReturnsStructuredReasonFromRealDomain(t *testing.T) {
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("scope overlap integration tests need a database (DB_PASSWORD)")
	}
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			miner := database.CreateDevice(user.OrganizationID, "proto")
			queries := sqlstores.NewSQLConnectionManager(database.DB)
			svc := domain.NewService(&queries, sqlstores.NewSQLTransactor(database.DB), nil, nil, nil)
			h := NewHandler(svc)
			ctx := authn.SetInfo(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate), &session.Info{
				OrganizationID: user.OrganizationID, UserID: user.DatabaseID, Username: user.Username, AuthMethod: session.AuthMethodSession,
			})
			scope := &pb.ReleaseChannelScope{DeviceIdentifiers: []string{miner.ID}}
			_, err = h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{Name: "Owner", Scope: scope}))
			require.NoError(t, err)
			var editedID int64
			if operation == "update" {
				created, err := h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{Name: "Empty channel"}))
				require.NoError(t, err)
				editedID = created.Msg.Channel.Id
			}
			before, _, err := svc.ListChannels(ctx, user.OrganizationID, 100, "")
			require.NoError(t, err)

			if operation == "create" {
				_, err = h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{Name: "Conflicting channel", Scope: scope}))
			} else {
				_, err = h.UpdateReleaseChannel(ctx, connect.NewRequest(&pb.UpdateReleaseChannelRequest{
					ChannelId: editedID, Name: "Rejected edit", Description: "Must not persist", Scope: scope,
				}))
			}
			require.ErrorContains(t, err, "scope overlaps release channel Owner (1 miners)")
			var rpcError *connect.Error
			require.ErrorAs(t, err, &rpcError)
			require.Equal(t, connect.CodeFailedPrecondition, rpcError.Code())
			var reasons []*pb.RolloutErrorInfo
			for _, detail := range rpcError.Details() {
				message, err := detail.Value()
				require.NoError(t, err)
				if info, ok := message.(*pb.RolloutErrorInfo); ok {
					reasons = append(reasons, info)
				}
			}
			require.Len(t, reasons, 1, "clients need a structured reason, not message parsing")
			require.Equal(t, pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_SCOPE_OVERLAP, reasons[0].Reason)

			after, _, err := svc.ListChannels(ctx, user.OrganizationID, 100, "")
			require.NoError(t, err)
			require.Equal(t, before, after, "rejection must preserve channel names, scopes and counts")
		})
	}
}
