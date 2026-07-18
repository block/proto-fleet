package reconciler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Compile-time assertion that recordingMetrics satisfies curtailment.Metrics —
// surfaces a missing method at build time rather than letting the duplicate
// definition in service_test.go drift independently.
var _ curtailment.Metrics = (*recordingMetrics)(nil)

// fakeStore is an in-memory CurtailmentStore for reconciler tests. Methods
// the reconciler does not exercise panic so an unintended call is loud.
type fakeStore struct {
	events           []*models.Event
	targetsByEventID map[int64][]*models.Target
	candidates       []*models.Candidate
	orgConfig        *models.OrgConfig
	activeDevices    []string

	listEventsErr      error
	listEventsCalls    int
	listEventsPanicErr string
	listTargetsHook    func(context.Context, uuid.UUID)
	listTargetsCtxErr  map[uuid.UUID]error

	updateEventCalls      int
	updateEventLast       map[int64]models.EventState
	updateTargetCalls     int
	updateTargetParams    map[string]interfaces.UpdateCurtailmentTargetStateParams
	updateTargetStateErr  error
	updateTargetStateHook func(device string, params interfaces.UpdateCurtailmentTargetStateParams, call int) error

	bumpTargetRetryCalls int
	lastBumpTargetRetry  bumpRetryCall
	bumpTargetRetryErr   error

	listTargetsByEventCalls int
	listCandidatesCalls     int
	claimTargetsCalls       int
	claimedTargetParams     []models.InsertTargetParams
	claimAllPairedCalls     int
	claimedAllPairedParams  []models.InsertTargetParams
	bulkRefreshCalls        int
	lastBulkRefreshUpdates  []interfaces.AllPairedReadinessUpdate
	bulkRefreshErr          error
	// bulkRefreshSkipDevices simulates rows another actor advanced between
	// the reconciler's read and the bulk UPDATE: the SQL guards skip them,
	// so they are neither mutated nor reported in RETURNING.
	bulkRefreshSkipDevices map[string]bool
	cooldownDevices        []string
	cooldownCalls          int
	lastCooldownOrgID      int64
	lastCooldownSec        int32
	lastCooldownFilter     []string
	lastCooldownSiteIDs    []int64

	heartbeatCalls            int
	lastHeartbeatActive       int32
	lastHeartbeatTickUUID     uuid.UUID
	lastListCandidatesSiteIDs []int64
	lastListCandidatesFilter  []string
	listCandidatesFilters     [][]string

	// BeginRestoreTransition captures, exercised by max_duration tests.
	beginRestoreCalls       int
	beginRestoreLastEventID uuid.UUID
	beginRestoreErr         error
	updateFanCalls          int
	lastFanUpdate           interfaces.UpdateCurtailmentFanStateParams
	rejectExpiredFanUpdate  bool
	failFanUpdateCall       int
}

type bumpRetryCall struct {
	EventID          int64
	DeviceIdentifier string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		targetsByEventID:   map[int64][]*models.Target{},
		updateEventLast:    map[int64]models.EventState{},
		updateTargetParams: map[string]interfaces.UpdateCurtailmentTargetStateParams{},
		listTargetsCtxErr:  map[uuid.UUID]error{},
	}
}

