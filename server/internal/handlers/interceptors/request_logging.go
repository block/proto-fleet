package interceptors

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

func RequestLoggingInterceptor(debug bool) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Skip logging if debug is disabled
			if !debug {
				slog.Info("Request logging not enabled")
				return next(ctx, req)
			}

			// Log request procedure
			slog.Debug("incoming request",
				"procedure", req.Spec().Procedure)

			// Log request body logging
			if msg, ok := req.Any().(proto.Message); ok {
				slog.Debug("request body", "message", msg)
			}

			// Call the handler
			start := time.Now()
			res, err := next(ctx, req)
			duration := time.Since(start)

			// Log response details
			if err != nil {
				connectErr, ok := err.(*connect.Error)
				if ok {
					slog.Error("request error",
						"error", connectErr.Message(),
						"code", connectErr.Code(),
						"duration", duration)
				} else {
					slog.Error("request error",
						"error", err,
						"duration", duration)
				}
			} else {
				slog.Debug("request success",
					"duration", duration)

				// For detailed response body logging (be careful with sensitive data)
				if msg, ok := res.Any().(proto.Message); ok {
					slog.Debug("response body", "message", msg)
				}
			}

			return res, err
		}
	}
}
