package rollout

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type rolloutService interface { //nolint:interfacebloat // Handler controls and aggregate reads share one rollout service.
	Create(ctx context.Context, req rolloutDomain.CreateRequest) (*rolloutDomain.Rollout, error)
	Get(ctx context.Context, orgID int64, rolloutID uuid.UUID) (*rolloutDomain.Rollout, error)
	GetGroup(ctx context.Context, orgID int64, groupID uuid.UUID) (*rolloutDomain.Group, error)
	ListGroups(ctx context.Context, orgID int64) ([]rolloutDomain.Group, error)
	List(ctx context.Context, orgID int64, states []rolloutDomain.State) ([]rolloutDomain.Rollout, error)
	Admit(ctx context.Context, req rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Continue(ctx context.Context, req rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Pause(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Resume(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Abort(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Revert(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Complete(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
}

//nolint:interfacebloat // The Connect handler exposes the complete rollout-lane service surface.
type laneService interface {
	PreviewLane(
		ctx context.Context,
		req betweenchannel.PreviewLaneRequest,
	) (betweenchannel.InitialEnforcementPreview, error)
	CreateLane(ctx context.Context, req betweenchannel.CreateLaneRequest) (*betweenchannel.Lane, error)
	GetLane(
		ctx context.Context,
		orgID int64,
		laneID uuid.UUID,
		includeFirmwareConvergenceMembers bool,
		firmwareConvergenceMembersUpdatedAfter *time.Time,
	) (*betweenchannel.Lane, error)
	GetLaneForRollout(
		ctx context.Context,
		orgID int64,
		rolloutID uuid.UUID,
	) (*betweenchannel.Lane, error)
	ListLanes(ctx context.Context, orgID int64, activeFirmwareConvergenceOnly bool) ([]betweenchannel.Lane, error)
	ListMembers(
		ctx context.Context,
		req betweenchannel.ListMembersRequest,
	) (betweenchannel.ListMembersResult, error)
	GetAssignments(
		ctx context.Context,
		orgID int64,
		deviceIdentifiers []string,
	) ([]betweenchannel.LaneAssignment, error)
	PreviewMembershipChange(
		ctx context.Context,
		req betweenchannel.PreviewMembershipChangeRequest,
	) (betweenchannel.MembershipChangePreview, error)
	UpdateMembership(
		ctx context.Context,
		req betweenchannel.UpdateMembershipRequest,
	) (betweenchannel.UpdateMembershipResult, error)
	CreateModelDeclaration(
		ctx context.Context,
		req betweenchannel.CreateModelDeclarationRequest,
	) (*betweenchannel.Lane, error)
	PublishModelTarget(
		ctx context.Context,
		req betweenchannel.PublishModelTargetRequest,
	) (*betweenchannel.Lane, error)
	PreviewModelMembershipChange(
		ctx context.Context,
		req betweenchannel.PreviewModelMembershipChangeRequest,
	) (betweenchannel.MembershipChangePreview, error)
	UpdateModelMembership(
		ctx context.Context,
		req betweenchannel.UpdateModelMembershipRequest,
	) (betweenchannel.UpdateMembershipResult, error)
	GetTopologyReadiness(
		ctx context.Context,
		orgID int64,
	) (betweenchannel.TopologyReadiness, error)
	RepairModelBinding(
		ctx context.Context,
		req betweenchannel.RepairModelBindingRequest,
	) (betweenchannel.RepairModelBindingResult, error)
	EnableTopology(
		ctx context.Context,
		req betweenchannel.EnableTopologyRequest,
	) (betweenchannel.EnableTopologyResult, error)
	DeleteLane(ctx context.Context, req betweenchannel.DeleteLaneRequest) error
	StartRollout(
		ctx context.Context,
		req betweenchannel.StartRolloutRequest,
	) (betweenchannel.StartRolloutResult, error)
}

type Handler struct {
	service     rolloutService
	laneService laneService
}

var _ rolloutv1connect.RolloutServiceHandler = (*Handler)(nil)

func NewHandler(service rolloutService, laneService laneService) *Handler {
	return &Handler{service: service, laneService: laneService}
}

func (h *Handler) PreviewRolloutLane(
	ctx context.Context,
	req *connect.Request[pb.PreviewRolloutLaneRequest],
) (*connect.Response[pb.PreviewRolloutLaneResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	preview, err := h.laneService.PreviewLane(ctx, betweenchannel.PreviewLaneRequest{
		OrgID:             info.OrganizationID,
		FirmwareFileIDs:   req.Msg.GetFirmwareFileIds(),
		DeviceIdentifiers: req.Msg.GetDeviceIdentifiers(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PreviewRolloutLaneResponse{
		Preview: lanePreviewToProto(preview),
	}), nil
}

func (h *Handler) CreateRolloutLane(
	ctx context.Context,
	req *connect.Request[pb.CreateRolloutLaneRequest],
) (*connect.Response[pb.CreateRolloutLaneResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	lane, err := h.laneService.CreateLane(ctx, betweenchannel.CreateLaneRequest{
		OrgID:                         info.OrganizationID,
		Label:                         req.Msg.GetLabel(),
		Description:                   req.Msg.GetDescription(),
		FirmwareFileIDs:               req.Msg.GetFirmwareFileIds(),
		DeviceIdentifiers:             req.Msg.GetDeviceIdentifiers(),
		IdempotencyKey:                req.Msg.GetIdempotencyKey(),
		ActorUserID:                   info.UserID,
		ActorType:                     actorType,
		ActorCredentialID:             actorCredentialID,
		ConfirmInitialEnforcement:     req.Msg.GetConfirmInitialEnforcement(),
		ConfirmReassignment:           req.Msg.GetConfirmReassignment(),
		ReassignmentConfirmationToken: req.Msg.GetReassignmentConfirmationToken(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRolloutLaneResponse{
		Lane: laneToProto(lane),
	}), nil
}

func (h *Handler) GetRolloutLaneAssignments(
	ctx context.Context,
	req *connect.Request[pb.GetRolloutLaneAssignmentsRequest],
) (*connect.Response[pb.GetRolloutLaneAssignmentsResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	assignments, err := h.laneService.GetAssignments(
		ctx,
		info.OrganizationID,
		req.Msg.GetDeviceIdentifiers(),
	)
	if err != nil {
		return nil, err
	}
	response := &pb.GetRolloutLaneAssignmentsResponse{
		Assignments: make([]*pb.RolloutLaneAssignment, 0, len(assignments)),
	}
	for _, assignment := range assignments {
		response.Assignments = append(response.Assignments, &pb.RolloutLaneAssignment{
			DeviceIdentifier: assignment.DeviceIdentifier,
			LaneId:           assignment.LaneID.String(),
			LaneLabel:        assignment.LaneLabel,
		})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) GetRolloutLane(
	ctx context.Context,
	req *connect.Request[pb.GetRolloutLaneRequest],
) (*connect.Response[pb.GetRolloutLaneResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	var membersUpdatedAfter *time.Time
	if value := req.Msg.GetFirmwareConvergenceMembersUpdatedAfter(); value != nil {
		if err = value.CheckValid(); err != nil {
			return nil, fleeterror.NewInvalidArgumentError(
				"firmware_convergence_members_updated_after must be a valid timestamp",
			)
		}
		parsed := value.AsTime()
		membersUpdatedAfter = &parsed
	}
	lane, err := h.laneService.GetLane(
		ctx,
		info.OrganizationID,
		laneID,
		req.Msg.GetIncludeFirmwareConvergenceMembers(),
		membersUpdatedAfter,
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutLaneResponse{
		Lane: laneToProto(lane),
	}), nil
}

func (h *Handler) GetRolloutLaneForRollout(
	ctx context.Context,
	req *connect.Request[pb.GetRolloutLaneForRolloutRequest],
) (*connect.Response[pb.GetRolloutLaneForRolloutResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	rolloutID, err := parseRolloutID(req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	lane, err := h.laneService.GetLaneForRollout(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutLaneForRolloutResponse{
		Lane: laneToProto(lane),
	}), nil
}

func (h *Handler) ListRolloutLanes(
	ctx context.Context,
	req *connect.Request[pb.ListRolloutLanesRequest],
) (*connect.Response[pb.ListRolloutLanesResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	lanes, err := h.laneService.ListLanes(
		ctx,
		info.OrganizationID,
		req.Msg.GetActiveFirmwareConvergenceOnly(),
	)
	if err != nil {
		return nil, err
	}
	response := &pb.ListRolloutLanesResponse{
		Lanes: make([]*pb.RolloutLane, 0, len(lanes)),
	}
	for index := range lanes {
		response.Lanes = append(response.Lanes, laneToProto(&lanes[index]))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) ListRolloutLaneMembers(
	ctx context.Context,
	req *connect.Request[pb.ListRolloutLaneMembersRequest],
) (*connect.Response[pb.ListRolloutLaneMembersResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	var laneModelID uuid.UUID
	if req.Msg.GetLaneModelId() != "" {
		laneModelID, err = uuid.Parse(req.Msg.GetLaneModelId())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid rollout lane model ID")
		}
	}
	pageToken, err := decodeLaneMemberPageToken(req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	if pageToken.LaneID != uuid.Nil && pageToken.LaneID != laneID {
		return nil, fleeterror.NewInvalidArgumentError(
			"rollout lane member page token belongs to a different lane",
		)
	}
	if pageToken.LaneID != uuid.Nil {
		tokenLaneModelID := uuid.Nil
		if pageToken.LaneModelID != nil {
			tokenLaneModelID = *pageToken.LaneModelID
		}
		if tokenLaneModelID != laneModelID {
			return nil, fleeterror.NewInvalidArgumentError(
				"rollout lane member page token belongs to a different model declaration",
			)
		}
	}
	result, err := h.laneService.ListMembers(ctx, betweenchannel.ListMembersRequest{
		OrgID:             info.OrganizationID,
		LaneID:            laneID,
		LaneModelID:       laneModelID,
		ExpectedRevision:  pageToken.Revision,
		AfterIdentifier:   pageToken.Cursor,
		Limit:             int32(req.Msg.GetPageSize()), //nolint:gosec // Proto validation caps page size at 1000.
		IncludeTotalCount: req.Msg.GetIncludeTotalCount(),
	})
	if err != nil {
		return nil, err
	}
	response := &pb.ListRolloutLaneMembersResponse{
		Members:    make([]*pb.RolloutLaneMember, 0, len(result.Members)),
		TotalCount: nonNegativeUint32(int32(result.TotalCount)), //nolint:gosec // Lane membership is API bounded.
	}
	for _, member := range result.Members {
		response.Members = append(response.Members, laneMemberToProto(member))
	}
	if result.NextIdentifier != "" {
		var tokenLaneModelID *uuid.UUID
		if laneModelID != uuid.Nil {
			value := laneModelID
			tokenLaneModelID = &value
		}
		response.NextPageToken = encodeLaneMemberPageToken(laneMemberPageToken{
			Version: laneMemberPageTokenVersion, LaneID: laneID,
			LaneModelID: tokenLaneModelID, Revision: result.Revision,
			Cursor: result.NextIdentifier,
		})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) PreviewRolloutLaneMembershipChange(
	ctx context.Context,
	req *connect.Request[pb.PreviewRolloutLaneMembershipChangeRequest],
) (*connect.Response[pb.PreviewRolloutLaneMembershipChangeResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	preview, err := h.laneService.PreviewMembershipChange(
		ctx,
		betweenchannel.PreviewMembershipChangeRequest{
			OrgID:             info.OrganizationID,
			LaneID:            laneID,
			AddIdentifiers:    req.Msg.GetAddDeviceIdentifiers(),
			RemoveIdentifiers: req.Msg.GetRemoveDeviceIdentifiers(),
		},
	)
	if err != nil {
		return nil, err
	}
	response := &pb.PreviewRolloutLaneMembershipChangeResponse{
		TargetFirmwarePreview:            lanePreviewToProto(preview.TargetFirmwarePreview),
		Reassignments:                    make([]*pb.RolloutLaneMembershipReassignment, 0, len(preview.Reassignments)),
		Removals:                         make([]*pb.RolloutLaneMember, 0, len(preview.Removals)),
		RequiresFirmwareConfirmation:     preview.RequiresFirmwareConfirmation,
		RequiresReassignmentConfirmation: preview.RequiresReassignConfirmation,
	}
	for _, reassignment := range preview.Reassignments {
		response.Reassignments = append(
			response.Reassignments,
			&pb.RolloutLaneMembershipReassignment{
				DeviceIdentifier: reassignment.DeviceIdentifier,
				SourceLaneId:     reassignment.SourceLaneID.String(),
				SourceLaneLabel:  reassignment.SourceLaneLabel,
			},
		)
	}
	for _, removal := range preview.Removals {
		response.Removals = append(response.Removals, laneMemberToProto(removal))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) UpdateRolloutLaneMembership(
	ctx context.Context,
	req *connect.Request[pb.UpdateRolloutLaneMembershipRequest],
) (*connect.Response[pb.UpdateRolloutLaneMembershipResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.laneService.UpdateMembership(
		ctx,
		betweenchannel.UpdateMembershipRequest{
			OrgID:             info.OrganizationID,
			LaneID:            laneID,
			ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow fails domain validation.
			AddIdentifiers:    req.Msg.GetAddDeviceIdentifiers(),
			RemoveIdentifiers: req.Msg.GetRemoveDeviceIdentifiers(),
			ConfirmFirmware:   req.Msg.GetConfirmFirmware(),
			ConfirmReassign:   req.Msg.GetConfirmReassign(),
			IdempotencyKey:    req.Msg.GetIdempotencyKey(),
			Reason:            req.Msg.GetReason(),
			ActorUserID:       info.UserID,
			ActorType:         actorType,
			ActorCredentialID: actorCredentialID,
		},
	)
	if err != nil {
		return nil, err
	}
	response := &pb.UpdateRolloutLaneMembershipResponse{
		Lane:              laneToProto(result.Lane),
		TransitionMembers: make([]*pb.RolloutLaneMember, 0, len(result.TransitionMembers)),
	}
	for _, member := range result.TransitionMembers {
		response.TransitionMembers = append(
			response.TransitionMembers,
			laneMemberToProto(member),
		)
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) PreviewRolloutLaneModelDeclaration(
	ctx context.Context,
	req *connect.Request[pb.PreviewRolloutLaneModelDeclarationRequest],
) (*connect.Response[pb.PreviewRolloutLaneModelDeclarationResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError("rollout lane service is not registered")
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	preview, err := h.laneService.PreviewLane(ctx, betweenchannel.PreviewLaneRequest{
		OrgID:             info.OrganizationID,
		FirmwareFileIDs:   []string{req.Msg.GetFirmwareFileId()},
		DeviceIdentifiers: req.Msg.GetDeviceIdentifiers(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PreviewRolloutLaneModelDeclarationResponse{
		Preview: lanePreviewToProto(preview),
		LaneId:  laneID.String(),
	}), nil
}

func (h *Handler) CreateRolloutLaneModelDeclaration(
	ctx context.Context,
	req *connect.Request[pb.CreateRolloutLaneModelDeclarationRequest],
) (*connect.Response[pb.CreateRolloutLaneModelDeclarationResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError("rollout lane service is not registered")
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	lane, err := h.laneService.CreateModelDeclaration(
		ctx,
		betweenchannel.CreateModelDeclarationRequest{
			OrgID:                         info.OrganizationID,
			LaneID:                        laneID,
			ExpectedRevision:              int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Zero is the only accepted creation revision.
			FirmwareFileIDs:               []string{req.Msg.GetFirmwareFileId()},
			DeviceIdentifiers:             req.Msg.GetDeviceIdentifiers(),
			IdempotencyKey:                req.Msg.GetIdempotencyKey(),
			Reason:                        req.Msg.GetReason(),
			ActorUserID:                   info.UserID,
			ActorType:                     actorType,
			ActorCredentialID:             actorCredentialID,
			ConfirmInitialEnforcement:     req.Msg.GetConfirmInitialEnforcement(),
			ConfirmReassignment:           req.Msg.GetConfirmReassignment(),
			ReassignmentConfirmationToken: req.Msg.GetReassignmentConfirmationToken(),
		},
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRolloutLaneModelDeclarationResponse{
		Lane: laneToProto(lane),
	}), nil
}

func (h *Handler) PublishRolloutLaneModelTarget(
	ctx context.Context,
	req *connect.Request[pb.PublishRolloutLaneModelTargetRequest],
) (*connect.Response[pb.PublishRolloutLaneModelTargetResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError("rollout lane service is not registered")
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	laneModelID, modelIdentityKey, err := parseLaneModelSelector(req.Msg.GetDeclaration())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	lane, err := h.laneService.PublishModelTarget(ctx, betweenchannel.PublishModelTargetRequest{
		OrgID:             info.OrganizationID,
		LaneID:            laneID,
		LaneModelID:       laneModelID,
		ModelIdentityKey:  modelIdentityKey,
		ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow fails domain validation.
		FirmwareFileIDs:   []string{req.Msg.GetFirmwareFileId()},
		IdempotencyKey:    req.Msg.GetIdempotencyKey(),
		Reason:            req.Msg.GetReason(),
		ActorUserID:       info.UserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PublishRolloutLaneModelTargetResponse{Lane: laneToProto(lane)}), nil
}

func (h *Handler) PreviewRolloutLaneModelMembershipChange(
	ctx context.Context,
	req *connect.Request[pb.PreviewRolloutLaneModelMembershipChangeRequest],
) (*connect.Response[pb.PreviewRolloutLaneModelMembershipChangeResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError("rollout lane service is not registered")
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	laneModelID, modelIdentityKey, err := parseLaneModelSelector(req.Msg.GetDeclaration())
	if err != nil {
		return nil, err
	}
	preview, err := h.laneService.PreviewModelMembershipChange(
		ctx,
		betweenchannel.PreviewModelMembershipChangeRequest{
			OrgID: info.OrganizationID, LaneID: laneID,
			LaneModelID: laneModelID, ModelIdentityKey: modelIdentityKey,
			AddIdentifiers:    req.Msg.GetAddDeviceIdentifiers(),
			RemoveIdentifiers: req.Msg.GetRemoveDeviceIdentifiers(),
		},
	)
	if err != nil {
		return nil, err
	}
	response := &pb.PreviewRolloutLaneModelMembershipChangeResponse{
		TargetFirmwarePreview:            lanePreviewToProto(preview.TargetFirmwarePreview),
		RequiresFirmwareConfirmation:     preview.RequiresFirmwareConfirmation,
		RequiresReassignmentConfirmation: preview.RequiresReassignConfirmation,
		Reassignments:                    make([]*pb.RolloutLaneMembershipReassignment, 0, len(preview.Reassignments)),
		Removals:                         make([]*pb.RolloutLaneMember, 0, len(preview.Removals)),
	}
	for _, reassignment := range preview.Reassignments {
		response.Reassignments = append(response.Reassignments, &pb.RolloutLaneMembershipReassignment{
			DeviceIdentifier: reassignment.DeviceIdentifier,
			SourceLaneId:     reassignment.SourceLaneID.String(),
			SourceLaneLabel:  reassignment.SourceLaneLabel,
		})
	}
	for _, removal := range preview.Removals {
		response.Removals = append(response.Removals, laneMemberToProto(removal))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) UpdateRolloutLaneModelMembership(
	ctx context.Context,
	req *connect.Request[pb.UpdateRolloutLaneModelMembershipRequest],
) (*connect.Response[pb.UpdateRolloutLaneModelMembershipResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError("rollout lane service is not registered")
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	laneModelID, modelIdentityKey, err := parseLaneModelSelector(req.Msg.GetDeclaration())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.laneService.UpdateModelMembership(ctx, betweenchannel.UpdateModelMembershipRequest{
		OrgID: info.OrganizationID, LaneID: laneID,
		LaneModelID: laneModelID, ModelIdentityKey: modelIdentityKey,
		ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow fails domain validation.
		AddIdentifiers:    req.Msg.GetAddDeviceIdentifiers(),
		RemoveIdentifiers: req.Msg.GetRemoveDeviceIdentifiers(),
		ConfirmFirmware:   req.Msg.GetConfirmFirmware(),
		ConfirmReassign:   req.Msg.GetConfirmReassign(),
		IdempotencyKey:    req.Msg.GetIdempotencyKey(), Reason: req.Msg.GetReason(),
		ActorUserID: info.UserID, ActorType: actorType, ActorCredentialID: actorCredentialID,
	})
	if err != nil {
		return nil, err
	}
	response := &pb.UpdateRolloutLaneModelMembershipResponse{
		Lane:              laneToProto(result.Lane),
		TransitionMembers: make([]*pb.RolloutLaneMember, 0, len(result.TransitionMembers)),
	}
	for _, member := range result.TransitionMembers {
		response.TransitionMembers = append(response.TransitionMembers, laneMemberToProto(member))
	}
	return connect.NewResponse(response), nil
}

func parseLaneModelSelector(
	selector *pb.RolloutLaneModelSelector,
) (uuid.UUID, string, error) {
	if selector == nil {
		return uuid.Nil, "", fleeterror.NewInvalidArgumentError(
			"rollout lane model declaration selector is required",
		)
	}
	switch value := selector.GetSelector().(type) {
	case *pb.RolloutLaneModelSelector_LaneModelId:
		id, err := uuid.Parse(value.LaneModelId)
		if err != nil {
			return uuid.Nil, "", fleeterror.NewInvalidArgumentError(
				"invalid rollout lane model declaration ID",
			)
		}
		return id, "", nil
	case *pb.RolloutLaneModelSelector_ModelIdentityKey:
		if value.ModelIdentityKey == "" {
			return uuid.Nil, "", fleeterror.NewInvalidArgumentError(
				"rollout lane model identity key is required",
			)
		}
		return uuid.Nil, value.ModelIdentityKey, nil
	default:
		return uuid.Nil, "", fleeterror.NewInvalidArgumentError(
			"exactly one rollout lane model declaration selector is required",
		)
	}
}

func (h *Handler) DeleteRolloutLane(
	ctx context.Context,
	req *connect.Request[pb.DeleteRolloutLaneRequest],
) (*connect.Response[pb.DeleteRolloutLaneResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	if err = h.laneService.DeleteLane(ctx, betweenchannel.DeleteLaneRequest{
		OrgID:             info.OrganizationID,
		LaneID:            laneID,
		ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow becomes negative and fails domain validation.
		IdempotencyKey:    req.Msg.GetIdempotencyKey(),
		Reason:            req.Msg.GetReason(),
		ActorUserID:       info.UserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteRolloutLaneResponse{}), nil
}

func (h *Handler) GetRolloutLaneTopologyReadiness(
	ctx context.Context,
	_ *connect.Request[pb.GetRolloutLaneTopologyReadinessRequest],
) (*connect.Response[pb.GetRolloutLaneTopologyReadinessResponse], error) {
	info, err := middleware.RequireAnyPermission(
		ctx,
		[]string{authz.PermChannelRead, authz.PermChannelManage},
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	readiness, err := h.laneService.GetTopologyReadiness(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	translated, err := topologyReadinessToProto(readiness)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutLaneTopologyReadinessResponse{
		Readiness: translated,
	}), nil
}

func (h *Handler) RepairRolloutLaneModelBinding(
	ctx context.Context,
	req *connect.Request[pb.RepairRolloutLaneModelBindingRequest],
) (*connect.Response[pb.RepairRolloutLaneModelBindingResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	laneModelID, err := uuid.Parse(req.Msg.GetLaneModelId())
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid rollout lane model ID")
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.laneService.RepairModelBinding(
		ctx,
		betweenchannel.RepairModelBindingRequest{
			OrgID:             info.OrganizationID,
			LaneID:            laneID,
			LaneModelID:       laneModelID,
			DeviceIdentifier:  req.Msg.GetDeviceIdentifier(),
			ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow fails validation.
			IdempotencyKey:    req.Msg.GetIdempotencyKey(),
			Reason:            req.Msg.GetReason(),
			ActorUserID:       info.UserID,
			ActorType:         actorType,
			ActorCredentialID: actorCredentialID,
		},
	)
	if err != nil {
		return nil, err
	}
	readiness, err := topologyReadinessToProto(result.Readiness)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RepairRolloutLaneModelBindingResponse{
		BindingId:         result.BindingID.String(),
		ResultingRevision: uint64(result.ResultingRevision), //nolint:gosec // Revisions are positive.
		Replayed:          result.Replayed,
		Readiness:         readiness,
	}), nil
}

func (h *Handler) EnableRolloutLaneModelTopology(
	ctx context.Context,
	req *connect.Request[pb.EnableRolloutLaneModelTopologyRequest],
) (*connect.Response[pb.EnableRolloutLaneModelTopologyResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.laneService.EnableTopology(
		ctx,
		betweenchannel.EnableTopologyRequest{
			OrgID:             info.OrganizationID,
			ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow fails validation.
			IdempotencyKey:    req.Msg.GetIdempotencyKey(),
			Reason:            req.Msg.GetReason(),
			ActorUserID:       info.UserID,
			ActorType:         actorType,
			ActorCredentialID: actorCredentialID,
		},
	)
	if err != nil {
		return nil, err
	}
	readiness, err := topologyReadinessToProto(result.Readiness)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.EnableRolloutLaneModelTopologyResponse{
		Readiness: readiness,
		Replayed:  result.Replayed,
	}), nil
}

func topologyReadinessToProto(
	readiness betweenchannel.TopologyReadiness,
) (*pb.RolloutLaneTopologyReadiness, error) {
	result := &pb.RolloutLaneTopologyReadiness{
		Enabled:                  readiness.Enabled,
		Revision:                 uint64(readiness.Revision),                 //nolint:gosec // Revisions are positive.
		AnomalyCount:             uint64(readiness.AnomalyCount),             //nolint:gosec // Counts are non-negative.
		ActiveLegacyRolloutCount: uint64(readiness.ActiveLegacyRolloutCount), //nolint:gosec // Counts are non-negative.
		Anomalies:                make([]*pb.RolloutLaneTopologyAnomaly, 0, len(readiness.Anomalies)),
		UpdatedAt:                timestamppb.New(readiness.UpdatedAt),
	}
	for _, anomaly := range readiness.Anomalies {
		details, err := structpb.NewStruct(anomaly.Details)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf(
				"invalid rollout lane topology anomaly details: %v",
				err,
			)
		}
		translated := &pb.RolloutLaneTopologyAnomaly{
			AnomalyId:        anomaly.ID.String(),
			LaneId:           anomaly.LaneID.String(),
			DeviceIdentifier: anomaly.DeviceIdentifier,
			Type:             topologyAnomalyTypeToProto(anomaly.Type),
			SupportedRepairActions: make(
				[]pb.RolloutLaneTopologyRepairAction,
				0,
				len(anomaly.SupportedRepairActions),
			),
			Details: details,
		}
		if anomaly.LaneModelID != nil {
			translated.LaneModelId = anomaly.LaneModelID.String()
		}
		if anomaly.LaneModelRevision != nil {
			translated.LaneModelRevision = uint64(*anomaly.LaneModelRevision) //nolint:gosec // Revisions are positive.
		}
		for _, action := range anomaly.SupportedRepairActions {
			translated.SupportedRepairActions = append(
				translated.SupportedRepairActions,
				topologyRepairActionToProto(action),
			)
		}
		result.Anomalies = append(result.Anomalies, translated)
	}
	return result, nil
}

func topologyAnomalyTypeToProto(
	value betweenchannel.TopologyAnomalyType,
) pb.RolloutLaneTopologyAnomalyType {
	switch value {
	case betweenchannel.TopologyAnomalyNullIdentity:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_NULL_IDENTITY
	case betweenchannel.TopologyAnomalyAmbiguousTargetMatch:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_AMBIGUOUS_TARGET_MATCH
	case betweenchannel.TopologyAnomalyNoTargetMatch:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_NO_TARGET_MATCH
	case betweenchannel.TopologyAnomalyPhysicalMismatch:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_PHYSICAL_MISMATCH
	case betweenchannel.TopologyAnomalyMissingBinding:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_MISSING_BINDING
	case betweenchannel.TopologyAnomalyDuplicateActiveBinding:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_DUPLICATE_ACTIVE_BINDING
	default:
		return pb.RolloutLaneTopologyAnomalyType_ROLLOUT_LANE_TOPOLOGY_ANOMALY_TYPE_UNSPECIFIED
	}
}

func topologyRepairActionToProto(
	value betweenchannel.TopologyRepairAction,
) pb.RolloutLaneTopologyRepairAction {
	switch value {
	case betweenchannel.TopologyRepairConfirmIdentity:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_CONFIRM_IDENTITY
	case betweenchannel.TopologyRepairSelectDeclaration:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_SELECT_DECLARATION
	case betweenchannel.TopologyRepairPhysicalMembership:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_REPAIR_PHYSICAL_MEMBERSHIP
	case betweenchannel.TopologyRepairEndStaleBinding:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_END_STALE_BINDING
	case betweenchannel.TopologyRepairBinding:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_REPAIR_BINDING
	case betweenchannel.TopologyRepairRerunBackfill:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_RERUN_BACKFILL
	default:
		return pb.RolloutLaneTopologyRepairAction_ROLLOUT_LANE_TOPOLOGY_REPAIR_ACTION_UNSPECIFIED
	}
}

const laneMemberPageTokenVersion = 1

type laneMemberPageToken struct {
	Version     int        `json:"v"`
	LaneID      uuid.UUID  `json:"lane_id"`
	LaneModelID *uuid.UUID `json:"lane_model_id,omitempty"`
	Revision    int64      `json:"revision"`
	Cursor      string     `json:"cursor"`
}

func encodeLaneMemberPageToken(token laneMemberPageToken) string {
	encoded, err := json.Marshal(token)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeLaneMemberPageToken(value string) (laneMemberPageToken, error) {
	if value == "" {
		return laneMemberPageToken{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 {
		return laneMemberPageToken{}, fleeterror.NewInvalidArgumentError(
			"invalid rollout lane member page token",
		)
	}
	var token laneMemberPageToken
	if err = json.Unmarshal(decoded, &token); err != nil ||
		token.Version != laneMemberPageTokenVersion ||
		token.LaneID == uuid.Nil ||
		(token.LaneModelID != nil && *token.LaneModelID == uuid.Nil) ||
		token.Revision <= 0 ||
		token.Cursor == "" {
		return laneMemberPageToken{}, fleeterror.NewInvalidArgumentError(
			"invalid rollout lane member page token",
		)
	}
	return token, nil
}

func (h *Handler) StartRolloutLane(
	ctx context.Context,
	req *connect.Request[pb.StartRolloutLaneRequest],
) (*connect.Response[pb.StartRolloutLaneResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	if _, err = middleware.RequirePermission(
		ctx,
		authz.PermChannelManage,
		authz.ResourceContext{},
	); err != nil {
		return nil, err
	}
	if h.laneService == nil {
		return nil, fleeterror.NewUnimplementedError(
			"rollout lane service is not registered",
		)
	}
	laneID, err := parseLaneID(req.Msg.GetLaneId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	modelPlans := make([]betweenchannel.StartRolloutModelPlan, 0, len(req.Msg.GetModelPlans()))
	for _, input := range req.Msg.GetModelPlans() {
		laneModelID, parseErr := uuid.Parse(input.GetLaneModelId())
		if parseErr != nil {
			return nil, fleeterror.NewInvalidArgumentError("lane_model_id must be a UUID")
		}
		modelPlans = append(modelPlans, betweenchannel.StartRolloutModelPlan{
			LaneModelID:           laneModelID,
			ExpectedModelRevision: int64(input.GetExpectedModelRevision()), //nolint:gosec // Overflow fails domain validation.
			FirmwareFileID:        input.GetFirmwareFileId(),
			Batches:               batchesFromProto(input.GetBatches()),
			HashratePolicy:        hashratePolicyFromProto(input.GetHashratePolicy()),
			ModelStartKey:         input.GetModelStartKey(),
		})
	}
	result, err := h.laneService.StartRollout(
		ctx,
		betweenchannel.StartRolloutRequest{
			OrgID:             info.OrganizationID,
			LaneID:            laneID,
			Name:              req.Msg.GetName(),
			FirmwareFileIDs:   req.Msg.GetFirmwareFileIds(),
			Batches:           batchesFromProto(req.Msg.GetBatches()),
			HashratePolicy:    hashratePolicyFromProto(req.Msg.GetHashratePolicy()),
			IdempotencyKey:    req.Msg.GetIdempotencyKey(),
			Reason:            req.Msg.GetReason(),
			ActorUserID:       info.UserID,
			ActorType:         actorType,
			ActorCredentialID: actorCredentialID,
			ModelPlans:        modelPlans,
		},
	)
	if err != nil {
		return nil, err
	}
	response := &pb.StartRolloutLaneResponse{
		Lane:     laneToProto(result.Lane),
		Rollout:  rolloutToProto(result.Rollout),
		Parent:   groupToProto(result.Parent),
		Children: make([]*pb.StartedRolloutLaneModel, 0, len(result.Children)),
	}
	for _, child := range result.Children {
		response.Children = append(response.Children, &pb.StartedRolloutLaneModel{
			Child:        rolloutToProto(child.Child),
			FirstBatchId: child.FirstBatchID,
		})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) CreateRollout(
	ctx context.Context,
	req *connect.Request[pb.CreateRolloutRequest],
) (*connect.Response[pb.CreateRolloutResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutManage,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	created, err := h.service.Create(ctx, createRequestFromProto(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRolloutResponse{
		Rollout: rolloutToProto(created),
	}), nil
}

func (h *Handler) GetRollout(
	ctx context.Context,
	req *connect.Request[pb.GetRolloutRequest],
) (*connect.Response[pb.GetRolloutResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	rolloutID, err := parseRolloutID(req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	result, err := h.service.Get(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		if !errors.Is(err, rolloutDomain.ErrParentNotControllable) {
			return nil, err
		}
		parent, parentErr := h.service.GetGroup(ctx, info.OrganizationID, rolloutID)
		if parentErr == nil {
			return connect.NewResponse(&pb.GetRolloutResponse{
				Parent: groupToProto(parent),
			}), nil
		}
		return nil, err
	}
	var parent *pb.RolloutGroup
	if result.GroupID != nil {
		group, groupErr := h.service.GetGroup(ctx, info.OrganizationID, *result.GroupID)
		if groupErr != nil {
			return nil, groupErr
		}
		parent = groupToProto(group)
	}
	return connect.NewResponse(&pb.GetRolloutResponse{
		Rollout: rolloutToProto(result),
		Parent:  parent,
	}), nil
}

func (h *Handler) GetRolloutGroup(
	ctx context.Context,
	req *connect.Request[pb.GetRolloutGroupRequest],
) (*connect.Response[pb.GetRolloutGroupResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	parentID, err := parseRolloutID(req.Msg.GetParentId())
	if err != nil {
		return nil, err
	}
	parent, err := h.service.GetGroup(ctx, info.OrganizationID, parentID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutGroupResponse{
		Parent: groupToProto(parent),
	}), nil
}

func (h *Handler) ListRolloutGroups(
	ctx context.Context,
	_ *connect.Request[pb.ListRolloutGroupsRequest],
) (*connect.Response[pb.ListRolloutGroupsResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	parents, err := h.service.ListGroups(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	legacy, err := h.service.List(ctx, info.OrganizationID, nil)
	if err != nil {
		return nil, err
	}
	response := &pb.ListRolloutGroupsResponse{
		Parents:       make([]*pb.RolloutGroup, 0, len(parents)),
		LegacyHistory: make([]*pb.Rollout, 0, len(legacy)),
	}
	for index := range parents {
		response.Parents = append(response.Parents, groupToProto(&parents[index]))
	}
	for index := range legacy {
		response.LegacyHistory = append(response.LegacyHistory, rolloutToProto(&legacy[index]))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) ListRollouts(
	ctx context.Context,
	req *connect.Request[pb.ListRolloutsRequest],
) (*connect.Response[pb.ListRolloutsResponse], error) {
	info, err := middleware.RequirePermission(
		ctx,
		authz.PermRolloutRead,
		authz.ResourceContext{},
	)
	if err != nil {
		return nil, err
	}
	states, err := rolloutStatesFromProto(req.Msg.GetStates())
	if err != nil {
		return nil, err
	}
	results, err := h.service.List(ctx, info.OrganizationID, states)
	if err != nil {
		return nil, err
	}
	response := &pb.ListRolloutsResponse{
		Rollouts: make([]*pb.Rollout, 0, len(results)),
	}
	for index := range results {
		response.Rollouts = append(response.Rollouts, rolloutToProto(&results[index]))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) AdmitRollout(
	ctx context.Context,
	req *connect.Request[pb.AdmitRolloutRequest],
) (*connect.Response[pb.AdmitRolloutResponse], error) {
	info, err := requireRolloutControl(ctx)
	if err != nil {
		return nil, err
	}
	rolloutID, err := parseRolloutID(req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.service.Admit(ctx, rolloutDomain.AdmitRequest{
		OrgID:             info.OrganizationID,
		RolloutID:         rolloutID,
		BatchID:           req.Msg.GetBatchId(),
		ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow becomes negative and fails domain validation.
		IdempotencyKey:    req.Msg.GetIdempotencyKey(),
		Reason:            req.Msg.GetReason(),
		ActorUserID:       info.UserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdmitRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) ContinueRollout(
	ctx context.Context,
	req *connect.Request[pb.ContinueRolloutRequest],
) (*connect.Response[pb.ContinueRolloutResponse], error) {
	info, err := requireRolloutControl(ctx)
	if err != nil {
		return nil, err
	}
	rolloutID, err := parseRolloutID(req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	result, err := h.service.Continue(ctx, rolloutDomain.AdmitRequest{
		OrgID:             info.OrganizationID,
		RolloutID:         rolloutID,
		ExpectedRevision:  int64(req.Msg.GetExpectedRevision()), //nolint:gosec // Overflow becomes negative and fails domain validation.
		IdempotencyKey:    req.Msg.GetIdempotencyKey(),
		Reason:            req.Msg.GetReason(),
		ActorUserID:       info.UserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ContinueRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) PauseRollout(
	ctx context.Context,
	req *connect.Request[pb.PauseRolloutRequest],
) (*connect.Response[pb.PauseRolloutResponse], error) {
	control, err := controlRequest(
		ctx,
		req.Msg.GetRolloutId(),
		req.Msg.GetExpectedRevision(),
		req.Msg.GetIdempotencyKey(),
		req.Msg.GetReason(),
	)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Pause(ctx, control)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PauseRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) ResumeRollout(
	ctx context.Context,
	req *connect.Request[pb.ResumeRolloutRequest],
) (*connect.Response[pb.ResumeRolloutResponse], error) {
	control, err := controlRequest(
		ctx,
		req.Msg.GetRolloutId(),
		req.Msg.GetExpectedRevision(),
		req.Msg.GetIdempotencyKey(),
		req.Msg.GetReason(),
	)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Resume(ctx, control)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResumeRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) AbortRollout(
	ctx context.Context,
	req *connect.Request[pb.AbortRolloutRequest],
) (*connect.Response[pb.AbortRolloutResponse], error) {
	control, err := controlRequest(
		ctx,
		req.Msg.GetRolloutId(),
		req.Msg.GetExpectedRevision(),
		req.Msg.GetIdempotencyKey(),
		req.Msg.GetReason(),
	)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Abort(ctx, control)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AbortRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) RevertRollout(
	ctx context.Context,
	req *connect.Request[pb.RevertRolloutRequest],
) (*connect.Response[pb.RevertRolloutResponse], error) {
	control, err := controlRequest(
		ctx,
		req.Msg.GetRolloutId(),
		req.Msg.GetExpectedRevision(),
		req.Msg.GetIdempotencyKey(),
		req.Msg.GetReason(),
	)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Revert(ctx, control)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevertRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func (h *Handler) CompleteRollout(
	ctx context.Context,
	req *connect.Request[pb.CompleteRolloutRequest],
) (*connect.Response[pb.CompleteRolloutResponse], error) {
	control, err := controlRequest(
		ctx,
		req.Msg.GetRolloutId(),
		req.Msg.GetExpectedRevision(),
		req.Msg.GetIdempotencyKey(),
		req.Msg.GetReason(),
	)
	if err != nil {
		return nil, err
	}
	control.WithFailures = req.Msg.GetWithFailures()
	result, err := h.service.Complete(ctx, control)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CompleteRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
}

func requireRolloutControl(ctx context.Context) (*session.Info, error) {
	return middleware.RequirePermission(
		ctx,
		authz.PermRolloutControl,
		authz.ResourceContext{},
	)
}

func controlRequest(
	ctx context.Context,
	rolloutIDValue string,
	expectedRevision uint64,
	idempotencyKey string,
	reason string,
) (rolloutDomain.ControlRequest, error) {
	info, err := requireRolloutControl(ctx)
	if err != nil {
		return rolloutDomain.ControlRequest{}, err
	}
	actorType, actorCredentialID := actorIdentityFromSession(info)
	rolloutID, err := parseRolloutID(rolloutIDValue)
	if err != nil {
		return rolloutDomain.ControlRequest{}, err
	}
	return rolloutDomain.ControlRequest{
		OrgID:             info.OrganizationID,
		RolloutID:         rolloutID,
		ExpectedRevision:  int64(expectedRevision), //nolint:gosec // Overflow becomes negative and fails domain validation.
		IdempotencyKey:    idempotencyKey,
		Reason:            reason,
		ActorUserID:       info.UserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	}, nil
}
