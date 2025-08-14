package interceptors

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
)

// LoggingInterceptor provides request/response logging
type LoggingInterceptor struct{}

// NewLoggingInterceptor creates a new logging interceptor
func NewLoggingInterceptor() connect.Interceptor {
	return &LoggingInterceptor{}
}

// WrapUnary implements the connect.Interceptor interface
func (i *LoggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()
		procedure := req.Spec().Procedure

		slog.Debug("Starting RPC call", "procedure", procedure)

		resp, err := next(ctx, req)

		duration := time.Since(start)
		if err != nil {
			slog.Error("RPC call failed", "procedure", procedure, "duration", duration, "error", err)
		} else {
			slog.Debug("RPC call completed", "procedure", procedure, "duration", duration)
		}

		return resp, err
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *LoggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		slog.Debug("Starting streaming RPC", "procedure", spec.Procedure)
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *LoggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next // No modification needed for server-side handlers
}
