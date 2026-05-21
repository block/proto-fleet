package curtailment

import (
	"context"
	"encoding/json"
	"errors"
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

// listHandlerTestCursorFixture is the opaque next-page cursor returned
// by the stub store. Hoisted out of the inline struct literal so gosec's
// hardcoded-credentials heuristic doesn't conflate a PageToken string
// field with a real credential.
const listHandlerTestCursorFixture = "opaque-next-cursor"

// listStubStore implements interfaces.CurtailmentStore for ListCurtailment
// handler tests. ListEvents is the only method tests configure; the rest
// panic so an unintended path is loud rather than silently default-valuing.
type listStubStore struct {
	events        []*models.Event
	nextPageToken string
	err           error
	lastParams    interfaces.ListEventsParams
}

func (s *listStubStore) ListEvents(_ context.Context, params interfaces.ListEventsParams) ([]*models.Event, string, error) {
	s.lastParams = params
	if s.err != nil {
		return nil, "", s.err
	}
	return s.events, s.nextPageToken, nil
}

func (s *listStubStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised by List handler tests")
}
func (s *listStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by List handler tests")
}
func (s *listStubStore) ListRecentlyResolvedCurtailedDevices(context.Context, int64, int32) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by List handler tests")
}
func (s *listStubStore) ListCandidates(context.Context, int64, []string) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by List handler tests")
}
func (s *listStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by List handler tests")
}
func (s *listStubStore) GetEventByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventByUUID not exercised by List handler tests")
}
func (s *listStubStore) GetActiveEvent(context.Context, int64) (*models.Event, error) {
	panic("GetActiveEvent not exercised by List handler tests")
}
func (s *listStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	panic("ListTargetsByEvent not exercised by List handler tests")
}
func (s *listStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by List handler tests")
}
func (s *listStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by List handler tests")
}
func (s *listStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by List handler tests")
}
func (s *listStubStore) UpdateEventState(context.Context, int64, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by List handler tests")
}
func (s *listStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by List handler tests")
}
func (s *listStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by List handler tests")
}
func (s *listStubStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by List handler tests")
}
func (s *listStubStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, error) {
	panic("AdminTerminateEvent not exercised by List handler tests")
}
func (s *listStubStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by List handler tests")
}
func (s *listStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by List handler tests")
}

func sessionCtx(orgID int64) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	})
}

// TestHandler_ListCurtailmentEvents_HappyPath: a single event with a
// trimmed decision snapshot survives the handler → service → store hop,
// next_page_token round-trips, and the per-target heavy payload is
// intentionally absent.
func TestHandler_ListCurtailmentEvents_HappyPath(t *testing.T) {
	t.Parallel()
	store := &listStubStore{
		events: []*models.Event{
			{
				ID:                      1,
				EventUUID:               uuid.New(),
				OrgID:                   42,
				State:                   models.EventStateCompleted,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "test",
			},
		},
		nextPageToken: listHandlerTestCursorFixture,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{
		PageSize: 20,
	}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, store.events[0].EventUUID.String(), resp.Msg.Events[0].EventUuid)
	assert.Empty(t, resp.Msg.Events[0].Targets, "list-view response must not include per-target rows")
	assert.Equal(t, listHandlerTestCursorFixture, resp.Msg.NextPageToken)
	// Org from session attaches to the store call; not the request.
	assert.Equal(t, int64(42), store.lastParams.OrgID)
}

// TestHandler_ListCurtailmentEvents_StateFilterForwards: the proto enum
// filter maps to the canonical string sentinel the store expects.
func TestHandler_ListCurtailmentEvents_StateFilterForwards(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{
		StateFilter: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING,
	}))
	require.NoError(t, err)
	assert.Equal(t, models.EventStateRestoring, store.lastParams.StateFilter)
}

// TestHandler_ListCurtailmentEvents_UnspecifiedFilterMeansAll: the
// UNSPECIFIED enum value collapses to the empty-string "no filter"
// sentinel — the store sees an empty string, not a literal "unspecified".
func TestHandler_ListCurtailmentEvents_UnspecifiedFilterMeansAll(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{
		StateFilter: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_UNSPECIFIED,
	}))
	require.NoError(t, err)
	assert.Equal(t, models.EventState(""), store.lastParams.StateFilter)
}

// TestHandler_ListCurtailmentEvents_RejectsMissingSession: missing
// session.Info on a session-auth path remaps to Unauthenticated.
func TestHandler_ListCurtailmentEvents_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(t.Context(), connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

// TestHandler_ListCurtailmentEvents_PropagatesStoreError: a store-level
// failure surfaces as the wrapped fleeterror — no silent empty list.
func TestHandler_ListCurtailmentEvents_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := &listStubStore{err: errors.New("db down")}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

// TestHandler_ListCurtailmentEvents_TrimsDecisionSnapshot: the per-device
// skipped array is replaced by aggregate reason counts so list responses
// stay bounded on 10K-miner events.
func TestHandler_ListCurtailmentEvents_TrimsDecisionSnapshot(t *testing.T) {
	t.Parallel()
	rawSnapshot := map[string]any{
		"candidate_min_power_w":  1500,
		"estimated_reduction_kw": 12.5,
		"selected_count":         42,
		"skipped": []map[string]string{
			{"device_identifier": "m1", "reason": "phantom_load_no_hash"},
			{"device_identifier": "m2", "reason": "phantom_load_no_hash"},
			{"device_identifier": "m3", "reason": "stale_telemetry"},
		},
	}
	snapshotJSON, err := json.Marshal(rawSnapshot)
	require.NoError(t, err)

	store := &listStubStore{
		events: []*models.Event{{
			ID:                   1,
			EventUUID:            uuid.New(),
			OrgID:                42,
			State:                models.EventStateCompleted,
			Mode:                 models.ModeFixedKw,
			Strategy:             models.StrategyLeastEfficientFirst,
			Level:                models.LevelFull,
			Priority:             models.PriorityNormal,
			DecisionSnapshotJSON: snapshotJSON,
		}},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)

	snap := resp.Msg.Events[0].DecisionSnapshot
	require.NotNil(t, snap)
	fields := snap.GetFields()
	assert.NotContains(t, fields, "skipped", "per-device skipped array must be removed from the list view")
	require.Contains(t, fields, "skipped_aggregate")
	agg := fields["skipped_aggregate"].GetStructValue().GetFields()
	assert.Equal(t, float64(2), agg["phantom_load_no_hash"].GetNumberValue())
	assert.Equal(t, float64(1), agg["stale_telemetry"].GetNumberValue())
}
