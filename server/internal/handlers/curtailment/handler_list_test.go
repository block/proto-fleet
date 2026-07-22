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
	events               []*models.Event
	activeEvents         []*models.Event
	eventByUUID          map[uuid.UUID]*models.Event
	eventDetailByUUID    map[uuid.UUID]*models.Event
	targetsByUUID        map[uuid.UUID][]*models.Target
	targetSiteIDsByUUID  map[uuid.UUID][]int64
	incompleteTargetSite map[uuid.UUID]bool
	targetRollupByUUID   map[uuid.UUID]*models.TargetRollup
	eventsByPageToken    map[string][]*models.Event
	nextByPageToken      map[string]string
	nextPageToken        string
	targetNextPageToken  string
	err                  error
	lastParams           interfaces.ListEventsParams
	listPageTokens       []string
	lastTargetPageParams interfaces.ListTargetsByEventPageParams
	lastGetOrgID         int64
	lastGetUUID          uuid.UUID
	coverageBatchCalls   int
	lastCoverageBatch    []uuid.UUID
}

func (s *listStubStore) ListEvents(_ context.Context, params interfaces.ListEventsParams) ([]*models.Event, string, error) {
	s.lastParams = params
	s.listPageTokens = append(s.listPageTokens, params.PageToken)
	if s.err != nil {
		return nil, "", s.err
	}
	if s.eventsByPageToken != nil {
		return s.eventsByPageToken[params.PageToken], s.nextByPageToken[params.PageToken], nil
	}
	return s.events, s.nextPageToken, nil
}

