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
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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

func (s *updateStubStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised by Update handler tests")
}
func (s *updateStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by Update handler tests")
}
func (s *updateStubStore) ListRecentlyResolvedCurtailedDevices(context.Context, int64, int32) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by Update handler tests")
}
func (s *updateStubStore) ListCandidates(context.Context, int64, []string) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by Update handler tests")
}
func (s *updateStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by Update handler tests")
}
func (s *updateStubStore) GetActiveEvent(context.Context, int64) (*models.Event, error) {
	panic("GetActiveEvent not exercised by Update handler tests")
}
func (s *updateStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	panic("ListTargetsByEvent not exercised by Update handler tests")
}
func (s *updateStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by Update handler tests")
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
func (s *updateStubStore) UpdateEventState(context.Context, int64, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by Update handler tests")
}
func (s *updateStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by Update handler tests")
}
func (s *updateStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by Update handler tests")
}

func updateSessionCtx(orgID int64, role string) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           role,
	})
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

	_, err := h.UpdateCurtailmentEvent(
		updateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.UpdateCurtailmentEventRequest{
			EventUuid: store.event.EventUUID.String(),
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
