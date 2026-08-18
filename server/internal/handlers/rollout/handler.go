package rollout

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type rolloutService interface {
	Create(ctx context.Context, req rolloutDomain.CreateRequest) (*rolloutDomain.Rollout, error)
	Get(ctx context.Context, orgID int64, rolloutID uuid.UUID) (*rolloutDomain.Rollout, error)
	List(ctx context.Context, orgID int64, states []rolloutDomain.State) ([]rolloutDomain.Rollout, error)
	Admit(ctx context.Context, req rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Continue(ctx context.Context, req rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Pause(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Resume(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Abort(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Revert(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Complete(ctx context.Context, req rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
}

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
		includeInitialEnforcementMembers bool,
		initialEnforcementMembersUpdatedAfter *time.Time,
	) (*betweenchannel.Lane, error)
	ListLanes(ctx context.Context, orgID int64, activeInitialOnly bool) ([]betweenchannel.Lane, error)
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
	lane, err := h.laneService.CreateLane(ctx, betweenchannel.CreateLaneRequest{
		OrgID:                     info.OrganizationID,
		Label:                     req.Msg.GetLabel(),
		Description:               req.Msg.GetDescription(),
		FirmwareFileIDs:           req.Msg.GetFirmwareFileIds(),
		DeviceIdentifiers:         req.Msg.GetDeviceIdentifiers(),
		IdempotencyKey:            req.Msg.GetIdempotencyKey(),
		ActorUserID:               info.UserID,
		ConfirmInitialEnforcement: req.Msg.GetConfirmInitialEnforcement(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRolloutLaneResponse{
		Lane: laneToProto(lane),
	}), nil
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
	if value := req.Msg.GetInitialEnforcementMembersUpdatedAfter(); value != nil {
		if err = value.CheckValid(); err != nil {
			return nil, fleeterror.NewInvalidArgumentError(
				"initial_enforcement_members_updated_after must be a valid timestamp",
			)
		}
		parsed := value.AsTime()
		membersUpdatedAfter = &parsed
	}
	lane, err := h.laneService.GetLane(
		ctx,
		info.OrganizationID,
		laneID,
		req.Msg.GetIncludeInitialEnforcementMembers(),
		membersUpdatedAfter,
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutLaneResponse{
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
		req.Msg.GetActiveInitialOnly(),
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
	result, err := h.laneService.StartRollout(
		ctx,
		betweenchannel.StartRolloutRequest{
			OrgID:             info.OrganizationID,
			LaneID:            laneID,
			Name:              req.Msg.GetName(),
			FirmwareFileIDs:   req.Msg.GetFirmwareFileIds(),
			Batches:           batchesFromProto(req.Msg.GetBatches()),
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
	return connect.NewResponse(&pb.StartRolloutLaneResponse{
		Lane:    laneToProto(result.Lane),
		Rollout: rolloutToProto(result.Rollout),
	}), nil
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
		return nil, err
	}
	return connect.NewResponse(&pb.GetRolloutResponse{
		Rollout: rolloutToProto(result),
	}), nil
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