func (s *listStubStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised by List handler tests")
}
func (s *listStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by List handler tests")
}
func (s *listStubStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailmentTargetDevices not exercised by List handler tests")
}
func (s *listStubStore) ListRecentlyResolvedCurtailedDevices(
	context.Context,
	interfaces.ListRecentlyResolvedCurtailedDevicesParams,
) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by List handler tests")
}
func (s *listStubStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised by List handler tests")
}
func (s *listStubStore) ListCandidates(context.Context, interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by List handler tests")
}
func (s *listStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by List handler tests")
}
func (s *listStubStore) ClaimClosedLoopFullFleetTargets(
	context.Context,
	int64,
	int64,
	int32,
	[]models.InsertTargetParams,
) ([]*models.Target, error) {
	panic("ClaimClosedLoopFullFleetTargets not exercised by List handler tests")
}
func (s *listStubStore) ClaimAllPairedPolicyTargets(
	context.Context,
	int64,
	[]models.InsertTargetParams,
) (int64, error) {
	panic("ClaimAllPairedPolicyTargets not exercised by List handler tests")
}
func (s *listStubStore) BulkRefreshAllPairedTargetReadiness(
	context.Context,
	int64,
	models.EventState,
	[]interfaces.AllPairedReadinessUpdate,
) ([]string, error) {
	panic("BulkRefreshAllPairedTargetReadiness not exercised by List handler tests")
}
func (s *listStubStore) GetEventByUUID(_ context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	s.lastGetOrgID = orgID
	s.lastGetUUID = eventUUID
	if s.eventByUUID == nil {
		panic("GetEventByUUID not exercised by List handler tests")
	}
	ev, ok := s.eventByUUID[eventUUID]
	if !ok {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	return ev, nil
}
func (s *listStubStore) GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	s.lastGetOrgID = orgID
	s.lastGetUUID = eventUUID
	if s.eventDetailByUUID == nil {
		return s.GetEventByUUID(ctx, orgID, eventUUID)
	}
	ev, ok := s.eventDetailByUUID[eventUUID]
	if !ok {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	return ev, nil
}
func (s *listStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	return s.activeEvents, nil
}
func (s *listStubStore) ListTargetsByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if s.targetsByUUID == nil {
		panic("ListTargetsByEvent not exercised by List handler tests")
	}
	return s.targetsByUUID[eventUUID], nil
}
func (s *listStubStore) ListTargetsByEventPage(_ context.Context, params interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	s.lastTargetPageParams = params
	if s.targetsByUUID == nil {
		panic("ListTargetsByEventPage not exercised by List handler tests")
	}
	return s.targetsByUUID[params.EventUUID], s.targetNextPageToken, nil
}
func (s *listStubStore) ListTargetSiteCoverageByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) (models.TargetSiteCoverage, error) {
	if s.targetSiteIDsByUUID == nil {
		panic("ListTargetSiteCoverageByEvent not exercised by List handler tests")
	}
	siteIDs := append([]int64(nil), s.targetSiteIDsByUUID[eventUUID]...)
	complete := !s.incompleteTargetSite[eventUUID]
	mappedTargetCount := int64(len(siteIDs))
	targetCount := mappedTargetCount
	if !complete {
		targetCount++
	}
	return models.TargetSiteCoverage{
		SiteIDs:           siteIDs,
		Complete:          complete,
		TargetCount:       targetCount,
		MappedTargetCount: mappedTargetCount,
	}, nil
}
func (s *listStubStore) ListTargetSiteCoverageByEvents(_ context.Context, _ int64, eventUUIDs []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	if s.targetSiteIDsByUUID == nil {
		panic("ListTargetSiteCoverageByEvents not exercised by List handler tests")
	}
	s.coverageBatchCalls++
	s.lastCoverageBatch = append([]uuid.UUID(nil), eventUUIDs...)
	coverageByEvent := make(map[uuid.UUID]models.TargetSiteCoverage, len(eventUUIDs))
	for _, eventUUID := range eventUUIDs {
		coverage, err := s.ListTargetSiteCoverageByEvent(context.Background(), 0, eventUUID)
		if err != nil {
			return nil, err
		}
		coverageByEvent[eventUUID] = coverage
	}
	return coverageByEvent, nil
}
func (s *listStubStore) GetTargetRollupByEvent(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.TargetRollup, error) {
	if s.targetRollupByUUID != nil {
		if rollup, ok := s.targetRollupByUUID[eventUUID]; ok {
			return rollup, nil
		}
	}
	if s.targetsByUUID == nil {
		panic("GetTargetRollupByEvent not exercised by List handler tests")
	}
	rollup := &models.TargetRollup{}
	for _, target := range s.targetsByUUID[eventUUID] {
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
	rollup.Total = int64(len(s.targetsByUUID[eventUUID]))
	return rollup, nil
}
func (s *listStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID, interfaces.BeginRestoreTransitionParams) (*models.Event, error) {
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
func (s *listStubStore) RecordCurtailPendingDispatch(context.Context, int64, models.EventState, time.Time) error {
	panic("RecordCurtailPendingDispatch not exercised by List handler tests")
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
func (s *listStubStore) ForceReleaseEvent(context.Context, int64, uuid.UUID, string) (interfaces.ForceReleaseEventResult, error) {
	panic("ForceReleaseEvent not exercised by List handler tests")
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
		PageSize: 1,
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
// non-terminal events round-trip through the handler with their live target
// rollups, and the per-target heavy payload and decision snapshot are
// intentionally absent (use GetCurtailmentEvent for detail).
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
				DecisionSnapshotJSON:    []byte(`{"selected_count":10}`),
				TargetRollup: &models.TargetRollup{
					Pending:       1,
					Dispatched:    2,
					Confirmed:     4990,
					Drifted:       1,
					Resolved:      2,
					Released:      1,
					RestoreFailed: 1,
					Unavailable:   2,
					Total:         5000,
					UnavailableReasons: []models.TargetUnavailableReasonCount{
						{Reason: "offline", Count: 1},
						{Reason: "authentication_needed", Count: 1},
					},
				},
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
				TargetRollup:            &models.TargetRollup{},
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
	assert.Nil(t, resp.Msg.Events[0].DecisionSnapshot, "list-active response must not include the decision snapshot")
	// Live rollups ride along so active displays can show the event's current
	// target set instead of the event-start snapshot count.
	require.NotNil(t, resp.Msg.Events[0].TargetRollup)
	assert.Equal(t, int32(5000), resp.Msg.Events[0].TargetRollup.Total)
	assert.Equal(t, int32(4990), resp.Msg.Events[0].TargetRollup.Confirmed)
	assert.Equal(t, int32(2), resp.Msg.Events[0].TargetRollup.Unavailable)
	assert.Equal(t, int32(1), resp.Msg.Events[0].TargetRollup.RestoreFailed)
	assert.Equal(t, []*pb.CurtailmentUnavailableReason{
		{Reason: "offline", Count: 1},
		{Reason: "authentication_needed", Count: 1},
	}, resp.Msg.Events[0].TargetRollup.UnavailableReasons)
	require.NotNil(t, resp.Msg.Events[1].TargetRollup, "target-less events carry a zeroed rollup")
	assert.Equal(t, int32(0), resp.Msg.Events[1].TargetRollup.Total)
	// Replay handles are scrubbed from the list view, like the history list.
	assert.Empty(t, resp.Msg.Events[0].ExternalSource)
	assert.Empty(t, resp.Msg.Events[0].ExternalReference)
	assert.Empty(t, resp.Msg.Events[0].IdempotencyKey)
}

// TestHandler_ListActiveCurtailments_OmitsWholeOrgRollupForNarrowedRead: a
// whole-org event stays visible on the plain org grant, but its live rollup
// aggregates target counts across every site — including sites the caller's
// read grant is narrowed away from — so the rollup requires unnarrowed
// org-wide read. Site-scoped events at permitted sites keep their rollups.
func TestHandler_ListActiveCurtailments_OmitsWholeOrgRollupForNarrowedRead(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		narrowSite  = int64(8)
	)
	wholeOrgUUID := uuid.New()
	allowedSiteUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:                      1,
				EventUUID:               wholeOrgUUID,
				OrgID:                   orgID,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFullFleet,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "whole-org",
				ScopeType:               models.ScopeTypeWholeOrg,
				TargetRollup:            &models.TargetRollup{Confirmed: 4990, Pending: 10, Total: 5000},
			},
			{
				ID:                      2,
				EventUUID:               allowedSiteUUID,
				OrgID:                   orgID,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "allowed-site",
				ScopeType:               models.ScopeTypeSite,
				ScopeJSON:               siteScopeJSON(t, allowedSite),
				TargetRollup:            &models.TargetRollup{Confirmed: 3, Total: 3},
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	narrowedCtx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead),
		testSiteAssignment(allowedSite, authz.PermCurtailmentRead),
		testSiteAssignment(narrowSite))

	resp, err := h.ListActiveCurtailments(narrowedCtx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 2)
	assert.Equal(t, wholeOrgUUID.String(), resp.Msg.Events[0].EventUuid,
		"whole-org events stay visible so narrowed operators learn their sites are curtailed")
	assert.Nil(t, resp.Msg.Events[0].TargetRollup,
		"whole-org rollup aggregates narrowed sites; it must not ship without org-wide read")
	require.NotNil(t, resp.Msg.Events[1].TargetRollup,
		"site-scoped rollups at permitted sites are unaffected by narrowing elsewhere")
	assert.Equal(t, int32(3), resp.Msg.Events[1].TargetRollup.Total)

	orgWideCtx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead),
		testSiteAssignment(allowedSite, authz.PermCurtailmentRead),
		testSiteAssignment(narrowSite, authz.PermCurtailmentRead))

	resp, err = h.ListActiveCurtailments(orgWideCtx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 2)
	require.NotNil(t, resp.Msg.Events[0].TargetRollup,
		"matching narrowed grants keep org read effective everywhere, so the rollup ships")
	assert.Equal(t, int32(5000), resp.Msg.Events[0].TargetRollup.Total)
}

