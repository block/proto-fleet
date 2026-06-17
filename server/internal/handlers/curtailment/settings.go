package curtailment

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func (h *Handler) GetCurtailmentSettings(ctx context.Context, _ *connect.Request[pb.GetCurtailmentSettingsRequest]) (*connect.Response[pb.GetCurtailmentSettingsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentSettings")
	}
	settings, err := h.service.GetSettings(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out, err := toCurtailmentSettingsProto(settings)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCurtailmentSettingsResponse{Settings: out}), nil
}

func (h *Handler) UpdateCurtailmentSettings(ctx context.Context, req *connect.Request[pb.UpdateCurtailmentSettingsRequest]) (*connect.Response[pb.UpdateCurtailmentSettingsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("UpdateCurtailmentSettings")
	}
	if req.Msg.PostEventCooldownSec == nil {
		return nil, fleeterror.NewInvalidArgumentError("post_event_cooldown_sec must be set")
	}
	postEventCooldownSec, err := uint32ToInt32Strict("post_event_cooldown_sec", req.Msg.GetPostEventCooldownSec())
	if err != nil {
		return nil, err
	}
	settings, err := h.service.UpdateSettings(ctx, domainCurtailment.UpdateSettingsRequest{
		OrgID:                info.OrganizationID,
		PostEventCooldownSec: postEventCooldownSec,
	})
	if err != nil {
		return nil, err
	}
	out, err := toCurtailmentSettingsProto(settings)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCurtailmentSettingsResponse{Settings: out}), nil
}

func toCurtailmentSettingsProto(settings *models.OrgConfig) (*pb.CurtailmentSettings, error) {
	if settings == nil {
		return nil, fleeterror.NewInternalError("curtailment settings are missing")
	}
	if settings.PostEventCooldownSec < 0 {
		return nil, fleeterror.NewInternalErrorf(
			"curtailment org config post_event_cooldown_sec is negative: %d",
			settings.PostEventCooldownSec,
		)
	}
	return &pb.CurtailmentSettings{
		PostEventCooldownSec: uint32(settings.PostEventCooldownSec),
	}, nil
}
