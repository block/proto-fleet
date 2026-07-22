package curtailment

import (
	"context"
	"math"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// startStubStore implements interfaces.CurtailmentStore for handler-level
// Start coverage. It runs a single canned plan through Service.Start so the
// handler exercise the full translate -> service -> store -> translate path
// without DB I/O.
type startStubStore struct {
	orgConfig          *models.OrgConfig
	candidates         []*models.Candidate
	replayByKey        map[string]*models.Event
	targetsByEventUUID map[uuid.UUID][]*models.Target
	rollupByEventUUID  map[uuid.UUID]*models.TargetRollup

	// Captures.
	lastEvent   models.InsertEventParams
	lastTargets []models.InsertTargetParams
}

func newStartStubStore() *startStubStore {
	return &startStubStore{
		orgConfig: &models.OrgConfig{
			OrgID:                 1,
			MaxDurationDefaultSec: 14400,
			CandidateMinPowerW:    1500,
		},
	}
}

func (s *startStubStore) GetOrgConfig(_ context.Context, orgID int64) (*models.OrgConfig, error) {
	cfg := *s.orgConfig
	cfg.OrgID = orgID
	return &cfg, nil
}

func (s *startStubStore) ListActiveCurtailedDevices(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (s *startStubStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailmentTargetDevices not exercised by handler Start tests")
}

func (s *startStubStore) ListRecentlyResolvedCurtailedDevices(
	context.Context,
	interfaces.ListRecentlyResolvedCurtailedDevicesParams,
) ([]string, error) {
	return nil, nil
}

func (s *startStubStore) SiteBelongsToOrg(_ context.Context, _, _ int64) (bool, error) {
	return true, nil
}

func (s *startStubStore) ListCandidates(_ context.Context, _ interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	return s.candidates, nil
}

func (s *startStubStore) InsertEventWithTargets(
	_ context.Context,
	event models.InsertEventParams,
	targets []models.InsertTargetParams,
) (*models.InsertEventResult, error) {
	s.lastEvent = event
	s.lastTargets = append([]models.InsertTargetParams(nil), targets...)
	return &models.InsertEventResult{
		ID:        1,
		EventUUID: event.EventUUID,
	}, nil
}
func (s *startStubStore) ClaimClosedLoopFullFleetTargets(
	context.Context,
	int64,
	int64,
	int32,
	[]models.InsertTargetParams,
) ([]*models.Target, error) {
	panic("ClaimClosedLoopFullFleetTargets not exercised by handler Start tests")
}
func (s *startStubStore) ClaimAllPairedPolicyTargets(
	context.Context,
	int64,
	[]models.InsertTargetParams,
) (int64, error) {
	panic("ClaimAllPairedPolicyTargets not exercised by handler Start tests")
}

func (s *startStubStore) BulkRefreshAllPairedTargetReadiness(
	context.Context,
	int64,
	models.EventState,
	[]interfaces.AllPairedReadinessUpdate,
) ([]string, error) {
	panic("BulkRefreshAllPairedTargetReadiness not exercised by handler Start tests")
}

// --- panic stubs for surface the handler-level tests don't reach ---

func (s *startStubStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised by handler Start tests")
}

func (s *startStubStore) GetEventDetailByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventDetailByUUID not exercised by handler Start tests")
}

func (s *startStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised by handler Start tests")
}

func (s *startStubStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by Start handler tests")
}
func (s *startStubStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by Start handler tests")
}
func (s *startStubStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by Start handler tests")
}
func (s *startStubStore) ForceReleaseEvent(context.Context, int64, uuid.UUID, string) (interfaces.ForceReleaseEventResult, error) {
	panic("ForceReleaseEvent not exercised by Start handler tests")
}
func (s *startStubStore) GetEventByIdempotencyKey(_ context.Context, _ int64, key string) (*models.Event, error) {
	// Default to "no prior match" so Start tests that pass an idempotency
	// key fall through to the normal insert path. Replay-specific tests
	// override with a field on the stub.
	return s.replayByKey[key], nil
}
func (s *startStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	// Default to "no prior match" so Start tests that pass an external
	// reference fall through to the normal insert path. Replay-specific
	// tests override with a field on the stub.
	return nil, nil
}
func (s *startStubStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if s.targetsByEventUUID == nil {
		return nil, nil
	}
	return s.targetsByEventUUID[eventUUID], nil
}

func (s *startStubStore) ListTargetsByEventPage(context.Context, interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	panic("ListTargetsByEventPage not exercised by handler Start tests")
}

func (s *startStubStore) ListTargetSiteCoverageByEvent(context.Context, int64, uuid.UUID) (models.TargetSiteCoverage, error) {
	panic("ListTargetSiteCoverageByEvent not exercised by handler Start tests")
}
func (s *startStubStore) ListTargetSiteCoverageByEvents(context.Context, int64, []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	panic("ListTargetSiteCoverageByEvents not exercised by handler Start tests")
}

func (s *startStubStore) GetTargetRollupByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.TargetRollup, error) {
	rollup, ok := s.rollupByEventUUID[eventUUID]
	if !ok {
		panic("GetTargetRollupByEvent not seeded for this handler Start test")
	}
	return rollup, nil
}