func TestHandler_ListActiveCurtailments_FiltersSiteScopedEvents(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	orgScopedUUID := uuid.New()
	allowedSiteUUID := uuid.New()
	deniedSiteUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:                      1,
				EventUUID:               orgScopedUUID,
				OrgID:                   orgID,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "org-scoped",
			},
			{
				ID:                      2,
				EventUUID:               allowedSiteUUID,
				OrgID:                   orgID,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "allowed-site",
				ScopeType:               models.ScopeTypeSite,
				ScopeJSON:               siteScopeJSON(t, allowedSite),
			},
			{
				ID:                      3,
				EventUUID:               deniedSiteUUID,
				OrgID:                   orgID,
				State:                   models.EventStateActive,
				Mode:                    models.ModeFixedKw,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "denied-site",
				ScopeType:               models.ScopeTypeSite,
				ScopeJSON:               siteScopeJSON(t, deniedSite),
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListActiveCurtailments(ctx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.Events, 2)
	assert.Equal(t, orgScopedUUID.String(), resp.Msg.Events[0].EventUuid)
	assert.Equal(t, allowedSiteUUID.String(), resp.Msg.Events[1].EventUuid)
}

func TestHandler_ListCurtailmentEvents_FiltersSiteScopedEventsAcrossPages(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	deniedSiteUUID := uuid.New()
	allowedSiteUUID := uuid.New()
	store := &listStubStore{
		eventsByPageToken: map[string][]*models.Event{
			"": {
				{
					ID:        3,
					EventUUID: deniedSiteUUID,
					OrgID:     orgID,
					State:     models.EventStateActive,
					ScopeType: models.ScopeTypeSite,
					ScopeJSON: siteScopeJSON(t, deniedSite),
					Reason:    "denied-site",
				},
			},
			"next": {
				{
					ID:        2,
					EventUUID: allowedSiteUUID,
					OrgID:     orgID,
					State:     models.EventStateCompleted,
					ScopeType: models.ScopeTypeSite,
					ScopeJSON: siteScopeJSON(t, allowedSite),
					Reason:    "allowed-site",
				},
			},
		},
		nextByPageToken: map[string]string{"": "next"},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{PageSize: 1}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, allowedSiteUUID.String(), resp.Msg.Events[0].EventUuid)
	assert.Empty(t, resp.Msg.NextPageToken)
	assert.Equal(t, []string{"", "next"}, store.listPageTokens)
}

