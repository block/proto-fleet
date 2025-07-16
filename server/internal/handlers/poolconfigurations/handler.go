package poolconfigurations

import (
	"context"

	"connectrpc.com/connect"
	poolsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/poolconfigurations"
)

type Handler struct {
	service *poolconfigurations.Service
}

var _ poolsv1connect.PoolConfigurationsServiceHandler = &Handler{}

func NewHandler(svc *poolconfigurations.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) ListPoolConfigurationsWithPools(ctx context.Context, _ *connect.Request[poolsv1.ListPoolConfigurationsWithPoolsRequest]) (*connect.Response[poolsv1.ListPoolConfigurationsWithPoolsResponse], error) {
	poolConfigurationsWithPools, err := h.service.GetPoolConfigurationsWithPools(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(
		&poolsv1.ListPoolConfigurationsWithPoolsResponse{PoolConfigurationsWithPools: poolConfigurationsWithPools}), nil
}

func (h *Handler) CreatePoolConfiguration(ctx context.Context, r *connect.Request[poolsv1.CreatePoolConfigurationRequest]) (*connect.Response[poolsv1.CreatePoolConfigurationResponse], error) {
	poolConfiguration, err := h.service.CreatePoolConfiguration(ctx, r.Msg.PoolConfigurationConfig)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&poolsv1.CreatePoolConfigurationResponse{PoolConfiguration: poolConfiguration}), nil
}

func (h *Handler) DeletePoolConfiguration(ctx context.Context, r *connect.Request[poolsv1.DeletePoolConfigurationRequest]) (*connect.Response[poolsv1.DeletePoolConfigurationResponse], error) {
	err := h.service.DeletePoolConfiguration(ctx, r.Msg.PoolConfigurationId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&poolsv1.DeletePoolConfigurationResponse{}), nil
}

func (h *Handler) AddPoolToConfiguration(ctx context.Context, r *connect.Request[poolsv1.AddPoolToConfigurationRequest]) (*connect.Response[poolsv1.AddPoolToConfigurationResponse], error) {
	poolConfigurationPool, err := h.service.AddPoolToConfiguration(ctx, r.Msg.PoolConfigurationId, r.Msg.PoolId, r.Msg.Priority)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&poolsv1.AddPoolToConfigurationResponse{PoolConfigurationPool: poolConfigurationPool}), nil
}

func (h *Handler) RemovePoolFromConfiguration(ctx context.Context, r *connect.Request[poolsv1.RemovePoolFromConfigurationRequest]) (*connect.Response[poolsv1.RemovePoolFromConfigurationResponse], error) {
	err := h.service.RemovePoolFromConfiguration(ctx, r.Msg.PoolConfigurationPoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&poolsv1.RemovePoolFromConfigurationResponse{}), nil
}
