package fleeterror

import (
	"errors"
	"fmt"
	"testing"

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
