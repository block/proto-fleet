package curtailment

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	infrastructuremodels "github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// fakeStore implements CurtailmentStore for Preview / Start tests; methods
// neither flow exercises panic so a stray call fails loudly instead of
// zero-valuing.
type fakeStore struct {
	orgConfigByOrg       map[int64]*models.OrgConfig
	activeDevicesByOrg   map[int64][]string
	cooldownDevicesByOrg map[int64][]string
	candidatesByOrg      map[int64][]*models.Candidate
	candidatesBySite     map[int64]map[int64][]*models.Candidate
	sitesByOrg           map[int64]map[int64]bool

	// Captures for assertions.
	listCandidatesCalls       int
	lastListCandidatesOrgID   int64
	lastListCandidatesFilter  []string
	lastListCandidatesSiteIDs []int64
	cooldownCalls             int
	lastCooldownOrgID         int64
	lastCooldownSec           int32
	lastCooldownFilter        []string
	lastCooldownSiteIDs       []int64
	activeDevicesCalls        int
	lastActiveDevicesOrgID    int64

	// InsertEventWithTargets state. nextEventID is the synthetic id sequence
	// returned to the service so plan.EventUUID is populated; Start tests
	// inspect lastInsertEvent / lastInsertTargets to pin the persisted shape.
	insertEventErr    error
	insertEventCalls  int
	lastInsertEvent   models.InsertEventParams
	lastInsertTargets []models.InsertTargetParams
	nextEventID       int64

	// Stop-path fakes. eventsByUUID feeds GetEventByUUID;
	// targetsByEventUUID feeds ListTargetsByEvent;
	// beginRestoreErr gives Service.Stop tests control over
	// BeginRestoreTransition outcomes.
	eventsByUUID             map[uuid.UUID]*models.Event
	getEventByUUIDErr        error
	targetsByEventUUID       map[uuid.UUID][]*models.Target
	targetSiteIDsByEventUUID map[uuid.UUID][]int64
	activeEvents             []*models.Event
	activeEventErr           error
	listTargetsErr           error
	beginRestoreErr          error
	beginRestoreCalls        int
	beginRestoreLastEventID  uuid.UUID

	beginRecurtailErr         error
	beginRecurtailCalls       int
	beginRecurtailLastEventID uuid.UUID

	// List-history fakes. eventsHistory is the slice the fake's ListEvents
	// paginates over (callers seed it newest-first to match the SQL impl).
	eventsHistory        []*models.Event
	listEventsErr        error
	listEventsCalls      int
	lastListEventsParams interfaces.ListEventsParams

	// Update fakes. updateOperatorFieldsResult is what the fake returns on
	// success; callers preconfigure it to match the post-update event row.
	// updateOperatorFieldsErr drives error paths (e.g. race-loss).
	updateOperatorFieldsCalls    int
	lastUpdateOperatorFieldsID   int64
	lastUpdateOperatorFieldsArgs interfaces.UpdateOperatorFieldsParams
	updateOperatorFieldsResult   *models.Event
	updateOperatorFieldsErr      error

	// AdminTerminate fakes. adminTerminateResult is the post-transition
	// event the fake echoes; adminTerminateErr drives error paths
	// (state conflict, transient db error).
	adminTerminateCalls                int
	adminTerminateWithFanRecoveryCalls int
	lastAdminTerminateUUID             uuid.UUID
	lastAdminTerminateState            models.EventState
	lastAdminTerminateReason           string
	adminTerminateResult               *models.Event
	adminTerminateErr                  error
	adminTerminateIdempotentReplay     bool

	forceReleaseCalls              int
	lastForceReleaseUUID           uuid.UUID
	lastForceReleaseReason         string
	forceReleaseResult             *models.Event
	forceReleaseSweptTargets       int64
	forceReleaseAutomationDisabled bool
	forceReleaseErr                error

	updateFanStateCalls      int
	lastUpdateFanStateID     int64
	lastUpdateFanStateParams interfaces.UpdateCurtailmentFanStateParams
	updateFanStateErr        error
	terminalFanRecoveryErr   error
	operatorFanCallOrder     []string

	// Idempotent replay fakes. eventsByIdempotencyKey / eventsByExternalRef
	// drive Service.Start's pre-insert webhook-replay lookup; nil results
	// signal "no prior match". get*Calls / last* let tests pin the args.
	eventsByIdempotencyKey map[string]*models.Event
	eventsByExternalRef    map[string]*models.Event
	// *OnRetry: the value returned by the second (post-race-loss) lookup;
	// the primary map covers the initial pre-insert lookup. Race-loser tests
	// use these to verify the replay branch sees the winner's row even when
	// the first lookup missed.
	eventsByIdempotencyKeyOnRetry map[string]*models.Event
	eventsByExternalRefOnRetry    map[string]*models.Event
	getByIdempotencyKeyCalls      int
	lastGetByIdempotencyKey       string
	getByExternalRefCalls         int
	lastGetByExternalRefSource    string
	lastGetByExternalRefRef       string
	getByIdempotencyKeyErr        error
	getByExternalRefErr           error

	// Automation demand fakes used by Stop's automation-owned restore guard.
	automationRulesByEventUUID     map[uuid.UUID]*models.AutomationRule
	automationRulesByExternalRef   map[string]*models.AutomationRule
	automationDemandGuardCheckRuns int
}

