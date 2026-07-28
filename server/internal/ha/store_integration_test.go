package ha

import (
	"database/sql"
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
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	holder := uuid.New()

	ownership, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), holder, 2*time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, holder, ownership.HolderID)
	require.Equal(t, Token{WriterGeneration: 41, LeaseEpoch: 1}, ownership.Token)
	require.WithinDuration(t, ownership.DatabaseTime.Add(2*time.Second), ownership.ExpiresAt, 50*time.Millisecond)

	renewed, err := store.Renew(t.Context(), ownership, time.Second)
	require.NoError(t, err)
	require.Equal(t, ownership.Token, renewed.Token)
	require.WithinDuration(t, renewed.DatabaseTime.Add(time.Second), renewed.ExpiresAt, 50*time.Millisecond)
}

func TestRacingCoordinatorsProduceOneOwner(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	observer := staticObserver{observation: observation("cluster-a", 41)}
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
	require.Equal(t, 1, countMatchingErrors(errs, ErrLeaseHeld))
}

func TestPromotionAfterLostAsyncLeaseStateSupersedesUnexpiredLease(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	// This unexpired generation-41 row represents lease state acknowledged on
	// the former primary and then exposed again after asynchronous rollback.
	first, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	second, err := store.Acquire(
		t.Context(), observation("cluster-a", 42), uuid.New(), time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, int64(42), second.Token.WriterGeneration)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreRejectsClusterIdentityMismatch(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	_, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	_, err = store.Acquire(
		t.Context(), observation("cluster-b", 42), uuid.New(), time.Minute,
	)
	require.ErrorIs(t, err, ErrDCSClusterIdentityMismatch)
}

func TestLeaseStoreRejectsGenerationRegression(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	_, err := store.Acquire(
		t.Context(), observation("cluster-a", 42), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	_, err = store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Minute,
	)
	require.ErrorIs(t, err, ErrWriterGenerationRegression)
}

func TestLeaseStoreSameGenerationWaitsForExpiry(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	first, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	_, err = store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Second,
	)
	require.ErrorIs(t, err, ErrLeaseHeld)

	expireFleetRuntimeLease(t, db)
	second, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Second,
	)
	require.NoError(t, err)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreSameHolderAcquireIsIdempotentWhileUnexpired(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	holder := uuid.New()
	first, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), holder, time.Minute,
	)
	require.NoError(t, err)

	second, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), holder, 2*time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, first.Token, second.Token)
	require.Equal(t, holder, second.HolderID)
	require.WithinDuration(t, second.DatabaseTime.Add(2*time.Minute), second.ExpiresAt, 50*time.Millisecond)
}

func TestLeaseStoreSameHolderAfterExpiryAdvancesEpoch(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	holder := uuid.New()
	first, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), holder, time.Minute,
	)
	require.NoError(t, err)
	expireFleetRuntimeLease(t, db)

	second, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), holder, time.Minute,
	)
	require.NoError(t, err)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreRenewRequiresExactOwnership(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := NewLeaseStore(sqlc.New(db))
	ownership, err := store.Acquire(
		t.Context(), observation("cluster-a", 41), uuid.New(), time.Minute,
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
			_, err = store.Renew(t.Context(), wrong, time.Minute)
			require.True(t, errors.Is(err, ErrOwnershipLost))
		})
	}
}

func observation(clusterID string, generation int64) WriterObservation {
	return WriterObservation{
		DCSClusterID:     clusterID,
		WriterGeneration: generation,
		LeaderName:       "patroni-a",
	}
}

func expireFleetRuntimeLease(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(
		t.Context(),
		`UPDATE fleet_runtime_lease
		 SET expires_at = clock_timestamp() - INTERVAL '1 second'
		 WHERE lease_name = 'fleet-active'`,
	)
	require.NoError(t, err)
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