func (f *fakeStore) GetOrgConfig(_ context.Context, orgID int64) (*models.OrgConfig, error) {
	if f.orgConfig != nil {
		return f.orgConfig, nil
	}
	return &models.OrgConfig{
		OrgID:              orgID,
		CandidateMinPowerW: 1500,
	}, nil
}
func (f *fakeStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	return append([]string(nil), f.activeDevices...), nil
}
func (f *fakeStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	return append([]string(nil), f.activeDevices...), nil
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
	return append([]string(nil), f.cooldownDevices...), nil
}
func (f *fakeStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised")
}
func (f *fakeStore) GetEventByUUID(_ context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	for _, ev := range f.events {
		if ev.OrgID == orgID && ev.EventUUID == eventUUID {
			return ev, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	return f.GetEventByUUID(ctx, orgID, eventUUID)
}
func (f *fakeStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised")
}
func (f *fakeStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised")
}
func (f *fakeStore) ClaimClosedLoopFullFleetTargets(
	_ context.Context,
	eventID int64,
	_ int64,
	_ int32,
	targets []models.InsertTargetParams,
) ([]*models.Target, error) {
	f.claimTargetsCalls++
	f.claimedTargetParams = append([]models.InsertTargetParams(nil), targets...)
	existing := map[string]struct{}{}
	for _, t := range f.targetsByEventID[eventID] {
		existing[t.DeviceIdentifier] = struct{}{}
	}
	var claimed []*models.Target
	for _, target := range targets {
		if _, ok := existing[target.DeviceIdentifier]; ok {
			continue
		}
		row := &models.Target{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   target.DeviceIdentifier,
			TargetType:         target.TargetType,
			State:              models.TargetStateDispatching,
			DesiredState:       target.DesiredState,
			BaselinePowerW:     target.BaselinePowerW,
		}
		f.targetsByEventID[eventID] = append(f.targetsByEventID[eventID], row)
		claimed = append(claimed, row)
		existing[target.DeviceIdentifier] = struct{}{}
	}
	return claimed, nil
}
func (f *fakeStore) ClaimAllPairedPolicyTargets(
	_ context.Context,
	eventID int64,
	targets []models.InsertTargetParams,
) (int64, error) {
	f.claimAllPairedCalls++
	f.claimedAllPairedParams = append([]models.InsertTargetParams(nil), targets...)
	existing := map[string]*models.Target{}
	for _, t := range f.targetsByEventID[eventID] {
		existing[t.DeviceIdentifier] = t
	}

	var claimed int64
	for _, target := range targets {
		state := target.State
		if state == "" {
			state = models.TargetStatePending
		}
		if row, ok := existing[target.DeviceIdentifier]; ok {
			if row.State != models.TargetStateReleased {
				continue
			}
			row.State = state
			row.DesiredState = target.DesiredState
			row.BaselinePowerW = target.BaselinePowerW
			row.LastError = target.LastError
			row.ReleasedAt = nil
			row.CurtailPhase = models.TargetPhaseSummary{}
			claimed++
			continue
		}

		row := &models.Target{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   target.DeviceIdentifier,
			TargetType:         target.TargetType,
			State:              state,
			DesiredState:       target.DesiredState,
			BaselinePowerW:     target.BaselinePowerW,
			AddedAt:            time.Now(),
			LastError:          target.LastError,
		}
		f.targetsByEventID[eventID] = append(f.targetsByEventID[eventID], row)
		existing[target.DeviceIdentifier] = row
		claimed++
	}
	return claimed, nil
}

// BulkRefreshAllPairedTargetReadiness: real-fake mirroring the SQL guards —
// only pending/unavailable curtail-phase rows on an event still in the
// expected state flip; everything else is skipped, not clobbered. Returns
// the applied device identifiers, mirroring the RETURNING clause.
func (f *fakeStore) BulkRefreshAllPairedTargetReadiness(
	_ context.Context,
	eventID int64,
	expectedEventState models.EventState,
	updates []interfaces.AllPairedReadinessUpdate,
) ([]string, error) {
	f.bulkRefreshCalls++
	f.lastBulkRefreshUpdates = append([]interfaces.AllPairedReadinessUpdate(nil), updates...)
	if f.bulkRefreshErr != nil {
		return nil, f.bulkRefreshErr
	}
	for _, ev := range f.events {
		if ev.ID == eventID && ev.State != expectedEventState {
			return nil, nil
		}
	}
	var applied []string
	byDevice := map[string]*models.Target{}
	for _, t := range f.targetsByEventID[eventID] {
		byDevice[t.DeviceIdentifier] = t
	}
	for _, update := range updates {
		if f.bulkRefreshSkipDevices[update.DeviceIdentifier] {
			continue
		}
		t, ok := byDevice[update.DeviceIdentifier]
		if !ok {
			continue
		}
		if t.DesiredState != "" && t.DesiredState != models.DesiredStateCurtailed {
			continue
		}
		if t.State != models.TargetStatePending && t.State != models.TargetStateUnavailable {
			continue
		}
		if update.State != models.TargetStatePending && update.State != models.TargetStateUnavailable {
			continue
		}
		t.State = update.State
		if update.Reason == "" {
			t.LastError = nil
		} else {
			reason := update.Reason
			t.LastError = &reason
		}
		if update.BaselinePowerW != nil && t.BaselinePowerW == nil {
			baseline := *update.BaselinePowerW
			t.BaselinePowerW = &baseline
		}
		reason := update.Reason
		updateTargetPhaseSummary(t, interfaces.UpdateCurtailmentTargetStateParams{
			State:     update.State,
			LastError: &reason,
		})
		applied = append(applied, update.DeviceIdentifier)
	}
	return applied, nil
}

func (f *fakeStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised")
}

func (f *fakeStore) ListTargetsByEvent(ctx context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	f.listTargetsByEventCalls++
	if f.listTargetsHook != nil {
		f.listTargetsHook(ctx, eventUUID)
	}
	f.listTargetsCtxErr[eventUUID] = ctx.Err()
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			// Return shared pointers so the reconciler's per-tick mutations
			// are visible across phases (mirrors how the SQL store flow
			// works once dispatchPending returns the in-memory slice).
			return append([]*models.Target(nil), f.targetsByEventID[ev.ID]...), nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListTargetsByEventPage(ctx context.Context, params interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	targets, err := f.ListTargetsByEvent(ctx, params.OrgID, params.EventUUID)
	return targets, "", err
}

func (f *fakeStore) ListTargetSiteCoverageByEvent(context.Context, int64, uuid.UUID) (models.TargetSiteCoverage, error) {
	panic("ListTargetSiteCoverageByEvent not exercised by reconciler tests")
}

func (f *fakeStore) ListTargetSiteCoverageByEvents(context.Context, int64, []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	panic("ListTargetSiteCoverageByEvents not exercised by reconciler tests")
}

func (f *fakeStore) GetTargetRollupByEvent(context.Context, int64, uuid.UUID) (*models.TargetRollup, error) {
	panic("GetTargetRollupByEvent not exercised by reconciler tests")
}

func (f *fakeStore) ListCandidates(_ context.Context, params interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	f.listCandidatesCalls++
	f.lastListCandidatesSiteIDs = append([]int64(nil), params.SiteIDs...)
	f.lastListCandidatesFilter = append([]string(nil), params.DeviceIdentifiers...)
	f.listCandidatesFilters = append(f.listCandidatesFilters, append([]string(nil), params.DeviceIdentifiers...))
	if len(params.DeviceIdentifiers) == 0 {
		return f.candidates, nil
	}
	want := map[string]struct{}{}
	for _, id := range params.DeviceIdentifiers {
		want[id] = struct{}{}
	}
	out := make([]*models.Candidate, 0, len(f.candidates))
	for _, c := range f.candidates {
		if _, ok := want[c.DeviceIdentifier]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by reconciler tests")
}

func (f *fakeStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by reconciler tests")
}

func (f *fakeStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by reconciler tests")
}

func (f *fakeStore) ForceReleaseEvent(context.Context, int64, uuid.UUID, string) (interfaces.ForceReleaseEventResult, error) {
	panic("ForceReleaseEvent not exercised by reconciler tests")
}

func (f *fakeStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by reconciler tests")
}

func (f *fakeStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by reconciler tests")
}

func (f *fakeStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	f.listEventsCalls++
	if f.listEventsPanicErr != "" {
		panic(f.listEventsPanicErr)
	}
	if f.listEventsErr != nil {
		return nil, f.listEventsErr
	}
	out := make([]*models.Event, 0, len(f.events))
	for _, ev := range f.events {
		out = append(out, ev)
	}
	return out, nil
}

func (f *fakeStore) UpdateEventState(_ context.Context, eventID int64, expectedState models.EventState, state models.EventState, _ *time.Time, _ *time.Time) error {
	f.updateEventCalls++
	f.updateEventLast[eventID] = state
	for _, ev := range f.events {
		if ev.ID == eventID {
			if ev.State != expectedState {
				return interfaces.ErrCurtailmentEventStateRaceLoss
			}
			ev.State = state
		}
	}
	return nil
}

func (f *fakeStore) UpdateFanState(ctx context.Context, eventID int64, params interfaces.UpdateCurtailmentFanStateParams) error {
	f.updateFanCalls++
	f.lastFanUpdate = params
	if f.failFanUpdateCall == f.updateFanCalls {
		return errors.New("injected fan state update failure")
	}
	if f.rejectExpiredFanUpdate && ctx.Err() != nil {
		return fmt.Errorf("expired fan update context: %w", ctx.Err())
	}
	for _, event := range f.events {
		if event.ID != eventID {
			continue
		}
		if event.State != params.ExpectedEventState {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		if params.FanOffSentAt != nil {
			event.FanOffSentAt = params.FanOffSentAt
		}
		if params.FanOnSentAt != nil {
			event.FanOnSentAt = params.FanOnSentAt
		}
		if params.FanAirflowReopenedAt != nil {
			event.FanAirflowReopenedAt = params.FanAirflowReopenedAt
		}
		if params.ClearFanAirflowReopenedAt {
			event.FanAirflowReopenedAt = nil
		}
		event.FanLastError = params.LastError
	}
	return nil
}

func (f *fakeStore) CommandFanState(
	ctx context.Context,
	eventID int64,
	params interfaces.UpdateCurtailmentFanStateParams,
	command func(context.Context) *string,
) (*string, error) {
	lastError := command(ctx)
	if lastError == nil && params.FanAirflowReopenedAtOnSuccess != nil {
		params.FanAirflowReopenedAt = params.FanAirflowReopenedAtOnSuccess
	}
	params.LastError = lastError
	return lastError, f.UpdateFanState(ctx, eventID, params)
}

func (f *fakeStore) UpdateTargetState(_ context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	f.updateTargetCalls++
	f.updateTargetParams[deviceIdentifier] = params
	// updateTargetStateHook lets a test reject specific writes (e.g.
	// simulate the EXISTS guard's race-loss sentinel firing on the Nth
	// call) without globally poisoning the fake.
	if f.updateTargetStateHook != nil {
		if err := f.updateTargetStateHook(deviceIdentifier, params, f.updateTargetCalls); err != nil {
			return err
		}
	}
	// updateTargetStateErr lets tests inject the race-loss sentinel or other
	// errors without going through the in-memory state machine. When set,
	// the mirror is not advanced — matches the sqlstore contract.
	if f.updateTargetStateErr != nil {
		return f.updateTargetStateErr
	}
	for _, t := range f.targetsByEventID[eventID] {
		if t.DeviceIdentifier == deviceIdentifier {
			// Honor the ExpectedDesiredState predicate: the real SQL's
			// `desired_state = $11` clause makes the UPDATE no-op when the
			// caller's expected direction doesn't match the row, surfacing
			// as the race-loss sentinel. The fake mirrors that contract.
			//
			// Test-double simplification: an empty t.DesiredState means the
			// test author didn't set it (production targets are NOT NULL).
			// Treat empty as "any" so existing tests that don't care about
			// phase don't have to backfill the field. Tests that pin the
			// Curtail-vs-Stop dispatch-direction race set t.DesiredState
			// explicitly.
			if params.ExpectedEventState != nil {
				for _, ev := range f.events {
					if ev.ID == eventID && ev.State != *params.ExpectedEventState {
						return interfaces.ErrCurtailmentEventStateRaceLoss
					}
				}
			}
			if params.ExpectedDesiredState != nil && t.DesiredState != "" && t.DesiredState != *params.ExpectedDesiredState {
				return interfaces.ErrCurtailmentEventStateRaceLoss
			}
			t.State = params.State
			if params.LastDispatchedAt != nil {
				t.LastDispatchedAt = params.LastDispatchedAt
			}
			if params.LastBatchUUID != nil {
				t.LastBatchUUID = params.LastBatchUUID
			}
			if params.ObservedPowerW != nil {
				t.ObservedPowerW = params.ObservedPowerW
			}
			if params.ObservedAt != nil {
				t.ObservedAt = params.ObservedAt
			}
			if params.ConfirmedAt != nil {
				t.ConfirmedAt = params.ConfirmedAt
			}
			if params.RetryCount != nil {
				t.RetryCount = *params.RetryCount
			}
			if params.LastError != nil {
				// Empty-string maps to "clear the error" so callers can
				// signal a successful redispatch without resorting to NULL
				// over the wire.
				if *params.LastError == "" {
					t.LastError = nil
				} else {
					t.LastError = params.LastError
				}
			}
			updateTargetPhaseSummary(t, params)
		}
	}
	return nil
}

func (f *fakeStore) BumpTargetRetry(_ context.Context, eventID int64, deviceIdentifier string) error {
	f.bumpTargetRetryCalls++
	f.lastBumpTargetRetry = bumpRetryCall{EventID: eventID, DeviceIdentifier: deviceIdentifier}
	if f.bumpTargetRetryErr != nil {
		return f.bumpTargetRetryErr
	}
	for _, t := range f.targetsByEventID[eventID] {
		if t.DeviceIdentifier == deviceIdentifier {
			t.RetryCount++
			bumpTargetPhaseRetry(t)
			return nil
		}
	}
	return interfaces.ErrCurtailmentEventStateRaceLoss
}

func (f *fakeStore) UpsertHeartbeat(_ context.Context, params interfaces.UpsertCurtailmentHeartbeatParams) error {
	f.heartbeatCalls++
	f.lastHeartbeatActive = params.ActiveEventCount
	f.lastHeartbeatTickUUID = params.LastTickUUID
	return nil
}

// BeginRestoreTransition: real-fake behavior so enforceMaxDuration tests can
// assert the call happened and the event row flips to restoring in-place
// (mirroring SQL store semantics — the reconciler reads ev again on the next
// tick). effective_batch_size was stamped at Start; this fake does not touch it.
func (f *fakeStore) BeginRestoreTransition(_ context.Context, _ int64, eventUUID uuid.UUID, _ interfaces.BeginRestoreTransitionParams) (*models.Event, error) {
	f.beginRestoreCalls++
	f.beginRestoreLastEventID = eventUUID
	if f.beginRestoreErr != nil {
		return nil, f.beginRestoreErr
	}
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			ev.State = models.EventStateRestoring
			now := time.Now()
			for _, t := range f.targetsByEventID[ev.ID] {
				if t.State == models.TargetStateResolved ||
					t.State == models.TargetStateRestoreFailed ||
					t.State == models.TargetStateReleased {
					continue
				}
				t.DesiredState = models.DesiredStateActive
				t.State = models.TargetStatePending
				t.RetryCount = 0
				t.LastDispatchedAt = nil
				t.LastBatchUUID = nil
				t.ConfirmedAt = nil
				t.LastError = nil
				t.RestorePhase = &models.TargetPhaseSummary{
					Phase:     models.TargetPhaseRestore,
					State:     models.TargetStatePending,
					StartedAt: &now,
				}
			}
			return ev, nil
		}
	}
	return nil, nil
}

// BeginRecurtailTransition satisfies the store interface; the reconciler never
// calls it (the MQTT subscriber's watchdog drives re-curtail).
func (f *fakeStore) BeginRecurtailTransition(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	for _, ev := range f.events {
		if ev.EventUUID == eventUUID {
			ev.State = models.EventStatePending
			for _, t := range f.targetsByEventID[ev.ID] {
				if t.RestorePhase == nil {
					continue
				}
				t.DesiredState = models.DesiredStateCurtailed
				t.State = models.TargetStatePending
				t.RetryCount = 0
				t.LastDispatchedAt = nil
				t.LastBatchUUID = nil
				t.ConfirmedAt = nil
				t.LastError = nil
				t.CurtailPhase = models.TargetPhaseSummary{
					Phase: models.TargetPhaseCurtail,
					State: models.TargetStatePending,
				}
			}
			return ev, nil
		}
	}
	return nil, nil
}

func updateTargetPhaseSummary(t *models.Target, params interfaces.UpdateCurtailmentTargetStateParams) {
	desired := t.DesiredState
	if desired == "" && params.ExpectedDesiredState != nil {
		desired = *params.ExpectedDesiredState
	}
	var phase *models.TargetPhaseSummary
	switch desired {
	case models.DesiredStateCurtailed, "":
		if t.CurtailPhase.Phase == "" {
			t.CurtailPhase.Phase = models.TargetPhaseCurtail
		}
		if t.CurtailPhase.StartedAt == nil && !t.AddedAt.IsZero() {
			started := t.AddedAt
			t.CurtailPhase.StartedAt = &started
		}
		phase = &t.CurtailPhase
	case models.DesiredStateActive:
		if t.RestorePhase == nil {
			t.RestorePhase = &models.TargetPhaseSummary{Phase: models.TargetPhaseRestore}
		}
		phase = t.RestorePhase
	default:
		return
	}

	phase.State = params.State
	if params.LastDispatchedAt != nil {
		phase.DispatchedAt = params.LastDispatchedAt
	}
	if params.LastBatchUUID != nil {
		phase.BatchUUID = params.LastBatchUUID
	}
	if params.RetryCount != nil {
		phase.RetryCount = *params.RetryCount
	} else {
		phase.RetryCount = t.RetryCount
	}
	if params.LastError != nil && *params.LastError != "" {
		phase.FailureCount++
		phase.LastError = params.LastError
	}
	if phaseCompleted(params.State, desired) {
		if params.ConfirmedAt != nil {
			phase.CompletedAt = params.ConfirmedAt
			return
		}
		now := time.Now()
		phase.CompletedAt = &now
	}
}

func bumpTargetPhaseRetry(t *models.Target) {
	switch t.DesiredState {
	case models.DesiredStateActive:
		if t.RestorePhase == nil {
			t.RestorePhase = &models.TargetPhaseSummary{Phase: models.TargetPhaseRestore}
		}
		t.RestorePhase.RetryCount++
	default:
		t.CurtailPhase.RetryCount++
	}
}

func phaseCompleted(state models.TargetState, desired string) bool {
	switch desired {
	case models.DesiredStateActive:
		return state == models.TargetStateResolved ||
			state == models.TargetStateReleased ||
			state == models.TargetStateRestoreFailed
	default:
		return state == models.TargetStateConfirmed ||
			state == models.TargetStateReleased ||
			state == models.TargetStateRestoreFailed
	}
}

// fakeDispatcher records Curtail / Uncurtail calls and returns the
// configured outcome.
type fakeDispatcher struct {
	curtailErr              error
	curtailResultOverride   *command.CommandResult
	uncurtailErr            error
	uncurtailResultOverride *command.CommandResult
	curtailCalls            int
	curtailLastIDs          []string
	curtailCallIDs          [][]string
	curtailLastActor        session.Actor
	curtailLastSuppressed   bool
	uncurtailCalls          int
	uncurtailLastIDs        []string
	uncurtailLastSuppressed bool
	// curtailHook fires synchronously inside Curtail before the result is
	// returned. Tests use it to inspect store state at the moment the
	// command-service call happens (e.g., to verify the DISPATCHING
	// pre-write committed before the command issued).
	curtailHook func(ids []string)
	// uncurtailHook mirrors curtailHook for the restore path.
	uncurtailHook func(ids []string)
}

type fakeFanController struct {
	powers              []driver.PowerMode
	err                 *string
	waitForCancellation bool
}

func (f *fakeFanController) SetState(ctx context.Context, _ *models.Event, power driver.PowerMode) *string {
	f.powers = append(f.powers, power)
	if f.waitForCancellation {
		<-ctx.Done()
		message := "fan command timed out"
		return &message
	}
	return f.err
}

type fakeFanAlertEmitter struct {
	values []bool
}

func (f *fakeFanAlertEmitter) EmitCurtailmentFanRestoreFailure(_ context.Context, _ int64, _ string, failed bool) {
	f.values = append(f.values, failed)
}

func (f *fakeDispatcher) Curtail(ctx context.Context, selector *pb.DeviceSelector, _ sdk.CurtailLevel) (*command.CommandResult, error) {
	f.curtailCalls++
	f.curtailLastIDs = identifiersFromSelector(selector)
	f.curtailCallIDs = append(f.curtailCallIDs, append([]string(nil), f.curtailLastIDs...))
	if info, err := session.GetInfo(ctx); err == nil {
		f.curtailLastActor = info.Actor
	}
	f.curtailLastSuppressed = command.CommandActivitySuppressed(ctx)
	if f.curtailHook != nil {
		f.curtailHook(f.curtailLastIDs)
	}
	if f.curtailErr != nil {
		return nil, f.curtailErr
	}
	if f.curtailResultOverride != nil {
		return f.curtailResultOverride, nil
	}
	return &command.CommandResult{BatchIdentifier: "batch-curtail", DispatchedCount: len(f.curtailLastIDs), DispatchedDeviceIdentifiers: f.curtailLastIDs}, nil
}

func (f *fakeDispatcher) Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error) {
	f.uncurtailCalls++
	f.uncurtailLastIDs = identifiersFromSelector(selector)
	f.uncurtailLastSuppressed = command.CommandActivitySuppressed(ctx)
	if f.uncurtailHook != nil {
		f.uncurtailHook(f.uncurtailLastIDs)
	}
	if f.uncurtailErr != nil {
		return nil, f.uncurtailErr
	}
	if f.uncurtailResultOverride != nil {
		return f.uncurtailResultOverride, nil
	}
	return &command.CommandResult{BatchIdentifier: "batch-uncurtail", DispatchedCount: len(f.uncurtailLastIDs), DispatchedDeviceIdentifiers: f.uncurtailLastIDs}, nil
}

func identifiersFromSelector(selector *pb.DeviceSelector) []string {
	if selector == nil {
		return nil
	}
	if inc, ok := selector.SelectionType.(*pb.DeviceSelector_IncludeDevices); ok && inc.IncludeDevices != nil {
		return append([]string(nil), inc.IncludeDevices.DeviceIdentifiers...)
	}
	return nil
}

// --- helpers ---

func newReconcilerForTest(store *fakeStore, disp *fakeDispatcher) *Reconciler {
	r := New(Config{
		TickInterval:         time.Hour, // tests drive runTick directly
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		CurtailMaxRetries:    3,
		DriftThresholdFactor: 0.5,
	}, store, disp)
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	return r
}

func newReconcilerWithFansForTest(store *fakeStore, disp *fakeDispatcher, fans *fakeFanController) *Reconciler {
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		CurtailMaxRetries:    3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithFacilityFanController(fans))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	return r
}

func newReconcilerWithFanAlertForTest(
	store *fakeStore,
	disp *fakeDispatcher,
	fans *fakeFanController,
	alert *fakeFanAlertEmitter,
) *Reconciler {
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		CurtailMaxRetries:    3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithFacilityFanController(fans), WithFacilityFanAlertEmitter(alert))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	return r
}