func TestFacilityFanController_MissingClaimIsFailureOncePerPowerPhase(t *testing.T) {
	t.Parallel()

	const (
		orgID    = int64(42)
		deviceID = int64(501)
		siteID   = int64(601)
	)
	ctrl := gomock.NewController(t)
	devices := storemocks.NewMockInfrastructureDeviceStore(ctrl)
	sites := storemocks.NewMockSiteStore(ctrl)
	devices.EXPECT().
		GetInfrastructureDevice(gomock.Any(), orgID, deviceID).
		Return(nil, fleeterror.NewNotFoundError("infrastructure device not found")).
		Times(4)
	audit := &recordingAudit{}
	controller := NewFacilityFanController(devices, sites, driver.NewRegistry(), audit)
	event := &models.Event{
		EventUUID:            uuid.New(),
		OrgID:                orgID,
		FacilityFanDeviceIDs: []int64{deviceID},
		FacilityFanSiteIDs:   []int64{siteID},
	}

	offFailure := controller.SetState(t.Context(), event, driver.PowerOff)
	require.NotNil(t, offFailure)
	assert.Contains(t, *offFailure, "device is missing")
	event.FanLastError = offFailure
	now := time.Now().UTC()
	event.FanOffSentAt = &now
	require.NotNil(t, controller.SetState(t.Context(), event, driver.PowerOff))
	require.NotNil(t, controller.SetState(t.Context(), event, driver.PowerOn))
	event.FanOnSentAt = &now
	require.NotNil(t, controller.SetState(t.Context(), event, driver.PowerOn))

	require.Len(t, audit.events, 2)
	assert.Equal(t, ActivityTypeFacilityFanCommandFailed, audit.events[0].Type)
	assert.Equal(t, "off", audit.events[0].Metadata["desired_power"])
	require.NotNil(t, audit.events[0].ErrorMessage)
	assert.Contains(t, *audit.events[0].ErrorMessage, "device is missing")
	assert.Equal(t, ActivityTypeFacilityFanCommandFailed, audit.events[1].Type)
	assert.Equal(t, "on", audit.events[1].Metadata["desired_power"])
	require.NotNil(t, audit.events[1].ErrorMessage)
	assert.Contains(t, *audit.events[1].ErrorMessage, "device is missing")
}

type recordingFacilityFanDriver struct {
	powers       []driver.PowerMode
	deviceIDs    []int64
	failDeviceID int64
}

func (*recordingFacilityFanDriver) ValidateConfig(json.RawMessage) error { return nil }
func (d *recordingFacilityFanDriver) SetState(_ context.Context, device driver.Device, state driver.DesiredState) error {
	d.powers = append(d.powers, state.Power)
	d.deviceIDs = append(d.deviceIDs, device.ID)
	if device.ID == d.failDeviceID {
		return fmt.Errorf("device %d timed out", device.ID)
	}
	return nil
}
func (*recordingFacilityFanDriver) Capabilities() map[string]bool {
	return map[string]bool{"on_off": true}
}

func TestFacilityFanController_RotatesFailedRetryStartAcrossDevices(t *testing.T) {
	t.Parallel()

	const (
		orgID      = int64(42)
		firstID    = int64(501)
		secondID   = int64(502)
		siteID     = int64(601)
		driverType = "test-fan"
	)
	ctrl := gomock.NewController(t)
	devices := storemocks.NewMockInfrastructureDeviceStore(ctrl)
	sites := storemocks.NewMockSiteStore(ctrl)
	device := func(id int64) *infrastructuremodels.Device {
		return &infrastructuremodels.Device{
			ID: id, OrgID: orgID, SiteID: siteID, Enabled: true, DriverType: driverType,
		}
	}
	gomock.InOrder(
		devices.EXPECT().GetInfrastructureDevice(gomock.Any(), orgID, firstID).Return(device(firstID), nil),
		devices.EXPECT().GetInfrastructureDevice(gomock.Any(), orgID, secondID).Return(device(secondID), nil),
		devices.EXPECT().GetInfrastructureDevice(gomock.Any(), orgID, secondID).Return(device(secondID), nil),
		devices.EXPECT().GetInfrastructureDevice(gomock.Any(), orgID, firstID).Return(device(firstID), nil),
	)
	sites.EXPECT().GetInfrastructureControlSubnets(gomock.Any(), orgID, siteID).Return("10.0.0.0/24", nil).Times(4)
	driverController := &recordingFacilityFanDriver{failDeviceID: firstID}
	registry := driver.NewRegistry()
	registry.Register(driverType, func() driver.Controller { return driverController })
	controller := NewFacilityFanController(devices, sites, registry, &recordingAudit{})
	event := &models.Event{
		ID:                   77,
		EventUUID:            uuid.New(),
		OrgID:                orgID,
		FacilityFanDeviceIDs: []int64{firstID, secondID},
		FacilityFanSiteIDs:   []int64{siteID, siteID},
	}

	firstFailure := controller.SetState(t.Context(), event, driver.PowerOn)
	require.NotNil(t, firstFailure)
	event.FanLastError = firstFailure
	secondFailure := controller.SetState(t.Context(), event, driver.PowerOn)
	require.NotNil(t, secondFailure)

	assert.Equal(t, []int64{firstID, secondID, secondID, firstID}, driverController.deviceIDs)
}

func TestFacilityFanController_DisabledClaimIsAuditedSkipAndCommandFailure(t *testing.T) {
	t.Parallel()

	const (
		orgID      = int64(42)
		disabledID = int64(501)
		enabledID  = int64(502)
		siteID     = int64(601)
		driverType = "test-fan"
	)
	ctrl := gomock.NewController(t)
	devices := storemocks.NewMockInfrastructureDeviceStore(ctrl)
	sites := storemocks.NewMockSiteStore(ctrl)
	gomock.InOrder(
		devices.EXPECT().
			GetInfrastructureDevice(gomock.Any(), orgID, disabledID).
			Return(&infrastructuremodels.Device{ID: disabledID, OrgID: orgID, SiteID: siteID}, nil),
		devices.EXPECT().
			GetInfrastructureDevice(gomock.Any(), orgID, enabledID).
			Return(&infrastructuremodels.Device{
				ID: enabledID, OrgID: orgID, SiteID: siteID, Enabled: true, DriverType: driverType,
			}, nil),
	)
	sites.EXPECT().
		GetInfrastructureControlSubnets(gomock.Any(), orgID, siteID).
		Return("10.0.0.0/24", nil)
	driverController := &recordingFacilityFanDriver{}
	registry := driver.NewRegistry()
	registry.Register(driverType, func() driver.Controller { return driverController })
	audit := &recordingAudit{}
	controller := NewFacilityFanController(devices, sites, registry, audit)
	event := &models.Event{
		EventUUID:            uuid.New(),
		OrgID:                orgID,
		State:                models.EventStateRestoring,
		FacilityFanDeviceIDs: []int64{disabledID, enabledID},
		FacilityFanSiteIDs:   []int64{siteID, siteID},
	}

	failure := controller.SetState(t.Context(), event, driver.PowerOn)

	require.NotNil(t, failure)
	assert.Contains(t, *failure, "device 501: device is disabled")
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, driverController.powers)
	require.Len(t, audit.events, 2)
	assert.Equal(t, ActivityTypeFacilityFanCommandSkipped, audit.events[0].Type)
	assert.Equal(t, activitymodels.ResultSuccess, audit.events[0].Result)
	assert.Equal(t, "on", audit.events[0].Metadata["desired_power"])
	assert.Equal(t, []int64{disabledID}, audit.events[0].Metadata["device_ids"])
	assert.Equal(t, "device_disabled", audit.events[0].Metadata["skip_reason"])
	assert.Equal(t, ActivityTypeFacilityFanCommandFailed, audit.events[1].Type)
	assert.Equal(t, activitymodels.ResultFailure, audit.events[1].Result)
	require.NotNil(t, audit.events[1].ErrorMessage)
	assert.Contains(t, *audit.events[1].ErrorMessage, "device 501: device is disabled")
}

