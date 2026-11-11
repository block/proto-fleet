package frigateupdate

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestStartUpdate_CreatesJobAndReplays(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	assert.Equal(t, StateStaging, job.Row.State)
	assert.Equal(t, "2.0.0", job.Row.TargetVersion)
	assert.Equal(t, []string{instanceA, instanceB}, job.Row.ExpectedInstanceIds)
	assert.Len(t, job.Instances, 2)
	assert.Equal(t, e.device.ID, job.DeviceIdentifier)

	again := e.start("key-1")
	assert.Equal(t, job.Row.ID, again.Row.ID)

	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, StageOnly: true, IdempotencyKey: "key-1"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}

func TestStartUpdate_RefusesSecondBlockingJob(t *testing.T) {
	e := newEnv(t)
	e.start("key-1")
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
}

func TestStartUpdate_ConcurrentStartsHaveOneWinner(t *testing.T) {
	e := newEnv(t)
	const n = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: fmt.Sprintf("race-%d", i)})
			if err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			} else {
				assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, ok)
}

func TestStartUpdate_ConcurrentSameKeyStartsReplayOneJob(t *testing.T) {
	e := newEnv(t)
	const n = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	ready := make(chan struct{})
	ids := make(map[uuid.UUID]int)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "same-key"})
			if !assert.NoError(t, err) {
				return
			}
			mu.Lock()
			ids[job.Row.ID]++
			mu.Unlock()
		}()
	}
	close(ready)
	wg.Wait()
	assert.Len(t, ids, 1, "every request with the key returns the one job it created")
	for _, count := range ids {
		assert.Equal(t, n, count)
	}
}

func TestStartUpdate_ValidatesInput(t *testing.T) {
	e := newEnv(t)
	for _, key := range []string{"", string(make([]byte, 129)), "bad\nkey", "naïve"} {
		_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: key})
		require.Error(t, err, key)
		assert.True(t, fleeterror.IsInvalidArgumentError(err), key)
	}
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: "nope", PackageID: e.pkg.ID, IdempotencyKey: "k"})
	assert.True(t, fleeterror.IsNotFoundError(err), err)
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: uuid.New(), IdempotencyKey: "k"})
	assert.True(t, fleeterror.IsNotFoundError(err), err)
}

func TestStartUpdate_Forbidden(t *testing.T) {
	e := newEnv(t)
	e.auth.denySite = true
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "k"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err), err)
	_, err = e.svc.GetSupport(e.ctx, e.device.ID, uuid.Nil)
	assert.True(t, fleeterror.IsForbiddenError(err), err)
}

func TestStartUpdate_MissingBlob(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.store.Delete(e.pkg.ID))
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "k"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), CodePackageMissing)
}

func TestStartUpdate_FleetNodeOwnedControllerIsFailedPrecondition(t *testing.T) {
	e := newEnv(t)
	e.getter.mu.Lock()
	e.getter.err = fleeterror.NewFailedPreconditionError("Frigate updates are not supported for FleetNode-owned controllers")
	e.getter.mu.Unlock()
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "k"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	_, err = e.svc.GetSupport(e.ctx, e.device.ID, uuid.Nil)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
}

