package curtailment

import (
	"context"
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
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// stopStubStore is a focused fake for Stop handler tests. Only the methods
// Service.Stop touches are wired; the rest panic so an unintended path is
// loud rather than zero-valuing.
type stopStubStore struct {
	event   *models.Event
	targets []*models.Target

	getEventErr       error
	listTargetsErr    error
	beginRestoreErr   error
	beginRestoreCalls int
}

func (s *stopStubStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised by Stop handler tests")
}
func (s *stopStubStore) UpdateOrgConfigPostEventCooldown(context.Context, int64, int32) (*models.OrgConfig, error) {
	panic("UpdateOrgConfigPostEventCooldown not exercised by Stop handler tests")
}
func (s *stopStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by Stop handler tests")
}
func (s *stopStubStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailmentTargetDevices not exercised by Stop handler tests")
}
func (s *stopStubStore) ListRecentlyResolvedCurtailedDevices(context.Context, int64, int32) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by Stop handler tests")
}
func (s *stopStubStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised by Stop handler tests")
}
func (s *stopStubStore) ListCandidates(context.Context, interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by Stop handler tests")
}
func (s *stopStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by Stop handler tests")
}
func (s *stopStubStore) ClaimClosedLoopFullFleetTargets(context.Context, int64, []models.InsertTargetParams) ([]*models.Target, error) {
	panic("ClaimClosedLoopFullFleetTargets not exercised by Stop handler tests")
}
func (s *stopStubStore) GetEventByUUID(_ context.Context, _ int64, _ uuid.UUID) (*models.Event, error) {
	if s.getEventErr != nil {
		return nil, s.getEventErr
	}
	return s.event, nil
}
func (s *stopStubStore) GetEventDetailByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventDetailByUUID not exercised by Stop handler tests")
}
func (s *stopStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised by Stop handler tests")
}
func (s *stopStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	if s.listTargetsErr != nil {
		return nil, s.listTargetsErr
	}
	return s.targets, nil
}
func (s *stopStubStore) ListTargetsByEventPage(context.Context, interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	panic("ListTargetsByEventPage not exercised by Stop handler tests")
}
func (s *stopStubStore) ListTargetSiteIDsByEvent(context.Context, int64, uuid.UUID) ([]int64, bool, error) {
	panic("ListTargetSiteIDsByEvent not exercised by Stop handler tests")
}
func (s *stopStubStore) GetTargetRollupByEvent(context.Context, int64, uuid.UUID) (*models.TargetRollup, error) {
	panic("GetTargetRollupByEvent not exercised by Stop handler tests")
}
func (s *stopStubStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by Stop handler tests")
}
func (s *stopStubStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by Stop handler tests")
}
func (s *stopStubStore) AdminTerminateEvent(context.Context, int64, uuid.UUID, models.EventState, string) (*models.Event, bool, error) {
	panic("AdminTerminateEvent not exercised by Stop handler tests")
}
func (s *stopStubStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by Stop handler tests")
}
func (s *stopStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by Stop handler tests")
}
func (s *stopStubStore) BeginRestoreTransition(_ context.Context, _ int64, eventUUID uuid.UUID, _ interfaces.BeginRestoreTransitionParams) (*models.Event, error) {
	s.beginRestoreCalls++
	if s.beginRestoreErr != nil {
		return nil, s.beginRestoreErr
	}
	updated := *s.event
	updated.State = models.EventStateRestoring
	updated.EventUUID = eventUUID
	for _, target := range s.targets {
		target.State = models.TargetStatePending
		target.DesiredState = models.DesiredStateActive
	}
	return &updated, nil
}
func (s *stopStubStore) BeginRecurtailTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRecurtailTransition not exercised (no gRPC re-curtail endpoint)")
}
func (s *stopStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised")
}
func (s *stopStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised")
}
func (s *stopStubStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised")
}
func (s *stopStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised")
}
func (s *stopStubStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised")
}
func (s *stopStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised")
}