func ptrFloat64(v float64) *float64 { return &v }

// --- tests ---

func TestReconciler_PendingDispatchesCurtail(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(2)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &effBatch, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// One Curtail call covers the bounded initial batch.
	assert.Equal(t, 1, disp.curtailCalls)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, disp.curtailLastIDs)
	assert.Equal(t, session.ActorCurtailment, disp.curtailLastActor)
	assert.True(t, disp.curtailLastSuppressed, "curtailment-owned batches must suppress command activity")

	// Both targets transitioned to dispatched.
	require.Len(t, store.targetsByEventID[eventID], 2)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)
	require.NotNil(t, store.targetsByEventID[eventID][0].LastBatchUUID)
	require.NotNil(t, store.targetsByEventID[eventID][1].LastBatchUUID)
	assert.Equal(t, *store.targetsByEventID[eventID][0].LastBatchUUID, *store.targetsByEventID[eventID][1].LastBatchUUID,
		"batched Curtail targets must share one command batch UUID")

	// Heartbeat upserted once.
	assert.Equal(t, 1, store.heartbeatCalls)
	assert.Equal(t, int32(1), store.lastHeartbeatActive)
}

func TestReconciler_PendingDispatchesAllTargetsInEffectiveBatches(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(2)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &effBatch, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-3", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 2, disp.curtailCalls)
	require.Len(t, disp.curtailCallIDs, 2)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, disp.curtailCallIDs[0])
	assert.ElementsMatch(t, []string{"miner-3"}, disp.curtailCallIDs[1])
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][2].State)
}

func TestReconciler_PendingDispatchesLargeEventInBoundedBatches(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(100)
	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &effBatch, EffectiveBatchSize: &effBatch},
	}
	for i := range 205 {
		store.targetsByEventID[eventID] = append(store.targetsByEventID[eventID], &models.Target{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   fmt.Sprintf("miner-%03d", i),
			State:              models.TargetStatePending,
			BaselinePowerW:     ptrFloat64(3000),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 3, disp.curtailCalls)
	require.Len(t, disp.curtailCallIDs, 3)
	assert.Len(t, disp.curtailCallIDs[0], 100)
	assert.Len(t, disp.curtailCallIDs[1], 100)
	assert.Len(t, disp.curtailCallIDs[2], 5)
	for _, target := range store.targetsByEventID[eventID] {
		assert.Equal(t, models.TargetStateDispatched, target.State)
	}
}

func TestReconciler_PendingClosedLoopFullFleetWithoutTargetsMarksActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:        eventID,
			EventUUID: eventUUID,
			OrgID:     1,
			State:     models.EventStatePending,
			Mode:      models.ModeFullFleet,
			LoopType:  models.LoopTypeClosed,
			ScopeType: models.ScopeTypeWholeOrg,
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID],
		"empty closed-loop full_fleet pending event should become an active watcher")
}

func TestReconciler_ActiveClosedLoopFullFleetAdmitsAndDispatchesNewTarget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:              eventID,
			EventUUID:       eventUUID,
			OrgID:           1,
			State:           models.EventStateActive,
			Mode:            models.ModeFullFleet,
			LoopType:        models.LoopTypeClosed,
			ScopeType:       models.ScopeTypeWholeOrg,
			CreatedByUserID: 99,
		},
	}
	driver := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-new",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(100),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.claimTargetsCalls)
	require.Len(t, store.targetsByEventID[eventID], 1)
	target := store.targetsByEventID[eventID][0]
	assert.Equal(t, "miner-new", target.DeviceIdentifier)
	assert.Nil(t, target.BaselinePowerW, "below-floor full_fleet target should use hash confirmation fallback")
	assert.Equal(t, 1, disp.curtailCalls)
	assert.ElementsMatch(t, []string{"miner-new"}, disp.curtailLastIDs)
	assert.Equal(t, models.TargetStateDispatched, target.State)
}

func TestReconciler_ActiveClosedLoopFullFleetSkipsCandidateScanWhenAdmissionIntervalBlocked(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	batchSize := int32(1)
	lastBatchAt := time.Now()
	lastBatchUUID := "batch-recent"
	store.events = []*models.Event{
		{
			ID:                      eventID,
			EventUUID:               eventUUID,
			OrgID:                   1,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFullFleet,
			LoopType:                models.LoopTypeClosed,
			ScopeType:               models.ScopeTypeWholeOrg,
			CurtailBatchSize:        &batchSize,
			CurtailBatchIntervalSec: 600,
			CreatedByUserID:         99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "recently-dispatched",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
			LastDispatchedAt:   &lastBatchAt,
			LastBatchUUID:      &lastBatchUUID,
			CurtailPhase: models.TargetPhaseSummary{
				Phase:        models.TargetPhaseCurtail,
				State:        models.TargetStateConfirmed,
				DispatchedAt: &lastBatchAt,
				BatchUUID:    &lastBatchUUID,
			},
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-new", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.listCandidatesCalls,
		"tick may read existing-target telemetry, but admission interval gate should prevent a second fleet candidate scan")
	assert.Equal(t, 0, store.claimTargetsCalls)
	assert.Equal(t, 0, disp.curtailCalls)
}

func TestReconciler_ActiveAllPairedPolicySkipsAdmissionScanWhenIntervalBlocked(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	batchSize := int32(1)
	lastBatchAt := time.Now()
	lastBatchUUID := "batch-recent"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CurtailBatchSize:            &batchSize,
			CurtailBatchIntervalSec:     600,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "recently-dispatched",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
			LastDispatchedAt:   &lastBatchAt,
			LastBatchUUID:      &lastBatchUUID,
			CurtailPhase: models.TargetPhaseSummary{
				Phase:        models.TargetPhaseCurtail,
				State:        models.TargetStateConfirmed,
				DispatchedAt: &lastBatchAt,
				BatchUUID:    &lastBatchUUID,
			},
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "recently-dispatched", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(100)},
		{DeviceIdentifier: "miner-new", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.listCandidatesCalls,
		"drift observation may read existing-target telemetry, but the interval gate must prevent the fleet-wide admission scan")
	assert.Equal(t, 0, store.claimAllPairedCalls)
	assert.Equal(t, 0, disp.curtailCalls)
}

func TestReconciler_ActiveAllPairedPolicyClaimsDispatchableAndUnavailableTargets(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.activeDevices = []string{"owned-elsewhere"}
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "online", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
		{DeviceIdentifier: "offline", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(0)},
		{DeviceIdentifier: "auth-needed", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "AUTHENTICATION_NEEDED"},
		{DeviceIdentifier: "unpaired", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "UNPAIRED"},
		{DeviceIdentifier: "owned-elsewhere", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.claimAllPairedCalls)
	assert.Equal(t, 0, store.claimTargetsCalls)
	assert.Equal(t, 0, disp.curtailCalls, "all-paired admission dispatches on a later pending-target pass")
	require.Len(t, store.targetsByEventID[eventID], 3)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateUnavailable, store.targetsByEventID[eventID][1].State)
	require.NotNil(t, store.targetsByEventID[eventID][1].LastError)
	assert.Equal(t, "offline", *store.targetsByEventID[eventID][1].LastError)
	assert.Equal(t, models.TargetStateUnavailable, store.targetsByEventID[eventID][2].State)
	require.NotNil(t, store.targetsByEventID[eventID][2].LastError)
	assert.Equal(t, "authentication_needed", *store.targetsByEventID[eventID][2].LastError)
}

func TestReconciler_AllPairedPolicyUnavailableTargetBecomesPendingAndDispatches(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 1, disp.curtailCalls)
	assert.ElementsMatch(t, []string{"miner-1"}, disp.curtailLastIDs)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Nil(t, final.LastError)
	require.NotNil(t, final.BaselinePowerW,
		"promotion must backfill the missing pre-curtail baseline so confirm/drift checks don't fall back to hash-only")
	assert.InDelta(t, 3000.0, *final.BaselinePowerW, 0.001)
}

// A pool-less miner parked unavailable by the pre-#663 classifier (or by a
// transient non-actionable status) is promoted, baseline-backfilled, and
// dispatched once the classifier sees NEEDS_MINING_POOL as commandable —
// existing all-paired events self-heal without migration.
func TestReconciler_AllPairedPolicyParkedPoolLessTargetPromotedAndDispatched(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	nonActionableReason := "non_actionable_status"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			DecisionSnapshotJSON:        []byte(`{"candidate_min_power_w":1500}`),
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "pool-less",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &nonActionableReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "pool-less", DriverName: &driver, DeviceStatus: "NEEDS_MINING_POOL", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(2000), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 1, disp.curtailCalls)
	assert.ElementsMatch(t, []string{"pool-less"}, disp.curtailLastIDs)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Nil(t, final.LastError)
	require.NotNil(t, final.BaselinePowerW,
		"promotion must backfill the idle-draw baseline; hash-only fallback cannot confirm curtail/restore for a never-hashing miner")
	assert.InDelta(t, 2000.0, *final.BaselinePowerW, 0.001)
}

// A never-hashing miner's idle draw is usually below candidate_min_power_w,
// but its baseline must still be persisted: the hash-only fallback the floor
// relies on cannot confirm curtail (hash is already 0 → instant false
// positive) or restore (hash never rises → ages out to restore_failed) for a
// miner that never hashes. The floor applies only to hashing miners.
func TestReconciler_AllPairedPolicyPoolLessPromotionPersistsBelowFloorBaseline(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	nonActionableReason := "non_actionable_status"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			// Real events stamp the floor into the decision snapshot; the
			// readiness-refresh path reads it back from here.
			DecisionSnapshotJSON: []byte(`{"candidate_min_power_w":1500}`),
			CreatedByUserID:      99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "pool-less",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &nonActionableReason,
		},
	}
	driver := "antminer"
	// 400 W idle draw: below the 1500 W candidate_min_power_w floor.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "pool-less", DriverName: &driver, DeviceStatus: "NEEDS_MINING_POOL", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(400), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 1, disp.curtailCalls)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	require.NotNil(t, final.BaselinePowerW,
		"non-hashing miners must persist any positive baseline; the min-power floor only makes sense where the hash-only fallback works")
	assert.InDelta(t, 400.0, *final.BaselinePowerW, 0.001)
}

// A readiness flip the bulk UPDATE skips (row advanced concurrently, so it is
// absent from RETURNING) must not be mirrored in memory: an optimistic mirror
// would feed the same-tick dispatch pass and re-issue a duplicate Curtail
// against a row another actor already advanced.
func TestReconciler_AllPairedPolicyRefreshSkippedRowNotMirroredOrDispatched(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
	}
	// The SQL guards skip miner-1 (concurrently-advanced row): no mutation,
	// no RETURNING entry.
	store.bulkRefreshSkipDevices = map[string]bool{"miner-1": true}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	assert.Equal(t, 1, store.bulkRefreshCalls)
	assert.Equal(t, 0, disp.curtailCalls,
		"a skipped readiness flip must not feed the same-tick dispatch pass")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateUnavailable, final.State,
		"in-memory mirror must not advance for rows the bulk UPDATE skipped")
	assert.GreaterOrEqual(t, metrics.EventStateRaceLossCount(), 1,
		"partial apply must surface as a race-loss metric so sustained races are visible")
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(),
		"a skipped row is benign concurrency, not a write failure")
}

