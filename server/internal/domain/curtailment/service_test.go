package curtailment

import (
	"cmp"
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// fakeStore implements CurtailmentStore for Preview / Start tests; methods
// neither flow exercises panic so a stray call fails loudly instead of
// zero-valuing.
type fakeStore struct {
	orgConfigByOrg       map[int64]*models.OrgConfig
	activeDevicesByOrg   map[int64][]string
	cooldownDevicesByOrg map[int64][]string
	candidatesByOrg      map[int64][]*models.Candidate

	// Captures for assertions.
	listCandidatesCalls      int
	lastListCandidatesOrgID  int64
	lastListCandidatesFilter []string
	cooldownCalls            int
	lastCooldownOrgID        int64
	lastCooldownSec          int32
	activeDevicesCalls       int
	lastActiveDevicesOrgID   int64

	// InsertEventWithTargets state. nextEventID is the synthetic id sequence
	// returned to the service so plan.EventUUID is populated; Start tests
	// inspect lastInsertEvent / lastInsertTargets to pin the persisted shape.
	insertEventErr    error
	insertEventCalls  int
	lastInsertEvent   models.InsertEventParams
	lastInsertTargets []models.InsertTargetParams
	nextEventID       int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		orgConfigByOrg:       map[int64]*models.OrgConfig{},
		activeDevicesByOrg:   map[int64][]string{},
		cooldownDevicesByOrg: map[int64][]string{},
		candidatesByOrg:      map[int64][]*models.Candidate{},
		nextEventID:          1,
	}
}

func (f *fakeStore) GetOrgConfig(_ context.Context, orgID int64) (*models.OrgConfig, error) {
	if cfg, ok := f.orgConfigByOrg[orgID]; ok {
		return cfg, nil
	}
	return nil, fleeterror.NewNotFoundErrorf("no org config for %d", orgID)
}

func (f *fakeStore) ListActiveCurtailedDevices(_ context.Context, orgID int64) ([]string, error) {
	f.activeDevicesCalls++
	f.lastActiveDevicesOrgID = orgID
	return append([]string(nil), f.activeDevicesByOrg[orgID]...), nil
}

func (f *fakeStore) ListRecentlyResolvedCurtailedDevices(_ context.Context, orgID int64, cooldownSec int32) ([]string, error) {
	f.cooldownCalls++
	f.lastCooldownOrgID = orgID
	f.lastCooldownSec = cooldownSec
	return append([]string(nil), f.cooldownDevicesByOrg[orgID]...), nil
}

