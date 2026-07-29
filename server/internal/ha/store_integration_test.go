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
	observed := databaseObservation(t, reader)

	ownership, err := store.Acquire(
		t.Context(), observed, holder, 2*time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, holder, ownership.HolderID)
	require.Equal(
		t,
		Token{WriterGeneration: observed.WriterGeneration, LeaseEpoch: 1},
		ownership.Token,
	)
	require.WithinDuration(t, ownership.DatabaseTime.Add(2*time.Second), ownership.ExpiresAt, 50*time.Millisecond)

	renewed, err := store.Renew(
		t.Context(),
		observed,
		ownership,
		time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, ownership.Token, renewed.Token)
	require.WithinDuration(t, renewed.DatabaseTime.Add(time.Second), renewed.ExpiresAt, 50*time.Millisecond)
}

func TestRacingCoordinatorsProduceOneOwner(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observer := staticObserver{observation: databaseObservation(t, reader)}
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

func TestLeaseStoreCoordinatorDemotionRequiresExpiryBeforeSameWriterReacquisition(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader)
	observer := &sequenceObserver{
		results: []observerResult{
			{observation: observed},
			{err: errors.New("writer unavailable")},
			{observation: observed},
			{observation: observed},
		},
	}
	config := coordinatorTestConfig()
	config.LeaseDuration = shortIntegrationLeaseDuration
	coordinator := newCoordinatorWithHolder(observer, store, config, uuid.New())

	require.NoError(t, coordinator.step(t.Context()))
	first := coordinator.Snapshot()
	firstHolder := first.HolderID

	require.Error(t, coordinator.step(t.Context()))
	require.Equal(t, StatePassive, coordinator.Snapshot().State)
	require.NotEqual(t, firstHolder, coordinator.HolderID())

	require.ErrorIs(t, coordinator.step(t.Context()), ErrLeaseUnavailable)

	waitForLeaseExpiry(t, Ownership{
		DatabaseTime: first.ExpiresAt.Add(-config.LeaseDuration),
		ExpiresAt:    first.ExpiresAt,
	})
	require.NoError(t, coordinator.step(t.Context()))
	second := coordinator.Snapshot()
	require.Equal(t, StateActive, second.State)
	require.Greater(t, second.Token.LeaseEpoch, first.Token.LeaseEpoch)
}

func TestLeaseStoreRejectsPostgresSystemIdentifierMismatch(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader)
	observed.PostgresSystemIdentifier += "-different"

	_, err := store.Acquire(t.Context(), observed, uuid.New(), time.Minute)
	require.ErrorIs(t, err, ErrLeaseUnavailable)
}

func TestLeaseStoreSameGenerationWaitsForExpiry(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader)
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

func TestLeaseStoreSameHolderAcquireDoesNotExtendUnexpiredLease(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	holder := uuid.New()
	first, err := store.Acquire(
		t.Context(), databaseObservation(t, reader), holder, time.Minute,
	)
	require.NoError(t, err)

	second, err := store.Acquire(
		t.Context(), databaseObservation(t, reader), holder, 2*time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, first.Token, second.Token)
	require.Equal(t, holder, second.HolderID)
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
}

func TestLeaseStoreSameHolderAfterExpiryAdvancesEpoch(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader)
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
		t.Context(), databaseObservation(t, reader), uuid.New(), time.Minute,
	)
	require.NoError(t, err)

	tests := map[string]func(*Ownership){
		"holder": func(wrong *Ownership) { wrong.HolderID = uuid.New() },
		"epoch":  func(wrong *Ownership) { wrong.Token.LeaseEpoch++ },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			wrong := ownership
			mutate(&wrong)
			_, renewErr := store.Renew(
				t.Context(),
				databaseObservation(t, reader),
				wrong,
				time.Minute,
			)
			require.ErrorIs(t, renewErr, ErrOwnershipLost)
		})
	}
}

func TestLeaseStoreRejectsDifferentWritableServerIdentity(t *testing.T) {
	store, reader := leaseTestSurfaces(t)
	observed := databaseObservation(t, reader)
	observed.ServerAddress = "203.0.113.1"

	_, err := store.Acquire(t.Context(), observed, uuid.New(), time.Minute)
	require.ErrorIs(t, err, ErrLeaseUnavailable)

	ownership, err := store.Acquire(
		t.Context(),
		databaseObservation(t, reader),
		uuid.New(),
		time.Minute,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), ownership.Token.LeaseEpoch)
}

func databaseObservation(
	t *testing.T,
	reader postgresIdentityReader,
) WriterObservation {
	t.Helper()
	identity, err := reader.GetConnectedPostgresIdentity(t.Context())
	require.NoError(t, err)
	return WriterObservation{
		PostgresSystemIdentifier: identity.SystemIdentifier,
		WriterGeneration:         identity.Timeline,
		ServerAddress:            identity.ServerAddress,
		ServerPort:               identity.ServerPort,
	}
}

func leaseTestSurfaces(t *testing.T) (*LeaseStore, postgresIdentityReader) {
	t.Helper()
	queries := sqlc.New(testutil.GetTestDB(t))
	return NewLeaseStore(queries), queries
}

func observation(systemIdentifier string, generation int64) WriterObservation {
	return WriterObservation{
		PostgresSystemIdentifier: systemIdentifier,
		WriterGeneration:         generation,
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
		if errors.Is(err, target) {
			count++
		}
	}
	return count
}
