package ha

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestLeaseStoreAcquireAndRenewUseDatabaseTime(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	holder := uuid.New()

	ownership, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), holder, 2*time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, holder, ownership.HolderID)
	require.Equal(t, Token{WriterGeneration: 41, LeaseEpoch: 1}, ownership.Token)
	require.WithinDuration(t, ownership.DatabaseTime.Add(2*time.Second), ownership.ExpiresAt, 50*time.Millisecond)

	renewed, err := store.Renew(
		t.Context(),
		databaseObservation(t, reader, "cluster-a", 41),
		ownership,
		time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, ownership.Token, renewed.Token)
	require.WithinDuration(t, renewed.DatabaseTime.Add(time.Second), renewed.ExpiresAt, 50*time.Millisecond)
}

func TestRacingCoordinatorsProduceOneOwner(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observer := staticObserver{observation: databaseObservation(t, reader, "cluster-a", 41)}
	coordinators := []*Coordinator{
		newCoordinatorWithHolder(observer, store, coordinatorTestConfig(), uuid.New()),
		newCoordinatorWithHolder(observer, store, coordinatorTestConfig(), uuid.New()),
	}

	var wait sync.WaitGroup
	errs := make([]error, len(coordinators))
	wait.Add(len(coordinators))
	for index, coordinator := range coordinators {
		go func() {
			defer wait.Done()
			errs[index] = coordinator.step(t.Context())
		}()
	}
	wait.Wait()

	active := 0
	for _, coordinator := range coordinators {
		if coordinator.Snapshot().State == StateActive {
			active++
		}
	}
	require.Equal(t, 1, active)
	require.Equal(t, 1, countMatchingErrors(errs, nil))
	require.Equal(t, 1, countMatchingErrors(errs, ErrLeaseUnavailable))
}

func TestPromotionAfterLostAsyncLeaseStateSupersedesUnexpiredLease(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	// This unexpired generation-41 row represents lease state acknowledged on
	// the former primary and then exposed again after asynchronous rollback.
	first, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	second, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 42), uuid.New(), time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, int64(42), second.Token.WriterGeneration)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreRejectsClusterIdentityMismatch(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	_, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	_, err = store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-b", 42), uuid.New(), time.Minute,
	)
	require.ErrorIs(t, err, ErrLeaseUnavailable)
}

func TestLeaseStoreRejectsGenerationRegression(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	_, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 42), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	_, err = store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), uuid.New(), time.Minute,
	)
	require.ErrorIs(t, err, ErrLeaseUnavailable)
}

func TestLeaseStoreSameGenerationWaitsForExpiry(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader, "cluster-a", 41)
	first, err := store.Acquire(
		t.Context(),
		observed,
		uuid.New(),
		shortIntegrationLeaseDuration,
	)
	require.NoError(t, err)

	_, err = store.Acquire(t.Context(), observed, uuid.New(), time.Second)
	require.ErrorIs(t, err, ErrLeaseUnavailable)

	waitForLeaseExpiry(t, first)
	second, err := store.Acquire(t.Context(), observed, uuid.New(), time.Second)
	require.NoError(t, err)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreSameHolderAcquireIsIdempotentWhileUnexpired(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	holder := uuid.New()
	first, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), holder, time.Minute,
	)
	require.NoError(t, err)

	second, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), holder, 2*time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, first.Token, second.Token)
	require.Equal(t, holder, second.HolderID)
	require.WithinDuration(t, second.DatabaseTime.Add(2*time.Minute), second.ExpiresAt, 50*time.Millisecond)
}

func TestLeaseStoreSameHolderAfterExpiryAdvancesEpoch(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader, "cluster-a", 41)
	holder := uuid.New()
	first, err := store.Acquire(
		t.Context(),
		observed,
		holder,
		shortIntegrationLeaseDuration,
	)
	require.NoError(t, err)
	waitForLeaseExpiry(t, first)

	second, err := store.Acquire(t.Context(), observed, holder, time.Minute)
	require.NoError(t, err)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreRenewRequiresExactOwnership(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	ownership, err := store.Acquire(
		t.Context(), databaseObservation(t, reader, "cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	tests := map[string]func(*Ownership){
		"holder":     func(wrong *Ownership) { wrong.HolderID = uuid.New() },
		"generation": func(wrong *Ownership) { wrong.Token.WriterGeneration++ },
		"epoch":      func(wrong *Ownership) { wrong.Token.LeaseEpoch++ },
		"cluster":    func(wrong *Ownership) { wrong.DCSClusterID = "cluster-b" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			wrong := ownership
			mutate(&wrong)
			_, renewErr := store.Renew(
				t.Context(),
				databaseObservation(
					t,
					reader,
					wrong.DCSClusterID,
					wrong.Token.WriterGeneration,
				),
				wrong,
				time.Minute,
			)
			require.ErrorIs(t, renewErr, ErrOwnershipLost)
		})
	}
}

func TestLeaseStoreRejectsDifferentWritableServerIdentity(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader, "cluster-a", 41)
	observed.ServerAddress = "203.0.113.1"

	_, err := store.Acquire(t.Context(), observed, uuid.New(), time.Minute)
	require.ErrorIs(t, err, ErrLeaseUnavailable)

	ownership, err := store.Acquire(
		t.Context(),
		databaseObservation(t, reader, "cluster-a", 41),
		uuid.New(),
		time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, Token{WriterGeneration: 41, LeaseEpoch: 1}, ownership.Token)
}

func TestLeaseStoreRejectsRenewalOnDifferentWritableServerIdentity(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader, "cluster-a", 41)
	ownership, err := store.Acquire(t.Context(), observed, uuid.New(), time.Minute)
	require.NoError(t, err)
	observed.Timeline++

	_, err = store.Renew(t.Context(), observed, ownership, time.Minute)
	require.ErrorIs(t, err, ErrOwnershipLost)
}

func databaseObservation(
	t *testing.T,
	reader *SQLWriterIdentityReader,
	clusterID string,
	generation int64,
) WriterObservation {
	t.Helper()
	identity, err := reader.WritableIdentity(t.Context())
	require.NoError(t, err)
	return WriterObservation{
		DCSClusterID:     clusterID,
		WriterGeneration: generation,
		LeaderName:       "patroni-a",
		ServerAddress:    identity.ServerAddress,
		ServerPort:       identity.ServerPort,
		Timeline:         identity.Timeline,
		DCSProofDeadline: time.Now().Add(time.Minute),
	}
}

func leaseTestSurfaces(t *testing.T) (*LeaseStore, *SQLWriterIdentityReader) {
	t.Helper()
	queries := sqlc.New(testutil.GetTestDB(t))
	return NewLeaseStore(queries), NewSQLWriterIdentityReader(queries)
}

func observation(clusterID string, generation int64) WriterObservation {
	return WriterObservation{
		DCSClusterID:     clusterID,
		WriterGeneration: generation,
		LeaderName:       "patroni-a",
	}
}

const shortIntegrationLeaseDuration = 250 * time.Millisecond

func waitForLeaseExpiry(t *testing.T, ownership Ownership) {
	t.Helper()
	const expiryMargin = 50 * time.Millisecond
	wait := ownership.ExpiresAt.Sub(ownership.DatabaseTime) + expiryMargin
	require.Positive(t, wait)
	require.LessOrEqual(t, wait, time.Second)
	time.Sleep(wait)
}

func countMatchingErrors(errs []error, target error) int {
	count := 0
	for _, err := range errs {
		if target == nil && err == nil || target != nil && errors.Is(err, target) {
			count++
		}
	}
	return count
}