func (f *fakeStore) ListCandidates(_ context.Context, orgID int64, deviceIdentifiers []string) ([]*models.Candidate, error) {
	f.listCandidatesCalls++
	f.lastListCandidatesOrgID = orgID
	f.lastListCandidatesFilter = append([]string(nil), deviceIdentifiers...)
	cands := f.candidatesByOrg[orgID]
	if len(deviceIdentifiers) == 0 {
		return cands, nil
	}
	want := map[string]struct{}{}
	for _, id := range deviceIdentifiers {
		want[id] = struct{}{}
	}
	out := make([]*models.Candidate, 0, len(cands))
	for _, c := range cands {
		if _, ok := want[c.DeviceIdentifier]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// --- panic stubs for methods the service does not exercise ---

func (f *fakeStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised by Preview tests")
}

func (f *fakeStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	panic("ListTargetsByEvent not exercised by Preview tests")
}

func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by Preview tests")
}

func (f *fakeStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by Preview tests")
}

func (f *fakeStore) UpdateEventState(context.Context, int64, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by Preview tests")
}

func (f *fakeStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by Preview tests")
}

func (f *fakeStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by Preview tests")
}

// InsertEventWithTargets is exercised by Service.Start tests in
// service_start_test.go; Preview never invokes it. Default behavior records
// the call so Start tests can assert against captured params; Preview tests
// don't reach this path.
func (f *fakeStore) InsertEventWithTargets(
	_ context.Context,
	event models.InsertEventParams,
	targets []models.InsertTargetParams,
) (*models.InsertEventResult, error) {
	f.insertEventCalls++
	f.lastInsertEvent = event
	f.lastInsertTargets = append([]models.InsertTargetParams(nil), targets...)
	if f.insertEventErr != nil {
		return nil, f.insertEventErr
	}
	id := f.nextEventID
	f.nextEventID++
	return &models.InsertEventResult{
		ID:        id,
		EventUUID: event.EventUUID,
	}, nil
}

// --- helpers ---

func miner(id string, status, pairing string, powerW, hashRateHS float64) *models.Candidate {
	t := time.Now()
	pw := powerW
	hr := hashRateHS
	driver := "antminer"
	return &models.Candidate{
		DeviceIdentifier: id,
		DriverName:       &driver,
		DeviceStatus:     status,
		PairingStatus:    pairing,
		LatestMetricsAt:  &t,
		LatestPowerW:     &pw,
		LatestHashRateHS: &hr,
	}
}

func minerWithEff(id string, powerW, hashRateHS, effJH float64) *models.Candidate {
	c := miner(id, "ACTIVE", "PAIRED", powerW, hashRateHS)
	c.AvgEfficiencyJH = &effJH
	return c
}

func staleMiner(id string) *models.Candidate {
	// LatestMetricsAt nil → service treats as stale_telemetry.
	driver := "antminer"
	return &models.Candidate{
		DeviceIdentifier: id,
		DriverName:       &driver,
		DeviceStatus:     "ACTIVE",
		PairingStatus:    "PAIRED",
	}
}

func defaultOrgConfig(orgID int64) *models.OrgConfig {
	return &models.OrgConfig{
		OrgID:                 orgID,
		MaxDurationDefaultSec: 14400,
		CandidateMinPowerW:    1500,
		PostEventCooldownSec:  600,
	}
}

func validRequest(orgID int64) PreviewRequest {
	return PreviewRequest{
		OrgID:    orgID,
		Scope:    Scope{Type: models.ScopeTypeWholeOrg},
		Mode:     models.ModeFixedKw,
		Strategy: models.StrategyLeastEfficientFirst,
		Level:    models.LevelFull,
		Priority: models.PriorityNormal,
		TargetKW: 5,
	}
}

// --- happy-path test ---

func TestService_Preview_HappyPath_FixedKw(t *testing.T) {
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
	plan, err := svc.Preview(t.Context(), validRequest(orgID))

	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, modes.OutcomeTargetReached, plan.Outcome)
	require.Len(t, plan.Selected, 2, "5 kW target picks worst + mid (6 kW)")
	assert.Equal(t, "worst", plan.Selected[0].DeviceIdentifier)
	assert.Equal(t, "mid", plan.Selected[1].DeviceIdentifier)
	assert.InDelta(t, 6.0, plan.EstimatedReductionKW, 0.001)
	assert.InDelta(t, 3.0, plan.EstimatedRemainingPowerKW, 0.001)
}

// --- request validation ---

func TestService_Preview_RejectsUnsupportedMode(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Mode = models.Mode("FIXED_COUNT")
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
}

func TestService_Preview_RejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Strategy = models.Strategy("MOST_POWER_FIRST")
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MOST_POWER_FIRST")
	assert.Contains(t, err.Error(), "LEAST_EFFICIENT_FIRST")
}

func TestService_Preview_RejectsUnsupportedPriority(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Priority = models.PriorityHigh
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HIGH", "rejected priority must appear in error")
}

func TestService_Preview_RejectsUnbalancedMaintenancePair(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.IncludeMaintenance = true
	req.ForceIncludeMaintenance = false
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
}

func TestService_Preview_RejectsZeroOrNegativeTarget(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	for _, target := range []float64{0, -1} {
		req := validRequest(1)
		req.TargetKW = target
		_, err := svc.Preview(t.Context(), req)
		require.Error(t, err, "target=%v should be rejected", target)
	}
}

// --- scope resolution ---

func TestService_Preview_DeviceSetScopeIsUnimplemented(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeDeviceSets, DeviceSetIDs: []string{"set-a"}}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	// device-set scope is reported via UnimplementedError; the handler maps
	// fleeterror codes to Connect codes elsewhere.
	assert.Contains(t, err.Error(), "device-set")
}

func TestService_Preview_DeviceListScopePassesFilterToStore(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("alpha", 3000, 100, 30),
		minerWithEff("beta", 3000, 100, 30),
	}
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{
		Type:              models.ScopeTypeDeviceList,
		DeviceIdentifiers: []string{"alpha"},
	}
	_, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, store.lastListCandidatesFilter,
		"device-list scope must narrow the store query, not load every miner")
}

