// Package admissionctx carries the active-runtime lifetime associated with an
// admitted request independently from the request's own cancellation.
package admissionctx

import "context"

type activeLifetimeKey struct{}

// WithActiveLifetime binds ctx to the exact runtime activation that admitted
// it. The lifetime must not be replaced with a later activation.
func WithActiveLifetime(ctx, lifetime context.Context) context.Context {
	return context.WithValue(ctx, activeLifetimeKey{}, lifetime)
}

// ActiveLifetime returns the runtime activation that admitted ctx.
func ActiveLifetime(ctx context.Context) (context.Context, bool) {
	lifetime, ok := ctx.Value(activeLifetimeKey{}).(context.Context)
	return lifetime, ok && lifetime != nil
}

// DetachRequestCancellation returns a context that retains ctx's values and
// the active-runtime cancellation signal while ignoring caller cancellation
// and deadlines. The boolean is false when ctx did not pass through the active
// admission gate; privileged work must fail closed in that case.
func DetachRequestCancellation(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	lifetime, ok := ActiveLifetime(ctx)
	if !ok {
		return nil, nil, false
	}

	detached, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	return detached, func() {
		stop()
		cancel()
	}, true
}
