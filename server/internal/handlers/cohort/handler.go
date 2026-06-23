// Package cohort wires the cohort RPC surface.
package cohort

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/generated/grpc/cohort/v1/cohortv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/cohort"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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

func (h *Handler) ListCohorts(ctx context.Context, req *connect.Request[pb.ListCohortsRequest]) (*connect.Response[pb.ListCohortsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohorts, err := h.service.ListCohorts(ctx, toListCohortsParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListCohortsResponse{Cohorts: toProtoCohortSummaries(cohorts)}), nil
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

func (h *Handler) UpdateCohort(ctx context.Context, _ *connect.Request[pb.UpdateCohortRequest]) (*connect.Response[pb.UpdateCohortResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("UpdateCohort")
}

func (h *Handler) AddDevicesToCohort(ctx context.Context, _ *connect.Request[pb.AddDevicesToCohortRequest]) (*connect.Response[pb.AddDevicesToCohortResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("AddDevicesToCohort")
}

func (h *Handler) RemoveDevicesFromCohort(ctx context.Context, _ *connect.Request[pb.RemoveDevicesFromCohortRequest]) (*connect.Response[pb.RemoveDevicesFromCohortResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("RemoveDevicesFromCohort")
}

func (h *Handler) ReleaseCohort(ctx context.Context, _ *connect.Request[pb.ReleaseCohortRequest]) (*connect.Response[pb.ReleaseCohortResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("ReleaseCohort")
}

func (h *Handler) GetMyCohorts(ctx context.Context, req *connect.Request[pb.GetMyCohortsRequest]) (*connect.Response[pb.GetMyCohortsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	cohorts, err := h.service.ListCohortsByOwner(ctx, toListCohortsByOwnerParams(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMyCohortsResponse{Cohorts: toProtoCohortSummaries(cohorts)}), nil
}

func (h *Handler) ListDevices(ctx context.Context, _ *connect.Request[pb.ListDevicesRequest]) (*connect.Response[pb.ListDevicesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("ListDevices")
}

func (h *Handler) AdminReassign(ctx context.Context, _ *connect.Request[pb.AdminReassignRequest]) (*connect.Response[pb.AdminReassignResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("AdminReassign")
}

func (h *Handler) AdminReleaseCohort(ctx context.Context, _ *connect.Request[pb.AdminReleaseCohortRequest]) (*connect.Response[pb.AdminReleaseCohortResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCohortManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCohortNotImplemented("AdminReleaseCohort")
}

func errCohortNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("cohort.%s is not implemented yet", rpc)
}
