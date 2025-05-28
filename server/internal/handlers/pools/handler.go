package pools

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/pools"
	"github.com/rsjethani/secret/v3"
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

func (h *Handler) ValidatePool(ctx context.Context, r *connect.Request[pb.ValidatePoolRequest]) (*connect.Response[pb.ValidatePoolResponse], error) {
	var pass *secret.Text
	if r.Msg.Password != nil {
		tmpPass := secret.New(r.Msg.Password.GetValue())
		pass = &tmpPass
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
