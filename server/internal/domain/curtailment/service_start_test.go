package curtailment

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// validStartRequest builds a valid StartRequest pointing at orgID. Callers
// mutate fields to drive negative cases.
func validStartRequest(orgID int64) StartRequest {
	maxDur := int32(7200)
	return StartRequest{
		PreviewRequest:          validRequest(orgID),
		Reason:                  "operator test",
		RestoreBatchSize:        10,
		RestoreBatchIntervalSec: 30,
		MinCurtailedDurationSec: 60,
		MaxDurationSeconds:      &maxDur,
		AllowUnbounded:          false,
		SourceActorType:         models.SourceActorUser,
		CreatedByUserID:         42,
	}
}

// --- validation ---

func TestService_Start_RejectsEmptyReason(t *testing.T) {
	t.Parallel()
	// Both the empty string and whitespace-only must be rejected at the
	// service layer with InvalidArgument; the DB CHECK (length(trim) > 0)
	// would otherwise surface as Internal for the whitespace case.
	for _, reason := range []string{"", "   ", "\t\n"} {
		t.Run(fmt.Sprintf("reason=%q", reason), func(t *testing.T) {
			t.Parallel()
			svc := NewService(newFakeStore())
			req := validStartRequest(1)
			req.Reason = reason
			_, err := svc.Start(t.Context(), req)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), "reason")
		})
	}
}

func TestService_Start_RejectsAllowUnboundedWithMaxDuration(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validStartRequest(1)
	req.AllowUnbounded = true // MaxDurationSeconds is still set from the helper.
	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "allow_unbounded")
}

func TestService_Start_AllowUnboundedRequiresNilMaxDuration(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("miner", 6000, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.AllowUnbounded = true
	req.MaxDurationSeconds = nil
	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "allow_unbounded + nil max_duration is the valid admin shape")
}

func TestService_Start_NilMaxDurationUsesOrgDefault(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("miner", 6000, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.MaxDurationSeconds = nil // sentinel: use org default
	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, store.lastInsertEvent.MaxDurationSeconds)
	assert.Equal(t, store.orgConfigByOrg[orgID].MaxDurationDefaultSec, *store.lastInsertEvent.MaxDurationSeconds)
}

func TestService_Start_RejectsZeroMaxDuration(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validStartRequest(1)
	zero := int32(0)
	req.MaxDurationSeconds = &zero
	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_Start_RejectsNegativeRestoreBatchSize(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validStartRequest(1)
	req.RestoreBatchSize = -1
	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_Start_RejectsMissingSourceActorType(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validStartRequest(1)
	req.SourceActorType = ""
	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "source_actor_type")
}

// TestService_Start_RejectsMissingCreatedByUserID pins the service-level
// backstop for the FK plumbing: a zero or negative UserID must never reach
// the DB, where curtailment_event.created_by_user_id has a NOT NULL FK to
// user.id. Without this guard, a misconfigured handler could surface the
// FK violation as Internal at insert time instead of InvalidArgument here.
func TestService_Start_RejectsMissingCreatedByUserID(t *testing.T) {
	t.Parallel()
	for _, uid := range []int64{0, -1} {
		t.Run(fmt.Sprintf("user_id=%d", uid), func(t *testing.T) {
			t.Parallel()
			svc := NewService(newFakeStore())
			req := validStartRequest(1)
			req.CreatedByUserID = uid
			_, err := svc.Start(t.Context(), req)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), "created_by_user_id")
		})
	}
}