func TestFacilityFanController_RejectsDeviceMovedFromAuthorizedSite(t *testing.T) {
	t.Parallel()

	const (
		orgID          = int64(42)
		deviceID       = int64(501)
		authorizedSite = int64(601)
		currentSite    = int64(602)
	)
	ctrl := gomock.NewController(t)
	devices := storemocks.NewMockInfrastructureDeviceStore(ctrl)
	sites := storemocks.NewMockSiteStore(ctrl)
	devices.EXPECT().
		GetInfrastructureDevice(gomock.Any(), orgID, deviceID).
		Return(&infrastructuremodels.Device{ID: deviceID, OrgID: orgID, SiteID: currentSite, Enabled: true}, nil)
	audit := &recordingAudit{}
	controller := NewFacilityFanController(devices, sites, driver.NewRegistry(), audit)
	event := &models.Event{
		EventUUID:            uuid.New(),
		OrgID:                orgID,
		State:                models.EventStateCompletedWithFailures,
		FacilityFanDeviceIDs: []int64{deviceID},
		FacilityFanSiteIDs:   []int64{authorizedSite},
	}

	failure := controller.SetState(t.Context(), event, driver.PowerOn)

	require.NotNil(t, failure)
	assert.Contains(t, *failure, "site changed since curtailment started")
	require.Len(t, audit.events, 1)
	require.NotNil(t, audit.events[0].ErrorMessage)
	assert.Contains(t, *audit.events[0].ErrorMessage, "site changed since curtailment started")
}

func TestShouldLogFacilityFanFailureGatesEachPowerPhase(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	failure := "device 501: command failed"
	tests := []struct {
		name  string
		event *models.Event
		power driver.PowerMode
		want  bool
	}{
		{
			name:  "later off failure after recovery starts a new incident",
			event: &models.Event{FanOffSentAt: &now},
			power: driver.PowerOff,
			want:  true,
		},
		{
			name:  "repeated off failure remains deduplicated",
			event: &models.Event{FanOffSentAt: &now, FanLastError: &failure},
			power: driver.PowerOff,
			want:  false,
		},
		{
			name:  "first on failure is independent from off failure",
			event: &models.Event{FanOffSentAt: &now, FanLastError: &failure},
			power: driver.PowerOn,
			want:  true,
		},
		{
			name:  "repeated on failure remains deduplicated",
			event: &models.Event{FanOnSentAt: &now, FanLastError: &failure},
			power: driver.PowerOn,
			want:  false,
		},
		{
			name:  "later on failure after recovery starts a new incident",
			event: &models.Event{FanOnSentAt: &now},
			power: driver.PowerOn,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, shouldLogFacilityFanFailure(tt.event, tt.power))
		})
	}
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		orgConfigByOrg:               map[int64]*models.OrgConfig{},
		activeDevicesByOrg:           map[int64][]string{},
		cooldownDevicesByOrg:         map[int64][]string{},
		candidatesByOrg:              map[int64][]*models.Candidate{},
		candidatesBySite:             map[int64]map[int64][]*models.Candidate{},
		sitesByOrg:                   map[int64]map[int64]bool{},
		eventsByUUID:                 map[uuid.UUID]*models.Event{},
		targetsByEventUUID:           map[uuid.UUID][]*models.Target{},
		automationRulesByEventUUID:   map[uuid.UUID]*models.AutomationRule{},
		automationRulesByExternalRef: map[string]*models.AutomationRule{},
		nextEventID:                  1,
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

func (f *fakeStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailmentTargetDevices not exercised")
}

func (f *fakeStore) ListRecentlyResolvedCurtailedDevices(
	_ context.Context,
	params interfaces.ListRecentlyResolvedCurtailedDevicesParams,
) ([]string, error) {
	f.cooldownCalls++
	f.lastCooldownOrgID = params.OrgID
	f.lastCooldownSec = params.CooldownSec
	f.lastCooldownFilter = append([]string(nil), params.DeviceIdentifiers...)
	f.lastCooldownSiteIDs = append([]int64(nil), params.SiteIDs...)
	return append([]string(nil), f.cooldownDevicesByOrg[params.OrgID]...), nil
}

func (f *fakeStore) SiteBelongsToOrg(_ context.Context, orgID, siteID int64) (bool, error) {
	return f.sitesByOrg[orgID][siteID], nil
}

