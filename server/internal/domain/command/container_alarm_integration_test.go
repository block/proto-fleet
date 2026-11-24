package command_test

import (
	"context"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPlantAlarmActionAdmissionRequiresFleetNodeAlarmProtocol(t *testing.T) {
	for _, tc := range []struct {
		name    string
		remote  bool
		version gatewaypb.CommandProtocolVersion
		allowed bool
	}{
		{name: "remote below alarm protocol requires upgrade", remote: true, version: remotenode.ContainerAlarmCommandProtocolVersion - 1},
		{name: "remote at alarm protocol supported", remote: true, version: remotenode.ContainerAlarmCommandProtocolVersion, allowed: true},
		{name: "local supported", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			identifier, deviceID, _ := seedTankController(t, conn, dbs, user.OrganizationID)
			_, err := conn.ExecContext(t.Context(), `INSERT INTO container_controls_observation(device_id,observed_at,supported,alarm_mute_supported,alarm_reset_supported) VALUES($1,now(),true,true,false)`, deviceID)
			require.NoError(t, err)
			registry := control.NewRegistry()
			if tc.remote {
				nodeID := bindTankControllerToFleetNode(t, conn, user.OrganizationID, deviceID)
				stream, err := registry.RegisterAuthenticated(nodeID, "session", tc.version)
				require.NoError(t, err)
				defer stream.Unregister()
			}
			svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
			svc.SetCommandProtocolChecker(registry)
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64) error { return nil }
			result, err := svc.RunContainerAlarmAction(ctx, identifier, sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}, allow)
			var queued int
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE command_type='ContainerAlarmAction'`).Scan(&queued))
			if !tc.allowed {
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				require.ErrorContains(t, err, "upgrade Fleet Node before retrying")
				require.Zero(t, queued)
				require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, queued)
			require.Equal(t, 1, countCommandBatches(t, conn, user.OrganizationID))

			status, err := svc.GetContainerControlResult(ctx, identifier, result.BatchIdentifier, allow)
			require.NoError(t, err)
			require.Equal(t, int32(1), status.TotalCount)
		})
	}
}

func TestPlantAlarmActionAuthorizationAndTargetBoundaries(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	identifier, deviceID, _ := seedTankController(t, conn, dbs, user.OrganizationID)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO container_controls_observation(device_id,observed_at,supported,alarm_mute_supported,alarm_reset_supported) VALUES($1,now(),true,true,true)`, deviceID)
	require.NoError(t, err)
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	mute := sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute}
	allow := func(context.Context, *int64) error { return nil }

	_, err = svc.RunContainerAlarmAction(ctx, identifier, mute, func(context.Context, *int64) error { return fleeterror.NewForbiddenError("denied") })
	require.True(t, fleeterror.IsForbiddenError(err))
	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.RunContainerAlarmAction(foreign, identifier, mute, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	_, err = svc.RunContainerAlarmAction(ctx, "missing-controller", mute, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	miner := dbs.CreateDevice(user.OrganizationID, "proto")
	_, err = svc.RunContainerAlarmAction(ctx, miner.ID, mute, allow)
	require.True(t, fleeterror.IsNotFoundError(err), "a miner must not accept plant alarm actions")
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))

	result, err := svc.RunContainerAlarmAction(ctx, identifier, mute, allow)
	require.NoError(t, err)
	_, err = svc.GetContainerControlResult(foreign, identifier, result.BatchIdentifier, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	_, err = svc.GetContainerControlResult(ctx, identifier, result.BatchIdentifier, func(context.Context, *int64) error { return fleeterror.NewForbiddenError("denied") })
	require.Error(t, err)
}
