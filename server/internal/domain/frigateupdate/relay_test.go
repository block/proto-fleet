package frigateupdate

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// relayUpdater is a controller pair that Fleet reaches through a relay, as
// through a Fleet Node. The relay copies the package itself: it runs the
// node's SDK code against the in-memory pair.
type relayUpdater struct {
	*fakeUpdater
	blob []byte

	mu            sync.Mutex
	pkgs          []interfaces.FrigatePackageRef
	stageCalls    [][]interfaces.FrigateStageTarget
	directAppends int
	// callErr fails a whole stage call. outcome fails the copy to one
	// controller. Calls count from 1.
	callErr func(call int) error
	outcome func(call int, target interfaces.FrigateStageTarget) error
}

var (
	_ interfaces.FrigateUpdater       = (*relayUpdater)(nil)
	_ interfaces.FrigatePackageStager = (*relayUpdater)(nil)
)

// FrigateStageAppend counts package bytes that Fleet tried to send itself.
func (r *relayUpdater) FrigateStageAppend(context.Context, string, sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	r.mu.Lock()
	r.directAppends++
	r.mu.Unlock()
	return sdk.FrigateStageProgress{}, sdk.ErrFrigateRelayAppend
}

func (r *relayUpdater) FrigateStagePackage(ctx context.Context, pkg interfaces.FrigatePackageRef, targets []interfaces.FrigateStageTarget) ([]interfaces.FrigateStageOutcome, error) {
	r.mu.Lock()
	r.pkgs = append(r.pkgs, pkg)
	r.stageCalls = append(r.stageCalls, targets)
	call, callErr, outcome := len(r.stageCalls), r.callErr, r.outcome
	r.mu.Unlock()
	if callErr != nil {
		if err := callErr(call); err != nil {
			return nil, err
		}
	}
	outcomes := make([]interfaces.FrigateStageOutcome, len(targets))
	for i, target := range targets {
		if outcome != nil {
			if err := outcome(call, target); err != nil {
				outcomes[i].Err = err
				continue
			}
		}
		begin, err := sdk.EncodeFrigateRelayStageBegin(target.InstanceID, target.Manifest)
		if err != nil {
			outcomes[i].Err = err
			continue
		}
		response, err := sdk.ServeFrigateRelayStage(ctx, r.fakeUpdater, begin, target.ManifestSHA256, sdk.FrigateRelayPackage{Data: bytes.NewReader(r.blob), SizeBytes: int64(len(r.blob))})
		if err != nil {
			outcomes[i].Err = err
			continue
		}
		outcomes[i].Commit, outcomes[i].Err = sdk.DecodeFrigateRelayStageCommit(response)
	}
	return outcomes, nil
}

func (r *relayUpdater) calls() [][]interfaces.FrigateStageTarget {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]interfaces.FrigateStageTarget(nil), r.stageCalls...)
}

type relayMiner struct {
	interfaces.Miner
	*relayUpdater
}

func newRelayEnv(t *testing.T) (*env, *relayUpdater) {
	t.Helper()
	e := newEnv(t)
	relay := &relayUpdater{fakeUpdater: e.updater, blob: e.blob}
	e.getter.mu.Lock()
	e.getter.miner = relayMiner{relayUpdater: relay}
	e.getter.mu.Unlock()
	return e, relay
}

func TestRunner_StagesThroughARelayInOneCall(t *testing.T) {
	e, relay := newRelayEnv(t)
	job := e.start("key-1")
	job = e.advanceToRunning(job)

	calls := relay.calls()
	require.Len(t, calls, 1, "one call copies the package to both controllers")
	assert.Equal(t, interfaces.FrigatePackageRef{ID: e.pkg.ID.String(), Filename: e.pkg.Filename, SizeBytes: e.pkg.SizeBytes, SHA256: e.pkg.Sha256}, relay.pkgs[0])
	require.Len(t, calls[0], 2)
	for _, target := range calls[0] {
		in := e.instance(job, target.InstanceID)
		assert.Equal(t, StageStaged, in.StageState)
		assert.Equal(t, e.pkg.SizeBytes, in.StagedBytes)
		assert.Equal(t, in.ManifestSha256, target.ManifestSHA256, "Fleet recorded the fingerprint before the relay copied")
		assert.Equal(t, target.InstanceID, target.Manifest.InstanceID)
		assert.Equal(t, e.blob, e.updater.stages[target.InstanceID+"/"+job.Row.ID.String()].data)
	}
	assert.ElementsMatch(t, []string{instanceA, instanceB}, []string{calls[0][0].InstanceID, calls[0][1].InstanceID})
	assert.Zero(t, relay.directAppends, "Fleet sends no package bytes itself")

	require.Len(t, e.updater.startCalls, 1)
	assert.Equal(t, e.instance(job, instanceA).ManifestSha256, e.updater.startCalls[0].ExpectedManifestSHA256[instanceA])
}

