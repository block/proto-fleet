package curtailment

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// FULL_FLEET selects every eligible miner regardless of target_kw and persists
// a closed-loop event; the reconciler claims per-miner rows at dispatch time.
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
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Equal(t, models.EventStateActive, store.lastInsertEvent.State)
	assert.Empty(t, store.lastInsertTargets)
}

func TestService_Start_FullFleet_PersistsCooldownForClosedLoopAdmission(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.cooldownDevicesByOrg[orgID] = []string{"recent"}
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("recent", 6000, 100, 40),
		minerWithEff("fresh", 5000, 100, 45),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.PostEventCooldownSec = 600

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, 1, store.cooldownCalls)
	assert.Equal(t, int32(600), store.lastCooldownSec)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "fresh", plan.Selected[0].DeviceIdentifier)
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Empty(t, store.lastInsertTargets)

	var snapshot struct {
		PostEventCooldownSec int32 `json:"post_event_cooldown_sec"`
	}
	require.NoError(t, json.Unmarshal(store.lastInsertEvent.DecisionSnapshotJSON, &snapshot))
	assert.Equal(t, int32(600), snapshot.PostEventCooldownSec)
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
	assert.Empty(t, store.lastInsertTargets,
		"closed-loop full_fleet claims per-miner rows at dispatch time")
}

func TestService_Start_FullFleet_AllPairedPersistsPolicyTargetsImmediately(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("online", "ACTIVE", "PAIRED", 6000, 100),
		miner("auth-needed", "ACTIVE", "AUTHENTICATION_NEEDED", 0, 0),
		miner("offline", "OFFLINE", "PAIRED", 0, 0),
		miner("unpaired", "ACTIVE", "UNPAIRED", 6000, 100),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.ForceIncludeAllPairedMiners = true
	req.CanUseAdminControls = true

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.True(t, store.lastInsertEvent.ForceIncludeAllPairedMiners)
	assert.Equal(t, models.EventStateActive, store.lastInsertEvent.State,
		"a dispatchable target exists, so enforcement starts immediately")
	assert.NotNil(t, store.lastInsertEvent.StartedAt)
	assert.Equal(t, 3, plan.PolicyTargetCount)
	assert.Equal(t, 2, plan.UnavailableTargetCount)
	require.Len(t, store.lastInsertTargets, 3)
	assert.Equal(t, models.TargetStatePending, store.lastInsertTargets[0].State)
	assert.Nil(t, store.lastInsertTargets[0].LastError)
	assert.Equal(t, models.TargetStateUnavailable, store.lastInsertTargets[1].State)
	require.NotNil(t, store.lastInsertTargets[1].LastError)
	assert.Equal(t, "authentication_needed", *store.lastInsertTargets[1].LastError)
	assert.Equal(t, models.TargetStateUnavailable, store.lastInsertTargets[2].State)
	require.NotNil(t, store.lastInsertTargets[2].LastError)
	assert.Equal(t, "offline", *store.lastInsertTargets[2].LastError)

	var snapshot struct {
		ForceIncludeAllPairedMiners bool `json:"force_include_all_paired_miners"`
		PolicyTargetCount           int  `json:"policy_target_count"`
		UnavailableTargetCount      int  `json:"unavailable_target_count"`
	}
	require.NoError(t, json.Unmarshal(store.lastInsertEvent.DecisionSnapshotJSON, &snapshot))
	assert.True(t, snapshot.ForceIncludeAllPairedMiners)
	assert.Equal(t, 3, snapshot.PolicyTargetCount)
	assert.Equal(t, 2, snapshot.UnavailableTargetCount)
}

// An all-paired start whose every paired miner is currently unavailable
// holds in pending with no started_at: inserting it ACTIVE would start
// enforceMaxDuration's clock before a single Curtail could dispatch, and the
// forced restore would then release the never-dispatched policy rows —
// dropping durable ownership having curtailed nothing. The reconciler
// promotes the event to active once a target confirms.
func TestService_Start_FullFleet_AllPairedAllUnavailableHoldsPending(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("offline", "OFFLINE", "PAIRED", 0, 0),
		miner("auth-needed", "ACTIVE", "AUTHENTICATION_NEEDED", 0, 0),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.ForceIncludeAllPairedMiners = true
	req.CanUseAdminControls = true

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, models.EventStatePending, store.lastInsertEvent.State,
		"nothing dispatchable yet: the max-duration clock must not start")
	assert.Nil(t, store.lastInsertEvent.StartedAt)
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Equal(t, 2, plan.UnavailableTargetCount)
	require.Len(t, store.lastInsertTargets, 2)
	assert.Equal(t, models.TargetStateUnavailable, store.lastInsertTargets[0].State)
	assert.Equal(t, models.TargetStateUnavailable, store.lastInsertTargets[1].State)
}