// A row promoted to pending while its telemetry was still missing carries no
// pre-curtail baseline. Later ticks with no state flip must keep offering the
// backfill once telemetry qualifies; otherwise the promotion tick is the only
// attempt and confirm/drift checks degrade to hash-only for the row's life.
func TestReconciler_AllPairedPolicyStablePendingTargetBackfillsMissingBaseline(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	// Already pending (promoted on an earlier tick while telemetry was
	// missing), baseline never captured.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Len(t, store.lastBulkRefreshUpdates, 1,
		"a stable pending row with a missing baseline must still get a backfill update")
	require.NotNil(t, store.lastBulkRefreshUpdates[0].BaselinePowerW)
	final := store.targetsByEventID[eventID][0]
	require.NotNil(t, final.BaselinePowerW,
		"late backfill must land once telemetry qualifies")
	assert.InDelta(t, 3000.0, *final.BaselinePowerW, 0.001)
	assert.Equal(t, models.TargetStateDispatched, final.State,
		"the pending row still dispatches this tick")
}

func TestReconciler_AllPairedPolicyPendingTargetBecomesUnavailableWhenOffline(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateUnavailable, final.State)
	require.NotNil(t, final.LastError)
	assert.Equal(t, "offline", *final.LastError)
}

func TestReconciler_AllPairedPolicyUnavailableTargetReleasedWhenNoLongerPairedLike(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "unpaired",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "vanished",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		// "unpaired" is still a candidate row but no longer paired-like;
		// "vanished" has no candidate row at all (deleted device).
		{DeviceIdentifier: "unpaired", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "UNPAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	for _, target := range store.targetsByEventID[eventID] {
		assert.Equal(t, models.TargetStateReleased, target.State, target.DeviceIdentifier)
		require.NotNil(t, target.LastError, target.DeviceIdentifier)
		assert.Equal(t, "released: device is no longer paired-like", *target.LastError, target.DeviceIdentifier)
	}
}

func TestAllPairedPolicyRefreshDeviceIdentifiersOnlyIncludesRefreshableTargets(t *testing.T) {
	t.Parallel()

	targets := []*models.Target{
		{DeviceIdentifier: "pending", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed},
		{DeviceIdentifier: "unavailable", State: models.TargetStateUnavailable, DesiredState: models.DesiredStateCurtailed},
		{DeviceIdentifier: "confirmed", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
		{DeviceIdentifier: "released", State: models.TargetStateReleased, DesiredState: models.DesiredStateCurtailed},
		{DeviceIdentifier: "restore-pending", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
		nil,
		{State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed},
	}

	assert.Equal(t, []string{"pending", "unavailable"}, allPairedPolicyRefreshDeviceIdentifiers(targets))
}

func TestReconciler_AllPairedPolicyRefreshQueriesOnlyRefreshableTargets(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "needs-refresh",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "confirmed",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "released",
			State:              models.TargetStateReleased,
			DesiredState:       models.DesiredStateCurtailed,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "restore-pending",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateActive,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "needs-refresh", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
		{DeviceIdentifier: "confirmed", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED"},
		{DeviceIdentifier: "released", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED"},
		{DeviceIdentifier: "restore-pending", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// First call is the device-scoped readiness refresh; the second is the
	// pending-phase admission scan (fleet-wide, no device filter).
	require.Len(t, store.listCandidatesFilters, 2)
	assert.Equal(t, []string{"needs-refresh"}, store.listCandidatesFilters[0],
		"readiness refresh must query only pending/unavailable curtailed targets")
	assert.Empty(t, store.listCandidatesFilters[1])
	assert.Equal(t, 0, disp.curtailCalls)
}

func TestReconciler_PendingAllPairedPolicyUnavailableTargetsDoNotBlockActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeOpen,
			ScopeType:                   models.ScopeTypeDeviceList,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "confirmed",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "offline",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "confirmed", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(200)},
		{DeviceIdentifier: "offline", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
	assert.Equal(t, models.EventStateActive, store.events[0].State)
	assert.Equal(t, 0, disp.curtailCalls)
}

// A bounded all-paired event whose entire scope is non-commandable must hold
// in pending: transitioning to Active would stamp StartedAt and start
// enforceMaxDuration's clock while nothing is curtailed, letting the event
// burn its bounded window — then force-restore, releasing every
// never-dispatched row — without a single dispatch having happened.
func TestReconciler_PendingAllPairedPolicyAllUnavailableStaysPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "offline-1",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "offline-2",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "offline-1", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
		{DeviceIdentifier: "offline-2", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, models.EventStatePending, store.events[0].State,
		"all-unavailable policy event must hold in pending until something confirms")
	assert.NotContains(t, store.updateEventLast, eventID)
	assert.Equal(t, 0, disp.curtailCalls)
}

// A pending all-paired event whose every row is a released policy placeholder
// (e.g. the whole scope unpaired between admission ticks) must also hold:
// released rows are reopenable, so flipping to Active would stamp StartedAt
// and burn the bounded window as an empty watcher with nothing curtailed.
func TestReconciler_PendingAllPairedPolicyAllReleasedStaysPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	eventID := int64(10)
	eventUUID := uuid.New()
	releasedReason := "released: device is no longer paired-like"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
			// Fresh event: held, but not yet stalled long enough to warn.
			CreatedAt: time.Date(2026, 5, 7, 11, 59, 0, 0, time.UTC),
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "unpaired-1",
			State:              models.TargetStateReleased,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &releasedReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "unpaired-2",
			State:              models.TargetStateReleased,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &releasedReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		// Still unpaired: nothing for admission to reopen this tick.
		{DeviceIdentifier: "unpaired-1", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "UNPAIRED"},
		{DeviceIdentifier: "unpaired-2", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "UNPAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	assert.Equal(t, models.EventStatePending, store.events[0].State,
		"all-released policy event must hold in pending; released rows are reopenable placeholders")
	assert.NotContains(t, store.updateEventLast, eventID)
	assert.Equal(t, 0, disp.curtailCalls)
	assert.Equal(t, 0, metrics.AllPairedPendingStallCount(),
		"a freshly created hold must not count as stalled")
}

// A held pending all-paired event older than the stall threshold must emit
// the stall metric and warning each tick: the hold blocks every other
// curtailment start for the scope, so a sustained stall (fleet-wide outage)
// needs a dashboard signal, not just a paused UI.
func TestReconciler_PendingAllPairedPolicyStallEmitsMetricAfterThreshold(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
			// Pending for an hour against the fixed test clock (12:00).
			CreatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC),
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "offline-1",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "offline-1", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	assert.Equal(t, models.EventStatePending, store.events[0].State)
	assert.Equal(t, 1, metrics.AllPairedPendingStallCount(),
		"a hold past the stall threshold must surface on the stall counter")
}

// Readiness flips are applied through one bulk statement per tick; a mass
// readiness change must not become one UPDATE round trip per device.
func TestReconciler_AllPairedPolicyReadinessRefreshBatchesFlipsIntoOneCall(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	offlineReason := "offline"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "wakes-1",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "wakes-2",
			State:              models.TargetStateUnavailable,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &offlineReason,
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "sleeps",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "wakes-1", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
		{DeviceIdentifier: "wakes-2", DriverName: &driver, DeviceStatus: "ACTIVE", PairingStatus: "PAIRED", LatestPowerW: ptrFloat64(3000)},
		{DeviceIdentifier: "sleeps", DriverName: &driver, DeviceStatus: "OFFLINE", PairingStatus: "PAIRED"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.bulkRefreshCalls, "one bulk statement per tick, not one write per device")
	require.Len(t, store.lastBulkRefreshUpdates, 3)
	byDevice := map[string]*models.Target{}
	for _, target := range store.targetsByEventID[eventID] {
		byDevice[target.DeviceIdentifier] = target
	}
	assert.Equal(t, models.TargetStateDispatched, byDevice["wakes-1"].State, "promoted rows dispatch in the same tick")
	assert.Equal(t, models.TargetStateDispatched, byDevice["wakes-2"].State)
	assert.Equal(t, models.TargetStateUnavailable, byDevice["sleeps"].State)
	require.NotNil(t, byDevice["sleeps"].LastError)
	assert.Equal(t, "offline", *byDevice["sleeps"].LastError)
}

// Durable ownership must not pause while an all-paired event is pending
// (e.g. immediately after a recurtail transition): released policy rows are
// reopened by the pending-phase admission pass, then dispatched by the next
// tick's pending/active dispatch pass.
func TestReconciler_PendingAllPairedPolicyReleasedTargetsReopenWhilePending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	releasedReason := "released without restore: no curtail command dispatched"
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			Mode:                        models.ModeFullFleet,
			LoopType:                    models.LoopTypeClosed,
			ScopeType:                   models.ScopeTypeWholeOrg,
			ForceIncludeAllPairedMiners: true,
			CreatedByUserID:             99,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "confirmed",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "released-policy-row",
			State:              models.TargetStateReleased,
			DesiredState:       models.DesiredStateCurtailed,
			LastError:          &releasedReason,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "confirmed",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestPowerW:     ptrFloat64(100),
			LatestHashRateHS: ptrFloat64(0),
		},
		{
			DeviceIdentifier: "released-policy-row",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestPowerW:     ptrFloat64(3000),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
	assert.Equal(t, models.EventStateActive, store.events[0].State)
	assert.Equal(t, 1, store.claimAllPairedCalls, "pending-phase admission must reopen released policy rows")
	require.Len(t, store.claimedAllPairedParams, 1)
	assert.Equal(t, "released-policy-row", store.claimedAllPairedParams[0].DeviceIdentifier)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][1].State)
	assert.Nil(t, store.targetsByEventID[eventID][1].LastError)
	assert.Equal(t, 0, disp.curtailCalls, "reopened rows dispatch on a later pending pass, not the claiming tick")

	r.runTick(context.Background())

	assert.Equal(t, 1, store.claimAllPairedCalls,
		"no re-claim once every device holds a non-released row")
	require.Equal(t, 1, disp.curtailCalls, "reopened row dispatches on the following tick")
	assert.ElementsMatch(t, []string{"released-policy-row"}, disp.curtailLastIDs)
}

func TestReconciler_ActiveClosedLoopFullFleetUsesPersistedCandidateFloor(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.orgConfig = &models.OrgConfig{OrgID: 1, CandidateMinPowerW: 1500}
	store.events = []*models.Event{
		{
			ID:                   eventID,
			EventUUID:            eventUUID,
			OrgID:                1,
			State:                models.EventStateActive,
			Mode:                 models.ModeFullFleet,
			LoopType:             models.LoopTypeClosed,
			ScopeType:            models.ScopeTypeWholeOrg,
			DecisionSnapshotJSON: []byte(`{"candidate_min_power_w":500}`),
			CreatedByUserID:      99,
		},
	}
	driver := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-low",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(800),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Len(t, store.targetsByEventID[eventID], 1)
	target := store.targetsByEventID[eventID][0]
	require.NotNil(t, target.BaselinePowerW,
		"dynamic admission must use the floor resolved at Start, not a later org default")
	assert.Equal(t, 800.0, *target.BaselinePowerW)
}

