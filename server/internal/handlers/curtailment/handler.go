// Package curtailment wires the curtailment RPC surface.
package curtailment

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Action verb for requireAdminFromContext error messages on the legacy
// admin-only override checks that run after the curtailment:manage gate.
const actionSupplyOverrideFields = "supply curtailment override fields"

// Handler implements the curtailment RPC surface; service=nil keeps
// RPC bodies at Unimplemented after any entry auth gates run.
type Handler struct {
	service *curtailment.Service
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

func NewHandler(service *curtailment.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) PreviewCurtailmentPlan(ctx context.Context, req *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if req.Msg.CandidateMinPowerWOverride != nil {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
	}

	previewReq, err := toPreviewRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Preview(ctx, previewReq)
	if err != nil {
		return nil, err
	}

	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}

	return connect.NewResponse(toPreviewResponse(plan, req.Msg)), nil
}

func (h *Handler) StartCurtailment(ctx context.Context, req *connect.Request[pb.StartCurtailmentRequest]) (*connect.Response[pb.StartCurtailmentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.AllowUnbounded || req.Msg.ForceIncludeMaintenance {
		// force_include_maintenance is safety-critical (curtails miners
		// under physical maintenance), so the same admin gate applies.
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StartCurtailment")
	}

	startReq, err := toStartRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Start(ctx, startReq)
	if err != nil {
		return nil, err
	}

	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}
	if plan.ReplayEvent != nil {
		return connect.NewResponse(&pb.StartCurtailmentResponse{
			Event: toEventProtoWithTargets(plan.ReplayEvent, plan.ReplayTargets),
		}), nil
	}

	return connect.NewResponse(toStartResponse(plan, req.Msg)), nil
}

func (h *Handler) UpdateCurtailmentEvent(ctx context.Context, req *connect.Request[pb.UpdateCurtailmentEventRequest]) (*connect.Response[pb.UpdateCurtailmentEventResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("UpdateCurtailmentEvent")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	updateReq, err := toUpdateRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	updateReq.CanUseAdminControls = canUseAdminControls(info)
	event, err := h.service.Update(ctx, updateReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCurtailmentEventResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

func (h *Handler) StopCurtailment(ctx context.Context, req *connect.Request[pb.StopCurtailmentRequest]) (*connect.Response[pb.StopCurtailmentResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if req.Msg.GetForce() {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StopCurtailment")
	}

	stopReq, err := toStopRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	event, err := h.service.Stop(ctx, stopReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.StopCurtailmentResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

func (h *Handler) GetActiveCurtailment(ctx context.Context, _ *connect.Request[pb.GetActiveCurtailmentRequest]) (*connect.Response[pb.GetActiveCurtailmentResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("GetActiveCurtailment")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	event, targets, err := h.service.GetActiveWithTargets(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.GetActiveCurtailmentResponse{}
	if event != nil {
		resp.Event = toEventProtoWithTargets(event, targets)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListActiveCurtailments(ctx context.Context, _ *connect.Request[pb.ListActiveCurtailmentsRequest]) (*connect.Response[pb.ListActiveCurtailmentsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListActiveCurtailments")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	events, err := h.service.ListActive(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListActiveCurtailmentsResponse(events)), nil
}

func (h *Handler) ListCurtailmentEvents(ctx context.Context, req *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	listReq, err := toListEventsRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	events, nextToken, err := h.service.ListEvents(ctx, listReq)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListEventsResponse(events, nextToken)), nil
}

func (h *Handler) GetCurtailmentEvent(ctx context.Context, req *connect.Request[pb.GetCurtailmentEventRequest]) (*connect.Response[pb.GetCurtailmentEventResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentEvent")
	}
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	eventUUID, err := uuid.Parse(req.Msg.GetEventUuid())
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("event_uuid must be a valid UUID: %v", err)
	}
	event, targets, nextTargetPageToken, err := h.service.GetEventWithTargets(ctx, curtailment.GetEventWithTargetsRequest{
		OrgID:           info.OrganizationID,
		EventUUID:       eventUUID,
		TargetPageSize:  req.Msg.GetTargetPageSize(),
		TargetPageToken: req.Msg.GetTargetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCurtailmentEventResponse{
		Event:               toEventProtoWithTargets(event, targets),
		NextTargetPageToken: nextTargetPageToken,
	}), nil
}

// AdminTerminateEvent forces a non-terminal event to terminal. Paired
// with SessionOnlyProcedures (see interceptors/config.go); the
// curtailment:manage permission gate is the authoritative RBAC check.
func (h *Handler) AdminTerminateEvent(ctx context.Context, req *connect.Request[pb.AdminTerminateEventRequest]) (*connect.Response[pb.AdminTerminateEventResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("AdminTerminateEvent")
	}
	terminateReq, err := toAdminTerminateRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	event, err := h.service.AdminTerminate(ctx, terminateReq)
	if err != nil {
		return nil, err
	}
	targets, err := h.service.ListTargetsByEvent(ctx, info.OrganizationID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminTerminateEventResponse{
		Event: toEventProtoWithTargets(event, targets),
	}), nil
}

// IngestCurtailmentSignal starts a curtailment event from an external
// dispatch signal. Permission gate runs before the body so denial
// surfaces regardless of whether the body has shipped.
func (h *Handler) IngestCurtailmentSignal(ctx context.Context, _ *connect.Request[pb.IngestCurtailmentSignalRequest]) (*connect.Response[pb.IngestCurtailmentSignalResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentIngest, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	return nil, errCurtailmentNotImplemented("IngestCurtailmentSignal")
}

func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}

// requireAdminFromContext returns Forbidden unless the caller has Admin
// or SuperAdmin role.
func requireAdminFromContext(ctx context.Context, action string) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewUnauthenticatedError("authentication required")
	}
	if !canUseAdminControls(info) {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}

func canUseAdminControls(info *session.Info) bool {
	return info != nil &&
		(info.Role == domainAuth.SuperAdminRoleName || info.Role == domainAuth.AdminRoleName)
}