func TestEvaluateSupport(t *testing.T) {
	pkg := &Package{Architecture: "arm64", SizeBytes: 100, ReleaseKeyID: testKeyID}
	mod := func(f func(in *sdk.FrigateInstanceSupport)) []sdk.FrigateInstanceSupport {
		s := newFakeUpdater().support
		f(&s[1])
		return s
	}
	tests := []struct {
		name string
		in   []sdk.FrigateInstanceSupport
		pkg  *Package
		code string
	}{
		{"ok", newFakeUpdater().support, pkg, ""},
		{"ok without package", newFakeUpdater().support, nil, ""},
		{"none", nil, pkg, CodeNoInstances},
		{"unreachable", mod(func(in *sdk.FrigateInstanceSupport) { in.Err = "down" }), pkg, CodeInstanceUnreachable},
		{"unreachable with reason", mod(func(in *sdk.FrigateInstanceSupport) { in.Err = "down"; in.ErrReason = "UPDATE_X" }), pkg, "UPDATE_X"},
		{"active", mod(func(in *sdk.FrigateInstanceSupport) { in.ActiveJobID = "j" }), pkg, CodeUpdateInProgress},
		{"not deb", mod(func(in *sdk.FrigateInstanceSupport) { in.InstallationType = "other" }), pkg, CodeUnsupportedInstallation},
		{"no helper", mod(func(in *sdk.FrigateInstanceSupport) { in.HelperAvailable = false }), pkg, CodeHelperUnavailable},
		{"no self", mod(func(in *sdk.FrigateInstanceSupport) { in.SelfCoordinationSupported = false }), pkg, CodeSelfCoordination},
		{"no remote", mod(func(in *sdk.FrigateInstanceSupport) { in.RemoteTriggerAllowed = false }), pkg, CodeRemoteTriggerDisabled},
		{"cluster", mod(func(in *sdk.FrigateInstanceSupport) { in.ClusterID = "other" }), pkg, CodeClusterMismatch},
		{"cluster case", mod(func(in *sdk.FrigateInstanceSupport) { in.ClusterID = "CLUSTER-1" }), pkg, ""},
		{"no cluster", func() []sdk.FrigateInstanceSupport {
			s := newFakeUpdater().support
			for i := range s {
				s[i].ClusterID = ""
			}
			return s
		}(), pkg, CodeClusterMismatch},
		{"etcd", mod(func(in *sdk.FrigateInstanceSupport) { in.EtcdMembersHealthy = 1 }), pkg, CodeEtcdUnhealthy},
		{"arch", mod(func(in *sdk.FrigateInstanceSupport) { in.Architecture = "amd64" }), pkg, CodeArchitectureMismatch},
		{"key", mod(func(in *sdk.FrigateInstanceSupport) { in.TrustedReleaseKeyIDs = []string{"sha256:other"} }), pkg, CodeReleaseKeyUntrusted},
		{"space", mod(func(in *sdk.FrigateInstanceSupport) { in.SpoolFreeBytes = 10 }), pkg, CodeInsufficientSpace},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, code, _ := evaluateSupport(tt.in, tt.pkg)
			assert.Equal(t, tt.code == "", ok)
			assert.Equal(t, tt.code, code)
		})
	}
}

func TestGetSupport(t *testing.T) {
	e := newEnv(t)
	s, err := e.svc.GetSupport(e.ctx, e.device.ID, e.pkg.ID)
	require.NoError(t, err)
	assert.True(t, s.Eligible)
	assert.Len(t, s.Instances, 2)

	e.updater.mu.Lock()
	e.updater.supportErr = fmt.Errorf("dial failed")
	e.updater.mu.Unlock()
	s, err = e.svc.GetSupport(e.ctx, e.device.ID, uuid.Nil)
	require.NoError(t, err)
	assert.False(t, s.Eligible)
	assert.Equal(t, CodeControllerUnreachable, s.IneligibleCode)
}

func TestStartUpdate_IneligibleCarriesCode(t *testing.T) {
	e := newEnv(t)
	e.updater.mu.Lock()
	e.updater.support[0].RemoteTriggerAllowed = false
	e.updater.mu.Unlock()
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "k"})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), CodeRemoteTriggerDisabled)
}