func TestReconciler_ActiveClosedLoopFullFleetSkipsCooldownDevices(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.cooldownDevices = []string{"miner-recent"}
	store.events = []*models.Event{
		{
			ID:                   eventID,
			EventUUID:            eventUUID,
			OrgID:                1,
			State:                models.EventStateActive,
			Mode:                 models.ModeFullFleet,
			LoopType:             models.LoopTypeClosed,
			ScopeType:            models.ScopeTypeWholeOrg,
			DecisionSnapshotJSON: []byte(`{"post_event_cooldown_sec":600}`),
			CreatedByUserID:      99,
		},
	}
	driver := "antminer"
	now := time.Now()
	for _, id := range []string{"miner-recent", "miner-fresh"} {
		store.candidates = append(store.candidates, &models.Candidate{
			DeviceIdentifier: id,
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.cooldownCalls)
	assert.Equal(t, int64(1), store.lastCooldownOrgID)
	assert.Equal(t, int32(600), store.lastCooldownSec)
	require.Len(t, store.claimedTargetParams, 1)
	assert.Equal(t, "miner-fresh", store.claimedTargetParams[0].DeviceIdentifier)
	assert.ElementsMatch(t, []string{"miner-fresh"}, disp.curtailLastIDs)
}

func TestReconciler_ActiveClosedLoopFullFleetClaimsOnlyOneCurtailBatch(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	batchSize := int32(2)
	store.events = []*models.Event{
		{
			ID:               eventID,
			EventUUID:        eventUUID,
			OrgID:            1,
			State:            models.EventStateActive,
			Mode:             models.ModeFullFleet,
			LoopType:         models.LoopTypeClosed,
			ScopeType:        models.ScopeTypeWholeOrg,
			CurtailBatchSize: &batchSize,
			CreatedByUserID:  99,
		},
	}
	driver := "antminer"
	now := time.Now()
	for _, id := range []string{"miner-a", "miner-b", "miner-c"} {
		store.candidates = append(store.candidates, &models.Candidate{
			DeviceIdentifier: id,
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.claimTargetsCalls)
	require.Len(t, store.claimedTargetParams, 2,
		"dynamic claims must respect curtail_batch_size before inserting DISPATCHING rows")
	assert.Equal(t, 1, disp.curtailCalls)
	assert.Len(t, disp.curtailLastIDs, 2)
	assert.Len(t, store.targetsByEventID[eventID], 2)
}

func TestReconciler_EmptyClosedLoopFullFleetWatcherUsesPersistedCurtailBatchFloor(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	curtailBatchSize := int32(10)
	store.events = []*models.Event{
		{
			ID:               eventID,
			EventUUID:        eventUUID,
			OrgID:            1,
			State:            models.EventStateActive,
			Mode:             models.ModeFullFleet,
			LoopType:         models.LoopTypeClosed,
			ScopeType:        models.ScopeTypeWholeOrg,
			CurtailBatchSize: &curtailBatchSize,
			CreatedByUserID:  99,
		},
	}
	driver := "antminer"
	now := time.Now()
	for i := range 12 {
		store.candidates = append(store.candidates, &models.Candidate{
			DeviceIdentifier: fmt.Sprintf("miner-%02d", i),
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.claimTargetsCalls)
	require.Len(t, store.claimedTargetParams, int(curtailBatchSize),
		"empty watchers must not admit every later-eligible miner in one tick")
	assert.Len(t, disp.curtailLastIDs, int(curtailBatchSize))
	assert.Len(t, store.targetsByEventID[eventID], int(curtailBatchSize))
}

func TestReconciler_ActiveClosedLoopFullFleetSkipsConflictsBeforeBatchLimit(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	batchSize := int32(2)
	store.events = []*models.Event{
		{
			ID:               eventID,
			EventUUID:        eventUUID,
			OrgID:            1,
			State:            models.EventStateActive,
			Mode:             models.ModeFullFleet,
			LoopType:         models.LoopTypeClosed,
			ScopeType:        models.ScopeTypeWholeOrg,
			CurtailBatchSize: &batchSize,
			CreatedByUserID:  99,
		},
	}
	driver := "antminer"
	now := time.Now()
	for _, id := range []string{"conflict-a", "conflict-b", "miner-c", "miner-d"} {
		store.candidates = append(store.candidates, &models.Candidate{
			DeviceIdentifier: id,
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		})
	}
	store.activeDevices = []string{"conflict-a", "conflict-b"}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Len(t, store.claimedTargetParams, 2)
	assert.Equal(t, "miner-c", store.claimedTargetParams[0].DeviceIdentifier)
	assert.Equal(t, "miner-d", store.claimedTargetParams[1].DeviceIdentifier)
	assert.ElementsMatch(t, []string{"miner-c", "miner-d"}, disp.curtailLastIDs)
}

func TestReconciler_ActiveClosedLoopFullFleetUsesPersistedSiteScope(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	siteID := int64(77)
	store.events = []*models.Event{
		{
			ID:                   eventID,
			EventUUID:            eventUUID,
			OrgID:                1,
			State:                models.EventStateActive,
			Mode:                 models.ModeFullFleet,
			LoopType:             models.LoopTypeClosed,
			ScopeType:            models.ScopeTypeSite,
			ScopeJSON:            []byte(`{"site_id":77}`),
			DecisionSnapshotJSON: []byte(`{"post_event_cooldown_sec":600}`),
			CreatedByUserID:      99,
		},
	}
	driver := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "site-miner",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, []int64{siteID}, store.lastListCandidatesSiteIDs)
	assert.Equal(t, 1, store.cooldownCalls)
	assert.Equal(t, []int64{siteID}, store.lastCooldownSiteIDs)
	assert.ElementsMatch(t, []string{"site-miner"}, disp.curtailLastIDs)
}

func TestReconciler_ActiveClosedLoopFullFleetUsesPersistedMultiSiteScope(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	siteIDs := []int64{77, 88}
	store.events = []*models.Event{
		{
			ID:                   eventID,
			EventUUID:            eventUUID,
			OrgID:                1,
			State:                models.EventStateActive,
			Mode:                 models.ModeFullFleet,
			LoopType:             models.LoopTypeClosed,
			ScopeType:            models.ScopeTypeMixed,
			ScopeJSON:            []byte(`{"site_ids":[77,88],"device_identifiers":null}`),
			DecisionSnapshotJSON: []byte(`{"post_event_cooldown_sec":600}`),
			CreatedByUserID:      99,
		},
	}
	driver := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "site-miner",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, siteIDs, store.lastListCandidatesSiteIDs)
	assert.Equal(t, 1, store.cooldownCalls)
	assert.Equal(t, siteIDs, store.lastCooldownSiteIDs)
	assert.Equal(t, 1, store.claimTargetsCalls)
	assert.ElementsMatch(t, []string{"site-miner"}, disp.curtailLastIDs)
}

func TestReconciler_DispatchingFailureDoesNotRetryAgainAsPendingInSameTick(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue unavailable")}

	eventID := int64(10)
	eventUUID := uuid.New()
	effBatch := int32(3)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-orphan",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"failed DISPATCHING recovery must not be re-claimed by the same tick's PENDING pass")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatching, final.State,
		"failed DISPATCHING recovery must remain DISPATCHING for the next tick")
	assert.Equal(t, int32(1), final.RetryCount,
		"failed DISPATCHING recovery must consume only one retry slot per tick")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "queue unavailable")
}

func TestReconciler_CurtailBatchRecordsSkippedAndNotEnqueuedFailures(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{
			BatchIdentifier:             "batch-partial",
			DispatchedDeviceIdentifiers: []string{"miner-dispatched"},
			Skipped: []command.SkippedDevice{
				{DeviceIdentifier: "miner-skipped", Reason: "maintenance mode"},
			},
		},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	effBatch := int32(3)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-dispatched", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-skipped", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-missing", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls)
	byID := map[string]*models.Target{}
	for _, target := range store.targetsByEventID[eventID] {
		byID[target.DeviceIdentifier] = target
	}

	dispatched := byID["miner-dispatched"]
	require.NotNil(t, dispatched)
	assert.Equal(t, models.TargetStateDispatched, dispatched.State)
	require.NotNil(t, dispatched.LastBatchUUID)
	assert.Equal(t, "batch-partial", *dispatched.LastBatchUUID)

	skipped := byID["miner-skipped"]
	require.NotNil(t, skipped)
	assert.Equal(t, models.TargetStatePending, skipped.State)
	assert.Equal(t, int32(1), skipped.RetryCount)
	require.NotNil(t, skipped.LastError)
	assert.Equal(t, "maintenance mode", *skipped.LastError)

	missing := byID["miner-missing"]
	require.NotNil(t, missing)
	assert.Equal(t, models.TargetStatePending, missing.State)
	assert.Equal(t, int32(1), missing.RetryCount)
	require.NotNil(t, missing.LastError)
	assert.Equal(t, "curtail command did not enqueue device", *missing.LastError)
}

func TestReconciler_TargetPhaseSummariesCaptureCurtailAndRestoreCycle(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	effBatch := int32(1)
	eventID := int64(10)
	eventUUID := uuid.New()
	addedAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	store.events = []*models.Event{
		{
			ID:                 eventID,
			EventUUID:          eventUUID,
			OrgID:              1,
			State:              models.EventStatePending,
			EffectiveBatchSize: &effBatch,
		},
	}
	baseline := float64(3000)
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     &baseline,
			AddedAt:            addedAt,
		},
	}
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-1",
			LatestPowerW:     ptrFloat64(0),
			LatestHashRateHS: ptrFloat64(0),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	target := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.EventStateActive, store.events[0].State)
	assert.Equal(t, models.TargetStateConfirmed, target.CurtailPhase.State)
	require.NotNil(t, target.CurtailPhase.DispatchedAt)
	require.NotNil(t, target.CurtailPhase.BatchUUID)
	assert.Equal(t, "batch-curtail", *target.CurtailPhase.BatchUUID)
	require.NotNil(t, target.CurtailPhase.CompletedAt)
	assert.Equal(t, int32(0), target.CurtailPhase.FailureCount)

	_, err := store.BeginRestoreTransition(context.Background(), 1, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-1",
			LatestPowerW:     ptrFloat64(3100),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r.runTick(context.Background())
	target = store.targetsByEventID[eventID][0]
	require.NotNil(t, target.RestorePhase)
	assert.Equal(t, models.TargetStateDispatched, target.RestorePhase.State)
	require.NotNil(t, target.RestorePhase.DispatchedAt)
	require.NotNil(t, target.RestorePhase.BatchUUID)
	assert.Equal(t, "batch-uncurtail", *target.RestorePhase.BatchUUID)

	r.runTick(context.Background())
	target = store.targetsByEventID[eventID][0]
	require.NotNil(t, target.RestorePhase)
	assert.Equal(t, models.EventStateCompleted, store.events[0].State)
	assert.Equal(t, models.TargetStateResolved, target.RestorePhase.State)
	require.NotNil(t, target.RestorePhase.CompletedAt)
	assert.Equal(t, int32(0), target.RestorePhase.FailureCount)
}

func TestReconciler_SkipsCurtailDispatchWhenEventTerminatesBeforeCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	curtailBatchSize := int32(1)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &curtailBatchSize},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}
	store.listTargetsHook = func(context.Context, uuid.UUID) {
		store.events[0].State = models.EventStateCancelled
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls)
	assert.Equal(t, 0, store.updateTargetCalls)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
}

// A concurrent AdminTerminate that lands between target N and N+1 must
// stop further Curtail commands — the EXISTS guard on the DISPATCHING
// pre-write catches the race.
func TestReconciler_SkipsRemainingCurtailDispatchesWhenEventTerminatesMidLoop(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	curtailBatchSize := int32(1)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &curtailBatchSize},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-3", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}
	// Target 1's DISPATCHING pre-write (call 1) succeeds; the hook returns
	// the race-loss sentinel on the post-command DISPATCHED write (call 2)
	// and every subsequent DISPATCHING pre-write thereafter, simulating an
	// AdminTerminate that committed mid-loop. Only target 1's Curtail can
	// fire — its DISPATCHING write landed before the race.
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call >= 2 {
			return interfaces.ErrCurtailmentEventStateRaceLoss
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"only the first target should dispatch; the rest skip after the event flipped")
}

// On a typed race-loss return, the reconciler increments
// IncEventStateRaceLoss and does NOT advance the in-memory mirror — that
// keeps the mirror consistent with persisted state on a silent SQL no-op.
func TestReconciler_TargetStateRaceLoss_LogsAndMetersWithoutMirrorAdvance(t *testing.T) {
	store := newFakeStore()
	store.updateTargetStateErr = interfaces.ErrCurtailmentEventStateRaceLoss
	disp := &fakeDispatcher{}
	metrics := &recordingMetrics{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	// The store's UpdateTargetState returned the sentinel — mirror update
	// must be skipped, so the in-memory state stays at PENDING.
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State,
		"in-memory mirror must NOT advance when the store reports race-loss")
	// Race-loss surfaces as IncEventStateRaceLoss, not IncTickFailure.
	assert.GreaterOrEqual(t, metrics.EventStateRaceLossCount(), 1,
		"race-loss sentinel must increment IncEventStateRaceLoss")
	// Race-loss is benign concurrency — IncTargetWriteFailure stays at 0
	// so the "degraded write path" alert doesn't trip on routine Stop /
	// AdminTerminate landings.
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(),
		"race-loss must NOT count as a target-write failure; the two counters are operationally distinct signals")
}