func newStopStubStore() *stopStubStore {
	startedAt := time.Now().Add(-2 * time.Hour)
	eventUUID := uuid.New()
	return &stopStubStore{
		event: &models.Event{
			ID:                      99,
			EventUUID:               eventUUID,
			OrgID:                   42,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
			StartedAt:               &startedAt,
			Reason:                  "operator stop test",
		},
		targets: []*models.Target{
			{DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
			{DeviceIdentifier: "m2", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
		},
	}
}

func stopSessionCtxWithPerms(t *testing.T, orgID int64, role string, perms ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
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

func TestHandler_StopCurtailment_HappyPath(t *testing.T) {
	t.Parallel()

	store := newStopStubStore()
	h := NewHandler(curtailment.NewService(store))

	ctx := stopSessionCtxWithPerms(t, 42, "OPERATOR", authz.PermCurtailmentManage)

	resp, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: store.event.EventUUID.String(),
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING, resp.Msg.Event.State)
	assert.Equal(t, store.event.EventUUID.String(), resp.Msg.Event.EventUuid)
	require.Len(t, resp.Msg.Event.Targets, 2)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING, resp.Msg.Event.Targets[0].State)
	assert.Equal(t, pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_ACTIVE, resp.Msg.Event.Targets[0].DesiredState)
	assert.Equal(t, int32(2), resp.Msg.Event.TargetRollup.Pending)
	assert.Equal(t, int32(2), resp.Msg.Event.TargetRollup.Total)
	assert.Equal(t, 1, store.beginRestoreCalls)
}

func TestHandler_StopCurtailment_RequiresCurtailmentManage(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(curtailment.NewService(store))

	for _, tc := range []struct {
		name        string
		permissions []string
	}{
		{"caller without curtailment:manage is rejected", []string{authz.PermCurtailmentRead}},
		{"empty permissions set is rejected", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := h.StopCurtailment(
				stopSessionCtxWithPerms(t, 42, "OPERATOR", tc.permissions...),
				connect.NewRequest(&pb.StopCurtailmentRequest{EventUuid: store.event.EventUUID.String()}),
			)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
			assert.Equal(t, 0, store.beginRestoreCalls)
		})
	}
}

func TestHandler_StopCurtailment_UsesSiteScopedEventPermission(t *testing.T) {
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
		{"org permission without site narrowing allows stop", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage)}, 0, 1},
		{"matching site narrowing allows stop", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, 0, 1},
		{"site-only permission denies stop", []authz.Assignment{testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, connect.CodePermissionDenied, 0},
		{"site narrowing without manage denies stop", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite)}, connect.CodePermissionDenied, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newStopStubStore()
			store.event.ScopeType = models.ScopeTypeSite
			store.event.ScopeJSON = siteScopeJSON(t, allowedSite)
			h := NewHandler(curtailment.NewService(store))

			ctx := testSessionCtxWithAssignments(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: orgID,
				UserID:         9,
				Role:           "OPERATOR",
			}, tc.assignments...)

			_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
				EventUuid: store.event.EventUUID.String(),
			}))

			if tc.wantCode == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			}
			assert.Equal(t, tc.wantCalls, store.beginRestoreCalls)
		})
	}
}

func TestHandler_StopCurtailment_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(curtailment.NewService(store))

	_, err := h.StopCurtailment(t.Context(), connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: store.event.EventUUID.String(),
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.beginRestoreCalls)
}

func TestHandler_StopCurtailment_RejectsMalformedUUID(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(curtailment.NewService(store))

	ctx := stopSessionCtxWithPerms(t, 42, "OPERATOR", authz.PermCurtailmentManage)

	_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: "not-a-uuid",
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestHandler_StopCurtailment_ForceRequiresAdmin(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(curtailment.NewService(store))

	ctx := stopSessionCtxWithPerms(t, 42, "OPERATOR" /* non-Admin */, authz.PermCurtailmentManage)

	_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: store.event.EventUUID.String(),
		Force:     true,
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.beginRestoreCalls,
		"role gate must fail before reaching the service")
}

func TestHandler_StopCurtailment_AdminForcePassesThrough(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-30 * time.Second)
	store := newStopStubStore()
	store.event.MinCurtailedDurationSec = 600 // gate would block without force
	store.event.StartedAt = &startedAt
	h := NewHandler(curtailment.NewService(store))

	ctx := stopSessionCtxWithPerms(t, 42, "ADMIN", authz.PermCurtailmentManage)

	_, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: store.event.EventUUID.String(),
		Force:     true,
	}))
	require.NoError(t, err)
	assert.Equal(t, 1, store.beginRestoreCalls,
		"admin force=true must bypass the min-duration gate")
}

// TestHandler_StopCurtailment_ListTargetsErrorPropagates pins the post-Stop
// read path: Stop completes the transition (beginRestoreCalls advances), then
// the handler's ListTargetsByEvent call propagates the store error to the
// caller instead of silently returning an event with no targets.
func TestHandler_StopCurtailment_ListTargetsErrorPropagates(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	store.listTargetsErr = errors.New("simulated targets read failure")
	h := NewHandler(curtailment.NewService(store))

	ctx := stopSessionCtxWithPerms(t, 42, "OPERATOR", authz.PermCurtailmentManage)

	resp, err := h.StopCurtailment(ctx, connect.NewRequest(&pb.StopCurtailmentRequest{
		EventUuid: store.event.EventUUID.String(),
	}))
	require.Error(t, err)
	assert.Nil(t, resp,
		"a failed post-Stop targets read must not leak a partial response")
	assert.Equal(t, 1, store.beginRestoreCalls,
		"the transition already committed before the targets read failed")
}