func TestRunner_StagesStartsAndSucceeds(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	job = e.advanceToRunning(job)
	assert.True(t, job.Row.StartAttempted)
	assert.Equal(t, "1.3.0", job.Row.FromVersion)

	for _, id := range []string{instanceA, instanceB} {
		in := e.instance(job, id)
		assert.Equal(t, StageStaged, in.StageState)
		assert.Equal(t, e.pkg.SizeBytes, in.StagedBytes)
		assert.Len(t, in.ManifestSha256, 64)
		assert.Equal(t, []int64{0, 1 << 20, 2 << 20}, e.updater.appendOffset[id])
		st := e.updater.stages[id+"/"+job.Row.ID.String()]
		require.NotNil(t, st)
		assert.Equal(t, e.blob, st.data)
		assert.Equal(t, sdk.FrigateCoordinationSelf, st.manifest.Coordination)
		assert.Equal(t, []string{instanceA, instanceB}, st.manifest.ExpectedInstanceIDs)
		assert.Equal(t, id, st.manifest.InstanceID)
		assert.Equal(t, e.pkg.Sha256, st.manifest.SHA256)
		assert.Equal(t, e.pkg.SizeBytes, st.manifest.SizeBytes)
		assert.Equal(t, `{"stmt":true}`, string(st.manifest.ReleaseStatement))
		assert.Empty(t, st.manifest.SiteID)
		assert.Empty(t, st.manifest.ContainerID)
		assert.Zero(t, st.manifest.AssetID)
	}

	require.Len(t, e.updater.startCalls, 1)
	req := e.updater.startCalls[0]
	assert.Equal(t, job.Row.ID.String(), req.JobID)
	assert.Equal(t, "fleet-start-"+job.Row.ID.String(), req.IdempotencyKey)
	assert.Equal(t, "2.0.0", req.TargetVersion)
	assert.Equal(t, []string{instanceA, instanceB}, req.ExpectedInstanceIDs)
	assert.Equal(t, e.instance(job, instanceA).ManifestSha256, req.ExpectedManifestSHA256[instanceA])
	assert.Equal(t, e.instance(job, instanceB).ManifestSha256, req.ExpectedManifestSHA256[instanceB])
	assert.NotEmpty(t, req.Actor)

	// Progress, then success.
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase = sdk.FrigatePhaseInstalling
		r.DisruptionStarted = true
		r.DriverInstanceID = instanceA
		r.Instances[0].Phase = sdk.FrigateAgentInstalled
	})
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State)
	assert.Equal(t, string(sdk.FrigatePhaseInstalling), got.Row.RolloutPhase)
	assert.True(t, got.Row.DisruptionStarted)
	assert.Equal(t, instanceA, got.Row.DriverInstanceID)
	assert.Equal(t, string(sdk.FrigateAgentInstalled), e.instance(got, instanceA).AgentPhase)

	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseSucceeded; r.Cancellable = false })
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateSucceeded, got.Row.State)
	assert.True(t, got.Row.CompletedAt.Valid)

	// A finished job is never touched again, even by a later contradictory answer.
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseInstalling })
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateSucceeded, e.job(job.Row.ID).Row.State)
}

func TestRunner_ResumesFromStoredOffset(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	failed := false
	e.updater.appendHook = func(id string, off int64) error {
		if id == instanceA && off == 2<<20 && !failed {
			failed = true
			return lostReply()
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStaging, got.Row.State)
	a := e.instance(got, instanceA)
	assert.Equal(t, StageStaging, a.StageState)
	assert.Equal(t, int64(2<<20), a.StagedBytes)
	assert.Contains(t, a.ErrorMessage, "Retrying")
	assert.Equal(t, StageStaged, e.instance(got, instanceB).StageState)

	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State, got.Row.ErrorMessage)
	// No byte was re-sent: each offset appears once per instance.
	assert.Equal(t, []int64{0, 1 << 20, 2 << 20}, e.updater.appendOffset[instanceA])
	assert.Equal(t, []int64{0, 1 << 20, 2 << 20}, e.updater.appendOffset[instanceB])
}

