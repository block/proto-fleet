package grpc

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
)

func ErrorLoggingInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			request connect.AnyRequest,
		) (connect.AnyResponse, error) {
			result, err := next(ctx, request)
			if err != nil {
				slog.Error("request error", "path", request.Spec().Procedure, "error", err)
				return nil, err
			}

			return result, nil
		}
	}
}
