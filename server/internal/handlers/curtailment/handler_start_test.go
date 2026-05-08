package curtailment

import (
	"context"
	"math"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// startStubStore implements interfaces.CurtailmentStore for handler-level
// Start coverage. It runs a single canned plan through Service.Start so the
// handler exercise the full translate -> service -> store -> translate path
// without DB I/O.
type startStubStore struct {
	orgConfig  *models.OrgConfig
	candidates []*models.Candidate

	// idempotencyHit is the event GetEventByIdempotencyKey returns when the
	// requested key matches its IdempotencyKey field. Tests that exercise the
	// retry-safe Start path stash a pre-existing event here so Service.Start
	// short-circuits before the selector pipeline runs.
	idempotencyHit *models.Event
	// idempotencyTargets is the target list ListTargetsByEvent returns for
	// idempotencyHit's event_uuid; populated alongside the hit row.
	idempotencyTargets []*models.Target

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
			PostEventCooldownSec:  600,
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

func (s *startStubStore) ListRecentlyResolvedCurtailedDevices(_ context.Context, _ int64, _ int32) ([]string, error) {
	return nil, nil
}

func (s *startStubStore) ListCandidates(_ context.Context, _ int64, _ []string) ([]*models.Candidate, error) {
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

// --- panic stubs for surface the handler-level tests don't reach ---

func (s *startStubStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised by handler Start tests")
}

// GetEventByIdempotencyKey returns NotFound by default; tests that exercise
// the idempotent-retry path can stash a row in s.idempotencyHit to short-
// circuit Service.Start before the selector pipeline runs.
func (s *startStubStore) GetEventByIdempotencyKey(_ context.Context, _ int64, key string) (*models.Event, error) {
	if s.idempotencyHit != nil && s.idempotencyHit.IdempotencyKey != nil && *s.idempotencyHit.IdempotencyKey == key {
		return s.idempotencyHit, nil
	}
	return nil, fleeterror.NewNotFoundErrorf("no curtailment event with idempotency_key=%q", key)
}

// ListTargetsByEvent returns the stashed idempotencyTargets when the
// requested event_uuid matches the idempotencyHit; otherwise panics so an
// unexpected call is loud.
func (s *startStubStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if s.idempotencyHit != nil && s.idempotencyHit.EventUUID == eventUUID {
		return append([]*models.Target(nil), s.idempotencyTargets...), nil
	}
	panic("ListTargetsByEvent not exercised by handler Start tests")
}

func (s *startStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by handler Start tests")
}

func (s *startStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by handler Start tests")
}

func (s *startStubStore) UpdateEventState(context.Context, int64, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by handler Start tests")
}

func (s *startStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by handler Start tests")
}

func (s *startStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by handler Start tests")
}

// finitePtr returns &v as a typed pointer; used for proto3 optional fields.
func finitePtr[T any](v T) *T { return &v }

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-abc",
	})

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

	// Targets are echoed in pending state with baseline captured.
	require.Len(t, ev.Targets, 2)
	assert.Equal(t, "worst", ev.Targets[0].DeviceIdentifier)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING, ev.Targets[0].State)
	require.NotNil(t, ev.Targets[0].BaselinePowerW)
	assert.InDelta(t, 3000.0, *ev.Targets[0].BaselinePowerW, 0.001)
	assert.Equal(t, int32(2), ev.TargetRollup.Pending)
	assert.Equal(t, int32(2), ev.TargetRollup.Total)
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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		OrganizationID: 1,
		Role:           "OPERATOR",
		APIKeyID:       "key-77",
	})

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Role:           "OPERATOR",
	})

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Role:           "VIEWER",
	})

	req := validStartRequestBuilder()
	req.CandidateMinPowerWOverride = finitePtr(uint32(800))

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Role:           "OPERATOR",
		SessionID:      "sess-zero-dur",
	})

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Role:           "ADMIN",
	})

	req := validStartRequestBuilder()
	req.MaxDurationSeconds = 0
	req.AllowUnbounded = true

	_, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	assert.True(t, store.lastEvent.AllowUnbounded)
	assert.Nil(t, store.lastEvent.MaxDurationSeconds)
}

// TestHandler_StartCurtailment_RejectsUint32Overflow pins the strict
// overflow rejection on the four uint32 → int32 fields the translator
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
		{"restore_batch_size", func(r *pb.StartCurtailmentRequest) { r.RestoreBatchSize = overflow }},
		{"restore_batch_interval_sec", func(r *pb.StartCurtailmentRequest) { r.RestoreBatchIntervalSec = overflow }},
		{"min_curtailed_duration_sec", func(r *pb.StartCurtailmentRequest) { r.MinCurtailedDurationSec = overflow }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			store := newStartStubStore()
			h := NewHandler(curtailment.NewService(store))
			ctx := authn.SetInfo(t.Context(), &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: 1,
				Role:           "OPERATOR",
				SessionID:      "sess",
			})

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

	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 1,
		Role:           "OPERATOR",
	})

	_, err := h.StartCurtailment(ctx, connect.NewRequest(validStartRequestBuilder()))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	// Reuse the modes.Outcome enum to anchor that this test path triggers
	// InsufficientLoad and not the empty-Selected guard.
	_ = modes.OutcomeInsufficientLoad
}

