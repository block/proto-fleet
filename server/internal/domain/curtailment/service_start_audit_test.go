package curtailment

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// recordingAuditLogger captures every Log call so tests can pin the
// emitted activity rows. The mutex defends against the (currently
// serial, but enforced anyway) emission loop.
type recordingAuditLogger struct {
	mu     sync.Mutex
	events []activitymodels.Event
}

func (r *recordingAuditLogger) Log(_ context.Context, event activitymodels.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
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
	require.NotNil(t, events[0].Metadata)
	assert.Equal(t, plan.EventUUID.String(), events[0].Metadata["event_uuid"])
	assert.Equal(t, false, events[0].Metadata["allow_unbounded"])
	assert.Equal(t, false, events[0].Metadata["force_include"])
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

	_, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 2)
	assert.Equal(t, ActivityTypeStarted, events[0].Type)
	assert.Equal(t, ActivityTypeStartedForceMaintenance, events[1].Type)
	assert.Equal(t, true, events[1].Metadata["force_include"])

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
