// Package curtailment registers the curtailment v1 handler.
package curtailment

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type previewService interface {
	PreviewCurtailmentPlan(ctx context.Context, req *pb.PreviewCurtailmentPlanRequest) (*pb.PreviewCurtailmentPlanResponse, error)
}

// Handler implements curtailment v1 RPCs.
type Handler struct {
	svc previewService
}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// NewHandler returns a curtailment handler. A nil service keeps preview in
// Unimplemented mode for tests or partial wiring.
func NewHandler(svc previewService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) PreviewCurtailmentPlan(ctx context.Context, req *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	if h.svc == nil {
		return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
	}
	resp, err := h.svc.PreviewCurtailmentPlan(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) StartCurtailment(_ context.Context, _ *connect.Request[pb.StartCurtailmentRequest]) (*connect.Response[pb.StartCurtailmentResponse], error) {
	return nil, errCurtailmentNotImplemented("StartCurtailment")
}

func (h *Handler) UpdateCurtailmentEvent(_ context.Context, _ *connect.Request[pb.UpdateCurtailmentEventRequest]) (*connect.Response[pb.UpdateCurtailmentEventResponse], error) {
	return nil, errCurtailmentNotImplemented("UpdateCurtailmentEvent")
}

func (h *Handler) StopCurtailment(_ context.Context, _ *connect.Request[pb.StopCurtailmentRequest]) (*connect.Response[pb.StopCurtailmentResponse], error) {
	return nil, errCurtailmentNotImplemented("StopCurtailment")
}

func (h *Handler) GetActiveCurtailment(_ context.Context, _ *connect.Request[pb.GetActiveCurtailmentRequest]) (*connect.Response[pb.GetActiveCurtailmentResponse], error) {
	return nil, errCurtailmentNotImplemented("GetActiveCurtailment")
}

func (h *Handler) ListCurtailmentEvents(_ context.Context, _ *connect.Request[pb.ListCurtailmentEventsRequest]) (*connect.Response[pb.ListCurtailmentEventsResponse], error) {
	return nil, errCurtailmentNotImplemented("ListCurtailmentEvents")
}

// errCurtailmentNotImplemented standardizes stub errors.
func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}
