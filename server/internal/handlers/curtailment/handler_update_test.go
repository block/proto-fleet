package curtailment

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// updateStubStore is a focused fake for Update handler tests. It supports
// the Service.Update read-then-update pattern: GetEventByUUID returns the
// pre-read row, UpdateOperatorFields returns the post-update row.
type updateStubStore struct {
	event             *models.Event
	updatedEvent      *models.Event
	updateErr         error
	lastUpdateID      int64
	lastUpdateOrgID   int64
	lastUpdateParams  interfaces.UpdateOperatorFieldsParams
	getEventCalls     int
	updateCalls       int
	expectedEventUUID uuid.UUID
	getEventErr       error
	// Targets returned by ListTargetsByEvent; the handler fetches them
	// post-update so the response shape mirrors the read endpoints.
	targets    []*models.Target
	targetsErr error
}

func newUpdateStubStore(state models.EventState) *updateStubStore {
	eventUUID := uuid.New()
	return &updateStubStore{
		expectedEventUUID: eventUUID,
		event: &models.Event{
			ID:                      99,
			EventUUID:               eventUUID,
			OrgID:                   42,
			State:                   state,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
			Reason:                  "initial reason",
		},
	}
}

func (s *updateStubStore) GetEventByUUID(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	s.getEventCalls++
	if s.getEventErr != nil {
		return nil, s.getEventErr
	}
	if eventUUID != s.expectedEventUUID {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	return s.event, nil
}

func (s *updateStubStore) GetEventDetailByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventDetailByUUID not exercised by Update handler tests")
}

func (s *updateStubStore) UpdateOperatorFields(_ context.Context, eventID, orgID int64, params interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	s.updateCalls++
	s.lastUpdateID = eventID
	s.lastUpdateOrgID = orgID
	s.lastUpdateParams = params
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	if s.updatedEvent != nil {
		return s.updatedEvent, nil
	}
	// Default: synthesize a row that reflects the patch applied to the
	// pre-read event. Tests that need exact assertions seed updatedEvent
	// explicitly.
	out := *s.event
	if params.Reason != nil {
		out.Reason = *params.Reason
	}
	if params.RestoreBatchSize != nil {
		out.RestoreBatchSize = *params.RestoreBatchSize
	}
	if params.RestoreBatchIntervalSec != nil {
		out.RestoreBatchIntervalSec = *params.RestoreBatchIntervalSec
	}
	if params.MaxDurationSeconds != nil {
		v := *params.MaxDurationSeconds
		out.MaxDurationSeconds = &v
	}
	return &out, nil
}

// --- panic stubs for methods Update path does not exercise ---

// GetOrgConfig is real-faked (rather than panicking) because the admin
// gate on max_duration_seconds inside Service.Update fetches the org
// config lazily for non-admin callers. Returns a high default so the
// existing happy-path tests stay under the gate without being admin.
func (s *updateStubStore) GetOrgConfig(_ context.Context, orgID int64) (*models.OrgConfig, error) {
	return &models.OrgConfig{
		OrgID:                 orgID,
		MaxDurationDefaultSec: 7200,
	}, nil
}
func (s *updateStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by Update handler tests")
}
func (s *updateStubStore) ListRecentlyResolvedCurtailedDevices(context.Context, int64, int32) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by Update handler tests")
}
func (s *updateStubStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised by Update handler tests")
}
func (s *updateStubStore) ListCandidates(context.Context, interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by Update handler tests")
}
func (s *updateStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by Update handler tests")
}
func (s *updateStubStore) GetActiveEvent(context.Context, int64) (*models.Event, error) {
	panic("GetActiveEvent not exercised by Update handler tests")
}
func (s *updateStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised by Update handler tests")
}
func (s *updateStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	return s.targets, s.targetsErr
}
func (s *updateStubStore) ListTargetsByEventPage(context.Context, interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	panic("ListTargetsByEventPage not exercised by Update handler tests")
}
func (s *updateStubStore) GetTargetRollupByEvent(context.Context, int64, uuid.UUID) (*models.TargetRollup, error) {
	panic("GetTargetRollupByEvent not exercised by Update handler tests")
}
func (s *updateStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by Update handler tests")
}
func (s *updateStubStore) BeginRecurtailTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRecurtailTransition not exercised by Update handler tests")
}
func (s *updateStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by Update handler tests")
}
func (s *updateStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by Update handler tests")
}
func (s *updateStubStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by Update handler tests")
}
func (s *updateStubStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by Update handler tests")
}
func (s *updateStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by Update handler tests")
}
func (s *updateStubStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised by Update handler tests")
}
func (s *updateStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by Update handler tests")
}
func (s *updateStubStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by Update handler tests")
}
func (s *updateStubStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by Update handler tests")
}
func (s *updateStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by Update handler tests")
}

func updateSessionCtx(orgID int64, role string) context.Context {
	return updateSessionCtxWithPerms(orgID, role, authz.PermCurtailmentManage)
}

// updateSessionCtxWithPerms returns a session context with explicit
// effective permissions; the permission-denied tests pass an empty
// permission set (or unrelated perms) to exercise the gate.
func updateSessionCtxWithPerms(orgID int64, role string, perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           role,
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}}))
}

