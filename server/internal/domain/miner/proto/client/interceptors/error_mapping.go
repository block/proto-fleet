package interceptors

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
)

// ErrorMappingInterceptor maps Connect errors to fleet errors
type ErrorMappingInterceptor struct{}

// NewErrorMappingInterceptor creates a new error mapping interceptor
func NewErrorMappingInterceptor() connect.Interceptor {
	return &ErrorMappingInterceptor{}
}

// WrapUnary implements the connect.Interceptor interface
func (i *ErrorMappingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		if err != nil {
			// Map Connect errors to fleet errors
			return resp, mapConnectError(err)
		}
		return resp, nil
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *ErrorMappingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *ErrorMappingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next // No modification needed for server-side handlers
}

// mapConnectError maps Connect errors to fleet errors
func mapConnectError(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		//nolint:exhaustive // Handle specific Connect error codes that directly map to fleet errors
		switch connectErr.Code() {
		case connect.CodeUnauthenticated:
			return fleeterror.NewUnauthenticatedError(connectErr.Message())
		case connect.CodePermissionDenied:
			return fleeterror.NewForbiddenError(connectErr.Message())
		case connect.CodeNotFound:
			return fleeterror.NewInternalErrorf("not found: %v", connectErr.Message())
		case connect.CodeUnavailable:
			return fleeterror.NewInternalErrorf("service unavailable: %v", connectErr.Message())
		case connect.CodeDeadlineExceeded:
			return fleeterror.NewInternalErrorf("timeout: %v", connectErr.Message())
		case connect.CodeInvalidArgument:
			return fleeterror.NewInvalidArgumentError(connectErr.Message())
		case connect.CodeCanceled:
			return fleeterror.NewCanceledError()
		default:
			return fleeterror.NewInternalErrorf("RPC error: %v", connectErr.Message())
		}
	}
	return fleeterror.NewInternalErrorf("unknown error: %v", err)
}