func TestService_Preview_DeviceListScopeRequiresNonEmptyList(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Scope = Scope{Type: models.ScopeTypeDeviceList}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
}

// TestService_Preview_DeviceListScopeRejectsMixedPayload pins the
// oneof-style scope contract: explicit ScopeTypeDeviceList with a
// populated DeviceSetIDs slice must reject as InvalidArgument rather
// than silently ignore the set IDs and execute a device-list plan.
func TestService_Preview_DeviceListScopeRejectsMixedPayload(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Scope = Scope{
		Type:              models.ScopeTypeDeviceList,
		DeviceIdentifiers: []string{"miner-a"},
		DeviceSetIDs:      []string{"set-x"},
	}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device_set_ids")
}

// TestService_Preview_DeviceSetScopeRejectsMixedPayload mirrors the
// device-list mutual-exclusion guard for the symmetric case: explicit
// ScopeTypeDeviceSets with a populated DeviceIdentifiers slice. The
// scope branch is itself unimplemented, but the mutual-exclusion check
// must fire first so the caller sees the contract violation rather
// than the unimplemented status.
func TestService_Preview_DeviceSetScopeRejectsMixedPayload(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Scope = Scope{
		Type:              models.ScopeTypeDeviceSets,
		DeviceSetIDs:      []string{"set-x"},
		DeviceIdentifiers: []string{"miner-a"},
	}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device_identifiers")
}

// --- pre-selector filters ---

func TestService_Preview_FiltersByPairingDeviceStatusAndStaleness(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("unpaired", "ACTIVE", "UNPAIRED", 3000, 100),
		miner("updating", "UPDATING", "PAIRED", 3000, 100),
		miner("rebooting", "REBOOT_REQUIRED", "PAIRED", 3000, 100),
		miner("offline", "OFFLINE", "PAIRED", 3000, 100),
		miner("inactive", "INACTIVE", "PAIRED", 3000, 100),
		miner("needs-pool", "NEEDS_MINING_POOL", "PAIRED", 3000, 100),
		miner("maintenance", "MAINTENANCE", "PAIRED", 3000, 100),
		staleMiner("stale"),
		minerWithEff("eligible", 3000, 100, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 2.5 // single 3 kW eligible miner reaches this target
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	// Eligible miner is selected.
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "eligible", plan.Selected[0].DeviceIdentifier)

	// Each non-eligible device shows up in Skipped with the right reason.
	reasons := map[string]SkipReason{}
	for _, s := range plan.Skipped {
		reasons[s.DeviceIdentifier] = s.Reason
	}
	assert.Equal(t, SkipPairing, reasons["unpaired"])
	assert.Equal(t, SkipUpdating, reasons["updating"])
	assert.Equal(t, SkipRebootRequired, reasons["rebooting"])
	assert.Equal(t, SkipUnreachableResidualLoad, reasons["offline"])
	assert.Equal(t, SkipNonActionableStatus, reasons["inactive"])
	assert.Equal(t, SkipNonActionableStatus, reasons["needs-pool"])
	assert.Equal(t, SkipMaintenance, reasons["maintenance"])
	assert.Equal(t, SkipStaleTelemetry, reasons["stale"])
}

func TestService_Preview_MaintenancePairAdmitsMiners(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	c := miner("maint", "MAINTENANCE", "PAIRED", 3000, 100)
	eff := 40.0
	c.AvgEfficiencyJH = &eff
	store.candidatesByOrg[orgID] = []*models.Candidate{c}

	svc := NewService(store)
	req := validRequest(orgID)
	req.IncludeMaintenance = true
	req.ForceIncludeMaintenance = true
	req.TargetKW = 1

	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "maint", plan.Selected[0].DeviceIdentifier)
}

// --- cooldown ---

func TestService_Preview_NormalPriority_AppliesCooldown(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.cooldownDevicesByOrg[orgID] = []string{"recent"}
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("recent", 3000, 100, 40),
		minerWithEff("ok", 3000, 100, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 2.5
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, 1, store.cooldownCalls, "NORMAL priority must consult cooldown")
	assert.Equal(t, int32(600), store.lastCooldownSec, "cooldown sec must come from org config")

	// recent miner gets skipped with cooldown reason; ok miner is selected.
	reasons := map[string]SkipReason{}
	for _, s := range plan.Skipped {
		reasons[s.DeviceIdentifier] = s.Reason
	}
	assert.Equal(t, SkipCooldown, reasons["recent"])
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "ok", plan.Selected[0].DeviceIdentifier)
}