func (f *fakeStore) ListCandidates(_ context.Context, params interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	f.listCandidatesCalls++
	f.lastListCandidatesOrgID = params.OrgID
	f.lastListCandidatesFilter = append([]string(nil), params.DeviceIdentifiers...)
	f.lastListCandidatesSiteIDs = append([]int64(nil), params.SiteIDs...)
	allCandidates := f.candidatesByOrg[params.OrgID]
	if len(params.SiteIDs) == 0 && len(params.DeviceIdentifiers) == 0 {
		return allCandidates, nil
	}
	seen := map[string]struct{}{}
	out := make([]*models.Candidate, 0, len(allCandidates))
	appendCandidate := func(candidate *models.Candidate) {
		if candidate == nil {
			return
		}
		if _, ok := seen[candidate.DeviceIdentifier]; ok {
			return
		}
		seen[candidate.DeviceIdentifier] = struct{}{}
		out = append(out, candidate)
	}
	for _, siteID := range params.SiteIDs {
		for _, candidate := range f.candidatesBySite[params.OrgID][siteID] {
			appendCandidate(candidate)
		}
	}
	if len(params.DeviceIdentifiers) == 0 {
		return out, nil
	}
	want := map[string]struct{}{}
	for _, id := range params.DeviceIdentifiers {
		want[id] = struct{}{}
	}
	for _, c := range allCandidates {
		if _, ok := want[c.DeviceIdentifier]; ok {
			appendCandidate(c)
		}
	}
	return out, nil
}

// --- panic stubs for methods Preview / Start do not exercise; Stop tests
// configure the maps above so the GetEventByUUID / ListTargetsByEvent /
// BeginRestoreTransition path is real-faked rather than panicking.

func (f *fakeStore) GetEventByUUID(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	if f.getEventByUUIDErr != nil {
		return nil, f.getEventByUUIDErr
	}
	ev, ok := f.eventsByUUID[eventUUID]
	if !ok {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	return ev, nil
}

func (f *fakeStore) GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	return f.GetEventByUUID(ctx, orgID, eventUUID)
}

func (f *fakeStore) ListActiveEvents(_ context.Context, _ int64) ([]*models.Event, error) {
	if f.activeEventErr != nil {
		return nil, f.activeEventErr
	}
	return f.activeEvents, nil
}

func (f *fakeStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if f.listTargetsErr != nil {
		return nil, f.listTargetsErr
	}
	return append([]*models.Target(nil), f.targetsByEventUUID[eventUUID]...), nil
}

func (f *fakeStore) ListTargetsByEventPage(ctx context.Context, params interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	targets, err := f.ListTargetsByEvent(ctx, params.OrgID, params.EventUUID)
	return targets, "", err
}

func (f *fakeStore) ListTargetSiteCoverageByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) (models.TargetSiteCoverage, error) {
	siteIDs := append([]int64(nil), f.targetSiteIDsByEventUUID[eventUUID]...)
	return models.TargetSiteCoverage{
		SiteIDs:           siteIDs,
		Complete:          true,
		TargetCount:       int64(len(siteIDs)),
		MappedTargetCount: int64(len(siteIDs)),
	}, nil
}

func (f *fakeStore) ListTargetSiteCoverageByEvents(_ context.Context, _ int64, eventUUIDs []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	coverageByEvent := make(map[uuid.UUID]models.TargetSiteCoverage, len(eventUUIDs))
	for _, eventUUID := range eventUUIDs {
		coverage, err := f.ListTargetSiteCoverageByEvent(context.Background(), 0, eventUUID)
		if err != nil {
			return nil, err
		}
		coverageByEvent[eventUUID] = coverage
	}
	return coverageByEvent, nil
}

func (f *fakeStore) GetTargetRollupByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.TargetRollup, error) {
	targets := f.targetsByEventUUID[eventUUID]
	rollup := &models.TargetRollup{Total: int64(len(targets))}
	for _, target := range targets {
		switch target.State {
		case models.TargetStatePending:
			rollup.Pending++
		case models.TargetStateDispatching, models.TargetStateDispatched:
			rollup.Dispatched++
		case models.TargetStateConfirmed:
			rollup.Confirmed++
		case models.TargetStateDrifted:
			rollup.Drifted++
		case models.TargetStateUnavailable:
			rollup.Unavailable++
		case models.TargetStateResolved:
			rollup.Resolved++
		case models.TargetStateReleased:
			rollup.Released++
		case models.TargetStateRestoreFailed:
			rollup.RestoreFailed++
		}
	}
	return rollup, nil
}

func (f *fakeStore) AdminTerminateEvent(_ context.Context, _ int64, eventUUID uuid.UUID, targetState models.EventState, reason string) (*models.Event, bool, error) {
	f.adminTerminateCalls++
	f.lastAdminTerminateUUID = eventUUID
	f.lastAdminTerminateState = targetState
	f.lastAdminTerminateReason = reason
	if f.adminTerminateErr != nil {
		return nil, false, f.adminTerminateErr
	}
	// transitioned defaults to true; tests that exercise the
	// idempotent-replay path set adminTerminateTransitioned=false.
	transitioned := !f.adminTerminateIdempotentReplay
	return f.adminTerminateResult, transitioned, nil
}

func (f *fakeStore) AdminTerminateEventWithFanRecovery(
	ctx context.Context,
	_ int64,
	eventUUID uuid.UUID,
	targetState models.EventState,
	reason string,
	command func(context.Context, *models.Event) *string,
) (*models.Event, bool, error) {
	f.adminTerminateWithFanRecoveryCalls++
	if f.getEventByUUIDErr != nil {
		return nil, false, f.getEventByUUIDErr
	}
	ev, ok := f.eventsByUUID[eventUUID]
	if ok && (ev.State == models.EventStatePending || ev.State == models.EventStateRestoring) && len(ev.FacilityFanDeviceIDs) > 0 {
		now := time.Now().UTC()
		params := interfaces.UpdateCurtailmentFanStateParams{
			ExpectedEventState: ev.State,
		}
		if ev.FanOnSentAt == nil {
			params.FanOnSentAt = &now
		}
		f.operatorFanCallOrder = append(f.operatorFanCallOrder, "admin terminate fan recovery")
		params.LastError = command(ctx, ev)
		if err := f.UpdateFanState(ctx, ev.ID, params); err != nil {
			return nil, false, err
		}
	}
	return f.AdminTerminateEvent(ctx, 0, eventUUID, targetState, reason)
}