func (s *startStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by handler Start tests")
}

func (s *startStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by handler Start tests")
}

func (s *startStubStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by handler Start tests")
}
func (s *startStubStore) RecordCurtailPendingDispatch(context.Context, int64, models.EventState, time.Time) error {
	panic("RecordCurtailPendingDispatch not exercised by handler Start tests")
}

func (s *startStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by handler Start tests")
}

func (s *startStubStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised by handler Start tests")
}

func (s *startStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by handler Start tests")
}

func (s *startStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID, interfaces.BeginRestoreTransitionParams) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by handler Start tests")
}
func (s *startStubStore) BeginRecurtailTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRecurtailTransition not exercised by handler Start tests")
}

func miner(id, status, pairing string, powerW float64, hashRateHS float64, effJH float64) *models.Candidate {
	driver := "antminer"
	pw := powerW
	hr := hashRateHS
	eff := effJH
	t := mustParseTime("2026-05-01T00:00:00Z")
	return &models.Candidate{
		DeviceIdentifier: id,
		DriverName:       &driver,
		DeviceStatus:     status,
		PairingStatus:    pairing,
		LatestPowerW:     &pw,
		LatestHashRateHS: &hr,
		AvgEfficiencyJH:  &eff,
		LatestMetricsAt:  &t,
	}
}

// validStartRequestBuilder is a separate shape from the handler_test.go
// helper validStartCurtailmentRequest, which returns a minimal proto
// Request; this builder targets the handler-with-service tests by adding
// the operational controls the service requires.
func validStartRequestBuilder() *pb.StartCurtailmentRequest {
	return &pb.StartCurtailmentRequest{
		Scope: &pb.StartCurtailmentRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}},
		Mode:  pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		// UNSPECIFIED maps to LEAST_EFFICIENT_FIRST in the translator; the
		// proto-named constant passes through as `s.String()` and is rejected
		// by the validator (existing pre-Start behavior preserved).
		Strategy: pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED,
		Level:    pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority: pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
		ModeParams: &pb.StartCurtailmentRequest_FixedKw{
			FixedKw: &pb.FixedKwParams{TargetKw: 5},
		},
		MaxDurationSeconds: 7200,
		Reason:             "operator handler test",
	}
}

func TestStartCurtailmentRequest_FacilityFanLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		fanCount int
		wantErr  bool
	}{
		{name: "allows current selection ceiling", fanCount: 8},
		{name: "allows legacy copied profile ceiling", fanCount: 1024},
		{name: "rejects above legacy ceiling", fanCount: 1025, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := validStartRequestBuilder()
			req.FacilityFanDeviceIds = make([]int64, tc.fanCount)
			for index := range req.FacilityFanDeviceIds {
				req.FacilityFanDeviceIds[index] = int64(index + 1)
			}

			err := protovalidate.Validate(req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no more than 1024")
				return
			}
			require.NoError(t, err)
		})
	}
}

