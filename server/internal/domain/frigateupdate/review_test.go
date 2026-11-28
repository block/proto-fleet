package frigateupdate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// startingJob drives a job to starting: staged everywhere, with a Start whose
// outcome was lost and which Frigate never accepted.
func (e *env) startingJob(key string) Job {
	e.t.Helper()
	job := e.start(key)
	e.updater.mu.Lock()
	e.updater.startHook = func(int) (bool, error) { return false, lostReply() }
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	require.Equal(e.t, StateStarting, got.Row.State, "error %s: %s", got.Row.ErrorCode, got.Row.ErrorMessage)
	require.True(e.t, got.Row.StartAttemptedAt.Valid)
	return got
}

func (e *env) trackedFor(id uuid.UUID) (progress, lost bool) {
	e.svc.mu.Lock()
	defer e.svc.mu.Unlock()
	_, progress = e.svc.progress[id]
	_, lost = e.svc.lost[id]
	return progress, lost
}

func (e *env) cleanupFlag(id uuid.UUID) sqlc.FrigateUpdateJob { return e.job(id).Row }

func (e *env) stageCount() int {
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	return len(e.updater.stages)
}

// Start deadline.

func TestRunner_StartDeadlineFailsJobAndCleansUp(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.clock.advance(5 * time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State, "still retrying before the deadline")
	assert.Empty(t, e.updater.cancelCalls)

	e.clock.advance(6 * time.Minute)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStartTimeout, got.Row.ErrorCode)
	assert.False(t, got.Row.Retryable)
	assert.True(t, got.Row.CleanupPending)
	assert.True(t, got.Row.CompletedAt.Valid)
	require.Len(t, e.updater.cancelCalls, 1, "Frigate is asked to cancel with a derived key before the job fails")
	assert.Equal(t, "fleet-start-timeout-"+job.Row.ID.String(), e.updater.cancelCalls[0].IdempotencyKey)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 2, starts, "Start was retried once on the pass before the deadline, and not at all after it")

	// The sweep reclaims the staged copies, and the container is free again.
	e.svc.runOnce(e.ctx)
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
	assert.Zero(t, e.stageCount())
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
}

func TestRunner_StartDeadlineWaitsForReadableStatus(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.updater.mu.Lock()
	e.updater.statusErr = lostReply()
	e.updater.mu.Unlock()
	e.clock.advance(time.Hour)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStarting, got.Row.State, "an unreadable status proves nothing, so the job is not failed")
	assert.Empty(t, e.updater.cancelCalls)
	assert.True(t, got.Row.UnreachableSince.Valid)
}

func TestRunner_StartDeadlineTakesFrigatesAnswerWhenItKnowsTheJob(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	// Status said NotFound, but Frigate accepted the job before the Cancel arrived.
	e.updater.mu.Lock()
	e.updater.cancelHook = func(a sdk.FrigateAction) sdk.FrigateRollout {
		return sdk.FrigateRollout{JobID: a.JobID, Phase: sdk.FrigatePhaseCancelled, UpdatedAt: time.Now()}
	}
	e.updater.mu.Unlock()
	e.clock.advance(11 * time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
}

func TestRunner_StartDeadlineKeepsJobWhenFrigateRefusesTheCancel(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.updater.mu.Lock()
	e.updater.cancelErr = refusal(codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE", "past the point of no return")
	e.updater.mu.Unlock()
	e.clock.advance(11 * time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State, "Frigate knows the job, so Fleet must not fail it")
}

func TestRunner_StartDeadlineFailsEvenWhenTheCancelCannotBeDelivered(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.updater.mu.Lock()
	e.updater.cancelErr = lostReply()
	e.updater.mu.Unlock()
	e.clock.advance(11 * time.Minute)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStartTimeout, got.Row.ErrorCode)
}

// Operator cancel of a job whose start never reached Frigate.

func TestCancelUpdate_StartingWithNoFrigateRecordCompletesLocally(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")

	// The runner's Start may still be in flight: the lease holds the cancel back,
	// so it is recorded as pending instead of being refused.
	pending, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.True(t, CancelPending(pending.Row))
	assert.Equal(t, StateStarting, pending.Row.State)

	// A runner pass inside the lease changes nothing; once the lease has expired
	// a fresh NotFound lets the runner finish the cancel here.
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State)
	e.clock.advance(time.Minute)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateCancelled, got.Row.State)
	assert.True(t, got.Row.CleanupPending)
	assert.False(t, got.Row.Cancellable)
	require.Len(t, e.updater.cancelCalls, 1)

	// A replay answers from the record without another Frigate call.
	again, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, again.Row.State)
	assert.Len(t, e.updater.cancelCalls, 1)

	// The runner never starts it, cleans up, and the container is free.
	e.svc.runOnce(e.ctx)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 1, starts)
	assert.Zero(t, e.stageCount())
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
}

