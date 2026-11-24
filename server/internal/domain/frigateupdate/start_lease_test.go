package frigateupdate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A delay between taking the start lease and sending Start must not let Start
// outlive the lease: once the lease has expired an operator cancel completes
// locally, so a Start sent afterwards could begin an update Fleet reported as
// cancelled.
func TestStart_NotSentAfterItsLeaseExpired(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	var cancelErr error
	var cancelled Job
	e.svc.afterStartLease = func() {
		e.svc.afterStartLease = nil
		e.clock.advance(e.svc.cfg.CallTimeout + startLeaseMargin + time.Second)
		cancelled, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	}

	e.svc.runOnce(e.ctx)
	require.NoError(t, cancelErr)
	require.Equal(t, StateCancelled, cancelled.Row.State, "the expired lease lets the cancel complete locally")

	e.updater.mu.Lock()
	starts := len(e.updater.startCalls)
	e.updater.mu.Unlock()
	assert.Zero(t, starts, "Start must not reach Frigate after the cancel")
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
}

// A shorter delay leaves time on the lease, so Start is still sent.
func TestStart_SentWhileItsLeaseHolds(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.svc.afterStartLease = func() {
		e.svc.afterStartLease = nil
		e.clock.advance(e.svc.cfg.CallTimeout / 2)
	}

	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
}

// Time spent by the reserved margin alone leaves nothing for Start: the margin
// is kept for transport and clock skew, not spent on the call.
func TestStart_NotSentOnceOnlyTheMarginRemains(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.svc.afterStartLease = func() {
		e.svc.afterStartLease = nil
		e.clock.advance(e.svc.cfg.CallTimeout)
	}

	e.svc.runOnce(e.ctx)
	e.updater.mu.Lock()
	starts := len(e.updater.startCalls)
	e.updater.mu.Unlock()
	assert.Zero(t, starts)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State, "the next pass reconciles")
}

// A runner that reaches the start deadline while another runner's Start is
// unresolved must not fail the job: that Start may still be accepted.
func TestStartTimeout_WaitsForAnotherRunnersLiveLease(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	other := NewService(e.conn, e.getter, e.store, nil, e.auth, nil, e.svc.cfg)
	other.now = e.clock.now

	e.updater.mu.Lock()
	e.updater.startHook = nil
	e.updater.mu.Unlock()
	var otherState string
	e.svc.afterStartLease = func() {
		e.svc.afterStartLease = nil
		// While this runner's Start is unresolved, the start deadline passes
		// and the other runner reads that Frigate does not know the job.
		_, err := e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET start_attempted_at=$2 WHERE id=$1`,
			job.Row.ID, e.clock.now().Add(-e.svc.cfg.StartTimeout-time.Minute))
		require.NoError(t, err)
		other.runOnce(e.ctx)
		otherState = e.job(job.Row.ID).Row.State
	}

	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStarting, otherState, "the other runner waits for the live lease")
	e.updater.mu.Lock()
	cancels := len(e.updater.cancelCalls)
	e.updater.mu.Unlock()
	assert.Zero(t, cancels, "no timeout Cancel races the unresolved Start")
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the leased Start was accepted")
}

// A lease taken after a runner's snapshot but before its status read returned
// is seen before the timeout Cancel and failure.
func TestStartTimeout_RechecksTheLeaseAfterTheStatusRead(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.clock.advance(e.svc.cfg.StartTimeout + time.Minute)
	e.updater.mu.Lock()
	e.updater.statusHook = func(int) {
		_, err := e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET start_lease_until=$2 WHERE id=$1`,
			job.Row.ID, e.clock.now().Add(time.Minute))
		require.NoError(t, err)
	}
	e.updater.mu.Unlock()

	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID).Row
	assert.Equal(t, StateStarting, got.State)
	e.updater.mu.Lock()
	cancels := len(e.updater.cancelCalls)
	e.updater.statusHook = nil
	e.updater.mu.Unlock()
	assert.Zero(t, cancels)

	// Once the lease expires the next pass reconciles and times the job out.
	e.clock.advance(2 * time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateFailed, e.job(job.Row.ID).Row.State)
	assert.Equal(t, CodeStartTimeout, e.job(job.Row.ID).Row.ErrorCode)
}
