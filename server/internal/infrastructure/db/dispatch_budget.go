package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"
)

// All dispatchers sharing a pool share its budget. Waiting for admission does
// not lease a connection, leaving capacity for authentication and completion.
var dispatchBudgets sync.Map

type dispatchBudget struct {
	mu              sync.Mutex
	configuredLimit int
	capacity        int
	slots           *semaphore.Weighted
}

func registerDispatchBudget(database *sql.DB, limit int) {
	dispatchBudgets.LoadOrStore(database, &dispatchBudget{configuredLimit: limit})
}

func acquireDispatchBudget(ctx context.Context, database *sql.DB) (func(), error) {
	value, _ := dispatchBudgets.LoadOrStore(database, &dispatchBudget{})
	budget, ok := value.(*dispatchBudget)
	if !ok {
		return nil, fmt.Errorf("invalid registered dispatch budget")
	}
	maxOpen := database.Stats().MaxOpenConnections
	budget.mu.Lock()
	if maxOpen == 1 {
		budget.mu.Unlock()
		return nil, fmt.Errorf("physical dispatch requires at least two database connections")
	}
	if budget.slots == nil {
		capacity := budget.configuredLimit
		if capacity == 0 {
			capacity = 125 // Same default as half the standard pool for an unbounded pool.
			if maxOpen > 0 {
				capacity = maxOpen / 2
			}
		}
		budget.capacity = capacity
		budget.slots = semaphore.NewWeighted(int64(capacity))
	}
	if maxOpen > 0 && budget.capacity >= maxOpen {
		budget.mu.Unlock()
		return nil, fmt.Errorf("dispatch budget must be below the current database pool capacity")
	}
	slots := budget.slots
	budget.mu.Unlock()
	if err := slots.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("wait for dispatch slot: %w", err)
	}
	// Lifetime follows the adapter call, not the caller's context: canceled I/O
	// can continue holding binding locks until its backend actually returns.
	return func() { slots.Release(1) }, nil
}
