package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// frigateUpdaterDevice records what crossed the plugin boundary and returns
// canned results.
type frigateUpdaterDevice struct {
	Device
	err      error
	supports []FrigateInstanceSupport

	instance string
	manifest FrigateManifest
	appended FrigateStageAppend
	ref      FrigateStageRef
	start    FrigateStartRequest
	action   FrigateAction
	jobID    string
	method   string
}

const (
	testFrigateJob    = "0b9f0f4e-1c2d-4e5f-8a6b-7c8d9e0f1a2b"
	testFrigateFP     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testFrigateFPA    = "1111111111111111111111111111111111111111111111111111111111111111"
	testFrigateFPB    = "2222222222222222222222222222222222222222222222222222222222222222"
	testFrigateDigest = "3333333333333333333333333333333333333333333333333333333333333333"
)

var (
	frigateRoundTripTime = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	frigateRollout       = FrigateRollout{
		JobID: testFrigateJob, TargetVersion: "2.0.0", Phase: FrigatePhaseMigrating,
		Instances: []FrigateInstanceProgress{
			{InstanceID: "a", Phase: FrigateAgentMigrated, LastContact: frigateRoundTripTime, RunningVersion: "1.0.0", MigrationExecuted: true, ManifestSHA256: testFrigateFPA},
			{InstanceID: "b", Phase: FrigateAgentFailed, ErrorCode: "E", ErrorMessage: "boom", ContactStale: true},
		},
		DriverInstanceID: "a", MigrationExecutorInstanceID: "a", ErrorCode: "UPDATE_MIGRATE_FAILED", ErrorMessage: "migration failed", NextStep: "retry",
		StageOnly: true, DisruptionStarted: true, Retryable: true, Cancellable: true, AutomaticRetries: 2, Actor: "proto-fleet:alice",
		CreatedAt: frigateRoundTripTime, UpdatedAt: frigateRoundTripTime.Add(time.Minute), StoppedAt: frigateRoundTripTime.Add(2 * time.Minute), ExpiresAt: frigateRoundTripTime.Add(time.Hour),
		FromVersion: "1.0.0", PackageSHA256: testFrigateDigest, ReleaseKeyID: "sha256:abc",
	}
)

func (d *frigateUpdaterDevice) FrigateUpdateSupport(context.Context) ([]FrigateInstanceSupport, error) {
	d.method = "support"
	if d.err != nil {
		return nil, d.err
	}
	if d.supports != nil {
		return d.supports, nil
	}
	return []FrigateInstanceSupport{
		{InstanceID: "a", PeerInstanceID: "b", RunningVersion: "1.0.0", Architecture: "arm64", InstallationType: FrigateInstallationDebianPackage, HelperAvailable: true, SupportMessage: "ok", ClusterID: "cluster", ActiveJobID: testFrigateJob, SpoolFreeBytes: 1 << 30, EtcdMembersTotal: 3, EtcdMembersHealthy: 2, TrustedReleaseKeyIDs: []string{"sha256:abc", "sha256:def"}, SelfCoordinationSupported: true, RemoteTriggerAllowed: true},
		{InstanceID: "b", Err: "controller unreachable"},
	}, nil
}

func (d *frigateUpdaterDevice) FrigateStageBegin(_ context.Context, instance string, m FrigateManifest) (FrigateStageProgress, error) {
	d.method, d.instance, d.manifest = "begin", instance, m
	return FrigateStageProgress{JobID: m.JobID, ManifestSHA256: testFrigateFP, NextOffset: 1024, SizeBytes: m.SizeBytes}, d.err
}

func (d *frigateUpdaterDevice) FrigateStageAppend(_ context.Context, instance string, a FrigateStageAppend) (FrigateStageProgress, error) {
	d.method, d.instance, d.appended = "append", instance, a
	return FrigateStageProgress{JobID: a.JobID, ManifestSHA256: a.ManifestSHA256, NextOffset: a.Offset + int64(len(a.Chunk)), SizeBytes: 4096}, d.err
}