func TestHandler_ListCurtailmentEvents_CapsPermissionFilteringScan(t *testing.T) {
	t.Parallel()
	const (
		orgID      = int64(42)
		deniedSite = int64(8)
	)
	deniedEvent := func(id int64) *models.Event {
		return &models.Event{
			ID:        id,
			EventUUID: uuid.New(),
			OrgID:     orgID,
			State:     models.EventStateCompleted,
			ScopeType: models.ScopeTypeSite,
			ScopeJSON: siteScopeJSON(t, deniedSite),
			Reason:    "denied-site",
		}
	}
	store := &listStubStore{
		eventsByPageToken: map[string][]*models.Event{
			"":       {deniedEvent(5)},
			"page-2": {deniedEvent(4)},
			"page-3": {deniedEvent(3)},
			"page-4": {deniedEvent(2)},
		},
		nextByPageToken: map[string]string{"": "page-2", "page-2": "page-3", "page-3": "page-4"},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{PageSize: 1}))
	require.NoError(t, err)

	assert.Empty(t, resp.Msg.Events)
	assert.Equal(t, "page-4", resp.Msg.NextPageToken)
	assert.Equal(t, []string{"", "page-2", "page-3"}, store.listPageTokens)
}

func TestHandler_ListCurtailmentEvents_FiltersEventsByFacilityFanSite(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
		firstFanID  = int64(31)
		secondFanID = int64(32)
	)
	store := &listStubStore{
		events: []*models.Event{
			{
				ID:                   5,
				EventUUID:            uuid.New(),
				OrgID:                orgID,
				State:                models.EventStateCompleted,
				ScopeType:            models.ScopeTypeSite,
				ScopeJSON:            siteScopeJSON(t, allowedSite),
				FacilityFanDeviceIDs: []int64{firstFanID},
			},
			{
				ID:                   4,
				EventUUID:            uuid.New(),
				OrgID:                orgID,
				State:                models.EventStateCompleted,
				ScopeType:            models.ScopeTypeSite,
				ScopeJSON:            siteScopeJSON(t, allowedSite),
				FacilityFanDeviceIDs: []int64{secondFanID},
			},
		},
	}
	profileStore := newHandlerResponseProfileStore()
	profileStore.infrastructureDevices[firstFanID] = models.ResponseProfileInfrastructureDevice{
		ID:      firstFanID,
		SiteID:  deniedSite,
		Enabled: true,
	}
	profileStore.infrastructureDevices[secondFanID] = models.ResponseProfileInfrastructureDevice{
		ID:      secondFanID,
		SiteID:  deniedSite,
		Enabled: true,
	}
	h := NewHandlerWithResponseProfiles(
		domainCurtailment.NewService(store),
		domainCurtailment.NewResponseProfileService(profileStore),
	)
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.NoError(t, err)

	assert.Empty(t, resp.Msg.Events)
	assert.Equal(t, 1, profileStore.infrastructureDeviceListCalls, "fan sites should be resolved once per event page")
}

func TestHandler_ListCurtailmentEvents_UsesPersistedFacilityFanSiteSnapshot(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		fanID       = int64(31)
	)
	store := &listStubStore{events: []*models.Event{{
		ID:                   5,
		EventUUID:            uuid.New(),
		OrgID:                orgID,
		State:                models.EventStateCompleted,
		ScopeType:            models.ScopeTypeSite,
		ScopeJSON:            siteScopeJSON(t, allowedSite),
		FacilityFanDeviceIDs: []int64{fanID},
		FacilityFanSiteIDs:   []int64{allowedSite},
	}}}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead))

	resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))

	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, store.events[0].EventUUID.String(), resp.Msg.Events[0].GetEventUuid())
}