func TestService_Preview_EmergencyPriority_BypassesCooldown(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.cooldownDevicesByOrg[orgID] = []string{"recent"} // would skip if cooldown applied
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("recent", 3000, 100, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.Priority = models.PriorityEmergency
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	assert.Zero(t, store.cooldownCalls, "EMERGENCY must skip the cooldown lookup entirely")
	require.Len(t, plan.Selected, 1, "recent miner is admitted under EMERGENCY")
	assert.Equal(t, "recent", plan.Selected[0].DeviceIdentifier)
}

// --- active-event ---

func TestService_Preview_ExcludesDevicesLockedInActiveEvent(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.activeDevicesByOrg[orgID] = []string{"locked"}
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("locked", 3000, 100, 40),
		minerWithEff("free", 3000, 100, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 2.5
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	reasons := map[string]SkipReason{}
	for _, s := range plan.Skipped {
		reasons[s.DeviceIdentifier] = s.Reason
	}
	assert.Equal(t, SkipActiveEvent, reasons["locked"])
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "free", plan.Selected[0].DeviceIdentifier)
}

// --- candidate_min_power_w override ---

func TestService_Preview_OverrideTakesPrecedenceOverOrgDefault(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	cfg := defaultOrgConfig(orgID)
	cfg.CandidateMinPowerW = 1500
	store.orgConfigByOrg[orgID] = cfg
	// 800 W miner is below the org default (1500) but above an override of 500.
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("low", 800, 100, 40),
	}

	svc := NewService(store)

	// Without override → skipped because below candidate_min_power_w.
	planNoOverride, err := svc.Preview(t.Context(), validRequest(orgID))
	require.NoError(t, err)
	require.Len(t, planNoOverride.Skipped, 1)

	// With override of 500 → admitted.
	override := int32(500)
	req := validRequest(orgID)
	req.CandidateMinPowerWOverride = &override
	req.TargetKW = 0.5
	planWithOverride, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, planWithOverride.Selected, 1)
	assert.Equal(t, "low", planWithOverride.Selected[0].DeviceIdentifier)
}

// --- cross-tenant isolation ---

// TestService_Preview_PassesCallerOrgIDToEveryStoreCall pins the cross-tenant
// isolation invariant: a Preview from org A must scope every store call to
// org A. A regression that drops the org_id (e.g., a refactor that hard-codes
// orgID=1 in one query) would let org A see org B's devices — this test
// populates both orgs and asserts the caller's org_id is the one that reaches
// each store method.
func TestService_Preview_PassesCallerOrgIDToEveryStoreCall(t *testing.T) {
	t.Parallel()

	const callerOrg = int64(101)
	const otherOrg = int64(202)

	store := newFakeStore()
	store.orgConfigByOrg[callerOrg] = defaultOrgConfig(callerOrg)
	store.orgConfigByOrg[otherOrg] = defaultOrgConfig(otherOrg)
	store.activeDevicesByOrg[callerOrg] = []string{"caller-locked"}
	store.activeDevicesByOrg[otherOrg] = []string{"other-locked"}
	store.cooldownDevicesByOrg[callerOrg] = []string{"caller-cooldown"}
	store.cooldownDevicesByOrg[otherOrg] = []string{"other-cooldown"}
	store.candidatesByOrg[callerOrg] = []*models.Candidate{minerWithEff("caller-miner", 3000, 100, 40)}
	store.candidatesByOrg[otherOrg] = []*models.Candidate{minerWithEff("other-miner", 3000, 100, 40)}

	svc := NewService(store)
	plan, err := svc.Preview(t.Context(), validRequest(callerOrg))
	require.NoError(t, err)

	assert.Equal(t, callerOrg, store.lastActiveDevicesOrgID, "active-event lookup must use caller's org_id")
	assert.Equal(t, callerOrg, store.lastCooldownOrgID, "cooldown lookup must use caller's org_id")
	assert.Equal(t, callerOrg, store.lastListCandidatesOrgID, "candidate listing must use caller's org_id")

	// Plan must contain only the caller's devices — no leakage from otherOrg.
	for _, s := range plan.Selected {
		assert.NotContains(t, s.DeviceIdentifier, "other-")
	}
	for _, s := range plan.Skipped {
		assert.NotContains(t, s.DeviceIdentifier, "other-")
	}
}