func TestRunner_ResumesFromControllerOffsetAfterRestart(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	// The controller already holds the first MiB (an earlier replica uploaded it
	// but never persisted the ack).
	failed := false
	e.updater.appendHook = func(id string, off int64) error {
		if off == 1<<20 && id == instanceA && !failed {
			failed = true
			return lostReply()
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	// Forget what Fleet stored; Begin must report the controller's offset.
	_, err := e.conn.ExecContext(e.ctx, `UPDATE frigate_update_instance SET staged_bytes=0 WHERE job_id=$1 AND instance_id=$2`, job.Row.ID, instanceA)
	require.NoError(t, err)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	assert.Equal(t, []int64{0, 1 << 20, 2 << 20}, e.updater.appendOffset[instanceA])
}

func TestRunner_MissingBlobFailsBeforeAnyController(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	require.NoError(t, e.store.Delete(e.pkg.ID))
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodePackageMissing, got.Row.ErrorCode)
	begin, starts, _ := e.updater.calls()
	assert.Zero(t, begin)
	assert.Zero(t, starts)
}

func TestRunner_StageRefusalFailsJobAndCleansUp(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.appendHook = func(id string, off int64) error {
		if id == instanceB {
			return refusal(codes.FailedPrecondition, "UPDATE_RELEASE_SIGNATURE_INVALID", "bad signature")
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, "UPDATE_RELEASE_SIGNATURE_INVALID", got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, instanceB)
	assert.False(t, got.Row.Retryable)
	assert.Equal(t, StageFailed, e.instance(got, instanceB).StageState)
	_, starts, _ := e.updater.calls()
	assert.Zero(t, starts)

	// The sweep ran at the start of the pass, before the job failed; the next
	// pass aborts the staged copies and clears the flag.
	e.svc.runOnce(e.ctx)
	e.updater.mu.Lock()
	assert.Empty(t, e.updater.stages)
	assert.NotEmpty(t, e.updater.aborts)
	e.updater.mu.Unlock()
	assert.False(t, e.job(job.Row.ID).Row.CleanupPending)

	// A fresh update can start again.
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
}

func TestRunner_DigestMismatchOnCommit(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.appendHook = func(id string, off int64) error { return nil }
	// Corrupt the controller's copy after the first chunk.
	var once sync.Once
	e.updater.appendHook = func(id string, off int64) error {
		if off == 2<<20 && id == instanceA {
			once.Do(func() {
				e.updater.mu.Lock()
				defer e.updater.mu.Unlock()
				e.updater.stages[id+"/"+job.Row.ID.String()].data[0] ^= 0xff
			})
		}
		return nil
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStageDigestMismatch, got.Row.ErrorCode)
	_, starts, _ := e.updater.calls()
	assert.Zero(t, starts)
}

func TestRunner_StageStalls(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.appendHook = func(id string, off int64) error { return lostReply() }
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStaging, e.job(job.Row.ID).Row.State)
	e.clock.advance(11 * time.Minute)
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeStageStalled, got.Row.ErrorCode)
}

func TestRunner_StartRefusalFailsJob(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.startHook = func(int) (bool, error) {
		return false, refusal(codes.FailedPrecondition, "UPDATE_ALREADY_RUNNING", "another update is running")
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, "UPDATE_ALREADY_RUNNING", got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, "Frigate refused to start the update")
	assert.Contains(t, got.Row.ErrorMessage, "another update is running")
}

func TestRunner_FleetPreStartRefusalIsNotBlamedOnFrigate(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.startHook = func(int) (bool, error) {
		return false, refusal(codes.FailedPrecondition, "FLEET_PAIR_MISMATCH", "controller b is not this device's CT1 cluster")
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, "FLEET_PAIR_MISMATCH", got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, "Fleet stopped the update before starting it")
	assert.NotContains(t, got.Row.ErrorMessage, "Frigate refused")
}

func TestRunner_AmbiguousStartAcceptedButReplyLost(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.startHook = func(int) (bool, error) { return true, lostReply() }
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStarting, got.Row.State)
	assert.True(t, got.Row.StartAttempted)
	assert.True(t, got.Row.UnreachableSince.Valid)

	// The next pass reads the rollout back instead of sending Start again.
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State)
	assert.False(t, got.Row.UnreachableSince.Valid)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 1, starts)
}

