package interceptors

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapErrorPreservesConnectErrors(t *testing.T) {
	validationErr := connect.NewError(connect.CodeInvalidArgument, errors.New("invalid priority"))

	err := mapError(validationErr)

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Same(t, validationErr, connectErr)
}

func TestMapErrorConvertsFleetErrors(t *testing.T) {
	err := mapError(fleeterror.NewUnimplementedError("not ready"))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
}

func TestMapErrorConvertsGenericErrorsToInternal(t *testing.T) {
	err := mapError(errors.New("plain failure"))

	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}