// --- insufficient-load detail propagation ---

func TestService_Preview_InsufficientLoad_DetailCarriesExclusionCounts(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		// Only 3 kW eligible, target 100 kW → insufficient.
		minerWithEff("a", 1500, 100, 40),
		minerWithEff("b", 1500, 100, 40),
		// Plus skipped reasons that should appear in the summary so the
		// operator sees the real cause when InsufficientLoad fires.
		miner("offline-1", "OFFLINE", "PAIRED", 0, 0),
		miner("offline-2", "OFFLINE", "PAIRED", 0, 0),
		// Transient-status branches: UPDATING / REBOOT_REQUIRED / stale.
		// Each must increment its dedicated counter — without that, a
		// fleet-wide firmware rollout reports zero exclusions on Insufficient.
		miner("updating-1", "UPDATING", "PAIRED", 0, 0),
		miner("reboot-1", "REBOOT_REQUIRED", "PAIRED", 0, 0),
		miner("empty-status", "", "PAIRED", 0, 0),
		staleMiner("stale-metrics"),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 100

	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.InsufficientLoadDetail)

	assert.InDelta(t, 3.0, plan.InsufficientLoadDetail.AvailableKW, 0.001)
	assert.Equal(t, 100.0, plan.InsufficientLoadDetail.RequestedKW)
	assert.Equal(t, int32(2), plan.InsufficientLoadDetail.ExcludedOffline)
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedUpdating)
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedRebootRequired)
	// Both stale paths (empty device_status sentinel and nil
	// LatestMetricsAt) funnel into one ExcludedStale counter.
	assert.Equal(t, int32(2), plan.InsufficientLoadDetail.ExcludedStale)
	assert.Equal(t, int32(1500), plan.InsufficientLoadDetail.CandidateMinPowerW)

	// Plan-level fields must reflect the empty-Selected reality on the
	// rejection branch: zero reduction, full eligible pool still on the
	// floor. Pins the Result.RealizedReductionW=0 contract through
	// BuildPlan so a future caller that consumes Plan without
	// short-circuiting on InsufficientLoadDetail sees consistent numbers.
	assert.Empty(t, plan.Selected)
	assert.Equal(t, 0.0, plan.EstimatedReductionKW)
	assert.InDelta(t, 3.0, plan.EstimatedRemainingPowerKW, 0.001)
}

// TestService_Preview_EmptyDeviceStatusSkipsAsStale pins the COALESCE sentinel
// behavior: a candidate row whose device_status is empty (no device_status
// row joined; the sqlc query COALESCEs to empty string) must be skipped with
// SkipStaleTelemetry rather than silently flowing into the eligible set.
// Without the case "": arm in service.classifyCandidates, an unstatused
// device would fall through to the dual-signal filter and could be picked
// for curtailment — which is exactly the safety boundary the COALESCE was
// added to defend.
func TestService_Preview_EmptyDeviceStatusSkipsAsStale(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	// Power and hash both well above the dual-signal floor — the only thing
	// that should keep this miner out of the eligible set is the empty
	// device_status sentinel.
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("unstatused", "", "PAIRED", 5000, 1e12),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)

	assert.Empty(t, plan.Selected, "empty device_status must not become eligible")
	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, "unstatused", plan.Skipped[0].DeviceIdentifier)
	assert.Equal(t, SkipStaleTelemetry, plan.Skipped[0].Reason)
}

