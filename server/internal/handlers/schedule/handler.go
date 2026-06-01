package schedule

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	scheduleDomain "github.com/block/proto-fleet/server/internal/domain/schedule"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc *scheduleDomain.Service
}

var _ schedulev1connect.ScheduleServiceHandler = &Handler{}

func NewHandler(svc *scheduleDomain.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListSchedules(ctx context.Context, r *connect.Request[pb.ListSchedulesRequest]) (*connect.Response[pb.ListSchedulesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	status := scheduleStatusFilterToString(r.Msg.Status)
	action := scheduleActionFilterToString(r.Msg.Action)

	schedules, err := h.svc.ListSchedules(ctx, status, action)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListSchedulesResponse{Schedules: schedules}), nil
}

func (h *Handler) CreateSchedule(ctx context.Context, r *connect.Request[pb.CreateScheduleRequest]) (*connect.Response[pb.CreateScheduleResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	if err := requireActionAuthority(ctx, r.Msg.Action); err != nil {
		return nil, err
	}
	schedule, err := h.svc.CreateSchedule(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) UpdateSchedule(ctx context.Context, r *connect.Request[pb.UpdateScheduleRequest]) (*connect.Response[pb.UpdateScheduleResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	if err := requireActionAuthority(ctx, r.Msg.Action); err != nil {
		return nil, err
	}
	schedule, err := h.svc.UpdateSchedule(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) DeleteSchedule(ctx context.Context, r *connect.Request[pb.DeleteScheduleRequest]) (*connect.Response[pb.DeleteScheduleResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	if err := h.svc.DeleteSchedule(ctx, r.Msg.ScheduleId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteScheduleResponse{}), nil
}

func (h *Handler) PauseSchedule(ctx context.Context, r *connect.Request[pb.PauseScheduleRequest]) (*connect.Response[pb.PauseScheduleResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	schedule, err := h.svc.PauseSchedule(ctx, r.Msg.ScheduleId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PauseScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) ResumeSchedule(ctx context.Context, r *connect.Request[pb.ResumeScheduleRequest]) (*connect.Response[pb.ResumeScheduleResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	// The action-authority check runs inside the service's transaction
	// so it sees the same row the resume update operates on; a pre-flight
	// handler-side read could race with an Update that swapped the
	// schedule's action between the read and the resume.
	schedule, err := h.svc.ResumeSchedule(ctx, r.Msg.ScheduleId, func(action pb.ScheduleAction) error {
		return requireActionAuthority(ctx, action)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResumeScheduleResponse{Schedule: schedule}), nil
}

func (h *Handler) ReorderSchedules(ctx context.Context, r *connect.Request[pb.ReorderSchedulesRequest]) (*connect.Response[pb.ReorderSchedulesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermScheduleManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	if err := h.svc.ReorderSchedules(ctx, r.Msg.ScheduleIds); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReorderSchedulesResponse{}), nil
}

// requireActionAuthority re-checks the caller's permission for the
// underlying miner action the schedule will eventually dispatch.
// schedule:manage alone is not enough — a manager without
// miner:set_power_target should not be able to schedule a
// SET_POWER_TARGET job that the processor later runs on their behalf.
//
// UNSPECIFIED falls through to the service's field validation, which
// rejects it with InvalidArgument. Any other unrecognized enum value
// fails closed here so a future action added to the proto can't be
// persisted by a schedule:manage holder before this mapping is updated.
func requireActionAuthority(ctx context.Context, action pb.ScheduleAction) error {
	if action == pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED {
		return nil
	}
	key, ok := requiredPermForAction(action)
	if !ok {
		return fleeterror.NewInvalidArgumentErrorf("unsupported schedule action %v", action)
	}
	_, err := middleware.RequirePermission(ctx, key, authz.ResourceContext{})
	return err
}

// requiredPermForAction maps a schedule action to the catalog key the
// caller must hold to schedule it. Returns ok=false for UNSPECIFIED and
// any unrecognized enum value; the caller (requireActionAuthority)
// distinguishes the two and rejects unrecognized actions with
// InvalidArgument so they never reach the service layer.
func requiredPermForAction(action pb.ScheduleAction) (string, bool) {
	switch action {
	case pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		return authz.PermMinerSetPowerTarget, true
	case pb.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return authz.PermMinerReboot, true
	case pb.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return authz.PermMinerStopMining, true
	case pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
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