func TestRunner_RelayStageSendsAgainOnlyForTheControllerThatFailed(t *testing.T) {
	e, relay := newRelayEnv(t)
	relay.outcome = func(call int, target interfaces.FrigateStageTarget) error {
		if call == 1 && target.InstanceID == instanceB {
			return errors.New("controller did not answer")
		}
		return nil
	}
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)

	assert.Equal(t, StateStaging, e.job(job.Row.ID).Row.State)
	assert.Equal(t, StageStaged, e.instance(job, instanceA).StageState)
	b := e.instance(job, instanceB)
	assert.Equal(t, StageStaging, b.StageState)
	assert.Contains(t, b.ErrorMessage, "Retrying: controller did not answer")

	e.advanceToRunning(job)
	calls := relay.calls()
	require.Len(t, calls, 2)
	require.Len(t, calls[1], 1)
	assert.Equal(t, instanceB, calls[1][0].InstanceID)
}

func TestRunner_RelayStageRefusalFailsTheJob(t *testing.T) {
	e, relay := newRelayEnv(t)
	relay.outcome = func(_ int, target interfaces.FrigateStageTarget) error {
		if target.InstanceID == instanceB {
			return refusal(codes.ResourceExhausted, "UPDATE_INSUFFICIENT_SPACE", "spool is full")
		}
		return nil
	}
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, "UPDATE_INSUFFICIENT_SPACE", got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, instanceB)
	assert.Equal(t, StageFailed, e.instance(job, instanceB).StageState)
	assert.Empty(t, e.updater.startCalls)
}

func TestRunner_RelayStageCallFailureIsRetried(t *testing.T) {
	for name, callErr := range map[string]error{
		"no reply":          lostReply(),
		"no control stream": devicewrite.NotDispatched(fleeterror.NewUnavailableErrorf("fleet node has no active control stream; retry shortly")),
		"node busy":         devicewrite.NotDispatched(fleeterror.NewPlainError("fleet node busy; retry shortly", connect.CodeResourceExhausted)),
	} {
		t.Run(name, func(t *testing.T) {
			e, relay := newRelayEnv(t)
			relay.callErr = func(call int) error {
				if call == 1 {
					return callErr
				}
				return nil
			}
			job := e.start("key-1")
			e.svc.runOnce(e.ctx)

			assert.Equal(t, StateStaging, e.job(job.Row.ID).Row.State)
			for _, id := range []string{instanceA, instanceB} {
				in := e.instance(job, id)
				assert.Equal(t, StageStaging, in.StageState)
				assert.Len(t, in.ManifestSha256, 64, "the fingerprint is recorded, so cleanup can remove the copy")
				assert.Contains(t, in.ErrorMessage, "Retrying:")
			}

			e.advanceToRunning(job)
			assert.Len(t, relay.calls(), 2)
		})
	}
}

// The node refuses a stage that it can never run, such as a package above its
// download limit. The same stage cannot succeed later, so the job fails in
// the same pass.
func TestRunner_RelayStageThatTheNodeRejectsFailsTheJob(t *testing.T) {
	e, relay := newRelayEnv(t)
	relay.callErr = func(int) error {
		return sdk.InvalidWriteRequest(fleeterror.NewInvalidArgumentError(
			"fleet node rejected command: execute command: controller software package size 525336576 exceeds fleet node limit 524288000"))
	}
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID).Row
	assert.Equal(t, StateFailed, got.State)
	assert.Equal(t, CodeStageFailed, got.ErrorCode)
	assert.Contains(t, got.ErrorMessage, "exceeds fleet node limit 524288000")
	assert.True(t, got.CleanupPending, "cleanup removes the copies that Fleet began")
	assert.Len(t, relay.calls(), 1)
	assert.Empty(t, e.updater.startCalls)

	e.svc.sweepCleanup(e.ctx)
	assert.Zero(t, e.stageCount(), "no copy of the failed job is left on a controller")
}

