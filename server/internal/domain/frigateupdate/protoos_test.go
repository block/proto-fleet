package frigateupdate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const protoOSTestTag = "v0.6.2-internal.14"

// newProtoOSEnv is newEnv with a proto-os package as e.pkg.
func newProtoOSEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.pkg, e.blob = e.seedProtoOSPackage(protoOSTestTag)
	return e
}

// startProtoOSRunning starts a ProtoOS job and runs the runner until every
// controller committed its copy.
func (e *env) startProtoOSRunning(key string) Job {
	e.t.Helper()
	job := e.start(key)
	return e.advanceToRunning(job)
}

func TestProtoOS_SucceedsOnAPair(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")

	assert.False(t, job.Row.Cancellable, "every copy is committed")
	assert.Equal(t, PackageKindProtoOS, job.Package.Kind)
	assert.Equal(t, "0.6.2~internal.14", job.Row.TargetVersion)
	e.updater.mu.Lock()
	for _, id := range []string{instanceA, instanceB} {
		m := e.updater.stages[id+"/"+job.Row.ID.String()].manifest
		assert.Equal(t, sdk.FrigatePackageProtoOS, m.Package)
		assert.Equal(t, protoOSTestTag, m.ReleaseTag)
		assert.Equal(t, sdk.ProtoOSAssetName(protoOSTestTag), m.AssetName)
		assert.Equal(t, "0.6.2~internal.14", m.TargetVersion)
	}
	assert.Empty(t, e.updater.startCalls, "a ProtoOS job sends no Start")
	e.updater.mu.Unlock()

	// One controller is done; the job waits for the other.
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "proto-os 0.6.2~internal.14 is installed")
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	b := e.instance(job, instanceB)
	assert.Equal(t, string(sdk.FrigateProtoOSJobQueued), b.ProtoosJobState)
	assert.Equal(t, "0.5.0", b.ProtoosInstalledVersion)

	e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobSucceeded, "proto-os 0.6.2~internal.14 is installed")
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	require.Equal(t, StateSucceeded, got.Row.State, "%s: %s", got.Row.ErrorCode, got.Row.ErrorMessage)
	assert.True(t, got.Row.CompletedAt.Valid)
	assert.False(t, got.Row.CleanupPending, "each controller owns its committed copy")
	for _, in := range got.Instances {
		assert.Equal(t, StageStaged, in.StageState)
		assert.Equal(t, string(sdk.FrigateProtoOSJobSucceeded), in.ProtoosJobState)
		assert.Equal(t, "0.6.2~internal.14", in.ProtoosInstalledVersion)
		assert.Equal(t, protoOSTestTag, in.ProtoosServedReleaseTag)
	}
}

func TestProtoOS_OnlyAFailedControllerHasAnInstanceError(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	assertState := func(id string, state sdk.FrigateProtoOSJobState, code, message string) {
		t.Helper()
		in := e.instance(job, id)
		assert.Equal(t, string(state), in.ProtoosJobState, id)
		assert.Equal(t, code, in.ErrorCode, id)
		assert.Equal(t, message, in.ErrorMessage, id)
	}

	for state, message := range map[sdk.FrigateProtoOSJobState]string{
		sdk.FrigateProtoOSJobQueued:     "Queued proto-os 0.6.2~internal.14.",
		sdk.FrigateProtoOSJobInstalling: "Installing proto-os 0.6.2~internal.14.",
	} {
		e.updater.finishProtoOS(instanceA, state, message)
		e.updater.finishProtoOS(instanceB, state, message)
		e.svc.runOnce(e.ctx)
		require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
		assertState(instanceA, state, "", "")
		assertState(instanceB, state, "", "")
	}

	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "proto-os 0.6.2~internal.14 is installed")
	e.svc.runOnce(e.ctx)
	assertState(instanceA, sdk.FrigateProtoOSJobSucceeded, "", "")

	e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobFailed, "dpkg exited 1")
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateFailed, e.job(job.Row.ID).Row.State)
	assertState(instanceA, sdk.FrigateProtoOSJobSucceeded, "", "")
	assertState(instanceB, sdk.FrigateProtoOSJobFailed, CodeProtoOSInstallFailed, "dpkg exited 1")
}

