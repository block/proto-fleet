package interceptors

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

type fakeAdmission struct {
	ctx context.Context //nolint:containedctx // Supplies the admitted lifetime to the interceptor.
	err error
}

func (a fakeAdmission) Admit(context.Context) (context.Context, func(), error) {
	if a.err != nil {
		return nil, nil, a.err
	}
	return a.ctx, func() {}, nil
}

func TestActiveInterceptorReturnsMachineReadableNotActive(t *testing.T) {
	interceptor := NewActiveInterceptor(fakeAdmission{err: errors.New("not active")})
	nextCalled := false
	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		nextCalled = true
		return connect.NewResponse(&commonpb.ResourceRef{}), nil
	})

	_, err := wrapped(t.Context(), connect.NewRequest(&commonpb.ResourceRef{}))

	require.False(t, nextCalled)
	requireNotActiveError(t, err)
}

func requireNotActiveError(t *testing.T, err error) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	require.Equal(t, commonpb.FleetErrorCode_FLEET_ERROR_CODE_NOT_ACTIVE, commonpb.FleetErrorCode(fleetErr.FleetErrorCode))
	require.Equal(t, connect.CodeUnavailable, fleetErr.GRPCCode)
}

func TestActiveInterceptorBindsRequestToActiveLifetime(t *testing.T) {
	activeCtx, cancelActive := context.WithCancel(t.Context())
	interceptor := NewActiveInterceptor(fakeAdmission{ctx: activeCtx})
	handlerStarted := make(chan struct{})
	wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		close(handlerStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() {
		_, err := wrapped(t.Context(), connect.NewRequest(&commonpb.ResourceRef{}))
		result <- err
	}()

	<-handlerStarted
	cancelActive()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestActiveInterceptorRejectsPassiveStream(t *testing.T) {
	interceptor := NewActiveInterceptor(fakeAdmission{err: errors.New("not active")})
	nextCalled := false
	wrapped := interceptor.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error {
		nextCalled = true
		return nil
	})

	err := wrapped(t.Context(), nil)

	require.False(t, nextCalled)
	requireNotActiveError(t, err)
}

func TestActiveInterceptorCancelsAdmittedStreamOnDemotion(t *testing.T) {
	activeCtx, cancelActive := context.WithCancel(t.Context())
	interceptor := NewActiveInterceptor(fakeAdmission{ctx: activeCtx})
	handlerStarted := make(chan struct{})
	wrapped := interceptor.WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		close(handlerStarted)
		<-ctx.Done()
		return ctx.Err()
	})
	result := make(chan error, 1)
	go func() {
		result <- wrapped(t.Context(), nil)
	}()

	<-handlerStarted
	cancelActive()
	require.ErrorIs(t, <-result, context.Canceled)
}