func TestService_Start_FullFleet_AllPairedBypassesPostEventCooldown(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.cooldownDevicesByOrg[orgID] = []string{"recent"}
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("recent", "ACTIVE", "PAIRED", 6000, 100),
		miner("fresh", "ACTIVE", "PAIRED", 5000, 100),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeWholeOrg}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0
	req.PostEventCooldownSec = 600
	req.ForceIncludeAllPairedMiners = true
	req.CanUseAdminControls = true

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, 0, store.cooldownCalls, "all-paired policies intentionally bypass post-event cooldown")
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, 2, plan.PolicyTargetCount)
	require.Len(t, store.lastInsertTargets, 2)
	assert.Equal(t, "recent", store.lastInsertTargets[0].DeviceIdentifier)

	var snapshot struct {
		PostEventCooldownSec        int32 `json:"post_event_cooldown_sec"`
		ForceIncludeAllPairedMiners bool  `json:"force_include_all_paired_miners"`
	}
	require.NoError(t, json.Unmarshal(store.lastInsertEvent.DecisionSnapshotJSON, &snapshot))
	assert.Equal(t, int32(600), snapshot.PostEventCooldownSec)
	assert.True(t, snapshot.ForceIncludeAllPairedMiners)
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

// The empty-eligible case persists an active closed-loop watcher so newly
// eligible miners can be admitted while the signal remains asserted.
func TestService_Start_FullFleet_NoEligibleMinersPersistsActiveWatcher(t *testing.T) {
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
	req.RestoreBatchSize = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err, "empty full_fleet is valid, not an insufficient-load rejection")
	assert.Empty(t, plan.Selected)
	assert.Equal(t, models.ModeFullFleet, store.lastInsertEvent.Mode)
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Equal(t, models.EventStateActive, store.lastInsertEvent.State,
		"nothing currently eligible still needs an active enforcement window")
	assert.NotNil(t, store.lastInsertEvent.StartedAt, "active watcher records when enforcement began")
	assert.Empty(t, store.lastInsertTargets, "an empty watcher starts with no targets")
	require.NotNil(t, store.lastInsertEvent.CurtailBatchSize)
	assert.Equal(t, defaultManualCurtailBatchSizeFloor, *store.lastInsertEvent.CurtailBatchSize,
		"empty immediate watchers must keep a positive curtail admission throttle")
	assert.Equal(t, int32(0), store.lastInsertEvent.EffectiveBatchSize)
}

func TestService_Preview_FullFleet_AllSkippedReturnsTargetReachedWithSkips(t *testing.T) {
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
	require.Nil(t, plan.InsufficientLoadDetail)
	assert.Equal(t, modes.OutcomeTargetReached, plan.Outcome)
	assert.Empty(t, plan.Selected)
	assert.Len(t, plan.Skipped, 3)
	assert.Zero(t, store.insertEventCalls, "Preview must not persist")
}

func TestService_Start_FullFleet_AllSkippedPersistsActiveWatcher(t *testing.T) {
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
	require.NoError(t, err)
	require.Nil(t, plan.InsufficientLoadDetail)
	assert.Equal(t, modes.OutcomeTargetReached, plan.Outcome)
	assert.NotNil(t, plan.EventUUID, "closed-loop full_fleet persists a watcher even when no miner is actionable yet")
	assert.Equal(t, 1, store.insertEventCalls)
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Equal(t, models.EventStateActive, store.lastInsertEvent.State)
	assert.Empty(t, store.lastInsertTargets)
}

func TestService_Start_FullFleet_DeviceListNoTargetsPersistsCompleted(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("offline-device", "OFFLINE", "PAIRED", 0, 0),
	}
	svc := NewService(store)
	req := validStartRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeDeviceList, DeviceIdentifiers: []string{"offline-device"}}
	req.Mode = models.ModeFullFleet
	req.TargetKW = 0

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	assert.Empty(t, plan.Selected)
	assert.Equal(t, models.LoopTypeOpen, store.lastInsertEvent.LoopType)
	assert.Equal(t, models.EventStateCompleted, store.lastInsertEvent.State)
	assert.NotNil(t, store.lastInsertEvent.EndedAt)
	assert.Empty(t, store.lastInsertTargets)
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
	assert.Equal(t, models.LoopTypeClosed, store.lastInsertEvent.LoopType)
	assert.Equal(t, models.EventStateActive, store.lastInsertEvent.State)
	assert.Empty(t, store.lastInsertTargets)
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
