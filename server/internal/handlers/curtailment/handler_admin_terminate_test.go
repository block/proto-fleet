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
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// adminTerminateStubStore is a focused fake for admin recovery handler
// tests. Only recovery methods are wired; the rest panic so an
// unintended path is loud rather than silently zero-valuing.
type adminTerminateStubStore struct {
	authEvent                      *models.Event
	result                         *models.Event
	transitioned                   bool
	idempotentReplay               bool
	err                            error
	calls                          int
	lastOrgID                      int64
	lastEventUUID                  uuid.UUID
	lastTargetState                models.EventState
	lastReason                     string
	forceReleaseCalls              int
	lastForceReleaseOrgID          int64
	lastForceReleaseUUID           uuid.UUID
	lastForceReleaseReason         string
	forceReleaseResult             *models.Event
	forceReleaseAutomationDisabled bool
	forceReleaseErr                error
	// Targets returned by ListTargetsByEvent; admin-terminate fetches them
	// post-terminate so the response shape mirrors the read endpoints.
	targets             []*models.Target
	targetsErr          error
	listTargetsCalls    int
	targetSiteIDs       []int64
	targetSitesComplete bool
}

func (s *adminTerminateStubStore) AdminTerminateEvent(_ context.Context, orgID int64, eventUUID uuid.UUID, targetState models.EventState, reason string) (*models.Event, bool, error) {
	s.calls++
	s.lastOrgID = orgID
	s.lastEventUUID = eventUUID
	s.lastTargetState = targetState
	s.lastReason = reason
	if s.err != nil {
		return nil, false, s.err
	}
	transitioned := !s.idempotentReplay
	s.transitioned = transitioned
	return s.result, transitioned, nil
}

func (s *adminTerminateStubStore) ForceReleaseEvent(_ context.Context, orgID int64, eventUUID uuid.UUID, reason string) (interfaces.ForceReleaseEventResult, error) {
	s.forceReleaseCalls++
	s.lastForceReleaseOrgID = orgID
	s.lastForceReleaseUUID = eventUUID
	s.lastForceReleaseReason = reason
	if s.forceReleaseErr != nil {
		return interfaces.ForceReleaseEventResult{}, s.forceReleaseErr
	}
	event := s.result
	if s.forceReleaseResult != nil {
		event = s.forceReleaseResult
	}
	return interfaces.ForceReleaseEventResult{
		Event:              event,
		SweptTargets:       int64(len(s.targets)),
		OwnershipReleased:  true,
		AutomationDisabled: s.forceReleaseAutomationDisabled,
	}, nil
}

