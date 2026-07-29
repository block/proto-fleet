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

func (a fakeAdmission) Admit(ctx context.Context) (context.Context, func(), error) {
	if a.err != nil {
		return nil, nil, a.err
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	return requestCtx, func() {
		stop()
		cancel()
	}, nil
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

func TestActiveInterceptorPreservesHandlerResultAfterDemotion(t *testing.T) {
	t.Run("unary response", func(t *testing.T) {
		activeCtx, cancelActive := context.WithCancel(t.Context())
		interceptor := NewActiveInterceptor(fakeAdmission{ctx: activeCtx})
		wantResponse := connect.NewResponse(&commonpb.ResourceRef{})
		wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			cancelActive()
			<-ctx.Done()
			return wantResponse, nil
		})

		response, err := wrapped(t.Context(), connect.NewRequest(&commonpb.ResourceRef{}))

		require.NoError(t, err)
		require.Same(t, wantResponse, response)
	})

	t.Run("streaming cancellation", func(t *testing.T) {
		activeCtx, cancelActive := context.WithCancel(t.Context())
		interceptor := NewActiveInterceptor(fakeAdmission{ctx: activeCtx})
		wrapped := interceptor.WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
			cancelActive()
			<-ctx.Done()
			return ctx.Err()
		})

		err := wrapped(t.Context(), nil)

		require.ErrorIs(t, err, context.Canceled)
	})
}