func TestCancelUpdate_LocalCancelFencesARunnerAboutToStart(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.clock.advance(time.Minute) // The first pass's lease has expired.

	// The operator cancels between the runner's Status read and its Start.
	var once sync.Once
	var cancelErr error
	e.updater.mu.Lock()
	e.updater.statusHook = func(int) {
		once.Do(func() {
			e.updater.mu.Lock()
			e.updater.statusHook = nil
			e.updater.mu.Unlock()
			_, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
		})
	}
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)

	require.NoError(t, cancelErr)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateCancelled, got.Row.State)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 1, starts, "the runner re-checked the state under the job lock and did not send Start")
}

func TestCancelUpdate_StartingRunnerLeaseBlocksALocalCancel(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	// The runner just took the lease for an attempt that is still in flight.
	_, err := e.conn.ExecContext(e.ctx, `UPDATE frigate_update_job SET start_lease_until=$2 WHERE id=$1`, job.Row.ID, e.clock.now().Add(time.Minute))
	require.NoError(t, err)
	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateStarting, got.Row.State, "the cancel waits for the lease instead of completing")
	assert.True(t, CancelPending(got.Row))
}

func TestCancelUpdate_StartingButFrigateHasTheRollout(t *testing.T) {
	e := newEnv(t)
	job := e.startingJob("key-1")
	e.updater.mu.Lock()
	e.updater.cancelErr = refusal(codes.NotFound, frigateNotFoundReason, "unknown rollout")
	ro := sdk.FrigateRollout{JobID: job.Row.ID.String(), Phase: sdk.FrigatePhaseVerifying, Cancellable: true, UpdatedAt: time.Now()}
	e.updater.rollout = &ro
	e.updater.mu.Unlock()
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "accepted the update")
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the answer Fleet read is applied")
}

// Retry and cancel replay.

func TestRetryUpdate_ReplayReachesFrigateOnceWithTheSameKey(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase, r.Retryable, r.DisruptionStarted, r.Cancellable = sdk.FrigatePhaseFailed, true, true, true
	})
	e.svc.runOnce(e.ctx)
	e.updater.retryHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Retryable = sdk.FrigatePhaseInstalling, false
		ro.UpdatedAt = ro.UpdatedAt.Add(time.Second)
		return ro
	}
	for range 2 {
		got, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
		require.NoError(t, err)
		assert.Equal(t, StateRunning, got.Row.State)
	}
	require.Len(t, e.updater.retryCalls, 2)
	want := "fleet-retry-" + job.Row.ID.String() + "-1"
	assert.Equal(t, want, e.updater.retryCalls[0].IdempotencyKey)
	assert.Equal(t, want, e.updater.retryCalls[1].IdempotencyKey, "a replay reuses the derived key")
	assert.Equal(t, 1, e.updater.hookRuns, "Frigate performed the retry once")
	assert.EqualValues(t, 1, e.job(job.Row.ID).Row.ActionCounter, "a replay does not advance the counter")
}

func TestCancelUpdate_ReplayAfterFrigateSideCancel(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.cancelHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Cancellable = sdk.FrigatePhaseCancelled, false
		ro.UpdatedAt = ro.UpdatedAt.Add(time.Second)
		return ro
	}
	first, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, first.Row.State)

	again, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err, "the job is already cancelled, but the same key still replays")
	assert.Equal(t, StateCancelled, again.Row.State)
	require.Len(t, e.updater.cancelCalls, 2)
	assert.Equal(t, e.updater.cancelCalls[0].IdempotencyKey, e.updater.cancelCalls[1].IdempotencyKey)
	assert.Equal(t, 1, e.updater.hookRuns)

	// A different key is a new action and the finished job refuses it.
	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-2")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
}