// TestService_Preview_NonFiniteTelemetrySkipsAsStale pins the safety guard
// against NaN / +Inf / -Inf telemetry samples. device_metrics.power_w and
// hash_rate_hs are DOUBLE PRECISION columns, which support IEEE 754 non-
// finite values. Without the finite-number gate in classifyCandidates,
// NaN comparisons return false in the dual-signal filter (admitting the
// miner) and FixedKw's running sum becomes NaN; +Inf satisfies any target.
// Both produce nonsense plans. The fix routes them to SkipStaleTelemetry
// — same operational signal as a missing sample.
func TestService_Preview_NonFiniteTelemetrySkipsAsStale(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)

	withPower := func(id string, power, hash float64) *models.Candidate {
		c := miner(id, "ACTIVE", "PAIRED", power, hash)
		// miner() already populated PowerW/HashRateHS from the args, but
		// build helpers like miner() can't carry NaN/Inf cleanly because
		// they take float64 values. Overwrite explicitly.
		p, h := power, hash
		c.LatestPowerW = &p
		c.LatestHashRateHS = &h
		eff := 40.0
		c.AvgEfficiencyJH = &eff
		return c
	}

	store.candidatesByOrg[orgID] = []*models.Candidate{
		withPower("nan-power", math.NaN(), 1e12),
		withPower("posinf-power", math.Inf(+1), 1e12),
		withPower("neginf-power", math.Inf(-1), 1e12),
		withPower("nan-hash", 5000, math.NaN()),
		withPower("posinf-hash", 5000, math.Inf(+1)),
		// One legitimate miner so the request can succeed; pins that
		// the gate is precise (only non-finite candidates dropped).
		minerWithEff("ok", 5000, 1e12, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)

	// Only the legitimate miner is selected; the running sum is finite.
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "ok", plan.Selected[0].DeviceIdentifier)
	assert.False(t, math.IsNaN(plan.EstimatedReductionKW),
		"running sum must not be poisoned by NaN telemetry")
	assert.False(t, math.IsInf(plan.EstimatedReductionKW, 0),
		"running sum must not be poisoned by Inf telemetry")
	assert.InDelta(t, 5.0, plan.EstimatedReductionKW, 0.001)

	// All five non-finite candidates show up in Skipped with SkipStaleTelemetry.
	skippedReasons := map[string]SkipReason{}
	for _, s := range plan.Skipped {
		skippedReasons[s.DeviceIdentifier] = s.Reason
	}
	for _, id := range []string{"nan-power", "posinf-power", "neginf-power", "nan-hash", "posinf-hash"} {
		assert.Equal(t, SkipStaleTelemetry, skippedReasons[id],
			"non-finite telemetry must skip as stale; %s did not", id)
	}
}

// TestService_Preview_NonFiniteAvgEfficiencyTreatedAsUnknown pins the
// extension of the finite-number guard to AvgEfficiencyJH. NaN/Inf
// efficiency would violate sort.SliceStable's transitivity contract in
// BuildPlan (NaN comparisons return false), producing undefined ranking
// across ties. Treating non-finite as unknown (nil) routes the device
// through the existing rank-last path and preserves a deterministic plan.
func TestService_Preview_NonFiniteAvgEfficiencyTreatedAsUnknown(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)

	// Helper builds a candidate whose telemetry is fully valid (finite
	// power + hash) but whose avg_efficiency is non-finite.
	withEfficiency := func(id string, eff float64) *models.Candidate {
		c := miner(id, "ACTIVE", "PAIRED", 3000, 1e12)
		e := eff
		c.AvgEfficiencyJH = &e
		return c
	}

	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("known-worst", 3000, 1e12, 50),
		withEfficiency("nan-eff", math.NaN()),
		withEfficiency("posinf-eff", math.Inf(+1)),
		withEfficiency("neginf-eff", math.Inf(-1)),
		minerWithEff("known-best", 3000, 1e12, 20),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	// 9 kW target picks exactly 3 of 5 miners (3 kW each). Worst-J/H first,
	// unknowns last → known-worst, then known-best, then one non-finite one.
	req.TargetKW = 9
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)

	// All five miners are eligible — non-finite avg_efficiency must NOT
	// drop them from the candidate set (unlike non-finite power/hash, which
	// skip as stale). The remaining-unselected unknowns stay in the pool;
	// they're not "skipped".
	assert.Empty(t, plan.Skipped,
		"non-finite avg_efficiency must rank-last, not skip-filter")

	// Running sum stays finite.
	assert.False(t, math.IsNaN(plan.EstimatedReductionKW),
		"running sum must not be poisoned by NaN avg_efficiency")
	assert.False(t, math.IsInf(plan.EstimatedReductionKW, 0),
		"running sum must not be poisoned by Inf avg_efficiency")

	// Exactly 3 selected at 9 kW target with 3 kW miners.
	require.Len(t, plan.Selected, 3)
	// Known-efficiency miners rank ahead of non-finite ones.
	assert.Equal(t, "known-worst", plan.Selected[0].DeviceIdentifier,
		"worst known efficiency must rank first")
	assert.Equal(t, "known-best", plan.Selected[1].DeviceIdentifier,
		"second known efficiency must rank ahead of unknowns")
}

