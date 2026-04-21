package interceptors

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type RequestLoggingInterceptor struct {
	logLevel              slog.Level
	nextRequestID         atomic.Int64
	redactRequestForProc  map[string]struct{}
	redactResponseForProc map[string]struct{}
}

var _ connect.Interceptor = &RequestLoggingInterceptor{}

func NewRequestLoggingInterceptor(level slog.Level, redactRequestProcedures, redactResponseProcedures []string) *RequestLoggingInterceptor {
	reqSet := make(map[string]struct{}, len(redactRequestProcedures))
	for _, p := range redactRequestProcedures {
		reqSet[p] = struct{}{}
	}
	respSet := make(map[string]struct{}, len(redactResponseProcedures))
	for _, p := range redactResponseProcedures {
		respSet[p] = struct{}{}
	}
	return &RequestLoggingInterceptor{
		logLevel:              level,
		redactRequestForProc:  reqSet,
		redactResponseForProc: respSet,
	}
}

func (e *RequestLoggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(
		ctx context.Context,
		request connect.AnyRequest,
	) (connect.AnyResponse, error) {
		start := time.Now()

		result, err := next(ctx, request)

		duration := time.Since(start)

		e.logUnaryRequest(request, duration, result, err)

		return result, err
	}
}

func (e *RequestLoggingInterceptor) logUnaryRequest(request connect.AnyRequest, duration time.Duration, result connect.AnyResponse, err error) {
	procedure := request.Spec().Procedure
	_, redactRequest := e.redactRequestForProc[procedure]
	_, redactResponse := e.redactResponseForProc[procedure]

	reqBody := any("[REDACTED]")
	if !redactRequest {
		reqBody = request.Any()
	}

	logBody := e.logLevel <= slog.LevelDebug && !SensitiveBodyProcedures[procedure]

	if err != nil {
		if logBody {
			slog.Error("incoming unary request failed",
				"procedure", procedure,
				"took", duration,
				"request", reqBody,
				"error", err,
			)
		} else {
			slog.Error("incoming unary request failed", "procedure", procedure, "took", duration, "error", err)
		}
	} else {
		if logBody {
			respBody := any("[REDACTED]")
			if !redactResponse {
				respBody = result.Any()
			}
			slog.Debug("incoming unary request",
				"procedure", procedure,
				"took", duration,
				"request", reqBody,
				"result", respBody,
			)
		} else {
			slog.Debug("incoming unary request", "procedure", procedure, "took", duration)
		}
	}
}

func (e *RequestLoggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (e *RequestLoggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		requestID := getAndAdd(&e.nextRequestID, 1)
		procedure := conn.Spec().Procedure

		e.logStreamingRequestStarted(requestID, procedure)

		start := time.Now()

		wrappedConn := &loggingStreamingHandlerConn{
			StreamingHandlerConn: conn,
			requestID:            requestID,
			procedure:            procedure,
			start:                start,
			logLevel:             e.logLevel,
		}

		err := next(ctx, wrappedConn)

		duration := time.Since(start)

		e.logStreamingRequestEnded(err, requestID, procedure, duration)

		return err
	}
}

func (e *RequestLoggingInterceptor) logStreamingRequestStarted(requestID int64, procedure string) {
	slog.Info("incoming streaming request started",
		"request_id", requestID,
		"procedure", procedure,
	)
}

func (e *RequestLoggingInterceptor) logStreamingRequestEnded(err error, requestID int64, procedure string, duration time.Duration) {
	if err != nil {
		// Check if this is a cancellation error (client disconnect) - log at INFO level since it's expected
		if fleeterror.IsCanceledError(err) {
			slog.Info("incoming streaming request ended (client disconnected)",
				"request_id", requestID,
				"procedure", procedure,
				"duration", duration,
			)
			return
		}
		slog.Error("incoming streaming request failed",
			"request_id", requestID,
			"procedure", procedure,
			"duration", duration,
			"error", err,
		)
	} else {
		slog.Info("incoming streaming request ended successfully",
			"request_id", requestID,
			"procedure", procedure,
			"duration", duration,
		)
	}
}

type loggingStreamingHandlerConn struct {
	connect.StreamingHandlerConn
	requestID       int64
	procedure       string
	start           time.Time
	logLevel        slog.Level
	receivedCounter atomic.Int64
	sentCounter     atomic.Int64
}

func (w *loggingStreamingHandlerConn) Receive(msg interface{}) error {
	err := w.StreamingHandlerConn.Receive(msg)

	if errors.Is(err, io.EOF) {
		// nolint:wrapcheck
		return err
	}

	messageIndex := getAndAdd(&w.receivedCounter, 1)

	if err != nil {
		slog.Error("incoming streaming request received an error",
			"request_id", w.requestID,
			"procedure", w.procedure,
			"since_start", time.Since(w.start),
			"message_index", messageIndex,
			"error", err,
		)
	} else {
		slog.Debug("incoming streaming request received a message",
			"request_id", w.requestID,
			"procedure", w.procedure,
			"since_start", time.Since(w.start),
			"message_index", messageIndex,
			"message", msg,
		)
	}

	// nolint:wrapcheck
	return err
}

func (w *loggingStreamingHandlerConn) Send(msg interface{}) error {
	err := w.StreamingHandlerConn.Send(msg)

	messageIndex := getAndAdd(&w.sentCounter, 1)

	if err != nil {
		if w.logLevel <= slog.LevelDebug {
			slog.Error("incoming streaming request failed to send a message",
				"request_id", w.requestID,
				"procedure", w.procedure,
				"since_start", time.Since(w.start),
				"message_index", messageIndex,
				"message", msg,
				"error", err,
			)
		} else {
			slog.Error("incoming streaming request failed to send a message",
				"request_id", w.requestID,
				"procedure", w.procedure,
				"since_start", time.Since(w.start),
				"message_index", messageIndex,
				"error", err,
			)
		}
	} else {
		slog.Debug("incoming streaming request sent a message",
			"request_id", w.requestID,
			"procedure", w.procedure,
			"since_start", time.Since(w.start),
			"message_index", messageIndex,
			"message", msg,
		)
	}

	// nolint:wrapcheck
	return err
}

func getAndAdd(atomicInt *atomic.Int64, delta int64) int64 {
	return atomicInt.Add(delta) - delta
}
