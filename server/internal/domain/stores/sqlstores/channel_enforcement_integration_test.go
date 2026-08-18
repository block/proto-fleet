package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	channel "github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestChannelEnforcementStoreAuthorityRevisionAndHaltGateClaims(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "enforcement-gate")
	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	deviceID, targetID := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)
	authority, err := enforcementStore.CreateAuthority(t.Context(), channel.CreateAuthorityParams{
		ID:              uuid.New(),
		OrgID:           orgID,
		Type:            "rollout",
		Reference:       "rollout-enforcement-gate",
		CreatedByUserID: testOrganizationUserID(t, db, orgID),
	})
	require.NoError(t, err)
	enforcement, err := enforcementStore.CreateEnforcement(t.Context(), channel.CreateEnforcementParams{
		OrgID:             orgID,
		DeviceID:          deviceID,
		ReleaseTargetID:   targetID,
		CauseType:         "rollout_admission",
		AuthorityID:       authority.ID,
		AuthorityRevision: authority.Revision,
	})
	require.NoError(t, err)

	advanced, err := enforcementStore.AdvanceAuthorityRevision(
		t.Context(),
		authority.ID,
		orgID,
		authority.Revision,
	)
	require.NoError(t, err)
	assert.Equal(t, authority.Revision+1, advanced.Revision)
	_, err = enforcementStore.Claim(
		t.Context(),
		enforcement,
		"batch-stale-authority",
		time.Now(),
	)
	require.ErrorIs(t, err, channel.ErrCASConflict)

	halted, err := enforcementStore.HaltAuthority(
		t.Context(),
		authority.ID,
		orgID,
		advanced.Revision,
	)
	require.NoError(t, err)
	require.NotNil(t, halted.HaltedAt)
	_, err = enforcementStore.Claim(
		t.Context(),
		enforcement,
		"batch-after-halt",
		time.Now(),
	)
	require.ErrorIs(t, err, channel.ErrCASConflict)
}

func TestChannelEnforcementStorePreHaltClaimRemainsCommitted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "pre-halt")
	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	deviceID, targetID := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)
	authority, err := enforcementStore.CreateAuthority(t.Context(), channel.CreateAuthorityParams{
		ID:              uuid.New(),
		OrgID:           orgID,
		Type:            "rollout",
		Reference:       "rollout-pre-halt",
		CreatedByUserID: testOrganizationUserID(t, db, orgID),
	})
	require.NoError(t, err)
	enforcement, err := enforcementStore.CreateEnforcement(t.Context(), channel.CreateEnforcementParams{
		OrgID:             orgID,
		DeviceID:          deviceID,
		ReleaseTargetID:   targetID,
		CauseType:         "rollout_admission",
		AuthorityID:       authority.ID,
		AuthorityRevision: authority.Revision,
	})
	require.NoError(t, err)

	claimed, err := enforcementStore.Claim(
		t.Context(),
		enforcement,
		"batch-before-halt",
		time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, channel.EnforcementStateDispatching, claimed.State)
	assert.Equal(t, "batch-before-halt", claimed.CommandBatchUUID)

	_, err = enforcementStore.HaltAuthority(
		t.Context(),
		authority.ID,
		orgID,
		authority.Revision,
	)
	require.NoError(t, err)
	persisted, err := enforcementStore.GetEnforcement(t.Context(), enforcement.ID)
	require.NoError(t, err)
	assert.Equal(t, channel.EnforcementStateDispatching, persisted.State)
	assert.Equal(t, "batch-before-halt", persisted.CommandBatchUUID)

	_, err = enforcementStore.Claim(
		t.Context(),
		persisted,
		"batch-after-halt",
		time.Now(),
	)
	require.True(t, errors.Is(err, channel.ErrCASConflict))
}

func TestChannelEnforcementStoreClaimAndHaltLinearize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "claim-halt-race")
	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	deviceID, targetID := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)
	authority, err := enforcementStore.CreateAuthority(t.Context(), channel.CreateAuthorityParams{
		ID:              uuid.New(),
		OrgID:           orgID,
		Type:            "rollout",
		Reference:       "rollout-claim-halt-race",
		CreatedByUserID: testOrganizationUserID(t, db, orgID),
	})
	require.NoError(t, err)
	enforcement, err := enforcementStore.CreateEnforcement(t.Context(), channel.CreateEnforcementParams{
		OrgID:             orgID,
		DeviceID:          deviceID,
		ReleaseTargetID:   targetID,
		CauseType:         "rollout_admission",
		AuthorityID:       authority.ID,
		AuthorityRevision: authority.Revision,
	})
	require.NoError(t, err)

	start := make(chan struct{})
	var claimErr, haltErr error
	var claimed channel.Enforcement
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		claimed, claimErr = enforcementStore.Claim(
			t.Context(),
			enforcement,
			"batch-racing-halt",
			time.Now(),
		)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, haltErr = enforcementStore.HaltAuthority(
			t.Context(),
			authority.ID,
			orgID,
			authority.Revision,
		)
	}()
	close(start)
	wg.Wait()

	require.NoError(t, haltErr)
	if claimErr == nil {
		assert.Equal(t, channel.EnforcementStateDispatching, claimed.State)
		assert.Equal(t, "batch-racing-halt", claimed.CommandBatchUUID)
	} else {
		require.ErrorIs(t, claimErr, channel.ErrCASConflict)
		persisted, getErr := enforcementStore.GetEnforcement(t.Context(), enforcement.ID)
		require.NoError(t, getErr)
		assert.Equal(t, channel.EnforcementStatePending, persisted.State)
		assert.Empty(t, persisted.CommandBatchUUID)
	}
}

