package fleeterror

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionError(t *testing.T) {
	t.Run("creates connection error with device identifier", func(t *testing.T) {
		// Arrange
		baseErr := errors.New("connection refused")

		// Act
		connErr := NewConnectionError("device-123", baseErr)

		// Assert
		assert.Equal(t, "device-123", connErr.DeviceIdentifier)
		assert.Equal(t, baseErr, connErr.Err)
		assert.Contains(t, connErr.Error(), "device-123")
		assert.Contains(t, connErr.Error(), "connection refused")
	})

	t.Run("unwraps to underlying error", func(t *testing.T) {
		baseErr := errors.New("timeout")
		connErr := NewConnectionError("device-456", baseErr)

		unwrapped := errors.Unwrap(connErr)
		assert.Equal(t, baseErr, unwrapped)
	})

	t.Run("can be wrapped and detected", func(t *testing.T) {
		baseErr := errors.New("dial tcp failed")
		connErr := NewConnectionError("device-789", baseErr)
		wrappedErr := fmt.Errorf("failed to get status: %w", connErr)

		assert.True(t, IsConnectionError(wrappedErr))
	})
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "direct ConnectionError",
			err:      NewConnectionError("device-123", errors.New("connection refused")),
			expected: true,
		},
		{
			name:     "wrapped ConnectionError",
			err:      fmt.Errorf("outer: %w", NewConnectionError("device-456", errors.New("timeout"))),
			expected: true,
		},
		{
			name:     "doubly wrapped ConnectionError",
			err:      fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", NewConnectionError("device-789", errors.New("network error")))),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "wrapped generic error",
			err:      fmt.Errorf("wrapped: %w", errors.New("generic error")),
			expected: false,
		},
		{
			name:     "FleetError is not ConnectionError",
			err:      NewInternalError("internal error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsConnectionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConnectionErrorWithErrorsAs(t *testing.T) {
	t.Run("errors.As can extract ConnectionError", func(t *testing.T) {
		// Arrange
		baseErr := errors.New("connection refused")
		connErr := NewConnectionError("device-123", baseErr)
		wrappedErr := fmt.Errorf("failed to connect: %w", connErr)

		// Act
		var extractedErr ConnectionError
		require.True(t, errors.As(wrappedErr, &extractedErr))

		// Assert
		assert.Equal(t, "device-123", extractedErr.DeviceIdentifier)
		assert.Equal(t, baseErr, extractedErr.Err)
	})

	t.Run("errors.As returns false for non-ConnectionError", func(t *testing.T) {
		genericErr := errors.New("generic error")

		var connErr ConnectionError
		assert.False(t, errors.As(genericErr, &connErr))
	})
}

func TestIsUnimplementedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "FleetError with CodeUnimplemented",
			err:      NewUnimplementedError("not supported"),
			expected: true,
		},
		{
			name:     "FleetError with CodeUnimplemented via format",
			err:      NewUnimplementedErrorf("capability %s not supported", "reboot"),
			expected: true,
		},
		{
			name:     "wrapped FleetError with CodeUnimplemented",
			err:      fmt.Errorf("plugin error: %w", NewUnimplementedError("not supported")),
			expected: true,
		},
		{
			name:     "connect.Error with CodeUnimplemented",
			err:      connect.NewError(connect.CodeUnimplemented, errors.New("unimplemented")),
			expected: true,
		},
		{
			name:     "wrapped connect.Error with CodeUnimplemented",
			err:      fmt.Errorf("rpc failed: %w", connect.NewError(connect.CodeUnimplemented, errors.New("unimplemented"))),
			expected: true,
		},
		{
			name:     "FleetError with CodeInternal is not unimplemented",
			err:      NewInternalError("internal error"),
			expected: false,
		},
		{
			name:     "FleetError with CodeNotFound is not unimplemented",
			err:      NewNotFoundError("not found"),
			expected: false,
		},
		{
			name:     "connect.Error with CodeInternal is not unimplemented",
			err:      connect.NewError(connect.CodeInternal, errors.New("internal")),
			expected: false,
		},
		{
			name:     "generic error is not unimplemented",
			err:      errors.New("something went wrong"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result := IsUnimplementedError(tt.err)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsUnavailableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "FleetError with CodeUnavailable via format",
			err:      NewUnavailableErrorf("device %s unavailable", "miner-1"),
			expected: true,
		},
		{
			name:     "wrapped FleetError with CodeUnavailable",
			err:      fmt.Errorf("plugin error: %w", NewUnavailableErrorf("device unavailable")),
			expected: true,
		},
		{
			name:     "connect.Error with CodeUnavailable",
			err:      connect.NewError(connect.CodeUnavailable, errors.New("unavailable")),
			expected: true,
		},
		{
			name:     "wrapped connect.Error with CodeUnavailable",
			err:      fmt.Errorf("rpc failed: %w", connect.NewError(connect.CodeUnavailable, errors.New("unavailable"))),
			expected: true,
		},
		{
			name:     "FleetError with CodeInternal is not unavailable",
			err:      NewInternalError("internal error"),
			expected: false,
		},
		{
			name:     "FleetError with CodeUnimplemented is not unavailable",
			err:      NewUnimplementedError("unimplemented"),
			expected: false,
		},
		{
			name:     "connect.Error with CodeInternal is not unavailable",
			err:      connect.NewError(connect.CodeInternal, errors.New("internal")),
			expected: false,
		},
		{
			name:     "generic error is not unavailable",
			err:      errors.New("something went wrong"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnavailableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsFailedPreconditionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "FleetError with CodeFailedPrecondition",
			err:      NewFailedPreconditionErrorf("payload too large"),
			expected: true,
		},
		{
			name:     "wrapped FleetError with CodeFailedPrecondition",
			err:      fmt.Errorf("plugin error: %w", NewFailedPreconditionErrorf("payload too large")),
			expected: true,
		},
		{
			name:     "connect.Error with CodeFailedPrecondition",
			err:      connect.NewError(connect.CodeFailedPrecondition, errors.New("precondition failed")),
			expected: true,
		},
		{
			name:     "wrapped connect.Error with CodeFailedPrecondition",
			err:      fmt.Errorf("rpc failed: %w", connect.NewError(connect.CodeFailedPrecondition, errors.New("precondition failed"))),
			expected: true,
		},
		{
			name:     "FleetError with CodeInternal is not failed precondition",
			err:      NewInternalError("internal error"),
			expected: false,
		},
		{
			name:     "FleetError with CodeUnimplemented is not failed precondition",
			err:      NewUnimplementedError("unimplemented"),
			expected: false,
		},
		{
			name:     "connect.Error with CodeUnknown is not failed precondition",
			err:      connect.NewError(connect.CodeUnknown, errors.New("unknown")),
			expected: false,
		},
		{
			name:     "generic error is not failed precondition",
			err:      errors.New("something went wrong"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFailedPreconditionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsCanceledError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "context.Canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "wrapped context.Canceled",
			err:      fmt.Errorf("operation failed: %w", context.Canceled),
			expected: true,
		},
		{
			name:     "FleetError with CodeCanceled",
			err:      NewCanceledError(),
			expected: true,
		},
		{
			name:     "wrapped FleetError with CodeCanceled",
			err:      fmt.Errorf("stream error: %w", NewCanceledError()),
			expected: true,
		},
		{
			name:     "connect.Error with CodeCanceled",
			err:      connect.NewError(connect.CodeCanceled, errors.New("canceled")),
			expected: true,
		},
		{
			name:     "wrapped connect.Error with CodeCanceled",
			err:      fmt.Errorf("rpc failed: %w", connect.NewError(connect.CodeCanceled, errors.New("canceled"))),
			expected: true,
		},
		{
			name:     "FleetError with CodeInternal is not canceled",
			err:      NewInternalError("internal error"),
			expected: false,
		},
		{
			name:     "FleetError with CodeNotFound is not canceled",
			err:      NewNotFoundError("not found"),
			expected: false,
		},
		{
			name:     "connect.Error with CodeInternal is not canceled",
			err:      connect.NewError(connect.CodeInternal, errors.New("internal")),
			expected: false,
		},
		{
			name:     "generic error is not canceled",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "context.DeadlineExceeded is not canceled",
			err:      context.DeadlineExceeded,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCanceledError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