func (d *frigateUpdaterDevice) FrigateStageCommit(_ context.Context, instance string, r FrigateStageRef) (FrigateStageCommit, error) {
	d.method, d.instance, d.ref = "commit", instance, r
	return FrigateStageCommit{JobID: r.JobID, BytesStaged: 4096, SHA256: testFrigateDigest}, d.err
}

func (d *frigateUpdaterDevice) FrigateStageAbort(_ context.Context, instance string, r FrigateStageRef) error {
	d.method, d.instance, d.ref = "abort", instance, r
	return d.err
}

func (d *frigateUpdaterDevice) FrigateUpdateStart(_ context.Context, r FrigateStartRequest) (FrigateRollout, error) {
	d.method, d.start = "start", r
	return frigateRollout, d.err
}

func (d *frigateUpdaterDevice) FrigateUpdateStatus(_ context.Context, jobID string) (FrigateStatus, error) {
	d.method, d.jobID = "status", jobID
	return FrigateStatus{Rollout: frigateRollout, ServedByMaintenance: true, AnsweredBy: "b"}, d.err
}

func (d *frigateUpdaterDevice) FrigateUpdateRetry(_ context.Context, a FrigateAction) (FrigateRollout, error) {
	d.method, d.action = "retry", a
	return frigateRollout, d.err
}

func (d *frigateUpdaterDevice) FrigateUpdateCancel(_ context.Context, a FrigateAction) (FrigateRollout, error) {
	d.method, d.action = "cancel", a
	return frigateRollout, d.err
}

func TestFrigateUpdateRoundTripsAllOperations(t *testing.T) {
	device := &frigateUpdaterDevice{}
	client := outcomeRPCDevice(t, device)
	ctx := t.Context()

	supports, err := client.FrigateUpdateSupport(ctx)
	require.NoError(t, err)
	require.Len(t, supports, 2)
	require.Equal(t, "a", supports[0].InstanceID)
	require.Equal(t, FrigateInstallationDebianPackage, supports[0].InstallationType)
	require.Equal(t, []string{"sha256:abc", "sha256:def"}, supports[0].TrustedReleaseKeyIDs)
	require.EqualValues(t, 1<<30, supports[0].SpoolFreeBytes)
	require.EqualValues(t, 2, supports[0].EtcdMembersHealthy)
	require.True(t, supports[0].SelfCoordinationSupported && supports[0].RemoteTriggerAllowed && supports[0].HelperAvailable)
	require.Equal(t, "controller unreachable", supports[1].Err)

	manifest := FrigateManifest{
		JobID: testFrigateJob, InstanceID: "b", ExpectedInstanceIDs: []string{"a", "b"}, ReleaseTag: "v2", TargetVersion: "2.0.0", AssetID: 7, AssetName: "frigate.deb",
		SHA256: testFrigateDigest, SizeBytes: 4096, Actor: "fleet:alice", Coordination: FrigateCoordinationSelf, ReleaseStatement: []byte("stmt"), ReleaseSignature: []byte("sig"),
	}
	progress, err := client.FrigateStageBegin(ctx, "b", manifest)
	require.NoError(t, err)
	require.Equal(t, FrigateStageProgress{JobID: testFrigateJob, ManifestSHA256: testFrigateFP, NextOffset: 1024, SizeBytes: 4096}, progress)
	require.Equal(t, "b", device.instance)
	require.Equal(t, manifest, device.manifest)

	progress, err = client.FrigateStageAppend(ctx, "b", FrigateStageAppend{JobID: testFrigateJob, ManifestSHA256: testFrigateFP, Offset: 1024, Chunk: []byte("hello"), Actor: "fleet:alice"})
	require.NoError(t, err)
	require.EqualValues(t, 1029, progress.NextOffset)
	require.Equal(t, []byte("hello"), device.appended.Chunk)
	require.EqualValues(t, 1024, device.appended.Offset)
	require.Equal(t, "fleet:alice", device.appended.Actor)

	commit, err := client.FrigateStageCommit(ctx, "a", FrigateStageRef{JobID: testFrigateJob, ManifestSHA256: testFrigateFP, Actor: "fleet:alice"})
	require.NoError(t, err)
	require.Equal(t, FrigateStageCommit{JobID: testFrigateJob, BytesStaged: 4096, SHA256: testFrigateDigest}, commit)
	require.Equal(t, "a", device.instance)

	require.NoError(t, client.FrigateStageAbort(ctx, "a", FrigateStageRef{JobID: testFrigateJob, ManifestSHA256: testFrigateFP}))
	require.Equal(t, "abort", device.method)

	start := FrigateStartRequest{JobID: testFrigateJob, TargetVersion: "2.0.0", ExpectedInstanceIDs: []string{"a", "b"}, StageOnly: true, IdempotencyKey: "start-key", ExpectedManifestSHA256: map[string]string{"a": testFrigateFPA, "b": testFrigateFPB}, Actor: "fleet:alice"}
	rollout, err := client.FrigateUpdateStart(ctx, start)
	require.NoError(t, err)
	require.Equal(t, frigateRollout, rollout)
	require.Equal(t, start, device.start)

	got, err := client.FrigateUpdateStatus(ctx, "")
	require.NoError(t, err)
	require.Equal(t, FrigateStatus{Rollout: frigateRollout, ServedByMaintenance: true, AnsweredBy: "b"}, got)
	require.Empty(t, device.jobID)
	_, err = client.FrigateUpdateStatus(ctx, testFrigateJob)
	require.NoError(t, err)
	require.Equal(t, testFrigateJob, device.jobID)

	action := FrigateAction{JobID: testFrigateJob, IdempotencyKey: "retry-key", Actor: "fleet:alice"}
	rollout, err = client.FrigateUpdateRetry(ctx, action)
	require.NoError(t, err)
	require.Equal(t, frigateRollout, rollout)
	require.Equal(t, action, device.action)
	action.IdempotencyKey = "cancel-key"
	_, err = client.FrigateUpdateCancel(ctx, action)
	require.NoError(t, err)
	require.Equal(t, "cancel", device.method)
	require.Equal(t, action, device.action)
}

