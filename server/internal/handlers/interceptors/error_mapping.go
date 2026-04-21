package interceptors

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type ErrorMappingInterceptor struct{}

var _ connect.Interceptor = &ErrorMappingInterceptor{}

func NewErrorMappingInterceptor() *ErrorMappingInterceptor {
	return &ErrorMappingInterceptor{}
}

func (e *ErrorMappingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(
		ctx context.Context,
		request connect.AnyRequest,
	) (connect.AnyResponse, error) {
		result, err := next(ctx, request)

		return result, mapError(err)
	}
}

func (e *ErrorMappingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (e *ErrorMappingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)

		return mapError(err)
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}

	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.ConnectError()
	}

	return fleeterror.NewInternalError(err.Error()).ConnectError()
}
