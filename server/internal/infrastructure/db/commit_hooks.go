package db

import "context"

type commitHooksKey struct{}

// WithCommitHooks creates callbacks for one transaction attempt. Run commit
// only after that attempt commits; discard it on rollback or before retrying.
// Callbacks retain request context values but must not reuse committed queries.
func WithCommitHooks(ctx context.Context) (txCtx context.Context, commit func()) {
	hooks := []func(){}
	return context.WithValue(ctx, commitHooksKey{}, &hooks), func() {
		for _, hook := range hooks {
			hook()
		}
	}
}

// HasCommitHooks reports whether the transaction owner supports post-commit
// work. A command must check this before joining an ambient transaction.
func HasCommitHooks(ctx context.Context) bool {
	_, ok := ctx.Value(commitHooksKey{}).(*[]func())
	return ok
}

// AfterCommit registers work without running it inside the transaction.
// It returns false when no transaction owner installed a callback scope.
func AfterCommit(ctx context.Context, fn func()) bool {
	hooks, ok := ctx.Value(commitHooksKey{}).(*[]func())
	if ok {
		*hooks = append(*hooks, fn)
	}
	return ok
}

// WithoutTransaction retains authentication and cancellation while removing
// transaction-bound queries and callbacks from post-commit work.
func WithoutTransaction(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, txContextKey{}, struct{}{})
	return context.WithValue(ctx, commitHooksKey{}, struct{}{})
}
