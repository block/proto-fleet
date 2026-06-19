package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// FULL_FLEET curtails every eligible miner regardless of target_kw and persists
// the event with mode=FULL_FLEET in the normal PENDING lifecycle.
func TestService_Start_FullFleet_CurtailsAllEligible(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("a", 6000, 100, 40),
		minerWithEff("b", 5000, 100, 45),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0 // ignored by full_fleet

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	assert.Len(t, plan.Selected, 2, "full_fleet curtails every eligible miner")
	assert.Equal(t, models.ModeFullFleet, store.lastInsertEvent.Mode)
	assert.Equal(t, models.EventStatePending, store.lastInsertEvent.State)
	assert.Len(t, store.lastInsertTargets, 2)
}

func TestService_Start_FullFleet_CurtailsLowPowerAndZeroHashrateMiners(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("low-power-hashing", 100, 100, 40),
		minerWithEff("not-yet-hashing", 2000, 0, 45),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, "not-yet-hashing", plan.Selected[0].DeviceIdentifier,
		"full_fleet still ranks by efficiency when selecting all eligible miners")
	assert.Equal(t, "low-power-hashing", plan.Selected[1].DeviceIdentifier)
	assert.Empty(t, plan.Skipped)
	require.Len(t, store.lastInsertTargets, 2)
	require.NotNil(t, store.lastInsertTargets[0].BaselinePowerW,
		"above-floor full_fleet targets keep a power baseline")
	assert.InDelta(t, 2000, *store.lastInsertTargets[0].BaselinePowerW, 0.001)
	assert.Nil(t, store.lastInsertTargets[1].BaselinePowerW,
		"below-floor full_fleet targets confirm via hash-rate fallback")
}

func TestService_Preview_FullFleet_SkipsMissingTelemetrySamples(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)

	missingPower := miner("missing-power", "ACTIVE", "PAIRED", 0, 100)
	missingPower.LatestPowerW = nil
	missingHash := miner("missing-hash", "ACTIVE", "PAIRED", 100, 0)
	missingHash.LatestHashRateHS = nil
	negativePower := miner("negative-power", "ACTIVE", "PAIRED", -1, 100)
	negativeHash := miner("negative-hash", "ACTIVE", "PAIRED", 100, -1)
	measuredZero := minerWithEff("measured-zero", 0, 0, 40)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		missingPower,
		missingHash,
		negativePower,
		negativeHash,
		measuredZero,
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "measured-zero", plan.Selected[0].DeviceIdentifier,
		"measured zero values are valid for full_fleet; missing samples are not")
	require.Len(t, plan.Skipped, 4)
	for _, skipped := range plan.Skipped {
		assert.Equal(t, SkipStaleTelemetry, skipped.Reason)
	}
}

// The empty-eligible case is the chosen behavior: persist a vacuously COMPLETED
// event with no targets, not an insufficient-load rejection.
func TestService_Start_FullFleet_NoEligibleMinersPersistsCompleted(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	// candidatesByOrg[orgID] left unset: nothing eligible.
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "empty full_fleet is valid, not an insufficient-load rejection")
	assert.Empty(t, plan.Selected)
	assert.Equal(t, models.ModeFullFleet, store.lastInsertEvent.Mode)
	assert.Equal(t, models.EventStateCompleted, store.lastInsertEvent.State,
		"nothing eligible == vacuously complete on arrival")
	assert.NotNil(t, store.lastInsertEvent.EndedAt, "a completed-empty event records its completion time")
	assert.Empty(t, store.lastInsertTargets, "a completed-empty event has no targets")
}

func TestService_Preview_FullFleet_AllSkippedReturnsInsufficientDetail(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("offline", "OFFLINE", "PAIRED", 0, 0),
		miner("updating", "UPDATING", "PAIRED", 0, 0),
		staleMiner("stale"),
	}
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.InsufficientLoadDetail)
	assert.Equal(t, modes.OutcomeInsufficientLoad, plan.Outcome)
	assert.Empty(t, plan.Selected)
	assert.Len(t, plan.Skipped, 3)
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedOffline)
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedUpdating)
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedStale)
	assert.Zero(t, plan.InsufficientLoadDetail.ExcludedBelowThreshold)
	assert.Equal(t, int32(1500), plan.InsufficientLoadDetail.CandidateMinPowerW)
	assert.Zero(t, store.insertEventCalls, "Preview must not persist")
}

func TestService_Start_FullFleet_AllSkippedDoesNotPersist(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("offline", "OFFLINE", "PAIRED", 0, 0),
		staleMiner("stale"),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "all-skipped full_fleet surfaces via Plan, not as a service error")
	require.NotNil(t, plan.InsufficientLoadDetail)
	assert.Equal(t, modes.OutcomeInsufficientLoad, plan.Outcome)
	assert.Nil(t, plan.EventUUID, "no event must be persisted when no miner was actionable")
	assert.Zero(t, store.insertEventCalls, "all-skipped full_fleet must not persist a completed event")
}

func TestService_Start_FullFleet_MixedSelectedAndSkippedPersists(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("eligible", 6000, 100, 40),
		miner("offline", "OFFLINE", "PAIRED", 0, 0),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	assert.Nil(t, plan.InsufficientLoadDetail)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "eligible", plan.Selected[0].DeviceIdentifier)
	assert.Len(t, plan.Skipped, 1)
	assert.Equal(t, 1, store.insertEventCalls)
	assert.Equal(t, models.EventStatePending, store.lastInsertEvent.State)
	assert.Len(t, store.lastInsertTargets, 1)
}

// FIXED_KW still requires a positive target_kw; FULL_FLEET does not.
func TestService_Start_FullFleet_IgnoresTargetKwValidation(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{minerWithEff("a", 6000, 100, 40)}
	svc := NewService(store)

	fixed := validStartRequest(orgID)
	fixed.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	fixed.Mode = models.ModeFixedKw
	fixed.TargetKW = 0
	_, err := svc.Start(t.Context(), fixed)
	require.Error(t, err, "FIXED_KW with target_kw=0 is rejected")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	full := validStartRequest(orgID)
	full.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	full.Mode = models.ModeFullFleet
	full.TargetKW = 0
	_, err = svc.Start(t.Context(), full)
	require.NoError(t, err, "FULL_FLEET ignores target_kw")
}
