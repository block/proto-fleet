package curtailment

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestAdminPolicy_FullFleetProfileAndManualStartRemainDifferent(t *testing.T) {
	t.Parallel()
	const orgID int64 = 42
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	req := validStartRequest(orgID)
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.CanUseAdminControls = false
	plan, err := NewService(store).Start(t.Context(), req)
	require.NoError(t, err, "ordinary manual FULL_FLEET starts are available to operators")
	assert.Equal(t, models.EventStateActive, plan.StartState)
	required, err := EventRequiresAdminControls(&models.Event{
		Mode: models.ModeFullFleet, DecisionSnapshotJSON: store.lastInsertEvent.DecisionSnapshotJSON,
	}, store.orgConfigByOrg[orgID])
	require.NoError(t, err)
	assert.False(t, required, "FULL_FLEET alone must not become a persisted admin requirement")

	_, err = NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{OrgID: orgID, ProfileName: "Fleet profile", Mode: models.ModeFullFleet},
	})
	require.Error(t, err, "saving FULL_FLEET profiles remains admin-only")
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestControlValidation_RestoreBoundsAgreeAcrossStartProfilesAndUpdate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		batch, interval int32
		valid           bool
	}{
		{"explicit immediate restore", 0, 0, true},
		{"absolute ceilings", RestoreBatchSizeMax, batchIntervalUpperBoundSec, true},
		{"negative batch", -1, 0, false},
		{"oversized batch", RestoreBatchSizeMax + 1, 0, false},
		{"negative interval", 0, -1, false},
		{"oversized interval", 0, batchIntervalUpperBoundSec + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := validStartRequest(42)
			req.CanUseAdminControls = true
			req.RestoreBatchSize, req.RestoreBatchIntervalSec = tc.batch, tc.interval
			targetKW := req.TargetKW
			profile := normalizeResponseProfile(models.ResponseProfile{
				OrgID: 42, Mode: models.ModeFixedKw, TargetKW: &targetKW,
				RestoreBatchSize: tc.batch, RestoreBatchIntervalSec: tc.interval,
			})
			checks := map[string]error{
				"start":   validateStartRequest(req),
				"profile": validateResponseProfileBehavior(profile, true),
				"update":  validateUpdateRequest(UpdateRequest{RestoreBatchSize: &tc.batch, RestoreBatchIntervalSec: &tc.interval}),
			}
			for name, err := range checks {
				if tc.valid {
					assert.NoError(t, err, name)
				} else {
					assert.True(t, fleeterror.IsInvalidArgumentError(err), "%s: %v", name, err)
				}
			}
		})
	}
}

func TestExecutionFenceValidation_PreservesCallerAttributionRules(t *testing.T) {
	t.Parallel()
	// The store receives a creator on manual inserts. A Recurtail service user,
	// however, is meaningful only as part of a complete automation fence.
	manual := ExecutionFence{AutomationServiceUserID: 42}
	assert.NoError(t, ValidateExecutionFence(manual, ExecutionFenceStore))
	assert.Error(t, ValidateExecutionFence(manual, ExecutionFenceRecurtail))

	automated := ExecutionFence{
		ResponseProfileID: 1, ResponseProfileRevision: uuid.New(),
		AutomationRuleID: 2, AutomationMQTTSourceID: 3, AutomationServiceUserID: 42,
	}
	assert.NoError(t, ValidateExecutionFence(automated, ExecutionFenceStore))
	assert.NoError(t, ValidateExecutionFence(automated, ExecutionFenceRecurtail))
	assert.Error(t, ValidateExecutionFence(automated, ExecutionFenceStart), "Start also requires automation actor attribution")
	automated.SourceActorType = models.SourceActorAutomation
	assert.NoError(t, ValidateExecutionFence(automated, ExecutionFenceStart))
	automated.AutomationServiceUserID = 0
	assert.NoError(t, ValidateExecutionFence(automated, ExecutionFenceStart), "Start separately validates its creator ID")
	assert.Error(t, ValidateExecutionFence(automated, ExecutionFenceRecurtail))
	assert.Error(t, ValidateExecutionFence(automated, ExecutionFenceStore))
}

func TestServiceStart_PlanStateMatchesPersistedLifecycle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		scope    Scope
		hasMiner bool
		state    models.EventState
	}{
		{"targetless watcher", Scope{Type: models.ScopeTypeWholeOrg}, false, models.EventStateActive},
		{"empty explicit scope", Scope{Type: models.ScopeTypeDeviceList, DeviceIdentifiers: []string{"a"}}, false, models.EventStateCompleted},
		{"explicit target pending dispatch", Scope{Type: models.ScopeTypeDeviceList, DeviceIdentifiers: []string{"a"}}, true, models.EventStatePending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore()
			store.orgConfigByOrg[42] = defaultOrgConfig(42)
			if tc.hasMiner {
				store.candidatesByOrg[42] = []*models.Candidate{minerWithEff("a", 6000, 100, 40)}
			} else if tc.scope.Type == models.ScopeTypeDeviceList {
				store.candidatesByOrg[42] = []*models.Candidate{miner("a", "OFFLINE", "PAIRED", 0, 0)}
			}
			req := validStartRequest(42)
			req.Mode, req.TargetKW, req.Scope = models.ModeFullFleet, 0, tc.scope
			plan, err := NewService(store).Start(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.state, plan.StartState)
			assert.Equal(t, store.lastInsertEvent.State, plan.StartState)
		})
	}
}

func TestServiceStart_MinimumDurationBoundsAndEffectiveCap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		minimum   int32
		cap       *int32
		wantError bool
	}{
		{"minimum at hard ceiling", maxFiniteDurationSeconds, int32Ptr(maxFiniteDurationSeconds), false},
		{"minimum above hard ceiling", maxFiniteDurationSeconds + 1, int32Ptr(maxFiniteDurationSeconds), true},
		{"minimum above explicit cap", 601, int32Ptr(600), true},
		{"minimum above organization default", 14401, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.orgConfigByOrg[42] = defaultOrgConfig(42)
			store.candidatesByOrg[42] = []*models.Candidate{minerWithEff("a", 6000, 100, 40)}
			req := validStartRequest(42)
			req.MinCurtailedDurationSec, req.MaxDurationSeconds, req.CanUseAdminControls = tc.minimum, tc.cap, true
			_, err := NewService(store).Start(t.Context(), req)
			if tc.wantError {
				require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
				assert.Zero(t, store.insertEventCalls)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
