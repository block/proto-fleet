package device

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
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

		// Connect-RPC errors with auth codes
		{
			name:     "connect_error_unauthenticated",
			err:      connect.NewError(connect.CodeUnauthenticated, errors.New("missing auth")),
			expected: true,
		},
		{
			name:     "connect_error_permission_denied",
			err:      connect.NewError(connect.CodePermissionDenied, errors.New("access denied")),
			expected: true,
		},
		{
			name:     "connect_error_internal_not_auth",
			err:      connect.NewError(connect.CodeInternal, errors.New("server error")),
			expected: false,
		},
		{
			name:     "connect_error_unknown_not_auth",
			err:      connect.NewError(connect.CodeUnknown, errors.New("unknown error")),
			expected: false,
		},

		// Wrapped Connect-RPC errors
		{
			name:     "wrapped_connect_unauthenticated",
			err:      fmt.Errorf("failed to call API: %w", connect.NewError(connect.CodeUnauthenticated, errors.New("no token"))),
			expected: true,
		},
		{
			name:     "wrapped_connect_permission_denied",
			err:      fmt.Errorf("API error: %w", connect.NewError(connect.CodePermissionDenied, errors.New("forbidden"))),
			expected: true,
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
