package curtailment

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// recordingAuditLogger captures every Log/LogStrict call so tests can
// pin the emitted activity rows. The mutex defends against the
// (currently serial, but enforced anyway) emission loop.
//
// logStrictErr lets tests inject a persistence failure on the strict
// path so the IncAuditWriteFailure observability hook can be pinned.
type recordingAuditLogger struct {
	mu           sync.Mutex
	events       []activitymodels.Event
	logStrictErr error
}

func (r *recordingAuditLogger) Log(_ context.Context, event activitymodels.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// LogStrict mirrors Log but returns logStrictErr (nil by default). When
// non-nil the test signals "the activity store failed to persist this
// row" and service code routes through Metrics.IncAuditWriteFailure.
func (r *recordingAuditLogger) LogStrict(_ context.Context, event activitymodels.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.logStrictErr != nil {
		return r.logStrictErr
	}
	r.events = append(r.events, event)
	return nil
}

func (r *recordingAuditLogger) snapshot() []activitymodels.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]activitymodels.Event, len(r.events))
	copy(out, r.events)
	return out
}

// TestService_Start_EmitsBaseAuditRowOnSuccess: every successful Start
// records exactly one curtailment_started row carrying the event UUID
// and override flags. Override-specific rows are absent here.
func TestService_Start_EmitsBaseAuditRowOnSuccess(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.TargetKW = 2

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID)

	events := audit.snapshot()
	require.Len(t, events, 1, "base curtailment_started row only")
	assert.Equal(t, ActivityTypeStarted, events[0].Type)
	assert.Equal(t, activitymodels.CategoryCurtailment, events[0].Category)
	assert.Equal(t, activitymodels.ResultSuccess, events[0].Result)
	assert.Equal(t, activitymodels.ActorUser, events[0].ActorType,
		"default SourceActorUser must map to ActorUser")
	require.NotNil(t, events[0].Metadata)
	assert.Equal(t, plan.EventUUID.String(), events[0].Metadata["event_uuid"])
	assert.Equal(t, string(models.ModeFixedKw), events[0].Metadata["mode"])
	assert.Equal(t, float64(2), events[0].Metadata["target_kw"])
	assert.Equal(t, req.ToleranceKW, events[0].Metadata["tolerance_kw"])
	assert.Equal(t, false, events[0].Metadata["allow_unbounded"])
	assert.Equal(t, false, events[0].Metadata["force_include_maintenance"])
}

// TestService_Start_FullFleetAuditOmitsTargetKW: target_kw is only meaningful
// for FIXED_KW. FULL_FLEET still records mode so a grouped audit entry can be
// interpreted without inspecting the persisted event.
func TestService_Start_FullFleetAuditOmitsTargetKW(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.ToleranceKW = 0

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Metadata)
	assert.Equal(t, string(models.ModeFullFleet), events[0].Metadata["mode"])
	assert.NotContains(t, events[0].Metadata, "target_kw")
	assert.NotContains(t, events[0].Metadata, "tolerance_kw")
}

// TestService_Start_MapsSchedulerActorType: a Start initiated by the
// internal scheduler (SourceActorType=scheduler) records ActorScheduler on
// the audit row, distinguishing automated runs from operator actions in the
// audit feed.
func TestService_Start_MapsSchedulerActorType(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.TargetKW = 2
	req.SourceActorType = models.SourceActorScheduler

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, activitymodels.ActorScheduler, events[0].ActorType,
		"SourceActorScheduler must map to ActorScheduler")
}

// TestService_Start_CoercesAPIKeyActorTypeToUser: SourceActorAPIKey is the
// curtailment-vocabulary distinction between session-token and API-key
// callers; the activity_log doesn't yet model an api_key actor, so the
// audit row uses ActorUser. Pinning this so a future ActorAPIKey addition
// can't silently keep the legacy mapping.
func TestService_Start_CoercesAPIKeyActorTypeToUser(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.TargetKW = 2
	req.SourceActorType = models.SourceActorAPIKey

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, activitymodels.ActorUser, events[0].ActorType,
		"SourceActorAPIKey currently coerces to ActorUser pending an ActorAPIKey activity type")
}

