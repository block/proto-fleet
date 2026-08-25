package rollout

import (
	"context"
	"testing"
	"time"

	"buf.build/go/protovalidate"
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
		{"PreviewRolloutLaneModelDeclaration", func() error {
			_, err := handler.PreviewRolloutLaneModelDeclaration(
				ctx,
				connect.NewRequest(&pb.PreviewRolloutLaneModelDeclarationRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"CreateRolloutLaneModelDeclaration", func() error {
			_, err := handler.CreateRolloutLaneModelDeclaration(
				ctx,
				connect.NewRequest(&pb.CreateRolloutLaneModelDeclarationRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"PublishRolloutLaneModelTarget", func() error {
			_, err := handler.PublishRolloutLaneModelTarget(
				ctx,
				connect.NewRequest(&pb.PublishRolloutLaneModelTargetRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"PreviewRolloutLaneModelMembershipChange", func() error {
			_, err := handler.PreviewRolloutLaneModelMembershipChange(
				ctx,
				connect.NewRequest(&pb.PreviewRolloutLaneModelMembershipChangeRequest{LaneId: rolloutID}),
			)
			return err
		}},
		{"UpdateRolloutLaneModelMembership", func() error {
			_, err := handler.UpdateRolloutLaneModelMembership(
				ctx,
				connect.NewRequest(&pb.UpdateRolloutLaneModelMembershipRequest{LaneId: rolloutID}),
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
		{"GetRolloutLaneTopologyReadiness", func() error {
			_, err := handler.GetRolloutLaneTopologyReadiness(
				ctx,
				connect.NewRequest(&pb.GetRolloutLaneTopologyReadinessRequest{}),
			)
			return err
		}},
		{"RepairRolloutLaneModelBinding", func() error {
			_, err := handler.RepairRolloutLaneModelBinding(
				ctx,
				connect.NewRequest(&pb.RepairRolloutLaneModelBindingRequest{
					LaneId:      rolloutID,
					LaneModelId: uuid.New().String(),
				}),
			)
			return err
		}},
		{"EnableRolloutLaneModelTopology", func() error {
			_, err := handler.EnableRolloutLaneModelTopology(
				ctx,
				connect.NewRequest(&pb.EnableRolloutLaneModelTopologyRequest{}),
			)
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
		{"GetRolloutGroup", func() error {
			_, err := handler.GetRolloutGroup(
				ctx,
				connect.NewRequest(&pb.GetRolloutGroupRequest{ParentId: rolloutID}),
			)
			return err
		}},
		{"ListRolloutGroups", func() error {
			_, err := handler.ListRolloutGroups(ctx, connect.NewRequest(&pb.ListRolloutGroupsRequest{}))
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

func TestModelMutationsUseChannelManageAndSessionIdentity(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneModelID := uuid.New()
	laneService := &recordingLaneService{}
	handler := NewHandler(nil, laneService)
	ctx := rolloutHandlerContext(t, 42, 9, authz.PermChannelManage)
	selector := &pb.RolloutLaneModelSelector{
		Selector: &pb.RolloutLaneModelSelector_LaneModelId{LaneModelId: laneModelID.String()},
	}

	_, err := handler.CreateRolloutLaneModelDeclaration(
		ctx,
		connect.NewRequest(&pb.CreateRolloutLaneModelDeclarationRequest{
			LaneId: laneID.String(), FirmwareFileId: "firmware-a",
			IdempotencyKey: "declare-model-handler", Reason: "declare model",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(42), laneService.createdModel.OrgID)
	assert.Equal(t, int64(9), laneService.createdModel.ActorUserID)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.createdModel.ActorType)
	assert.Equal(t, int64(0), laneService.createdModel.ExpectedRevision)

	_, err = handler.PublishRolloutLaneModelTarget(
		ctx,
		connect.NewRequest(&pb.PublishRolloutLaneModelTargetRequest{
			LaneId: laneID.String(), Declaration: selector, ExpectedRevision: 4,
			FirmwareFileId: "firmware-b", IdempotencyKey: "publish-model-handler",
			Reason: "publish model",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, laneModelID, laneService.publishedModel.LaneModelID)
	assert.Equal(t, int64(4), laneService.publishedModel.ExpectedRevision)
	assert.Equal(t, int64(9), laneService.publishedModel.ActorUserID)

	_, err = handler.UpdateRolloutLaneModelMembership(
		ctx,
		connect.NewRequest(&pb.UpdateRolloutLaneModelMembershipRequest{
			LaneId: laneID.String(), Declaration: selector, ExpectedRevision: 5,
			AddDeviceIdentifiers: []string{"miner-a"},
			IdempotencyKey:       "members-model-handler", Reason: "add model member",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, laneModelID, laneService.updatedModelMembership.LaneModelID)
	assert.Equal(t, int64(5), laneService.updatedModelMembership.ExpectedRevision)
	assert.Equal(t, int64(9), laneService.updatedModelMembership.ActorUserID)
}

func TestTopologyReadinessUsesChannelReadWhileMutationsUseChannelManage(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneModelID := uuid.New()
	laneService := &recordingLaneService{
		topologyReadiness: betweenchannel.TopologyReadiness{
			OrgID:    42,
			Revision: 3,
			NextCursor: &betweenchannel.TopologyAnomalyCursor{
				LaneID:           laneID,
				DeviceIdentifier: "miner-page",
				Type:             betweenchannel.TopologyAnomalyMissingBinding,
				ID:               uuid.New(),
			},
		},
	}
	handler := NewHandler(nil, laneService)
	readinessRequest := connect.NewRequest(&pb.GetRolloutLaneTopologyReadinessRequest{AnomalyPageSize: 5})
	repairRequest := connect.NewRequest(&pb.RepairRolloutLaneModelBindingRequest{
		LaneId:           laneID.String(),
		LaneModelId:      laneModelID.String(),
		DeviceIdentifier: "miner-repair",
		ExpectedRevision: 7,
		IdempotencyKey:   "repair-binding-handler",
		Reason:           "resolve legacy binding",
	})
	enableRequest := connect.NewRequest(&pb.EnableRolloutLaneModelTopologyRequest{
		ExpectedRevision: 3,
		IdempotencyKey:   "enable-topology-handler",
		Reason:           "all legacy work drained",
	})

	readOnly := rolloutHandlerContext(t, 42, 9, authz.PermChannelRead)
	readiness, err := handler.GetRolloutLaneTopologyReadiness(readOnly, readinessRequest)
	require.NoError(t, err)
	require.Equal(t, uint64(3), readiness.Msg.GetReadiness().GetRevision())
	assert.Equal(t, int32(5), laneService.topologyReadinessPageRequest.Limit)
	assert.NotEmpty(t, readiness.Msg.GetReadiness().GetNextAnomalyPageToken())
	_, err = handler.GetRolloutLaneTopologyReadiness(
		readOnly,
		connect.NewRequest(&pb.GetRolloutLaneTopologyReadinessRequest{
			AnomalyPageSize:  5,
			AnomalyPageToken: readiness.Msg.GetReadiness().GetNextAnomalyPageToken(),
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, laneService.topologyReadinessPageRequest.After)
	assert.Equal(t, laneID, laneService.topologyReadinessPageRequest.After.LaneID)
	_, err = handler.RepairRolloutLaneModelBinding(readOnly, repairRequest)
	assertPermissionDenied(t, err)
	_, err = handler.EnableRolloutLaneModelTopology(readOnly, enableRequest)
	assertPermissionDenied(t, err)

	managed := rolloutHandlerContext(t, 42, 9, authz.PermChannelManage)
	readiness, err = handler.GetRolloutLaneTopologyReadiness(managed, readinessRequest)
	require.NoError(t, err)
	require.Equal(t, uint64(3), readiness.Msg.GetReadiness().GetRevision())

	repair, err := handler.RepairRolloutLaneModelBinding(managed, repairRequest)
	require.NoError(t, err)
	require.NotNil(t, repair)
	assert.Equal(t, int64(42), laneService.repairedBinding.OrgID)
	assert.Equal(t, int64(9), laneService.repairedBinding.ActorUserID)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.repairedBinding.ActorType)
	assert.Equal(t, laneID, laneService.repairedBinding.LaneID)
	assert.Equal(t, laneModelID, laneService.repairedBinding.LaneModelID)
	assert.Equal(t, int64(7), laneService.repairedBinding.ExpectedRevision)
	assert.Equal(t, "miner-repair", laneService.repairedBinding.DeviceIdentifier)

	enabled, err := handler.EnableRolloutLaneModelTopology(managed, enableRequest)
	require.NoError(t, err)
	require.NotNil(t, enabled)
	assert.Equal(t, int64(42), laneService.enabledTopology.OrgID)
	assert.Equal(t, int64(9), laneService.enabledTopology.ActorUserID)
	assert.Equal(t, rolloutDomain.ActorTypeUser, laneService.enabledTopology.ActorType)
	assert.Equal(t, int64(3), laneService.enabledTopology.ExpectedRevision)
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

func TestLaneTranslationPublishesRepeatedModelsAndScalarAvailability(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	translated := laneToProto(&betweenchannel.Lane{
		ID:                        uuid.New(),
		CurrentChannelID:          0,
		Revision:                  4,
		TopologyEnabled:           true,
		ScalarProjectionAvailable: false,
		Models: []betweenchannel.LaneModel{{
			ID:               modelID,
			ModelIdentityKey: "v1:8:testcorp:9:testminer",
			Revision:         3,
			Manufacturer:     "testcorp",
			Model:            "testminer",
			CurrentChannelID: 42,
			CurrentFirmwareTarget: &betweenchannel.LaneModelFirmwareTarget{
				ReleaseTargetID: 72,
				ReleaseSetID:    8,
				FirmwareFileID:  "testminer-2",
				FirmwareVersion: "2.0.0",
				SHA256:          "abc",
			},
			MemberCount: 2,
			Bindings: betweenchannel.LaneModelBindingSummary{
				ActiveCount:     2,
				HistoricalCount: 1,
			},
			Compatibility: betweenchannel.LaneModelCompatible,
		}},
	})

	assert.True(t, translated.GetTopologyEnabled())
	assert.False(t, translated.GetScalarProjectionAvailable())
	assert.Zero(t, translated.GetCurrentChannelId())
	require.Len(t, translated.GetModels(), 1)
	model := translated.GetModels()[0]
	assert.Equal(t, modelID.String(), model.GetLaneModelId())
	assert.Equal(t, uint64(3), model.GetRevision())
	assert.Equal(t, int64(42), model.GetCurrentChannelId())
	assert.Equal(t, "2.0.0", model.GetCurrentFirmwareTarget().GetFirmwareVersion())
	assert.Equal(t, uint32(2), model.GetBindings().GetActiveCount())
	assert.Equal(t, uint32(1), model.GetBindings().GetHistoricalCount())
	assert.Equal(
		t,
		pb.RolloutLaneModelCompatibility_ROLLOUT_LANE_MODEL_COMPATIBILITY_COMPATIBLE,
		model.GetCompatibility(),
	)
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

func TestListRolloutLaneMembersBindsPageTokenToModelDeclaration(t *testing.T) {
	t.Parallel()

	laneID := uuid.New()
	laneModelID := uuid.New()
	laneModelIDValue := laneModelID.String()
	laneService := &recordingLaneService{
		listMembersResult: betweenchannel.ListMembersResult{
			NextIdentifier: "miner-b",
			Revision:       7,
		},
	}
	handler := NewHandler(nil, laneService)
	ctx := rolloutHandlerContext(t, 42, 9, authz.PermChannelRead)

	first, err := handler.ListRolloutLaneMembers(
		ctx,
		connect.NewRequest(&pb.ListRolloutLaneMembersRequest{
			LaneId: laneID.String(), LaneModelId: &laneModelIDValue, PageSize: 1,
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, laneModelID, laneService.listMembersRequest.LaneModelID)
	token, err := decodeLaneMemberPageToken(first.Msg.GetNextPageToken())
	require.NoError(t, err)
	require.NotNil(t, token.LaneModelID)
	assert.Equal(t, laneModelID, *token.LaneModelID)

	otherModelID := uuid.NewString()
	_, err = handler.ListRolloutLaneMembers(
		ctx,
		connect.NewRequest(&pb.ListRolloutLaneMembersRequest{
			LaneId: laneID.String(), LaneModelId: &otherModelID,
			PageSize: 1, PageToken: first.Msg.GetNextPageToken(),
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

func TestAggregateHandlersKeepParentsAndLegacyHistorySeparate(t *testing.T) {
	t.Parallel()

	now := time.Now()
	parentID := uuid.New()
	childID := uuid.New()
	legacyID := uuid.New()
	child := rolloutDomain.Rollout{
		ID:        childID,
		GroupID:   &parentID,
		OrgID:     42,
		Name:      "Proto child",
		State:     rolloutDomain.StateRunning,
		Revision:  2,
		CreatedAt: now,
		UpdatedAt: now,
	}
	parent := rolloutDomain.Group{
		ID:                parentID,
		LaneID:            uuid.New(),
		OrgID:             42,
		Name:              "Two model rollout",
		Reason:            "Update selected models",
		Lifecycle:         rolloutDomain.GroupLifecycleActive,
		Activity:          rolloutDomain.GroupActivityRunning,
		TerminalOutcome:   rolloutDomain.GroupTerminalOutcomePending,
		EvidenceReadiness: rolloutDomain.GroupEvidencePending,
		CreatedAt:         now,
		UpdatedAt:         now,
		Children:          []rolloutDomain.Rollout{child},
	}
	legacy := rolloutDomain.Rollout{
		ID:        legacyID,
		OrgID:     42,
		Name:      "Completed legacy mixed rollout",
		State:     rolloutDomain.StateCompleted,
		Revision:  4,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	service := &recordingRolloutService{
		group:         &parent,
		groups:        []rolloutDomain.Group{parent},
		legacy:        []rolloutDomain.Rollout{legacy},
		groupHasMore:  true,
		legacyHasMore: true,
	}
	handler := NewHandler(service, nil)
	ctx := rolloutHandlerContext(t, 42, 9, authz.PermRolloutRead)

	got, err := handler.GetRolloutGroup(
		ctx,
		connect.NewRequest(&pb.GetRolloutGroupRequest{ParentId: parentID.String()}),
	)
	require.NoError(t, err)
	require.NotNil(t, got.Msg.GetParent())
	assert.Equal(t, parentID.String(), got.Msg.GetParent().GetParentId())
	require.Len(t, got.Msg.GetParent().GetChildren(), 1)
	assert.Equal(t, childID.String(), got.Msg.GetParent().GetChildren()[0].GetRolloutId())
	assert.Equal(t, int64(42), service.getGroupOrgID)
	assert.Equal(t, parentID, service.getGroupID)

	listed, err := handler.ListRolloutGroups(
		ctx,
		connect.NewRequest(&pb.ListRolloutGroupsRequest{PageSize: 2, LegacyPageSize: 3}),
	)
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetParents(), 1)
	require.Len(t, listed.Msg.GetLegacyHistory(), 1)
	assert.Equal(t, parentID.String(), listed.Msg.GetParents()[0].GetParentId())
	assert.Equal(t, legacyID.String(), listed.Msg.GetLegacyHistory()[0].GetRolloutId())
	assert.Empty(t, listed.Msg.GetLegacyHistory()[0].GetParentId())
	assert.Equal(t, int64(42), service.listGroupOrgID)
	assert.Equal(t, int32(2), service.groupPageReq.Limit)
	assert.Equal(t, int32(3), service.legacyPageReq.Limit)
	assert.NotEmpty(t, listed.Msg.GetNextPageToken())
	assert.NotEmpty(t, listed.Msg.GetNextLegacyPageToken())

	_, err = handler.ListRolloutGroups(
		ctx,
		connect.NewRequest(&pb.ListRolloutGroupsRequest{
			PageSize:        2,
			PageToken:       listed.Msg.GetNextPageToken(),
			LegacyPageSize:  3,
			LegacyPageToken: listed.Msg.GetNextLegacyPageToken(),
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, service.groupPageReq.Before)
	require.NotNil(t, service.legacyPageReq.Before)
	assert.Equal(t, parent.ID, service.groupPageReq.Before.ID)
	assert.Equal(t, legacy.ID, service.legacyPageReq.Before.ID)
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

func TestHandlersForwardOptionalHashratePolicy(t *testing.T) {
	t.Parallel()

	t.Run("generic create", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		service := &recordingRolloutService{result: &rolloutDomain.Rollout{
			ID:        uuid.New(),
			CreatedAt: now,
			UpdatedAt: now,
		}}
		handler := NewHandler(service, nil)
		_, err := handler.CreateRollout(
			rolloutHandlerContext(t, 42, 9, authz.PermRolloutManage),
			connect.NewRequest(&pb.CreateRolloutRequest{
				HashratePolicy: &pb.RolloutHashratePolicy{
					MaxDropBasisPoints:     10,
					HealthyDurationSeconds: 30,
				},
			}),
		)
		require.NoError(t, err)
		require.NotNil(t, service.createdRollout.HashratePolicy)
		assert.Equal(t, int32(10), service.createdRollout.HashratePolicy.MaxDropBasisPoints)
		assert.Equal(t, int32(30), service.createdRollout.HashratePolicy.HealthyDurationSeconds)
	})

	t.Run("lane start", func(t *testing.T) {
		t.Parallel()

		laneID := uuid.New()
		service := &recordingLaneService{}
		handler := NewHandler(nil, service)
		_, err := handler.StartRolloutLane(
			rolloutHandlerContext(
				t,
				42,
				9,
				authz.PermRolloutManage,
				authz.PermChannelManage,
			),
			connect.NewRequest(&pb.StartRolloutLaneRequest{
				LaneId: laneID.String(),
				HashratePolicy: &pb.RolloutHashratePolicy{
					MaxDropBasisPoints:     10,
					HealthyDurationSeconds: 30,
				},
			}),
		)
		require.NoError(t, err)
		require.NotNil(t, service.startedRollout.HashratePolicy)
		assert.Equal(t, int32(10), service.startedRollout.HashratePolicy.MaxDropBasisPoints)
		assert.Equal(t, int32(30), service.startedRollout.HashratePolicy.HealthyDurationSeconds)
	})
}

func TestRolloutHashratePolicyProtoValidation(t *testing.T) {
	t.Parallel()

	manualBatch := []*pb.CreateRolloutBatch{{
		Members: []*pb.CreateRolloutMember{{DeviceIdentifier: "miner-a"}},
	}}
	require.NoError(t, protovalidate.Validate(&pb.CreateRolloutRequest{
		Name:           "manual rollout",
		StrategyKey:    "fake",
		Batches:        manualBatch,
		IdempotencyKey: "manual-create",
		Reason:         "manual compatibility",
	}))
	require.NoError(t, protovalidate.Validate(&pb.StartRolloutLaneRequest{
		LaneId:          uuid.NewString(),
		Name:            "manual lane rollout",
		FirmwareFileIds: []string{"firmware-a"},
		Batches:         manualBatch,
		IdempotencyKey:  "manual-lane-start",
		Reason:          "manual compatibility",
	}))
	modelStart := &pb.StartRolloutLaneRequest{
		LaneId:         uuid.NewString(),
		Name:           "model lane rollout",
		IdempotencyKey: "model-parent-start",
		Reason:         "model compatibility",
		ModelPlans: []*pb.StartRolloutLaneModelPlan{{
			LaneModelId:           uuid.NewString(),
			ExpectedModelRevision: 1,
			FirmwareFileId:        "firmware-a",
			Batches:               manualBatch,
			ModelStartKey:         "model-child-start",
		}},
	}
	require.NoError(t, protovalidate.Validate(modelStart))
	modelStart.HashratePolicy = &pb.RolloutHashratePolicy{
		MaxDropBasisPoints:     10,
		HealthyDurationSeconds: 30,
	}
	require.Error(t, protovalidate.Validate(modelStart))
	modelStart.HashratePolicy = nil
	modelStart.ModelPlans[0].HashratePolicy = &pb.RolloutHashratePolicy{
		MaxDropBasisPoints:     10,
		HealthyDurationSeconds: 30,
	}
	require.NoError(t, protovalidate.Validate(modelStart))
	modelStart.FirmwareFileIds = []string{"legacy-mixed-input"}
	require.Error(t, protovalidate.Validate(modelStart))

	tests := []struct {
		name    string
		policy  *pb.RolloutHashratePolicy
		wantErr bool
	}{
		{name: "minimum", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 0, HealthyDurationSeconds: 10}},
		{name: "default UI values", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 30}},
		{name: "maximum", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10000, HealthyDurationSeconds: 1800}},
		{name: "drop above maximum", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10001, HealthyDurationSeconds: 30}, wantErr: true},
		{name: "drop precision", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 1, HealthyDurationSeconds: 30}, wantErr: true},
		{name: "duration below minimum", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 9}, wantErr: true},
		{name: "duration above maximum", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 1801}, wantErr: true},
		{name: "duration precision", policy: &pb.RolloutHashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 11}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := protovalidate.Validate(test.policy)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRolloutPolicyAndBatchEvidenceProtoTranslation(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	lastBoundary := completedAt.Add(20 * time.Second)
	evaluatedAt := lastBoundary.Add(time.Second)
	finalizedAt := completedAt.Add(30 * time.Minute)
	baseline := 300.0
	current := 285.0
	cumulativeDelta := int32(-500)
	bucketHashrate := 95.0
	bucketDelta := int32(-500)
	evidenceError := "automatic continue failed"
	translated := rolloutToProto(&rolloutDomain.Rollout{
		HashratePolicy: &rolloutDomain.HashratePolicy{
			MaxDropBasisPoints:     10,
			HealthyDurationSeconds: 30,
		},
		Batches: []rolloutDomain.Batch{{
			CompletedAt:                        &completedAt,
			EvidenceStatus:                     rolloutDomain.EvidenceStatusObserving,
			EvidenceTotalCount:                 3,
			EvidencePairedCount:                2,
			CumulativeBaselineHashrateHS:       &baseline,
			CumulativeCurrentHashrateHS:        &current,
			CumulativeDeltaBasisPoints:         &cumulativeDelta,
			LatestPolicyBucketHashrateHS:       &bucketHashrate,
			LatestPolicyBucketDeltaBasisPoints: &bucketDelta,
			LastPolicyBucketBoundary:           &lastBoundary,
			EvaluatedAt:                        &evaluatedAt,
			EvidenceErrorMessage:               &evidenceError,
			PostWindowFinalized:                true,
			PostWindowFinalizedAt:              &finalizedAt,
		}},
	})

	require.NotNil(t, translated.GetHashratePolicy())
	assert.Equal(t, uint32(10), translated.GetHashratePolicy().GetMaxDropBasisPoints())
	assert.Equal(t, uint32(30), translated.GetHashratePolicy().GetHealthyDurationSeconds())
	require.Len(t, translated.GetBatches(), 1)
	batch := translated.GetBatches()[0]
	require.NotNil(t, batch.GetCompletedAt())
	assert.Equal(t, completedAt, batch.GetCompletedAt().AsTime())
	summary := batch.GetEvidenceSummary()
	require.NotNil(t, summary)
	assert.Equal(t, pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_OBSERVING, summary.GetStatus())
	assert.Equal(t, uint64(3), summary.GetTotalCount())
	assert.Equal(t, uint64(2), summary.GetPairedCount())
	assert.InDelta(t, baseline, summary.GetCumulativeBaselineHashrateHs(), 0.001)
	assert.InDelta(t, current, summary.GetCumulativeCurrentHashrateHs(), 0.001)
	assert.Equal(t, cumulativeDelta, summary.GetCumulativeDeltaBasisPoints())
	assert.InDelta(t, bucketHashrate, summary.GetLatestPolicyBucketHashrateHs(), 0.001)
	assert.Equal(t, bucketDelta, summary.GetLatestPolicyBucketDeltaBasisPoints())
	assert.Nil(t, summary.GetHealthySince())
	assert.Equal(t, lastBoundary, summary.GetLastPolicyBucketBoundary().AsTime())
	assert.Equal(t, evaluatedAt, summary.GetEvaluatedAt().AsTime())
	assert.Equal(t, evidenceError, summary.GetErrorMessage())
	assert.True(t, summary.GetPostWindowFinalized())
	assert.Equal(t, finalizedAt, summary.GetPostWindowFinalizedAt().AsTime())

	cancellationReason := "rollout aborted: operator cancelled this model"
	cancelled := rolloutToProto(&rolloutDomain.Rollout{
		Batches: []rolloutDomain.Batch{{
			EvidenceStatus:             rolloutDomain.EvidenceStatusCancelled,
			EvidenceCancellationReason: &cancellationReason,
			EvidenceCancelledAt:        &finalizedAt,
			PostWindowFinalized:        true,
			PostWindowFinalizedAt:      &finalizedAt,
		}},
	})
	cancelledSummary := cancelled.GetBatches()[0].GetEvidenceSummary()
	require.NotNil(t, cancelledSummary)
	assert.Equal(
		t,
		pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_CANCELLED,
		cancelledSummary.GetStatus(),
	)
	assert.Equal(t, cancellationReason, cancelledSummary.GetCancellationReason())
	assert.Equal(t, finalizedAt, cancelledSummary.GetCancelledAt().AsTime())

	manual := rolloutToProto(&rolloutDomain.Rollout{Batches: []rolloutDomain.Batch{{}}})
	assert.Nil(t, manual.GetHashratePolicy())
	require.Len(t, manual.GetBatches(), 1)
	require.NotNil(t, manual.GetBatches()[0].GetEvidenceSummary())
	assert.Equal(
		t,
		pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_UNSPECIFIED,
		manual.GetBatches()[0].GetEvidenceSummary().GetStatus(),
	)
	assert.Nil(t, manual.GetBatches()[0].GetCompletedAt())

	legacyCompleted := rolloutToProto(&rolloutDomain.Rollout{
		Batches: []rolloutDomain.Batch{{
			State: rolloutDomain.BatchStateCompleted,
		}},
	})
	require.Len(t, legacyCompleted.GetBatches(), 1)
	assert.Nil(t, legacyCompleted.GetBatches()[0].GetCompletedAt())
	assert.Nil(t, legacyCompleted.GetBatches()[0].GetEvidenceSummary())
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
	result         *rolloutDomain.Rollout
	group          *rolloutDomain.Group
	groups         []rolloutDomain.Group
	legacy         []rolloutDomain.Rollout
	getOrgID       int64
	getGroupOrgID  int64
	getGroupID     uuid.UUID
	listGroupOrgID int64
	groupPageReq   rolloutDomain.ListPageRequest
	legacyPageReq  rolloutDomain.ListPageRequest
	groupHasMore   bool
	legacyHasMore  bool
	createdRollout rolloutDomain.CreateRequest
	control        rolloutDomain.ControlRequest
}

type recordingLaneService struct {
	laneService
	deleted                       betweenchannel.DeleteLaneRequest
	updatedMembership             betweenchannel.UpdateMembershipRequest
	createdModel                  betweenchannel.CreateModelDeclarationRequest
	publishedModel                betweenchannel.PublishModelTargetRequest
	updatedModelMembership        betweenchannel.UpdateModelMembershipRequest
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
	startedRollout                betweenchannel.StartRolloutRequest
	preview                       betweenchannel.InitialEnforcementPreview
	topologyReadiness             betweenchannel.TopologyReadiness
	topologyReadinessPageRequest  betweenchannel.TopologyReadinessRequest
	repairedBinding               betweenchannel.RepairModelBindingRequest
	enabledTopology               betweenchannel.EnableTopologyRequest
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

func (s *recordingLaneService) StartRollout(
	_ context.Context,
	req betweenchannel.StartRolloutRequest,
) (betweenchannel.StartRolloutResult, error) {
	s.startedRollout = req
	return betweenchannel.StartRolloutResult{
		Lane:    &betweenchannel.Lane{ID: req.LaneID, OrgID: req.OrgID},
		Rollout: &rolloutDomain.Rollout{ID: uuid.New(), OrgID: req.OrgID},
	}, nil
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

func (s *recordingLaneService) CreateModelDeclaration(
	_ context.Context,
	req betweenchannel.CreateModelDeclarationRequest,
) (*betweenchannel.Lane, error) {
	s.createdModel = req
	return &betweenchannel.Lane{ID: req.LaneID, OrgID: req.OrgID}, nil
}

func (s *recordingLaneService) PublishModelTarget(
	_ context.Context,
	req betweenchannel.PublishModelTargetRequest,
) (*betweenchannel.Lane, error) {
	s.publishedModel = req
	return &betweenchannel.Lane{ID: req.LaneID, OrgID: req.OrgID}, nil
}

func (s *recordingLaneService) PreviewModelMembershipChange(
	_ context.Context,
	_ betweenchannel.PreviewModelMembershipChangeRequest,
) (betweenchannel.MembershipChangePreview, error) {
	return betweenchannel.MembershipChangePreview{}, nil
}

func (s *recordingLaneService) UpdateModelMembership(
	_ context.Context,
	req betweenchannel.UpdateModelMembershipRequest,
) (betweenchannel.UpdateMembershipResult, error) {
	s.updatedModelMembership = req
	return betweenchannel.UpdateMembershipResult{
		Lane: &betweenchannel.Lane{ID: req.LaneID, OrgID: req.OrgID},
	}, nil
}

func (s *recordingLaneService) GetTopologyReadiness(
	_ context.Context,
	orgID int64,
) (betweenchannel.TopologyReadiness, error) {
	s.topologyReadiness.OrgID = orgID
	return s.topologyReadiness, nil
}

func (s *recordingLaneService) RepairModelBinding(
	_ context.Context,
	req betweenchannel.RepairModelBindingRequest,
) (betweenchannel.RepairModelBindingResult, error) {
	s.repairedBinding = req
	return betweenchannel.RepairModelBindingResult{
		BindingID:         uuid.New(),
		ResultingRevision: req.ExpectedRevision + 1,
		Readiness:         s.topologyReadiness,
	}, nil
}

func (s *recordingLaneService) EnableTopology(
	_ context.Context,
	req betweenchannel.EnableTopologyRequest,
) (betweenchannel.EnableTopologyResult, error) {
	s.enabledTopology = req
	s.topologyReadiness.Enabled = true
	s.topologyReadiness.Revision = req.ExpectedRevision + 1
	return betweenchannel.EnableTopologyResult{Readiness: s.topologyReadiness}, nil
}

func (s *recordingLaneService) GetTopologyReadinessPage(
	_ context.Context,
	_ int64,
	req betweenchannel.TopologyReadinessRequest,
) (betweenchannel.TopologyReadiness, error) {
	s.topologyReadinessPageRequest = req
	return s.topologyReadiness, nil
}

func (s *recordingRolloutService) Create(
	_ context.Context,
	req rolloutDomain.CreateRequest,
) (*rolloutDomain.Rollout, error) {
	s.createdRollout = req
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

func (s *recordingRolloutService) GetGroup(
	_ context.Context,
	orgID int64,
	groupID uuid.UUID,
) (*rolloutDomain.Group, error) {
	s.getGroupOrgID = orgID
	s.getGroupID = groupID
	if s.group != nil {
		return s.group, nil
	}
	return nil, rolloutDomain.ErrNotFound
}

func (s *recordingRolloutService) ListGroups(
	_ context.Context,
	orgID int64,
) ([]rolloutDomain.Group, error) {
	s.listGroupOrgID = orgID
	return s.groups, nil
}

func (s *recordingRolloutService) ListGroupsPage(
	_ context.Context,
	orgID int64,
	req rolloutDomain.ListPageRequest,
) (rolloutDomain.GroupPage, error) {
	s.listGroupOrgID = orgID
	s.groupPageReq = req
	return rolloutDomain.GroupPage{Groups: s.groups, HasMore: s.groupHasMore}, nil
}

func (s *recordingRolloutService) ListLegacyPage(
	_ context.Context,
	_ int64,
	req rolloutDomain.ListPageRequest,
) (rolloutDomain.RolloutPage, error) {
	s.legacyPageReq = req
	return rolloutDomain.RolloutPage{Rollouts: s.legacy, HasMore: s.legacyHasMore}, nil
}

func (s *recordingRolloutService) List(
	context.Context,
	int64,
	[]rolloutDomain.State,
) ([]rolloutDomain.Rollout, error) {
	if s.legacy != nil {
		return s.legacy, nil
	}
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