// A non-race-loss target-state write failure increments
// IncTargetWriteFailure so dashboards can detect "heartbeat fresh but
// writes failing" outages.
func TestReconciler_TargetWriteFailure_IncrementsCounter(t *testing.T) {
	store := newFakeStore()
	store.updateTargetStateErr = errors.New("connection refused")
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.metrics = metrics
	r.runTick(context.Background())

	assert.GreaterOrEqual(t, metrics.TargetWriteFailureCount(), 1,
		"non-race-loss target-write failure must increment IncTargetWriteFailure")
	assert.Equal(t, 0, metrics.EventStateRaceLossCount(),
		"non-race-loss failure must NOT increment IncEventStateRaceLoss (different signal class)")
}

// dispatchOneCurtail must commit DISPATCHING before calling cmd.Curtail
// so AdminTerminate's in-flight gate observes it and rejects.
func TestReconciler_DispatchingPreWrite_CommitsBeforeCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	// Capture the in-store target state at the moment cmd.Curtail is invoked.
	var stateAtCommandTime models.TargetState
	disp.curtailHook = func(_ []string) {
		for _, target := range store.targetsByEventID[eventID] {
			if target.DeviceIdentifier == "miner-1" {
				stateAtCommandTime = target.State
				return
			}
		}
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, models.TargetStateDispatching, stateAtCommandTime,
		"target must be DISPATCHING at the moment cmd.Curtail is called so a concurrent AdminTerminate's in-flight gate observes it")
	// After the command returns successfully, the target advances to
	// DISPATCHED — the DISPATCHING window only exists during the command call.
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// A row-specific persistent write failure on the DISPATCHING pre-write
// (non-race-loss) must burn a retry slot via recordDispatchFailure so a
// stuck target eventually escalates to terminal — otherwise the event
// stalls indefinitely. Mirrors the analogous restore-path coverage.
func TestReconciler_CurtailPreWriteFailureBurnsRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	// Fail only the DISPATCHING pre-write (call 1); recordDispatchFailure's
	// follow-up write at call 2 must succeed so retry_count actually lands.
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call == 1 {
			return errors.New("transient row write failure")
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"pre-write failure must short-circuit before cmd.Curtail")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State,
		"target stays in the prior state after a pre-write failure under MaxRetries")
	assert.Equal(t, int32(1), final.RetryCount,
		"pre-write failure must bump retry_count so the event can't stall indefinitely")
	require.NotNil(t, final.LastError, "pre-write failure must record last_error")
}

// If Stop moves the parent event out of the active phase after the liveness
// read but before the DISPATCHING pre-write, the write must race-lose and the
// reconciler must not issue another Curtail.
func TestReconciler_CurtailPreWriteRaceLosesWhenStopFlipsEventState(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.updateTargetStateHook = func(_ string, _ interfaces.UpdateCurtailmentTargetStateParams, call int) error {
		if call == 1 {
			store.events[0] = &models.Event{
				ID:        eventID,
				EventUUID: eventUUID,
				OrgID:     1,
				State:     models.EventStateRestoring,
			}
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"stale active-phase pre-write must fail before cmd.Curtail is issued")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
}

// If Stop runs between the pre-cmd DISPATCHING write and the post-cmd
// DISPATCHED write, the post-cmd write's ExpectedDesiredState predicate
// must race-lose so it doesn't clobber Stop's reset. curtailHook
// simulates the race by flipping desired_state to 'active' during the
// command call.
func TestReconciler_CurtailPostWriteRaceLosesWhenStopFlipsDesiredState(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStatePending,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}

	// Mid-command, simulate a concurrent Stop: event → RESTORING, target's
	// DesiredState flipped to 'active', state reset to 'pending'. (Per
	// ResetCurtailmentTargetsForRestore semantics.)
	disp.curtailHook = func(_ []string) {
		store.events[0].State = models.EventStateRestoring
		store.targetsByEventID[eventID][0].DesiredState = models.DesiredStateActive
		store.targetsByEventID[eventID][0].State = models.TargetStatePending
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// cmd.Curtail fired exactly once — the device received the Curtail.
	assert.Equal(t, 1, disp.curtailCalls,
		"cmd.Curtail fires before the race-lose detection (device-level effect is unavoidable mid-command)")

	// The post-cmd DISPATCHED write must NOT have landed. The target
	// must retain Stop's reset state (PENDING + DesiredStateActive) so
	// observeRestoring picks it up next tick and issues the compensating
	// Uncurtail via maybeClaimRestoreBatch.
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State,
		"post-cmd write must race-lose; target stays in Stop's reset state for restore to pick up")
	assert.Equal(t, models.DesiredStateActive, final.DesiredState,
		"Stop's desired_state flip must be preserved (not clobbered by the racing post-cmd write)")
	assert.Nil(t, final.LastBatchUUID,
		"no Curtail batch identifier should be stamped on a target that Stop has reset for restore")
}

// A target left in DISPATCHING by an interrupted prior tick must be
// redispatched on the next tick (Curtail is device-idempotent).
func TestReconciler_RecoversOrphanedDispatchingTarget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatching, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"orphaned DISPATCHING target must be redispatched on the next tick")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// observeActive's orphan recovery: a DISPATCHING target on an ACTIVE
// event (interrupted drift-fix) must be re-issued via Curtail.
func TestReconciler_RecoversOrphanedDispatchingTargetOnActiveEvent(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"orphaned DISPATCHING target on an ACTIVE event must be redispatched")
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// TestReconciler_ObserveActive_DispatchingOrphanRespectsRetryBudget: a
// DISPATCHING orphan whose RetryCount has already hit MaxRetries must NOT
// be redispatched. Matches the symmetric Drifted-arm backstop and prevents
// budget-exhausted orphans from cycling indefinitely.
// A DISPATCHING orphan whose retry_count is already at MaxRetries must
// terminalize on the next tick rather than loop forever in DISPATCHING.
// The recordDispatchFailure fallback (BumpTargetRetry on writeTargetState
// failure) can leave a target in DISPATCHING with a bumped retry count,
// so observeActive must keep curtailed targets retryable even after the alert
// threshold is reached.
func TestReconciler_ObserveActive_ExhaustedCurtailDispatchingOrphanKeepsRetrying(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			RetryCount:         3, // already at MaxRetries default
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"curtailment retry threshold surfaces an alert but does not abandon an asserted OFF policy")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(3), final.RetryCount)
	assert.Nil(t, final.LastError, "successful redispatch clears the alert error")
}

// A race-loss on the orphan-redispatch pre-write must not fire Curtail
// — pins the EXISTS-guard race-closure on the observeActive path.
func TestReconciler_ObserveActive_DispatchingOrphanRaceLossDoesNotIssueCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	store.updateTargetStateHook = func(string, interfaces.UpdateCurtailmentTargetStateParams, int) error {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}

	eventID := int64(11)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatching,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"race-loss on the DISPATCHING pre-write must prevent cmd.Curtail from firing")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatching, final.State,
		"in-memory mirror must not advance on race-loss")
}

// Restore-path orphan recovery: a DISPATCHING target with
// DesiredState=Active is redispatched via Uncurtail. uncurtailHook
// asserts the DISPATCHING pre-write commits before the command issues —
// guards against a regression that calls Uncurtail before stamping.
func TestReconciler_RecoversOrphanedDispatchingRestoreTarget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateRestoring},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			DesiredState:       models.DesiredStateActive,
			State:              models.TargetStateDispatching,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	var stateAtUncurtail models.TargetState
	disp.uncurtailHook = func(_ []string) {
		stateAtUncurtail = store.targetsByEventID[eventID][0].State
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.uncurtailCalls,
		"orphaned DISPATCHING restore target must be redispatched via Uncurtail")
	assert.Equal(t, models.TargetStateDispatching, stateAtUncurtail,
		"target must be re-stamped DISPATCHING before Uncurtail fires so AdminTerminate's in-flight gate sees the row")
	require.Len(t, store.targetsByEventID[eventID], 1)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

func TestReconciler_SkipsRestoreDispatchWhenEventTerminatesBeforeCommand(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateRestoring},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			DesiredState:       models.DesiredStateActive,
			State:              models.TargetStatePending,
			BaselinePowerW:     ptrFloat64(3000),
		},
	}
	store.listTargetsHook = func(context.Context, uuid.UUID) {
		store.events[0].State = models.EventStateFailed
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.uncurtailCalls)
	assert.Equal(t, 0, store.updateTargetCalls)
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
}

func TestReconciler_DispatchedConfirmedViaTelemetry_TransitionsEventActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	// Single target already in dispatched state; telemetry shows curtailed.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// No new dispatch (target was already dispatched).
	assert.Equal(t, 0, disp.curtailCalls)
	// Target promoted to confirmed.
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	// Event flipped to active.
	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
}

func TestReconciler_DriftDetectionRetriesDispatch(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		// power_w=2500 vs baseline=3000 * 0.5 threshold=1500 → drifted
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// Drifted target re-dispatched.
	assert.Equal(t, 1, disp.curtailCalls)
	// Target ends in dispatched state (after re-dispatch updates it from drifted).
	// RetryCount stays at 0 because the redispatch succeeded — the budget is
	// consumed on dispatch *failures*, not on drift detection.
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(0), final.RetryCount)
}

// A Drifted target whose retry_count already sits at MaxRetries must
// escalate to the terminal state rather than loop in Drifted. Mirrors
// the DISPATCHING arm: BumpTargetRetry's fallback can bump retry_count
// past the alert threshold without a state transition, so observeActive keeps
// curtailed Drifted targets retryable while OFF is asserted.
func TestReconciler_RetryExhaustedCurtailDriftedKeepsRetrying(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDrifted, BaselinePowerW: ptrFloat64(3000), RetryCount: 3},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, disp.curtailCalls,
		"curtailment retry threshold surfaces an alert but does not abandon an asserted OFF policy")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(3), final.RetryCount)
	assert.Nil(t, final.LastError, "successful redispatch clears the alert error")
}

func TestReconciler_PerEventErrorIsolation(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Two events: the first will panic mid-process via a poisoned target;
	// the second must still complete.
	store.events = []*models.Event{
		{ID: 10, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
		{ID: 20, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[10] = []*models.Target{
		{CurtailmentEventID: 10, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}
	store.targetsByEventID[20] = []*models.Target{
		{CurtailmentEventID: 20, DeviceIdentifier: "miner-2", State: models.TargetStatePending},
	}

	// Force a panic on the first dispatch only.
	first := true
	disp.curtailErr = nil
	r := newReconcilerForTest(store, disp)
	originalCmd := r.cmd
	r.cmd = &panickyDispatcher{wrapped: originalCmd, panicOn: func() bool {
		if first {
			first = false
			return true
		}
		return false
	}}

	r.runTick(context.Background())

	// Event 20 still saw a dispatch.
	assert.Equal(t, 1, disp.curtailCalls)
	// Heartbeat still fires.
	assert.Equal(t, 1, store.heartbeatCalls)
}

func TestReconciler_HeartbeatAdvancesOnEveryTick(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	// Empty event list still upserts heartbeat.
	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())
	r.runTick(context.Background())
	r.runTick(context.Background())
	assert.Equal(t, 3, store.heartbeatCalls)
}

// Heartbeat advances on tick freshness, not query health: a List failure
// still upserts the heartbeat (with active_count=0) and increments
// IncTickFailure, so the SQL staleness alert distinguishes "reconciler
// dead" (no upsert) from "DB read path degraded" (upsert advances,
// IncTickFailure rises).
func TestReconciler_ListEventsErrorAdvancesHeartbeatAndIncrementsFailure(t *testing.T) {
	store := newFakeStore()
	store.listEventsErr = errors.New("db down")
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.runTick(context.Background())

	assert.Equal(t, 1, store.heartbeatCalls,
		"heartbeat must advance on tick freshness so the SQL staleness alert distinguishes reconciler-dead from DB-read-degraded")
	assert.Equal(t, int32(0), store.lastHeartbeatActive,
		"List failure carries activeCount=0 — no events observed this tick")
	assert.Equal(t, 1, metrics.TickFailureCount())
}

func TestReconciler_RunTickSharesBudgetAcrossEvents(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	firstUUID := uuid.New()
	secondUUID := uuid.New()
	store.events = []*models.Event{
		{ID: 1, EventUUID: firstUUID, OrgID: 1, State: models.EventStateActive},
		{ID: 2, EventUUID: secondUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.listTargetsHook = func(ctx context.Context, eventUUID uuid.UUID) {
		if eventUUID == firstUUID {
			<-ctx.Done()
		}
	}

	r := newReconcilerForTest(store, disp)
	r.cfg.TickInterval = 50 * time.Millisecond
	r.runTick(context.Background())

	assert.ErrorIs(t, store.listTargetsCtxErr[firstUUID], context.DeadlineExceeded)
	secondErr, processedSecond := store.listTargetsCtxErr[secondUUID]
	assert.True(t, processedSecond, "a slow first event must not starve later events in the same tick")
	assert.NoError(t, secondErr)
}

func TestReconciler_DispatchErrorMarksLastError(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "dispatch error keeps target pending for retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "queue down")
	assert.Equal(t, int32(1), final.RetryCount, "dispatch error bumps retry budget")
}

// TestReconciler_DispatchSkippedKeepsTargetPending: result.Skipped means the
// command was filter-blocked and never enqueued; promoting to dispatched
// would silently drop the work. Stay pending and surface the skip reason.
func TestReconciler_DispatchSkippedKeepsTargetPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{
			BatchIdentifier: "",
			DispatchedCount: 0,
			Skipped: []command.SkippedDevice{{
				DeviceIdentifier: "miner-1",
				FilterName:       "schedule_conflict",
				Reason:           "schedule 99 holds higher priority",
			}},
		},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "filter-skipped target stays pending")
	assert.Nil(t, final.LastDispatchedAt, "filter-skipped target must not record a dispatch timestamp")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "schedule 99")
	assert.Equal(t, int32(1), final.RetryCount, "skip counts toward retry budget")
}

// TestSkippedDeviceReason pins the priority-order contract for the shared
// skip-reason rendering helper. Both the single-device dispatch path and the
// batch restore path route audit strings through this switch, so each branch
// must land on the right string.
func TestSkippedDeviceReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    command.SkippedDevice
		want string
	}{
		{"reason_present_wins", command.SkippedDevice{Reason: "schedule conflict", FilterName: "schedule"}, "schedule conflict"},
		{"filter_name_only", command.SkippedDevice{FilterName: "maintenance_window"}, "filtered by maintenance_window"},
		{"both_empty_fallback", command.SkippedDevice{}, "filtered by command preflight"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, skippedDeviceReason(tc.s))
		})
	}
}