func TestFrigateRolloutTimestampsRoundTripZeroAndSet(t *testing.T) {
	device := &frigateUpdaterDevice{}
	client := outcomeRPCDevice(t, device)
	got, err := client.FrigateUpdateStatus(t.Context(), testFrigateJob)
	require.NoError(t, err)
	require.True(t, got.Rollout.CreatedAt.Equal(frigateRoundTripTime))
	require.True(t, got.Rollout.Instances[1].LastContact.IsZero(), "an unset timestamp must stay zero")
	require.True(t, got.Rollout.Instances[0].LastContact.Equal(frigateRoundTripTime))
}

func TestFrigateRefusalRoundTripsReasonAndCode(t *testing.T) {
	for _, tc := range []struct {
		reason string
		code   codes.Code
	}{
		{"UPDATE_NOT_STAGED", codes.FailedPrecondition},
		{"UPDATE_SIGNATURE_INVALID", codes.FailedPrecondition},
		{"UPDATE_ETCD_UNHEALTHY", codes.Unavailable},
		{"UPDATE_NOT_FOUND", codes.NotFound},
		{"UPDATE_MANIFEST_INVALID", codes.InvalidArgument},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			device := &frigateUpdaterDevice{err: &FrigateRefusal{Reason: tc.reason, Message: "no thanks", Code: tc.code}}
			client := outcomeRPCDevice(t, device)
			calls := map[string]func() error{
				"start": func() error {
					_, err := client.FrigateUpdateStart(t.Context(), validStart())
					return err
				},
				"status": func() error { _, err := client.FrigateUpdateStatus(t.Context(), testFrigateJob); return err },
				"retry":  func() error { _, err := client.FrigateUpdateRetry(t.Context(), validAction()); return err },
				"cancel": func() error { _, err := client.FrigateUpdateCancel(t.Context(), validAction()); return err },
				"begin":  func() error { _, err := client.FrigateStageBegin(t.Context(), "a", validManifest()); return err },
				"append": func() error {
					_, err := client.FrigateStageAppend(t.Context(), "a", validAppend())
					return err
				},
				"commit": func() error {
					_, err := client.FrigateStageCommit(t.Context(), "a", validRef())
					return err
				},
				"abort":   func() error { return client.FrigateStageAbort(t.Context(), "a", validRef()) },
				"support": func() error { _, err := client.FrigateUpdateSupport(t.Context()); return err },
			}
			for name, call := range calls {
				err := call()
				refusal := AsFrigateRefusal(err)
				require.NotNil(t, refusal, "%s: %v", name, err)
				require.Equal(t, tc.reason, refusal.Reason, name)
				require.Equal(t, tc.code, refusal.Code, name)
				require.Equal(t, "no thanks", refusal.Message, name)
			}
		})
	}
}