func TestRunner_AmbiguousStartNotAccepted(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.startHook = func(call int) (bool, error) {
		if call == 1 {
			return false, lostReply()
		}
		return true, nil
	}
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateStarting, e.job(job.Row.ID).Row.State)
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	require.Len(t, e.updater.startCalls, 2)
	assert.Equal(t, e.updater.startCalls[0].IdempotencyKey, e.updater.startCalls[1].IdempotencyKey)
}

func TestRunner_StartingSurvivesUnreachableStatus(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.updater.startHook = func(int) (bool, error) { return false, lostReply() }
	e.svc.runOnce(e.ctx)
	e.updater.mu.Lock()
	e.updater.statusErr = lostReply()
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStarting, got.Row.State)
	_, starts, _ := e.updater.calls()
	assert.Equal(t, 1, starts, "Start must not be re-sent while status is unreadable")
}

func TestRunner_PollToleratesUnavailableAndMaintenance(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))

	// Both instances unreachable: stay running, remember since when.
	e.updater.mu.Lock()
	e.updater.statusErr = lostReply()
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State)
	assert.True(t, got.Row.UnreachableSince.Valid)
	since := got.Row.UnreachableSince.Time
	e.clock.advance(time.Minute)
	e.svc.runOnce(e.ctx)
	assert.True(t, e.job(job.Row.ID).Row.UnreachableSince.Time.Equal(since), "the first failure time is kept")

	// Answered by the maintenance service while Frigate is stopped.
	e.updater.mu.Lock()
	e.updater.statusErr = nil
	e.updater.served = true
	e.updater.mu.Unlock()
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseInstalling; r.DisruptionStarted = true })
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.True(t, got.Row.ServedByMaintenance)
	assert.False(t, got.Row.UnreachableSince.Valid)
	assert.Equal(t, string(sdk.FrigatePhaseInstalling), got.Row.RolloutPhase)

	e.updater.mu.Lock()
	e.updater.served = false
	e.updater.mu.Unlock()
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseSucceeded })
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateSucceeded, got.Row.State)
	assert.False(t, got.Row.ServedByMaintenance)
}

func TestRunner_StaleRolloutIsIgnored(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseInstalling })
	e.svc.runOnce(e.ctx)
	newest := e.job(job.Row.ID).Row.RolloutUpdatedAt.Time

	// A lagging controller answers with an older view.
	e.updater.mu.Lock()
	e.updater.rollout.Phase = sdk.FrigatePhaseVerifying
	e.updater.rollout.UpdatedAt = newest.Add(-time.Minute)
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, string(sdk.FrigatePhaseInstalling), got.Row.RolloutPhase)
	assert.True(t, got.Row.RolloutUpdatedAt.Time.Equal(newest))
	assert.True(t, got.Row.LastContactAt.Valid)
}

func TestRunner_FinalRolloutAppliesDespiteClockRegression(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseInstalling })
	e.svc.runOnce(e.ctx)
	newest := e.job(job.Row.ID).Row.RolloutUpdatedAt.Time

	// A new driver whose clock is behind finishes the rollout.
	e.updater.mu.Lock()
	e.updater.rollout.Phase = sdk.FrigatePhaseSucceeded
	e.updater.rollout.UpdatedAt = newest.Add(-time.Hour)
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateSucceeded, got.Row.State)
	assert.Equal(t, string(sdk.FrigatePhaseSucceeded), got.Row.RolloutPhase)
	assert.True(t, got.Row.CompletedAt.Valid)
	assert.True(t, got.Row.RolloutUpdatedAt.Time.Equal(newest), "the newest updated_at is kept")
}