func TestProtoOS_OneFailingControllerFailsTheJobWithoutBlocking(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
	e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobFailed, "UPDATE_SIGNATURE_INVALID: no trusted ProtoOS release key validates the signature")

	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	require.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeProtoOSInstallFailed, got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, instanceB)
	assert.Contains(t, got.Row.ErrorMessage, "UPDATE_SIGNATURE_INVALID: no trusted ProtoOS release key validates the signature")
	assert.False(t, got.Row.Retryable)
	assert.NotEmpty(t, got.Row.NextStep)
	assert.Equal(t, CodeProtoOSInstallFailed, e.instance(job, instanceB).ErrorCode)

	_, err := e.svc.RetryUpdate(e.ctx, job.Row.ID, "retry-1")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Empty(t, e.updater.retryCalls)

	// A new job for the same package is the retry.
	next, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
	assert.Equal(t, StateStaging, next.Row.State)
}

func TestProtoOS_StageOnlyAndRetryAreRefused(t *testing.T) {
	e := newProtoOSEnv(t)
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, StageOnly: true, IdempotencyKey: "key-1"})
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)

	job := e.startProtoOSRunning("key-2")
	_, err = e.svc.RetryUpdate(e.ctx, job.Row.ID, "retry-1")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Empty(t, e.updater.retryCalls)
}

func TestProtoOS_CancelBeforeTheFirstCommit(t *testing.T) {
	e := newProtoOSEnv(t)
	e.updater.appendHook = func(string, int64) error { return lostReply() }
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateStaging, e.job(job.Row.ID).Row.State)
	require.True(t, e.job(job.Row.ID).Row.Cancellable)

	cancelled, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")

	require.NoError(t, err)
	assert.Equal(t, StateCancelled, cancelled.Row.State)
	e.updater.appendHook = nil
	e.svc.runOnce(e.ctx)
	assert.Zero(t, e.updater.commitCount(instanceA)+e.updater.commitCount(instanceB), "no copy commits after the cancel")
	e.updater.mu.Lock()
	assert.ElementsMatch(t, []string{instanceA, instanceB}, e.updater.aborts, "cleanup aborts the staged copies")
	assert.Empty(t, e.updater.cancelCalls)
	e.updater.mu.Unlock()
}

func TestProtoOS_CancelAfterTheFirstCommitIsRefused(t *testing.T) {
	e := newProtoOSEnv(t)
	e.updater.appendHook = func(id string, _ int64) error {
		if id == instanceB {
			return lostReply()
		}
		return nil
	}
	job := e.start("key-1")
	e.svc.runOnce(e.ctx)
	require.Equal(t, 1, e.updater.commitCount(instanceA))
	require.Zero(t, e.updater.commitCount(instanceB))

	_, err := e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")

	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.ErrorContains(t, err, "runs to its end")
	got := e.job(job.Row.ID)
	assert.Equal(t, StateStaging, got.Row.State)
	assert.False(t, got.Row.Cancellable)

	// The other copy still goes to its controller, and the job runs to its end.
	e.updater.appendHook = nil
	e.svc.runOnce(e.ctx)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	_, err = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-2")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
}

func TestProtoOS_RelayClosesTheCancelWindowBeforeTheStageCommand(t *testing.T) {
	e, relay := newRelayEnv(t)
	e.pkg, e.blob = e.seedProtoOSPackage(protoOSTestTag)
	relay.blob = e.blob
	job := e.start("key-1")
	var cancelErr error
	relay.callErr = func(int) error {
		_, cancelErr = e.svc.CancelUpdate(e.ctx, job.Row.ID, "op-1")
		return nil
	}

	e.svc.runOnce(e.ctx)

	assert.True(t, fleeterror.IsFailedPreconditionError(cancelErr), "the node may commit a copy as soon as it has the command: %v", cancelErr)
	assert.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)
	for _, target := range relay.calls()[0] {
		assert.Equal(t, sdk.FrigatePackageProtoOS, target.Manifest.Package)
	}
}

