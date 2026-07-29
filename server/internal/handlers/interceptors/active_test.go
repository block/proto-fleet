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

func TestActiveInterceptorMapsOnlyActiveLifetimeCancellation(t *testing.T) {
	handlerErr := fleeterror.NewInternalError("handler returned after cancellation")
	wrappers := []struct {
		name   string
		invoke func(*ActiveInterceptor, context.Context, func(context.Context) error) error
	}{
		{
			name: "unary",
			invoke: func(interceptor *ActiveInterceptor, ctx context.Context, handler func(context.Context) error) error {
				wrapped := interceptor.WrapUnary(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(&commonpb.ResourceRef{}), handler(ctx)
				})
				_, err := wrapped(ctx, connect.NewRequest(&commonpb.ResourceRef{}))
				return err
			},
		},
		{
			name: "streaming",
			invoke: func(interceptor *ActiveInterceptor, ctx context.Context, handler func(context.Context) error) error {
				return interceptor.WrapStreamingHandler(
					func(ctx context.Context, _ connect.StreamingHandlerConn) error {
						return handler(ctx)
					},
				)(ctx, nil)
			},
		},
	}
	scenarios := []struct {
		name        string
		cancel      func(context.CancelFunc, context.CancelFunc)
		handlerErr  error
		wantPassive bool
	}{
		{
			name:        "demotion",
			cancel:      func(_ context.CancelFunc, cancelActive context.CancelFunc) { cancelActive() },
			handlerErr:  handlerErr,
			wantPassive: true,
		},
		{
			name:        "demotion after handler success",
			cancel:      func(_ context.CancelFunc, cancelActive context.CancelFunc) { cancelActive() },
			wantPassive: true,
		},
		{
			name:       "client cancellation",
			cancel:     func(cancelClient context.CancelFunc, _ context.CancelFunc) { cancelClient() },
			handlerErr: handlerErr,
		},
	}

	for _, wrapper := range wrappers {
		for _, scenario := range scenarios {
			t.Run(wrapper.name+"/"+scenario.name, func(t *testing.T) {
				clientCtx, cancelClient := context.WithCancel(t.Context())
				activeCtx, cancelActive := context.WithCancel(t.Context())
				interceptor := NewActiveInterceptor(fakeAdmission{ctx: activeCtx})

				err := wrapper.invoke(interceptor, clientCtx, func(ctx context.Context) error {
					scenario.cancel(cancelClient, cancelActive)
					<-ctx.Done()
					return scenario.handlerErr
				})

				if scenario.wantPassive {
					requireNotActiveError(t, err)
				} else {
					require.Equal(t, scenario.handlerErr, err)
				}
			})
		}
	}
}
