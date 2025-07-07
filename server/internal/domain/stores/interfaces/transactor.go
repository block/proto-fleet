package interfaces

import (
	"context"
)

// Transactor is a wrapper for stores that allows for transactions.
// It hides transaction boundaries and ensures stores use the right queries handle.
type Transactor interface {
	// RunInTx executes the provided function within a transaction
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error

	// RunInTxWithResult executes the provided function within a transaction
	RunInTxWithResult(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error)
}
