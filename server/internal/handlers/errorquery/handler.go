// Package errorquery provides gRPC handlers for the error query service.
package errorquery

import (
	"context"
	"fmt"
	"sort"

	"connectrpc.com/connect"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics"
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/errorquery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Ensure Handler implements the service interface.
var _ errorsv1connect.ErrorQueryServiceHandler = &Handler{}

// Handler implements the ErrorQueryService gRPC handlers.
type Handler struct {
	service            *errorquery.Service
	diagnosticsService *diagnostics.Service
}

// NewHandler creates a new error query handler.
func NewHandler(service *errorquery.Service, diagnosticsService *diagnostics.Service) *Handler {
	return &Handler{
		service:            service,
		diagnosticsService: diagnosticsService,
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

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
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

// convertDomainErrorToProto converts a domain ErrorMessage to protobuf ErrorMessage.
func convertDomainErrorToProto(domainErr *models.ErrorMessage) *errorsv1.ErrorMessage {
	msg := &errorsv1.ErrorMessage{
		ErrorId:           domainErr.ErrorID,
		CanonicalError:    errorsv1.MinerError(domainErr.MinerError), // #nosec G115 -- MinerError enum bounded by protobuf (max ~9000), safe for int32
		Summary:           domainErr.Summary,
		CauseSummary:      domainErr.CauseSummary,
		RecommendedAction: domainErr.RecommendedAction,
		Severity:          errorsv1.Severity(domainErr.Severity), // #nosec G115 -- Severity enum bounded (max 4), safe for int32
		FirstSeenAt:       timestamppb.New(domainErr.FirstSeenAt),
		LastSeenAt:        timestamppb.New(domainErr.LastSeenAt),
		VendorAttributes:  domainErr.VendorAttributes,
		DeviceIdentifier:  domainErr.DeviceID,
		Impact:            domainErr.Impact,
	}

	if domainErr.ClosedAt != nil {
		msg.ClosedAt = timestamppb.New(*domainErr.ClosedAt)
	}

	if domainErr.ComponentID != nil {
		msg.ComponentId = domainErr.ComponentID
	}

	return msg
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
