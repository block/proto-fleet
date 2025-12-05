// Package errorquery provides gRPC handlers for the error query service.
package errorquery

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/errorquery"
)

// Ensure Handler implements the service interface.
var _ errorsv1connect.ErrorQueryServiceHandler = &Handler{}

// Handler implements the ErrorQueryService gRPC handlers.
type Handler struct {
	service *errorquery.Service
}

// NewHandler creates a new error query handler.
func NewHandler(service *errorquery.Service) *Handler {
	return &Handler{
		service: service,
	}
}

// Query handles the Query RPC call.
func (h *Handler) Query(
	ctx context.Context,
	req *connect.Request[errorsv1.QueryRequest],
) (*connect.Response[errorsv1.QueryResponse], error) {
	resp, err := h.service.Query(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

// GetError handles the GetError RPC call.
func (h *Handler) GetError(
	ctx context.Context,
	req *connect.Request[errorsv1.GetErrorRequest],
) (*connect.Response[errorsv1.GetErrorResponse], error) {
	errorID := req.Msg.GetErrorId()
	if errorID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("error_id is required"))
	}

	errorMsg, err := h.service.GetError(ctx, errorID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&errorsv1.GetErrorResponse{
		Error: errorMsg,
	}), nil
}

// ListMinerErrors handles the ListMinerErrors RPC call.
func (h *Handler) ListMinerErrors(
	ctx context.Context,
	_ *connect.Request[errorsv1.ListMinerErrorsRequest],
) (*connect.Response[errorsv1.ListMinerErrorsResponse], error) {
	resp, err := h.service.ListMinerErrors(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

// Watch handles the Watch streaming RPC call.
func (h *Handler) Watch(
	ctx context.Context,
	req *connect.Request[errorsv1.WatchRequest],
	stream *connect.ServerStream[errorsv1.WatchResponse],
) error {
	updateChan, err := h.service.Watch(ctx, req.Msg.GetFilter())
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	for {
		select {
		case <-ctx.Done():
			return connect.NewError(connect.CodeAborted, ctx.Err())
		case event, ok := <-updateChan:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return fmt.Errorf("failed to send watch event: %w", err)
			}
		}
	}
}