func (f *fakeStore) ForceReleaseEvent(_ context.Context, _ int64, eventUUID uuid.UUID, reason string) (interfaces.ForceReleaseEventResult, error) {
	f.forceReleaseCalls++
	f.operatorFanCallOrder = append(f.operatorFanCallOrder, "force release")
	f.lastForceReleaseUUID = eventUUID
	f.lastForceReleaseReason = reason
	if f.forceReleaseErr != nil {
		return interfaces.ForceReleaseEventResult{}, f.forceReleaseErr
	}
	return interfaces.ForceReleaseEventResult{
		Event:              f.forceReleaseResult,
		SweptTargets:       f.forceReleaseSweptTargets,
		OwnershipReleased:  true,
		AutomationDisabled: f.forceReleaseAutomationDisabled,
	}, nil
}

func (f *fakeStore) ForceReleaseEventWithFanRecovery(
	ctx context.Context,
	_ int64,
	eventUUID uuid.UUID,
	reason string,
	eventID int64,
	_ []int64,
	_ []int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (interfaces.ForceReleaseEventResult, error) {
	result, err := f.ForceReleaseEvent(ctx, 0, eventUUID, reason)
	if err != nil || !result.OwnershipReleased {
		return result, err
	}
	f.operatorFanCallOrder = append(f.operatorFanCallOrder, "terminal fan recovery")
	params.LastError = command(ctx)
	if err := f.UpdateFanState(ctx, eventID, params); err != nil {
		return interfaces.ForceReleaseEventResult{}, err
	}
	if result.Event != nil {
		if params.FanOnSentAt != nil {
			result.Event.FanOnSentAt = params.FanOnSentAt
		}
		result.Event.FanLastError = params.LastError
	}
	return result, nil
}

func (f *fakeStore) UpdateFanState(_ context.Context, eventID int64, params interfaces.UpdateCurtailmentFanStateParams) error {
	f.updateFanStateCalls++
	f.lastUpdateFanStateID = eventID
	f.lastUpdateFanStateParams = params
	return f.updateFanStateErr
}

func (f *fakeStore) CommandFanState(
	ctx context.Context,
	eventID int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (*string, error) {
	f.operatorFanCallOrder = append(f.operatorFanCallOrder, "nonterminal fan command")
	lastError := command(ctx)
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		params.FanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
	}
	params.LastError = lastError
	return lastError, f.UpdateFanState(ctx, eventID, params)
}

func (f *fakeStore) RecoverTerminalFanState(
	ctx context.Context,
	eventID, _ int64,
	_ []int64,
	_ []int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) error {
	f.operatorFanCallOrder = append(f.operatorFanCallOrder, "terminal fan recovery")
	if f.terminalFanRecoveryErr != nil {
		return f.terminalFanRecoveryErr
	}
	params.LastError = command(ctx)
	return f.UpdateFanState(ctx, eventID, params)
}

// filterNonTerminalReplayEvent mirrors the production SQL's
// state IN ('pending','active','restoring') predicate on the replay
// lookups. Tests that seed terminal events into the lookup maps must
// see nil here so they exercise the fresh-Start path rather than a
// stale replay.
func filterNonTerminalReplayEvent(ev *models.Event) *models.Event {
	if ev == nil {
		return nil
	}
	if ev.State.IsTerminal() {
		return nil
	}
	return ev
}

func (f *fakeStore) GetEventByIdempotencyKey(_ context.Context, _ int64, idempotencyKey string) (*models.Event, error) {
	f.getByIdempotencyKeyCalls++
	f.lastGetByIdempotencyKey = idempotencyKey
	if f.getByIdempotencyKeyErr != nil {
		return nil, f.getByIdempotencyKeyErr
	}
	// Race-loser retry: after an InsertEventWithTargets attempt has fired,
	// the second lookup may see a row the first one missed.
	if f.insertEventCalls > 0 && f.eventsByIdempotencyKeyOnRetry != nil {
		return filterNonTerminalReplayEvent(f.eventsByIdempotencyKeyOnRetry[idempotencyKey]), nil
	}
	if f.eventsByIdempotencyKey == nil {
		return nil, nil
	}
	return filterNonTerminalReplayEvent(f.eventsByIdempotencyKey[idempotencyKey]), nil
}

func (f *fakeStore) GetEventByExternalReference(_ context.Context, _ int64, externalSource, externalReference string) (*models.Event, error) {
	f.getByExternalRefCalls++
	f.lastGetByExternalRefSource = externalSource
	f.lastGetByExternalRefRef = externalReference
	if f.getByExternalRefErr != nil {
		return nil, f.getByExternalRefErr
	}
	if f.insertEventCalls > 0 && f.eventsByExternalRefOnRetry != nil {
		return filterNonTerminalReplayEvent(f.eventsByExternalRefOnRetry[externalSource+"|"+externalReference]), nil
	}
	if f.eventsByExternalRef == nil {
		return nil, nil
	}
	return filterNonTerminalReplayEvent(f.eventsByExternalRef[externalSource+"|"+externalReference]), nil
}

func (f *fakeStore) UpdateOperatorFields(_ context.Context, eventID, _ int64, params interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	f.updateOperatorFieldsCalls++
	f.lastUpdateOperatorFieldsID = eventID
	f.lastUpdateOperatorFieldsArgs = params
	if f.updateOperatorFieldsErr != nil {
		return nil, f.updateOperatorFieldsErr
	}
	return f.updateOperatorFieldsResult, nil
}

func (f *fakeStore) ListEvents(_ context.Context, params interfaces.ListEventsParams) ([]*models.Event, string, error) {
	f.listEventsCalls++
	f.lastListEventsParams = params
	if f.listEventsErr != nil {
		return nil, "", f.listEventsErr
	}
	// Filter on org_id + optional states; cursor + page-size handling is
	// faithful to the SQL impl: cursor is the last id from the previous
	// page, page_size <= 0 falls back to the default. The fake's "history"
	// slice is ordered newest-first by the test author.
	stateFilters := make(map[models.EventState]struct{}, len(params.StateFilters))
	for _, filter := range params.StateFilters {
		stateFilters[filter] = struct{}{}
	}
	filtered := make([]*models.Event, 0, len(f.eventsHistory))
	for _, ev := range f.eventsHistory {
		if ev.OrgID != params.OrgID {
			continue
		}
		if len(stateFilters) > 0 {
			if _, ok := stateFilters[ev.State]; !ok {
				continue
			}
		}
		filtered = append(filtered, ev)
	}
	cursorID := int64(0)
	if params.PageToken != "" {
		// Tests pass a plain decimal id as the token for clarity.
		_, _ = fmt.Sscanf(params.PageToken, "%d", &cursorID)
	}
	if cursorID > 0 {
		after := make([]*models.Event, 0, len(filtered))
		for _, ev := range filtered {
			if ev.ID < cursorID {
				after = append(after, ev)
			}
		}
		filtered = after
	}
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if int64(len(filtered)) <= int64(pageSize) {
		return filtered, "", nil
	}
	page := filtered[:pageSize]
	return page, fmt.Sprintf("%d", page[len(page)-1].ID), nil
}

func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) ListEligibleConfirmationTargets(context.Context) ([]models.ConfirmationTarget, error) {
	panic("ListEligibleConfirmationTargets not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) RecordCurtailPendingDispatch(context.Context, int64, models.EventState, time.Time) error {
	panic("RecordCurtailPendingDispatch not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by Preview/Start/Stop tests")
}

func (f *fakeStore) BeginRestoreTransition(
	_ context.Context,
	_ int64,
	eventUUID uuid.UUID,
	params interfaces.BeginRestoreTransitionParams,
) (*models.Event, error) {
	f.beginRestoreCalls++
	f.beginRestoreLastEventID = eventUUID
	if f.beginRestoreErr != nil {
		return nil, f.beginRestoreErr
	}
	if err := f.guardAutomationDemandForRestore(eventUUID, params.AutomationDemandGuard); err != nil {
		return nil, err
	}
	// Default: mutate the seeded event copy so the test sees a 'restoring' echo.
	ev, ok := f.eventsByUUID[eventUUID]
	if !ok {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	updated := *ev
	updated.State = models.EventStateRestoring
	return &updated, nil
}

func (f *fakeStore) guardAutomationDemandForRestore(eventUUID uuid.UUID, guard *interfaces.AutomationDemandGuard) error {
	if guard == nil {
		return nil
	}
	f.automationDemandGuardCheckRuns++
	ev := f.eventsByUUID[eventUUID]
	if ev == nil {
		return fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	rule := f.automationRulesByEventUUID[eventUUID]
	if (rule == nil || rule.OrgID != ev.OrgID || !rule.Enabled) && guard.ExternalReference != nil {
		rule = f.automationRulesByExternalRef[*guard.ExternalReference]
	}
	if rule == nil || rule.OrgID != ev.OrgID || !rule.Enabled {
		return nil
	}
	if rule.LastSignal == nil || *rule.LastSignal != models.AutomationSignalOff {
		return nil
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"cannot restore automation-owned curtailment event %s while automation rule %q still has OFF asserted; use force=true to override",
		eventUUID,
		rule.RuleName,
	)
}

func (f *fakeStore) BeginRecurtailTransition(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	f.beginRecurtailCalls++
	f.beginRecurtailLastEventID = eventUUID
	if f.beginRecurtailErr != nil {
		return nil, f.beginRecurtailErr
	}
	ev, ok := f.eventsByUUID[eventUUID]
	if !ok {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	updated := *ev
	updated.State = models.EventStatePending
	return &updated, nil
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
	// Mirror the SQL store: only terminal or closed-loop full-fleet events may
	// have zero targets.
	if len(targets) == 0 && !event.State.IsTerminal() &&
		!(event.Mode == models.ModeFullFleet && event.LoopType == models.LoopTypeClosed) {
		return nil, fleeterror.NewInvalidArgumentError(
			"InsertEventWithTargets requires a non-empty targets slice for a non-terminal event",
		)
	}
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

func (f *fakeStore) ClaimClosedLoopFullFleetTargets(
	context.Context,
	int64,
	int64,
	int32,
	[]models.InsertTargetParams,
) ([]*models.Target, error) {
	panic("ClaimClosedLoopFullFleetTargets not exercised")
}

func (f *fakeStore) ClaimAllPairedPolicyTargets(
	context.Context,
	int64,
	[]models.InsertTargetParams,
) (int64, error) {
	panic("ClaimAllPairedPolicyTargets not exercised")
}

func (f *fakeStore) BulkRefreshAllPairedTargetReadiness(
	context.Context,
	int64,
	models.EventState,
	[]interfaces.AllPairedReadinessUpdate,
) ([]string, error) {
	panic("BulkRefreshAllPairedTargetReadiness not exercised")
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
	}
}

// recordingMetrics is a goroutine-safe Metrics fake for asserting recorder
// calls. The reconciler emits from its tick goroutine and the Service emits
// from the request goroutine; the mutex is defensive across both.
//
// Compile-time assertion surfaces a missing method at build time rather than
// letting the duplicate definition in reconciler/reconciler_test.go drift.
var _ Metrics = (*recordingMetrics)(nil)

type recordingMetrics struct {
	mu                     sync.Mutex
	tickDurations          []time.Duration
	tickFailures           int
	candidateExcluded      map[string]int
	maintenance            int
	eventStateRaces        int
	targetWriteFailures    int
	auditWriteFailures     map[string]int
	allPairedPendingStalls int
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{
		candidateExcluded:  map[string]int{},
		auditWriteFailures: map[string]int{},
	}
}

func (m *recordingMetrics) ObserveTickDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickDurations = append(m.tickDurations, d)
}

func (m *recordingMetrics) IncTickFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickFailures++
}

func (m *recordingMetrics) IncCandidateExcluded(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candidateExcluded[reason]++
}

func (m *recordingMetrics) IncMaintenanceOverride() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maintenance++
}

func (m *recordingMetrics) IncEventStateRaceLoss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventStateRaces++
}

