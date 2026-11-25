package frigateupdate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	instanceA = "frigate-a"
	instanceB = "frigate-b"
	testKeyID = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	blobSize  = 5 << 19 // 2.5 MiB: three 1 MiB chunks.
)

func lostReply() error { return fleeterror.NewUnavailableErrorf("rpc error: code = DeadlineExceeded") }

func refusal(code codes.Code, reason, message string) error {
	return &sdk.FrigateRefusal{Reason: reason, Message: message, Code: code}
}

// fakeAuthorizer allows everything unless a flag denies it.
type fakeAuthorizer struct {
	mu          sync.Mutex
	denySite    bool
	denyAny     bool
	denyOrgWide bool
	sites       []*int64
}

func (a *fakeAuthorizer) AuthorizeSite(_ context.Context, site *int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sites = append(a.sites, site)
	if a.denySite {
		return fleeterror.NewForbiddenError("denied")
	}
	return nil
}

func (a *fakeAuthorizer) AuthorizeAnySite(context.Context) error {
	if a.denyAny {
		return fleeterror.NewForbiddenError("denied")
	}
	return nil
}

func (a *fakeAuthorizer) AuthorizeOrgWide(context.Context) error {
	if a.denyOrgWide {
		return fleeterror.NewForbiddenError("denied")
	}
	return nil
}

type stagedCopy struct {
	manifest    sdk.FrigateManifest
	fingerprint string
	data        []byte
}

// fakeUpdater is an in-memory Frigate pair. Hooks inject failures.
type fakeUpdater struct {
	mu sync.Mutex

	support    []sdk.FrigateInstanceSupport
	supportErr error

	stages map[string]*stagedCopy

	rollout *sdk.FrigateRollout
	served  bool

	beginCalls   int
	appendOffset map[string][]int64
	aborts       []string
	abortRefs    []sdk.FrigateStageRef
	startCalls   []sdk.FrigateStartRequest
	statusCalls  int
	statusJobs   []string
	retryCalls   []sdk.FrigateAction
	cancelCalls  []sdk.FrigateAction
	startKeys    map[string]sdk.FrigateRollout
	actionKeys   map[string]sdk.FrigateRollout
	// hookRuns counts retry and cancel hooks that actually ran, as opposed to
	// being answered from a recorded idempotency key.
	hookRuns int

	// commits counts the copies each controller committed.
	commits map[string]int
	// protoOSCheckErr is what CheckProtoOSUpdates returns, as for a Fleet
	// Node that is too old to stage the ProtoOS package.
	protoOSCheckErr error

	beginHook func(instanceID string) error
	// beginReplyHook runs after Begin has created the copy, so it can lose the
	// reply of a call the controller already carried out.
	beginReplyHook func(instanceID string) error
	appendHook     func(instanceID string, offset int64) error
	abortHook      func(ctx context.Context, instanceID string, ref sdk.FrigateStageRef) error
	statusHook     func(call int)
	startHook      func(call int) (accept bool, err error)
	statusErr      error
	retryErr       error
	cancelErr      error
	retryHook      func(a sdk.FrigateAction) sdk.FrigateRollout
	cancelHook     func(a sdk.FrigateAction) sdk.FrigateRollout
}

var _ interfaces.FrigateUpdater = (*fakeUpdater)(nil)

func newFakeUpdater() *fakeUpdater {
	f := &fakeUpdater{stages: map[string]*stagedCopy{}, appendOffset: map[string][]int64{}, startKeys: map[string]sdk.FrigateRollout{}, actionKeys: map[string]sdk.FrigateRollout{}, commits: map[string]int{}}
	for _, id := range []string{instanceA, instanceB} {
		peer := instanceB
		if id == instanceB {
			peer = instanceA
		}
		f.support = append(f.support, sdk.FrigateInstanceSupport{
			InstanceID: id, PeerInstanceID: peer, RunningVersion: "1.3.0", Architecture: "arm64",
			InstallationType: sdk.FrigateInstallationDebianPackage, HelperAvailable: true, ClusterID: "cluster-1",
			SpoolFreeBytes: 8 << 30, EtcdMembersTotal: 3, EtcdMembersHealthy: 3,
			TrustedReleaseKeyIDs: []string{testKeyID}, SelfCoordinationSupported: true, RemoteTriggerAllowed: true,
			ProtoOS: sdk.FrigateProtoOSSupport{
				SelfSupported: true, InstalledVersion: "0.5.0", ReleaseRepositories: []string{"btc-mining/proto-fleet-internal", "block/proto-fleet"},
				Served: sdk.FrigateProtoOSBundle{Source: sdk.FrigateProtoOSBundleBundled},
			},
		})
	}
	return f
}

