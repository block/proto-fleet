package frigateupdate

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// addContainer pairs one more CT1 container and returns its identifier.
func (e *env) addContainer() string {
	e.t.Helper()
	device := e.db.CreateDevice(e.orgID, "proto")
	_, err := e.conn.ExecContext(e.ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
	require.NoError(e.t, err)
	_, err = e.conn.ExecContext(e.ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(e.t, err)
	return device.ID
}

func (e *env) inflightCount() int {
	e.svc.mu.Lock()
	defer e.svc.mu.Unlock()
	return len(e.svc.inflight)
}

// gate blocks callers until release is called, then lets later callers pass
// through the next gate.
type gate struct {
	mu      sync.Mutex
	cur     chan struct{}
	entered chan struct{}
}

func newGate() *gate {
	return &gate{cur: make(chan struct{}), entered: make(chan struct{}, 64)}
}

func (g *gate) wait() {
	g.mu.Lock()
	ch := g.cur
	g.mu.Unlock()
	g.entered <- struct{}{}
	<-ch
}

func (g *gate) release() {
	g.mu.Lock()
	close(g.cur)
	g.cur = make(chan struct{})
	g.mu.Unlock()
}

func (f *fakeUpdater) polled(jobID uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.statusJobs {
		if id == jobID.String() {
			return true
		}
	}
	return false
}

func TestRun_FullWorkerSlotsDoNotStarveALaterJob(t *testing.T) {
	e := newEnv(t)
	// Nine jobs that stay runnable forever (retryable failures are listed until
	// an operator acts) and whose status reads outlast a tick: one more than
	// the workers that poll and start rollouts.
	var failed []uuid.UUID
	for i := range maxConcurrentJobs + 1 {
		job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.addContainer(), PackageID: e.pkg.ID, IdempotencyKey: fmt.Sprintf("failed-%d", i)})
		require.NoError(t, err)
		_, err = e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET state='failed', retryable=TRUE, cancellable=TRUE, disruption_started=TRUE WHERE id=$1`, job.Row.ID)
		require.NoError(t, err)
		failed = append(failed, job.Row.ID)
	}
	ninth := failed[maxConcurrentJobs]

	g := newGate()
	e.updater.mu.Lock()
	e.updater.statusHook = func(int) { g.wait() }
	e.updater.mu.Unlock()
	t.Cleanup(func() { // Never leave a worker blocked.
		for range 3 {
			g.release()
		}
	})

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.svc.run(ctx, ticks)
	}()
	defer func() {
		cancel()
		g.release()
		<-done
	}()

	// The first pass fills every slot with the oldest jobs; the ninth waits.
	for range maxConcurrentJobs {
		select {
		case <-g.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the first pass did not fill the worker slots")
		}
	}
	require.False(t, e.updater.polled(ninth))

	// Their calls finish between ticks, so every slot is free again on the next pass.
	g.release()
	require.Eventually(t, func() bool { return e.inflightCount() == 0 }, 10*time.Second, 5*time.Millisecond)

	ticks <- time.Now()
	require.Eventually(t, func() bool { return e.updater.polled(ninth) }, 10*time.Second, 5*time.Millisecond,
		"the job that never had a slot goes first on the second pass")
	for _, id := range failed {
		assert.Equal(t, StateFailed, e.job(id).Row.State)
	}
}

func TestFairOrder_LeastRecentlyDispatchedFirstAndPrunesTheRest(t *testing.T) {
	e := newEnv(t)
	jobs := make([]Job, 3)
	for i := range jobs {
		job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.addContainer(), PackageID: e.pkg.ID, IdempotencyKey: uuid.NewString()})
		require.NoError(t, err)
		jobs[i] = job
	}
	rows := []sqlc.FrigateUpdateJob{jobs[0].Row, jobs[1].Row, jobs[2].Row}
	ids := func(in []sqlc.FrigateUpdateJob) []uuid.UUID {
		out := make([]uuid.UUID, len(in))
		for i, r := range in {
			out[i] = r.ID
		}
		return out
	}
	assert.Equal(t, ids(rows), ids(e.svc.fairOrder(rows)), "never-dispatched jobs keep created_at order")

	e.svc.noteDispatched(rows[0].ID)
	e.svc.noteDispatched(rows[1].ID)
	assert.Equal(t, []uuid.UUID{rows[2].ID, rows[0].ID, rows[1].ID}, ids(e.svc.fairOrder(rows)))
	e.svc.noteDispatched(rows[0].ID)
	assert.Equal(t, []uuid.UUID{rows[2].ID, rows[1].ID, rows[0].ID}, ids(e.svc.fairOrder(rows)))

	// A job that left the runnable set is forgotten.
	e.svc.fairOrder(rows[:1])
	e.svc.mu.Lock()
	assert.Len(t, e.svc.dispatched, 1)
	e.svc.mu.Unlock()
}

// Pending cancel.

// pendingCancel runs the runner at a short real interval with a Start that keeps
// failing in an ambiguous way, cancels the job while the start lease is live and
// returns once the cancel is pending. The fake clock stays put, so the lease
// stays live until the test advances it.
type pendingCancel struct {
	e      *env
	job    Job
	cancel func()
}

func (e *env) runWithPendingCancel(t *testing.T) *pendingCancel {
	t.Helper()
	e.svc.cfg.PollInterval = 20 * time.Millisecond
	job := e.start("key-1")
	e.updater.mu.Lock()
	e.updater.startHook = func(int) (bool, error) { return false, lostReply() }
	// Keep the staged copies so the cancelled job's cleanup stays pending.
	e.updater.abortHook = func(context.Context, string, sdk.FrigateStageRef) error { return lostReply() }
	e.updater.mu.Unlock()

	ctx, stop := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.svc.Run(ctx)
	}()
	stopRunner := func() {
		stop()
		<-done
	}
	t.Cleanup(stopRunner)

	// The failing Start keeps renewing the lease on every pass.
	require.Eventually(t, func() bool {
		_, starts, _ := e.updater.calls()
		return starts >= 3
	}, 10*time.Second, 5*time.Millisecond)

	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err, "a cancel during a live start lease is accepted as pending")
	assert.Equal(t, StateStarting, got.Row.State)
	assert.True(t, CancelPending(got.Row))
	assert.False(t, got.Row.Cancellable)
	// Let a Start that was already past the lease check finish.
	time.Sleep(100 * time.Millisecond)
	return &pendingCancel{e: e, job: job, cancel: stopRunner}
}

func (p *pendingCancel) starts() int {
	_, starts, _ := p.e.updater.calls()
	return starts
}

func (p *pendingCancel) cancelCalls() []sdk.FrigateAction {
	p.e.updater.mu.Lock()
	defer p.e.updater.mu.Unlock()
	return append([]sdk.FrigateAction(nil), p.e.updater.cancelCalls...)
}

func TestRunner_CancelDuringARenewedStartLeaseEndsCancelled(t *testing.T) {
	e := newEnv(t)
	p := e.runWithPendingCancel(t)
	startsAtCancel := p.starts()

	// While the lease is live the runner neither starts nor completes the cancel.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, startsAtCancel, p.starts(), "no further Start is sent once a cancel is pending")
	assert.True(t, CancelPending(e.job(p.job.Row.ID).Row))

	e.clock.advance(time.Minute)
	require.Eventually(t, func() bool { return e.job(p.job.Row.ID).Row.State == StateCancelled }, 10*time.Second, 10*time.Millisecond)
	got := e.job(p.job.Row.ID).Row
	assert.True(t, got.CleanupPending)
	assert.False(t, got.Cancellable)
	assert.True(t, got.CompletedAt.Valid)
	assert.Equal(t, startsAtCancel, p.starts())
	assert.Len(t, p.cancelCalls(), 1, "only the operator's own attempt reached Frigate")

	// The container is free again, and the operator's key replays the outcome
	// without another Frigate call.
	again, err := e.svc.CancelUpdate(e.ctx, p.job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, again.Row.State)
	assert.Len(t, p.cancelCalls(), 1)
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
}

func TestRunner_PendingCancelReplayAndOtherKeysReturnThePendingJob(t *testing.T) {
	e := newEnv(t)
	p := e.runWithPendingCancel(t)
	before := len(p.cancelCalls())

	same, err := e.svc.CancelUpdate(e.ctx, p.job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.True(t, CancelPending(same.Row))
	other, err := e.svc.CancelUpdate(e.ctx, p.job.Row.ID, "op-2")
	require.NoError(t, err, "a second key joins the pending cancel")
	assert.True(t, CancelPending(other.Row))
	assert.Len(t, p.cancelCalls(), before, "neither request starts another cancel at Frigate")

	e.clock.advance(time.Minute)
	require.Eventually(t, func() bool { return e.job(p.job.Row.ID).Row.State == StateCancelled }, 10*time.Second, 10*time.Millisecond)
	for _, key := range []string{"op-1", "op-2"} {
		got, err := e.svc.CancelUpdate(e.ctx, p.job.Row.ID, key)
		require.NoError(t, err, key)
		assert.Equal(t, StateCancelled, got.Row.State, key)
	}
	assert.Len(t, p.cancelCalls(), before)
}

func TestRunner_PendingCancelWithTheRolloutAtFrigateSendsACancel(t *testing.T) {
	e := newEnv(t)
	p := e.runWithPendingCancel(t)
	startsAtCancel := p.starts()
	id := p.job.Row.ID

	// One of the lost Starts turns out to have been accepted.
	e.updater.mu.Lock()
	ro := sdk.FrigateRollout{JobID: id.String(), Phase: sdk.FrigatePhaseVerifying, Cancellable: true, UpdatedAt: time.Now()}
	e.updater.rollout = &ro
	e.updater.cancelHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		return sdk.FrigateRollout{JobID: id.String(), Phase: sdk.FrigatePhaseCancelled, UpdatedAt: time.Now().Add(time.Second)}
	}
	e.updater.mu.Unlock()
	e.clock.advance(time.Minute)

	require.Eventually(t, func() bool { return e.job(id).Row.State == StateCancelled }, 10*time.Second, 10*time.Millisecond)
	calls := p.cancelCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, derivedKey("cancel", id, 1), calls[0].IdempotencyKey, "the operator's own attempt")
	assert.Equal(t, derivedKey("cancel-pending", id, 1), calls[1].IdempotencyKey, "derived from the operator's key")
	assert.Equal(t, startsAtCancel, p.starts())
	assert.False(t, e.job(id).Row.CancelRequestedAt.Valid && CancelPending(e.job(id).Row))
}

func TestRunner_PendingCancelRefusedByFrigateFollowsTheRollout(t *testing.T) {
	e := newEnv(t)
	p := e.runWithPendingCancel(t)
	id := p.job.Row.ID

	e.updater.mu.Lock()
	ro := sdk.FrigateRollout{JobID: id.String(), Phase: sdk.FrigatePhaseStopping, DisruptionStarted: true, Cancellable: false, UpdatedAt: time.Now()}
	e.updater.rollout = &ro
	e.updater.cancelErr = refusal(codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE", "past the point of no return")
	e.updater.mu.Unlock()
	e.clock.advance(time.Minute)

	require.Eventually(t, func() bool { return e.job(id).Row.State == StateRunning }, 10*time.Second, 10*time.Millisecond)
	got := e.job(id).Row
	assert.True(t, got.DisruptionStarted)
	assert.False(t, CancelPending(got))
	require.Eventually(t, func() bool { return !e.job(id).Row.CancelRequestedAt.Valid }, 10*time.Second, 10*time.Millisecond, "the refused intent is cleared")
	n := len(p.cancelCalls())
	time.Sleep(150 * time.Millisecond)
	assert.Len(t, p.cancelCalls(), n, "a refused cancel is not resent on every poll")
}

func TestRunner_PendingCancelWithUnreadableStatusStaysPending(t *testing.T) {
	e := newEnv(t)
	p := e.runWithPendingCancel(t)
	id := p.job.Row.ID
	startsAtCancel := p.starts()

	e.updater.mu.Lock()
	e.updater.statusErr = lostReply()
	e.updater.mu.Unlock()
	e.clock.advance(time.Minute)
	time.Sleep(250 * time.Millisecond)

	got := e.job(id).Row
	assert.True(t, CancelPending(got), "nothing proves the job was never started")
	assert.Equal(t, startsAtCancel, p.starts())
	assert.Len(t, p.cancelCalls(), 1)

	// Once a controller answers that it has no record, the cancel completes.
	e.updater.mu.Lock()
	e.updater.statusErr = nil
	e.updater.mu.Unlock()
	require.Eventually(t, func() bool { return e.job(id).Row.State == StateCancelled }, 10*time.Second, 10*time.Millisecond)
	assert.Equal(t, startsAtCancel, p.starts())
}

func TestCancelUpdate_ExpiredLeaseStillCompletesImmediately(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.clock.advance(time.Minute)
	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, got.Row.State)
	assert.False(t, CancelPending(got.Row))
}

func TestCancelUpdate_LiveLeaseRecordsIntentAndNoStartFollows(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.True(t, CancelPending(got.Row))
	assert.Equal(t, "op-1", got.Row.CancelRequestKey)
	assert.NotEmpty(t, got.Row.CancelRequestActor)

	// A pass inside the lease sends no Start; one after it completes the cancel.
	e.svc.runOnce(e.ctx)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 1, starts)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State)
	e.clock.advance(time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
	_, starts, _ = e.updater.calls()
	assert.Equal(t, 1, starts)
}

func TestCancelUpdate_PendingCancelIsForwardedWhenAnInFlightStartIsAccepted(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	var once sync.Once
	var cancelErr error
	e.updater.mu.Lock()
	e.updater.startHook = func(int) (bool, error) {
		// The operator cancels while Start is in flight, under the live lease.
		once.Do(func() {
			e.updater.mu.Unlock()
			_, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
			e.updater.mu.Lock()
			e.updater.cancelHook = func(sdk.FrigateAction) sdk.FrigateRollout {
				return sdk.FrigateRollout{JobID: job.Row.ID.String(), Phase: sdk.FrigatePhaseCancelled, UpdatedAt: time.Now().Add(time.Second)}
			}
		})
		return true, nil
	}
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)

	require.NoError(t, cancelErr)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State, "the accepted Start is cancelled at Frigate")
}

func TestRun_BusyStagingWorkersDoNotDelayStatusPolls(t *testing.T) {
	e := newEnv(t)
	// Enough slow staging jobs to fill every worker if they shared one pool,
	// all listed ahead of a running job whose status must keep being read.
	for i := range maxConcurrentJobs {
		_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.addContainer(), PackageID: e.pkg.ID, IdempotencyKey: fmt.Sprintf("staging-%d", i)})
		require.NoError(t, err)
	}
	running, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.addContainer(), PackageID: e.pkg.ID, IdempotencyKey: "running"})
	require.NoError(t, err)
	_, err = e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET state='running', disruption_started=TRUE WHERE id=$1`, running.Row.ID)
	require.NoError(t, err)

	g := newGate()
	e.updater.mu.Lock()
	e.updater.appendHook = func(string, int64) error { g.wait(); return nil }
	e.updater.rollout = &sdk.FrigateRollout{JobID: running.Row.ID.String(), Phase: sdk.FrigatePhaseVerifying, DisruptionStarted: true, UpdatedAt: time.Now()}
	e.updater.mu.Unlock()

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.svc.run(ctx, ticks)
	}()
	defer func() {
		cancel()
		for range 4 {
			g.release()
		}
		<-done
	}()

	// Each job copies to both controllers at once.
	for range 2 * maxConcurrentStaging {
		select {
		case <-g.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("staging did not fill its workers")
		}
	}
	statuses := func() int {
		e.updater.mu.Lock()
		defer e.updater.mu.Unlock()
		return e.updater.statusCalls
	}
	require.Eventually(t, func() bool { return e.updater.polled(running.Row.ID) }, 10*time.Second, 5*time.Millisecond,
		"the running job is polled on the first pass")
	first := statuses()

	// Every staging worker is still busy, yet the next pass polls the rollout again.
	ticks <- time.Now()
	require.Eventually(t, func() bool { return statuses() > first }, 10*time.Second, 5*time.Millisecond)
	select {
	case <-g.entered:
		t.Fatal("a staging job ran beyond the staging worker limit")
	case <-time.After(50 * time.Millisecond):
	}
}