// A vanished candidate during confirm routes through
// recordDispatchFailure so the event can't stall on a deleted device.
func TestReconciler_MissingCandidateDuringConfirmConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "vanished", State: models.TargetStateDispatched, RetryCount: 0},
	}
	// store.candidates left empty: ListCandidates returns nothing for
	// "vanished" → confirmOneDispatched takes the nil-candidate path.

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State, "target stays dispatched while budget consumes")
	assert.Equal(t, int32(1), final.RetryCount, "missing candidate consumes a retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
}

func TestReconciler_AllPairedDispatchedTargetMissingCandidateDoesNotRelease(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStatePending,
			ForceIncludeAllPairedMiners: true,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "vanished",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateCurtailed,
			RetryCount:         0,
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	assert.Equal(t, int32(1), final.RetryCount)
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
}

func TestReconciler_AllPairedConfirmedTargetUnpairedDoesNotRelease(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			ForceIncludeAllPairedMiners: true,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "unpaired",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
			RetryCount:         0,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "unpaired",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "UNPAIRED",
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(1), final.RetryCount)
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "device is no longer paired-like")
}

// The tick after a confirmed all-paired target drifts on pairing loss, the
// Drifted arm must apply the same paired-like guard as confirm/drift: the row
// keeps policy ownership, but no re-curtail command may be dispatched to a
// device that is no longer paired-like.
func TestReconciler_AllPairedDriftedTargetUnpairedDoesNotDispatch(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{
			ID:                          eventID,
			EventUUID:                   eventUUID,
			OrgID:                       1,
			State:                       models.EventStateActive,
			ForceIncludeAllPairedMiners: true,
		},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "unpaired",
			State:              models.TargetStateDrifted,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
			RetryCount:         1,
		},
	}
	driver := "antminer"
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "unpaired",
			DriverName:       &driver,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "UNPAIRED",
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
		},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 0, disp.curtailCalls,
		"no Curtail command may be dispatched to a device that is no longer paired-like")
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State,
		"the row keeps policy ownership (not released) while unpaired")
	assert.Equal(t, int32(2), final.RetryCount)
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "device is no longer paired-like")
}

func TestReconciler_CurtailConfirmationTimeoutConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	dispatchedAt := time.Date(2026, 5, 7, 11, 59, 40, 0, time.UTC)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "slow-confirm",
			State:              models.TargetStateDispatched,
			LastDispatchedAt:   &dispatchedAt,
			BaselinePowerW:     ptrFloat64(3000),
			RetryCount:         0,
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "slow-confirm", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatching, final.State)
	assert.Equal(t, int32(1), final.RetryCount)
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "curtail telemetry timeout")
	assert.Equal(t, 0, disp.curtailCalls, "timeout retry should wait for the batch-aware dispatch path")
}

func TestReconciler_ActiveCurtailConfirmationTimeoutRespectsBatchLimit(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	batchSize := int32(1)
	dispatchedAt := time.Date(2026, 5, 7, 11, 59, 40, 0, time.UTC)
	store.events = []*models.Event{
		{
			ID:                      eventID,
			EventUUID:               eventUUID,
			OrgID:                   1,
			State:                   models.EventStateActive,
			CurtailBatchSize:        &batchSize,
			CurtailBatchIntervalSec: 60,
		},
	}
	for _, id := range []string{"slow-a", "slow-b"} {
		store.targetsByEventID[eventID] = append(store.targetsByEventID[eventID], &models.Target{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   id,
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateCurtailed,
			LastDispatchedAt:   &dispatchedAt,
			BaselinePowerW:     ptrFloat64(3000),
		})
		store.candidates = append(store.candidates, &models.Candidate{
			DeviceIdentifier: id,
			LatestPowerW:     ptrFloat64(3000),
			LatestHashRateHS: ptrFloat64(100),
		})
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	require.Equal(t, 1, disp.curtailCalls, "timed-out retries must drain through the configured batch limit")
	require.Len(t, disp.curtailCallIDs, 1)
	assert.Len(t, disp.curtailCallIDs[0], 1)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][1].RetryCount)
}

// Mirror of MissingCandidateDuringConfirm for the drift path: a
// vanished candidate burns retry budget toward RestoreFailed.
func TestReconciler_MissingCandidateDuringDriftConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	now := time.Now()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive, StartedAt: &now},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "vanished", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000), RetryCount: 0},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State, "missing candidate moves confirmed→drifted via failure record")
	assert.Equal(t, int32(1), final.RetryCount, "missing candidate consumes a retry")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
}

// TestReconciler_DispatchEmptyBatchKeepsTargetPending: a nil-error result
// with an empty BatchIdentifier means processCommand resolved zero device
// IDs (e.g. miner unpaired between Start and reconcile). No batch was
// enqueued, so the target must NOT be marked dispatched.
func TestReconciler_DispatchEmptyBatchKeepsTargetPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		curtailResultOverride: &command.CommandResult{BatchIdentifier: "", DispatchedCount: 0},
	}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State, "empty-batch target stays pending")
	assert.Nil(t, final.LastDispatchedAt, "empty-batch target must not record dispatch timestamp")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "no batch")
	assert.Equal(t, int32(1), final.RetryCount, "empty batch counts toward retry budget")
}

// TestReconciler_DispatchFailureExhaustionKeepsCurtailRetrying:
// after CurtailMaxRetries dispatch failures the target remains retryable while
// the curtailment demand is asserted; retry_count is the operator alert.
func TestReconciler_DispatchFailureExhaustionKeepsCurtailRetrying(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1 + 2: target stays pending with retry incremented.
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][0].RetryCount)

	r.runTick(context.Background())
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(2), store.targetsByEventID[eventID][0].RetryCount)

	// Tick 3 hits CurtailMaxRetries=3 but stays pending for the next retry.
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(3), store.targetsByEventID[eventID][0].RetryCount)
	assert.Empty(t, store.updateEventLast[eventID])
}

// TestReconciler_PendingPromotesActiveWithMixedTerminalTargets: a confirmed
// target plus a permanently failed target should let the event promote to
// active; the failure does not block lifecycle progression.
func TestReconciler_PendingPromotesActiveWithMixedTerminalTargets(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		// miner-1 already dispatched + telemetry shows curtailed → confirms.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000)},
		// miner-2 was already exhausted → terminal failure on this tick.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-2", State: models.TargetStateRestoreFailed, RetryCount: 3},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// miner-1 confirmed; miner-2 still terminal; event promoted to active
	// despite the failed target.
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateRestoreFailed, store.targetsByEventID[eventID][1].State)
	assert.Equal(t, models.EventStateActive, store.updateEventLast[eventID])
}

// TestReconciler_PendingDoesNotPromoteWhilePartialBudgetRemains: a single
// failing target with two retries left must not push the event into active
// — the budget is still in flight.
func TestReconciler_PendingDoesNotPromoteWhilePartialBudgetRemains(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// Retry incremented, target still pending, event not promoted.
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(1), store.targetsByEventID[eventID][0].RetryCount)
	_, promoted := store.updateEventLast[eventID]
	assert.False(t, promoted, "event must not promote while retry budget remains")
}

// TestReconciler_DispatchedReConfirmsViaObserveActive: a target that drifted
// then re-dispatched (Dispatched on an active event) should re-confirm in
// the same flow once telemetry shows curtailment.
func TestReconciler_DispatchedReConfirmsViaObserveActive(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		// On an active event, a dispatched target represents a
		// drift→redispatch waiting on confirmation telemetry.
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateDispatched, BaselinePowerW: ptrFloat64(3000), RetryCount: 1},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	// No new dispatch; observeActive promotes back to confirmed and resets retry.
	assert.Equal(t, 0, disp.curtailCalls)
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateConfirmed, final.State)
	assert.Equal(t, int32(0), final.RetryCount, "confirmation resets retry budget for the next drift cycle")
}

// TestReconciler_RetryBudgetResetsOnReConfirm: drift → confirm → drift →
// confirm cycles must each get a fresh retry budget so a flapping miner is
// not artificially terminated by carry-over attempts.
func TestReconciler_RetryBudgetResetsOnReConfirm(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	// Tick 1: drift power=2500 → drifted+redispatch (success keeps RetryCount=0).
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}
	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)

	// Tick 2: telemetry shows curtailed → reconfirm. Retry stays at 0.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}
	r.runTick(context.Background())
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount, "reconfirm clears retry budget")

	// Tick 3: drift again — successful redispatch leaves RetryCount=0 again.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}
	r.runTick(context.Background())
	assert.Equal(t, int32(0), store.targetsByEventID[eventID][0].RetryCount)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
}

// TestReconciler_DriftFailedRedispatchConsumesOneBudgetPerAttempt: the retry
// budget tracks dispatch attempts, not drift events. Three drift+fail cycles
// must consume exactly MaxRetries=3 dispatch slots — not 6 — and the third
// failure transitions the target to RestoreFailed.
func TestReconciler_DriftFailedRedispatchConsumesOneBudgetPerAttempt(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	// Telemetry stays drifted for every tick.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1: confirmed → drifted, redispatch fails → RetryCount=1, state Drifted
	// (not Pending: a Pending target on an active event is orphaned).
	r.runTick(context.Background())
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State, "non-terminal failure on active-event redispatch must stay Drifted, not flip to Pending")
	assert.Equal(t, int32(1), final.RetryCount)

	// Tick 2: drifted → redispatch fails → RetryCount=2, state Drifted.
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(2), final.RetryCount)

	// Tick 3: drifted → redispatch fails → RetryCount=3 reaches the alert
	// threshold but stays retryable while OFF is asserted.
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(3), final.RetryCount)

	// Exactly 3 dispatch attempts — not 6. Each cycle consumes one alert slot,
	// not two. (Old bug: checkDrift bumped retry, then recordDispatchFailure
	// bumped again, halving the effective budget.)
	assert.Equal(t, 3, disp.curtailCalls, "CurtailMaxRetries=3 should map to exactly 3 alert-counted attempts")
}

// TestReconciler_DriftFailedRedispatchStaysDriftedNotPending: when an
// active-event redispatch fails with budget remaining, the target must stay
// Drifted so observeActive's Drifted arm picks it up next tick. Flipping to
// Pending would orphan the target — observeActive's Pending case is a no-op.
func TestReconciler_DriftFailedRedispatchStaysDriftedNotPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{curtailErr: errors.New("queue down")}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStateActive},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStateConfirmed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)

	// Tick 1: drift detected, redispatch fails with 2 retries left.
	r.runTick(context.Background())
	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDrifted, final.State)
	assert.Equal(t, int32(1), final.RetryCount)

	// Tick 2: observeActive's Drifted arm must pick it up and re-dispatch.
	// Switch to a successful dispatcher so the retry consumes a slot only on
	// failure (this tick succeeds → RetryCount stays 1, state goes Dispatched).
	disp.curtailErr = nil
	r.runTick(context.Background())
	final = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State, "Drifted target with budget must redispatch on the next tick")
	assert.Equal(t, int32(1), final.RetryCount, "successful redispatch does not consume the budget")
	assert.Equal(t, 2, disp.curtailCalls)
}