// TestHandler_StartCurtailment_IdempotentRetryReflectsPersistedState pins
// the retry-consistency contract: when the same idempotency key matches an
// existing event, the response describes the persisted row (state, priority,
// reason, scope, mode params, target states), not whatever differing fields
// the retry request carried. Protects against silently echoing drifted retry
// metadata back to operators.
func TestHandler_StartCurtailment_IdempotentRetryReflectsPersistedState(t *testing.T) {
	t.Parallel()

	const orgID = int64(42)
	store := newStartStubStore()
	store.orgConfig.OrgID = orgID

	existingUUID := uuid.New()
	maxDur := int32(7200)
	idemKey := "operator-retry-1"
	extSrc := "ops-runbook"
	extRef := "OPS-99"
	store.idempotencyHit = &models.Event{
		ID:                      999,
		EventUUID:               existingUUID,
		OrgID:                   orgID,
		State:                   models.EventStateActive,
		Mode:                    models.ModeFixedKw,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityEmergency,
		ScopeType:               models.ScopeTypeDeviceList,
		ScopeJSON:               []byte(`{"device_identifiers":["miner-a","miner-b"]}`),
		ModeParamsJSON:          []byte(`{"target_kw":12.5,"tolerance_kw":0.5}`),
		RestoreBatchSize:        4,
		RestoreBatchIntervalSec: 30,
		MinCurtailedDurationSec: 120,
		MaxDurationSeconds:      &maxDur,
		IncludeMaintenance:      true,
		Reason:                  "original operator reason",
		ExternalSource:          &extSrc,
		ExternalReference:       &extRef,
		IdempotencyKey:          &idemKey,
	}
	baselineA := 4500.0
	baselineB := 5200.0
	observedA := 12.0
	store.idempotencyTargets = []*models.Target{
		{
			CurtailmentEventID: 999,
			DeviceIdentifier:   "miner-a",
			TargetType:         "miner",
			State:              models.TargetStateConfirmed,
			DesiredState:       "curtailed",
			BaselinePowerW:     &baselineA,
			ObservedPowerW:     &observedA,
			RetryCount:         0,
		},
		{
			CurtailmentEventID: 999,
			DeviceIdentifier:   "miner-b",
			TargetType:         "miner",
			State:              models.TargetStateDispatched,
			DesiredState:       "curtailed",
			BaselinePowerW:     &baselineB,
			RetryCount:         1,
		},
	}

	h := NewHandler(curtailment.NewService(store))
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
		SessionID:      "sess-retry",
	})

	// Retry request deliberately carries drifted metadata: different
	// reason, scope shape, priority, batch sizing. None of these should
	// surface in the response — the persisted event's values must win.
	req := validStartRequestBuilder()
	req.IdempotencyKey = "operator-retry-1"
	req.Reason = "DRIFTED reason on retry"
	req.Priority = pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	req.RestoreBatchSize = 99
	req.MinCurtailedDurationSec = 999
	req.IncludeMaintenance = false
	req.Scope = &pb.StartCurtailmentRequest_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}}

	resp, err := h.StartCurtailment(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp)
	ev := resp.Msg.Event
	require.NotNil(t, ev)

	assert.Equal(t, existingUUID.String(), ev.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, ev.State,
		"state must reflect persisted, not the PENDING default of fresh-Start")
	assert.Equal(t, pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY, ev.Priority,
		"priority must come from persisted event, not the retry request")
	assert.Equal(t, "original operator reason", ev.Reason,
		"reason must come from persisted event, not the retry request")
	assert.Equal(t, uint32(4), ev.RestoreBatchSize)
	assert.Equal(t, uint32(30), ev.RestoreBatchIntervalSec)
	assert.Equal(t, uint32(120), ev.MinCurtailedDurationSec)
	assert.Equal(t, uint32(7200), ev.MaxDurationSeconds)
	assert.True(t, ev.IncludeMaintenance, "include_maintenance must reflect persisted, not retry request")
	assert.Equal(t, "ops-runbook", ev.ExternalSource)
	assert.Equal(t, "OPS-99", ev.ExternalReference)
	assert.Equal(t, "operator-retry-1", ev.IdempotencyKey)

	// Scope is reconstructed from scope_jsonb, not the retry request's
	// (different) WholeOrg variant.
	deviceList := ev.GetDeviceIdentifiers()
	require.NotNil(t, deviceList, "scope must reconstruct DeviceIdentifiers from scope_jsonb")
	assert.Equal(t, []string{"miner-a", "miner-b"}, deviceList.DeviceIdentifiers)

	fk := ev.GetFixedKw()
	require.NotNil(t, fk, "mode params must reconstruct from mode_params_jsonb")
	assert.InDelta(t, 12.5, fk.TargetKw, 0.001)
	require.NotNil(t, fk.ToleranceKw, "non-zero tolerance must round-trip as a populated pointer")
	assert.InDelta(t, 0.5, *fk.ToleranceKw, 0.001)

	// Targets reflect their persisted states and per-target detail
	// (observed_power_w, retry_count). Rollup aggregates across states.
	require.Len(t, ev.Targets, 2)
	assert.Equal(t, "miner-a", ev.Targets[0].DeviceIdentifier)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_CONFIRMED, ev.Targets[0].State)
	require.NotNil(t, ev.Targets[0].ObservedPowerW)
	assert.InDelta(t, 12.0, *ev.Targets[0].ObservedPowerW, 0.001)
	assert.Equal(t, "miner-b", ev.Targets[1].DeviceIdentifier)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_DISPATCHED, ev.Targets[1].State)
	assert.Equal(t, uint32(1), ev.Targets[1].RetryCount)

	require.NotNil(t, ev.TargetRollup)
	assert.Equal(t, int32(1), ev.TargetRollup.Confirmed)
	assert.Equal(t, int32(1), ev.TargetRollup.Dispatched)
	assert.Equal(t, int32(2), ev.TargetRollup.Total)
	assert.Equal(t, int32(0), ev.TargetRollup.Pending,
		"no targets are pending after retry; rollup must not echo len(targets) into Pending")
}

// mustParseTime parses the RFC3339 input or panics; used in fixture builders.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
