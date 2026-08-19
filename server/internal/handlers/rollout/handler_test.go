package rollout

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestHandlerGatesEveryRolloutRPC(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil)
	ctx := rolloutHandlerContext(t, 42, 9)
	rolloutID := uuid.New().String()
	calls := []struct {
		name string
		call func() error
	}{
		{"PreviewRolloutLane", func() error {
			_, err := handler.PreviewRolloutLane(ctx, connect.NewRequest(&pb.PreviewRolloutLaneRequest{}))
			return err
		}},
		{"CreateRolloutLane", func() error {
			_, err := handler.CreateRolloutLane(ctx, connect.NewRequest(&pb.CreateRolloutLaneRequest{}))
			return err
		}},
		{"GetRolloutLane", func() error {
			_, err := handler.GetRolloutLane(ctx, connect.NewRequest(&pb.GetRolloutLaneRequest{LaneId: rolloutID}))
			return err
		}},
		{"GetRolloutLaneForRollout", func() error {
			_, err := handler.GetRolloutLaneForRollout(
				ctx,
				connect.NewRequest(&pb.GetRolloutLaneForRolloutRequest{RolloutId: rolloutID}),
			)
			return err
		}},
		{"ListRolloutLanes", func() error {
			_, err := handler.ListRolloutLanes(ctx, connect.NewRequest(&pb.ListRolloutLanesRequest{}))
			return err
		}},
		{"ListRolloutLaneMembers", func() error {
			_, err := handler.ListRolloutLaneMembers(
				ctx,
				connect.NewRequest(&pb.ListRolloutLaneMembersRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"GetRolloutLaneAssignments", func() error {
			_, err := handler.GetRolloutLaneAssignments(
				ctx,
				connect.NewRequest(&pb.GetRolloutLaneAssignmentsRequest{
					DeviceIdentifiers: []string{"miner-a"},
				}),
			)
			return err
		}},
		{"PreviewRolloutLaneMembershipChange", func() error {
			_, err := handler.PreviewRolloutLaneMembershipChange(
				ctx,
				connect.NewRequest(&pb.PreviewRolloutLaneMembershipChangeRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"UpdateRolloutLaneMembership", func() error {
			_, err := handler.UpdateRolloutLaneMembership(
				ctx,
				connect.NewRequest(&pb.UpdateRolloutLaneMembershipRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"DeleteRolloutLane", func() error {
			_, err := handler.DeleteRolloutLane(
				ctx,
				connect.NewRequest(&pb.DeleteRolloutLaneRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"StartRolloutLane", func() error {
			_, err := handler.StartRolloutLane(ctx, connect.NewRequest(&pb.StartRolloutLaneRequest{LaneId: rolloutID}))
			return err
		}},
		{"CreateRollout", func() error {
			_, err := handler.CreateRollout(ctx, connect.NewRequest(&pb.CreateRolloutRequest{}))
			return err
		}},
		{"GetRollout", func() error {
			_, err := handler.GetRollout(ctx, connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ListRollouts", func() error {
			_, err := handler.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{}))
			return err
		}},
		{"AdmitRollout", func() error {
			_, err := handler.AdmitRollout(ctx, connect.NewRequest(&pb.AdmitRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ContinueRollout", func() error {
			_, err := handler.ContinueRollout(ctx, connect.NewRequest(&pb.ContinueRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"PauseRollout", func() error {
			_, err := handler.PauseRollout(ctx, connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"ResumeRollout", func() error {
			_, err := handler.ResumeRollout(ctx, connect.NewRequest(&pb.ResumeRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"AbortRollout", func() error {
			_, err := handler.AbortRollout(ctx, connect.NewRequest(&pb.AbortRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"RevertRollout", func() error {
			_, err := handler.RevertRollout(ctx, connect.NewRequest(&pb.RevertRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
		{"CompleteRollout", func() error {
			_, err := handler.CompleteRollout(ctx, connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: rolloutID}))
			return err
		}},
	}

	for _, testCase := range calls {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := testCase.call()
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}

func TestDeleteRolloutLaneUsesChannelManagePermissionAndSessionIdentity(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	request := connect.NewRequest(&pb.DeleteRolloutLaneRequest{
		LaneId:           laneID.String(),
		ExpectedRevision: 7,
		IdempotencyKey:   "delete-lane-handler",
		Reason:           "remove broken demo lane",
	})

	_, err := handler.DeleteRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		request,
	)
	assertPermissionDenied(t, err)

	response, err := handler.DeleteRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		request,
	)
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, int64(42), laneService.deleted.OrgID)
	assert.Equal(t, int64(9), laneService.deleted.ActorUserID)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.deleted.ActorType)
	assert.Nil(t, laneService.deleted.ActorCredentialID)
	assert.Equal(t, laneID, laneService.deleted.LaneID)
	assert.Equal(t, int64(7), laneService.deleted.ExpectedRevision)
	assert.Equal(t, "delete-lane-handler", laneService.deleted.IdempotencyKey)
	assert.Equal(t, "remove broken demo lane", laneService.deleted.Reason)
}

func TestUpdateRolloutLaneMembershipUsesChannelManageAndSessionIdentity(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	request := connect.NewRequest(&pb.UpdateRolloutLaneMembershipRequest{
		LaneId:                  laneID.String(),
		ExpectedRevision:        5,
		AddDeviceIdentifiers:    []string{"miner-add"},
		RemoveDeviceIdentifiers: []string{"miner-remove"},
		ConfirmFirmware:         true,
		ConfirmReassign:         true,
		IdempotencyKey:          "membership-handler",
		Reason:                  "rebalance lane",
	})

	_, err := handler.UpdateRolloutLaneMembership(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		request,
	)
	assertPermissionDenied(t, err)

	response, err := handler.UpdateRolloutLaneMembership(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		request,
	)
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, int64(42), laneService.updatedMembership.OrgID)
	assert.Equal(t, int64(9), laneService.updatedMembership.ActorUserID)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.updatedMembership.ActorType)
	assert.Nil(t, laneService.updatedMembership.ActorCredentialID)
	assert.Equal(t, laneID, laneService.updatedMembership.LaneID)
	assert.Equal(t, int64(5), laneService.updatedMembership.ExpectedRevision)
	assert.Equal(t, []string{"miner-add"}, laneService.updatedMembership.AddIdentifiers)
	assert.Equal(t, []string{"miner-remove"}, laneService.updatedMembership.RemoveIdentifiers)
	assert.True(t, laneService.updatedMembership.ConfirmFirmware)
	assert.True(t, laneService.updatedMembership.ConfirmReassign)
}

func TestUpdateRolloutLaneMembershipDerivesAPIKeyCredential(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		APIKeyID:       "membership-key-77",
		OrganizationID: 42,
		UserID:         9,
	})
	ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  []string{authz.PermChannelManage},
	}}))

	_, err := handler.UpdateRolloutLaneMembership(
		ctx,
		connect.NewRequest(&pb.UpdateRolloutLaneMembershipRequest{
			LaneId:               uuid.NewString(),
			ExpectedRevision:     1,
			AddDeviceIdentifiers: []string{"miner-a"},
			IdempotencyKey:       "membership-api-key",
			Reason:               "automation rebalance",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.ActorTypeAPIKey, laneService.updatedMembership.ActorType)
	require.NotNil(t, laneService.updatedMembership.ActorCredentialID)
	assert.Equal(t, "apikey:membership-key-77", *laneService.updatedMembership.ActorCredentialID)
}

func TestListRolloutLanesForwardsActiveFirmwareConvergenceFilter(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	ctx := rolloutHandlerContext(t, 42, 9, authz.PermChannelRead)

	_, err := handler.ListRolloutLanes(
		ctx,
		connect.NewRequest(&pb.ListRolloutLanesRequest{}),
	)
	require.NoError(t, err)
	assert.False(t, laneService.activeFirmwareConvergenceOnly)

	_, err = handler.ListRolloutLanes(
		ctx,
		connect.NewRequest(&pb.ListRolloutLanesRequest{ActiveFirmwareConvergenceOnly: true}),
	)
	require.NoError(t, err)
	assert.True(t, laneService.activeFirmwareConvergenceOnly)
	assert.Equal(t, int64(42), laneService.listOrgID)
}

func TestGetRolloutLaneForRolloutUsesChannelReadAndOrganizationScope(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	laneID := uuid.New()
	laneService := &recordingLaneService{
		laneForRollout: &betweenchannel.Lane{
			ID:    laneID,
			OrgID: 42,
			Label: "Stable",
		},
	}
	handler := NewHandler(nil, laneService)
	request := connect.NewRequest(&pb.GetRolloutLaneForRolloutRequest{
		RolloutId: rolloutID.String(),
	})

	_, err := handler.GetRolloutLaneForRollout(
		rolloutHandlerContext(t, 42, 9, authz.PermRolloutRead),
		request,
	)
	assertPermissionDenied(t, err)

	response, err := handler.GetRolloutLaneForRollout(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		request,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(42), laneService.laneForRolloutOrgID)
	assert.Equal(t, rolloutID, laneService.laneForRolloutID)
	assert.Equal(t, laneID.String(), response.Msg.GetLane().GetLaneId())
	assert.Equal(t, "Stable", response.Msg.GetLane().GetLabel())
}

func TestGetRolloutLaneForRolloutPreservesNotFound(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, &recordingLaneService{
		laneForRolloutErr: fleeterror.NewNotFoundError("rollout lane not found"),
	})
	_, err := handler.GetRolloutLaneForRollout(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		connect.NewRequest(&pb.GetRolloutLaneForRolloutRequest{
			RolloutId: uuid.NewString(),
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestGetRolloutLaneAssignmentsUsesChannelReadAndOrganizationScope(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneService := &recordingLaneService{
		assignments: []betweenchannel.LaneAssignment{{
			DeviceIdentifier: "miner-a",
			LaneID:           laneID,
			LaneLabel:        "Stable",
		}},
	}
	handler := NewHandler(nil, laneService)
	request := connect.NewRequest(&pb.GetRolloutLaneAssignmentsRequest{
		DeviceIdentifiers: []string{"miner-a", "miner-b"},
	})

	_, err := handler.GetRolloutLaneAssignments(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		request,
	)
	assertPermissionDenied(t, err)

	response, err := handler.GetRolloutLaneAssignments(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		request,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(42), laneService.assignmentOrgID)
	assert.Equal(t, []string{"miner-a", "miner-b"}, laneService.assignmentIdentifiers)
	require.Len(t, response.Msg.GetAssignments(), 1)
	assert.Equal(t, laneID.String(), response.Msg.GetAssignments()[0].GetLaneId())
}

func TestCreateRolloutLaneForwardsReassignmentConfirmation(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	_, err := handler.CreateRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		connect.NewRequest(&pb.CreateRolloutLaneRequest{
			Label:                         "Stable",
			FirmwareFileIds:               []string{"firmware-a"},
			DeviceIdentifiers:             []string{"miner-a"},
			IdempotencyKey:                "create-reassigned-lane",
			ConfirmReassignment:           true,
			ReassignmentConfirmationToken: "preview-token",
		}),
	)
	require.NoError(t, err)
	assert.True(t, laneService.created.ConfirmReassignment)
	assert.Equal(t, "preview-token", laneService.created.ReassignmentConfirmationToken)
	assert.Equal(t, []string{"miner-a"}, laneService.created.DeviceIdentifiers)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.created.ActorType)
	assert.Nil(t, laneService.created.ActorCredentialID)
}

func TestPreviewRolloutLaneReturnsReassignmentConfirmationToken(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{
		preview: betweenchannel.InitialEnforcementPreview{
			RequiresReassignConfirmation:  true,
			ReassignmentConfirmationToken: "preview-token",
		},
	}
	handler := NewHandler(nil, laneService)
	response, err := handler.PreviewRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		connect.NewRequest(&pb.PreviewRolloutLaneRequest{
			FirmwareFileIds:   []string{"firmware-a"},
			DeviceIdentifiers: []string{"miner-a"},
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, response.Msg.GetPreview())
	assert.Equal(t, "preview-token", response.Msg.GetPreview().GetReassignmentConfirmationToken())
}

func TestListRolloutLaneMembersBindsPageTokenToLaneAndRevision(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneService := &recordingLaneService{
		listMembersResult: betweenchannel.ListMembersResult{
			NextIdentifier: "miner-b",
			TotalCount:     3,
			Revision:       7,
		},
	}
	handler := NewHandler(nil, laneService)
	ctx := rolloutHandlerContext(t, 42, 9, authz.PermChannelRead)

	first, err := handler.ListRolloutLaneMembers(
		ctx,
		connect.NewRequest(&pb.ListRolloutLaneMembersRequest{
			LaneId:            laneID.String(),
			PageSize:          1,
			IncludeTotalCount: true,
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), first.Msg.GetTotalCount())
	token, err := decodeLaneMemberPageToken(first.Msg.GetNextPageToken())
	require.NoError(t, err)
	assert.Equal(t, laneMemberPageTokenVersion, token.Version)
	assert.Equal(t, laneID, token.LaneID)
	assert.Equal(t, int64(7), token.Revision)
	assert.Equal(t, "miner-b", token.Cursor)

	_, err = handler.ListRolloutLaneMembers(
		ctx,
		connect.NewRequest(&pb.ListRolloutLaneMembersRequest{
			LaneId:    uuid.NewString(),
			PageSize:  1,
			PageToken: first.Msg.GetNextPageToken(),
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestListRolloutLaneMembersRejectsStaleRevisionContinuation(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneService := &recordingLaneService{
		listMembersErr: fleeterror.NewFailedPreconditionError("rollout lane changed during pagination"),
	}
	handler := NewHandler(nil, laneService)
	token := encodeLaneMemberPageToken(laneMemberPageToken{
		Version:  laneMemberPageTokenVersion,
		LaneID:   laneID,
		Revision: 7,
		Cursor:   "miner-b",
	})

	_, err := handler.ListRolloutLaneMembers(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelRead),
		connect.NewRequest(&pb.ListRolloutLaneMembersRequest{
			LaneId:    laneID.String(),
			PageSize:  1,
			PageToken: token,
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)
	assert.Equal(t, int64(7), laneService.listMembersRequest.ExpectedRevision)
	assert.Equal(t, "miner-b", laneService.listMembersRequest.AfterIdentifier)
}

func TestDeleteRolloutLaneDerivesAPIKeyActorIdentity(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		APIKeyID:       "lane-delete-key-77",
		OrganizationID: 42,
		UserID:         9,
	})
	ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  []string{authz.PermChannelManage},
	}}))

	_, err := handler.DeleteRolloutLane(
		ctx,
		connect.NewRequest(&pb.DeleteRolloutLaneRequest{
			LaneId:           uuid.NewString(),
			ExpectedRevision: 1,
			IdempotencyKey:   "delete-lane-api-key",
			Reason:           "automation cleanup",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.ActorTypeAPIKey, laneService.deleted.ActorType)
	require.NotNil(t, laneService.deleted.ActorCredentialID)
	assert.Equal(t, "apikey:lane-delete-key-77", *laneService.deleted.ActorCredentialID)
	assert.Equal(t, int64(9), laneService.deleted.ActorUserID)
}

func TestDeleteRolloutLanePreservesConflictCode(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{
		deleteErr: fleeterror.NewAlreadyExistsError("delete idempotency conflict"),
	}
	handler := NewHandler(nil, laneService)
	_, err := handler.DeleteRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		connect.NewRequest(&pb.DeleteRolloutLaneRequest{
			LaneId:           uuid.NewString(),
			ExpectedRevision: 1,
			IdempotencyKey:   "delete-conflict",
			Reason:           "remove lane",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeAlreadyExists, fleetErr.GRPCCode)
}

func TestDeleteRolloutLanePreservesNotFoundCode(t *testing.T) {
	t.Parallel()

	laneService := &recordingLaneService{
		deleteErr: fleeterror.NewNotFoundError("rollout lane not found"),
	}
	handler := NewHandler(nil, laneService)
	_, err := handler.DeleteRolloutLane(
		rolloutHandlerContext(t, 42, 9, authz.PermChannelManage),
		connect.NewRequest(&pb.DeleteRolloutLaneRequest{
			LaneId:           uuid.NewString(),
			ExpectedRevision: 1,
			IdempotencyKey:   "delete-missing",
			Reason:           "remove lane",
		}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestHandlerSeparatesReadManageAndControlPermissions(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     42,
			State:     rolloutDomain.StateCreated,
			Revision:  1,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)

	readCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutRead)
	response, err := handler.GetRollout(
		readCtx,
		connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID.String()}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutID.String(), response.Msg.GetRollout().GetRolloutId())
	assert.Equal(t, int64(42), service.getOrgID)

	_, err = handler.CreateRollout(
		readCtx,
		connect.NewRequest(&pb.CreateRolloutRequest{}),
	)
	assertPermissionDenied(t, err)

	manageCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutManage)
	_, err = handler.PauseRollout(
		manageCtx,
		connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: rolloutID.String()}),
	)
	assertPermissionDenied(t, err)

	controlCtx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutControl)
	_, err = handler.GetRollout(
		controlCtx,
		connect.NewRequest(&pb.GetRolloutRequest{RolloutId: rolloutID.String()}),
	)
	assertPermissionDenied(t, err)
}

func TestHandlerDerivesControlOrganizationAndActorFromSession(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     73,
			State:     rolloutDomain.StatePaused,
			Revision:  3,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)
	ctx := rolloutHandlerContext(t, 73, 91, authz.PermRolloutControl)

	_, err := handler.PauseRollout(
		ctx,
		connect.NewRequest(&pb.PauseRolloutRequest{
			RolloutId:        rolloutID.String(),
			ExpectedRevision: 2,
			IdempotencyKey:   "pause-73",
			Reason:           "operator pause",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(73), service.control.OrgID)
	assert.Equal(t, int64(91), service.control.ActorUserID)
	assert.Equal(t, rolloutID, service.control.RolloutID)
}

func TestHandlerDerivesAPIKeyControlActorFromSession(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	service := &recordingRolloutService{
		result: &rolloutDomain.Rollout{
			ID:        rolloutID,
			OrgID:     73,
			State:     rolloutDomain.StatePaused,
			Revision:  3,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler := NewHandler(service, nil)
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodAPIKey,
		APIKeyID:       "rollout-key-77",
		OrganizationID: 73,
		UserID:         91,
	})
	ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  []string{authz.PermRolloutControl},
	}}))

	_, err := handler.PauseRollout(
		ctx,
		connect.NewRequest(&pb.PauseRolloutRequest{
			RolloutId:        rolloutID.String(),
			ExpectedRevision: 2,
			IdempotencyKey:   "pause-api-key",
			Reason:           "automation pause",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.ActorTypeAPIKey, service.control.ActorType)
	require.NotNil(t, service.control.ActorCredentialID)
	assert.Equal(t, "apikey:rollout-key-77", *service.control.ActorCredentialID)
	assert.Equal(t, int64(91), service.control.ActorUserID)
}

func TestProtoTranslationClampsNegativePositionsAndRevisions(t *testing.T) {
	t.Parallel()

	translatedRollout := rolloutToProto(&rolloutDomain.Rollout{Revision: -1})
	translatedBatch := batchToProto(&rolloutDomain.Batch{Position: -1, Revision: -1})
	translatedMember := memberToProto(&rolloutDomain.Member{Position: -1, Revision: -1})
	translatedCause := causeToProto(&rolloutDomain.Cause{RolloutRevision: -1})

	assert.Zero(t, translatedRollout.GetRevision())
	assert.Zero(t, translatedBatch.GetPosition())
	assert.Zero(t, translatedBatch.GetRevision())
	assert.Zero(t, translatedMember.GetPosition())
	assert.Zero(t, translatedMember.GetRevision())
	assert.Zero(t, translatedCause.GetRolloutRevision())
}

func TestStateFromProtoTreatsUnspecifiedAsUnknown(t *testing.T) {
	t.Parallel()

	state, ok := stateFromProto(pb.RolloutState_ROLLOUT_STATE_UNSPECIFIED)

	assert.Empty(t, state)
	assert.False(t, ok)
}

func TestLaneTranslationIncludesFirmwareTransitionDetails(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	translated := laneToProto(&betweenchannel.Lane{
		MemberCount: 2,
		FirmwareConvergence: betweenchannel.FirmwareConvergenceStatus{
			TotalCount:     2,
			VerifyingCount: 1,
			AttentionCount: 1,
			Members: []channel.FirmwareTransitionMiner{
				{
					DeviceIdentifier:              "miner-verifying",
					Manufacturer:                  "Proto",
					Model:                         "Alpha",
					LatestObservedFirmwareVersion: "2.0.0",
					TargetFirmwareVersion:         "2.0.0",
					State:                         channel.FirmwareTransitionVerifying,
					UpdatedAt:                     updatedAt,
				},
				{
					DeviceIdentifier:      "miner-attention",
					Manufacturer:          "Proto",
					Model:                 "Beta",
					TargetFirmwareVersion: "3.0.0",
					State:                 channel.FirmwareTransitionNeedsAttention,
					LastError:             "Firmware identity could not be confirmed",
					UpdatedAt:             updatedAt,
				},
			},
		},
	})

	assert.Equal(t, uint32(2), translated.GetMemberCount())
	assert.Equal(t, uint32(1), translated.GetFirmwareConvergence().GetVerifyingCount())
	require.Len(t, translated.GetFirmwareConvergence().GetMembers(), 2)
	assert.Equal(
		t,
		pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_VERIFYING,
		translated.GetFirmwareConvergence().GetMembers()[0].GetState(),
	)
	assert.Equal(t, "2.0.0", translated.GetFirmwareConvergence().GetMembers()[0].GetLatestObservedFirmwareVersion())
	assert.Equal(t, updatedAt, translated.GetFirmwareConvergence().GetMembers()[0].GetUpdatedAt().AsTime())
	assert.Nil(t, translated.GetFirmwareConvergence().GetMembers()[1].LatestObservedFirmwareVersion)
	assert.Equal(
		t,
		pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_NEEDS_ATTENTION,
		translated.GetFirmwareConvergence().GetMembers()[1].GetState(),
	)
	assert.Equal(
		t,
		"Firmware identity could not be confirmed",
		translated.GetFirmwareConvergence().GetMembers()[1].GetLastError(),
	)
}

type recordingRolloutService struct {
	result   *rolloutDomain.Rollout
	getOrgID int64
	control  rolloutDomain.ControlRequest
}

type recordingLaneService struct {
	laneService
	deleted                       betweenchannel.DeleteLaneRequest
	updatedMembership             betweenchannel.UpdateMembershipRequest
	deleteErr                     error
	laneForRollout                *betweenchannel.Lane
	laneForRolloutErr             error
	laneForRolloutOrgID           int64
	laneForRolloutID              uuid.UUID
	listOrgID                     int64
	activeFirmwareConvergenceOnly bool
	listMembersRequest            betweenchannel.ListMembersRequest
	listMembersResult             betweenchannel.ListMembersResult
	listMembersErr                error
	assignments                   []betweenchannel.LaneAssignment
	assignmentOrgID               int64
	assignmentIdentifiers         []string
	created                       betweenchannel.CreateLaneRequest
	preview                       betweenchannel.InitialEnforcementPreview
}

func (s *recordingLaneService) GetLaneForRollout(
	_ context.Context,
	orgID int64,
	rolloutID uuid.UUID,
) (*betweenchannel.Lane, error) {
	s.laneForRolloutOrgID = orgID
	s.laneForRolloutID = rolloutID
	return s.laneForRollout, s.laneForRolloutErr
}

func (s *recordingLaneService) PreviewLane(
	_ context.Context,
	_ betweenchannel.PreviewLaneRequest,
) (betweenchannel.InitialEnforcementPreview, error) {
	return s.preview, nil
}

func (s *recordingLaneService) CreateLane(
	_ context.Context,
	req betweenchannel.CreateLaneRequest,
) (*betweenchannel.Lane, error) {
	s.created = req
	return &betweenchannel.Lane{ID: uuid.New(), OrgID: req.OrgID, Label: req.Label}, nil
}

func (s *recordingLaneService) GetAssignments(
	_ context.Context,
	orgID int64,
	deviceIdentifiers []string,
) ([]betweenchannel.LaneAssignment, error) {
	s.assignmentOrgID = orgID
	s.assignmentIdentifiers = deviceIdentifiers
	return s.assignments, nil
}

func (s *recordingLaneService) ListLanes(
	_ context.Context,
	orgID int64,
	activeFirmwareConvergenceOnly bool,
) ([]betweenchannel.Lane, error) {
	s.listOrgID = orgID
	s.activeFirmwareConvergenceOnly = activeFirmwareConvergenceOnly
	return nil, nil
}

func (s *recordingLaneService) ListMembers(
	_ context.Context,
	req betweenchannel.ListMembersRequest,
) (betweenchannel.ListMembersResult, error) {
	s.listMembersRequest = req
	return s.listMembersResult, s.listMembersErr
}

func (s *recordingLaneService) DeleteLane(
	_ context.Context,
	req betweenchannel.DeleteLaneRequest,
) error {
	s.deleted = req
	return s.deleteErr
}

func (s *recordingLaneService) UpdateMembership(
	_ context.Context,
	req betweenchannel.UpdateMembershipRequest,
) (betweenchannel.UpdateMembershipResult, error) {
	s.updatedMembership = req
	return betweenchannel.UpdateMembershipResult{}, nil
}

func (s *recordingRolloutService) Create(
	context.Context,
	rolloutDomain.CreateRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Get(
	_ context.Context,
	orgID int64,
	_ uuid.UUID,
) (*rolloutDomain.Rollout, error) {
	s.getOrgID = orgID
	return s.result, nil
}

func (s *recordingRolloutService) List(
	context.Context,
	int64,
	[]rolloutDomain.State,
) ([]rolloutDomain.Rollout, error) {
	return []rolloutDomain.Rollout{*s.result}, nil
}

func (s *recordingRolloutService) Admit(
	context.Context,
	rolloutDomain.AdmitRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Continue(
	context.Context,
	rolloutDomain.AdmitRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Pause(
	_ context.Context,
	req rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	s.control = req
	return s.result, nil
}

func (s *recordingRolloutService) Resume(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Abort(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Revert(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func (s *recordingRolloutService) Complete(
	context.Context,
	rolloutDomain.ControlRequest,
) (*rolloutDomain.Rollout, error) {
	return s.result, nil
}

func rolloutHandlerContext(
	t *testing.T,
	orgID int64,
	userID int64,
	permissions ...string,
) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{
		OrganizationID: orgID,
		UserID:         userID,
	})
	effective := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, effective)
}

func assertPermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}