func TestHandler_ListCurtailmentEvents_MissingFacilityFanRequiresOrgWideRead(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
		missingFanID = int64(31)
	)

	for _, test := range []struct {
		name       string
		orgWide    bool
		wantEvents int
	}{
		{name: "org-wide read", orgWide: true, wantEvents: 1},
		{
			name: "site-narrowed read",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := sessionCtx(orgID)
			if !test.orgWide {
				ctx = testSessionCtxWithAssignments(t, &session.Info{
					AuthMethod:     session.AuthMethodSession,
					OrganizationID: orgID,
					UserID:         9,
					Role:           "OPERATOR",
				}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(narrowedSite))
			}
			store := &listStubStore{events: []*models.Event{{
				ID:                   5,
				EventUUID:            uuid.New(),
				OrgID:                orgID,
				State:                models.EventStateCompleted,
				ScopeType:            models.ScopeTypeWholeOrg,
				FacilityFanDeviceIDs: []int64{missingFanID},
			}}}
			profileStore := newHandlerResponseProfileStore()
			h := NewHandlerWithResponseProfiles(
				domainCurtailment.NewService(store),
				domainCurtailment.NewResponseProfileService(profileStore),
			)

			resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
			require.NoError(t, err)
			assert.Len(t, resp.Msg.Events, test.wantEvents)
			assert.Equal(t, 1, profileStore.infrastructureDeviceListCalls)
		})
	}
}

func TestHandler_ListActiveCurtailments_FiltersDeviceListEventsByTargetSite(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	allowedUUID := uuid.New()
	deniedUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:        1,
				EventUUID: allowedUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "allowed-device",
			},
			{
				ID:        2,
				EventUUID: deniedUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "denied-device",
			},
		},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{
			allowedUUID: {allowedSite},
			deniedUUID:  {deniedSite},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListActiveCurtailments(ctx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, allowedUUID.String(), resp.Msg.Events[0].EventUuid)
}

func TestHandler_ListActiveCurtailments_BatchesDeviceListTargetSiteCoverage(t *testing.T) {
	t.Parallel()
	const (
		orgID     = int64(42)
		firstSite = int64(7)
	)
	firstUUID := uuid.New()
	secondUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:        1,
				EventUUID: firstUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "first-device",
			},
			{
				ID:        2,
				EventUUID: secondUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "second-device",
			},
		},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{
			firstUUID:  {firstSite},
			secondUUID: {},
		},
		incompleteTargetSite: map[uuid.UUID]bool{
			secondUUID: true,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ListActiveCurtailments(sessionCtx(orgID), connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.Events, 2)
	assert.Equal(t, 1, store.coverageBatchCalls)
	assert.ElementsMatch(t, []uuid.UUID{firstUUID, secondUUID}, store.lastCoverageBatch)
	assert.True(t, resp.Msg.Events[0].GetTargetSiteCoverage().GetComplete())
	assert.False(t, resp.Msg.Events[1].GetTargetSiteCoverage().GetComplete())
}

func TestHandler_ListActiveCurtailments_AllowsDeviceListEventsWithIncompleteTargetSitesForOrgWideRead(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	eventUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "unmapped-device",
			},
		},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{
			eventUUID: true,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := sessionCtx(orgID)

	resp, err := h.ListActiveCurtailments(ctx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, eventUUID.String(), resp.Msg.Events[0].EventUuid)
	coverage := resp.Msg.Events[0].GetTargetSiteCoverage()
	require.NotNil(t, coverage)
	assert.False(t, coverage.GetComplete())
	assert.Equal(t, uint32(1), coverage.GetTargetCount())
	assert.Equal(t, uint32(0), coverage.GetMappedTargetCount())
	assert.Equal(t, uint32(1), coverage.GetUnknownTargetCount())
}

func TestHandler_ListActiveCurtailments_FiltersDeviceListEventsWithIncompleteTargetSitesWhenOrgReadIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "unmapped-device",
			},
		},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{
			eventUUID: true,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(narrowedSite))

	resp, err := h.ListActiveCurtailments(ctx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Events)
}

