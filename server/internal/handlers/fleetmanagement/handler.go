package fleetmanagement

import (
	"connectrpc.com/connect"
	"context"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	fleetMgmtSvc *fleetmanagement.Service
}

var _ fleetmanagementv1connect.FleetManagementServiceHandler = &Handler{}

func NewHandler(fleetMgmtSvc *fleetmanagement.Service) *Handler {
	return &Handler{fleetMgmtSvc: fleetMgmtSvc}
}

func (h Handler) SetDefaultPool(ctx context.Context, r *connect.Request[pb.SetDefaultPoolRequest]) (*connect.Response[pb.SetDefaultPoolResponse], error) {
	err := h.fleetMgmtSvc.UpdateDefaultPool(ctx, fleetmanagement.UpdateDefaultPoolRequest{
		URL:        r.Msg.PoolConfig.Url,
		Username:   r.Msg.PoolConfig.Username,
		Password:   r.Msg.PoolConfig.Password,
		WorkerName: r.Msg.PoolConfig.WorkerName,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &connect.Response[pb.SetDefaultPoolResponse]{}, nil
}