func validManifest() FrigateManifest {
	return FrigateManifest{
		JobID: testFrigateJob, InstanceID: "a", ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2", SHA256: testFrigateDigest, SizeBytes: 1,
		Coordination: FrigateCoordinationSelf, ReleaseStatement: []byte("stmt"), ReleaseSignature: []byte("sig"),
	}
}

func validStart() FrigateStartRequest {
	return FrigateStartRequest{JobID: testFrigateJob, TargetVersion: "2", ExpectedInstanceIDs: []string{"a", "b"}, IdempotencyKey: "key-1", ExpectedManifestSHA256: map[string]string{"a": testFrigateFPA}}
}

func validAction() FrigateAction {
	return FrigateAction{JobID: testFrigateJob, IdempotencyKey: "key-1"}
}

func validRef() FrigateStageRef {
	return FrigateStageRef{JobID: testFrigateJob, ManifestSHA256: testFrigateFP}
}

func validAppend() FrigateStageAppend {
	return FrigateStageAppend{JobID: testFrigateJob, ManifestSHA256: testFrigateFP, Chunk: []byte{1}}
}

func TestFrigateNonRefusalErrorsAreNotRefusals(t *testing.T) {
	device := &frigateUpdaterDevice{err: status.Error(codes.Unavailable, "controller offline")}
	client := outcomeRPCDevice(t, device)
	_, err := client.FrigateUpdateStatus(t.Context(), testFrigateJob)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Nil(t, AsFrigateRefusal(err))
	device.err = errors.New("plain failure")
	_, err = client.FrigateUpdateStatus(t.Context(), testFrigateJob)
	require.Error(t, err)
	require.Nil(t, AsFrigateRefusal(err))
}

