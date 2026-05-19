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
// startEnabled additionally gates StartCurtailment until Stop + restorer
// land, so an operator can't Start an event that has no exit path.
type Handler struct {
	service      *curtailment.Service
	startEnabled bool
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

func NewHandler(service *curtailment.Service, startEnabled bool) *Handler {
	return &Handler{service: service, startEnabled: startEnabled}
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
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.AllowUnbounded {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	if !h.startEnabled {
		// Gated until Stop + restorer ship; an Active event has no
		// operator-facing exit path otherwise.
		return nil, errCurtailmentNotImplemented("StartCurtailment")
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

	return connect.NewResponse(toStartResponse(plan, req.Msg)), nil
}

func (h *Handler) UpdateCurtailmentEvent(_ context.Context, _ *connect.Request[pb.UpdateCurtailmentEventRequest]) (*connect.Response[pb.UpdateCurtailmentEventResponse], error) {
	return nil, errCurtailmentNotImplemented("UpdateCurtailmentEvent")
}

func (h *Handler) StopCurtailment(ctx context.Context, req *connect.Request[pb.StopCurtailmentRequest]) (*connect.Response[pb.StopCurtailmentResponse], error) {
	if req.Msg.RestoreBatchSizeOverride != nil {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	return nil, errCurtailmentNotImplemented("StopCurtailment")
}

func (h *Handler) GetActiveCurtailment(_ context.Context, _ *connect.Request[pb.GetActiveCurtailmentRequest]) (*connect.Response[pb.GetActiveCurtailmentResponse], error) {
	return nil, errCurtailmentNotImplemented("GetActiveCurtailment")
}

func (h *Handler) ListCurtailmentEvents(_ context.Context, _ *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
}

// AdminTerminateEvent forces a non-terminal event to terminal. Paired with
// SessionOnlyProcedures (interceptors/config.go); neither alone is enough.
func (h *Handler) AdminTerminateEvent(ctx context.Context, _ *connect.Request[pb.AdminTerminateEventRequest]) (*connect.Response[pb.AdminTerminateEventResponse], error) {
	if err := requireAdminFromContext(ctx, actionTerminateEvents); err != nil {
		return nil, err
	}
	return nil, errCurtailmentNotImplemented("AdminTerminateEvent")
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
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}