func startSessionCtxWithPerms(t *testing.T, orgID int64, role string, perms ...string) context.Context {
	t.Helper()
	return startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           role,
		SessionID:      "sess-start-perms",
	}, perms...)
}

func startSessionInfoCtxWithPerms(t *testing.T, info *session.Info, perms ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}}))
}

// TestHandler_StartCurtailment_HappyPath: with a stubbed service, a valid
// session, and ample candidates, Start returns the populated event with
// EventUuid set and pending targets echoed back.
func TestHandler_StartCurtailment_HappyPath(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("worst", "ACTIVE", "PAIRED", 3000, 100, 50),
		miner("mid", "ACTIVE", "PAIRED", 3000, 100, 35),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	}, authz.PermCurtailmentManage)

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(validStartRequestBuilder()))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Msg.Event)

	ev := resp.Msg.Event
	assert.NotEmpty(t, ev.EventUuid, "event_uuid must be populated on success")
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING, ev.State)
	assert.Equal(t, pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW, ev.Mode)
	assert.Equal(t, "operator handler test", ev.Reason)

	// Persisted event mirrors the request fields.
	assert.Equal(t, models.SourceActorUser, store.lastEvent.SourceActorType)
	require.NotNil(t, store.lastEvent.SourceActorID)
	assert.Equal(t, "sess-abc", *store.lastEvent.SourceActorID)
	// CreatedByUserID is the FK plumbing's load-bearing field: handler must
	// thread session.Info.UserID into the persisted event so the reconciler
	// dispatches under a real user.id (command_batch_log.created_by FK).
	assert.Equal(t, int64(9), store.lastEvent.CreatedByUserID)

	// Targets are echoed in pending state with baseline captured.
	require.Len(t, ev.Targets, 2)
	assert.Equal(t, "worst", ev.Targets[0].DeviceIdentifier)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING, ev.Targets[0].State)
	require.NotNil(t, ev.Targets[0].BaselinePowerW)
	assert.InDelta(t, 3000.0, *ev.Targets[0].BaselinePowerW, 0.001)
	assert.Equal(t, int32(2), ev.TargetRollup.Pending)
	assert.Equal(t, int32(2), ev.TargetRollup.Total)

	// effective_batch_size is stamped from the selected-target count and
	// echoed in the Start response. Two selected candidates with no caller
	// preference means immediate restore of the full pending set.
	assert.Equal(t, uint32(2), ev.EffectiveBatchSize)
}

func TestHandler_StartCurtailment_AllPairedPolicyReturnsBoundedTargetRollup(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("online", "ACTIVE", "PAIRED", 3000, 100, 40),
		miner("offline", "OFFLINE", "PAIRED", 0, 0, 0),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "ADMIN",
		SessionID:      "sess-admin",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
	req.ModeParams = nil
	req.ForceIncludeAllPairedMiners = true

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)

	ev := resp.Msg.Event
	assert.True(t, ev.GetForceIncludeAllPairedMiners())
	assert.True(t, store.lastEvent.ForceIncludeAllPairedMiners)
	assert.Empty(t, ev.Targets, "all-paired starts use rollups instead of returning one target per miner")
	assert.Equal(t, int32(1), ev.TargetRollup.Pending)
	assert.Equal(t, int32(1), ev.TargetRollup.Unavailable)
	assert.Equal(t, int32(2), ev.TargetRollup.Total)
}