func TestFrigateUpdateRejectsInvalidRequestsBeforeTheDevice(t *testing.T) {
	device := &frigateUpdaterDevice{}
	client := outcomeRPCDevice(t, device)
	ctx := t.Context()
	begin := func(mutate func(*FrigateManifest)) func() error {
		return func() error {
			m := validManifest()
			mutate(&m)
			_, err := client.FrigateStageBegin(ctx, "a", m)
			return err
		}
	}
	protoOSBegin := func(mutate func(*FrigateManifest)) func() error {
		return func() error {
			m := validProtoOSManifest()
			mutate(&m)
			_, err := client.FrigateStageBegin(ctx, "a", m)
			return err
		}
	}
	start := func(mutate func(*FrigateStartRequest)) func() error {
		return func() error {
			r := validStart()
			mutate(&r)
			_, err := client.FrigateUpdateStart(ctx, r)
			return err
		}
	}
	retry := func(mutate func(*FrigateAction)) func() error {
		return func() error {
			a := validAction()
			mutate(&a)
			_, err := client.FrigateUpdateRetry(ctx, a)
			return err
		}
	}
	cancel := func(mutate func(*FrigateAction)) func() error {
		return func() error {
			a := validAction()
			mutate(&a)
			_, err := client.FrigateUpdateCancel(ctx, a)
			return err
		}
	}
	appendCall := func(mutate func(*FrigateStageAppend)) func() error {
		return func() error {
			a := validAppend()
			mutate(&a)
			_, err := client.FrigateStageAppend(ctx, "a", a)
			return err
		}
	}
	ref := func(mutate func(*FrigateStageRef)) (func() error, func() error) {
		build := func() FrigateStageRef { r := validRef(); mutate(&r); return r }
		return func() error { _, err := client.FrigateStageCommit(ctx, "a", build()); return err },
			func() error { return client.FrigateStageAbort(ctx, "a", build()) }
	}
	badRefCommit, badRefAbort := ref(func(r *FrigateStageRef) { r.ManifestSHA256 = "" })
	upperFingerprint := strings.ToUpper(testFrigateFP)
	for name, call := range map[string]func() error{
		"manifest without job":         begin(func(m *FrigateManifest) { m.JobID = "" }),
		"manifest job not a uuid":      begin(func(m *FrigateManifest) { m.JobID = "job-1" }),
		"manifest uppercase uuid":      begin(func(m *FrigateManifest) { m.JobID = strings.ToUpper(testFrigateJob) }),
		"manifest zero size":           begin(func(m *FrigateManifest) { m.SizeBytes = 0 }),
		"manifest oversize":            begin(func(m *FrigateManifest) { m.SizeBytes = 1<<30 + 1 }),
		"manifest without hash":        begin(func(m *FrigateManifest) { m.SHA256 = "" }),
		"manifest short hash":          begin(func(m *FrigateManifest) { m.SHA256 = "abc" }),
		"manifest uppercase hash":      begin(func(m *FrigateManifest) { m.SHA256 = strings.ToUpper(testFrigateFP) }),
		"manifest legacy coordination": begin(func(m *FrigateManifest) { m.Coordination = FrigateCoordinationLegacy }),
		"manifest without instances":   begin(func(m *FrigateManifest) { m.ExpectedInstanceIDs = nil }),
		"manifest too many instances": begin(func(m *FrigateManifest) {
			m.ExpectedInstanceIDs = []string{"a", "1", "2", "3", "4", "5", "6", "7", "8"}
		}),
		"manifest duplicate instances": begin(func(m *FrigateManifest) { m.ExpectedInstanceIDs = []string{"a", "a"} }),
		"manifest omits target":        begin(func(m *FrigateManifest) { m.ExpectedInstanceIDs = []string{"b"} }),
		"manifest without statement":   begin(func(m *FrigateManifest) { m.ReleaseStatement = nil }),
		"manifest huge statement":      begin(func(m *FrigateManifest) { m.ReleaseStatement = make([]byte, 4<<10+1) }),
		"manifest without signature":   begin(func(m *FrigateManifest) { m.ReleaseSignature = nil }),
		"manifest huge signature":      begin(func(m *FrigateManifest) { m.ReleaseSignature = make([]byte, 513) }),
		"manifest wrong instance":      begin(func(m *FrigateManifest) { m.InstanceID = "b" }),
		"manifest unknown package":     begin(func(m *FrigateManifest) { m.Package = "firmware" }),
		"proto-os legacy coordination": protoOSBegin(func(m *FrigateManifest) { m.Coordination = FrigateCoordinationLegacy }),
		"proto-os without tag":         protoOSBegin(func(m *FrigateManifest) { m.ReleaseTag = "" }),
		"proto-os tag without v": protoOSBegin(func(m *FrigateManifest) {
			m.ReleaseTag, m.AssetName, m.TargetVersion = "0.6.2", "proto-os_0.6.2_all.deb", "0.6.2"
		}),
		"proto-os other asset":         protoOSBegin(func(m *FrigateManifest) { m.AssetName = "proto-os_v0.6.2-internal.14_arm64.deb" }),
		"proto-os tag as version":      protoOSBegin(func(m *FrigateManifest) { m.TargetVersion = "0.6.2-internal.14" }),
		"proto-os without statement":   protoOSBegin(func(m *FrigateManifest) { m.ReleaseStatement = nil }),
		"empty target instance":        func() error { _, err := client.FrigateStageBegin(ctx, "", validManifest()); return err },
		"empty chunk":                  appendCall(func(a *FrigateStageAppend) { a.Chunk = nil }),
		"oversize chunk":               appendCall(func(a *FrigateStageAppend) { a.Chunk = make([]byte, FrigateStageChunkBytes+1) }),
		"negative offset":              appendCall(func(a *FrigateStageAppend) { a.Offset = -1 }),
		"append without fingerprint":   appendCall(func(a *FrigateStageAppend) { a.ManifestSHA256 = "" }),
		"append uppercase fingerprint": appendCall(func(a *FrigateStageAppend) { a.ManifestSHA256 = upperFingerprint }),
		"commit without fingerprint":   badRefCommit,
		"abort without fingerprint":    badRefAbort,
		"commit without job": func() error {
			_, err := client.FrigateStageCommit(ctx, "a", FrigateStageRef{ManifestSHA256: testFrigateFP})
			return err
		},
		"start without instances": start(func(r *FrigateStartRequest) { r.ExpectedInstanceIDs = nil }),
		"start too many instances": start(func(r *FrigateStartRequest) {
			r.ExpectedInstanceIDs = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
		}),
		"start duplicate instances":  start(func(r *FrigateStartRequest) { r.ExpectedInstanceIDs = []string{"a", "a"} }),
		"start without version":      start(func(r *FrigateStartRequest) { r.TargetVersion = "" }),
		"start job not a uuid":       start(func(r *FrigateStartRequest) { r.JobID = "job" }),
		"start without key":          start(func(r *FrigateStartRequest) { r.IdempotencyKey = "" }),
		"start long key":             start(func(r *FrigateStartRequest) { r.IdempotencyKey = strings.Repeat("k", 129) }),
		"start non ascii key":        start(func(r *FrigateStartRequest) { r.IdempotencyKey = "k\u00e9y" }),
		"start control char key":     start(func(r *FrigateStartRequest) { r.IdempotencyKey = "k\ney" }),
		"start fingerprint stranger": start(func(r *FrigateStartRequest) { r.ExpectedManifestSHA256 = map[string]string{"z": testFrigateFPA} }),
		"start fingerprint not hex":  start(func(r *FrigateStartRequest) { r.ExpectedManifestSHA256 = map[string]string{"a": "fp-a"} }),
		"retry without job":          retry(func(a *FrigateAction) { a.JobID = "" }),
		"retry without key":          retry(func(a *FrigateAction) { a.IdempotencyKey = "" }),
		"retry long key":             retry(func(a *FrigateAction) { a.IdempotencyKey = strings.Repeat("k", 129) }),
		"cancel without job":         cancel(func(a *FrigateAction) { a.JobID = "" }),
		"cancel without key":         cancel(func(a *FrigateAction) { a.IdempotencyKey = "" }),
		"overlong job":               retry(func(a *FrigateAction) { a.JobID = strings.Repeat("x", 1000) }),
		"status job not a uuid":      func() error { _, err := client.FrigateUpdateStatus(ctx, "latest"); return err },
	} {
		err := call()
		require.Error(t, err, name)
		require.Equal(t, codes.InvalidArgument, status.Code(err), "%s: %v", name, err)
	}
	require.Empty(t, device.method, "an invalid request reached the device")

	// Boundary values are accepted.
	maximal := validStart()
	maximal.IdempotencyKey = strings.Repeat("k", 128)
	_, err := client.FrigateUpdateStart(ctx, maximal)
	require.NoError(t, err)
	m := validManifest()
	m.SizeBytes = 1 << 30
	m.ReleaseStatement = make([]byte, 4<<10)
	m.ReleaseSignature = make([]byte, 512)
	m.ExpectedInstanceIDs = []string{"a", "1", "2", "3", "4", "5", "6", "7"}
	_, err = client.FrigateStageBegin(ctx, "a", m)
	require.NoError(t, err)
	_, err = client.FrigateStageBegin(ctx, "a", validProtoOSManifest())
	require.NoError(t, err)
	_, err = client.FrigateUpdateStatus(ctx, "")
	require.NoError(t, err)
}