// TestHandler_UpdateCurtailmentEvent_HappyPath: optional proto fields
// thread through to the service params; the post-update event echoes on
// the wire.
func TestHandler_UpdateCurtailmentEvent_HappyPath(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	newReason := "schedule conflict — extending"
	newCap := uint32(1800)
	resp, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid:          store.event.EventUUID.String(),
			Reason:             &newReason,
			MaxDurationSeconds: &newCap,
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, store.event.EventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, "schedule conflict — extending", resp.Msg.Event.Reason)
	assert.Equal(t, uint32(1800), resp.Msg.Event.MaxDurationSeconds)

	// Service received the optional shape verbatim.
	require.NotNil(t, store.lastUpdateParams.Reason)
	assert.Equal(t, newReason, *store.lastUpdateParams.Reason)
	require.NotNil(t, store.lastUpdateParams.MaxDurationSeconds)
	assert.Equal(t, int32(1800), *store.lastUpdateParams.MaxDurationSeconds)
	assert.Nil(t, store.lastUpdateParams.RestoreBatchSize, "unset proto fields stay nil through the service layer")
}

// TestHandler_UpdateCurtailmentEvent_RejectsRestoringState: the service
// guard surfaces as FailedPrecondition at the RPC boundary.
func TestHandler_UpdateCurtailmentEvent_RejectsRestoringState(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateRestoring)
	h := NewHandler(domainCurtailment.NewService(store))

	newReason := "updated"
	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: store.event.EventUUID.String(),
			Reason:    &newReason,
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.updateCalls, "service must not reach the store after the state guard rejects")
}

// TestHandler_UpdateCurtailmentEvent_RejectsMissingSession: session-auth
// is required; missing info remaps to Unauthenticated rather than the
// generic Internal that the interceptor would otherwise raise.
func TestHandler_UpdateCurtailmentEvent_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.UpdateCurtailmentEvent(
		t.Context(),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: store.event.EventUUID.String(),
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

// TestHandler_UpdateCurtailmentEvent_RejectsWithoutCurtailmentManage:
// callers lacking curtailment:manage cannot mutate operator-safe fields.
func TestHandler_UpdateCurtailmentEvent_RejectsWithoutCurtailmentManage(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtxWithPerms(42, "OPERATOR" /* no curtailment:manage */),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: store.event.EventUUID.String(),
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_UpdateCurtailmentEvent_UsesSiteScopedEventPermission(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
	)

	for _, tc := range []struct {
		name        string
		assignments []authz.Assignment
		wantCode    connect.Code
		wantCalls   int
	}{
		{"org permission without site narrowing allows update", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage)}, 0, 1},
		{"matching site narrowing allows update", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, 0, 1},
		{"site-only permission denies update", []authz.Assignment{testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, connect.CodePermissionDenied, 0},
		{"site narrowing without manage denies update", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite)}, connect.CodePermissionDenied, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newUpdateStubStore(models.EventStateActive)
			store.event.ScopeType = models.ScopeTypeSite
			store.event.ScopeJSON = siteScopeJSON(t, allowedSite)
			h := NewHandler(domainCurtailment.NewService(store))

			ctx := testSessionCtxWithAssignments(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: orgID,
				UserID:         9,
				Role:           "OPERATOR",
			}, tc.assignments...)

			reason := "site-scoped update"
			_, err := h.UpdateCurtailmentEvent(ctx, connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
				EventUuid: store.event.EventUUID.String(),
				Reason:    &reason,
			}))

			if tc.wantCode == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			}
			assert.Equal(t, tc.wantCalls, store.updateCalls)
		})
	}
}

// TestHandler_UpdateCurtailmentEvent_RejectsMalformedUUID: invalid UUIDs
// surface as InvalidArgument before any store work.
func TestHandler_UpdateCurtailmentEvent_RejectsMalformedUUID(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: "not-a-uuid",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.getEventCalls, "malformed UUID must reject before any store call")
}

// TestHandler_UpdateCurtailmentEvent_AdminLargeIntervalAllowed: an Admin
// caller can set restore_batch_interval_sec above the non-admin cap.
// CanUseAdminControls flows from session.Role through the handler.
func TestHandler_UpdateCurtailmentEvent_AdminLargeIntervalAllowed(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	interval := uint32(600) // > 300 non-admin cap, < 3600 absolute ceiling
	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid:               store.event.EventUUID.String(),
			RestoreBatchIntervalSec: &interval,
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, store.lastUpdateParams.RestoreBatchIntervalSec)
	assert.Equal(t, int32(600), *store.lastUpdateParams.RestoreBatchIntervalSec)
}

// TestHandler_UpdateCurtailmentEvent_NonAdminLargeIntervalForbidden:
// mirror gate from Start applies on Update.
func TestHandler_UpdateCurtailmentEvent_NonAdminLargeIntervalForbidden(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	h := NewHandler(domainCurtailment.NewService(store))

	interval := uint32(600)
	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid:               store.event.EventUUID.String(),
			RestoreBatchIntervalSec: &interval,
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.updateCalls, "Forbidden must fire before the store update")
}

// TestHandler_UpdateCurtailmentEvent_ResponseCarriesScopeAndTargets: the
// post-update response must use the same fully-populated shape as the read
// endpoints — scope, mode params, decision snapshot, and targets — so a
// client merging the response into a cached event does not silently drop
// structured fields. Mirrors StopCurtailment's response wiring.
func TestHandler_UpdateCurtailmentEvent_ResponseCarriesScopeAndTargets(t *testing.T) {
	t.Parallel()
	store := newUpdateStubStore(models.EventStateActive)
	store.event.ScopeType = models.ScopeTypeWholeOrg
	store.targets = []*models.Target{
		{
			CurtailmentEventID: store.event.ID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateConfirmed,
			DesiredState:       models.DesiredStateCurtailed,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	newReason := "operator update"
	resp, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: store.event.EventUUID.String(),
			Reason:    &newReason,
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.NotNil(t, resp.Msg.Event.Scope, "response must carry the persisted scope so cached clients keep it")
	require.Len(t, resp.Msg.Event.Targets, 1, "response must carry persisted targets")
	assert.Equal(t, "miner-1", resp.Msg.Event.Targets[0].DeviceIdentifier)
}
