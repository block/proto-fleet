package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type admissionGate interface {
	Admit(ctx context.Context) (context.Context, func(), error)
}

type ActiveInterceptor struct {
	gate admissionGate
}

var _ connect.Interceptor = (*ActiveInterceptor)(nil)

func NewActiveInterceptor(gate admissionGate) *ActiveInterceptor {
	return &ActiveInterceptor{gate: gate}
}

func (i *ActiveInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		activeCtx, release, err := i.gate.Admit(ctx)
		if err != nil {
			return nil, fleeterror.NewNotActiveError()
		}
		defer release()
		response, err := next(activeCtx, request)
		return response, mapActiveLifetimeCancellation(ctx, activeCtx, err)
	}
}

func (i *ActiveInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *ActiveInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		activeCtx, release, err := i.gate.Admit(ctx)
		if err != nil {
			return fleeterror.NewNotActiveError()
		}
		defer release()
		return mapActiveLifetimeCancellation(ctx, activeCtx, next(activeCtx, conn))
	}
}

func mapActiveLifetimeCancellation(requestCtx, activeCtx context.Context, err error) error {
	if err == nil || requestCtx.Err() != nil || activeCtx.Err() == nil {
		return err
	}
	return fleeterror.NewNotActiveError()
}