// TestHandler_StartCurtailment_AllPairedReplayStaysCountOnly pins the
// idempotent-replay response shape for all-paired events: the retry must
// return the persisted rollup with no per-target rows, matching the
// count-only contract of the first-time response (all-paired starts persist
// one row per paired-like miner, so hydrating targets would serialize a
// fleet-sized payload on the retry path).
func TestHandler_StartCurtailment_AllPairedReplayStaysCountOnly(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	store := newStartStubStore()
	store.replayByKey = map[string]*models.Event{
		"all-paired-retry": {
			ID:                          11,
			EventUUID:                   eventUUID,
			OrgID:                       42,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			Strategy:                    models.StrategyLeastEfficientFirst,
			Level:                       models.LevelFull,
			Priority:                    models.PriorityNormal,
			RestoreBatchSize:            10,
			RestoreBatchIntervalSec:     60,
			ForceIncludeAllPairedMiners: true,
			Reason:                      "all-paired replay",
			CreatedAt:                   time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
			UpdatedAt:                   time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
			CreatedByUserID:             9,
		},
	}
	store.rollupByEventUUID = map[uuid.UUID]*models.TargetRollup{
		eventUUID: {Pending: 4, Unavailable: 2, Total: 6},
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "ADMIN",
		SessionID:      "sess-replay-admin",
	}, authz.PermCurtailmentManage)
	req := validStartRequestBuilder()
	req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
	req.ModeParams = nil
	req.ForceIncludeAllPairedMiners = true
	req.IdempotencyKey = "all-paired-retry"

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)

	ev := resp.Msg.Event
	assert.Equal(t, eventUUID.String(), ev.EventUuid)
	assert.Empty(t, ev.Targets, "all-paired replay must not hydrate per-target rows")
	require.NotNil(t, ev.TargetRollup)
	assert.Equal(t, int32(4), ev.TargetRollup.Pending)
	assert.Equal(t, int32(2), ev.TargetRollup.Unavailable)
	assert.Equal(t, int32(6), ev.TargetRollup.Total)
	assert.Empty(t, store.lastTargets, "replay must not persist a second event")
}

func TestHandler_StartCurtailment_PersistsCurtailBatchControls(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("worst", "ACTIVE", "PAIRED", 3000, 100, 50),
		miner("mid", "ACTIVE", "PAIRED", 3000, 100, 35),
	}
	h := NewHandler(curtailment.NewService(store))
	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.CurtailBatchSize = ptrUint32(1)
	req.CurtailBatchIntervalSec = ptrUint32(15)

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	require.NotNil(t, store.lastEvent.CurtailBatchSize)
	assert.Equal(t, int32(1), *store.lastEvent.CurtailBatchSize)
	assert.Equal(t, int32(15), store.lastEvent.CurtailBatchIntervalSec)
	require.NotNil(t, resp.Msg.Event.CurtailBatchSize)
	assert.Equal(t, uint32(1), resp.Msg.Event.GetCurtailBatchSize())
	assert.Equal(t, uint32(15), resp.Msg.Event.GetCurtailBatchIntervalSec())
}

func TestHandler_StartCurtailment_RejectsCurtailBatchIntervalWithoutSize(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	h := NewHandler(curtailment.NewService(store))
	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.CurtailBatchIntervalSec = ptrUint32(0)

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "curtail_batch_interval_sec requires curtail_batch_size")
}

func TestHandler_StartCurtailment_RequiresCurtailmentManage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		mutateReq   func(*pb.StartCurtailmentRequest)
		permissions []string
		wantMode    models.Mode
		wantCode    connect.Code
		wantPersist bool
		wantTargets int
	}{
		{
			name:        "fixed kw whole org without manage is rejected",
			permissions: []string{authz.PermCurtailmentRead},
			wantMode:    models.ModeFixedKw,
			wantCode:    connect.CodePermissionDenied,
		},
		{
			name:        "empty permissions set is rejected",
			permissions: nil,
			wantMode:    models.ModeFixedKw,
			wantCode:    connect.CodePermissionDenied,
		},
		{
			name: "full fleet whole org without manage is rejected",
			mutateReq: func(req *pb.StartCurtailmentRequest) {
				req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
				req.ModeParams = nil
			},
			permissions: []string{authz.PermCurtailmentRead},
			wantMode:    models.ModeFullFleet,
			wantCode:    connect.CodePermissionDenied,
		},
		{
			name: "full fleet explicit device list without manage is rejected",
			mutateReq: func(req *pb.StartCurtailmentRequest) {
				req.Scope = &pb.StartCurtailmentRequest_DeviceIdentifiers{
					DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: []string{"eligible"}},
				}
				req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
				req.ModeParams = nil
			},
			permissions: []string{authz.PermCurtailmentRead},
			wantMode:    models.ModeFullFleet,
			wantCode:    connect.CodePermissionDenied,
		},
		{
			name:        "fixed kw whole org with manage can start",
			permissions: []string{authz.PermCurtailmentManage},
			wantMode:    models.ModeFixedKw,
			wantPersist: true,
			wantTargets: 1,
		},
		{
			name: "full fleet whole org with manage can start",
			mutateReq: func(req *pb.StartCurtailmentRequest) {
				req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
				req.ModeParams = nil
			},
			permissions: []string{authz.PermCurtailmentManage},
			wantMode:    models.ModeFullFleet,
			wantPersist: true,
			wantTargets: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := newStartStubStore()
			store.candidates = []*models.Candidate{
				miner("eligible", "ACTIVE", "PAIRED", 6000, 100, 40),
			}
			h := NewHandler(curtailment.NewService(store))

			req := validStartRequestBuilder()
			if tc.mutateReq != nil {
				tc.mutateReq(req)
			}

			_, err := h.StartCurtailment(
				startSessionCtxWithPerms(t, 1, "OPERATOR", tc.permissions...),
				connect.NewRequest(req),
			)

			if tc.wantPersist {
				require.NoError(t, err)
				assert.Equal(t, tc.wantMode, store.lastEvent.Mode)
				assert.Len(t, store.lastTargets, tc.wantTargets)
				return
			}

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			assert.Zero(t, store.lastEvent.OrgID, "permission gate must fail before persistence")
			assert.Empty(t, store.lastTargets, "permission gate must fail before target selection persists")
		})
	}
}

