// Package errorquery provides gRPC handlers for the error query service.
package errorquery

import (
	"context"
	"fmt"
	"sort"

	"connectrpc.com/connect"

	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// Ensure Handler implements the service interface.
var _ errorsv1connect.ErrorQueryServiceHandler = &Handler{}

// Handler implements the ErrorQueryService gRPC handlers.
type Handler struct {
	diagnosticsService *diagnostics.Service
}

// NewHandler creates a new error query handler.
func NewHandler(diagnosticsService *diagnostics.Service) *Handler {
	return &Handler{
		diagnosticsService: diagnosticsService,
	}
}

// getSessionInfo retrieves session information from the context and returns an authentication error if not present.
func getSessionInfo(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return info, nil
}

// Query handles the Query RPC call.
func (h *Handler) Query(
	ctx context.Context,
	req *connect.Request[errorsv1.QueryRequest],
) (*connect.Response[errorsv1.QueryResponse], error) {
	info, err := getSessionInfo(ctx)
	if err != nil {
		return nil, err
	}

	opts := convertQueryRequestToDomain(info.OrganizationID, req.Msg)

	result, err := h.diagnosticsService.Query(ctx, opts)
	if fleeterror.IsInvalidArgumentError(err) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(convertQueryResultToProto(result)), nil
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

	info, err := getSessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	orgID := info.OrganizationID

	errorMsg, err := h.diagnosticsService.GetError(ctx, orgID, errorID)
	if fleeterror.IsNotFoundError(err) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protoError := convertDomainErrorToProto(errorMsg)

	return connect.NewResponse(&errorsv1.GetErrorResponse{
		Error: protoError,
	}), nil
}

// ListMinerErrors handles the ListMinerErrors RPC call.
func (h *Handler) ListMinerErrors(
	ctx context.Context,
	_ *connect.Request[errorsv1.ListMinerErrorsRequest],
) (*connect.Response[errorsv1.ListMinerErrorsResponse], error) {
	metadata := h.diagnosticsService.ListMinerErrors(ctx)

	var items []*errorsv1.MinerErrorInfo
	for code, info := range metadata {
		if code == models.MinerErrorUnspecified {
			continue
		}
		items = append(items, &errorsv1.MinerErrorInfo{
			Code:            errorsv1.MinerError(code), // #nosec G115 -- MinerError enum values bounded by protobuf
			Name:            info.Name,
			DefaultSummary:  info.DefaultSummary,
			DefaultSeverity: errorsv1.Severity(info.DefaultSeverity), // #nosec G115 -- Severity enum values bounded (max 4)
			DefaultAction:   info.DefaultAction,
			DefaultImpact:   info.DefaultImpact,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Code < items[j].Code
	})

	return connect.NewResponse(&errorsv1.ListMinerErrorsResponse{Items: items}), nil
}

// Watch handles the Watch streaming RPC call.
func (h *Handler) Watch(
	ctx context.Context,
	req *connect.Request[errorsv1.WatchRequest],
	stream *connect.ServerStream[errorsv1.WatchResponse],
) error {
	info, err := getSessionInfo(ctx)
	if err != nil {
		return err
	}

	opts := convertWatchRequestToDomain(req.Msg)

	updateChan, err := h.diagnosticsService.Watch(ctx, info.OrganizationID, opts)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}

	for {
		select {
		case <-ctx.Done():
			return connect.NewError(connect.CodeAborted, ctx.Err())
		case update, ok := <-updateChan:
			if !ok {
				return nil
			}
			protoResp := convertWatchUpdateToProto(update)
			if err := stream.Send(protoResp); err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send watch event: %w", err))
			}
		}
	}
}
