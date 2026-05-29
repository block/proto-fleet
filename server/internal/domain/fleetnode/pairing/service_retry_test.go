package pairing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
)

// retryOnceTransactor models the production retry path: WithTransaction
// re-runs the closure after a retryable Postgres or commit failure. The first
// attempt's work is rolled back, the second commits. We run the closure twice
// and return only the second result so any state the closure leaks across
// attempts surfaces as double-counting.
type retryOnceTransactor struct{}

func (retryOnceTransactor) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	_ = fn(ctx) // rolled-back attempt
	return fn(ctx)
}

func (retryOnceTransactor) RunInTxWithResult(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error) {
	_, _ = fn(ctx)
	return fn(ctx)
}

// fakeUpsertStore accepts reports whose identifier starts with "accept" (rows
// affected = 1) and treats everything else as an ownership rejection (rows
// affected = 0), so a single batch exercises both counters.
type fakeUpsertStore struct {
	fleetnodepairing.Store
	calls int
}

func (s *fakeUpsertStore) UpsertDiscoveredDeviceFromFleetNode(_ context.Context, _ int64, _ int64, report fleetnodepairing.DiscoveredDeviceReport) (int64, error) {
	s.calls++
	if report.DeviceIdentifier == "accept" {
		return 1, nil
	}
	return 0, nil
}

func TestUpsertDiscoveredDevices_CountsAreNotDoubledOnTxRetry(t *testing.T) {
	// Arrange: one accepted report and one ownership-rejected report, run
	// through a transactor that retries the closure once before committing.
	store := &fakeUpsertStore{}
	svc := fleetnodepairing.NewService(store, nil, retryOnceTransactor{})
	reports := []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "accept", IPAddress: "10.0.0.1", Port: "80", URLScheme: "http"},
		{DeviceIdentifier: "reject", IPAddress: "10.0.0.2", Port: "80", URLScheme: "http"},
	}

	// Act
	acceptedIdx, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), 1, 1, reports)

	// Assert: counts reflect a single committed pass, not the rolled-back one.
	require.NoError(t, err)
	assert.Equal(t, []int{0}, acceptedIdx, "accepted indices must not accumulate across retries")
	assert.Equal(t, int64(1), rejected, "rejected count must not accumulate across retries")
}