func TestRunner_RelayStageWithTheWrongBytesFailsTheJob(t *testing.T) {
	e, relay := newRelayEnv(t)
	relay.blob = bytes.Clone(e.blob)
	relay.blob[0] ^= 0xff
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStageDigestMismatch, got.Row.ErrorCode)
	assert.Empty(t, e.updater.startCalls)
}

// cancelAndClean cancels a job while the relay copies, and lets a cleanup pass
// remove every copy before the copy command returns.
func (e *env) cancelAndClean(job Job) {
	e.t.Helper()
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(e.t, err)
	e.svc.sweepCleanup(e.ctx)
	require.Zero(e.t, e.stageCount())
	require.False(e.t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

func (e *env) setAbortHook(hook func(ctx context.Context, instanceID string, ref sdk.FrigateStageRef) error) {
	e.updater.mu.Lock()
	e.updater.abortHook = hook
	e.updater.mu.Unlock()
}

func TestRunner_RelayCopyThatFailsAfterCleanupLeavesNoCopy(t *testing.T) {
	e, relay := newRelayEnv(t)
	job := e.start("key-1")
	relay.callErr = func(call int) error {
		if call == 1 {
			e.cancelAndClean(job)
			return lostReply()
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	e.svc.runOnce(e.ctx)

	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
	assert.Zero(t, e.stageCount(), "no copy of the cancelled job is left on a controller")
}

func TestRunner_RelayCopyThatStartsAfterCleanupIsRemoved(t *testing.T) {
	e, relay := newRelayEnv(t)
	job := e.start("key-1")
	relay.callErr = func(call int) error {
		if call == 1 {
			// The relay then begins, fills, and commits each copy again.
			e.cancelAndClean(job)
		}
		return nil
	}
	e.svc.runOnce(e.ctx)

	e.setAbortHook(func(context.Context, string, sdk.FrigateStageRef) error { return lostReply() })
	e.svc.sweepCleanup(e.ctx)
	assert.True(t, e.cleanupFlag(job.Row.ID).CleanupPending, "cleanup tries again while the controllers do not answer")

	e.setAbortHook(nil)
	e.svc.sweepCleanup(e.ctx)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
	assert.Zero(t, e.stageCount(), "no copy of the cancelled job is left on a controller")
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

// A cleanup pass can abort one controller's copy before the relay copies, and
// the other controller's copy after it.
func TestRunner_RelayCopyDuringACleanupPassIsRemoved(t *testing.T) {
	e, relay := newRelayEnv(t)
	job := e.start("key-1")
	bWaits, bGoes := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.setAbortHook(func(_ context.Context, instanceID string, _ sdk.FrigateStageRef) error {
		if instanceID == instanceB {
			once.Do(func() { close(bWaits); <-bGoes })
		}
		return nil
	})
	swept := make(chan struct{})
	relay.callErr = func(call int) error {
		if call == 1 {
			_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
			assert.NoError(t, err)
			go func() {
				defer close(swept)
				e.svc.sweepCleanup(e.ctx)
			}()
			<-bWaits
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	close(bGoes)
	<-swept

	e.svc.sweepCleanup(e.ctx)
	assert.Equal(t, StateCancelled, e.job(job.Row.ID).Row.State)
	assert.Zero(t, e.stageCount(), "no copy of the cancelled job is left on a controller")
	assert.False(t, e.cleanupFlag(job.Row.ID).CleanupPending)
}

// A relay can continue to copy after Fleet stopped waiting for it. An offset
// that moved since the last attempt is progress.
func TestRunner_RelayStageStallsOnlyWithoutProgress(t *testing.T) {
	e, relay := newRelayEnv(t)
	relay.callErr = func(int) error { return errors.New("the relay lost its connection") }
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateStaging, e.job(job.Row.ID).Row.State)
	a := e.instance(job, instanceA)
	require.Zero(t, a.StagedBytes)

	// The relay moves one chunk after its call failed.
	_, err := e.updater.FrigateStageAppend(e.ctx, instanceA, sdk.FrigateStageAppend{
		JobID: job.Row.ID.String(), ManifestSHA256: a.ManifestSha256, Chunk: e.blob[:1000], Actor: "fleet:alice",
	})
	require.NoError(t, err)
	e.clock.advance(e.svc.cfg.StageStallTimeout + time.Minute)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStaging, e.job(job.Row.ID).Row.State, "the offset moved, so the copy is not stalled")
	assert.Equal(t, int64(1000), e.instance(job, instanceA).StagedBytes)

	e.clock.advance(e.svc.cfg.StageStallTimeout + time.Minute)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
}

// One relayed copy can last longer than the stall timeout. After a copy that
// failed, Fleet reads the controller's offset before it checks for a stall.
func TestRunner_RelayStageThatMovedBytesInALongAttemptIsNotStalled(t *testing.T) {
	e, relay := newRelayEnv(t)
	moves := true
	relay.outcome = func(_ int, target interfaces.FrigateStageTarget) error {
		if target.InstanceID == instanceA {
			// The attempt lasts longer than the stall timeout.
			e.clock.advance(e.svc.cfg.StageStallTimeout + time.Minute)
			if moves {
				e.updater.mu.Lock()
				staged := len(e.updater.stages[target.InstanceID+"/"+target.Manifest.JobID].data)
				e.updater.mu.Unlock()
				_, err := e.updater.FrigateStageAppend(e.ctx, target.InstanceID, sdk.FrigateStageAppend{
					JobID: target.Manifest.JobID, ManifestSHA256: target.ManifestSHA256, Offset: int64(staged),
					Chunk: e.blob[staged : staged+1000], Actor: target.Manifest.Actor,
				})
				assert.NoError(t, err)
			}
		}
		return errors.New("the relay ran out of time")
	}
	job := e.start("key-1")

	for attempt := int64(1); attempt <= 2; attempt++ {
		e.svc.runOnce(e.ctx)
		require.Equal(t, StateStaging, e.job(job.Row.ID).Row.State, "attempt %d moved bytes", attempt)
		a := e.instance(job, instanceA)
		assert.Equal(t, 1000*attempt, a.StagedBytes, "the offset that the controller acknowledged is recorded")
		assert.Contains(t, a.ErrorMessage, "Retrying: the relay ran out of time")
	}

	moves = false
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State, "a long attempt that moved no bytes is a stall")
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
}

// longRelayAttempts makes each copy attempt run past the stall timeout, as an
// attempt that the node ends at its command limit does. After each attempt,
// Begin fails until controllerAnswers runs.
func longRelayAttempts(e *env, relay *relayUpdater, moveBytes bool) (controllerAnswers func()) {
	attemptOver := false
	e.updater.beginHook = func(string) error {
		if attemptOver {
			return lostReply()
		}
		return nil
	}
	relay.outcome = func(_ int, target interfaces.FrigateStageTarget) error {
		if target.InstanceID == instanceA {
			e.clock.advance(e.svc.cfg.StageStallTimeout + 5*time.Second)
			if moveBytes {
				_, err := e.updater.FrigateStageAppend(e.ctx, target.InstanceID, sdk.FrigateStageAppend{
					JobID: target.Manifest.JobID, ManifestSHA256: target.ManifestSHA256, Chunk: e.blob[:1<<20], Actor: target.Manifest.Actor,
				})
				assert.NoError(e.t, err)
			}
			attemptOver = true
		}
		return errors.New("the relay ran out of time")
	}
	return func() { attemptOver = false }
}

func TestRunner_RelayStageWithAnUnreadOffsetAfterALongAttemptIsNotStalled(t *testing.T) {
	e, relay := newRelayEnv(t)
	longRelayAttempts(e, relay, true)
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStaging, got.Row.State, "progress is unknown, so the job is not stalled: %s", got.Row.ErrorCode)

	// The controller stays unreachable, so no progress is confirmed.
	e.clock.advance(e.svc.cfg.StageStallTimeout)
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State, "twice the stall timeout passed with no confirmed progress")
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
}

func TestRunner_RelayStageWithOffsetsThatStayUnreadStalls(t *testing.T) {
	e, relay := newRelayEnv(t)
	controllerAnswers := longRelayAttempts(e, relay, false)
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	require.Equal(t, StateStaging, got.Row.State, "progress is unknown, so the job is not stalled: %s", got.Row.ErrorCode)

	// The next pass begins each copy again, and its attempt also ends with an
	// offset that Fleet cannot read.
	controllerAnswers()
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State, "two attempts in a row with no confirmed progress are a stall")
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
}