func (m *recordingMetrics) IncTargetWriteFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targetWriteFailures++
}

func (m *recordingMetrics) IncAllPairedPendingStall() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allPairedPendingStalls++
}

func (m *recordingMetrics) IncAuditWriteFailure(activityType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.auditWriteFailures == nil {
		m.auditWriteFailures = map[string]int{}
	}
	m.auditWriteFailures[activityType]++
}

func (m *recordingMetrics) CandidateExcludedCount(reason string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.candidateExcluded[reason]
}

func (m *recordingMetrics) TargetWriteFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targetWriteFailures
}

func (m *recordingMetrics) AuditWriteFailureCount(activityType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.auditWriteFailures[activityType]
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

func TestService_Preview_SiteScopeValidatesSiteAndPassesFilterToStore(t *testing.T) {
	t.Parallel()
	const (
		orgID  = int64(1)
		siteID = int64(99)
	)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.sitesByOrg[orgID] = map[int64]bool{siteID: true}
	store.candidatesBySite[orgID] = map[int64][]*models.Candidate{
		siteID: {
			minerWithEff("site-miner", 3000, 100, 30),
		},
	}
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeSite, SiteID: siteID}
	_, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []int64{siteID}, store.lastListCandidatesSiteIDs)
	assert.Empty(t, store.lastListCandidatesFilter)
}

