// Package curtailment registers v1 stubs that return Unimplemented.
package curtailment

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// Action verbs passed to requireAdminFromContext for error messages. Defining
// them once prevents wording drift across the override call sites.
const (
	actionSupplyOverrideFields = "supply curtailment override fields"
	actionTransitionEvents     = "transition curtailment events"
)

// Handler implements curtailment v1 stubs.
type Handler struct{}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// NewHandler returns a stub curtailment handler.
func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) PreviewCurtailmentPlan(ctx context.Context, req *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	if req.Msg.CandidateMinPowerWOverride != nil {
		if err := requireAdminFromContext(ctx, actionSupplyOverrideFields); err != nil {
			return nil, err
		}
	}
	return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
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

// AdminTransitionEvent forces a non-terminal event to a terminal state. Admin-only.
func (h *Handler) AdminTransitionEvent(ctx context.Context, _ *connect.Request[pb.AdminTransitionEventRequest]) (*connect.Response[pb.AdminTransitionEventResponse], error) {
	if err := requireAdminFromContext(ctx, actionTransitionEvents); err != nil {
		return nil, err
	}
	return nil, errCurtailmentNotImplemented("AdminTransitionEvent")
}

// errCurtailmentNotImplemented standardizes stub errors.
func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}

// requireAdminFromContext enforces the Admin / SuperAdmin gate on the caller.
// Also rejects API-key auth so a leaked key cannot exercise admin-gated paths
// even when its owning user has the admin role; this hardens the override path
// on Preview, which is otherwise API-key-accessible.
func requireAdminFromContext(ctx context.Context, action string) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return fleeterror.NewForbiddenErrorf("API key auth cannot %s; use an admin session", action)
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return nil
}
