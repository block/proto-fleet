package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestDispatchBudgetLeavesPoolHeadroomUntilCanceledAdapterReturns(t *testing.T) {
	conn, _ := newMigrationBridgeTestDB(t)
	conn.SetMaxOpenConns(4)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	callCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- WithDispatchTransaction(callCtx, conn, func(sqlc.Querier) error {
				entered <- struct{}{}
				<-release
				return nil
			})
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("dispatch did not enter")
		}
	}
	cancel()
	// A canceled caller cannot release an adapter still doing physical I/O.
	waitCtx, cancelWait := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancelWait()
	invoked := false
	err := WithDispatchTransaction(waitCtx, conn, func(sqlc.Querier) error { invoked = true; return nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, invoked)
	require.Equal(t, 2, conn.Stats().InUse, "budget wait must not consume the reserved connections")
	queryCtx, cancelQuery := context.WithTimeout(t.Context(), time.Second)
	defer cancelQuery()
	require.NoError(t, conn.PingContext(queryCtx), "auth and completion queries retain pool capacity")
	finish()
	for range 2 {
		require.NoError(t, <-done)
	}
	require.NoError(t, WithDispatchTransaction(t.Context(), conn, func(sqlc.Querier) error { return nil }))
}

func TestDispatchBudgetConfiguredLimitAndPoolResize(t *testing.T) {
	conn, _ := newMigrationBridgeTestDB(t)
	conn.SetMaxOpenConns(4)
	// Replace the test fixture's default registration before its first dispatch.
	dispatchBudgets.Store(conn, &dispatchBudget{configuredLimit: 1})
	release, err := acquireDispatchBudget(t.Context(), conn)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = acquireDispatchBudget(ctx, conn)
	require.True(t, errors.Is(err, context.Canceled))
	require.Zero(t, conn.Stats().InUse)
	release()
	conn.SetMaxOpenConns(1)
	require.Error(t, WithDispatchTransaction(t.Context(), conn, func(sqlc.Querier) error { t.Fatal("unsafe pool must not dispatch"); return nil }))
}

func TestDispatchBudgetConfigurationValidation(t *testing.T) {
	for _, limit := range []int{-1, 4, 5} {
		require.Error(t, (&Config{Address: "localhost:5432", Name: "fleet", MaxOpenConns: 4, MaxDispatchConns: limit}).Validate())
	}
	require.NoError(t, (&Config{Address: "localhost:5432", Name: "fleet", MaxOpenConns: 4, MaxDispatchConns: 2}).Validate())
}