func TestProtoOS_ControllerThatNeverReportsTheJob(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
	e.updater.reportProtoOS(instanceB, func(p *sdk.FrigateProtoOSSupport) { p.Job = sdk.FrigateProtoOSJob{} })

	e.clock.advance(e.svc.cfg.StartTimeout)
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the controller still has time")

	e.clock.advance(time.Second)
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	require.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeProtoOSJobNotReported, got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, instanceB)
	assert.False(t, isBlocking(got.Row))
}

func TestProtoOS_InstallThatDoesNotFinish(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
	e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobInstalling, "Installing proto-os 0.6.2~internal.14.")

	e.clock.advance(e.svc.cfg.StartTimeout + time.Second)
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	require.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeProtoOSInstallTimeout, got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, "Installing proto-os 0.6.2~internal.14.")
}

func TestProtoOS_SilentControllerAbandonsTheJob(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
	e.updater.mu.Lock()
	e.updater.support[1] = sdk.FrigateInstanceSupport{InstanceID: instanceB, Err: "controller did not answer"}
	e.updater.mu.Unlock()

	e.clock.advance(e.svc.cfg.StartTimeout + time.Minute)
	e.svc.runOnce(e.ctx)
	require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "an unread controller does not fail the job")

	e.clock.advance(unreachableGiveUp)
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	assert.Equal(t, StateAbandoned, got.Row.State)
	assert.Equal(t, CodeControllersLost, got.Row.ErrorCode)
}

// silenceController makes a controller stop answering update support. The
// returned function makes it answer again with its last report.
func (e *env) silenceController(id string) (answerAgain func()) {
	e.t.Helper()
	e.updater.mu.Lock()
	defer e.updater.mu.Unlock()
	for i, in := range e.updater.support {
		if in.InstanceID == id {
			e.updater.support[i] = sdk.FrigateInstanceSupport{InstanceID: id, Err: "controller did not answer"}
			return func() {
				e.updater.mu.Lock()
				defer e.updater.mu.Unlock()
				e.updater.support[i] = in
			}
		}
	}
	e.t.Fatalf("controller %s not found", id)
	return nil
}

func TestProtoOS_SuccessOfASilentControllerCounts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		restart bool
	}{
		{"same runner", false},
		{"runner restarts between the polls", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newProtoOSEnv(t)
			job := e.startProtoOSRunning("key-1")
			e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
			e.svc.runOnce(e.ctx)
			require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State)

			e.silenceController(instanceA)
			if tc.restart {
				e.svc = NewService(e.conn, e.getter, e.store, nil, e.auth, nil, e.svc.cfg)
				e.svc.now = e.clock.now
			}
			e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobSucceeded, "")
			e.svc.runOnce(e.ctx)

			got := e.job(job.Row.ID)
			require.Equal(t, StateSucceeded, got.Row.State, "%s: %s", got.Row.ErrorCode, got.Row.ErrorMessage)
			assert.True(t, got.Row.CompletedAt.Valid)
		})
	}
}

