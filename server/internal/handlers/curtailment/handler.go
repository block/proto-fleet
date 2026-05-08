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

// Handler implements the curtailment RPC surface. service=nil keeps the
// stub-level tests' Unimplemented contract; populated wires the real impl.
// startEnabled gates the operator-facing Start RPC: BE-3 ships the dispatch +
// reconciler primitives behind this flag and BE-4 (Stop + restorer +
// max_duration_seconds enforcement) flips the default. Without BE-4 an
// operator could Start an event that has no Stop path, so production
// deployments default to startEnabled=false until BE-4 is in place.
type Handler struct {
	service      *curtailment.Service
	startEnabled bool
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// NewHandler returns a curtailment Handler. Pass nil for the stub-only
// path (Preview returns Unimplemented); pass a populated *Service to wire
// the real implementation. startEnabled gates StartCurtailment; see the
// Handler godoc for the BE-3/BE-4 coupling.
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

	// Insufficient curtailable load is a request-shape failure, not a
	// successful empty plan — return InvalidArgument with the structured
	// numbers so the UI can render the diagnostic detail directly.
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
		// BE-3 ships dispatch + reconciler primitives but no Stop/restorer
		// yet (BE-4). Until that lands operators must not be able to Start
		// an event with no exit path.
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
		// Mirror Preview: insufficient curtailable load is a request-shape
		// failure with structured numbers, not a successful empty event.
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

// AdminTerminateEvent forces a non-terminal event to a terminal state.
// Paired with SessionOnlyProcedures in handlers/interceptors/config.go;
// neither check alone is sufficient.
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
		// Remap "no session info" from Internal to Unauthenticated so the
		// response code reflects "no identity" rather than "server bug".
		return fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}
