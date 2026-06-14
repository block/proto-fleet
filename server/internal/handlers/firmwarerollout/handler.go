package firmwarerollout

import (
	"context"
	"database/sql"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/firmwarerollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/firmwarerollout/v1/firmwarerolloutv1connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/firmwarerollout"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	service *domain.Service
}

var _ firmwarerolloutv1connect.FirmwareRolloutServiceHandler = &Handler{}

func NewHandler(service *domain.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateFirmwareRollout(ctx context.Context, req *connect.Request[pb.CreateFirmwareRolloutRequest]) (*connect.Response[pb.CreateFirmwareRolloutResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Create(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) StartFirmwareRollout(ctx context.Context, req *connect.Request[pb.StartFirmwareRolloutRequest]) (*connect.Response[pb.StartFirmwareRolloutResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Start(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.StartFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) PauseFirmwareRollout(ctx context.Context, req *connect.Request[pb.PauseFirmwareRolloutRequest]) (*connect.Response[pb.PauseFirmwareRolloutResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Pause(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PauseFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) ResumeFirmwareRollout(ctx context.Context, req *connect.Request[pb.ResumeFirmwareRolloutRequest]) (*connect.Response[pb.ResumeFirmwareRolloutResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Resume(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ResumeFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) CancelFirmwareRollout(ctx context.Context, req *connect.Request[pb.CancelFirmwareRolloutRequest]) (*connect.Response[pb.CancelFirmwareRolloutResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Cancel(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CancelFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) RetryFailedFirmwareRolloutTargets(ctx context.Context, req *connect.Request[pb.RetryFailedFirmwareRolloutTargetsRequest]) (*connect.Response[pb.RetryFailedFirmwareRolloutTargetsResponse], error) {
	if err := requireManage(ctx); err != nil {
		return nil, err
	}
	detail, retried, err := h.service.RetryFailed(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RetryFailedFirmwareRolloutTargetsResponse{Rollout: rolloutProto(detail), RetriedCount: retried}), nil
}

func (h *Handler) ListFirmwareRollouts(ctx context.Context, req *connect.Request[pb.ListFirmwareRolloutsRequest]) (*connect.Response[pb.ListFirmwareRolloutsResponse], error) {
	if err := requireRead(ctx); err != nil {
		return nil, err
	}
	page, err := h.service.List(ctx, req.Msg.GetPageSize(), req.Msg.GetPageToken())
	if err != nil {
		return nil, err
	}
	rollouts := make([]*pb.FirmwareRollout, 0, len(page.Rollouts))
	for i := range page.Rollouts {
		rollouts = append(rollouts, rolloutProto(&page.Rollouts[i]))
	}
	return connect.NewResponse(&pb.ListFirmwareRolloutsResponse{Rollouts: rollouts, NextPageToken: page.NextPageToken}), nil
}

func (h *Handler) GetFirmwareRollout(ctx context.Context, req *connect.Request[pb.GetFirmwareRolloutRequest]) (*connect.Response[pb.GetFirmwareRolloutResponse], error) {
	if err := requireRead(ctx); err != nil {
		return nil, err
	}
	detail, err := h.service.Get(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetFirmwareRolloutResponse{Rollout: rolloutProto(detail)}), nil
}

func (h *Handler) ListFirmwareRolloutTargets(ctx context.Context, req *connect.Request[pb.ListFirmwareRolloutTargetsRequest]) (*connect.Response[pb.ListFirmwareRolloutTargetsResponse], error) {
	if err := requireRead(ctx); err != nil {
		return nil, err
	}
	page, err := h.service.ListTargets(ctx, req.Msg.GetRolloutId(), req.Msg.GetPageSize(), req.Msg.GetPageToken(), targetStateString(req.Msg.GetStateFilter()))
	if err != nil {
		return nil, err
	}
	targets := make([]*pb.FirmwareRolloutTarget, 0, len(page.Targets))
	for _, target := range page.Targets {
		targets = append(targets, targetProto(target))
	}
	return connect.NewResponse(&pb.ListFirmwareRolloutTargetsResponse{Targets: targets, NextPageToken: page.NextPageToken}), nil
}

func (h *Handler) ListFirmwareRolloutEvents(ctx context.Context, req *connect.Request[pb.ListFirmwareRolloutEventsRequest]) (*connect.Response[pb.ListFirmwareRolloutEventsResponse], error) {
	if err := requireRead(ctx); err != nil {
		return nil, err
	}
	events, err := h.service.ListEvents(ctx, req.Msg.GetRolloutId())
	if err != nil {
		return nil, err
	}
	out := make([]*pb.FirmwareRolloutEvent, 0, len(events))
	for _, event := range events {
		out = append(out, eventProto(event))
	}
	return connect.NewResponse(&pb.ListFirmwareRolloutEventsResponse{Events: out}), nil
}

func requireRead(ctx context.Context) error {
	_, err := middleware.RequirePermission(ctx, authz.PermFirmwareRolloutRead, authz.ResourceContext{})
	return err
}

func requireManage(ctx context.Context) error {
	if _, err := middleware.RequirePermission(ctx, authz.PermFirmwareRolloutManage, authz.ResourceContext{}); err != nil {
		return err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerFirmwareUpdate, authz.ResourceContext{}); err != nil {
		return err
	}
	_, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, authz.ResourceContext{})
	return err
}

func rolloutProto(detail *domain.RolloutDetail) *pb.FirmwareRollout {
	if detail == nil {
		return nil
	}
	row := detail.Rollout
	return &pb.FirmwareRollout{
		RolloutId:            row.RolloutUuid.String(),
		Name:                 row.Name,
		FirmwareFileId:       row.FirmwareFileID,
		State:                rolloutStateProto(row.State),
		TargetCount:          row.TargetCount,
		BatchSize:            row.BatchSize,
		BatchIntervalSeconds: row.BatchIntervalSec,
		ScopeType:            row.ScopeType,
		CreatedAt:            timestamppb.New(row.CreatedAt),
		StartedAt:            timestampFromNull(row.StartedAt),
		EndedAt:              timestampFromNull(row.EndedAt),
		Counts: &pb.FirmwareRolloutCounts{
			TotalCount:      detail.Counts.TotalCount,
			PendingCount:    detail.Counts.PendingCount,
			InProgressCount: detail.Counts.InProgressCount,
			SuccessCount:    detail.Counts.SuccessCount,
			FailureCount:    detail.Counts.FailureCount,
			CanceledCount:   detail.Counts.CanceledCount,
			RetriedCount:    detail.Counts.RetriedCount,
		},
	}
}

func targetProto(row sqlc.ListFirmwareRolloutTargetsRow) *pb.FirmwareRolloutTarget {
	return &pb.FirmwareRolloutTarget{
		DeviceIdentifier:     row.DeviceIdentifier,
		DeviceName:           row.DeviceName,
		IpAddress:            nullString(row.IpAddress),
		MacAddress:           nullString(row.MacAddress),
		State:                targetStateProto(row.State),
		CurrentAttemptNumber: row.CurrentAttemptNumber,
		LastError:            nullString(row.LastError),
		UpdatedAt:            timestamppb.New(row.UpdatedAt),
		LastCommandBatchId:   nullString(row.LastCommandBatchUuid),
	}
}

func eventProto(row sqlc.FirmwareRolloutEvent) *pb.FirmwareRolloutEvent {
	return &pb.FirmwareRolloutEvent{
		EventType: row.EventType,
		ActorType: row.ActorType,
		UserId:    nullString(row.UserID),
		Username:  nullString(row.Username),
		Message:   row.Message,
		CreatedAt: timestamppb.New(row.CreatedAt),
	}
}

func rolloutStateProto(state string) pb.FirmwareRolloutState {
	switch state {
	case domain.StateDraft:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_DRAFT
	case domain.StateRunning:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_RUNNING
	case domain.StatePaused:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_PAUSED
	case domain.StateCompleted:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_COMPLETED
	case domain.StateCompletedWithFailures:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_COMPLETED_WITH_FAILURES
	case domain.StateCanceled:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_CANCELED
	default:
		return pb.FirmwareRolloutState_FIRMWARE_ROLLOUT_STATE_UNSPECIFIED
	}
}

func targetStateProto(state string) pb.FirmwareRolloutTargetState {
	switch state {
	case "pending":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_PENDING
	case "dispatching":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_DISPATCHING
	case "dispatched":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_DISPATCHED
	case "succeeded":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_SUCCEEDED
	case "failed":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_FAILED
	case "canceled":
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_CANCELED
	default:
		return pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_UNSPECIFIED
	}
}

func targetStateString(state pb.FirmwareRolloutTargetState) string {
	switch state {
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_UNSPECIFIED:
		return ""
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_PENDING:
		return "pending"
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_DISPATCHING:
		return "dispatching"
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_DISPATCHED:
		return "dispatched"
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_SUCCEEDED:
		return "succeeded"
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_FAILED:
		return "failed"
	case pb.FirmwareRolloutTargetState_FIRMWARE_ROLLOUT_TARGET_STATE_CANCELED:
		return "canceled"
	default:
		return ""
	}
}

func timestampFromNull(value sql.NullTime) *timestamppb.Timestamp {
	if !value.Valid {
		return nil
	}
	return timestamppb.New(value.Time)
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