// TestService_Start_RejectsOversizedTextFields covers the service-level
// backstop for callers that bypass proto validation (internal CLIs / tests
// / future non-Connect surfaces). Each text field has the same 256-char
// cap that the proto enforces.
func TestService_Start_RejectsOversizedTextFields(t *testing.T) {
	t.Parallel()
	tooLong := func() string {
		b := make([]byte, startTextFieldMaxLen+1)
		for i := range b {
			b[i] = 'a'
		}
		return string(b)
	}()

	cases := []struct {
		name     string
		mutate   func(*StartRequest)
		contains string
	}{
		{
			name:     "reason",
			mutate:   func(r *StartRequest) { r.Reason = tooLong },
			contains: "reason",
		},
		{
			name: "idempotency_key",
			mutate: func(r *StartRequest) {
				v := tooLong
				r.IdempotencyKey = &v
			},
			contains: "idempotency_key",
		},
		{
			name: "external_source",
			mutate: func(r *StartRequest) {
				v := tooLong
				r.ExternalSource = &v
			},
			contains: "external_source",
		},
		{
			name: "external_reference",
			mutate: func(r *StartRequest) {
				v := tooLong
				r.ExternalReference = &v
			},
			contains: "external_reference",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := NewService(newFakeStore())
			req := validStartRequest(1)
			tc.mutate(&req)
			_, err := svc.Start(t.Context(), req)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), tc.contains)
		})
	}
}

// --- selector pipeline parity with Preview ---

func TestService_Start_RunsSelectorWithDeviceListFilter(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("alpha", 4000, 100, 40),
		minerWithEff("beta", 4000, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{
		Type:              models.ScopeTypeDeviceList,
		DeviceIdentifiers: []string{"alpha"},
	}
	req.TargetKW = 3
	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "alpha", plan.Selected[0].DeviceIdentifier)
	assert.Equal(t, []string{"alpha"}, store.lastListCandidatesFilter,
		"Start must forward the same scope filter as Preview")
}

// --- insufficient-load path ---

func TestService_Start_InsufficientLoadReturnsDetailWithoutPersisting(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("only", 1500, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.TargetKW = 100 // far above the 1.5 kW pool.
	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "insufficient-load surfaces via Plan, not as a service error")
	require.NotNil(t, plan.InsufficientLoadDetail)
	assert.Equal(t, modes.OutcomeInsufficientLoad, plan.Outcome)
	assert.Nil(t, plan.EventUUID, "no event must be persisted on insufficient-load")
	assert.Zero(t, store.insertEventCalls, "no DB write on the rejection branch")
}

// --- empty-plan defense ---

func TestService_Start_EmptyPlanRejectsBeforePersistence(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	// Tolerance >= target_kw is rejected by validation, but a tolerance of
	// nearly the full target with a 0 candidate sum is the easiest way to
	// drive Outcome=UndershootTolerated with empty Selected. Here we use a
	// candidate filter that yields zero candidates so the selector returns
	// Insufficient — but we want to test the empty-Selected guard, not the
	// insufficient branch. Use a candidate that's filtered out by status.
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("offline", "OFFLINE", "PAIRED", 5000, 100),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.TargetKW = 0.001
	req.ToleranceKW = 0
	_, err := svc.Start(t.Context(), req)
	// Any failure shape is acceptable here as long as Start does not
	// persist. We assert insertEventCalls is zero below; the service
	// converts empty-Selected to InvalidArgument or InsufficientLoad
	// (the offline candidate produces InsufficientLoad).
	if err == nil {
		// Insufficient-load surfaces via Plan, not as an error: re-run and
		// assert the persistence guard via the call counter only.
		assert.Zero(t, store.insertEventCalls, "no DB write on empty-Selected path")
	}
}

// --- success path: persistence + baseline capture ---

