package command_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerMiningDispatchChecksOwnershipAuthorizationAndDuplicates(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	q := sqlc.New(conn)
	device := dbs.CreateDevice(user.OrganizationID, "proto")
	_, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{OrgID: user.OrganizationID, DeviceIdentifier: device.ID, DriverName: "proto", DeviceClass: 2, IpAddress: "10.0.0.10", Port: "8080", UrlScheme: "http", IsActive: true})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	realQueue := queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 3}, conn)
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: realQueue})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	req := sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}
	denied := func(context.Context, *int64, sdk.ContainerMiningRequest) error {
		return fleeterror.NewForbiddenError("denied")
	}
	allow := func(_ context.Context, site *int64, r sdk.ContainerMiningRequest) error {
		require.Nil(t, site)
		require.Equal(t, req, r)
		return nil
	}
	_, err = svc.SetContainerMining(ctx, device.ID, req, denied)
	require.True(t, fleeterror.IsForbiddenError(err))
	_, err = svc.ReadContainerMining(ctx, device.ID, sdk.ContainerMiningRequest{Action: "status"}, denied)
	require.True(t, fleeterror.IsForbiddenError(err))
	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.SetContainerMining(foreign, device.ID, req, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	_, err = svc.ReadContainerMining(foreign, device.ID, sdk.ContainerMiningRequest{Action: "status"}, denied)
	require.True(t, fleeterror.IsNotFoundError(err))
	result, err := svc.SetContainerMining(ctx, device.ID, req, allow)
	require.NoError(t, err)
	require.NotEmpty(t, result.BatchIdentifier)
	_, err = svc.SetContainerMining(ctx, device.ID, req, allow)
	require.ErrorContains(t, err, "already pending")
	batch, err := svc.GetContainerControlResult(ctx, device.ID, result.BatchIdentifier, func(context.Context, *int64) error { return nil })
	require.NoError(t, err)
	require.EqualValues(t, 1, batch.TotalCount)
	_, err = svc.SetContainerMining(ctx, device.ID, sdk.ContainerMiningRequest{Action: "status"}, allow)
	require.Error(t, err)
	_, err = svc.ReadContainerMining(ctx, device.ID, req, allow)
	require.Error(t, err)
}