func TestActions_AnyRecordedKeyReplays(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase, r.Retryable, r.DisruptionStarted, r.Cancellable = sdk.FrigatePhaseFailed, true, true, true
	})
	e.svc.runOnce(e.ctx)
	e.updater.retryHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Retryable = sdk.FrigatePhaseInstalling, false
		ro.UpdatedAt = ro.UpdatedAt.Add(time.Second)
		return ro
	}
	e.updater.cancelHook = func(sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Cancellable = sdk.FrigatePhaseCancelled, false
		ro.UpdatedAt = ro.UpdatedAt.Add(2 * time.Second)
		return ro
	}
	_, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "K1")
	require.NoError(t, err)
	cancelled, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "K2")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, cancelled.Row.State)

	// A delayed duplicate of the first request must replay it, not run as new.
	late, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "K1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, late.Row.State)
	last := e.updater.retryCalls[len(e.updater.retryCalls)-1]
	assert.Equal(t, "fleet-retry-"+job.Row.ID.String()+"-1", last.IdempotencyKey)
	assert.Equal(t, 1, e.updater.hookRuns-1, "the retry ran once; only the cancel ran besides it")
	assert.EqualValues(t, 2, e.job(job.Row.ID).Row.ActionCounter)

	// The same key under another kind is a different action.
	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "K1")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
}

func TestActions_AreBounded(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	for i := range maxActionsPerJob {
		_, err := e.conn.ExecContext(e.ctx, `INSERT INTO frigate_update_action(job_id,kind,request_key,counter) VALUES($1,'retry',$2,$3)`, job.Row.ID, "seed-"+string(rune('a'+i%26))+uuid.NewString(), i+1)
		require.NoError(t, err)
	}
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "one-too-many")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "too many")
}

// Runner bookkeeping.

func TestRunner_ProgressAndLostAreClearedWhenAJobEndsOtherwise(t *testing.T) {
	e := newEnv(t)

	// A cancelled job leaves no staging clock behind.
	job := e.start("key-1")
	e.svc.noteProgress(job.Row.ID)
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	progress, lost := e.trackedFor(job.Row.ID)
	assert.False(t, progress)
	assert.False(t, lost)

	// A rollout that ends through an operator action or an applied answer
	// clears the lost-poll count too.
	running := e.advanceToRunning(e.start("key-2"))
	e.svc.mu.Lock()
	e.svc.lost[running.Row.ID] = 2
	e.svc.progress[running.Row.ID] = stallClock{since: time.Now()}
	e.svc.mu.Unlock()
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseSucceeded })
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateSucceeded, e.job(running.Row.ID).Row.State)
	progress, lost = e.trackedFor(running.Row.ID)
	assert.False(t, progress)
	assert.False(t, lost)

	// A cancel that Frigate performs clears it as well.
	third := e.advanceToRunning(e.start("key-3"))
	e.updater.cancelHook = func(a sdk.FrigateAction) sdk.FrigateRollout {
		return sdk.FrigateRollout{JobID: a.JobID, Phase: sdk.FrigatePhaseCancelled, UpdatedAt: time.Now().Add(time.Hour)}
	}
	e.svc.mu.Lock()
	e.svc.lost[third.Row.ID] = 1
	e.svc.mu.Unlock()
	_, err = e.svc.CancelUpdate(e.ctx, third.Row.ID, "op-3")
	require.NoError(t, err)
	_, lost = e.trackedFor(third.Row.ID)
	assert.False(t, lost)
}

// Staging.

