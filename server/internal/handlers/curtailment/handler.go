// Package curtailment exposes the curtailment Connect-RPC surface. v1 wires
// every RPC to a stub that returns connect.CodeUnimplemented; persistence,
// the selector, dispatch, reconciliation, and restore land in follow-up
// issues. The proto types and route registrations exist now so later work
// can plug into a fixed handler/main.go contract.
package curtailment

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Handler implements curtailmentv1connect.CurtailmentServiceHandler. All v1
// RPCs return connect.CodeUnimplemented until the corresponding business
// logic lands.
type Handler struct{}

var _ curtailmentv1connect.CurtailmentServiceHandler = &Handler{}

// NewHandler returns a Handler. It takes no dependencies in v1 because every
// RPC is a stub; later issues will inject the curtailment domain service.
func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) PreviewCurtailmentPlan(_ context.Context, _ *connect.Request[pb.PreviewCurtailmentPlanRequest]) (*connect.Response[pb.PreviewCurtailmentPlanResponse], error) {
	return nil, errCurtailmentNotImplemented("PreviewCurtailmentPlan")
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

// errCurtailmentNotImplemented produces a uniform error message for v1 stubs
// so log surfaces and client errors are easy to grep for during the build-out.
func errCurtailmentNotImplemented(rpc string) error {
	return fleeterror.NewUnimplementedErrorf("curtailment.%s is not implemented yet", rpc)
}