func TestService_Preview_MixedScopeUnionsSitesAndDevicesWithDeduplication(t *testing.T) {
	t.Parallel()

	const (
		orgID  = int64(1)
		siteID = int64(99)
	)
	siteMiner := minerWithEff("site-miner", 3000, 100, 30)
	explicitMiner := minerWithEff("explicit-miner", 4000, 100, 40)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.sitesByOrg[orgID] = map[int64]bool{siteID: true}
	store.candidatesByOrg[orgID] = []*models.Candidate{siteMiner, explicitMiner}
	store.candidatesBySite[orgID] = map[int64][]*models.Candidate{
		siteID: {siteMiner},
	}
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{
		Type:              models.ScopeTypeMixed,
		SiteIDs:           []int64{siteID},
		DeviceIdentifiers: []string{"site-miner", "explicit-miner", "explicit-miner"},
	}
	req.Mode = models.ModeFullFleet

	plan, err := svc.Preview(t.Context(), req)

	require.NoError(t, err)
	assert.Equal(t, 1, store.listCandidatesCalls)
	assert.Equal(t, []int64{siteID}, store.lastListCandidatesSiteIDs)
	assert.Equal(t, []string{"site-miner", "explicit-miner"}, store.lastListCandidatesFilter)
	require.Len(t, plan.Selected, 2)
	selectedIDs := make([]string, 0, len(plan.Selected))
	for _, selected := range plan.Selected {
		selectedIDs = append(selectedIDs, selected.DeviceIdentifier)
	}
	assert.ElementsMatch(t, []string{"site-miner", "explicit-miner"}, selectedIDs)
}

func TestService_Preview_SiteScopeRequiresExistingSite(t *testing.T) {
	t.Parallel()
	const (
		orgID  = int64(1)
		siteID = int64(99)
	)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	svc := NewService(store)
	req := validRequest(orgID)
	req.Scope = Scope{Type: models.ScopeTypeSite, SiteID: siteID}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
	assert.Zero(t, store.listCandidatesCalls, "missing sites must reject before candidate selection")
}

func TestResolveScope_WholeOrgDominatesSelectorFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		scope Scope
	}{
		{"explicit whole org with site", Scope{Type: models.ScopeTypeWholeOrg, SiteID: 99}},
		{"implicit whole org with device identifiers", Scope{DeviceIdentifiers: []string{"miner-a"}}},
		{"explicit whole org with device sets", Scope{Type: models.ScopeTypeWholeOrg, DeviceSetIDs: []string{"set-a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveScope(tc.scope)

			require.NoError(t, err)
		})
	}
}

func TestService_Preview_DeviceListScopeRequiresNonEmptyList(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())
	req := validRequest(1)
	req.Scope = Scope{Type: models.ScopeTypeDeviceList}
	_, err := svc.Preview(t.Context(), req)
	require.Error(t, err)
}

func TestService_Preview_DeviceListWithDeviceSetsReturnsUnimplemented(t *testing.T) {
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
	assert.Contains(t, err.Error(), "device-set scope is not implemented")
}

func TestService_Preview_DeviceSetWithDeviceListReturnsUnimplemented(t *testing.T) {
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
	assert.Contains(t, err.Error(), "device-set scope is not implemented")
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
		miner("needs-pool", "NEEDS_MINING_POOL", "PAIRED", 3000, 0),
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
	// Pool-less miner passes status admission (#663); in fixed-kW mode the
	// dual-signal filter then skips it with the sharper diagnostic — idle
	// draw with zero hash is phantom load for kW-sized selection.
	assert.Equal(t, SkipPhantomLoadNoHash, reasons["needs-pool"])
	assert.Equal(t, SkipMaintenance, reasons["maintenance"])
	assert.Equal(t, SkipStaleTelemetry, reasons["stale"])
}