func TestRunner_OneInstanceUnreachableWhileStaging(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.beginHook = func(id string) error {
		if id == instanceB {
			return lostReply()
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStaging, got.Row.State, "one unreachable controller keeps the job staging")
	assert.Equal(t, StageStaged, e.instance(got, instanceA).StageState)
	_, starts, _ := e.updater.calls()
	assert.Zero(t, starts, "Start waits for every controller")

	e.clock.advance(11 * time.Minute)
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
	assert.True(t, got.Row.CleanupPending)
	assert.NotContains(t, got.Row.ErrorMessage, "refused")

	// The staged copy on the reachable controller is aborted by its stored
	// fingerprint. The other controller never answered Begin, so once it is
	// reachable cleanup repeats Begin to learn the fingerprint and aborts that
	// copy too.
	e.updater.beginHook = nil
	e.svc.runOnce(e.ctx)
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	assert.ElementsMatch(t, []string{instanceA, instanceB}, e.updater.aborts)
	assert.Empty(t, e.updater.stages)
	for i, id := range e.updater.aborts {
		if id == instanceA {
			assert.Equal(t, e.instance(got, instanceA).ManifestSha256, e.updater.abortRefs[i].ManifestSHA256)
		}
	}
}

func TestRunner_CancelDuringStagingStopsUploadsAndCleansUp(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	var once sync.Once
	var cancelErr error
	e.updater.appendHook = func(id string, off int64) error {
		if id == instanceA && off == 1<<20 {
			once.Do(func() { _, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1") })
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	require.NoError(t, cancelErr)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateCancelled, got.Row.State)
	_, starts, _ := e.updater.calls()
	assert.Zero(t, starts)
	assert.Equal(t, []int64{0, 1 << 20}, e.updater.appendOffset[instanceA][:2], "uploads stop at the first acknowledgement after the cancel")
	assert.Less(t, len(e.updater.appendOffset[instanceA]), 3)
	progress, _ := e.trackedFor(job.Row.ID)
	assert.False(t, progress)

	e.svc.runOnce(e.ctx)
	assert.Zero(t, e.stageCount(), "the sweep aborts every staged copy")
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

func TestRunner_CancelWhileBeginIsInFlightAbortsTheFreshCopyDirectly(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	var once sync.Once
	var cancelErr error
	e.updater.beginHook = func(id string) error {
		if id == instanceA {
			once.Do(func() { _, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1") })
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	require.NoError(t, cancelErr)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)

	// persistStaging affected no row, so the sweep could not have found this
	// fingerprint: the uploader aborted the copy itself, within the same pass.
	e.updater.mu.Lock()
	assert.Contains(t, e.updater.aborts, instanceA)
	_, stillStaged := e.updater.stages[instanceA+"/"+job.Row.ID.String()]
	e.updater.mu.Unlock()
	assert.False(t, stillStaged)
	var recorded string
	require.NoError(t, e.conn.QueryRowContext(e.ctx, `SELECT manifest_sha256 FROM frigate_update_instance WHERE job_id=$1 AND instance_id=$2`, job.Row.ID, instanceA).Scan(&recorded))
	assert.Empty(t, recorded, "the fingerprint was never persisted")
	e.updater.mu.Lock()
	require.NotEmpty(t, e.updater.abortRefs)
	assert.Len(t, e.updater.abortRefs[0].ManifestSHA256, 64)
	e.updater.mu.Unlock()

	// Whatever the other controller held is reclaimed by the sweep.
	e.svc.runOnce(e.ctx)
	assert.Zero(t, e.stageCount())
}

// Cleanup sweep.

// failedJobWithStagedCopies fails a job during staging so both controllers hold
// a fingerprint, and returns it with cleanup pending.
func (e *env) failedJobWithStagedCopies(key string) Job {
	e.t.Helper()
	job := e.start(key)
	// B refuses its second chunk only after A has started uploading, so both
	// controllers hold a persisted fingerprint when the job fails.
	aUploading := make(chan struct{})
	var aOnce sync.Once
	e.updater.mu.Lock()
	e.updater.appendHook = func(id string, off int64) error {
		if id == instanceA {
			aOnce.Do(func() { close(aUploading) })
		}
		if id == instanceB && off == 1<<20 {
			<-aUploading
			return refusal(codes.FailedPrecondition, "UPDATE_RELEASE_SIGNATURE_INVALID", "bad signature")
		}
		return nil
	}
	e.updater.mu.Unlock()
	// Process only this job: a full pass would also sweep earlier failed jobs.
	runnable, err := e.svc.queries.ListRunnableFrigateUpdateJobs(e.ctx)
	require.NoError(e.t, err)
	for _, row := range runnable {
		if row.ID == job.Row.ID {
			e.svc.processJob(e.ctx, row)
		}
	}
	got := e.job(job.Row.ID)
	require.Equal(e.t, StateFailed, got.Row.State)
	require.True(e.t, got.Row.CleanupPending)
	e.updater.mu.Lock()
	e.updater.appendHook = nil
	e.updater.mu.Unlock()
	return got
}

func TestSweepCleanup_AbortsByFingerprint(t *testing.T) {
	e := newEnv(t)
	job := e.failedJobWithStagedCopies("key-1")
	fingerprints := map[string]string{}
	for _, in := range job.Instances {
		fingerprints[in.InstanceID] = e.instance(job, in.InstanceID).ManifestSha256
	}
	require.Len(t, fingerprints[instanceA], 64)
	require.Len(t, fingerprints[instanceB], 64)

	e.svc.sweepCleanup(e.ctx)
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	require.Len(t, e.updater.abortRefs, 2)
	for i, ref := range e.updater.abortRefs {
		assert.Equal(t, job.Row.ID.String(), ref.JobID)
		assert.Equal(t, fingerprints[e.updater.aborts[i]], ref.ManifestSHA256)
		assert.NotEmpty(t, ref.Actor)
	}
	assert.Empty(t, e.updater.stages)
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

func TestSweepCleanup_RefusalCountsAsDone(t *testing.T) {
	e := newEnv(t)
	job := e.failedJobWithStagedCopies("key-1")
	e.updater.abortHook = func(context.Context, string, sdk.FrigateStageRef) error {
		return refusal(codes.FailedPrecondition, "UPDATE_STAGE_NOT_ABORTABLE", "already handed to the helper")
	}
	e.svc.sweepCleanup(e.ctx)
	got := e.cleanupFlag(job.Row.ID)
	assert.False(t, got.CleanupPending, "a refusal is final, so retrying would only repeat it")
	assert.False(t, got.CleanupAttemptedAt.Valid)
}

func TestSweepCleanup_UnreachableRetriesAndGivesUpAfterADay(t *testing.T) {
	e := newEnv(t)
	job := e.failedJobWithStagedCopies("key-1")
	calls := 0
	e.updater.abortHook = func(context.Context, string, sdk.FrigateStageRef) error {
		calls++
		return lostReply()
	}
	e.svc.sweepCleanup(e.ctx)
	got := e.cleanupFlag(job.Row.ID)
	assert.True(t, got.CleanupPending, "an unreachable controller keeps the job queued")
	assert.True(t, got.CleanupAttemptedAt.Valid)
	assert.Equal(t, 2, calls)

	e.clock.advance(23 * time.Hour)
	e.svc.sweepCleanup(e.ctx)
	assert.True(t, e.cleanupFlag(job.Row.ID).CleanupPending)
	assert.Equal(t, 4, calls)

	e.clock.advance(2 * time.Hour)
	e.svc.sweepCleanup(e.ctx)
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending, "gives up 24 hours after the job ended")
	assert.Equal(t, 4, calls, "no abort is sent once it has given up")
}

func TestSweepCleanup_LeastRecentlyAttemptedGoesFirst(t *testing.T) {
	e := newEnv(t)
	first := e.failedJobWithStagedCopies("key-1")
	second := e.failedJobWithStagedCopies("key-2")
	q := sqlc.New(e.conn)

	list := func() []uuid.UUID {
		rows, err := q.ListFrigateUpdateCleanupJobs(e.ctx)
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	assert.Equal(t, []uuid.UUID{first.Row.ID, second.Row.ID}, list())
	require.NoError(t, q.MarkFrigateUpdateCleanupAttempted(e.ctx, sqlc.MarkFrigateUpdateCleanupAttemptedParams{
		ID: first.Row.ID, AttemptedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}))
	assert.Equal(t, []uuid.UUID{second.Row.ID, first.Row.ID}, list(), "a job that failed an attempt moves behind the untried ones")
}

func TestRun_CleanupNeverBlocksPolling(t *testing.T) {
	e := newEnv(t)
	e.svc.cfg.PollInterval = 20 * time.Millisecond
	stuck := e.failedJobWithStagedCopies("key-1")

	blocked := make(chan struct{})
	e.updater.mu.Lock()
	e.updater.abortHook = func(ctx context.Context, _ string, _ sdk.FrigateStageRef) error {
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-ctx.Done() // A controller that never answers the abort.
		return ctx.Err()
	}
	e.updater.mu.Unlock()
	next := e.start("key-2")

	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.svc.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("the cleanup sweep never started")
	}
	require.Eventually(t, func() bool {
		return e.job(next.Row.ID).Row.State == StateRunning
	}, 10*time.Second, 20*time.Millisecond, "the next job is staged and started while an abort is stuck")
	assert.True(t, e.cleanupFlag(stuck.Row.ID).CleanupPending)
}

// Package re-upload, listing and orphan blobs.

func (e *env) importDeb(t *testing.T, rel releaseFixture, deb []byte, mutate func(s *frigaterelease.Statement)) (ImportResult, []byte, error) {
	t.Helper()
	statement := statementFor(t, deb, mutate)
	res, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "frigate_2.1.0_arm64.deb", Statement: statement, Signature: rel.sign(t, statement)})
	return res, statement, err
}

func (e *env) storedPackage(id uuid.UUID) sqlc.FrigateUpdatePackage {
	e.t.Helper()
	row, err := sqlc.New(e.conn).GetFrigateUpdatePackageBySHA256(e.ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: e.orgID, Sha256: e.shaOf(id)})
	require.NoError(e.t, err)
	return row
}

func (e *env) shaOf(id uuid.UUID) string {
	e.t.Helper()
	var sha string
	require.NoError(e.t, e.conn.QueryRowContext(e.ctx, `SELECT sha256 FROM frigate_update_package WHERE id=$1`, id).Scan(&sha))
	return sha
}

func TestImportPackage_ReuploadRefreshesTheStatementWhenUnused(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0-1", "arm64"), "control.tar.gz")
	first, firstStatement, err := e.importDeb(t, rel, deb, nil)
	require.NoError(t, err)

	// Re-signed by a rotated key with a newer statement for the same bytes.
	rotated := newReleaseFixture(t, e)
	e.svc.verifiers[PackageKindFrigate] = frigaterelease.NewStaticVerifier(rel.key, rotated.key)
	second, secondStatement, err := e.importDeb(t, rotated, deb, func(s *frigaterelease.Statement) {
		s.MinInstalledVersion = "1.5.0"
		s.IssuedAt = s.IssuedAt.Add(time.Hour)
	})
	require.NoError(t, err)
	assert.True(t, second.Reused)
	assert.Equal(t, first.Package.ID, second.Package.ID)
	assert.Equal(t, rotated.keyID(t), second.Package.ReleaseKeyID)
	assert.Equal(t, "1.5.0", second.Package.MinInstalledVersion)

	row := e.storedPackage(first.Package.ID)
	assert.Equal(t, secondStatement, row.Statement, "the freshly verified statement replaces the stored one")
	assert.NotEqual(t, firstStatement, row.Statement)
	assert.Equal(t, rotated.keyID(t), row.ReleaseKeyID)
	assert.NotEmpty(t, row.Signature)
	require.NoError(t, e.verifyStored(row))
}

func (e *env) verifyStored(row sqlc.FrigateUpdatePackage) error {
	_, err := e.svc.verifiers[PackageKindFrigate].Verify(row.Statement, row.Signature, row.ReleaseKeyID)
	return err
}

func TestImportPackage_ReuploadOfTheSameStatementKeepsTheStoredSignature(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sigOne, sigTwo := rel.sign(t, statement), rel.sign(t, statement)
	require.NotEqual(t, sigOne, sigTwo, "ECDSA signs differently every time")

	first, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "a.deb", Statement: statement, Signature: sigOne})
	require.NoError(t, err)
	// Even a package in use may be uploaded again with an equivalent signature.
	e.trustAnyKey()
	job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: first.Package.ID, IdempotencyKey: "key-1"})
	require.NoError(t, err)
	require.Equal(t, StateStaging, job.Row.State)

	again, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "b.deb", Statement: statement, Signature: sigTwo})
	require.NoError(t, err)
	assert.True(t, again.Reused)
	assert.Equal(t, sigOne, e.storedPackage(first.Package.ID).Signature)
}

