package fleeterror

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetErrorUnwrap(t *testing.T) {
	t.Run("Errorf with %w exposes the wrapped error via errors.Unwrap", func(t *testing.T) {
		base := errors.New("boom")
		fe := NewInternalErrorf("op failed: %w", base)
		assert.Same(t, base, errors.Unwrap(fe))
	})

	t.Run("Errorf with %v leaves Unwrap == nil", func(t *testing.T) {
		base := errors.New("boom")
		fe := NewInternalErrorf("op failed: %v", base)
		assert.Nil(t, errors.Unwrap(fe))
	})

	t.Run("errors.As walks through FleetError to the wrapped sentinel", func(t *testing.T) {
		// Mirrors the WithTransaction retry path: a business wrapper
		// over a sql/pg error must still let callers see the inner
		// error via errors.As so retry detection works.
		type pgLike struct{ error }
		base := pgLike{error: errors.New("serialization_failure")}
		fe := NewInternalErrorf("authz: list roles: %w", base)

		var got pgLike
		require.True(t, errors.As(fe, &got))
		assert.Equal(t, "serialization_failure", got.Error())
	})

	t.Run("Unwrap is nil for non-Errorf constructors", func(t *testing.T) {
		fe := NewInternalError("static message")
		assert.Nil(t, errors.Unwrap(fe))
	})
}

func TestNotActiveErrorIsMachineReadableAndRetryable(t *testing.T) {
	err := NewNotActiveError()

	require.Equal(t, connect.CodeUnavailable, err.GRPCCode)
	require.Equal(t, int32(commonv1.FleetErrorCode_FLEET_ERROR_CODE_NOT_ACTIVE), err.FleetErrorCode)
	require.Equal(t, ErrorCodeTypeCommon, err.FleetErrorCodeType)
}

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

func TestErrorCodePredicates(t *testing.T) {
	type row struct {
		name string
		err  error
		want bool
	}

	predicates := []struct {
		name     string
		is       func(error) bool
		code     connect.Code
		fleetErr error
		extra    []row
	}{
		{
			name:     "IsUnimplementedError",
			is:       IsUnimplementedError,
			code:     connect.CodeUnimplemented,
			fleetErr: NewUnimplementedError("not supported"),
			extra: []row{
				{name: "FleetError from the format constructor", err: NewUnimplementedErrorf("capability %s not supported", "reboot"), want: true},
			},
		},
		{
			name:     "IsUnavailableError",
			is:       IsUnavailableError,
			code:     connect.CodeUnavailable,
			fleetErr: NewUnavailableErrorf("device %s unavailable", "miner-1"),
		},
		{
			name:     "IsFailedPreconditionError",
			is:       IsFailedPreconditionError,
			code:     connect.CodeFailedPrecondition,
			fleetErr: NewFailedPreconditionErrorf("payload too large"),
		},
		{
			name:     "IsResourceExhaustedError",
			is:       IsResourceExhaustedError,
			code:     connect.CodeResourceExhausted,
			fleetErr: NewResourceExhaustedErrorf("too many miners"),
		},
		{
			name:     "IsCanceledError",
			is:       IsCanceledError,
			code:     connect.CodeCanceled,
			fleetErr: NewCanceledError(),
			extra: []row{
				{name: "context.Canceled", err: context.Canceled, want: true},
				{name: "wrapped context.Canceled", err: fmt.Errorf("operation failed: %w", context.Canceled), want: true},
				{name: "context.DeadlineExceeded", err: context.DeadlineExceeded},
			},
		},
	}

	for _, p := range predicates {
		rows := []row{
			{name: "nil error", err: nil},
			{name: "FleetError with the matching code", err: p.fleetErr, want: true},
			{name: "wrapped FleetError with the matching code", err: fmt.Errorf("plugin error: %w", p.fleetErr), want: true},
			{name: "connect.Error with the matching code", err: connect.NewError(p.code, errors.New("matching")), want: true},
			{name: "wrapped connect.Error with the matching code", err: fmt.Errorf("rpc failed: %w", connect.NewError(p.code, errors.New("matching"))), want: true},
			{name: "FleetError with CodeInternal", err: NewInternalError("internal error")},
			{name: "FleetError with CodeNotFound", err: NewNotFoundError("not found")},
			{name: "FleetError with CodeInvalidArgument", err: NewInvalidArgumentError("invalid scope")},
			{name: "connect.Error with CodeInternal", err: connect.NewError(connect.CodeInternal, errors.New("internal"))},
			{name: "connect.Error with CodeUnknown", err: connect.NewError(connect.CodeUnknown, errors.New("unknown"))},
			{name: "generic error", err: errors.New("something went wrong")},
		}
		for _, other := range predicates {
			if other.code == p.code {
				continue
			}
			rows = append(rows,
				row{name: "FleetError for " + other.name, err: other.fleetErr},
				row{name: "connect.Error for " + other.name, err: connect.NewError(other.code, errors.New("other"))},
			)
		}
		rows = append(rows, p.extra...)

		for _, tt := range rows {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, p.is(tt.err))
			})
		}
	}
}
