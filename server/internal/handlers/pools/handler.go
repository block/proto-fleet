package pools

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/pools"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
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
	if _, err := middleware.RequirePermission(ctx, authz.PermPoolRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	listedPools, err := h.poolsSvc.ListPools(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListPoolsResponse{Pools: listedPools}), nil
}

func (h *Handler) CreatePool(ctx context.Context, r *connect.Request[pb.CreatePoolRequest]) (*connect.Response[pb.CreatePoolResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermPoolManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	pool, err := h.poolsSvc.CreatePool(ctx, r.Msg.PoolConfig)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.CreatePoolResponse{Pool: pool}), nil
}

func (h *Handler) UpdatePool(ctx context.Context, r *connect.Request[pb.UpdatePoolRequest]) (*connect.Response[pb.UpdatePoolResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermPoolManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	pool, err := h.poolsSvc.UpdatePool(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePoolResponse{Pool: pool}), nil
}

func (h *Handler) DeletePool(ctx context.Context, r *connect.Request[pb.DeletePoolRequest]) (*connect.Response[pb.DeletePoolResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermPoolManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	err := h.poolsSvc.DeletePool(ctx, r.Msg.PoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.DeletePoolResponse{}), nil
}

func (h *Handler) ValidatePool(ctx context.Context, r *connect.Request[pb.ValidatePoolRequest]) (*connect.Response[pb.ValidatePoolResponse], error) {
	// Pool validation drives an outbound Stratum/SV2 handshake against
	// the caller-supplied URL, so it's gated on pool:manage rather than
	// pool:read — it's the same authority as creating or editing a
	// saved pool and keeps a read-only role from triggering server-side
	// network probes against arbitrary addresses.
	if _, err := middleware.RequirePermission(ctx, authz.PermPoolManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
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
		if fleeterror.IsInvalidArgumentError(err) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("failed to validate pool connection"))
	}
	return connect.NewResponse(&pb.ValidatePoolResponse{}), nil
}