// TestService_Preview_DeviceListScopeRejectsCrossOrgIdentifiers pins the
// org-ownership boundary: explicit miner-list scope must validate org
// ownership before persistence or dispatch. The SQL already filters by
// org_id, so a cross-org device_identifier would silently drop and the
// caller would see a confusing InsufficientLoad. The service layer guard
// converts that silent drop into an explicit NotFound listing the
// unrecognized identifiers.
func TestService_Preview_DeviceListScopeRejectsCrossOrgIdentifiers(t *testing.T) {
	t.Parallel()

	const callerOrg = int64(101)
	const otherOrg = int64(202)

	build := func() *fakeStore {
		store := newFakeStore()
		store.orgConfigByOrg[callerOrg] = defaultOrgConfig(callerOrg)
		store.orgConfigByOrg[otherOrg] = defaultOrgConfig(otherOrg)
		store.candidatesByOrg[callerOrg] = []*models.Candidate{
			minerWithEff("caller-only", 3000, 100, 40),
		}
		store.candidatesByOrg[otherOrg] = []*models.Candidate{
			minerWithEff("other-only", 3000, 100, 40),
		}
		return store
	}

	t.Run("only cross-org id rejects with NotFound", func(t *testing.T) {
		t.Parallel()
		svc := NewService(build())
		req := validRequest(callerOrg)
		req.Scope = Scope{
			Type:              models.ScopeTypeDeviceList,
			DeviceIdentifiers: []string{"other-only"},
		}
		plan, err := svc.Preview(t.Context(), req)
		require.Error(t, err)
		assert.Nil(t, plan)
		assert.True(t, fleeterror.IsNotFoundError(err),
			"cross-org device_identifier must surface as NotFound, got %v", err)
		assert.Contains(t, err.Error(), "other-only",
			"error must name the unrecognized identifier")
	})

	t.Run("mixed valid + cross-org rejects with NotFound naming the missing id", func(t *testing.T) {
		t.Parallel()
		svc := NewService(build())
		req := validRequest(callerOrg)
		req.Scope = Scope{
			Type:              models.ScopeTypeDeviceList,
			DeviceIdentifiers: []string{"caller-only", "other-only"},
		}
		plan, err := svc.Preview(t.Context(), req)
		require.Error(t, err)
		assert.Nil(t, plan)
		assert.True(t, fleeterror.IsNotFoundError(err),
			"any cross-org id must surface as NotFound, got %v", err)
		assert.Contains(t, err.Error(), "other-only",
			"error must name the missing identifier even when other ids are valid")
		assert.NotContains(t, err.Error(), "caller-only",
			"error must not falsely list valid identifiers as missing")
	})
}

// TestService_Preview_MissingDriverSkipsAsCurtailFullUnsupported pins the
// partial capability gate: a candidate row with no driver_name (NULL on the
// LEFT JOIN to discovered_device, so the model field is *string == nil) is
// skipped with SkipCurtailFullUnsupported rather than admitted into the
// eligible set. The full capability check (does the loaded plugin advertise
// curtail_full for this model?) is follow-up work; this guard catches the
// "no plugin metadata at all" edge today and prevents the selector from
// picking a device whose Curtail dispatch would have nowhere to land.
func TestService_Preview_MissingDriverSkipsAsCurtailFullUnsupported(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)

	// Construct a candidate that would otherwise be eligible — same shape as
	// happy-path but with DriverName left nil (LEFT JOIN missed).
	t1 := time.Now()
	pw := 3000.0
	hr := 1e12
	store.candidatesByOrg[orgID] = []*models.Candidate{
		{
			DeviceIdentifier: "no-driver-meta",
			DriverName:       nil, // missing discovered_device row
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &t1,
			LatestPowerW:     &pw,
			LatestHashRateHS: &hr,
		},
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)

	assert.Empty(t, plan.Selected, "a device with no driver metadata must not become eligible")
	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, "no-driver-meta", plan.Skipped[0].DeviceIdentifier)
	assert.Equal(t, SkipCurtailFullUnsupported, plan.Skipped[0].Reason)
}

