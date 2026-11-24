package frigateupdate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// removeContainer runs the same retirement transaction as removing the
// container from the fleet.
func (e *env) removeContainer() (int64, error) {
	e.t.Helper()
	return sqlstores.NewSQLDeviceStore(e.conn).SoftDeleteDevices(e.ctx, []string{e.device.ID}, e.orgID)
}

func (e *env) requireContainerLive() {
	e.t.Helper()
	var live bool
	require.NoError(e.t, e.conn.QueryRowContext(e.ctx, `SELECT deleted_at IS NULL FROM device WHERE id=$1`, e.device.DatabaseID).Scan(&live))
	require.True(e.t, live, "the container must stay in the fleet")
}

func TestRetirement_RefusedWhileARolloutIsRunning(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))

	_, err := e.removeContainer()
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "software update")
	e.requireContainerLive()

	// The job can still report on the rollout and drive it to completion.
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseSucceeded })
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateSucceeded, e.job(job.Row.ID).Row.State)

	deleted, err := e.removeContainer()
	require.NoError(t, err, "a finished update no longer holds the container")
	assert.EqualValues(t, 1, deleted)
}

func TestRetirement_RefusedWhileAFailedRolloutCanBeRetried(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase, r.Retryable, r.DisruptionStarted, r.Cancellable = sdk.FrigatePhaseFailed, true, true, true
	})
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateFailed, e.job(job.Row.ID).Row.State)

	_, err := e.removeContainer()
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	e.requireContainerLive()

	// Recovery stays available: the operator can still retry the stopped controllers.
	e.updater.retryHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Retryable = sdk.FrigatePhaseInstalling, false
		ro.UpdatedAt = ro.UpdatedAt.Add(1)
		return ro
	}
	got, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateRunning, got.Row.State)
}

func TestRetirement_RefusedWhileStaging(t *testing.T) {
	e := newEnv(t)
	e.start("key-1")

	_, err := e.removeContainer()
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	e.requireContainerLive()
}

func TestRetirement_AllowedOnceTheUpdateIsCancelled(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	require.Equal(t, StateCancelled, got.Row.State)

	deleted, err := e.removeContainer()
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	// A removed container cannot start a new update, so its controllers can be
	// re-paired as a new device without an old job still addressing them.
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.Error(t, err)
}

func TestRunner_AbandonsAJobWhoseControllersStayUnreachable(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.getter.mu.Lock()
	e.getter.err = fleeterror.NewNotFoundErrorf("device not found")
	e.getter.mu.Unlock()

	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	require.Equal(t, StateRunning, got.Row.State)
	require.True(t, got.Row.UnreachableSince.Valid)
	_, err := e.removeContainer()
	require.Error(t, err, "a briefly unreachable rollout still holds the container")

	e.clock.advance(unreachableGiveUp - time.Minute)
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the deadline has not passed yet")

	e.clock.advance(2 * time.Minute)
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateAbandoned, got.Row.State)
	assert.Equal(t, CodeControllersLost, got.Row.ErrorCode)
	assert.False(t, got.Row.Retryable)
	assert.False(t, got.Row.Cancellable)

	deleted, err := e.removeContainer()
	require.NoError(t, err, "an abandoned job no longer holds the container")
	assert.EqualValues(t, 1, deleted)
}

func TestRunner_ContactResetsTheUnreachableDeadline(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	setErr := func(err error) {
		e.getter.mu.Lock()
		e.getter.err = err
		e.getter.mu.Unlock()
	}
	setErr(fleeterror.NewUnavailableErrorf("controller offline"))
	e.svc.runOnce(e.ctx)
	e.clock.advance(unreachableGiveUp - time.Minute)

	setErr(nil)
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.UpdatedAt = r.UpdatedAt.Add(time.Second) })
	e.svc.runOnce(e.ctx)
	require.False(t, e.job(job.Row.ID).Row.UnreachableSince.Valid, "an answer clears the deadline")

	setErr(fleeterror.NewUnavailableErrorf("controller offline"))
	e.clock.advance(2 * time.Minute)
	e.svc.runOnce(e.ctx)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the deadline restarts from the last answer")
}

func TestCleanup_ARemovedContainerEndsTheSweep(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	_, err = e.removeContainer()
	require.NoError(t, err)
	e.getter.mu.Lock()
	e.getter.err = fleeterror.NewNotFoundErrorf("device not found")
	e.getter.mu.Unlock()

	row, err := sqlc.New(e.conn).GetFrigateUpdateJob(e.ctx, sqlc.GetFrigateUpdateJobParams{OrgID: e.orgID, ID: job.Row.ID})
	require.NoError(t, err)
	require.True(t, row.CleanupPending)
	assert.True(t, e.svc.cleanupJob(e.ctx, row), "a removed container leaves nothing Fleet can clean up")

	// An unpaired but still present container is retried instead.
	_, err = e.conn.ExecContext(e.ctx, `UPDATE device SET deleted_at=NULL WHERE id=$1`, e.device.DatabaseID)
	require.NoError(t, err)
	assert.False(t, e.svc.cleanupJob(e.ctx, row))
}

// The lookup before the job lock already answers NotFound; act also maps a
// removal that lands between that lookup and the lock.
func TestActions_OnARemovedContainerReportNotFound(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	_, err = e.removeContainer()
	require.NoError(t, err)

	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-2")
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), err)
}