func TestProtoOS_LaterAnswerReplacesASavedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report func(p *sdk.FrigateProtoOSSupport)
		code   string
		detail string
	}{
		{"another job", func(p *sdk.FrigateProtoOSSupport) {
			p.Job = sdk.FrigateProtoOSJob{JobID: "job-9", State: sdk.FrigateProtoOSJobSucceeded}
		}, CodeProtoOSJobNotReported, "did not report"},
		{"another version", func(p *sdk.FrigateProtoOSSupport) { p.InstalledVersion = "0.5.0" }, CodeProtoOSInstallTimeout, `has version "0.5.0" installed`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newProtoOSEnv(t)
			job := e.startProtoOSRunning("key-1")
			e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
			e.svc.runOnce(e.ctx)

			e.updater.reportProtoOS(instanceA, tc.report)
			e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobSucceeded, "")
			e.svc.runOnce(e.ctx)
			require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "the earlier success of %s no longer counts", instanceA)

			answerAgain := e.silenceController(instanceA)
			e.svc.runOnce(e.ctx)
			require.Equal(t, StateRunning, e.job(job.Row.ID).Row.State, "a silent controller keeps its latest answer, not the earlier success")

			answerAgain()
			e.clock.advance(e.svc.cfg.StartTimeout + time.Second)
			e.svc.runOnce(e.ctx)

			got := e.job(job.Row.ID)
			require.Equal(t, StateFailed, got.Row.State)
			assert.Equal(t, tc.code, got.Row.ErrorCode)
			assert.Contains(t, got.Row.ErrorMessage, instanceA)
			assert.Contains(t, got.Row.ErrorMessage, tc.detail)
		})
	}
}

func TestProtoOS_InstallDeadlineSkipsASilentFinishedController(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.finishProtoOS(instanceA, sdk.FrigateProtoOSJobSucceeded, "")
	e.updater.finishProtoOS(instanceB, sdk.FrigateProtoOSJobInstalling, "Installing proto-os 0.6.2~internal.14.")
	e.svc.runOnce(e.ctx)
	e.silenceController(instanceA)

	e.clock.advance(e.svc.cfg.StartTimeout + time.Second)
	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	require.Equal(t, StateFailed, got.Row.State)
	assert.Equal(t, CodeProtoOSInstallTimeout, got.Row.ErrorCode)
	assert.Contains(t, got.Row.ErrorMessage, instanceB)
	assert.NotContains(t, got.Row.ErrorMessage, instanceA)
}

func TestProtoOS_NoControllerAnswersMarksUnreachable(t *testing.T) {
	e := newProtoOSEnv(t)
	job := e.startProtoOSRunning("key-1")
	e.updater.mu.Lock()
	e.updater.supportErr = lostReply()
	e.updater.mu.Unlock()

	e.svc.runOnce(e.ctx)

	got := e.job(job.Row.ID)
	assert.Equal(t, StateRunning, got.Row.State)
	assert.True(t, got.Row.UnreachableSince.Valid)
}

func TestProtoOS_Eligibility(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(in *sdk.FrigateInstanceSupport)
		code   string
		reason string
	}{
		"not self supported": {func(in *sdk.FrigateInstanceSupport) {
			in.ProtoOS.SelfSupported, in.ProtoOS.SupportMessage = false, "this Frigate does not accept self-coordinated proto-os packages"
		}, CodeProtoOSUnsupported, "this Frigate does not accept self-coordinated proto-os packages"},
		"remote triggers off": {func(in *sdk.FrigateInstanceSupport) { in.RemoteTriggerAllowed = false }, CodeRemoteTriggerDisabled, "allow_remote_trigger"},
		"repository": {func(in *sdk.FrigateInstanceSupport) {
			in.ProtoOS.ReleaseRepositories = []string{"btc-mining/proto-fleet-internal"}
		}, CodeProtoOSRepositoryUntrusted, "block/proto-fleet"},
		"unknown peer":      {func(in *sdk.FrigateInstanceSupport) { in.PeerInstanceID = "frigate-c" }, CodeInstanceUnreachable, "frigate-c"},
		"controller update": {func(in *sdk.FrigateInstanceSupport) { in.ActiveJobID = "job-9" }, CodeUpdateInProgress, "job-9"},
		"ProtoOS update": {func(in *sdk.FrigateInstanceSupport) {
			in.ProtoOS.Job = sdk.FrigateProtoOSJob{JobID: "job-8", State: sdk.FrigateProtoOSJobInstalling}
		}, CodeUpdateInProgress, "job-8"},
		"unreadable controller": {func(in *sdk.FrigateInstanceSupport) {
			*in = sdk.FrigateInstanceSupport{InstanceID: in.InstanceID, Err: "down"}
		}, CodeInstanceUnreachable, "down"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newProtoOSEnv(t)
			e.updater.mu.Lock()
			tc.mutate(&e.updater.support[1])
			e.updater.mu.Unlock()

			support, err := e.svc.GetSupport(e.ctx, e.device.ID, e.pkg.ID)
			require.NoError(t, err)
			assert.False(t, support.Eligible)
			assert.Equal(t, tc.code, support.IneligibleCode)
			assert.Contains(t, support.IneligibleReason, tc.reason)

			_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-1"})
			assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
			assert.ErrorContains(t, err, tc.code)
		})
	}
}