func TestChannelEnforcementStoreVerificationRetriesDoNotStarveDueRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	const batchSize int32 = 2
	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 5)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "reconcile-fairness")
	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	_, targetID := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)
	authority, err := enforcementStore.CreateAuthority(t.Context(), channel.CreateAuthorityParams{
		ID:              uuid.New(),
		OrgID:           orgID,
		Type:            "rollout",
		Reference:       "rollout-reconcile-fairness",
		CreatedByUserID: testOrganizationUserID(t, db, orgID),
	})
	require.NoError(t, err)

	enforcements := make([]channel.Enforcement, 0, len(deviceIdentifiers))
	for _, deviceIdentifier := range deviceIdentifiers {
		deviceID, _ := enforcementFixtureIDs(t, db, deviceIdentifier, releaseSet.Id)
		enforcement, createErr := enforcementStore.CreateEnforcement(
			t.Context(),
			channel.CreateEnforcementParams{
				OrgID:             orgID,
				DeviceID:          deviceID,
				ReleaseTargetID:   targetID,
				CauseType:         "rollout_admission",
				AuthorityID:       authority.ID,
				AuthorityRevision: authority.Revision,
			},
		)
		require.NoError(t, createErr)
		enforcements = append(enforcements, enforcement)
	}

	now := time.Now().UTC()
	dueAt := now.Add(-time.Hour)
	for _, enforcement := range enforcements[:3] {
		_, err = db.ExecContext(
			t.Context(),
			`UPDATE channel_firmware_enforcement
			 SET state = 'verifying',
			     attempt_count = 1,
			     command_batch_uuid = $2,
			     command_completed_at = $3,
			     next_reconcile_at = $4
			 WHERE id = $1`,
			enforcement.ID,
			uuid.NewString(),
			now.Add(-2*time.Hour),
			dueAt,
		)
		require.NoError(t, err)
	}
	duePending := enforcements[3]
	_, err = db.ExecContext(
		t.Context(),
		`UPDATE channel_firmware_enforcement
		 SET next_reconcile_at = $2
		 WHERE id = $1`,
		duePending.ID,
		now.Add(-30*time.Minute),
	)
	require.NoError(t, err)
	delayedPending := enforcements[4]
	_, err = db.ExecContext(
		t.Context(),
		`UPDATE channel_firmware_enforcement
		 SET next_reconcile_at = $2
		 WHERE id = $1`,
		delayedPending.ID,
		now.Add(time.Hour),
	)
	require.NoError(t, err)

	firstBatch, err := enforcementStore.ListForReconcile(t.Context(), batchSize)
	require.NoError(t, err)
	require.Len(t, firstBatch, int(batchSize))
	for _, enforcement := range firstBatch {
		require.Equal(t, channel.EnforcementStateVerifying, enforcement.State)
		require.NoError(t, enforcementStore.RecordObservation(
			t.Context(),
			enforcement,
			channel.Observation{Error: "stale telemetry sample"},
			now.Add(time.Hour),
		))
	}

	secondBatch, err := enforcementStore.ListForReconcile(t.Context(), batchSize)
	require.NoError(t, err)
	require.Len(t, secondBatch, int(batchSize))
	secondIDs := []int64{secondBatch[0].ID, secondBatch[1].ID}
	require.Contains(t, secondIDs, duePending.ID)
	require.NotContains(t, secondIDs, delayedPending.ID)

	persisted, err := enforcementStore.GetEnforcement(t.Context(), firstBatch[0].ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.LastError)
	require.Equal(t, "stale telemetry sample", *persisted.LastError)
	require.WithinDuration(t, now.Add(time.Hour), persisted.NextReconcileAt, time.Millisecond)
	require.Equal(t, int32(1), persisted.AttemptCount)

	hashrate := 100.0
	freshObservedAt := now.Add(-time.Minute)
	require.NoError(t, enforcementStore.RecordObservation(
		t.Context(),
		persisted,
		channel.Observation{
			FirmwareVersion: "still-old",
			ObservedAt:      freshObservedAt,
			HashrateHS:      &hashrate,
		},
		now.Add(2*time.Hour),
	))
	persisted, err = enforcementStore.GetEnforcement(t.Context(), persisted.ID)
	require.NoError(t, err)
	require.Nil(t, persisted.LastError)
	require.NotNil(t, persisted.LastObservedFirmwareVersion)
	require.Equal(t, "still-old", *persisted.LastObservedFirmwareVersion)
	require.WithinDuration(t, now.Add(2*time.Hour), persisted.NextReconcileAt, time.Millisecond)
	require.Equal(t, int32(1), persisted.AttemptCount)
}

func enforcementFixtureIDs(
	t *testing.T,
	db queryRower,
	deviceIdentifier string,
	releaseSetID int64,
) (int64, int64) {
	t.Helper()
	var deviceID, targetID int64
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		"SELECT id FROM device WHERE device_identifier = $1",
		deviceIdentifier,
	).Scan(&deviceID))
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		"SELECT id FROM firmware_release_target WHERE release_set_id = $1",
		releaseSetID,
	).Scan(&targetID))
	return deviceID, targetID
}

func testOrganizationUserID(t *testing.T, db queryRower, orgID int64) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		`SELECT "user".id
		 FROM "user"
		 JOIN user_organization ON user_organization.user_id = "user".id
		 WHERE user_organization.organization_id = $1
		 ORDER BY "user".id
		 LIMIT 1`,
		orgID,
	).Scan(&userID))
	return userID
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
