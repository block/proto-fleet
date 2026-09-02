// Package rollout exposes the RolloutService Connect API for managing
// firmware rollout lanes.
package rollout

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc *rollout.Service
}

var _ rolloutv1connect.RolloutServiceHandler = &Handler{}

func NewHandler(svc *rollout.Service) *Handler {
	return &Handler{svc: svc}
}

// authorize gates every rollout RPC on the firmware-update permission, since
// lane management exists solely to drive firmware updates.
func authorize(ctx context.Context) (*session.Info, error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerFirmwareUpdate, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return session.GetInfo(ctx)
}

func (h *Handler) ListRolloutLanes(ctx context.Context, r *connect.Request[pb.ListRolloutLanesRequest]) (*connect.Response[pb.ListRolloutLanesResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	lanes, err := h.svc.ListLanes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListRolloutLanesResponse{}
	for i := range lanes {
		resp.Lanes = append(resp.Lanes, laneToProto(&lanes[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) CreateRolloutLane(ctx context.Context, r *connect.Request[pb.CreateRolloutLaneRequest]) (*connect.Response[pb.CreateRolloutLaneResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	lane, err := h.svc.CreateLane(ctx, info.OrganizationID, r.Msg.Name)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateRolloutLaneResponse{Lane: laneToProto(lane)}), nil
}

func (h *Handler) DeleteRolloutLane(ctx context.Context, r *connect.Request[pb.DeleteRolloutLaneRequest]) (*connect.Response[pb.DeleteRolloutLaneResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteLane(ctx, info.OrganizationID, r.Msg.LaneId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteRolloutLaneResponse{}), nil
}

func (h *Handler) UpdateRolloutLaneMembers(ctx context.Context, r *connect.Request[pb.UpdateRolloutLaneMembersRequest]) (*connect.Response[pb.UpdateRolloutLaneMembersResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	lane, err := h.svc.UpdateMembers(ctx, info.OrganizationID, r.Msg.LaneId, r.Msg.AddDeviceIdentifiers, r.Msg.RemoveDeviceIdentifiers)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateRolloutLaneMembersResponse{Lane: laneToProto(lane)}), nil
}

func (h *Handler) ApplyRolloutLaneFirmware(ctx context.Context, r *connect.Request[pb.ApplyRolloutLaneFirmwareRequest]) (*connect.Response[pb.ApplyRolloutLaneFirmwareResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	assignments := make([]rollout.Assignment, 0, len(r.Msg.Assignments))
	for _, a := range r.Msg.Assignments {
		assignments = append(assignments, rollout.Assignment{Model: a.Model, FirmwareFileID: a.FirmwareFileId})
	}
	opts := rollout.RolloutOptions{}
	if o := r.Msg.Options; o != nil {
		opts.Method = methodFromProto(o.Method)
		opts.BatchSize = o.BatchSize
		opts.AutoAdvance = o.AutoAdvance
		opts.MaxHashrateDropPercent = o.MaxHashrateDropPercent
		opts.StabilizationSeconds = o.StabilizationSeconds
	}
	started, err := h.svc.ApplyFirmware(ctx, info.OrganizationID, info.UserID, r.Msg.LaneId, assignments, opts)
	if err != nil {
		return nil, err
	}
	lane, err := h.svc.ListLanes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ApplyRolloutLaneFirmwareResponse{}
	for i := range lane {
		if lane[i].ID == r.Msg.LaneId {
			resp.Lane = laneToProto(&lane[i])
		}
	}
	for i := range started {
		resp.StartedRollouts = append(resp.StartedRollouts, rolloutToProto(&started[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RollbackRolloutLaneFirmware(ctx context.Context, r *connect.Request[pb.RollbackRolloutLaneFirmwareRequest]) (*connect.Response[pb.RollbackRolloutLaneFirmwareResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	laneID, started, err := h.svc.RollbackFirmware(ctx, info.OrganizationID, info.UserID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	lanes, err := h.svc.ListLanes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.RollbackRolloutLaneFirmwareResponse{}
	for i := range lanes {
		if lanes[i].ID == laneID {
			resp.Lane = laneToProto(&lanes[i])
		}
	}
	for i := range started {
		resp.StartedRollouts = append(resp.StartedRollouts, rolloutToProto(&started[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ContinueRollout(ctx context.Context, r *connect.Request[pb.ContinueRolloutRequest]) (*connect.Response[pb.ContinueRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.ContinueRollout(ctx, info.OrganizationID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ContinueRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) PauseRollout(ctx context.Context, r *connect.Request[pb.PauseRolloutRequest]) (*connect.Response[pb.PauseRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.PauseRollout(ctx, info.OrganizationID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PauseRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) ResumeRollout(ctx context.Context, r *connect.Request[pb.ResumeRolloutRequest]) (*connect.Response[pb.ResumeRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.ResumeRollout(ctx, info.OrganizationID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResumeRolloutResponse{Rollout: rolloutToProto(view)}), nil
}

func (h *Handler) AbortRollout(ctx context.Context, r *connect.Request[pb.AbortRolloutRequest]) (*connect.Response[pb.AbortRolloutResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.AbortRollout(ctx, info.OrganizationID, info.UserID, r.Msg.RolloutId)
	if err != nil {
		return nil, err
	}
	resp := &pb.AbortRolloutResponse{
		Rollout:          rolloutToProto(result.Rollout),
		Lane:             laneToProto(result.Lane),
		RestoredPrevious: result.RestoredPrevious,
	}
	for i := range result.Started {
		resp.StartedRollouts = append(resp.StartedRollouts, rolloutToProto(&result.Started[i]))
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListRollouts(ctx context.Context, r *connect.Request[pb.ListRolloutsRequest]) (*connect.Response[pb.ListRolloutsResponse], error) {
	info, err := authorize(ctx)
	if err != nil {
		return nil, err
	}
	rollouts, err := h.svc.ListRollouts(ctx, info.OrganizationID, r.Msg.LaneId)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListRolloutsResponse{}
	for i := range rollouts {
		resp.Rollouts = append(resp.Rollouts, rolloutToProto(&rollouts[i]))
	}
	return connect.NewResponse(resp), nil
}

func laneToProto(lane *rollout.Lane) *pb.RolloutLane {
	out := &pb.RolloutLane{
		Id:        lane.ID,
		Name:      lane.Name,
		CreatedAt: timestamppb.New(lane.CreatedAt),
	}
	for _, g := range lane.ModelGroups {
		group := &pb.RolloutLaneModelGroup{
			Model:           g.Model,
			FirmwareFileId:  g.FirmwareFileID,
			FirmwareVersion: g.FirmwareVersion,
			ActiveRolloutId: g.ActiveRolloutID,
		}
		for _, m := range g.Miners {
			group.Miners = append(group.Miners, &pb.RolloutLaneMiner{
				DeviceId:         m.DeviceID,
				DeviceIdentifier: m.DeviceIdentifier,
				Model:            m.Model,
				FirmwareVersion:  m.FirmwareVersion,
			})
		}
		out.ModelGroups = append(out.ModelGroups, group)
	}
	return out
}

func rolloutToProto(r *rollout.Rollout) *pb.Rollout {
	out := &pb.Rollout{
		Id:                      r.ID,
		LaneId:                  r.LaneID,
		LaneName:                r.LaneName,
		Model:                   r.Model,
		FirmwareFileId:          r.FirmwareFileID,
		FirmwareVersion:         r.FirmwareVersion,
		Status:                  statusToProto(r.Status),
		Method:                  methodToProto(r.Method),
		Stage:                   stageToProto(r.Stage),
		BatchSize:               r.BatchSize,
		BatchCount:              r.BatchCount,
		CurrentBatch:            r.CurrentBatch,
		AutoAdvance:             r.AutoAdvance,
		MaxHashrateDropPercent:  r.MaxHashrateDropPercent,
		StabilizationSeconds:    r.StabilizationSeconds,
		PreviousFirmwareFileId:  r.PreviousFirmwareFileID,
		PreviousFirmwareVersion: r.PreviousFirmwareVersion,
		CancelReason:            cancelReasonToProto(r.CancelReason),
		StageChangedAt:          timestamppb.New(r.StageChangedAt),
		CreatedAt:               timestamppb.New(r.CreatedAt),
	}
	if r.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*r.FinishedAt)
	}
	if r.PausedAt != nil {
		out.PausedAt = timestamppb.New(*r.PausedAt)
	}
	if ev := r.Evidence; ev != nil {
		out.Evidence = &pb.RolloutEvidence{
			DevicesTotal:                  ev.DevicesTotal,
			Verified:                      ev.Verified,
			Online:                        ev.Online,
			Hashing:                       ev.Hashing,
			BaselineHashing:               ev.BaselineHashing,
			HashrateChangePercent:         ev.HashrateChangePercent,
			HasHashrateEvidence:           ev.HasHashrateEvidence,
			BaselineHashRateHs:            ev.BaselineHashRateHs,
			CurrentHashRateHs:             ev.CurrentHashRateHs,
			NewErrors:                     ev.NewErrors,
			ReadyToAdvance:                ev.ReadyToAdvance,
			HoldReason:                    ev.HoldReason,
			StabilizationRemainingSeconds: ev.StabilizationRemainingSeconds,
			PowerW:                        metricToProto(ev.PowerW),
			EfficiencyJh:                  metricToProto(ev.EfficiencyJh),
			TempC:                         metricToProto(ev.TempC),
		}
	}
	for _, d := range r.Devices {
		out.Devices = append(out.Devices, &pb.RolloutDevice{
			DeviceId:           d.DeviceID,
			DeviceIdentifier:   d.DeviceIdentifier,
			IpAddress:          d.IPAddress,
			FirmwareVersion:    d.FirmwareVersion,
			State:              deviceStateToProto(d.State),
			Batch:              d.Batch,
			Status:             d.Status,
			Online:             d.Online,
			Hashing:            d.Hashing,
			HasBaseline:        d.HasBaseline,
			BaselineHashing:    d.BaselineHashing,
			OpenErrors:         d.OpenErrors,
			BaselineOpenErrors: d.BaselineOpenErrors,
			HashRateHs:         metricToProto(d.HashRateHs),
			PowerW:             metricToProto(d.PowerW),
			EfficiencyJh:       metricToProto(d.EfficiencyJh),
			TempC:              metricToProto(d.TempC),
		})
	}
	return out
}

func metricToProto(m rollout.Metric) *pb.MetricComparison {
	return &pb.MetricComparison{Baseline: m.Baseline, Current: m.Current}
}

func cancelReasonToProto(reason string) pb.RolloutCancelReason {
	switch reason {
	case rollout.CancelReasonSuperseded:
		return pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_SUPERSEDED
	case rollout.CancelReasonAborted:
		return pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_ABORTED
	case rollout.CancelReasonCleared:
		return pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_CLEARED
	}
	return pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_UNSPECIFIED
}

func methodFromProto(method pb.RolloutMethod) string {
	switch method {
	case pb.RolloutMethod_ROLLOUT_METHOD_PILOT:
		return rollout.MethodPilot
	case pb.RolloutMethod_ROLLOUT_METHOD_BATCHES:
		return rollout.MethodBatches
	case pb.RolloutMethod_ROLLOUT_METHOD_IMMEDIATE:
		return rollout.MethodImmediate
	case pb.RolloutMethod_ROLLOUT_METHOD_UNSPECIFIED:
		return "" // the domain treats this as immediate
	}
	return ""
}

func methodToProto(method string) pb.RolloutMethod {
	switch method {
	case rollout.MethodImmediate:
		return pb.RolloutMethod_ROLLOUT_METHOD_IMMEDIATE
	case rollout.MethodPilot:
		return pb.RolloutMethod_ROLLOUT_METHOD_PILOT
	case rollout.MethodBatches:
		return pb.RolloutMethod_ROLLOUT_METHOD_BATCHES
	}
	return pb.RolloutMethod_ROLLOUT_METHOD_UNSPECIFIED
}

func stageToProto(stage string) pb.RolloutStage {
	switch stage {
	case rollout.StageBatch:
		return pb.RolloutStage_ROLLOUT_STAGE_BATCH
	case rollout.StageAwaitingReview:
		return pb.RolloutStage_ROLLOUT_STAGE_AWAITING_REVIEW
	case rollout.StageRest:
		return pb.RolloutStage_ROLLOUT_STAGE_REST
	}
	return pb.RolloutStage_ROLLOUT_STAGE_UNSPECIFIED
}

func statusToProto(status string) pb.RolloutStatus {
	switch status {
	case rollout.StatusActive:
		return pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE
	case rollout.StatusCompleted:
		return pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED
	case rollout.StatusCanceled:
		return pb.RolloutStatus_ROLLOUT_STATUS_CANCELED
	}
	return pb.RolloutStatus_ROLLOUT_STATUS_UNSPECIFIED
}

func deviceStateToProto(state string) pb.RolloutDeviceState {
	switch state {
	case rollout.DeviceStatePending:
		return pb.RolloutDeviceState_ROLLOUT_DEVICE_STATE_PENDING
	case rollout.DeviceStateUpdating:
		return pb.RolloutDeviceState_ROLLOUT_DEVICE_STATE_UPDATING
	case rollout.DeviceStateUpdated:
		return pb.RolloutDeviceState_ROLLOUT_DEVICE_STATE_UPDATED
	case rollout.DeviceStateVerifying:
		return pb.RolloutDeviceState_ROLLOUT_DEVICE_STATE_VERIFYING
	}
	return pb.RolloutDeviceState_ROLLOUT_DEVICE_STATE_UNSPECIFIED
}