func TestHandler_StartCurtailment_IdempotentReplayRendersPersistedEvent(t *testing.T) {
	t.Parallel()

	eventUUID := uuid.New()
	store := newStartStubStore()
	store.replayByKey = map[string]*models.Event{
		"retry-key": {
			ID:                      7,
			EventUUID:               eventUUID,
			OrgID:                   42,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 60,
			Reason:                  "original persisted reason",
			CreatedAt:               time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC),
			UpdatedAt:               time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC),
			CreatedByUserID:         9,
		},
	}
	store.targetsByEventUUID = map[uuid.UUID][]*models.Target{
		eventUUID: {
			{
				DeviceIdentifier: "miner-1",
				TargetType:       "miner",
				State:            models.TargetStateConfirmed,
				DesiredState:     models.DesiredStateCurtailed,
			},
		},
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	}, authz.PermCurtailmentManage)
	req := validStartRequestBuilder()
	req.Reason = "changed retry reason"
	req.IdempotencyKey = "retry-key"

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)

	ev := resp.Msg.Event
	assert.Equal(t, eventUUID.String(), ev.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, ev.State)
	assert.Equal(t, "original persisted reason", ev.Reason)
	require.Len(t, ev.Targets, 1)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_CONFIRMED, ev.Targets[0].State)
	assert.Empty(t, store.lastTargets, "replay must not persist a second event")
}

func TestHandler_StartCurtailment_IdempotentReplayRequiresPersistedEventPermission(t *testing.T) {
	t.Parallel()

	const (
		requestSite = int64(7)
		replaySite  = int64(8)
	)
	eventUUID := uuid.New()
	store := newStartStubStore()
	store.replayByKey = map[string]*models.Event{
		"retry-key": {
			ID:                      7,
			EventUUID:               eventUUID,
			OrgID:                   42,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			ScopeType:               models.ScopeTypeSite,
			ScopeJSON:               siteScopeJSON(t, replaySite),
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 60,
			Reason:                  "original persisted reason",
			CreatedAt:               time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC),
			UpdatedAt:               time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC),
			CreatedByUserID:         9,
		},
	}
	h := NewHandler(curtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	}, testOrgAssignment(authz.PermCurtailmentManage),
		testSiteAssignment(requestSite, authz.PermCurtailmentManage),
		testSiteAssignment(replaySite))
	req := validStartRequestBuilder()
	req.Scope = &pb.StartCurtailmentRequest_Site{Site: &pb.ScopeSite{SiteId: requestSite}}
	req.IdempotencyKey = "retry-key"

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Empty(t, store.lastTargets, "replay denial must not persist a second event")
}