func TestProtoOSReleaseNames(t *testing.T) {
	require.Equal(t, "0.6.2~internal.14", ProtoOSDebianVersion("v0.6.2-internal.14"))
	require.Equal(t, "1.0.0~rc-1", ProtoOSDebianVersion("v1.0.0-rc-1"), "only the first hyphen becomes a tilde")
	require.Equal(t, "0.6.2", ProtoOSDebianVersion("v0.6.2"))
	require.Equal(t, "proto-os_v0.6.2-internal.14_all.deb", ProtoOSAssetName("v0.6.2-internal.14"))
}

func TestFrigateManifestMustTargetTheCalledInstance(t *testing.T) {
	require.NoError(t, validManifest().ValidateForInstance("a"))
	require.Error(t, validManifest().ValidateForInstance("b"))
}

func TestFrigateSupportErrorReasonRoundTrips(t *testing.T) {
	device := &frigateUpdaterDevice{supports: []FrigateInstanceSupport{
		{InstanceID: "a", Err: "frigate refused the request (UPDATE_ETCD_UNHEALTHY): etcd is down", ErrReason: "UPDATE_ETCD_UNHEALTHY"},
		{InstanceID: "b", Err: "rpc error: code = Unavailable [x] desc = down"},
		{InstanceID: "c", Err: "[lowercase] not a reason"},
	}}
	client := outcomeRPCDevice(t, device)
	got, err := client.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Equal(t, "UPDATE_ETCD_UNHEALTHY", got[0].ErrReason)
	require.Equal(t, "frigate refused the request (UPDATE_ETCD_UNHEALTHY): etcd is down", got[0].Err)
	require.Empty(t, got[1].ErrReason)
	require.Equal(t, "rpc error: code = Unavailable [x] desc = down", got[1].Err)
	require.Empty(t, got[2].ErrReason)
	require.Equal(t, "[lowercase] not a reason", got[2].Err)
}

