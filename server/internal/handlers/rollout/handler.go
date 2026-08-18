package rollout

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type rolloutService interface {
	Create(context.Context, rolloutDomain.CreateRequest) (*rolloutDomain.Rollout, error)
	Get(context.Context, int64, uuid.UUID) (*rolloutDomain.Rollout, error)
	List(context.Context, int64, []rolloutDomain.State) ([]rolloutDomain.Rollout, error)
	Admit(context.Context, rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Continue(context.Context, rolloutDomain.AdmitRequest) (*rolloutDomain.Rollout, error)
	Pause(context.Context, rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Resume(context.Context, rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Abort(context.Context, rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Revert(context.Context, rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
	Complete(context.Context, rolloutDomain.ControlRequest) (*rolloutDomain.Rollout, error)
}

type Handler struct {
	service rolloutService
}

var _ rolloutv1connect.RolloutServiceHandler = (*Handler)(nil)

func NewHandler(service rolloutService) *Handler {
	return &Handler{service: service}
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
	result, err := h.service.Admit(ctx, rolloutDomain.AdmitRequest{
		OrgID:            info.OrganizationID,
		RolloutID:        rolloutID,
		BatchID:          req.Msg.GetBatchId(),
		ExpectedRevision: int64(req.Msg.GetExpectedRevision()),
		IdempotencyKey:   req.Msg.GetIdempotencyKey(),
		Reason:           req.Msg.GetReason(),
		ActorUserID:      info.UserID,
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
	result, err := h.service.Continue(ctx, rolloutDomain.AdmitRequest{
		OrgID:            info.OrganizationID,
		RolloutID:        rolloutID,
		ExpectedRevision: int64(req.Msg.GetExpectedRevision()),
		IdempotencyKey:   req.Msg.GetIdempotencyKey(),
		Reason:           req.Msg.GetReason(),
		ActorUserID:      info.UserID,
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
	rolloutID, err := parseRolloutID(rolloutIDValue)
	if err != nil {
		return rolloutDomain.ControlRequest{}, err
	}
	return rolloutDomain.ControlRequest{
		OrgID:            info.OrganizationID,
		RolloutID:        rolloutID,
		ExpectedRevision: int64(expectedRevision),
		IdempotencyKey:   idempotencyKey,
		Reason:           reason,
		ActorUserID:      info.UserID,
	}, nil
}