// TestReconciler_SuccessfulDispatchClearsLastError: a dispatch success after
// a prior failure must clear LastError so the UI does not surface a stale
// transient error after the resolution succeeded.
func TestReconciler_SuccessfulDispatchClearsLastError(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	prior := "prior queue error"
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, LastError: &prior},
	}
	// Candidate row present but with no telemetry yet — confirmOneDispatched
	// finds the row (no nil-candidate failure path) and returns early on
	// !isPositivelyCurtailed, leaving the dispatch's clear-LastError write
	// as the final state.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1"},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State)
	if final.LastError != nil {
		assert.Empty(t, *final.LastError, "successful dispatch must clear LastError")
	}
	// The store call should have been made with a non-nil empty pointer
	// so the SQL UPDATE clears the column rather than leaving stale text.
	params := store.updateTargetParams["miner-1"]
	require.NotNil(t, params.LastError, "successful dispatch must explicitly clear LastError on the wire")
	assert.Empty(t, *params.LastError)
}

// TestReconciler_ListTargetsByEventOnce: dispatchPending+confirmDispatched+
// maybeMarkActive must share a single ListTargetsByEvent fetch per event
// per tick instead of round-tripping three times.
func TestReconciler_ListTargetsByEventOnce(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	eventID := int64(10)
	eventUUID := uuid.New()
	store.events = []*models.Event{
		{ID: eventID, EventUUID: eventUUID, OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	r.runTick(context.Background())

	assert.Equal(t, 1, store.listTargetsByEventCalls, "pending phases must share one ListTargetsByEvent per tick")
}

// TestReconciler_ObserveTickDurationFiresOnHappyPath: every successful
// safeTick records a duration sample.
func TestReconciler_ObserveTickDurationFiresOnHappyPath(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }

	r.safeTick(context.Background())
	r.safeTick(context.Background())

	assert.Equal(t, 2, metrics.TickCount(), "ObserveTickDuration fires once per safeTick invocation")
	assert.Equal(t, 0, metrics.TickFailureCount(), "no panic, no failure increment")
}

// TestReconciler_TickFailureFiresOnTickInfraPanic: ListNonTerminalEvents
// panicking is a tick-infra failure (heartbeat-skipping). IncTickFailure
// fires; ObserveTickDuration still fires because the deferred recorder runs
// regardless.
func TestReconciler_TickFailureFiresOnTickInfraPanic(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	store.listEventsPanicErr = "synthetic db panic"
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }

	r.safeTick(context.Background())

	assert.Equal(t, 1, metrics.TickFailureCount(), "tick-infra panic increments TickFailures")
	assert.Equal(t, 1, metrics.TickCount(), "ObserveTickDuration still fires on a panicked tick")
}

// TestReconciler_TickFailureFiresOnPerEventPanic: a panic inside processEvent
// is recovered per-event; the tick keeps running but the failure counter
// advances so operators can spot per-event panics.
func TestReconciler_TickFailureFiresOnPerEventPanic(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	metrics := newRecordingMetrics()

	store.events = []*models.Event{
		{ID: 10, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
		{ID: 20, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending},
	}
	store.targetsByEventID[10] = []*models.Target{
		{CurtailmentEventID: 10, DeviceIdentifier: "miner-1", State: models.TargetStatePending},
	}
	store.targetsByEventID[20] = []*models.Target{
		{CurtailmentEventID: 20, DeviceIdentifier: "miner-2", State: models.TargetStatePending},
	}

	first := true
	r := New(Config{
		TickInterval:         time.Hour,
		ShutdownDeadline:     time.Second,
		MaxRetries:           3,
		DriftThresholdFactor: 0.5,
	}, store, disp, WithMetrics(metrics))
	r.now = func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) }
	originalCmd := r.cmd
	r.cmd = &panickyDispatcher{wrapped: originalCmd, panicOn: func() bool {
		if first {
			first = false
			return true
		}
		return false
	}}

	r.safeTick(context.Background())

	assert.Equal(t, 1, metrics.TickFailureCount(), "per-event panic increments TickFailures even though tick continued")
	assert.Equal(t, 1, store.heartbeatCalls, "per-event panic does not skip the heartbeat")
}

// TestReconciler_PanicInListEventsRecovers: ListNonTerminalEvents panicking
// must not tear down the goroutine; the next tick still runs.
func TestReconciler_PanicInListEventsRecovers(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	store.listEventsPanicErr = "synthetic db panic"
	r := newReconcilerForTest(store, disp)

	// safeTick is what the tick loop invokes; calling it here exercises the
	// top-level recover() guard. A bare runTick would re-panic the test.
	r.safeTick(context.Background())
	assert.Equal(t, 1, store.listEventsCalls, "first tick saw the panicking call")

	// Now drop the panic and run another tick — the loop should still be
	// healthy.
	store.listEventsPanicErr = ""
	r.safeTick(context.Background())
	assert.Equal(t, 2, store.listEventsCalls, "subsequent tick still runs after recover")
}

// TestReconciler_StartIdempotency: calling Start twice without an
// intervening Stop must not fork a second goroutine.
func TestReconciler_StartIdempotency(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	r := New(Config{TickInterval: time.Hour, ShutdownDeadline: time.Second}, store, disp)

	require.NoError(t, r.Start(context.Background()))
	require.NoError(t, r.Start(context.Background()), "second Start is a no-op")
	require.NoError(t, r.Stop())
	// Second Stop is a no-op too; verify no panic / goroutine deadlock.
	require.NoError(t, r.Stop())
}

func TestReconciler_StartRejectsSubSecondTickInterval(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	r := New(Config{TickInterval: 500 * time.Millisecond, ShutdownDeadline: time.Second}, store, disp)

	err := r.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tick_interval must be at least 1s")
}

func TestReconciler_StartRejectsInvalidCurtailDispatchTimeout(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	r := New(Config{
		TickInterval:              time.Hour,
		ShutdownDeadline:          time.Second,
		CurtailDispatchTimeoutSec: -1,
	}, store, disp)

	err := r.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "curtail_dispatch_timeout_sec must be at least 1")
}

func TestReconciler_ConfigDefaultsDispatchTimeouts(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	r := New(Config{TickInterval: time.Hour, ShutdownDeadline: time.Second}, store, disp)

	assert.Equal(t, int32(10), r.cfg.MaxRetries)
	assert.Equal(t, int32(50), r.cfg.CurtailMaxRetries)
	assert.Equal(t, 5, r.cfg.CurtailDispatchTimeoutSec)
	assert.Equal(t, 30, r.cfg.RestoreDispatchTimeoutSec)
}

// --- isCurtailed unit tests ---
// requirePositiveEvidence=false (drift): missing samples preserve
// curtailed; =true (confirm): missing samples return false.

// A pool-less miner's persisted idle baseline (#663) makes power-vs-baseline
// the confirm signal; hash is 0 through the whole lifecycle so the hash-only
// fallback would confirm vacuously.
func TestIsCurtailed_ConfirmPath_IdleBaselinePoolLessMiner(t *testing.T) {
	t.Parallel()
	baseline := 400.0
	// Sleep draw well under half the idle draw: confirmed.
	assert.True(t, isCurtailed(ptrFloat64(30), &baseline, ptrFloat64(0), 0.5, true))
	// Draw still above half the idle baseline: not confirmed — the sleep
	// command has not (yet) taken effect.
	assert.False(t, isCurtailed(ptrFloat64(210), &baseline, ptrFloat64(0), 0.5, true))
}

func TestIsCurtailed_DriftPath_BaselineRelativeThreshold(t *testing.T) {
	baseline := 3000.0
	// 1000 < baseline*0.5=1500 → curtailed
	assert.True(t, isCurtailed(ptrFloat64(1000), &baseline, ptrFloat64(0), 0.5, false))
	// 2500 > 1500 → not curtailed
	assert.False(t, isCurtailed(ptrFloat64(2500), &baseline, ptrFloat64(100), 0.5, false))
}

func TestIsCurtailed_DriftPath_DualSignalFallbackWithoutBaseline(t *testing.T) {
	// No baseline; positive hash → drifted.
	assert.False(t, isCurtailed(ptrFloat64(2500), nil, ptrFloat64(100), 0.5, false))
	// No baseline; zero hash → curtailed.
	assert.True(t, isCurtailed(ptrFloat64(2500), nil, ptrFloat64(0), 0.5, false))
}

func TestIsCurtailed_DriftPath_NonFinitePreservesCurtailed(t *testing.T) {
	baseline := 3000.0
	nan := math.NaN()
	inf := math.Inf(1)
	// NaN power → no signal → preserve curtailed.
	assert.True(t, isCurtailed(&nan, &baseline, ptrFloat64(0), 0.5, false))
	// +Inf power → no signal → preserve curtailed.
	assert.True(t, isCurtailed(&inf, &baseline, ptrFloat64(0), 0.5, false))
	// nil power, NaN hash → preserve curtailed.
	assert.True(t, isCurtailed(nil, &baseline, &nan, 0.5, false))
}

// TestIsCurtailed_ConfirmPath_RequiresEvidence pins the asymmetric default
// for the confirm path: missing or non-finite telemetry returns false so
// `dispatched` targets are NOT promoted to `confirmed` without evidence
// the device actually went down.
func TestIsCurtailed_ConfirmPath_RequiresEvidence(t *testing.T) {
	baseline := 3000.0
	nan := math.NaN()
	inf := math.Inf(1)

	// Below threshold → confirmed.
	assert.True(t, isCurtailed(ptrFloat64(1000), &baseline, ptrFloat64(0), 0.5, true))
	// Above threshold → not confirmed.
	assert.False(t, isCurtailed(ptrFloat64(2500), &baseline, ptrFloat64(0), 0.5, true))

	// Missing power → not confirmed (asymmetric vs. drift detection).
	assert.False(t, isCurtailed(nil, &baseline, ptrFloat64(0), 0.5, true))
	// Non-finite power → not confirmed.
	assert.False(t, isCurtailed(&nan, &baseline, ptrFloat64(0), 0.5, true))
	assert.False(t, isCurtailed(&inf, &baseline, ptrFloat64(0), 0.5, true))

	// Baseline missing: dual-signal fallback requires finite zero-or-negative hash.
	assert.True(t, isCurtailed(ptrFloat64(1000), nil, ptrFloat64(0), 0.5, true))
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, nil, 0.5, true), "no baseline + missing hash → no evidence")
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, &nan, 0.5, true), "no baseline + non-finite hash → no evidence")
	assert.False(t, isCurtailed(ptrFloat64(1000), nil, ptrFloat64(100), 0.5, true), "no baseline + positive hash → drifted")
}

// panickyDispatcher proxies Curtail/Uncurtail and panics when panicOn() returns
// true. Used to verify per-event error isolation.
type panickyDispatcher struct {
	wrapped CommandDispatcher
	panicOn func() bool
}

func (p *panickyDispatcher) Curtail(ctx context.Context, selector *pb.DeviceSelector, level sdk.CurtailLevel) (*command.CommandResult, error) {
	if p.panicOn() {
		panic("simulated dispatch panic")
	}
	return p.wrapped.Curtail(ctx, selector, level)
}

func (p *panickyDispatcher) Uncurtail(ctx context.Context, selector *pb.DeviceSelector) (*command.CommandResult, error) {
	return p.wrapped.Uncurtail(ctx, selector)
}

// recordingMetrics captures Metrics calls for assertion. Counters are
// goroutine-safe via a single mutex; the reconciler emits from the tick
// goroutine but tests poke from the test goroutine.
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

func (m *recordingMetrics) IncAuditWriteFailure(activityType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.auditWriteFailures == nil {
		m.auditWriteFailures = map[string]int{}
	}
	m.auditWriteFailures[activityType]++
}

func (m *recordingMetrics) IncAllPairedPendingStall() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allPairedPendingStalls++
}

func (m *recordingMetrics) AllPairedPendingStallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allPairedPendingStalls
}

func (m *recordingMetrics) EventStateRaceLossCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eventStateRaces
}

func (m *recordingMetrics) TargetWriteFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targetWriteFailures
}

func (m *recordingMetrics) TickCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tickDurations)
}

func (m *recordingMetrics) TickFailureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tickFailures
}