func TestFrigateUpdateIsUnimplementedWithoutTheCapability(t *testing.T) {
	client := outcomeRPCDevice(t, fakeDevice{})
	_, err := client.FrigateUpdateStatus(t.Context(), testFrigateJob)
	require.Error(t, err)
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = client.FrigateUpdateSupport(t.Context())
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestFrigateUpdateRequiresAKnownDevice(t *testing.T) {
	server := &DriverGRPCServer{devices: map[string]Device{}}
	_, err := server.FrigateServerUpdate(t.Context(), nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = (&DriverGRPCServer{}).FrigateServerUpdate(t.Context(), nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAsFrigateRefusalOnlyMatchesTheFrigateDomain(t *testing.T) {
	require.Nil(t, AsFrigateRefusal(nil))
	require.Nil(t, AsFrigateRefusal(errors.New("x")))
	require.Nil(t, AsFrigateRefusal(status.Error(codes.FailedPrecondition, "no details")))
	wrapped := errors.Join(errors.New("context"), &FrigateRefusal{Reason: "UPDATE_ACTIVE_JOB", Code: codes.FailedPrecondition})
	require.Equal(t, "UPDATE_ACTIVE_JOB", AsFrigateRefusal(wrapped).Reason)
	// A refusal with an OK code is still sent as a failure.
	require.Equal(t, codes.FailedPrecondition, (&FrigateRefusal{Reason: "R"}).GRPCStatus().Code())
	require.Contains(t, (&FrigateRefusal{Reason: "R", Message: "m"}).Error(), "R")
	require.Contains(t, (&FrigateRefusal{Reason: "R"}).Error(), "R")
}

func TestFrigateAuditActor(t *testing.T) {
	require.Equal(t, "proto-fleet:alice@example.com", FrigateAuditActor("fleet:alice@example.com"))
	require.Equal(t, "proto-fleet:alice@example.com", FrigateAuditActor("  alice@example.com "))
	require.Equal(t, "proto-fleet", FrigateAuditActor(""))
	require.Equal(t, "proto-fleet", FrigateAuditActor("fleet:"))
	require.Equal(t, "proto-fleet:a?b?c", FrigateAuditActor("a\nb\u00e9c"))
	long := FrigateAuditActor(strings.Repeat("x", 5000))
	require.Less(t, len(long), 300)
	require.True(t, strings.HasPrefix(long, "proto-fleet:xxx"))
	require.Equal(t, "proto-fleet:a?b", FrigateAuditActor("a\x01b"))
}

func TestFrigateRolloutPhaseTerminal(t *testing.T) {
	for phase, terminal := range map[FrigateRolloutPhase]bool{
		FrigatePhaseVerifying: false, FrigatePhaseStopping: false, FrigatePhaseInstalling: false, FrigatePhaseMigrating: false, FrigatePhaseStarting: false,
		FrigatePhaseSucceeded: true, FrigatePhaseFailed: true, FrigatePhaseCancelled: true, FrigatePhaseVerified: true, FrigatePhaseAbandoned: true,
	} {
		require.Equal(t, terminal, phase.Terminal(), phase)
	}
}
