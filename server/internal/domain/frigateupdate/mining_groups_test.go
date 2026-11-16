package frigateupdate

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func (e *env) disableMiningGroupsPending(t *testing.T, state string) {
	t.Helper()
	require.NoError(t, sqlc.New(e.conn).EnsureMiningGroupState(e.ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: e.orgID, DeviceID: e.device.DatabaseID}))
	_, err := e.conn.ExecContext(e.ctx, `UPDATE container_mining_group_state SET operation_kind='deactivate',status=$2 WHERE device_id=$1`, e.device.DatabaseID, state)
	require.NoError(t, err)
}

func TestStartUpdateRejectsMiningGroupDeactivation(t *testing.T) {
	for _, state := range []string{"pending", "applying", "blocked"} {
		t.Run(state, func(t *testing.T) {
			e := newEnv(t)
			e.disableMiningGroupsPending(t, state)
			_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "new-update"})
			require.ErrorContains(t, err, "being disabled")
			var count int
			require.NoError(t, e.conn.QueryRowContext(e.ctx, `SELECT count(*) FROM frigate_update_job WHERE device_id=$1`, e.device.DatabaseID).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestStartUpdateRechecksModeAfterAdmissionWait(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, sqlc.New(e.conn).EnsureMiningGroupState(e.ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: e.orgID, DeviceID: e.device.DatabaseID}))
	require.NoError(t, sqlc.New(e.conn).EnsureDeviceWriteFences(e.ctx, []int64{e.device.DatabaseID}))
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	admission, err := e.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer admission.Rollback()
	_, err = sqlc.New(admission).LockDeviceWriteFences(ctx, []int64{e.device.DatabaseID})
	require.NoError(t, err)
	var blocker int
	require.NoError(t, admission.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.StartUpdate(ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "new-update"})
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := e.conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = admission.ExecContext(ctx, `UPDATE container_mining_group_state SET operation_kind='deactivate',status='pending' WHERE device_id=$1`, e.device.DatabaseID)
	require.NoError(t, err)
	require.NoError(t, admission.Commit())
	select {
	case err := <-done:
		require.ErrorContains(t, err, "being disabled")
	case <-ctx.Done():
		t.Fatal("update admission did not finish")
	}
}

func TestStartUpdateWaitsForAllocationBeforeLockingFence(t *testing.T) {
	testStartUpdateWaitsForDeviceBeforeLockingFence(t, false)
}

func TestStartUpdateWaitsForPoolReadinessBeforeLockingFence(t *testing.T) {
	testStartUpdateWaitsForDeviceBeforeLockingFence(t, true)
}

func testStartUpdateWaitsForDeviceBeforeLockingFence(t *testing.T, readiness bool) {
	t.Helper()
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	q := sqlc.New(e.conn)
	require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{e.device.DatabaseID}))
	allocation, err := e.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer allocation.Rollback()
	locked := sqlc.New(allocation)
	if readiness {
		_, err = locked.LockPoolEquipmentOrganization(ctx, e.orgID)
	} else {
		_, err = locked.LockHostedAccountOrganization(ctx, e.orgID)
	}
	require.NoError(t, err)
	_, err = locked.LockMinerBindingDevice(ctx, e.device.DatabaseID)
	require.NoError(t, err)
	var blocker int
	require.NoError(t, allocation.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.StartUpdate(ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "after-allocation"})
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := e.conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	// Allocation already holds the device. Update admission must not hold the
	// fence while it waits, or the two transactions form a lock cycle.
	_, err = allocation.ExecContext(ctx, `SET LOCAL lock_timeout='250ms'`)
	require.NoError(t, err)
	_, err = locked.LockDeviceWriteFences(ctx, []int64{e.device.DatabaseID})
	require.NoError(t, err)
	require.NoError(t, allocation.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("update admission did not finish after allocation")
	}
}

func TestExistingFrigateUpdateRecoveryRemainsAvailableDuringDeactivation(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("existing-update"))
	e.disableMiningGroupsPending(t, "blocked")
	replayed := e.start("existing-update")
	require.Equal(t, job.Row.ID, replayed.Row.ID)
	_, err := e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET state='failed',retryable=true,cancellable=true WHERE id=$1`, job.Row.ID)
	require.NoError(t, err)
	_, err = e.svc.RetryUpdate(e.ctx, job.Row.ID, "retry-existing")
	require.NoError(t, err)
	e.updater.cancelHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		rollout := *e.updater.rollout
		rollout.Phase = sdk.FrigatePhaseCancelled
		rollout.Cancellable = false
		return rollout
	}
	cancelled, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "cancel-existing")
	require.NoError(t, err)
	require.Equal(t, StateCancelled, cancelled.Row.State)
	require.True(t, e.svc.cleanupJob(e.ctx, cancelled.Row))
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	require.Len(t, e.updater.retryCalls, 1)
	require.Len(t, e.updater.cancelCalls, 1)
	require.NotEmpty(t, e.updater.aborts)
}
