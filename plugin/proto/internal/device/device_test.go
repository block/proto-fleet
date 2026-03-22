package device

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		// Nil error
		{
			name:     "nil_error",
			err:      nil,
			expected: false,
		},

		// HTTP status code detection
		{
			name:     "http_401_status",
			err:      fmt.Errorf("request failed with status %d", http.StatusUnauthorized),
			expected: true,
		},
		{
			name:     "wrapped_http_401_status",
			err:      fmt.Errorf("API call failed: %w", fmt.Errorf("status %d: unauthorized", http.StatusUnauthorized)),
			expected: true,
		},
		{
			name:     "http_500_status_not_auth",
			err:      fmt.Errorf("request failed with status %d", http.StatusInternalServerError),
			expected: false,
		},
		{
			name:     "http_403_status_not_auth",
			err:      fmt.Errorf("request failed with status %d", http.StatusForbidden),
			expected: false,
		},

		// String-based detection (serialized errors that crossed gRPC boundary)
		{
			name:     "string_unauthenticated",
			err:      errors.New("rpc error: code=Unknown desc=unauthenticated, missing API key"),
			expected: true,
		},
		{
			name:     "string_missing_api_key",
			err:      errors.New("failed to verify: missing api key - set via set auth key first"),
			expected: true,
		},
		{
			name:     "string_unauthorized",
			err:      errors.New("request failed: unauthorized access"),
			expected: true,
		},
		{
			name:     "string_authentication_failed",
			err:      errors.New("authentication failed: invalid token"),
			expected: true,
		},
		{
			name:     "string_invalid_credentials",
			err:      errors.New("login failed: invalid credentials"),
			expected: true,
		},

		// Negative cases - should NOT be auth errors
		{
			name:     "network_error_connection_refused",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "generic_internal_error",
			err:      errors.New("internal server error"),
			expected: false,
		},
		{
			name:     "timeout_error",
			err:      context.DeadlineExceeded,
			expected: false,
		},
		{
			name:     "io_timeout_error",
			err:      errors.New("i/o timeout"),
			expected: false,
		},
		{
			name:     "device_not_found_error",
			err:      errors.New("device not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthenticationError(tt.err)
			assert.Equal(t, tt.expected, result, "isAuthenticationError(%v)", tt.err)
		})
	}
}