// TestHandler_StartCurtailment_APIKeyDerivesAPIKeyActor pins the audit
// attribution: an API-key authenticated session must persist
// source_actor_type='api_key' even though the override fields aren't
// involved.
func TestHandler_StartCurtailment_APIKeyDerivesAPIKeyActor(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("a", "ACTIVE", "PAIRED", 6000, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		OrganizationID: 1,
		UserID:         9,
		Role:           "OPERATOR",
		APIKeyID:       "key-77",
	}, authz.PermCurtailmentManage)

	_, err := h.StartCurtailment(ctx, connect.NewRequest(validStartRequestBuilder()))
	require.NoError(t, err)

	assert.Equal(t, models.SourceActorAPIKey, store.lastEvent.SourceActorType)
	require.NotNil(t, store.lastEvent.SourceActorID)
	assert.Equal(t, "apikey:key-77", *store.lastEvent.SourceActorID,
		"api_key actor id must use the credential prefix matching session.Info.CredentialID")
}

// TestHandler_StartCurtailment_InsufficientLoadSurfacesAsInvalidArgument
// pins the error-translation contract: an InsufficientLoadDetail returned
// by the service must reach the caller as InvalidArgument with the kW
// numbers in the message, mirroring Preview's behavior.
func TestHandler_StartCurtailment_InsufficientLoadSurfacesAsInvalidArgument(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("only", "ACTIVE", "PAIRED", 1500, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "OPERATOR",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.ModeParams = &pb.StartCurtailmentRequest_FixedKw{
		FixedKw: &pb.FixedKwParams{TargetKw: 100}, // far above the 1.5 kW pool
	}

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "insufficient curtailable load")
	// No persistence on the rejection branch.
	assert.Empty(t, store.lastTargets)
}

func TestHandler_StartCurtailment_FullFleetAllSkippedReturnsActiveWatcher(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("offline", "OFFLINE", "PAIRED", 0, 0, 40),
		miner("updating", "UPDATING", "PAIRED", 0, 0, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "OPERATOR",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.Mode = pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
	req.ModeParams = nil

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetEvent())
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, resp.Msg.GetEvent().GetState())
	assert.Empty(t, resp.Msg.GetEvent().GetTargets())
	assert.Equal(t, int32(0), resp.Msg.GetEvent().GetTargetRollup().GetTotal())
	assert.Empty(t, store.lastTargets)
}

// TestHandler_StartCurtailment_RejectsMissingSession pins the auth gate:
// without session.Info in context, Start must fail with Unauthenticated
// (not crash on a nil-dereference of OrganizationID).
func TestHandler_StartCurtailment_RejectsMissingSession(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	h := NewHandler(curtailment.NewService(store))

	_, err := h.StartCurtailment(t.Context(), connect.NewRequest(validStartRequestBuilder()))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

// TestHandler_StartCurtailment_OverrideRoleGateBlocksNonAdmin pins the
// override matrix when the service is wired (matches the Unimplemented-only
// coverage in TestHandler_OverrideFieldsRoleGate). With a populated service
// the Forbidden response must precede the body.
func TestHandler_StartCurtailment_OverrideRoleGateBlocksNonAdmin(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("a", "ACTIVE", "PAIRED", 6000, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "VIEWER",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.CandidateMinPowerWOverride = ptr(uint32(800))

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	// Service must not have been reached.
	assert.Empty(t, store.lastTargets)
}

// TestHandler_StartCurtailment_ZeroMaxDurationUsesOrgDefault verifies the
// "use org default" sentinel: max_duration_seconds=0 with allow_unbounded
// false resolves to curtailment_org_config.max_duration_default_sec at
// persistence time rather than rejecting the request.
func TestHandler_StartCurtailment_ZeroMaxDurationUsesOrgDefault(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("a", "ACTIVE", "PAIRED", 6000, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-zero-dur",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.MaxDurationSeconds = 0 // sentinel: use org default.

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, store.lastEvent.MaxDurationSeconds)
	assert.Equal(t, store.orgConfig.MaxDurationDefaultSec, *store.lastEvent.MaxDurationSeconds)
}

// TestHandler_StartCurtailment_AllowUnboundedAdminPersistsNullDuration
// confirms admins can set allow_unbounded=true and that persistence
// captures max_duration_seconds=NULL.
func TestHandler_StartCurtailment_AllowUnboundedAdminPersistsNullDuration(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("a", "ACTIVE", "PAIRED", 6000, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "ADMIN",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.MaxDurationSeconds = 0
	req.AllowUnbounded = true

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	assert.True(t, store.lastEvent.AllowUnbounded)
	assert.Nil(t, store.lastEvent.MaxDurationSeconds)
}

// TestHandler_StartCurtailment_RejectsAllowUnboundedWithMaxDuration
// confirms the proto surface surfaces the service-level mutual-exclusion
// check: allow_unbounded=true with a non-zero max_duration_seconds must
// fail with InvalidArgument rather than silently dropping the cap.
func TestHandler_StartCurtailment_RejectsAllowUnboundedWithMaxDuration(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = []*models.Candidate{
		miner("a", "ACTIVE", "PAIRED", 6000, 100, 40),
	}
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "ADMIN",
	}, authz.PermCurtailmentManage)

	req := validStartRequestBuilder()
	req.AllowUnbounded = true
	req.MaxDurationSeconds = 7200

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "max_duration_seconds")
	assert.Zero(t, store.lastEvent.OrgID, "conflicting request must not reach persistence")
}