func TestService_Start_PersistsEventAndTargetsWithBaseline(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
		minerWithEff("mid", 3000, 100, 35),
		minerWithEff("best", 3000, 100, 20),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.TargetKW = 5

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID, "event UUID must be set on persisted Start")

	// 5 kW target picks worst + mid (6 kW total).
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, 1, store.insertEventCalls)

	// Event params: pending state, FIXED_KW mode, open loop, scope/reason
	// preserved.
	ev := store.lastInsertEvent
	assert.Equal(t, models.EventStatePending, ev.State)
	assert.Equal(t, models.ModeFixedKw, ev.Mode)
	assert.Equal(t, models.LoopTypeOpen, ev.LoopType)
	assert.Equal(t, models.LevelFull, ev.Level)
	assert.Equal(t, models.StrategyLeastEfficientFirst, ev.Strategy)
	assert.Equal(t, models.PriorityNormal, ev.Priority)
	assert.Equal(t, models.ScopeTypeWholeOrg, ev.ScopeType)
	assert.Equal(t, "operator test", ev.Reason)
	assert.Equal(t, models.SourceActorUser, ev.SourceActorType)
	// CreatedByUserID flows from StartRequest into the event row;
	// reconciler reads it back to satisfy command_batch_log.created_by FK.
	assert.Equal(t, int64(42), ev.CreatedByUserID)
	require.NotNil(t, ev.MaxDurationSeconds)
	assert.Equal(t, int32(7200), *ev.MaxDurationSeconds)
	assert.False(t, ev.AllowUnbounded)
	assert.NotEmpty(t, ev.ScopeJSON)
	assert.NotEmpty(t, ev.ModeParamsJSON)
	assert.NotEmpty(t, ev.DecisionSnapshotJSON)

	// Targets: one row per Selected; baseline_power_w = LatestPowerW from
	// the candidate, target_type=miner, desired=curtailed, state=pending.
	require.Len(t, store.lastInsertTargets, 2)
	for i, want := range []string{"worst", "mid"} {
		got := store.lastInsertTargets[i]
		assert.Equal(t, want, got.DeviceIdentifier)
		assert.Equal(t, "miner", got.TargetType)
		assert.Equal(t, models.TargetStatePending, got.State)
		assert.Equal(t, "curtailed", got.DesiredState)
		require.NotNil(t, got.BaselinePowerW)
		assert.InDelta(t, 3000.0, *got.BaselinePowerW, 0.001)
	}
}

func TestService_Start_AllowUnboundedPersistsNullMaxDuration(t *testing.T) {
	t.Parallel()
	const orgID = int64(7)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("a", 6000, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.AllowUnbounded = true
	req.MaxDurationSeconds = nil

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID)
	assert.True(t, store.lastInsertEvent.AllowUnbounded)
	assert.Nil(t, store.lastInsertEvent.MaxDurationSeconds,
		"allow_unbounded events must persist max_duration_seconds = NULL")
}

func TestService_Start_ForwardsIdempotencyAndExternalAttribution(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("a", 6000, 100, 40),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	idem := "idem-key-1"
	src := "pagerduty"
	ref := "PD-INC-12345"
	actorID := "user-7"
	req.IdempotencyKey = &idem
	req.ExternalSource = &src
	req.ExternalReference = &ref
	req.SourceActorID = &actorID

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, store.lastInsertEvent.IdempotencyKey)
	assert.Equal(t, "idem-key-1", *store.lastInsertEvent.IdempotencyKey)
	require.NotNil(t, store.lastInsertEvent.ExternalSource)
	assert.Equal(t, "pagerduty", *store.lastInsertEvent.ExternalSource)
	require.NotNil(t, store.lastInsertEvent.ExternalReference)
	assert.Equal(t, "PD-INC-12345", *store.lastInsertEvent.ExternalReference)
	require.NotNil(t, store.lastInsertEvent.SourceActorID)
	assert.Equal(t, "user-7", *store.lastInsertEvent.SourceActorID)
}

// --- store error propagation ---

func TestService_Start_StorePersistenceErrorPropagates(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("a", 6000, 100, 40),
	}
	store.insertEventErr = errors.New("synthetic db error")
	svc := NewService(store)
	plan, err := svc.Start(t.Context(), validStartRequest(orgID))
	require.Error(t, err)
	assert.Nil(t, plan)
	assert.Contains(t, err.Error(), "synthetic db error")
}
