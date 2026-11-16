package mininggroups

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/mininggroups/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/mininggroups"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
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
	out.DeactivationBlockedReason = state.DeactivationBlockedReason
	out.CanReturnToDefault = state.CanReturnToDefault
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
		v := groupResponse(g)
		v.PoolRevisionId = bindings[g.ID].ConfigID
		out.Groups = append(out.Groups, v)
	}
	for _, d := range state.Desired {
		out.Desired = append(out.Desired, &pb.BoardIntent{HashboardId: d.HashboardID, MacAddress: d.MacAddress, PoolRevisionId: d.PoolRevisionID})
	}
	return connect.NewResponse(out), nil
}

func groupResponse(g sdk.MiningGroup) *pb.Group {
	out := &pb.Group{Id: g.ID, Name: g.Name, Revision: g.Revision, MacAddresses: g.MACs, Mining: g.Mining}
	for _, p := range g.Pools {
		out.Pools = append(out.Pools, &pb.Pool{Id: p.ID, Name: p.Name, Url: p.URL, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight})
	}
	return out
}

func (h *Handler) ReviewReturnToDefault(ctx context.Context, r *connect.Request[pb.ReviewReturnToDefaultRequest]) (*connect.Response[pb.ReviewReturnToDefaultResponse], error) {
	reviews, err := h.service.ReviewReturnToDefault(ctx, r.Msg.DeviceIdentifier, r.Msg.HashboardIds)
	if err != nil {
		return nil, err
	}
	out := &pb.ReviewReturnToDefaultResponse{}
	for _, review := range reviews {
		item := &pb.DefaultReturnReview{DeviceIdentifier: review.Identifier, DefaultGroup: groupResponse(review.Default), ReviewToken: review.Token, BlockedReason: review.BlockedReason}
		for _, board := range review.Boards {
			item.Boards = append(item.Boards, &pb.DefaultReturnBoard{HashboardId: board.HashboardID, MacAddress: board.MacAddress, Name: review.BoardNames[board.HashboardID]})
		}
		out.Containers = append(out.Containers, item)
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) ReturnToDefault(ctx context.Context, r *connect.Request[pb.ReturnToDefaultRequest]) (*connect.Response[pb.ReturnToDefaultResponse], error) {
	if !r.Msg.AcknowledgeMiningChange {
		return nil, fleeterror.NewInvalidArgumentError("Confirm Default's pool and mining settings before moving boards.")
	}
	id, err := h.service.ReturnToDefault(ctx, r.Msg.DeviceIdentifier, r.Msg.ReviewToken, r.Msg.RequestId, r.Msg.HashboardIds)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReturnToDefaultResponse{OperationId: id.String()}), nil
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

func (h *Handler) Deactivate(ctx context.Context, r *connect.Request[pb.DeactivateRequest]) (*connect.Response[pb.DeactivateResponse], error) {
	if !r.Msg.AcknowledgeInterruption {
		return nil, fleeterror.NewInvalidArgumentError("confirm the mining-mode change before disabling mining groups")
	}
	id, err := h.service.Deactivate(ctx, r.Msg.DeviceIdentifier, r.Msg.RequestId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeactivateResponse{OperationId: id.String()}), nil
}
