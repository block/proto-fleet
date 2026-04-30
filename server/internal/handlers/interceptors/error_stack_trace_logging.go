package interceptors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type ErrorStackTraceLoggingInterceptor struct {
	logLevel slog.Level
}

var _ connect.Interceptor = &ErrorStackTraceLoggingInterceptor{}

func NewErrorStackTraceLoggingInterceptor(level slog.Level) *ErrorStackTraceLoggingInterceptor {
	return &ErrorStackTraceLoggingInterceptor{
		logLevel: level,
	}
}

func (e ErrorStackTraceLoggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(
		ctx context.Context,
		request connect.AnyRequest,
	) (connect.AnyResponse, error) {
		result, err := next(ctx, request)

		e.logError(err)

		return result, err
	}
}

func (e ErrorStackTraceLoggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (e ErrorStackTraceLoggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)

		e.logError(err)

		return err
	}
}

func (e ErrorStackTraceLoggingInterceptor) logError(err error) {
	if err == nil {
		return
	}

	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		if fleetErr.IsExpected() {
			if e.logLevel <= slog.LevelDebug {
				_, _ = fmt.Fprint(os.Stderr, fleetErr.ErrorWithStackTrace())
			}
		} else {
			if e.logLevel <= slog.LevelWarn {
				_, _ = fmt.Fprint(os.Stderr, fleetErr.ErrorWithStackTrace())
			}
		}
		return
	}

	// Errors from third-party interceptors (e.g. protovalidate) are
	// *connect.Error values that bypass our FleetError plumbing. If their
	// code is in the expected/client-error set, treat them like an
	// expected FleetError -- not a code-quality issue worth warning about.
	var connectErr *connect.Error
	if errors.As(err, &connectErr) && fleeterror.IsExpectedCode(connectErr.Code()) {
		return
	}

	slog.Warn("non-FleetError encountered - possible missing error wrapping", "error", err.Error())
}
