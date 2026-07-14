// Package cohort wires the cohort RPC surface.
package cohort

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/generated/grpc/cohort/v1/cohortv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/cohort"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the CohortService Connect-RPC surface.
type Handler struct {
	service *cohort.Service
}

var _ cohortv1connect.CohortServiceHandler = &Handler{}

func NewHandler(service *cohort.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateCohort(ctx context.Context, req *connect.Request[pb.CreateCohortRequest]) (*connect.Response[pb.CreateCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	params, err := toCreateCohortParams(req.Msg, info)
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.CreateCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) GetCohort(ctx context.Context, req *connect.Request[pb.GetCohortRequest]) (*connect.Response[pb.GetCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.GetCohort(ctx, info.OrganizationID, req.Msg.GetCohortId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) GetCohortFirmwareVersionHistory(ctx context.Context, req *connect.Request[pb.GetCohortFirmwareVersionHistoryRequest]) (*connect.Response[pb.GetCohortFirmwareVersionHistoryResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	history, err := h.service.GetCohortFirmwareVersionHistory(ctx, toCohortFirmwareVersionHistoryParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toProtoCohortFirmwareVersionHistory(history)), nil
}

func (h *Handler) ListCohorts(ctx context.Context, req *connect.Request[pb.ListCohortsRequest]) (*connect.Response[pb.ListCohortsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListCohorts(ctx, toListCohortsParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListCohortsResponse{
		Cohorts:       toProtoCohortSummaries(result.Cohorts),
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}), nil
}

func (h *Handler) DeleteCohort(ctx context.Context, req *connect.Request[pb.DeleteCohortRequest]) (*connect.Response[pb.DeleteCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.DeleteCohort(ctx, info.OrganizationID, req.Msg.GetCohortId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) UpdateCohort(ctx context.Context, req *connect.Request[pb.UpdateCohortRequest]) (*connect.Response[pb.UpdateCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	params, err := toUpdateCohortParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.UpdateCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) SetCohortFirmwareTarget(ctx context.Context, req *connect.Request[pb.SetCohortFirmwareTargetRequest]) (*connect.Response[pb.SetCohortFirmwareTargetResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.SetCohortFirmwareTarget(ctx, toSetCohortFirmwareTargetParams(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetCohortFirmwareTargetResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) AddDevicesToCohort(ctx context.Context, req *connect.Request[pb.AddDevicesToCohortRequest]) (*connect.Response[pb.AddDevicesToCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.AddDevicesToCohort(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetCohortId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AddDevicesToCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) RemoveDevicesFromCohort(ctx context.Context, req *connect.Request[pb.RemoveDevicesFromCohortRequest]) (*connect.Response[pb.RemoveDevicesFromCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.RemoveDevicesFromCohort(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetCohortId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RemoveDevicesFromCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) ReleaseCohort(ctx context.Context, req *connect.Request[pb.ReleaseCohortRequest]) (*connect.Response[pb.ReleaseCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohort, err := h.service.ReleaseCohort(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetCohortId(), nil))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReleaseCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) GetMyCohorts(ctx context.Context, req *connect.Request[pb.GetMyCohortsRequest]) (*connect.Response[pb.GetMyCohortsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListCohortsByOwner(ctx, toListCohortsByOwnerParams(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMyCohortsResponse{
		Cohorts:       toProtoCohortSummaries(result.Cohorts),
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}), nil
}

func (h *Handler) ListDevices(ctx context.Context, req *connect.Request[pb.ListDevicesRequest]) (*connect.Response[pb.ListDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListDevices(ctx, toListDevicesParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListDevicesResponse{
		Devices:        toProtoCohortDevices(result.Devices),
		NextPageToken:  result.NextPageToken,
		TotalCount:     result.TotalCount,
		AvailableCount: result.AvailableCount,
		ReservedCount:  result.ReservedCount,
	}), nil
}

func (h *Handler) AdminReassign(ctx context.Context, req *connect.Request[pb.AdminReassignRequest]) (*connect.Response[pb.AdminReassignResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireSuperAdmin(ctx, "reassign cohort"); err != nil {
		return nil, err
	}
	cohort, err := h.service.AddDevicesToCohort(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetTargetCohortId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminReassignResponse{Cohort: toProtoCohort(cohort)}), nil
}

func (h *Handler) AdminReleaseCohort(ctx context.Context, req *connect.Request[pb.AdminReleaseCohortRequest]) (*connect.Response[pb.AdminReleaseCohortResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireSuperAdmin(ctx, "release any cohort"); err != nil {
		return nil, err
	}
	cohort, err := h.service.ReleaseCohort(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetCohortId(), nil))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminReleaseCohortResponse{Cohort: toProtoCohort(cohort)}), nil
}
