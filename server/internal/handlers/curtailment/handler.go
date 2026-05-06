// Package curtailment wires the v1 RPC surface. PreviewCurtailmentPlan is
// implemented (BE-2); the remaining RPCs return Unimplemented and land in
// follow-up tickets (BE-3 Start + reconciler, BE-4 Stop + restore, BE-5
// read APIs + audit).
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

// Action verbs passed to requireAdminFromContext for error messages. Defining
// them once prevents wording drift across the override call sites.
const (
	actionSupplyOverrideFields = "supply curtailment override fields"
	actionTerminateEvents      = "terminate curtailment events"
)

// Handler implements the curtailment v1 RPC surface. The service field is
// optional: when nil (used by the existing stub-level handler tests) Preview
// returns Unimplemented; when populated (production wiring at fleetd
// startup) Preview is the real implementation.
type Handler struct {
	service *curtailment.Service
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// NewHandler constructs a curtailment handler. Pass nil for the service
// when wiring stub-only tests; pass a populated *curtailment.Service for
// production wiring.
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

	previewReq, err := translatePreviewRequest(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	plan, err := h.service.Preview(ctx, previewReq)
	if err != nil {
		return nil, err
	}

	// Insufficient curtailable load is a request-shape failure, not a
	// successful empty plan — return InvalidArgument with the structured
	// numbers so the UI can render the diagnostic detail directly.
	if plan.InsufficientLoadDetail != nil {
		return nil, translateInsufficientLoad(plan.InsufficientLoadDetail)
	}

	return connect.NewResponse(translatePreviewResponse(plan, req.Msg)), nil
}

func (h *Handler) StartCurtailment(ctx context.Context, req *connect.Request[pb.StartCurtailmentRequest]) (*connect.Response[pb.StartCurtailmentResponse], error) {
	if req.Msg.CandidateMinPowerWOverride != nil || req.Msg.AllowUnbounded {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	return nil, errCurtailmentNotImplemented("StartCurtailment")
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

// AdminTerminateEvent forces a non-terminal event to a terminal state.
// Paired with SessionOnlyProcedures in handlers/interceptors/config.go;
// neither check alone is sufficient.
func (h *Handler) AdminTerminateEvent(ctx context.Context, _ *connect.Request[pb.AdminTerminateEventRequest]) (*connect.Response[pb.AdminTerminateEventResponse], error) {
	if err := requireAdminFromContext(ctx, actionTerminateEvents); err != nil {
		return nil, err
	}
	return nil, errCurtailmentNotImplemented("AdminTerminateEvent")
}

// errCurtailmentNotImplemented standardizes stub errors.
func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}

// requireAdminFromContext returns Forbidden unless the caller has Admin or SuperAdmin role.
func requireAdminFromContext(ctx context.Context, action string) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		// Remap "no session info" from Internal to Unauthenticated so the
		// response code reflects "no identity" rather than "server bug".
		return fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}