func (f *fakeUpdater) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.supportErr != nil {
		return nil, f.supportErr
	}
	return append([]sdk.FrigateInstanceSupport(nil), f.support...), nil
}

func (f *fakeUpdater) FrigateStageBegin(_ context.Context, instanceID string, m sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	if err := m.ValidateForInstance(instanceID); err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	f.mu.Lock()
	beginHook := f.beginHook
	f.mu.Unlock()
	if beginHook != nil {
		if err := beginHook(instanceID); err != nil {
			return sdk.FrigateStageProgress{}, err
		}
	}
	f.mu.Lock()
	f.beginCalls++
	key := instanceID + "/" + m.JobID
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%v|%s", m.JobID, instanceID, m.SHA256, m.ExpectedInstanceIDs, m.Actor)))
	st, ok := f.stages[key]
	if !ok {
		st = &stagedCopy{manifest: m, fingerprint: hex.EncodeToString(fingerprint[:])}
		f.stages[key] = st
	}
	progress := sdk.FrigateStageProgress{JobID: m.JobID, ManifestSHA256: st.fingerprint, NextOffset: int64(len(st.data)), SizeBytes: m.SizeBytes}
	replyHook := f.beginReplyHook
	f.mu.Unlock()
	if replyHook != nil {
		if err := replyHook(instanceID); err != nil {
			return sdk.FrigateStageProgress{}, err
		}
	}
	return progress, nil
}

