package pools

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/pools"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

type Handler struct {
	poolsSvc *pools.Service
}

var _ poolsv1connect.PoolsServiceHandler = &Handler{}

func NewHandler(svc *pools.Service) *Handler {
	return &Handler{
		poolsSvc: svc,
	}
}

func (h *Handler) ListPools(ctx context.Context, _ *connect.Request[pb.ListPoolsRequest]) (*connect.Response[pb.ListPoolsResponse], error) {
	listedPools, err := h.poolsSvc.ListPools(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListPoolsResponse{Pools: listedPools}), nil
}

func (h *Handler) CreatePool(ctx context.Context, r *connect.Request[pb.CreatePoolRequest]) (*connect.Response[pb.CreatePoolResponse], error) {
	pool, err := h.poolsSvc.CreatePool(ctx, r.Msg.PoolConfig)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.CreatePoolResponse{Pool: pool}), nil
}

func (h *Handler) UpdatePool(ctx context.Context, r *connect.Request[pb.UpdatePoolRequest]) (*connect.Response[pb.UpdatePoolResponse], error) {
	pool, err := h.poolsSvc.UpdatePool(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePoolResponse{Pool: pool}), nil
}

func (h *Handler) DeletePool(ctx context.Context, r *connect.Request[pb.DeletePoolRequest]) (*connect.Response[pb.DeletePoolResponse], error) {
	err := h.poolsSvc.DeletePool(ctx, r.Msg.PoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.DeletePoolResponse{}), nil
}

func (h *Handler) ValidatePool(ctx context.Context, r *connect.Request[pb.ValidatePoolRequest]) (*connect.Response[pb.ValidatePoolResponse], error) {
	var pass *secrets.Text
	if r.Msg.Password != nil {
		pass = secrets.NewText(r.Msg.Password.GetValue())
	}

	var timeout *time.Duration
	if r.Msg.Timeout != nil {
		tmp := r.Msg.Timeout.AsDuration()
		timeout = &tmp
	}

	ok, err := h.poolsSvc.ValidateConnection(ctx, r.Msg.Url, r.Msg.Username, pass, timeout)

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("failed to validate pool connection"))
	}
	return connect.NewResponse(
		&pb.ValidatePoolResponse{},
	), nil
}
