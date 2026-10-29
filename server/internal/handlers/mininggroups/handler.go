package mininggroups

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/mininggroups/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	domain "github.com/block/proto-fleet/server/internal/domain/mininggroups"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	service  *domain.Service
	pools    *hostedaccounts.PoolService
	verifier hostedaccounts.CredentialsVerifier
}

func NewHandler(s *domain.Service, p *hostedaccounts.PoolService, verifier hostedaccounts.CredentialsVerifier) *Handler {
	return &Handler{service: s, pools: p, verifier: verifier}
}
func (h *Handler) GetState(ctx context.Context, r *connect.Request[pb.GetStateRequest]) (*connect.Response[pb.GetStateResponse], error) {
	state, err := h.service.GetState(ctx, r.Msg.DeviceIdentifier)
	if err != nil {
		return nil, err
	}
	o := state.Observed
	out := &pb.GetStateResponse{Enabled: o.Enabled, Suspended: o.Suspended, Supported: o.Supported, Revision: state.Record.Revision, Status: state.Record.Status, Message: state.Record.Message}
	if state.Record.ObservedAt.Valid {
		out.ObservedAt = timestamppb.New(state.Record.ObservedAt.Time)
	}
	if state.Record.OperationID.Valid {
		out.OperationId = state.Record.OperationID.UUID.String()
	}
	var bindings map[string]struct {
		ConfigID string `json:"config_id"`
	}
	if err := json.Unmarshal(state.Record.Bindings, &bindings); err != nil {
		return nil, fmt.Errorf("decode mining group bindings: %w", err)
	}
	for _, g := range o.Groups {
		v := &pb.Group{Id: g.ID, Name: g.Name, Revision: g.Revision, MacAddresses: g.MACs, Mining: g.Mining, PoolRevisionId: bindings[g.ID].ConfigID}
		for _, p := range g.Pools {
			v.Pools = append(v.Pools, &pb.Pool{Id: p.ID, Name: p.Name, Url: p.URL, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight})
		}
		out.Groups = append(out.Groups, v)
	}
	for _, d := range state.Desired {
		out.Desired = append(out.Desired, &pb.BoardIntent{HashboardId: d.HashboardID, MacAddress: d.MacAddress, PoolRevisionId: d.PoolRevisionID})
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) Activate(ctx context.Context, r *connect.Request[pb.ActivateRequest]) (*connect.Response[pb.ActivateResponse], error) {
	if !r.Msg.AcknowledgeInterruption {
		return nil, fleeterror.NewInvalidArgumentError("confirm mining interruption before enabling mining groups")
	}
	id, err := h.service.Submit(ctx, r.Msg.DeviceIdentifier, r.Msg.RequestId, "", nil, 0)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ActivateResponse{OperationId: id.String()}), nil
}
func (h *Handler) SetPools(ctx context.Context, r *connect.Request[pb.SetPoolsRequest]) (*connect.Response[pb.SetPoolsResponse], error) {
	if _, err := domain.OperatorPrincipal(ctx); err != nil {
		return nil, err
	}
	if err := h.verifier.VerifySessionCredentials(ctx, r.Msg.UserUsername, r.Msg.UserPassword); err != nil {
		return nil, err
	}
	id, err := h.service.Submit(ctx, r.Msg.DeviceIdentifier, r.Msg.RequestId, r.Msg.PoolRevisionId, r.Msg.HashboardIds, r.Msg.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetPoolsResponse{OperationId: id.String()}), nil
}
func (h *Handler) ResolveFleetPools(ctx context.Context, r *connect.Request[pb.ResolveFleetPoolsRequest]) (*connect.Response[pb.ResolveFleetPoolsResponse], error) {
	i, err := domain.OperatorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	revision, err := h.pools.ResolveHostFleetPools(ctx, i.UserID, i.OrganizationID, r.Msg.PoolIds)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResolveFleetPoolsResponse{Revision: poolDTO(revision)}), nil
}
func (h *Handler) ListPoolConfigurations(ctx context.Context, r *connect.Request[pb.ListPoolConfigurationsRequest]) (*connect.Response[pb.ListPoolConfigurationsResponse], error) {
	i, err := domain.OperatorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.pools.ListHost(ctx, i.UserID, i.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := &pb.ListPoolConfigurationsResponse{}
	for _, row := range rows {
		out.Configurations = append(out.Configurations, poolDTO(row))
	}
	return connect.NewResponse(out), nil
}
func (h *Handler) CreatePoolConfiguration(ctx context.Context, r *connect.Request[pb.CreatePoolConfigurationRequest]) (*connect.Response[pb.CreatePoolConfigurationResponse], error) {
	i, err := domain.OperatorPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	input := r.Msg.Configuration
	if input == nil {
		return nil, fleeterror.NewInvalidArgumentError("pool configuration required")
	}
	options := poolconfigurations.RevisionOptions{Name: input.Name}
	for _, p := range input.Slots {
		if p == nil {
			return nil, fleeterror.NewInvalidArgumentError("pool slot required")
		}
		options.Slots = append(options.Slots, poolconfigurations.SlotOptions{URL: p.Url, Username: p.Username, WorkerTemplate: p.WorkerTemplate, Password: p.Password})
	}
	row, err := h.pools.CreateHost(ctx, i.UserID, i.OrganizationID, options)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreatePoolConfigurationResponse{Revision: poolDTO(row)}), nil
}
func poolDTO(r hostedaccounts.PoolRevision) *hostedpb.PoolRevision {
	out := &hostedpb.PoolRevision{PoolId: r.PoolID, RevisionId: r.RevisionID, Number: r.Number, Name: r.Name, CreatedAt: timestamppb.New(r.CreatedAt), Archived: r.Archived}
	for _, p := range r.Slots {
		out.Slots = append(out.Slots, &hostedpb.PoolEndpoint{Priority: p.Priority, Url: p.URL, Username: p.Username, WorkerTemplate: p.WorkerTemplate, HasPassword: p.HasPassword})
	}
	return out
}