// TestService_Start_EmitsUnboundedAuditRowWhenAllowUnbounded: a Start
// with allow_unbounded=true emits the base row plus a typed
// curtailment_unbounded_start row.
func TestService_Start_EmitsUnboundedAuditRowWhenAllowUnbounded(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.TargetKW = 2
	req.AllowUnbounded = true
	req.MaxDurationSeconds = nil
	req.CanUseAdminControls = true // allow_unbounded is admin-gated

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 2)
	assert.Equal(t, ActivityTypeStarted, events[0].Type)
	assert.Equal(t, ActivityTypeStartedUnbounded, events[1].Type)
	assert.Equal(t, true, events[1].Metadata["allow_unbounded"])
}

// TestService_Start_EmitsForceMaintenanceAuditRowAndMetric: a Start
// with force_include_maintenance=true emits the base row plus the
// override-specific row AND increments IncMaintenanceOverride.
func TestService_Start_EmitsForceMaintenanceAuditRowAndMetric(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{}
	metrics := newRecordingMetrics()
	svc := NewService(store, WithAuditLogger(audit), WithServiceMetrics(metrics))

	req := validStartRequest(orgID)
	req.TargetKW = 2
	// IncludeMaintenance + ForceIncludeMaintenance both true so the
	// validator's mutual-exclusion gate is satisfied.
	req.IncludeMaintenance = true
	req.ForceIncludeMaintenance = true
	req.CanUseAdminControls = true

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 2)
	assert.Equal(t, ActivityTypeStarted, events[0].Type)
	assert.Equal(t, ActivityTypeStartedForceMaintenance, events[1].Type)
	assert.Equal(t, true, events[1].Metadata["force_include_maintenance"])

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	assert.Equal(t, 1, metrics.maintenance,
		"force_include_maintenance must increment IncMaintenanceOverride")
}

// TestService_Start_NoAuditOnInsufficientLoad: insufficient-load
// rejects without persisting, so no audit row fires.
func TestService_Start_NoAuditOnInsufficientLoad(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 100, 10, 50), // ~100 W candidate
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	req.TargetKW = 999_999 // wildly above available

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.InsufficientLoadDetail)

	assert.Empty(t, audit.snapshot(),
		"insufficient-load path must not emit an audit row")
}

// TestService_Start_NoAuditOnIdempotencyReplay: a replay returns the
// existing event without re-emitting audit rows. The original Start
// already recorded the activity trail; a duplicate webhook delivery
// should not double-log.
func TestService_Start_NoAuditOnIdempotencyReplay(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.eventsByIdempotencyKey = map[string]*models.Event{
		"key-1": {ID: 1, OrgID: orgID, State: models.EventStateActive},
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	req := validStartRequest(orgID)
	key := "key-1"
	req.IdempotencyKey = &key

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	assert.Empty(t, audit.snapshot(),
		"idempotent replay must not re-emit the audit trail")
}

// An audit persistence failure must increment IncAuditWriteFailure
// while Start still succeeds (audit is best-effort).
func TestService_Start_AuditPersistenceFailureIncrementsMetric(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	audit := &recordingAuditLogger{
		logStrictErr: errors.New("activity store offline"),
	}
	metrics := newRecordingMetrics()
	svc := NewService(store, WithAuditLogger(audit), WithServiceMetrics(metrics))

	req := validStartRequest(orgID)
	req.TargetKW = 2

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "audit failure is best-effort; Start must still succeed")
	assert.Equal(t, 1, metrics.AuditWriteFailureCount(ActivityTypeStarted),
		"audit persistence failure on curtailment_started must increment IncAuditWriteFailure(curtailment_started)")
}
