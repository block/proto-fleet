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
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
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
	activeEvents  []*models.Event
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
func (s *listStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	return s.activeEvents, nil
}
func (s *listStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	panic("ListTargetsByEvent not exercised by List handler tests")
}
func (s *listStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by List handler tests")
}
func (s *listStubStore) BeginRecurtailTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRecurtailTransition not exercised by List handler tests")
}
func (s *listStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by List handler tests")
}
func (s *listStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by List handler tests")
}
func (s *listStubStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by List handler tests")
}
func (s *listStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by List handler tests")
}
func (s *listStubStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised by List handler tests")
}
func (s *listStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by List handler tests")
}
func (s *listStubStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by List handler tests")
}
func (s *listStubStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by List handler tests")
}
func (s *listStubStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by List handler tests")
}
func (s *listStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by List handler tests")
}

func sessionCtx(orgID int64) context.Context {
	return sessionCtxWithPerms(orgID, authz.PermCurtailmentRead)
}

// sessionCtxWithPerms returns a session context carrying the supplied
// effective permissions; tests that need a permission-denied path pass
// no permissions (or only unrelated ones) here.
func sessionCtxWithPerms(orgID int64, perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}}))
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

// TestHandler_ListActiveCurtailments_ReturnsActiveEvents: multiple concurrent
// non-terminal events round-trip through the handler, and the per-target heavy
// payload is intentionally absent (use GetActiveCurtailment for detail).
func TestHandler_ListActiveCurtailments_ReturnsActiveEvents(t *testing.T) {
	t.Parallel()
	source, reference, key := "opensearch", "alert-1", "retry-key"
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:                      1,
				EventUUID:               uuid.New(),
				OrgID:                   42,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "site-a",
				ExternalSource:          &source,
				ExternalReference:       &reference,
				IdempotencyKey:          &key,
			},
			{
				ID:                      2,
				EventUUID:               uuid.New(),
				OrgID:                   42,
				State:                   models.EventStatePending,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "site-b",
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ListActiveCurtailments(sessionCtx(42), connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 2)
	assert.Equal(t, store.activeEvents[0].EventUUID.String(), resp.Msg.Events[0].EventUuid)
	assert.Equal(t, store.activeEvents[1].EventUUID.String(), resp.Msg.Events[1].EventUuid)
	assert.Empty(t, resp.Msg.Events[0].Targets, "list-active response must not include per-target rows")
	// Replay handles are scrubbed from the list view, like the history list.
	assert.Empty(t, resp.Msg.Events[0].ExternalSource)
	assert.Empty(t, resp.Msg.Events[0].ExternalReference)
	assert.Empty(t, resp.Msg.Events[0].IdempotencyKey)
}

func TestHandler_ListCurtailmentEvents_HidesReplayHandles(t *testing.T) {
	t.Parallel()
	key := "start-retry-key"
	source := "opensearch"
	reference := "alert-123"
	store := &listStubStore{
		events: []*models.Event{{
			ID:                      1,
			EventUUID:               uuid.New(),
			OrgID:                   42,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchIntervalSec: 120,
			Reason:                  "test",
			ExternalSource:          &source,
			ExternalReference:       &reference,
			IdempotencyKey:          &key,
		}},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)

	ev := resp.Msg.Events[0]
	assert.Empty(t, ev.ExternalSource)
	assert.Empty(t, ev.ExternalReference)
	assert.Empty(t, ev.IdempotencyKey)
}

// TestHandler_ListCurtailmentEvents_StateFiltersForward: proto enum filters
// map to the canonical string sentinels the store expects.
func TestHandler_ListCurtailmentEvents_StateFiltersForward(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{
		StateFilters: []pb.CurtailmentEventState{
			pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING,
			pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED,
		},
	}))
	require.NoError(t, err)
	assert.Equal(t, []models.EventState{models.EventStateRestoring, models.EventStateCompleted}, store.lastParams.StateFilters)
}

// TestHandler_ListCurtailmentEvents_LegacyStateFilterForwards: old clients
// using the singular filter still get the same store-level filter set.
func TestHandler_ListCurtailmentEvents_LegacyStateFilterForwards(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ListCurtailmentEvents(sessionCtx(42), connect.NewRequest(&pb.ListCurtailmentEventsRequest{
		StateFilter: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING,
	}))
	require.NoError(t, err)
	assert.Equal(t, []models.EventState{models.EventStateRestoring}, store.lastParams.StateFilters)
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
	assert.Empty(t, store.lastParams.StateFilters)
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

// TestHandler_ListCurtailmentEvents_RejectsWithoutCurtailmentRead: an
// authenticated caller without curtailment:read in their effective
// permissions is denied before any store work.
func TestHandler_ListCurtailmentEvents_RejectsWithoutCurtailmentRead(t *testing.T) {
	t.Parallel()
	store := &listStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	// Effective permissions present but lack curtailment:read; the gate
	// must deny rather than fall through to the store.
	_, err := h.ListCurtailmentEvents(
		sessionCtxWithPerms(42 /* no curtailment:read */),
		connect.NewRequest(&pb.ListCurtailmentEventsRequest{}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
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

// The SQL trim (decision_snapshot_jsonb's `skipped` → `skipped_aggregate`)
// rides through the handler untouched onto the wire.
func TestHandler_ListCurtailmentEvents_HydratesTrimmedDecisionSnapshot(t *testing.T) {
	t.Parallel()
	// Pre-trimmed snapshot matching what ListCurtailmentEventsForOrg
	// returns in production after the SQL CASE expression has stripped
	// `skipped` and computed `skipped_aggregate`.
	trimmedSnapshot := map[string]any{
		"candidate_min_power_w":  1500,
		"estimated_reduction_kw": 12.5,
		"selected_count":         42,
		"skipped_aggregate": map[string]any{
			"phantom_load_no_hash": float64(2),
			"stale_telemetry":      float64(1),
		},
	}
	snapshotJSON, err := json.Marshal(trimmedSnapshot)
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
	assert.NotContains(t, fields, "skipped", "per-device skipped array must not appear in the list view (SQL strips it before the handler runs)")
	require.Contains(t, fields, "skipped_aggregate")
	agg := fields["skipped_aggregate"].GetStructValue().GetFields()
	assert.Equal(t, float64(2), agg["phantom_load_no_hash"].GetNumberValue())
	assert.Equal(t, float64(1), agg["stale_telemetry"].GetNumberValue())
}
