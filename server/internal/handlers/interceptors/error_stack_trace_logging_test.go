package interceptors

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

// captureSlogWarn redirects slog to a buffer and returns it.
func captureSlogWarn(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestLogError_DoesNotWarnOnExpectedConnectError(t *testing.T) {
	// Arrange -- protovalidate emits *connect.Error with InvalidArgument; that
	// is in fleeterror.isExpectedCode and should be classified as expected.
	out := captureSlogWarn(t)
	interceptor := NewErrorStackTraceLoggingInterceptor(slog.LevelInfo)
	err := connect.NewError(connect.CodeInvalidArgument, errors.New("validation error: url: does not match regex pattern"))

	// Act
	interceptor.logError(err)

	// Assert
	assert.NotContains(t, out.String(), "non-FleetError encountered",
		"InvalidArgument from a non-FleetError source should not trigger the missing-wrapping warning")
}

func TestLogError_StillWarnsOnUnexpectedConnectError(t *testing.T) {
	// Arrange -- Internal-class connect.Error is genuinely unexpected and should
	// still be flagged so a developer wraps it as FleetError.
	out := captureSlogWarn(t)
	interceptor := NewErrorStackTraceLoggingInterceptor(slog.LevelInfo)
	err := connect.NewError(connect.CodeInternal, errors.New("oops"))

	// Act
	interceptor.logError(err)

	// Assert
	assert.Contains(t, out.String(), "non-FleetError encountered",
		"Internal-class non-FleetError should still WARN")
}

func TestLogError_StillWarnsOnPlainError(t *testing.T) {
	// Arrange
	out := captureSlogWarn(t)
	interceptor := NewErrorStackTraceLoggingInterceptor(slog.LevelInfo)
	err := errors.New("bare error from somewhere")

	// Act
	interceptor.logError(err)

	// Assert
	assert.Contains(t, out.String(), "non-FleetError encountered",
		"a non-connect, non-FleetError must still surface the warning")
	assert.True(t, strings.Contains(out.String(), "bare error from somewhere"),
		"underlying error message must appear in the WARN output")
}
