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

func (h *Handler) ListPoolConfigurations(ctx context.Context, _ *connect.Request[poolsv1.ListPoolConfigurationsRequest]) (*connect.Response[poolsv1.ListPoolConfigurationsResponse], error) {
	resp, err := h.service.ListPoolConfigurations(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) GetPoolConfiguration(ctx context.Context, r *connect.Request[poolsv1.GetPoolConfigurationRequest]) (*connect.Response[poolsv1.GetPoolConfigurationResponse], error) {
	resp, err := h.service.GetPoolConfiguration(ctx, r.Msg.Id)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) UpsertPoolConfiguration(ctx context.Context, r *connect.Request[poolsv1.UpsertPoolConfigurationRequest]) (*connect.Response[poolsv1.UpsertPoolConfigurationResponse], error) {
	resp, err := h.service.UpsertPoolConfiguration(ctx, r.Msg.Configuration, r.Msg.Pools)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) DeletePoolConfiguration(ctx context.Context, r *connect.Request[poolsv1.DeletePoolConfigurationRequest]) (*connect.Response[poolsv1.DeletePoolConfigurationResponse], error) {
	resp, err := h.service.DeletePoolConfiguration(ctx, r.Msg.ConfigurationId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}