func (e *env) trustAnyKey() {
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	for i := range e.updater.support {
		e.updater.support[i].TrustedReleaseKeyIDs = nil
	}
}

func TestImportPackage_ReuploadWithADifferentStatementIsRefusedWhileInUse(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	first, firstStatement, err := e.importDeb(t, rel, deb, nil)
	require.NoError(t, err)
	e.trustAnyKey()
	job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: first.Package.ID, IdempotencyKey: "key-1"})
	require.NoError(t, err)

	path := e.stage(t, deb)
	statement := statementFor(t, deb, func(s *frigaterelease.Statement) { s.MinInstalledVersion = "1.9.0" })
	_, err = e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: path, Filename: "c.deb", Statement: statement, Signature: rel.sign(t, statement)})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "still in progress")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "the staged upload is removed")
	assert.Equal(t, firstStatement, e.storedPackage(first.Package.ID).Statement, "the statement the controllers received is untouched")

	// Once the job is over the same upload is accepted.
	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "c")
	require.NoError(t, err)
	res, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "c.deb", Statement: statement, Signature: rel.sign(t, statement)})
	require.NoError(t, err)
	assert.True(t, res.Reused)
	assert.Equal(t, "1.9.0", res.Package.MinInstalledVersion)
}

func TestListPackages_ReportsWhetherTheBlobIsOnThisHost(t *testing.T) {
	e := newEnv(t)
	pkgs, err := e.svc.ListPackages(e.ctx)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.True(t, pkgs[0].StoredOnThisHost)

	require.NoError(t, e.store.Delete(e.pkg.ID))
	pkgs, err = e.svc.ListPackages(e.ctx)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.False(t, pkgs[0].StoredOnThisHost, "the row survived on this replica but the bytes did not")
}

