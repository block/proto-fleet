package schedule

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	scheduleDomain "github.com/block/proto-fleet/server/internal/domain/schedule"
)

type Handler struct {
	svc *scheduleDomain.Service
}

var _ schedulev1connect.ScheduleServiceHandler = &Handler{}

func NewHandler(svc *scheduleDomain.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListSchedules(ctx context.Context, r *connect.Request[pb.ListSchedulesRequest]) (*connect.Response[pb.ListSchedulesResponse], error) {
	status := scheduleStatusFilterToString(r.Msg.Status)
	action := scheduleActionFilterToString(r.Msg.Action)

	schedules, err := h.svc.ListSchedules(ctx, status, action)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListSchedulesResponse{Schedules: schedules}), nil
}

func (h *Handler) CreateSchedule(ctx context.Context, r *connect.Request[pb.CreateScheduleRequest]) (*connect.Response[pb.CreateScheduleResponse], error) {
	schedule, err := h.svc.CreateSchedule(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) UpdateSchedule(ctx context.Context, r *connect.Request[pb.UpdateScheduleRequest]) (*connect.Response[pb.UpdateScheduleResponse], error) {
	schedule, err := h.svc.UpdateSchedule(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) DeleteSchedule(ctx context.Context, r *connect.Request[pb.DeleteScheduleRequest]) (*connect.Response[pb.DeleteScheduleResponse], error) {
	if err := h.svc.DeleteSchedule(ctx, r.Msg.ScheduleId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteScheduleResponse{}), nil
}

func (h *Handler) PauseSchedule(ctx context.Context, r *connect.Request[pb.PauseScheduleRequest]) (*connect.Response[pb.PauseScheduleResponse], error) {
	schedule, err := h.svc.PauseSchedule(ctx, r.Msg.ScheduleId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PauseScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) ResumeSchedule(ctx context.Context, r *connect.Request[pb.ResumeScheduleRequest]) (*connect.Response[pb.ResumeScheduleResponse], error) {
	schedule, err := h.svc.ResumeSchedule(ctx, r.Msg.ScheduleId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResumeScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) ReorderSchedules(ctx context.Context, r *connect.Request[pb.ReorderSchedulesRequest]) (*connect.Response[pb.ReorderSchedulesResponse], error) {
	if err := h.svc.ReorderSchedules(ctx, r.Msg.ScheduleIds); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReorderSchedulesResponse{}), nil
}

func scheduleStatusFilterToString(s pb.ScheduleStatus) string {
	switch s {
	case pb.ScheduleStatus_SCHEDULE_STATUS_UNSPECIFIED:
		return ""
	case pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE:
		return "active"
	case pb.ScheduleStatus_SCHEDULE_STATUS_PAUSED:
		return "paused"
	case pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING:
		return "running"
	case pb.ScheduleStatus_SCHEDULE_STATUS_COMPLETED:
		return "completed"
	default:
		return ""
	}
}

func scheduleActionFilterToString(a pb.ScheduleAction) string {
	switch a {
	case pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return ""
	case pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		return "set_power_target"
	case pb.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return "reboot"
	case pb.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return "sleep"
	default:
		return ""
	}
}