// TestHandler_StartCurtailment_RejectsUint32Overflow pins the strict
// overflow rejection on the uint32 → int32 fields the translator
// converts. A value above MaxInt32 must surface as InvalidArgument
// naming the offending field rather than silently saturating.
func TestHandler_StartCurtailment_RejectsUint32Overflow(t *testing.T) {
	t.Parallel()

	const overflow uint32 = math.MaxInt32 + 1

	cases := []struct {
		field string
		mut   func(*pb.StartCurtailmentRequest)
	}{
		{"max_duration_seconds", func(r *pb.StartCurtailmentRequest) { r.MaxDurationSeconds = overflow }},
		{"curtail_batch_size", func(r *pb.StartCurtailmentRequest) { r.CurtailBatchSize = ptrUint32(overflow) }},
		{"curtail_batch_interval_sec", func(r *pb.StartCurtailmentRequest) {
			r.CurtailBatchSize = ptrUint32(1)
			r.CurtailBatchIntervalSec = ptrUint32(overflow)
		}},
		{"restore_batch_size", func(r *pb.StartCurtailmentRequest) { r.RestoreBatchSize = overflow }},
		{"restore_batch_interval_sec", func(r *pb.StartCurtailmentRequest) { r.RestoreBatchIntervalSec = overflow }},
		{"min_curtailed_duration_sec", func(r *pb.StartCurtailmentRequest) { r.MinCurtailedDurationSec = overflow }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			store := newStartStubStore()
			h := NewHandler(curtailment.NewService(store))
			ctx := startSessionInfoCtxWithPerms(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: 1,
				UserID:         9,
				Role:           "OPERATOR",
				SessionID:      "sess",
			}, authz.PermCurtailmentManage)

			req := validStartRequestBuilder()
			tc.mut(req)
			_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
			assert.Contains(t, err.Error(), tc.field)
		})
	}
}

// TestHandler_StartCurtailment_OutcomeMirrorsInsufficientLoadShapeOnZeroPool
// double-checks that an empty candidate pool produces InsufficientLoad
// rather than empty success.
func TestHandler_StartCurtailment_OutcomeMirrorsInsufficientLoadShapeOnZeroPool(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	store.candidates = nil
	h := NewHandler(curtailment.NewService(store))

	ctx := startSessionInfoCtxWithPerms(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		UserID:         9,
		Role:           "OPERATOR",
	}, authz.PermCurtailmentManage)

	_, err := h.StartCurtailment(ctx, connect.NewRequest(validStartRequestBuilder()))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	// Reuse the modes.Outcome enum to anchor that this test path triggers
	// InsufficientLoad and not the empty-Selected guard.
	_ = modes.OutcomeInsufficientLoad
}

// mustParseTime parses the RFC3339 input or panics; used in fixture builders.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