func TestSweepOrphanPackages(t *testing.T) {
	e := newEnv(t)
	publish := func(id uuid.UUID, age time.Duration) {
		path, err := e.store.NewStagingPath("orphan-" + id.String())
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte("blob"), 0o600))
		require.NoError(t, e.store.Publish(id, path))
		old := time.Now().Add(-age)
		require.NoError(t, os.Chtimes(filepath.Dir(e.store.Path(id)), old, old))
	}
	oldOrphan, youngOrphan := uuid.New(), uuid.New()
	publish(oldOrphan, 2*time.Hour)
	publish(youngOrphan, time.Minute) // Possibly an import between Publish and Insert.

	// A soft-deleted package whose blob delete failed is an orphan too.
	deleted, _ := e.seedPackage("1.0.0")
	require.NoError(t, sqlc.New(e.conn).SoftDeleteFrigateUpdatePackage(e.ctx, sqlc.SoftDeleteFrigateUpdatePackageParams{OrgID: e.orgID, ID: deleted.ID}))
	require.NoError(t, os.Chtimes(filepath.Dir(e.store.Path(deleted.ID)), time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(filepath.Dir(e.store.Path(e.pkg.ID)), time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))

	// Entries that are not package directories are left alone.
	stray := filepath.Join(filepath.Dir(filepath.Dir(e.store.Path(e.pkg.ID))), "not-a-uuid")
	require.NoError(t, os.Mkdir(stray, 0o750))

	e.svc.now = time.Now
	removed, err := e.svc.SweepOrphanPackages(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.False(t, e.store.Exists(oldOrphan, 4))
	assert.True(t, e.store.Exists(youngOrphan, 4), "inside the grace period")
	assert.False(t, e.store.Exists(deleted.ID, deleted.SizeBytes))
	assert.True(t, e.store.Exists(e.pkg.ID, e.pkg.SizeBytes), "a package with a live row keeps its blob however old")
	assert.DirExists(t, stray)
}

// A failed Begin on a resumed copy must keep the saved fingerprint, or cleanup
// would skip the partial copy without aborting it.
func TestRunner_FailedResumeBeginKeepsTheFingerprintForCleanup(t *testing.T) {
	for _, tc := range []struct {
		name  string
		begin error
		end   func(e *env, job Job)
	}{
		{name: "cancel after a lost Begin", begin: lostReply(), end: func(e *env, job Job) {
			_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
			require.NoError(e.t, err)
			assert.Equal(e.t, StateCancelled, e.job(job.Row.ID).Row.State)
		}},
		{name: "stall timeout after a lost Begin", begin: lostReply(), end: func(e *env, job Job) {
			e.clock.advance(11 * time.Minute)
			e.svc.runOnce(e.ctx)
			assert.Equal(e.t, CodeStageStalled, e.job(job.Row.ID).Row.ErrorCode)
		}},
		{name: "refused Begin", begin: refusal(codes.FailedPrecondition, "UPDATE_STAGE_UNREADABLE", "spool unreadable"), end: func(e *env, job Job) {
			assert.Equal(e.t, StateFailed, e.job(job.Row.ID).Row.State)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			job := e.start("key-1")
			failed := false
			e.updater.appendHook = func(id string, off int64) error {
				if id == instanceA && off == 1<<20 && !failed {
					failed = true
					return lostReply()
				}
				return nil
			}
			e.svc.runOnce(e.ctx)
			partial := e.instance(e.job(job.Row.ID), instanceA)
			require.Equal(t, StageStaging, partial.StageState)
			require.Equal(t, int64(1<<20), partial.StagedBytes)
			require.Len(t, partial.ManifestSha256, 64)

			e.updater.mu.Lock()
			e.updater.beginHook = func(id string) error {
				if id == instanceA {
					return tc.begin
				}
				return nil
			}
			e.updater.mu.Unlock()
			e.svc.runOnce(e.ctx)
			assert.Equal(t, partial.ManifestSha256, e.instance(e.job(job.Row.ID), instanceA).ManifestSha256,
				"a failed Begin must not erase the fingerprint")

			tc.end(e, job)
			e.svc.sweepCleanup(e.ctx)
			e.updater.mu.Lock()
			var aborted []string
			for i, id := range e.updater.aborts {
				if id == instanceA {
					aborted = append(aborted, e.updater.abortRefs[i].ManifestSHA256)
				}
			}
			e.updater.mu.Unlock()
			assert.Contains(t, aborted, partial.ManifestSha256, "cleanup aborts the partial copy")
			assert.Zero(t, e.stageCount())
			assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
		})
	}
}

// A first Begin whose reply is lost leaves a copy on the controller with no
// recorded fingerprint. Cleanup must still find and abort it.
func TestRunner_LostFirstBeginReplyIsStillCleanedUp(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	lost := false
	e.updater.beginReplyHook = func(id string) error {
		if id == instanceB && !lost {
			lost = true
			return lostReply()
		}
		return nil
	}
	e.updater.appendHook = func(id string, off int64) error {
		if id == instanceB {
			return lostReply()
		}
		return nil
	}
	runnable, err := e.svc.queries.ListRunnableFrigateUpdateJobs(e.ctx)
	require.NoError(t, err)
	for _, row := range runnable {
		if row.ID == job.Row.ID {
			e.svc.processJob(e.ctx, row)
		}
	}
	b := e.instance(e.job(job.Row.ID), instanceB)
	require.Equal(t, StageStaging, b.StageState, "the instance is marked before its first Begin")
	require.Empty(t, b.ManifestSha256, "the lost reply left no fingerprint")
	require.Equal(t, 2, e.stageCount(), "the controller kept the copy")

	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	e.svc.sweepCleanup(e.ctx)
	assert.Zero(t, e.stageCount(), "cleanup recovers the fingerprint and aborts the copy")
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

// When the job leaves staging while a first Begin is in flight and the direct
// abort fails, the next sweep still aborts the copy.
func TestRunner_FailedDirectAbortAfterAnInFlightBeginIsRetriedBySweep(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	var once sync.Once
	var cancelErr error
	e.updater.beginHook = func(id string) error {
		if id == instanceA {
			once.Do(func() { _, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1") })
		}
		return nil
	}
	abortFailed := false
	e.updater.abortHook = func(_ context.Context, id string, _ sdk.FrigateStageRef) error {
		if id == instanceA && !abortFailed {
			abortFailed = true
			return lostReply()
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	require.NoError(t, cancelErr)
	require.True(t, abortFailed, "the direct abort was attempted and failed")
	require.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)

	e.svc.sweepCleanup(e.ctx)
	e.updater.mu.Lock()
	_, stillStaged := e.updater.stages[instanceA+"/"+job.Row.ID.String()]
	e.updater.mu.Unlock()
	assert.False(t, stillStaged, "the sweep aborts the copy the direct abort missed")
	assert.Zero(t, e.stageCount())
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}
