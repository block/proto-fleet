package mininggroups

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/mininggroups/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/mininggroups"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	service *domain.Service
}

func NewHandler(s *domain.Service) *Handler {
	return &Handler{service: s}
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
	id, err := h.service.Activate(ctx, r.Msg.DeviceIdentifier, r.Msg.RequestId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ActivateResponse{OperationId: id.String()}), nil
}
