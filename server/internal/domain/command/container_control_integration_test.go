package command_test

import (
	"context"
	"sync"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestEquipmentDispatchAuthorizesControllerAndRejectsConcurrentDuplicates(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	device := dbs.CreateDevice(user.OrganizationID, "proto")
	_, err := conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2,model='CT1' WHERE device_identifier=$1`, device.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_controls_observation(device_id,observed_at,supported) VALUES($1,now(),true)`, device.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_control_observation(device_id,name,enabled) VALUES($1,'dry_cooler',false)`, device.DatabaseID)
	require.NoError(t, err)
	realQueue := queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 3}, conn)
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: realQueue})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	req := sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(true)}
	denied := func(context.Context, *int64) error { return fleeterror.NewForbiddenError("denied") }
	_, err = svc.SetContainerControl(ctx, device.ID, req, denied)
	require.Error(t, err)
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
	allow := func(_ context.Context, site *int64) error { require.Nil(t, site); return nil }
	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.SetContainerControl(foreign, device.ID, req, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	_, err = svc.SetContainerControl(ctx, "missing-controller", req, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := svc.SetContainerControl(ctx, device.ID, req, allow); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorContains(t, err, "already pending")
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, countCommandBatches(t, conn, user.OrganizationID))
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE device_id=$1 AND command_type='SetContainerControl'`, device.DatabaseID).Scan(&count))
	require.Equal(t, 1, count)

	var batch string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT command_batch_log_uuid FROM queue_message WHERE device_id=$1`, device.DatabaseID).Scan(&batch))
	result, err := svc.GetContainerControlResult(ctx, device.ID, batch, allow)
	require.NoError(t, err)
	require.Equal(t, int32(1), result.TotalCount)
	_, err = svc.GetContainerControlResult(ctx, device.ID, batch, denied)
	require.Error(t, err)
	_, err = svc.GetContainerControlResult(foreign, device.ID, batch, allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	other := dbs.CreateDevice(user.OrganizationID, "proto")
	_, err = svc.SetContainerControl(ctx, other.ID, req, allow)
	require.True(t, fleeterror.IsNotFoundError(err), "a miner must not accept equipment commands")
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, other.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, other.DatabaseID)
	require.NoError(t, err)
	_, err = svc.GetContainerControlResult(ctx, other.ID, batch, allow)
	require.True(t, fleeterror.IsNotFoundError(err), "another controller cannot expose this batch")
}