func (f *fakeUpdater) FrigateStageAppend(_ context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	if err := a.Validate(); err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	f.mu.Lock()
	hook := f.appendHook
	f.mu.Unlock()
	if hook != nil {
		if err := hook(instanceID, a.Offset); err != nil {
			return sdk.FrigateStageProgress{}, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.stages[instanceID+"/"+a.JobID]
	if !ok || st.fingerprint != a.ManifestSHA256 {
		return sdk.FrigateStageProgress{}, fmt.Errorf("not staged")
	}
	if a.Offset != int64(len(st.data)) {
		return sdk.FrigateStageProgress{}, fmt.Errorf("offset %d, staged %d", a.Offset, len(st.data))
	}
	f.appendOffset[instanceID] = append(f.appendOffset[instanceID], a.Offset)
	st.data = append(st.data, a.Chunk...)
	return sdk.FrigateStageProgress{JobID: a.JobID, ManifestSHA256: st.fingerprint, NextOffset: int64(len(st.data)), SizeBytes: st.manifest.SizeBytes}, nil
}

func (f *fakeUpdater) FrigateStageCommit(_ context.Context, instanceID string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	if err := r.Validate(); err != nil {
		return sdk.FrigateStageCommit{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.stages[instanceID+"/"+r.JobID]
	if !ok {
		return sdk.FrigateStageCommit{}, fmt.Errorf("not staged")
	}
	f.commits[instanceID]++
	if st.manifest.Package == sdk.FrigatePackageProtoOS {
		// The controller's update helper takes the job as soon as the copy commits.
		f.setProtoOSLocked(instanceID, func(p *sdk.FrigateProtoOSSupport) {
			p.Job = sdk.FrigateProtoOSJob{JobID: r.JobID, ReleaseTag: st.manifest.ReleaseTag, TargetVersion: st.manifest.TargetVersion, State: sdk.FrigateProtoOSJobQueued, UpdatedAt: time.Now()}
		})
	}
	sum := sha256.Sum256(st.data)
	return sdk.FrigateStageCommit{JobID: r.JobID, BytesStaged: int64(len(st.data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func (f *fakeUpdater) FrigateStageAbort(ctx context.Context, instanceID string, r sdk.FrigateStageRef) error {
	if err := r.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	hook := f.abortHook
	f.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, instanceID, r); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.stages, instanceID+"/"+r.JobID)
	f.aborts = append(f.aborts, instanceID)
	f.abortRefs = append(f.abortRefs, r)
	return nil
}

func (f *fakeUpdater) FrigateUpdateStart(_ context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	if err := r.Validate(); err != nil {
		return sdk.FrigateRollout{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, r)
	if prior, ok := f.startKeys[r.IdempotencyKey]; ok {
		return prior, nil
	}
	accept, err := true, error(nil)
	if f.startHook != nil {
		accept, err = f.startHook(len(f.startCalls))
	}
	if accept {
		ro := sdk.FrigateRollout{
			JobID: r.JobID, TargetVersion: r.TargetVersion, Phase: sdk.FrigatePhaseVerifying, StageOnly: r.StageOnly,
			FromVersion: "1.3.0", Cancellable: true, UpdatedAt: time.Now(),
			Instances: []sdk.FrigateInstanceProgress{
				{InstanceID: instanceA, Phase: sdk.FrigateAgentStaged, LastContact: time.Now()},
				{InstanceID: instanceB, Phase: sdk.FrigateAgentStaged, LastContact: time.Now()},
			},
		}
		f.rollout = &ro
		f.startKeys[r.IdempotencyKey] = ro
		if err == nil {
			return ro, nil
		}
	}
	return sdk.FrigateRollout{}, err
}

func (f *fakeUpdater) FrigateUpdateStatus(_ context.Context, jobID string) (sdk.FrigateStatus, error) {
	f.mu.Lock()
	f.statusCalls++
	f.statusJobs = append(f.statusJobs, jobID)
	call, hook := f.statusCalls, f.statusHook
	f.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return sdk.FrigateStatus{}, f.statusErr
	}
	if f.rollout == nil || f.rollout.JobID != jobID {
		return sdk.FrigateStatus{}, refusal(codes.NotFound, frigateNotFoundReason, "unknown rollout")
	}
	return sdk.FrigateStatus{Rollout: *f.rollout, ServedByMaintenance: f.served, AnsweredBy: instanceA}, nil
}

// replayOrRun returns the outcome Frigate recorded for an idempotency key, and
// otherwise runs the action and records it. The caller holds f.mu.
func (f *fakeUpdater) replayOrRun(a sdk.FrigateAction, run func() (sdk.FrigateRollout, error)) (sdk.FrigateRollout, error) {
	if prior, ok := f.actionKeys[a.IdempotencyKey]; ok {
		return prior, nil
	}
	ro, err := run()
	if err == nil {
		f.actionKeys[a.IdempotencyKey] = ro
	}
	return ro, err
}

func (f *fakeUpdater) FrigateUpdateRetry(_ context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return sdk.FrigateRollout{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryCalls = append(f.retryCalls, a)
	return f.replayOrRun(a, func() (sdk.FrigateRollout, error) {
		if f.retryErr != nil {
			return sdk.FrigateRollout{}, f.retryErr
		}
		if f.rollout == nil {
			return sdk.FrigateRollout{}, refusal(codes.NotFound, frigateNotFoundReason, "unknown rollout")
		}
		if f.retryHook != nil {
			f.hookRuns++
			ro := f.retryHook(a)
			f.rollout = &ro
			return ro, nil
		}
		return *f.rollout, nil
	})
}

func (f *fakeUpdater) FrigateUpdateCancel(_ context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return sdk.FrigateRollout{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalls = append(f.cancelCalls, a)
	return f.replayOrRun(a, func() (sdk.FrigateRollout, error) {
		if f.cancelErr != nil {
			return sdk.FrigateRollout{}, f.cancelErr
		}
		if f.cancelHook != nil {
			f.hookRuns++
			ro := f.cancelHook(a)
			f.rollout = &ro
			return ro, nil
		}
		if f.rollout == nil {
			return sdk.FrigateRollout{}, refusal(codes.NotFound, frigateNotFoundReason, "unknown rollout")
		}
		return *f.rollout, nil
	})
}

// CheckProtoOSUpdates answers as a Fleet Node does before a ProtoOS update.
func (f *fakeUpdater) CheckProtoOSUpdates(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.protoOSCheckErr
}

func (f *fakeUpdater) setProtoOSLocked(instanceID string, mutate func(p *sdk.FrigateProtoOSSupport)) {
	for i := range f.support {
		if f.support[i].InstanceID == instanceID {
			mutate(&f.support[i].ProtoOS)
		}
	}
}

// reportProtoOS changes what a controller reports about ProtoOS.
func (f *fakeUpdater) reportProtoOS(instanceID string, mutate func(p *sdk.FrigateProtoOSSupport)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setProtoOSLocked(instanceID, mutate)
}

// finishProtoOS ends a controller's ProtoOS job as its update helper does.
func (f *fakeUpdater) finishProtoOS(instanceID string, state sdk.FrigateProtoOSJobState, message string) {
	f.reportProtoOS(instanceID, func(p *sdk.FrigateProtoOSSupport) {
		p.Job.State, p.Job.Message = state, message
		if state == sdk.FrigateProtoOSJobSucceeded {
			p.InstalledVersion = p.Job.TargetVersion
			p.Served = sdk.FrigateProtoOSBundle{Source: sdk.FrigateProtoOSBundlePackage, ReleaseTag: p.Job.ReleaseTag}
		}
	})
}

func (f *fakeUpdater) commitCount(instanceID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commits[instanceID]
}

// setRollout replaces the rollout Frigate reports.
func (f *fakeUpdater) setRollout(mutate func(r *sdk.FrigateRollout)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f.rollout)
	f.rollout.UpdatedAt = f.rollout.UpdatedAt.Add(time.Second)
}

func (f *fakeUpdater) calls() (begin, starts, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.beginCalls, len(f.startCalls), f.statusCalls
}

// fakeMiner exposes the fake updater through the miner handle.
type fakeMiner struct {
	interfaces.Miner
	*fakeUpdater
}

type fakeGetter struct {
	mu    sync.Mutex
	miner interfaces.Miner
	err   error
}

func (g *fakeGetter) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.miner, g.err
}

type env struct {
	t       *testing.T
	conn    *sql.DB
	db      *testutil.DatabaseService
	svc     *Service
	ctx     context.Context //nolint:containedctx // Test fixture carries one authenticated context.
	orgID   int64
	userID  int64
	device  testutil.DeviceIdentification
	updater *fakeUpdater
	auth    *fakeAuthorizer
	getter  *fakeGetter
	store   *files.FrigatePackageStore
	pkg     sqlc.FrigateUpdatePackage
	blob    []byte
	clock   *testClock
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func userContext(ctx context.Context, userID, orgID int64, username string) context.Context {
	return authn.SetInfo(ctx, &session.Info{
		UserID: userID, OrganizationID: orgID, AuthMethod: session.AuthMethodSession,
		Username: username, ExternalUserID: "ext-" + username,
	})
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	conn := database.DB
	ctx := userContext(t.Context(), user.DatabaseID, user.OrganizationID, user.Username)
	_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)

	store, err := files.NewFrigatePackageStore(t.TempDir())
	require.NoError(t, err)
	updater := newFakeUpdater()
	getter := &fakeGetter{miner: fakeMiner{fakeUpdater: updater}}
	auth := &fakeAuthorizer{}
	svc := NewService(conn, getter, store, nil, auth, nil, Config{
		PollInterval: time.Second, StageStallTimeout: 10 * time.Minute, CallTimeout: 10 * time.Second, ChunkTimeout: 10 * time.Second,
	})
	clock := &testClock{t: time.Now()}
	svc.now = clock.now

	e := &env{t: t, conn: conn, db: database, svc: svc, ctx: ctx, orgID: user.OrganizationID, userID: user.DatabaseID, device: device,
		updater: updater, auth: auth, getter: getter, store: store, clock: clock}
	e.pkg, e.blob = e.seedPackage("2.0.0")
	return e
}

// seedPackage stores a random blob and a package row for it. The fake
// controllers never open the package, so it need not be a real .deb.
func (e *env) seedPackage(version string) (sqlc.FrigateUpdatePackage, []byte) {
	e.t.Helper()
	return e.seedPackageRow(sqlc.InsertFrigateUpdatePackageParams{
		Kind: string(PackageKindFrigate), Filename: "frigate_" + version + "_arm64.deb", Version: version, Architecture: "arm64",
		Repository: "btc-mining/frigate",
	})
}

// seedProtoOSPackage stores a proto-os package for a release tag.
func (e *env) seedProtoOSPackage(tag string) (sqlc.FrigateUpdatePackage, []byte) {
	e.t.Helper()
	return e.seedPackageRow(sqlc.InsertFrigateUpdatePackageParams{
		Kind: string(PackageKindProtoOS), Filename: sdk.ProtoOSAssetName(tag), Version: sdk.ProtoOSDebianVersion(tag), Architecture: "all",
		Repository: "block/proto-fleet",
	})
}

// seedPackageRow stores a random blob and the package row that params
// describes, with the blob's size and digest.
func (e *env) seedPackageRow(params sqlc.InsertFrigateUpdatePackageParams) (sqlc.FrigateUpdatePackage, []byte) {
	e.t.Helper()
	blob := make([]byte, blobSize)
	_, err := rand.Read(blob)
	require.NoError(e.t, err)
	sum := sha256.Sum256(blob)
	id := uuid.New()
	staged, err := e.store.NewStagingPath(id.String())
	require.NoError(e.t, err)
	require.NoError(e.t, writeFile(staged, blob))
	require.NoError(e.t, e.store.Publish(id, staged))
	params.ID, params.OrgID, params.Sha256, params.SizeBytes = id, e.orgID, hex.EncodeToString(sum[:]), int64(len(blob))
	params.ReleaseKeyID, params.IssuedAt, params.Statement, params.Signature = testKeyID, time.Now().UTC(), []byte(`{"stmt":true}`), []byte{1, 2, 3}
	params.UploadedBy = sql.NullInt64{Int64: e.userID, Valid: true}
	row, err := sqlc.New(e.conn).InsertFrigateUpdatePackage(e.ctx, params)
	require.NoError(e.t, err)
	return row, blob
}

func (e *env) start(key string) Job {
	e.t.Helper()
	job, err := e.svc.StartUpdate(e.ctx, StartInput{DeviceIdentifier: e.device.ID, PackageID: e.pkg.ID, IdempotencyKey: key})
	require.NoError(e.t, err)
	return job
}

func (e *env) job(id uuid.UUID) Job {
	e.t.Helper()
	job, err := e.svc.GetUpdate(e.ctx, id)
	require.NoError(e.t, err)
	return job
}

func (e *env) instance(job Job, id string) sqlc.FrigateUpdateInstance {
	e.t.Helper()
	for _, in := range e.job(job.Row.ID).Instances {
		if in.InstanceID == id {
			return in
		}
	}
	e.t.Fatalf("instance %s not found", id)
	return sqlc.FrigateUpdateInstance{}
}

// advanceToRunning runs the runner until Frigate has accepted the job.
func (e *env) advanceToRunning(job Job) Job {
	e.t.Helper()
	e.svc.runOnce(e.ctx)
	got := e.job(job.Row.ID)
	require.Equal(e.t, StateRunning, got.Row.State, "error %s: %s", got.Row.ErrorCode, got.Row.ErrorMessage)
	return got
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