func TestHandler_ListActiveCurtailments_UsesMixedSiteOnlyScopeJSON(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	eventUUID := uuid.New()
	store := &listStubStore{
		activeEvents: []*models.Event{
			{
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[7,8],"device_identifiers":null}`),
				Reason:    "multi-site full-fleet",
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	allowedCtx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead),
		testSiteAssignment(allowedSite, authz.PermCurtailmentRead),
		testSiteAssignment(deniedSite, authz.PermCurtailmentRead))
	resp, err := h.ListActiveCurtailments(allowedCtx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, eventUUID.String(), resp.Msg.Events[0].EventUuid)

	deniedCtx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead),
		testSiteAssignment(allowedSite, authz.PermCurtailmentRead),
		testSiteAssignment(deniedSite))
	resp, err = h.ListActiveCurtailments(deniedCtx, connect.NewRequest(&pb.ListActiveCurtailmentsRequest{}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Events)
}

func TestHandler_ListCurtailmentEvents_UsesTargetlessMixedScopeJSON(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	allowedUUID := uuid.New()
	deniedUUID := uuid.New()
	store := &listStubStore{
		events: []*models.Event{
			{
				ID:        1,
				EventUUID: allowedUUID,
				OrgID:     orgID,
				State:     models.EventStateCompleted,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[7],"device_identifiers":["allowed-miner"]}`),
				Reason:    "targetless allowed mixed scope",
			},
			{
				ID:        2,
				EventUUID: deniedUUID,
				OrgID:     orgID,
				State:     models.EventStateCompleted,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[7],"device_identifiers":["denied-miner"]}`),
				Reason:    "targetless denied mixed scope",
			},
		},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{},
	}
	profileStore := newHandlerResponseProfileStore()
	profileStore.deviceSites = map[string]*int64{
		"allowed-miner": ptrHandlerInt64(allowedSite),
		"denied-miner":  ptrHandlerInt64(deniedSite),
	}
	h := NewHandlerWithResponseProfiles(
		domainCurtailment.NewService(store),
		domainCurtailment.NewResponseProfileService(profileStore),
	)
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	resp, err := h.ListCurtailmentEvents(ctx, connect.NewRequest(&pb.ListCurtailmentEventsRequest{}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.Events, 1)
	assert.Equal(t, allowedUUID.String(), resp.Msg.Events[0].EventUuid)
}

func TestHandler_GetCurtailmentEvent_AllowsTargetlessDeviceListScope(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	eventUUID := uuid.New()
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateCompleted,
				ScopeType: models.ScopeTypeDeviceList,
				ScopeJSON: []byte(`{"device_identifiers":["skipped-miner"]}`),
				Reason:    "targetless device-list",
			},
		},
		targetsByUUID:        map[uuid.UUID][]*models.Target{eventUUID: {}},
		targetSiteIDsByUUID:  map[uuid.UUID][]int64{},
		targetRollupByUUID:   map[uuid.UUID]*models.TargetRollup{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.GetCurtailmentEvent(sessionCtx(orgID), connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid: eventUUID.String(),
	}))

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, eventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, eventUUID, store.lastTargetPageParams.EventUUID)
}

func TestHandler_GetCurtailmentEvent_DeniesTargetlessDeviceListScopeWhenOrgReadIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateCompleted,
				ScopeType: models.ScopeTypeDeviceList,
				ScopeJSON: []byte(`{"device_identifiers":["unknown-miner"]}`),
				Reason:    "targetless unresolved device-list",
			},
		},
		targetsByUUID:        map[uuid.UUID][]*models.Target{eventUUID: {}},
		targetSiteIDsByUUID:  map[uuid.UUID][]int64{},
		targetRollupByUUID:   map[uuid.UUID]*models.TargetRollup{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{},
	}
	h := NewHandlerWithResponseProfiles(
		domainCurtailment.NewService(store),
		domainCurtailment.NewResponseProfileService(newHandlerResponseProfileStore()),
	)
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(narrowedSite))

	_, err := h.GetCurtailmentEvent(ctx, connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid: eventUUID.String(),
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, uuid.Nil, store.lastTargetPageParams.EventUUID)
}

func TestHandler_GetCurtailmentEvent_UsesTargetSitesForDeviceListEvents(t *testing.T) {
	t.Parallel()
	const (
		orgID      = int64(42)
		deniedSite = int64(8)
	)
	eventUUID := uuid.New()
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "device-list",
			},
		},
		targetsByUUID: map[uuid.UUID][]*models.Target{eventUUID: {}},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{
			eventUUID: {deniedSite},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(deniedSite))

	_, err := h.GetCurtailmentEvent(ctx, connect.NewRequest(&pb.GetCurtailmentEventRequest{EventUuid: eventUUID.String()}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, uuid.Nil, store.lastTargetPageParams.EventUUID)
}

func TestHandler_GetCurtailmentEvent_DeniesIncompleteTargetSiteCoverageWhenOrgReadIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
				Reason:    "unmapped-device",
			},
		},
		targetsByUUID:        map[uuid.UUID][]*models.Target{eventUUID: {}},
		targetSiteIDsByUUID:  map[uuid.UUID][]int64{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{eventUUID: true},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           "OPERATOR",
	}, testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(narrowedSite))

	_, err := h.GetCurtailmentEvent(ctx, connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid: eventUUID.String(),
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, uuid.Nil, store.lastTargetPageParams.EventUUID)
}

func TestHandler_GetCurtailmentEvent_ReturnsTargetsWithPhaseSummaries(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	addedAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	curtailDispatchedAt := addedAt.Add(time.Minute)
	curtailCompletedAt := addedAt.Add(2 * time.Minute)
	restoreStartedAt := addedAt.Add(10 * time.Minute)
	restoreDispatchedAt := addedAt.Add(11 * time.Minute)
	restoreCompletedAt := addedAt.Add(12 * time.Minute)
	curtailBatch := "batch-curtail"
	restoreBatch := "batch-restore"
	targetCursorFixture := "opaque-target-cursor"
	nextTargetCursorFixture := "opaque-next-target-cursor"

	store := &listStubStore{
		targetNextPageToken: nextTargetCursorFixture,
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:                      1,
				EventUUID:               eventUUID,
				OrgID:                   42,
				State:                   models.EventStateCompleted,
				Mode:                    models.ModeFullFleet,
				Strategy:                models.StrategyLeastEfficientFirst,
				Level:                   models.LevelFull,
				Priority:                models.PriorityNormal,
				RestoreBatchSize:        10,
				RestoreBatchIntervalSec: 120,
				Reason:                  "test",
			},
		},
		targetsByUUID: map[uuid.UUID][]*models.Target{
			eventUUID: {
				{
					DeviceIdentifier: "miner-1",
					TargetType:       "miner",
					State:            models.TargetStateResolved,
					DesiredState:     models.DesiredStateActive,
					AddedAt:          addedAt,
					CurtailPhase: models.TargetPhaseSummary{
						Phase:        models.TargetPhaseCurtail,
						State:        models.TargetStateConfirmed,
						StartedAt:    &addedAt,
						DispatchedAt: &curtailDispatchedAt,
						BatchUUID:    &curtailBatch,
						CompletedAt:  &curtailCompletedAt,
					},
					RestorePhase: &models.TargetPhaseSummary{
						Phase:        models.TargetPhaseRestore,
						State:        models.TargetStateResolved,
						StartedAt:    &restoreStartedAt,
						DispatchedAt: &restoreDispatchedAt,
						BatchUUID:    &restoreBatch,
						CompletedAt:  &restoreCompletedAt,
					},
				},
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.GetCurtailmentEvent(sessionCtx(42), connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid:       eventUUID.String(),
		TargetPageSize:  1,
		TargetPageToken: targetCursorFixture,
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	require.Len(t, resp.Msg.Event.Targets, 1)
	target := resp.Msg.Event.Targets[0]
	assert.Equal(t, "miner-1", target.DeviceIdentifier)
	require.NotNil(t, target.CurtailPhase)
	assert.Equal(t, pb.CurtailmentTargetPhase_CURTAILMENT_TARGET_PHASE_CURTAIL, target.CurtailPhase.Phase)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_CONFIRMED, target.CurtailPhase.State)
	assert.Equal(t, curtailBatch, target.CurtailPhase.BatchUuid)
	require.NotNil(t, target.RestorePhase)
	assert.Equal(t, pb.CurtailmentTargetPhase_CURTAILMENT_TARGET_PHASE_RESTORE, target.RestorePhase.Phase)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_RESOLVED, target.RestorePhase.State)
	assert.Equal(t, restoreBatch, target.RestorePhase.BatchUuid)
	assert.Equal(t, nextTargetCursorFixture, resp.Msg.NextTargetPageToken)
	assert.Equal(t, int64(42), store.lastGetOrgID)
	assert.Equal(t, eventUUID, store.lastGetUUID)
	assert.Equal(t, interfaces.ListTargetsByEventPageParams{
		OrgID:     42,
		EventUUID: eventUUID,
		PageSize:  1,
		PageToken: targetCursorFixture,
	}, store.lastTargetPageParams)
}

func TestHandler_GetCurtailmentEvent_UsesSiteScopedEventPermission(t *testing.T) {
	t.Parallel()
	const (
		orgID       = int64(42)
		allowedSite = int64(7)
	)

	for _, tc := range []struct {
		name        string
		assignments []authz.Assignment
		wantCode    connect.Code
	}{
		{"org permission without site narrowing allows read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead)}, 0},
		{"matching site narrowing allows read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite, authz.PermCurtailmentRead)}, 0},
		{"site-only permission denies read", []authz.Assignment{testSiteAssignment(allowedSite, authz.PermCurtailmentRead)}, connect.CodePermissionDenied},
		{"site narrowing without read denies read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(allowedSite)}, connect.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eventUUID := uuid.New()
			store := &listStubStore{
				eventByUUID: map[uuid.UUID]*models.Event{
					eventUUID: {
						ID:        1,
						EventUUID: eventUUID,
						OrgID:     orgID,
						State:     models.EventStateActive,
						ScopeType: models.ScopeTypeSite,
						ScopeJSON: siteScopeJSON(t, allowedSite),
					},
				},
				targetsByUUID: map[uuid.UUID][]*models.Target{
					eventUUID: {},
				},
			}
			h := NewHandler(domainCurtailment.NewService(store))
			ctx := testSessionCtxWithAssignments(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: orgID,
				UserID:         9,
				Role:           "OPERATOR",
			}, tc.assignments...)

			_, err := h.GetCurtailmentEvent(ctx, connect.NewRequest(&pb.GetCurtailmentEventRequest{
				EventUuid: eventUUID.String(),
			}))

			if tc.wantCode == 0 {
				require.NoError(t, err)
				assert.Equal(t, eventUUID, store.lastTargetPageParams.EventUUID)
			} else {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
				assert.Equal(t, uuid.Nil, store.lastTargetPageParams.EventUUID)
			}
		})
	}
}

func TestHandler_GetCurtailmentEvent_AllowsIncompleteTargetSitesForOrgWideRead(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	eventUUID := uuid.New()
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: {
				ID:        1,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeDeviceList,
			},
		},
		targetsByUUID:       map[uuid.UUID][]*models.Target{eventUUID: {}},
		targetSiteIDsByUUID: map[uuid.UUID][]int64{eventUUID: {}},
		incompleteTargetSite: map[uuid.UUID]bool{
			eventUUID: true,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.GetCurtailmentEvent(sessionCtx(orgID), connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid: eventUUID.String(),
	}))

	require.NoError(t, err)
	assert.Equal(t, eventUUID, store.lastTargetPageParams.EventUUID)
	coverage := resp.Msg.Event.GetTargetSiteCoverage()
	require.NotNil(t, coverage)
	assert.False(t, coverage.GetComplete())
	assert.Equal(t, uint32(1), coverage.GetUnknownTargetCount())
}

func TestHandler_GetCurtailmentEvent_UsesBoundedSnapshotAndFullTargetRollup(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	largeSkipped := make([]map[string]string, 2048)
	for i := range largeSkipped {
		largeSkipped[i] = map[string]string{
			"device_identifier": "miner-skipped",
			"reason":            "maintenance",
		}
	}
	unboundedSnapshotJSON, err := json.Marshal(map[string]any{
		"selected_count": 1,
		"skipped":        largeSkipped,
	})
	require.NoError(t, err)
	boundedSnapshotJSON, err := json.Marshal(map[string]any{
		"selected_count": 1,
		"skipped_aggregate": map[string]int{
			"maintenance": len(largeSkipped),
		},
	})
	require.NoError(t, err)

	rawEvent := &models.Event{
		ID:                      1,
		EventUUID:               eventUUID,
		OrgID:                   42,
		State:                   models.EventStateCompletedWithFailures,
		Mode:                    models.ModeFullFleet,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityNormal,
		RestoreBatchSize:        10,
		RestoreBatchIntervalSec: 120,
		DecisionSnapshotJSON:    unboundedSnapshotJSON,
		Reason:                  "test",
	}
	detailEvent := *rawEvent
	detailEvent.DecisionSnapshotJSON = boundedSnapshotJSON
	store := &listStubStore{
		eventByUUID: map[uuid.UUID]*models.Event{
			eventUUID: rawEvent,
		},
		eventDetailByUUID: map[uuid.UUID]*models.Event{
			eventUUID: &detailEvent,
		},
		targetsByUUID: map[uuid.UUID][]*models.Target{
			eventUUID: {
				{
					DeviceIdentifier: "miner-1",
					TargetType:       "miner",
					State:            models.TargetStateResolved,
					DesiredState:     models.DesiredStateActive,
				},
			},
		},
		targetRollupByUUID: map[uuid.UUID]*models.TargetRollup{
			eventUUID: {
				Resolved:      1,
				RestoreFailed: 1,
				Total:         2,
			},
		},
		targetNextPageToken: "next-target-page",
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.GetCurtailmentEvent(sessionCtx(42), connect.NewRequest(&pb.GetCurtailmentEventRequest{
		EventUuid:      eventUUID.String(),
		TargetPageSize: 1,
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	require.Len(t, resp.Msg.Event.Targets, 1)
	assert.Equal(t, "next-target-page", resp.Msg.NextTargetPageToken)

	rollup := resp.Msg.Event.TargetRollup
	require.NotNil(t, rollup)
	assert.Equal(t, int32(1), rollup.Resolved)
	assert.Equal(t, int32(1), rollup.RestoreFailed)
	assert.Equal(t, int32(2), rollup.Total)

	require.NotNil(t, resp.Msg.Event.DecisionSnapshot)
	snapshot := resp.Msg.Event.DecisionSnapshot.AsMap()
	assert.NotContains(t, snapshot, "skipped")
	aggregate, ok := snapshot["skipped_aggregate"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(len(largeSkipped)), aggregate["maintenance"])
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