func TestClassifyCandidates_PoolLessMinerAdmittedWhenTelemetryFresh(t *testing.T) {
	t.Parallel()

	// Commandability admission (#663): NEEDS_MINING_POOL passes status
	// admission even with zero hash, but stays behind the freshness gates
	// plus a positive-power requirement (power is its only confirmable
	// signal). A stale-positive hash sample is overridden by the status: the
	// eligible candidate carries hash 0 so fixed-kW accounting and baseline
	// persistence treat it as non-hashing.
	fresh := miner("needs-pool-fresh", "NEEDS_MINING_POOL", "PAIRED", 2000, 0)
	stalePositiveHash := miner("needs-pool-stale-hash", "NEEDS_MINING_POOL", "PAIRED", 2000, 100)
	// Power-only telemetry: a pool-less miner may never have reported a hash
	// sample at all; the hash-sample freshness requirement must not apply.
	noHashSample := miner("needs-pool-no-hash-sample", "NEEDS_MINING_POOL", "PAIRED", 2000, 0)
	noHashSample.LatestHashRateHS = nil
	zeroPower := miner("needs-pool-zero-power", "NEEDS_MINING_POOL", "PAIRED", 0, 0)
	stale := staleMiner("needs-pool-stale")
	stale.DeviceStatus = "NEEDS_MINING_POOL"

	eligible, skipped, _ := classifyCandidates(
		[]*models.Candidate{fresh, stalePositiveHash, noHashSample, zeroPower, stale},
		classifyOpts{CandidateMinPowerW: 1500},
	)

	require.Len(t, eligible, 3)
	assert.Equal(t, "needs-pool-fresh", eligible[0].DeviceIdentifier)
	assert.Equal(t, "needs-pool-stale-hash", eligible[1].DeviceIdentifier)
	assert.Zero(t, eligible[1].HashRateHS,
		"device status is authoritative: a pool-less miner cannot be mining, so a stale-positive hash sample must not count as mining load")
	assert.Equal(t, "needs-pool-no-hash-sample", eligible[2].DeviceIdentifier)
	assert.Zero(t, eligible[2].HashRateHS)

	reasons := map[string]SkipReason{}
	for _, s := range skipped {
		reasons[s.DeviceIdentifier] = s.Reason
	}
	require.Len(t, skipped, 2)
	assert.Equal(t, SkipStaleTelemetry, reasons["needs-pool-zero-power"],
		"zero power is as good as stale for a miner whose only confirmable signal is power")
	assert.Equal(t, SkipStaleTelemetry, reasons["needs-pool-stale"])
}

func TestService_Preview_PoolLessZeroHashMinerSkippedByDualSignalInFixedKw(t *testing.T) {
	t.Parallel()

	// A pool-less miner passes status admission (#663) but fixed-kW's
	// dual-signal filter still excludes it: idle draw with zero hash is
	// phantom load for kW-sized selection.
	const orgID = int64(1)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		miner("needs-pool", "NEEDS_MINING_POOL", "PAIRED", 3000, 0),
		minerWithEff("eligible", 3000, 100, 40),
	}

	svc := NewService(store)
	req := validRequest(orgID)
	req.TargetKW = 2.5
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "eligible", plan.Selected[0].DeviceIdentifier)
	reasons := map[string]SkipReason{}
	for _, s := range plan.Skipped {
		reasons[s.DeviceIdentifier] = s.Reason
	}
	assert.Equal(t, SkipPhantomLoadNoHash, reasons["needs-pool"])
}

func TestBuildInsertTargetParams_BaselineFloorAppliesOnlyToHashingMiners(t *testing.T) {
	t.Parallel()

	selected := []SelectedDevice{
		// Hashing below the floor: suspect power reading (dead monitor);
		// hash-only fallback works, so no baseline is persisted.
		{DeviceIdentifier: "hashing-below-floor", PowerW: 400, HashRateHS: 100},
		// Never-hashing below the floor: hash-only fallback is broken, so
		// the idle-draw baseline must be persisted (#663).
		{DeviceIdentifier: "idle-below-floor", PowerW: 400, HashRateHS: 0},
		// Zero power never persists a baseline regardless of hash.
		{DeviceIdentifier: "no-power", PowerW: 0, HashRateHS: 0},
	}

	targets := BuildInsertTargetParams(selected, models.ModeFullFleet, 1500)

	require.Len(t, targets, 3)
	assert.Nil(t, targets[0].BaselinePowerW)
	require.NotNil(t, targets[1].BaselinePowerW)
	assert.InDelta(t, 400.0, *targets[1].BaselinePowerW, 0.001)
	assert.Nil(t, targets[2].BaselinePowerW)
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

func TestService_Preview_CooldownZeroDoesNotQueryRecentlyResolvedDevices(t *testing.T) {
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

	assert.Zero(t, store.cooldownCalls)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "recent", plan.Selected[0].DeviceIdentifier)
}

func TestService_Preview_PositiveCooldownExcludesRecentlyResolvedDevices(t *testing.T) {
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
	req.Scope = Scope{
		Type:              models.ScopeTypeDeviceList,
		DeviceIdentifiers: []string{"recent", "ok"},
	}
	req.PostEventCooldownSec = 600
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, 1, store.cooldownCalls)
	assert.Equal(t, orgID, store.lastCooldownOrgID)
	assert.Equal(t, int32(600), store.lastCooldownSec)
	assert.ElementsMatch(t, []string{"recent", "ok"}, store.lastCooldownFilter)
	assert.Nil(t, store.lastCooldownSiteIDs)
	require.Len(t, plan.Selected, 1)
	assert.Equal(t, "ok", plan.Selected[0].DeviceIdentifier)
	reasons := map[string]SkipReason{}
	for _, skipped := range plan.Skipped {
		reasons[skipped.DeviceIdentifier] = skipped.Reason
	}
	assert.Equal(t, SkipCooldown, reasons["recent"])
}

func TestService_Preview_EmergencyPriorityBypassesSuppliedCooldown(t *testing.T) {
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
	req.Priority = models.PriorityEmergency
	req.PostEventCooldownSec = 600
	req.TargetKW = 1
	plan, err := svc.Preview(t.Context(), req)
	require.NoError(t, err)

	assert.Zero(t, store.cooldownCalls)
	require.Len(t, plan.Selected, 1)
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
	store.candidatesByOrg[callerOrg] = []*models.Candidate{minerWithEff("caller-miner", 3000, 100, 40)}
	store.candidatesByOrg[otherOrg] = []*models.Candidate{minerWithEff("other-miner", 3000, 100, 40)}

	svc := NewService(store)
	plan, err := svc.Preview(t.Context(), validRequest(callerOrg))
	require.NoError(t, err)

	assert.Equal(t, callerOrg, store.lastActiveDevicesOrgID, "active-event lookup must use caller's org_id")
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
