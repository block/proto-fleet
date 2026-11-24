package pairing_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
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
	calls  int
	failAt int
	err    error
}

func (*fakeUpsertStore) LockDiscoveredDeviceReports(context.Context, int64, []fleetnodepairing.DiscoveredDeviceReport) error {
	return nil
}

func (s *fakeUpsertStore) UpsertDiscoveredDeviceFromFleetNode(_ context.Context, _ int64, _ int64, report fleetnodepairing.DiscoveredDeviceReport) (fleetnodepairing.DiscoveredDeviceReport, bool, error) {
	s.calls++
	if s.calls == s.failAt {
		return report, false, s.err
	}
	if report.DeviceIdentifier == "accept" {
		report.DeviceIdentifier = "canonical-accept"
		report.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_CONTAINER
		return report, true, nil
	}
	return report, false, nil
}

type retryPostgresTransactor struct {
	retryOnceTransactor
	t   *testing.T
	err error
}

func (tx retryPostgresTransactor) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	err := fn(ctx)
	require.ErrorIs(tx.t, err, tx.err)
	require.True(tx.t, db.IsRetryablePostgresError(err), "the batch must preserve transient errors for retry")
	return fn(ctx)
}

func TestUpsertDiscoveredDevicesPreservesRetryableDatabaseErrors(t *testing.T) {
	for _, code := range []string{db.PGSerializationFailure, db.PGDeadlockDetected} {
		t.Run(code, func(t *testing.T) {
			retryable := &pgconn.PgError{Code: code}
			store := &fakeUpsertStore{failAt: 2, err: retryable}
			svc := fleetnodepairing.NewService(store, nil, retryPostgresTransactor{t: t, err: retryable})
			reports := []fleetnodepairing.DiscoveredDeviceReport{
				{DeviceIdentifier: "accept", IPAddress: "10.0.0.1", Port: "80", URLScheme: "http"},
				{DeviceIdentifier: "reject", IPAddress: "10.0.0.2", Port: "80", URLScheme: "http"},
			}

			accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), 1, 1, reports)

			require.NoError(t, err)
			require.Len(t, accepted, 1)
			require.Equal(t, "canonical-accept", accepted[0].DeviceIdentifier)
			require.EqualValues(t, 1, rejected)
		})
	}
}

func TestUpsertDiscoveredDevices_CountsAreNotDoubledOnTxRetry(t *testing.T) {
	// Arrange: one accepted report, one ownership conflict, and invalid endpoints, run
	// through a transactor that retries the closure once before committing.
	store := &fakeUpsertStore{}
	svc := fleetnodepairing.NewService(store, nil, retryOnceTransactor{})
	reports := []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "accept", IPAddress: "10.0.0.1", Port: "80", URLScheme: "http"},
		{DeviceIdentifier: "reject", IPAddress: "10.0.0.2", Port: "80", URLScheme: "http"},
		{DeviceIdentifier: "malformed", IPAddress: "10.0.0.3", Port: "80", URLScheme: "http", Endpoints: []*commonpb.DeviceEndpoint{nil}},
	}

	// Act
	accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), 1, 1, reports)

	// Assert: counts reflect a single committed pass, not the rolled-back one.
	require.NoError(t, err)
	require.Len(t, accepted, 1, "accepted reports must not accumulate across retries")
	assert.Equal(t, "canonical-accept", accepted[0].DeviceIdentifier)
	assert.Equal(t, "accept", reports[0].DeviceIdentifier, "transaction attempts must not mutate the input")
	assert.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, accepted[0].DeviceClass)
	assert.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, reports[0].DeviceClass, "transaction attempts must not mutate the input")
	assert.Equal(t, int64(2), rejected, "rejected count must not accumulate across retries")
	assert.Equal(t, 4, store.calls, "invalid endpoints must not reach the store on either attempt")
}