func TestProtoOS_EligibilityIgnoresControllerSoftwareConditions(t *testing.T) {
	e := newProtoOSEnv(t)
	e.updater.mu.Lock()
	for i := range e.updater.support {
		in := &e.updater.support[i]
		in.SelfCoordinationSupported = false
		in.EtcdMembersHealthy = 0
		in.ClusterID = "cluster-" + in.InstanceID
		in.TrustedReleaseKeyIDs = []string{"sha256:" + "f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0"}
		in.HelperAvailable = false
		in.SpoolFreeBytes = 0
	}
	e.updater.mu.Unlock()

	support, err := e.svc.GetSupport(e.ctx, e.device.ID, e.pkg.ID)

	require.NoError(t, err)
	assert.True(t, support.Eligible, "%s: %s", support.IneligibleCode, support.IneligibleReason)
}

func TestProtoOS_OldFleetNodeIsRefused(t *testing.T) {
	e := newProtoOSEnv(t)
	e.updater.protoOSCheckErr = fleeterror.NewFailedPreconditionError("Upgrade the Fleet Node, then try again.")

	_, err := e.svc.GetSupport(e.ctx, e.device.ID, e.pkg.ID)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "key-1"})
	assert.ErrorContains(t, err, "Upgrade the Fleet Node")

	// Controller software updates do not need the newer node.
	frigate, _ := e.seedPackage("2.1.0")
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: frigate.ID, IdempotencyKey: "key-2"})
	require.NoError(t, err)
}

func TestProtoOS_AndControllerSoftwareJobsExcludeEachOther(t *testing.T) {
	e := newEnv(t)
	protoOS, _ := e.seedProtoOSPackage(protoOSTestTag)

	frigateJob := e.start("frigate-1")
	_, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: protoOS.ID, IdempotencyKey: "protoos-1"})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "a controller software job blocks a ProtoOS job: %v", err)

	_, err = e.svc.CancelUpdate(e.ctx, frigateJob.Row.ID, "op-1")
	require.NoError(t, err)
	protoOSJob, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: protoOS.ID, IdempotencyKey: "protoos-2"})
	require.NoError(t, err)
	_, err = e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: "frigate-2"})
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "a ProtoOS job blocks a controller software job: %v", err)

	// A controller that still installs ProtoOS refuses a controller software update.
	e.advanceToRunning(protoOSJob)
	support, err := e.svc.GetSupport(e.ctx, e.device.ID, e.pkg.ID)
	require.NoError(t, err)
	assert.Equal(t, CodeUpdateInProgress, support.IneligibleCode)
}

func TestProtoOS_RunningActivityEntryNamesTheInstall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		newEnv func(*testing.T) *env
		want   string
	}{
		{"ProtoOS", newProtoOSEnv, "The controllers committed the package for ProtoOS 0.6.2~internal.14 and are installing it"},
		{"controller software", newEnv, "The controller accepted the update to 2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.newEnv(t)
			e.svc.activity = activity.NewService(sqlstores.NewSQLActivityStore(e.conn))
			job := e.advanceToRunning(e.start("key-1"))

			var description string
			require.NoError(t, e.conn.QueryRowContext(e.ctx,
				`SELECT description FROM activity_log WHERE event_type='frigate_update_running' AND metadata->>'job_id'=$1`,
				job.Row.ID.String()).Scan(&description))
			assert.Equal(t, tc.want, description)
		})
	}
}

var _ interfaces.ProtoOSUpdateChecker = (*fakeUpdater)(nil)
