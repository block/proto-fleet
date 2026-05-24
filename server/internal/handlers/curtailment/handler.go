// Package curtailment wires the RPC surface. PreviewCurtailmentPlan is
// implemented; the remaining RPCs return Unimplemented and land in follow-up
// work (Start + reconciler, Stop + restore, read APIs + audit).
package curtailment

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// Action verbs for requireAdminFromContext error messages.
const (
	actionSupplyOverrideFields = "supply curtailment override fields"
	actionTerminateEvents      = "terminate curtailment events"
)

// Handler implements the curtailment RPC surface. service=nil keeps every
// RPC at Unimplemented (test stubs); a populated *Service wires the impl.
type Handler struct {
	service *curtailment.Service
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

func NewHandler(service *curtailment.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) PreviewCurtailmentPlan(ctx context.Context, req *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	if req.Msg.CandidateMinPowerWOverride != nil {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}

	previewReq, err := toPreviewRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Preview(ctx, previewReq)
	if err != nil {
		return nil, err
	}

	// Insufficient load is a request-shape failure, not a successful
	// empty plan; surface as InvalidArgument with structured detail.
	if plan.InsufficientLoadDetail != nil {
		return nil, toInsufficientLoadError(plan.InsufficientLoadDetail)
	}

	return connect.NewResponse(toPreviewResponse(plan, req.Msg)), nil
}

func (h *Handler) StartCurtailment(ctx context.Context, req *connect.Request[pb.StartCurtailmentRequest]) (*connect.Response[pb.StartCurtailmentResponse], error) {
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.AllowUnbounded || req.Msg.ForceIncludeMaintenance {
		// force_include_maintenance is safety-critical: it commands
		// curtailment on miners in active physical maintenance. Wire the
		// same admin gate as allow_unbounded so a non-admin API-key
		// caller cannot trigger a forced power-cycle of a miner a
		// technician is servicing.
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StartCurtailment")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
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
		// Mirror Preview: surface as InvalidArgument with structured detail.
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
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
	return connect.NewResponse(&pb.UpdateCurtailmentEventResponse{
		Event: toEventProto(event),
	}), nil
}

func (h *Handler) StopCurtailment(ctx context.Context, req *connect.Request[pb.StopCurtailmentRequest]) (*connect.Response[pb.StopCurtailmentResponse], error) {
	if req.Msg.GetForce() {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("StopCurtailment")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
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

func (h *Handler) ListCurtailmentEvents(ctx context.Context, req *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
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

// AdminTerminateEvent forces a non-terminal event to terminal. Paired with
// SessionOnlyProcedures (interceptors/config.go); neither alone is enough.
func (h *Handler) AdminTerminateEvent(ctx context.Context, req *connect.Request[pb.AdminTerminateEventRequest]) (*connect.Response[pb.AdminTerminateEventResponse], error) {
	if err := requireAdminFromContext(ctx, actionTerminateEvents); err != nil {
		return nil, err
	}
	if h.service == nil {
		return nil, errCurtailmentNotImplemented("AdminTerminateEvent")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	terminateReq, err := toAdminTerminateRequest(req.Msg, info)
	if err != nil {
		return nil, err
	}
	event, err := h.service.AdminTerminate(ctx, terminateReq)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminTerminateEventResponse{
		Event: toEventProto(event),
	}), nil
}

func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}

// requireAdminFromContext returns Forbidden unless the caller has Admin or SuperAdmin role.
func requireAdminFromContext(ctx context.Context, action string) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		// Remap missing session from Internal to Unauthenticated.
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
