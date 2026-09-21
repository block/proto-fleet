package pairing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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

// fakeUpsertStore accepts the "accept" identifier and rejects all others,
// so a single batch exercises both counters.
type fakeUpsertStore struct {
	fleetnodepairing.Store
	calls int
}

func (s *fakeUpsertStore) UpsertDiscoveredDeviceFromFleetNode(_ context.Context, _ int64, _ int64, report fleetnodepairing.DiscoveredDeviceReport) (commonpb.DeviceClass, bool, error) {
	s.calls++
	if report.DeviceIdentifier == "accept" {
		return commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, true, nil
	}
	return commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, false, nil
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
	accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), 1, 1, reports)

	// Assert: counts reflect a single committed pass, not the rolled-back one.
	require.NoError(t, err)
	require.Len(t, accepted, 1, "accepted reports must not accumulate across retries")
	assert.Equal(t, "accept", accepted[0].DeviceIdentifier)
	assert.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, accepted[0].DeviceClass)
	assert.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, reports[0].DeviceClass, "transaction attempts must not mutate the input")
	assert.Equal(t, int64(1), rejected, "rejected count must not accumulate across retries")
}