// TestService_Preview_DualSignalCountersPropagateIntoInsufficientLoadDetail
// pins the BuildPlan post-Select merge: dual-signal exclusions classified
// inside selector.BuildPlan (below-threshold, dead-monitor, phantom-load)
// must reach InsufficientLoadDetail so the operator sees per-reason counts,
// not zeros. Without the merge block in selector.go, the rejection detail
// would carry only the pre-selector summary's status/pairing/cooldown counts.
func TestService_Preview_DualSignalCountersPropagateIntoInsufficientLoadDetail(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID) // CandidateMinPowerW = 1500
	store.candidatesByOrg[orgID] = []*models.Candidate{
		// below-threshold AND no hash — SkipBelowThreshold.
		miner("below-threshold-no-hash", "ACTIVE", "PAIRED", 100, 0),
		// below-threshold WITH hash — SkipPowerTelemetryUnreliable (dead monitor).
		miner("dead-monitor", "ACTIVE", "PAIRED", 100, 1e12),
		// above-threshold WITH no hash — SkipPhantomLoadNoHash.
		miner("phantom-load", "ACTIVE", "PAIRED", 5000, 0),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 100 // far above any reachable selection — forces InsufficientLoad
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)

	require.Equal(t, modes.OutcomeInsufficientLoad, plan.Outcome)
	require.NotNil(t, plan.InsufficientLoadDetail)

	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedBelowThreshold,
		"below-threshold-no-hash candidate must increment ExcludedBelowThreshold")
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedDeadMonitor,
		"below-threshold-with-hash candidate must increment ExcludedDeadMonitor")
	assert.Equal(t, int32(1), plan.InsufficientLoadDetail.ExcludedPhantomLoad,
		"above-threshold-no-hash candidate must increment ExcludedPhantomLoad")
}

// TestService_Preview_RejectsToleranceGreaterThanOrEqualTarget pins the
// invariant that a tolerance >= target_kw is rejected at validation. Without
// this guard, a request with tolerance >= target accepts an empty selection
// as OutcomeUndershootTolerated — a no-op preview that looks like a
// successful plan to UIs and automations.
func TestService_Preview_RejectsToleranceGreaterThanOrEqualTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		target    float64
		tolerance float64
		wantErr   bool
	}{
		{name: "tolerance equal to target rejected", target: 5, tolerance: 5, wantErr: true},
		{name: "tolerance greater than target rejected", target: 5, tolerance: 6, wantErr: true},
		{name: "tolerance just below target accepted", target: 5, tolerance: 4.999, wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const orgID = int64(1)
			store := newFakeStore()
			store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
			store.candidatesByOrg[orgID] = []*models.Candidate{
				minerWithEff("ok", 6000, 100, 40),
			}
			svc := NewService(store)
			req := validRequest(orgID)
			req.TargetKW = tc.target
			req.ToleranceKW = tc.tolerance
			_, err := svc.Preview(t.Context(), req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "tolerance_kw must be < target_kw")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestService_Preview_DeterministicOrderingOnTiedEfficiencies pins the
// SQL-side ORDER BY contract. Two candidates with identical efficiency,
// telemetry, and pairing must select in a stable, reproducible order
// across repeated Preview calls — otherwise the same request can produce
// different plans, which makes execution and audit incoherent. The
// fakeStore returns insertion order; the real SQL store sorts by
// device_identifier, so we assert the lexicographic order the contract
// promises.
func TestService_Preview_DeterministicOrderingOnTiedEfficiencies(t *testing.T) {
	t.Parallel()

	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	// Insert in non-lexicographic order so the test fails if the selector
	// silently relies on insertion order rather than the documented
	// SQL ORDER BY contract enforced upstream.
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("zebra", 3000, 100, 40),
		minerWithEff("alpha", 3000, 100, 40),
		minerWithEff("mango", 3000, 100, 40),
	}

	// Pre-sort the store output to mirror what ListCurtailmentCandidatesByOrg
	// will produce after the ORDER BY clause. The fakeStore preserves
	// insertion order; this asserts the contract the real store guarantees.
	slices.SortFunc(store.candidatesByOrg[orgID], func(a, b *models.Candidate) int {
		return cmp.Compare(a.DeviceIdentifier, b.DeviceIdentifier)
	})

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 2.5

	// Run 5 times: ordering must be byte-stable across calls.
	for i := range 5 {
		plan, err := svc.Preview(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, plan.Selected, 1)
		assert.Equal(t, "alpha", plan.Selected[0].DeviceIdentifier,
			"tied efficiencies must select lexicographically smallest device_identifier (run %d)", i)
	}
}