func (s *adminTerminateStubStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	panic("GetOrgConfig not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListActiveCurtailedDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailedDevices not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListActiveCurtailmentTargetDevices(context.Context, int64) ([]string, error) {
	panic("ListActiveCurtailmentTargetDevices not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListRecentlyResolvedCurtailedDevices(
	context.Context,
	interfaces.ListRecentlyResolvedCurtailedDevicesParams,
) ([]string, error) {
	panic("ListRecentlyResolvedCurtailedDevices not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	panic("SiteBelongsToOrg not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListCandidates(context.Context, interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	panic("ListCandidates not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) InsertEventWithTargets(context.Context, models.InsertEventParams, []models.InsertTargetParams) (*models.InsertEventResult, error) {
	panic("InsertEventWithTargets not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ClaimClosedLoopFullFleetTargets(
	context.Context,
	int64,
	int64,
	int32,
	[]models.InsertTargetParams,
) ([]*models.Target, error) {
	panic("ClaimClosedLoopFullFleetTargets not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ClaimAllPairedPolicyTargets(
	context.Context,
	int64,
	[]models.InsertTargetParams,
) (int64, error) {
	panic("ClaimAllPairedPolicyTargets not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) BulkRefreshAllPairedTargetReadiness(
	context.Context,
	int64,
	models.EventState,
	[]interfaces.AllPairedReadinessUpdate,
) ([]string, error) {
	panic("BulkRefreshAllPairedTargetReadiness not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) GetEventByUUID(_ context.Context, _ int64, eventUUID uuid.UUID) (*models.Event, error) {
	if s.authEvent != nil {
		return s.authEvent, nil
	}
	if s.result != nil && s.result.EventUUID == eventUUID {
		return s.result, nil
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
}
func (s *adminTerminateStubStore) GetEventDetailByUUID(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("GetEventDetailByUUID not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListActiveEvents(context.Context, int64) ([]*models.Event, error) {
	panic("ListActiveEvents not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListTargetsByEvent(context.Context, int64, uuid.UUID) ([]*models.Target, error) {
	s.listTargetsCalls++
	return s.targets, s.targetsErr
}
func (s *adminTerminateStubStore) ListTargetsByEventPage(context.Context, interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	panic("ListTargetsByEventPage not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListTargetSiteCoverageByEvent(context.Context, int64, uuid.UUID) (models.TargetSiteCoverage, error) {
	siteIDs := append([]int64(nil), s.targetSiteIDs...)
	mappedTargetCount := int64(len(siteIDs))
	targetCount := mappedTargetCount
	if !s.targetSitesComplete {
		targetCount++
	}
	return models.TargetSiteCoverage{
		SiteIDs:           siteIDs,
		Complete:          s.targetSitesComplete,
		TargetCount:       targetCount,
		MappedTargetCount: mappedTargetCount,
	}, nil
}
func (s *adminTerminateStubStore) ListTargetSiteCoverageByEvents(context.Context, int64, []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	panic("ListTargetSiteCoverageByEvents not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) GetTargetRollupByEvent(context.Context, int64, uuid.UUID) (*models.TargetRollup, error) {
	panic("GetTargetRollupByEvent not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) BeginRestoreTransition(context.Context, int64, uuid.UUID, interfaces.BeginRestoreTransitionParams) (*models.Event, error) {
	panic("BeginRestoreTransition not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) BeginRecurtailTransition(context.Context, int64, uuid.UUID) (*models.Event, error) {
	panic("BeginRecurtailTransition not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) GetHeartbeat(context.Context) (*models.Heartbeat, error) {
	panic("GetHeartbeat not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListNonTerminalEvents(context.Context) ([]*models.Event, error) {
	panic("ListNonTerminalEvents not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListEligibleConfirmationTargets(context.Context) ([]models.ConfirmationTarget, error) {
	panic("ListEligibleConfirmationTargets not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) ListEvents(context.Context, interfaces.ListEventsParams) ([]*models.Event, string, error) {
	panic("ListEvents not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) UpdateEventState(context.Context, int64, models.EventState, models.EventState, *time.Time, *time.Time) error {
	panic("UpdateEventState not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) RecordCurtailPendingDispatch(context.Context, int64, models.EventState, time.Time) error {
	panic("RecordCurtailPendingDispatch not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) UpdateTargetState(context.Context, int64, string, interfaces.UpdateCurtailmentTargetStateParams) error {
	panic("UpdateTargetState not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) BumpTargetRetry(context.Context, int64, string) error {
	panic("BumpTargetRetry not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) UpsertHeartbeat(context.Context, interfaces.UpsertCurtailmentHeartbeatParams) error {
	panic("UpsertHeartbeat not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) UpdateOperatorFields(context.Context, int64, int64, interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	panic("UpdateOperatorFields not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) GetEventByIdempotencyKey(context.Context, int64, string) (*models.Event, error) {
	panic("GetEventByIdempotencyKey not exercised by AdminTerminate handler tests")
}
func (s *adminTerminateStubStore) GetEventByExternalReference(context.Context, int64, string, string) (*models.Event, error) {
	panic("GetEventByExternalReference not exercised by AdminTerminate handler tests")
}

func adminTerminateSessionCtx(orgID int64, role string) context.Context {
	return adminTerminateSessionCtxWithPerms(orgID, role, authz.PermCurtailmentManage)
}

// adminTerminateSessionCtxWithPerms lets the permission-denied test
// supply an explicit (possibly empty) permission set while keeping the
// happy-path callers compact.
func adminTerminateSessionCtxWithPerms(orgID int64, role string, perms ...string) context.Context {
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

// TestHandler_AdminTerminateEvent_HappyPath: ADMIN caller, the terminal
// row from the store round-trips, and the store sees the parsed UUID,
// org from session, and the validator-restricted target_state.
func TestHandler_AdminTerminateEvent_HappyPath(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateCancelled,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "operator escalation",
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, eventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED, resp.Msg.Event.State)

	assert.Equal(t, 1, store.calls)
	assert.Equal(t, int64(42), store.lastOrgID)
	assert.Equal(t, eventUUID, store.lastEventUUID)
	assert.Equal(t, models.EventStateCancelled, store.lastTargetState)
	assert.Equal(t, "operator escalation", store.lastReason)
}

func TestHandler_ForceReleaseCurtailmentOwnership_HappyPath(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:                   99,
			EventUUID:            eventUUID,
			OrgID:                42,
			State:                models.EventStateCancelled,
			DecisionSnapshotJSON: []byte(`{"skipped":[{"device_identifier":"miner-heavy"}]}`),
		},
		targets: []*models.Target{
			{DeviceIdentifier: "miner-1", State: models.TargetStateReleased},
		},
		forceReleaseAutomationDisabled: true,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ForceReleaseCurtailmentOwnership(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
			EventUuid: eventUUID.String(),
			Reason:    "operator release",
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, eventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED, resp.Msg.Event.State)
	assert.Empty(t, resp.Msg.Event.Targets, "force release response must not depend on post-write target hydration")
	assert.Nil(t, resp.Msg.Event.DecisionSnapshot, "force release response must stay bounded")
	assert.Equal(t, uint32(1), resp.Msg.ReleasedTargetCount)
	assert.True(t, resp.Msg.OwnershipReleased)
	assert.False(t, resp.Msg.RestoreAttempted)
	assert.True(t, resp.Msg.AutomationDisabled)
	assert.Equal(t, 1, store.forceReleaseCalls)
	assert.Equal(t, 0, store.listTargetsCalls)
	assert.Equal(t, int64(42), store.lastForceReleaseOrgID)
	assert.Equal(t, eventUUID, store.lastForceReleaseUUID)
	assert.Equal(t, "operator release", store.lastForceReleaseReason)
}

func TestHandler_ForceReleaseCurtailmentOwnership_TargetReadFailureDoesNotMaskRelease(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateCancelled,
		},
		targetsErr: assert.AnError,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.ForceReleaseCurtailmentOwnership(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
			EventUuid: eventUUID.String(),
			Reason:    "operator release",
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, eventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, 1, store.forceReleaseCalls)
	assert.Equal(t, 0, store.listTargetsCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_BypassesIncompleteTargetSiteCoverage(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateActive,
			ScopeType: models.ScopeTypeDeviceList,
		},
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeDeviceList,
		},
		targetSitesComplete: false,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ForceReleaseCurtailmentOwnership(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
			EventUuid: eventUUID.String(),
			Reason:    "operator release",
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsSiteNarrowedKnownSite(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeSite,
			ScopeJSON: siteScopeJSON(t, narrowedSite),
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(narrowedSite))

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_AllowsMatchingSiteNarrowingForKnownSite(t *testing.T) {
	t.Parallel()
	const (
		orgID  = int64(42)
		siteID = int64(7)
	)
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeSite,
			ScopeJSON: siteScopeJSON(t, siteID),
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(siteID, authz.PermCurtailmentManage))

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.NoError(t, err)
	assert.Equal(t, 1, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsIncompleteCoverageWhenOrgGrantIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateActive,
			ScopeType: models.ScopeTypeDeviceList,
		},
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeDeviceList,
		},
		targetSitesComplete: false,
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(narrowedSite))

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsWholeOrgWhenOrgGrantIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		narrowedSite = int64(7)
	)
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateActive,
			ScopeType: models.ScopeTypeWholeOrg,
		},
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeWholeOrg,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(narrowedSite))

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsWholeOrgWithFanWhenOrgGrantIsNarrowed(t *testing.T) {
	t.Parallel()
	const (
		orgID        = int64(42)
		fanID        = int64(11)
		fanSiteID    = int64(7)
		narrowedSite = int64(8)
	)
	eventUUID := uuid.New()
	authEvent := &models.Event{
		ID:                   99,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateActive,
		ScopeType:            models.ScopeTypeWholeOrg,
		FacilityFanDeviceIDs: []int64{fanID},
		FacilityFanSiteIDs:   []int64{fanSiteID},
	}
	store := &adminTerminateStubStore{
		authEvent: authEvent,
		result: &models.Event{
			ID:                   99,
			EventUUID:            eventUUID,
			OrgID:                orgID,
			State:                models.EventStateCancelled,
			ScopeType:            models.ScopeTypeWholeOrg,
			FacilityFanDeviceIDs: []int64{fanID},
			FacilityFanSiteIDs:   []int64{fanSiteID},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	},
		testOrgAssignment(authz.PermCurtailmentManage),
		testSiteAssignment(fanSiteID, authz.PermCurtailmentManage),
		testSiteAssignment(narrowedSite),
	)

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsSiteOnlyManage(t *testing.T) {
	t.Parallel()
	const (
		orgID  = int64(42)
		siteID = int64(7)
	)
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := testSessionCtxWithAssignments(t, &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         9,
		Role:           domainAuth.AdminRoleName,
	}, testSiteAssignment(siteID, authz.PermCurtailmentManage))

	_, err := h.ForceReleaseCurtailmentOwnership(ctx, connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
		EventUuid: eventUUID.String(),
		Reason:    "operator release",
	}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

func TestHandler_ForceReleaseCurtailmentOwnership_RejectsNonAdmin(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.ForceReleaseCurtailmentOwnership(
		adminTerminateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.ForceReleaseCurtailmentOwnershipRequest{
			EventUuid: eventUUID.String(),
			Reason:    "operator release",
		}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.forceReleaseCalls)
}

// TestHandler_AdminTerminateEvent_RejectsCallerWithoutCurtailmentManage:
// the caller is denied when curtailment:manage is absent from their
// effective permissions, regardless of role.
func TestHandler_AdminTerminateEvent_RejectsCallerWithoutCurtailmentManage(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStatePending,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtxWithPerms(42, domainAuth.AdminRoleName /* no curtailment:manage */),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "perm-gate test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.calls, "permission gate must fail before reaching the service")
}

func TestHandler_AdminTerminateEvent_RejectsNonAdminWithCurtailmentManage(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateRestoring,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "OPERATOR"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "admin role gate test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.calls, "admin role gate must fail before reaching the service")
}

// TestHandler_AdminTerminateEvent_RejectsMissingSession: missing
// session.Info remaps to Unauthenticated rather than Internal.
func TestHandler_AdminTerminateEvent_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	store := &adminTerminateStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		t.Context(),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   uuid.New().String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.calls)
}

// TestHandler_AdminTerminateEvent_RejectsMalformedUUID: invalid UUIDs
// surface as InvalidArgument before any store call.
func TestHandler_AdminTerminateEvent_RejectsMalformedUUID(t *testing.T) {
	t.Parallel()
	store := &adminTerminateStubStore{}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   "not-a-uuid",
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Equal(t, 0, store.calls)
}

// TestHandler_AdminTerminateEvent_StateConflictMapsFailedPrecondition:
// an already-terminal event in a different state surfaces as
// FailedPrecondition at the RPC boundary.
func TestHandler_AdminTerminateEvent_StateConflictMapsFailedPrecondition(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateCompleted,
		},
		err: interfaces.ErrCurtailmentAdminTerminateStateConflict,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED,
			Reason:      "test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)
}

// TestHandler_AdminTerminateEvent_ActiveEventMapsFailedPrecondition: an
// active event (still curtailing live miners) cannot be admin-terminated
// directly. The service surfaces ErrCurtailmentAdminTerminateActiveEvent and
// the handler must map that to FailedPrecondition so the operator gets a
// retry-able signal to call StopCurtailment first.
func TestHandler_AdminTerminateEvent_ActiveEventMapsFailedPrecondition(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		authEvent: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateActive,
		},
		err: interfaces.ErrCurtailmentAdminTerminateActiveEvent,
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "test",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)
}

// TestHandler_AdminTerminateEvent_SuperAdminAllowed: SUPER_ADMIN clears
// the role gate. Pairs with the OPERATOR rejection.
func TestHandler_AdminTerminateEvent_SuperAdminAllowed(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateFailed,
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "SUPER_ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED,
			Reason:      "test",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, 1, store.calls)
}

func TestHandler_AdminTerminateEvent_UsesSiteScopedEventPermission(t *testing.T) {
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
		{"org permission without site narrowing allows terminate", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage)}, 0, 1},
		{"matching site narrowing allows terminate", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, 0, 1},
		{"site-only permission denies terminate", []authz.Assignment{testSiteAssignment(allowedSite, authz.PermCurtailmentManage)}, connect.CodePermissionDenied, 0},
		{"site narrowing without manage denies terminate", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(allowedSite)}, connect.CodePermissionDenied, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eventUUID := uuid.New()
			event := &models.Event{
				ID:        99,
				EventUUID: eventUUID,
				OrgID:     orgID,
				State:     models.EventStateCancelled,
				ScopeType: models.ScopeTypeSite,
				ScopeJSON: siteScopeJSON(t, allowedSite),
			}
			store := &adminTerminateStubStore{
				authEvent: event,
				result:    event,
			}
			h := NewHandler(domainCurtailment.NewService(store))
			ctx := testSessionCtxWithAssignments(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: orgID,
				UserID:         9,
				Role:           domainAuth.AdminRoleName,
			}, tc.assignments...)

			_, err := h.AdminTerminateEvent(ctx, connect.NewRequest(&pb.AdminTerminateEventRequest{
				EventUuid:   eventUUID.String(),
				TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
				Reason:      "site scoped terminate",
			}))

			if tc.wantCode == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			}
			assert.Equal(t, tc.wantCalls, store.calls)
		})
	}
}

// TestHandler_AdminTerminateEvent_ResponseCarriesScopeAndTargets: the
// post-terminate response must use the same fully-populated shape as the
// read endpoints — scope, mode params, decision snapshot, and the swept
// targets — so a client merging the response into a cached event does not
// silently drop structured fields. Mirrors StopCurtailment's response.
func TestHandler_AdminTerminateEvent_ResponseCarriesScopeAndTargets(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	store := &adminTerminateStubStore{
		result: &models.Event{
			ID:        99,
			EventUUID: eventUUID,
			OrgID:     42,
			State:     models.EventStateCancelled,
			ScopeType: models.ScopeTypeWholeOrg,
		},
		targets: []*models.Target{
			{
				CurtailmentEventID: 99,
				DeviceIdentifier:   "miner-1",
				State:              models.TargetStateRestoreFailed,
				DesiredState:       models.DesiredStateCurtailed,
			},
		},
	}
	h := NewHandler(domainCurtailment.NewService(store))

	resp, err := h.AdminTerminateEvent(
		adminTerminateSessionCtx(42, "ADMIN"),
		connect.NewRequest(&pb.AdminTerminateEventRequest{
			EventUuid:   eventUUID.String(),
			TargetState: pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED,
			Reason:      "test",
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.NotNil(t, resp.Msg.Event.Scope, "response must carry the persisted scope so cached clients keep it")
	require.Len(t, resp.Msg.Event.Targets, 1, "response must carry the post-sweep targets")
	assert.Equal(t, "miner-1", resp.Msg.Event.Targets[0].DeviceIdentifier)
}