func TestRunner_DelayedRetryableFailureDoesNotOverwriteResumedRollout(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase, r.Retryable, r.ErrorCode = sdk.FrigatePhaseFailed, true, "STOP_TIMEOUT"
	})
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	require.Equal(t, StateFailed, got.Row.State)
	failedAt := got.Row.RolloutUpdatedAt.Time

	// Retry resumes the rollout.
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase, r.Retryable, r.ErrorCode = sdk.FrigatePhaseInstalling, false, ""
	})
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)

	// A lagging controller still reports the failure from before Retry.
	e.updater.mu.Lock()
	e.updater.rollout.Phase, e.updater.rollout.Retryable, e.updater.rollout.ErrorCode = sdk.FrigatePhaseFailed, true, "STOP_TIMEOUT"
	e.updater.rollout.UpdatedAt = failedAt
	e.updater.mu.Unlock()
	e.svc.runOnce(e.ctx)
	got = e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State)
	assert.Equal(t, string(sdk.FrigatePhaseInstalling), got.Row.RolloutPhase)
	assert.Empty(t, got.Row.ErrorCode)
}

func TestRunner_RolloutLostAfterRepeatedNotFound(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.mu.Lock()
	e.updater.rollout = nil
	e.updater.mu.Unlock()
	for range rolloutLostPolls - 1 {
		e.svc.runOnce(e.ctx)
		assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	}
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeRolloutLost, got.Row.ErrorCode)
}

func TestRunner_FailedRolloutRetryable(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) {
		r.Phase = sdk.FrigatePhaseFailed
		r.Retryable = true
		r.DisruptionStarted = true
		r.ErrorCode = "UPDATE_INSTALL_FAILED"
		r.ErrorMessage = "dpkg failed"
		r.NextStep = "Retry the update."
	})
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	assert.Equal(t, StateFailed, got.Row.State)
	assert.True(t, got.Row.Retryable)
	assert.False(t, got.Row.CompletedAt.Valid)
	assert.Equal(t, "UPDATE_INSTALL_FAILED", got.Row.ErrorCode)

	// A retryable failure still blocks a new update and keeps being polled.
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "other"})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)

	e.updater.retryHook = func(a sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Retryable, ro.ErrorCode, ro.ErrorMessage = sdk.FrigatePhaseInstalling, false, "", ""
		ro.UpdatedAt = ro.UpdatedAt.Add(time.Second)
		return ro
	}
	retried, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateRunning, retried.Row.State)
	require.Len(t, e.updater.retryCalls, 1)
	assert.Equal(t, "fleet-retry-"+job.Row.ID.String()+"-1", e.updater.retryCalls[0].IdempotencyKey)

	// The same operator key replays the same Frigate key; a new one advances.
	_, err = e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, e.updater.retryCalls[0].IdempotencyKey, e.updater.retryCalls[1].IdempotencyKey)
}

func TestRetryUpdate_NotRetryable(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	_, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Empty(t, e.updater.retryCalls)

	_, err = e.svc.RetryUpdate(e.ctx, job.Row.ID, "")
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = e.svc.RetryUpdate(e.ctx, uuid.New(), "op")
	assert.True(t, fleeterror.IsNotFoundError(err), err)
}

func TestRetryUpdate_RefusalIsReturnedIntact(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseFailed; r.Retryable = true })
	e.svc.runOnce(e.ctx)
	e.updater.retryErr = refusal(codes.FailedPrecondition, "UPDATE_NOT_RETRYABLE", "rollout is not retryable")
	_, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "op-1")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "UPDATE_NOT_RETRYABLE")
}

func TestCancelUpdate_BeforeStartIsLocal(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	cancelled, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, cancelled.Row.State)
	assert.Empty(t, e.updater.cancelCalls)

	// The runner does not stage it, and a new update may start.
	e.svc.runOnce(e.ctx)
	begin, _, _ := e.updater.calls()
	assert.Zero(t, begin)
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)

	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-2")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	again, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err, "same key replays")
	assert.Equal(t, StateCancelled, again.Row.State)
}

func TestCancelUpdate_RunningGoesToFrigate(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.cancelHook = func(a sdk.FrigateAction) sdk.FrigateRollout {
		ro := *e.updater.rollout
		ro.Phase, ro.Cancellable = sdk.FrigatePhaseCancelled, false
		ro.UpdatedAt = ro.UpdatedAt.Add(time.Second)
		return ro
	}
	got, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.NoError(t, err)
	assert.Equal(t, StateCancelled, got.Row.State)
	require.Len(t, e.updater.cancelCalls, 1)
	assert.Equal(t, "fleet-cancel-"+job.Row.ID.String()+"-1", e.updater.cancelCalls[0].IdempotencyKey)
}

func TestCancelUpdate_NotCancellable(t *testing.T) {
	e := newEnv(t)
	job := e.advanceToRunning(e.start("key-1"))
	e.updater.setRollout(func(r *sdk.FrigateRollout) { r.Phase = sdk.FrigatePhaseInstalling; r.Cancellable = false })
	e.svc.runOnce(e.ctx)
	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Empty(t, e.updater.cancelCalls)
}

func TestActions_Forbidden(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	e.auth.denySite = true
	for _, call := range []func() error{
		func() error { _, err := e.svc.GetUpdate(e.ctx, job.Row.ID); return err },
		func() error { _, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "k"); return err },
		func() error { _, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "k"); return err },
		func() error { _, err := e.svc.ListUpdates(e.ctx, e.device.ID, 0); return err },
	} {
		assert.True(t, fleeterror.IsForbiddenError(call()))
	}
}

func TestListUpdates(t *testing.T) {
	e := newEnv(t)
	first := e.start("key-1")
	_, err := e.svc.CancelUpdate(e.ctx, first.Row.ID, "c")
	require.NoError(t, err)
	second := e.start("key-2")
	jobs, err := e.svc.ListUpdates(e.ctx, e.device.ID, 0)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, second.Row.ID, jobs[0].Row.ID)
	assert.Len(t, jobs[0].Instances, 2)
}

func TestCrossOrgIsolation(t *testing.T) {
	e := newEnv(t)
	job := e.start("key-1")
	other := userContext(e.ctx, e.userID, e.orgID+1000, "intruder")
	_, err := e.svc.GetUpdate(other, job.Row.ID)
	assert.True(t, fleeterror.IsNotFoundError(err), err)
	_, err = e.svc.StartUpdate(other, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "x"})
	assert.True(t, fleeterror.IsNotFoundError(err), err)
	_, err = e.svc.GetSupport(other, e.device.ID, uuid.Nil)
	assert.True(t, fleeterror.IsNotFoundError(err), err)
}

func TestPackages_ListAndDelete(t *testing.T) {
	e := newEnv(t)
	pkgs, err := e.svc.ListPackages(e.ctx)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, e.pkg.ID, pkgs[0].ID)

	job := e.start("key-1")
	err = e.svc.DeletePackage(e.ctx, e.pkg.ID)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)

	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "c")
	require.NoError(t, err)

	e.auth.denyOrgWide = true
	assert.True(t, fleeterror.IsForbiddenError(e.svc.DeletePackage(e.ctx, e.pkg.ID)))
	e.auth.denyOrgWide = false
	e.auth.denyAny = true
	_, err = e.svc.ListPackages(e.ctx)
	assert.True(t, fleeterror.IsForbiddenError(err))
	e.auth.denyAny = false

	require.NoError(t, e.svc.DeletePackage(e.ctx, e.pkg.ID))
	assert.False(t, e.store.Exists(e.pkg.ID, e.pkg.SizeBytes))
	pkgs, err = e.svc.ListPackages(e.ctx)
	require.NoError(t, err)
	assert.Empty(t, pkgs)

	// History still resolves the deleted package.
	got := e.job(job.Row.ID)
	assert.Equal(t, e.pkg.ID, got.Package.ID)
	assert.True(t, got.Package.Deleted)

	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "after"})
	assert.True(t, fleeterror.IsNotFoundError(err), err)
	assert.True(t, fleeterror.IsNotFoundError(e.svc.DeletePackage(e.ctx, uuid.New())))
}
