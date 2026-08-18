package sqlstores_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	collectiondomain "github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestRolloutLaneArchiveRemovesAllMembershipsAndRetainsHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	store := sqlstores.NewSQLRolloutLaneStore(db)
	service := betweenchannel.NewService(store, nil)
	createRequest := betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Reusable stable lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "archive-create-lane",
		ActorUserID:       actorID,
	}
	lane, err := service.CreateLane(t.Context(), createRequest)
	require.NoError(t, err)
	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Historical rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
		Batches: []rollout.CreateBatch{{
			Label: "all",
			Members: []rollout.CreateMember{
				{DeviceIdentifier: deviceIdentifiers[0]},
				{DeviceIdentifier: deviceIdentifiers[1]},
			},
		}},
		IdempotencyKey: "archive-start-rollout",
		Reason:         "create retained history",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	sourceChannelID := *started.Rollout.SourceChannelID
	targetChannelID := *started.Rollout.TargetChannelID

	// Model a settled completed rollout with a split physical-channel
	// membership so archive has to clear every historical lane channel.
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	haltRolloutAuthorities(t, db, orgID, started.Rollout.ID)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed'
		WHERE rollout_id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_member
		SET state = 'succeeded',
		    settled_at = CURRENT_TIMESTAMP,
		    owner_released_at = CURRENT_TIMESTAMP
		WHERE rollout_id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_evidence (
		    rollout_id, member_id, org_id, phase, window_start, window_end
		)
		SELECT rollout_id,
		       id,
		       org_id,
		       'baseline',
		       CURRENT_TIMESTAMP - INTERVAL '2 minutes',
		       CURRENT_TIMESTAMP - INTERVAL '1 minute'
		FROM firmware_rollout_member
		WHERE rollout_id = $1 AND org_id = $2
		LIMIT 1
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		DELETE FROM device_set_membership
		WHERE org_id = $1
		  AND device_set_id = $2
		  AND device_identifier = $3
	`, orgID, sourceChannelID, deviceIdentifiers[0])
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO device_set_membership (
		    org_id, device_set_id, device_set_type, device_id, device_identifier
		)
		SELECT org_id, $2, 'channel', id, device_identifier
		FROM device
		WHERE org_id = $1 AND device_identifier = $3
	`, orgID, targetChannelID, deviceIdentifiers[0])
	require.NoError(t, err)

	before := retainedLaneHistoryCounts(t, db, orgID, lane.ID.String())
	actorCredentialID := "lane-delete-session-1"
	request := betweenchannel.DeleteLaneRequest{
		OrgID:             orgID,
		LaneID:            lane.ID,
		ExpectedRevision:  started.Lane.Revision,
		IdempotencyKey:    "archive-delete-lane",
		Reason:            "remove broken demo lane",
		ActorUserID:       actorID,
		ActorType:         rollout.ActorTypeUser,
		ActorCredentialID: &actorCredentialID,
	}
	require.NoError(t, service.DeleteLane(t.Context(), request))

	_, err = service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "got %v", err)
	lanes, err := service.ListLanes(t.Context(), orgID)
	require.NoError(t, err)
	assert.Empty(t, lanes)
	assert.Empty(t, channelMembers(t, db, orgID, sourceChannelID))
	assert.Empty(t, channelMembers(t, db, orgID, targetChannelID))
	assert.Equal(t, before, retainedLaneHistoryCounts(t, db, orgID, lane.ID.String()))

	var deletedAt sql.NullTime
	var deletedBy int64
	var actorType, actorCredential, reason, key, fingerprint string
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT deleted_at, deleted_by_user_id, deleted_actor_type,
		       deleted_actor_credential_id, delete_reason,
		       delete_idempotency_key, delete_fingerprint
		FROM rollout_lane
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID).Scan(
		&deletedAt,
		&deletedBy,
		&actorType,
		&actorCredential,
		&reason,
		&key,
		&fingerprint,
	))
	assert.True(t, deletedAt.Valid)
	assert.Equal(t, actorID, deletedBy)
	assert.Equal(t, string(rollout.ActorTypeUser), actorType)
	assert.Equal(t, actorCredentialID, actorCredential)
	assert.Equal(t, request.Reason, reason)
	assert.Equal(t, request.IdempotencyKey, key)
	assert.Len(t, fingerprint, 64)
	assert.True(t, rolloutLaneInitialAuthorityHalted(t, db, orgID, lane.ID.String()))

	// Exact replay is successful after the lane is hidden.
	require.NoError(t, service.DeleteLane(t.Context(), request))
	mismatch := request
	mismatch.Reason = "different request"
	err = service.DeleteLane(t.Context(), mismatch)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "got %v", err)
	freshKey := request
	freshKey.IdempotencyKey = "archive-delete-lane-fresh-key"
	err = service.DeleteLane(t.Context(), freshKey)
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "got %v", err)

	// The original create identity remains consumed by the archived lane and
	// cannot replay that hidden row into active client state.
	_, err = service.CreateLane(t.Context(), createRequest)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "got %v", err)
	lanes, err = service.ListLanes(t.Context(), orgID)
	require.NoError(t, err)
	assert.Empty(t, lanes)

	reused, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             lane.Label,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "archive-reuse-label",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	assert.NotEqual(t, lane.ID, reused.ID)
}

func TestRolloutLaneArchivePersistsAPIKeyActorIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "API key archive lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "api-key-archive-create",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	identity := "apikey:lane-archive-77"
	request := deleteLaneRequest(lane, orgID, actorID, "api-key-attribution")
	request.ActorType = rollout.ActorTypeAPIKey
	request.ActorCredentialID = &identity
	require.NoError(t, service.DeleteLane(t.Context(), request))

	var deletedBy int64
	var actorType, actorCredential string
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT deleted_by_user_id, deleted_actor_type, deleted_actor_credential_id
		FROM rollout_lane
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID).Scan(&deletedBy, &actorType, &actorCredential))
	assert.Equal(t, actorID, deletedBy)
	assert.Equal(t, string(rollout.ActorTypeAPIKey), actorType)
	assert.Equal(t, identity, actorCredential)
}

func TestRolloutLaneArchiveMetadataConstraintsRejectInvalidActorPairing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Archive actor constraint lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "archive-actor-constraint-create",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane
		SET deleted_at = CURRENT_TIMESTAMP,
		    deleted_by_user_id = $3,
		    deleted_actor_type = 'api_key',
		    deleted_actor_credential_id = NULL,
		    delete_reason = 'invalid API key identity',
		    delete_idempotency_key = 'invalid-api-key-identity',
		    delete_fingerprint = repeat('a', 64)
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID, actorID)
	assertPostgresConstraint(t, err, "ck_rollout_lane_deleted_actor_identity")

	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane
		SET deleted_actor_type = 'user'
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID)
	assertPostgresConstraint(t, err, "ck_rollout_lane_archive_metadata")
}

func TestRolloutLaneArchiveRemovesSoftDeletedMemberAndRetainsHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Soft-deleted member lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "d")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "soft-deleted-member-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), lane.InitialEnforcement.ConfirmedCount)
	before := retainedLaneHistoryCounts(t, db, orgID, lane.ID.String())

	removed, err := sqlc.New(db).SoftDeleteDevices(
		t.Context(),
		sqlc.SoftDeleteDevicesParams{
			OrgID:             orgID,
			DeviceIdentifiers: deviceIdentifiers,
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
	assert.Equal(t, deviceIdentifiers, channelMembers(t, db, orgID, lane.CurrentChannelID))

	require.NoError(t, service.DeleteLane(
		t.Context(),
		deleteLaneRequest(lane, orgID, actorID, "soft-deleted-member"),
	))
	assert.Empty(t, channelMembers(t, db, orgID, lane.CurrentChannelID))
	assert.Equal(t, before, retainedLaneHistoryCounts(t, db, orgID, lane.ID.String()))

	var deletedAt sql.NullTime
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT deleted_at
		FROM device
		WHERE org_id = $1 AND device_identifier = $2
	`, orgID, deviceIdentifiers[0]).Scan(&deletedAt))
	assert.True(t, deletedAt.Valid)
}

func TestRolloutLaneArchiveSerializesWithBetweenChannelRevert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("delete commits first", func(t *testing.T) {
		db, orgID, actorID, lane, completed := setupSettledRolloutLane(t, "delete-first")
		laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		rolloutStore := sqlstores.NewSQLRolloutStore(db)

		channelBlocker, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = channelBlocker.Rollback() })
		_, err = channelBlocker.ExecContext(t.Context(), `
			SELECT 1
			FROM device_set_channel
			WHERE device_set_id = $1 AND org_id = $2
			FOR UPDATE
		`, lane.CurrentChannelID, orgID)
		require.NoError(t, err)

		deleteDone := runAsyncError(func() error {
			return laneService.DeleteLane(
				t.Context(),
				deleteLaneRequest(lane, orgID, actorID, "delete-before-revert"),
			)
		})
		waitForLockedRow(t, db, `
			SELECT 1 FROM rollout_lane WHERE id = $1 AND org_id = $2 FOR UPDATE
		`, lane.ID, orgID)

		controlRequest := rolloutControlRequest(
			completed,
			rollout.ControlOperationRevert,
			"revert-after-delete-started",
		)
		controlDone := runAsyncError(func() error {
			_, applyErr := rolloutStore.ApplyControl(t.Context(), controlRequest)
			return applyErr
		})
		requireStillBlocked(t, controlDone, "revert control")

		require.NoError(t, channelBlocker.Commit())
		require.NoError(t, awaitAsyncError(t, deleteDone, "lane deletion"))
		controlErr := awaitAsyncError(t, controlDone, "revert control")
		require.ErrorIs(t, controlErr, rollout.ErrNotFound)

		var state string
		require.NoError(t, db.QueryRowContext(t.Context(), `
			SELECT state FROM firmware_rollout WHERE id = $1 AND org_id = $2
		`, completed.ID, orgID).Scan(&state))
		assert.Equal(t, string(rollout.StateCompleted), state)
	})

	t.Run("revert commits first", func(t *testing.T) {
		db, orgID, actorID, lane, completed := setupSettledRolloutLane(t, "revert-first")
		laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		rolloutStore := sqlstores.NewSQLRolloutStore(db)

		rolloutBlocker, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rolloutBlocker.Rollback() })
		_, err = rolloutBlocker.ExecContext(t.Context(), `
			SELECT 1
			FROM firmware_rollout
			WHERE id = $1 AND org_id = $2
			FOR UPDATE
		`, completed.ID, orgID)
		require.NoError(t, err)

		controlRequest := rolloutControlRequest(
			completed,
			rollout.ControlOperationRevert,
			"revert-before-delete",
		)
		controlDone := runAsyncError(func() error {
			_, applyErr := rolloutStore.ApplyControl(t.Context(), controlRequest)
			return applyErr
		})
		waitForLockedRow(t, db, `
			SELECT 1 FROM rollout_lane WHERE id = $1 AND org_id = $2 FOR UPDATE
		`, lane.ID, orgID)

		deleteDone := runAsyncError(func() error {
			return laneService.DeleteLane(
				t.Context(),
				deleteLaneRequest(lane, orgID, actorID, "delete-after-revert-started"),
			)
		})
		requireStillBlocked(t, deleteDone, "lane deletion")

		require.NoError(t, rolloutBlocker.Commit())
		require.NoError(t, awaitAsyncError(t, controlDone, "revert control"))
		deleteErr := awaitAsyncError(t, deleteDone, "lane deletion")
		require.Error(t, deleteErr)
		assert.True(t, fleeterror.IsFailedPreconditionError(deleteErr), "got %v", deleteErr)
		assert.ErrorContains(t, deleteErr, "rollout, revert, or finalizer work must settle")

		var state string
		require.NoError(t, db.QueryRowContext(t.Context(), `
			SELECT state FROM firmware_rollout WHERE id = $1 AND org_id = $2
		`, completed.ID, orgID).Scan(&state))
		assert.Equal(t, string(rollout.StateReverting), state)
	})
}

func TestRolloutLaneChannelsRejectGenericAssignment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, archived := range []bool{false, true} {
		name := "active lane"
		if archived {
			name = "archived lane"
		}
		t.Run(name, func(t *testing.T) {
			db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
			actorID := testOrganizationUserID(t, db, orgID)
			laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
			lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
				OrgID:             orgID,
				Label:             "Direct assignment " + name,
				ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "e")},
				DeviceIdentifiers: deviceIdentifiers,
				IdempotencyKey:    "direct-assignment-" + name,
				ActorUserID:       actorID,
			})
			require.NoError(t, err)
			if archived {
				require.NoError(t, laneService.DeleteLane(
					t.Context(),
					deleteLaneRequest(lane, orgID, actorID, "direct-assignment-archived"),
				))
			}

			assignmentService := newChannelAssignmentService(db)
			_, err = assignmentService.AssignDevicesToChannel(
				t.Context(),
				collectiondomain.AssignDevicesToChannelParams{
					OrgID:             orgID,
					TargetChannelID:   &lane.CurrentChannelID,
					DeviceIdentifiers: deviceIdentifiers,
				},
			)
			require.Error(t, err)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
			if archived {
				assert.Empty(t, channelMembers(t, db, orgID, lane.CurrentChannelID))
			} else {
				assert.Equal(t, deviceIdentifiers, channelMembers(t, db, orgID, lane.CurrentChannelID))
			}
		})
	}
}

func TestRolloutLaneArchiveSerializesWithQueuedGenericAssignment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("delete commits before queued assignment", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:             orgID,
			Label:             "Delete before assignment",
			ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "f")},
			DeviceIdentifiers: deviceIdentifiers,
			IdempotencyKey:    "delete-before-assignment",
			ActorUserID:       actorID,
		})
		require.NoError(t, err)

		deviceBlocker := lockDeviceForTest(t, db, orgID, deviceIdentifiers[0])
		deleteDone := runAsyncError(func() error {
			return laneService.DeleteLane(
				t.Context(),
				deleteLaneRequest(lane, orgID, actorID, "delete-before-assignment"),
			)
		})
		waitForLockedRow(t, db, `
			SELECT 1
			FROM device_set_channel
			WHERE device_set_id = $1 AND org_id = $2
			FOR UPDATE
		`, lane.CurrentChannelID, orgID)

		assignmentDone := runAsyncError(func() error {
			_, assignErr := newChannelAssignmentService(db).AssignDevicesToChannel(
				t.Context(),
				collectiondomain.AssignDevicesToChannelParams{
					OrgID:             orgID,
					TargetChannelID:   &lane.CurrentChannelID,
					DeviceIdentifiers: deviceIdentifiers,
				},
			)
			return assignErr
		})
		requireStillBlocked(t, assignmentDone, "channel assignment")

		require.NoError(t, deviceBlocker.Commit())
		require.NoError(t, awaitAsyncError(t, deleteDone, "lane deletion"))
		assignmentErr := awaitAsyncError(t, assignmentDone, "channel assignment")
		require.Error(t, assignmentErr)
		assert.True(t, fleeterror.IsFailedPreconditionError(assignmentErr), "got %v", assignmentErr)
		assert.Empty(t, channelMembers(t, db, orgID, lane.CurrentChannelID))
	})

	t.Run("queued delete follows rejected assignment", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:             orgID,
			Label:             "Assignment before delete",
			ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "1")},
			DeviceIdentifiers: deviceIdentifiers,
			IdempotencyKey:    "assignment-before-delete",
			ActorUserID:       actorID,
		})
		require.NoError(t, err)

		deviceBlocker := lockDeviceForTest(t, db, orgID, deviceIdentifiers[0])
		assignmentDone := runAsyncError(func() error {
			_, assignErr := newChannelAssignmentService(db).AssignDevicesToChannel(
				t.Context(),
				collectiondomain.AssignDevicesToChannelParams{
					OrgID:             orgID,
					TargetChannelID:   &lane.CurrentChannelID,
					DeviceIdentifiers: deviceIdentifiers,
				},
			)
			return assignErr
		})
		waitForLockedRow(t, db, `
			SELECT 1
			FROM device_set_channel
			WHERE device_set_id = $1 AND org_id = $2
			FOR UPDATE
		`, lane.CurrentChannelID, orgID)

		deleteDone := runAsyncError(func() error {
			return laneService.DeleteLane(
				t.Context(),
				deleteLaneRequest(lane, orgID, actorID, "delete-after-assignment"),
			)
		})
		waitForLockedRow(t, db, `
			SELECT 1 FROM rollout_lane WHERE id = $1 AND org_id = $2 FOR UPDATE
		`, lane.ID, orgID)

		require.NoError(t, deviceBlocker.Commit())
		assignmentErr := awaitAsyncError(t, assignmentDone, "channel assignment")
		require.Error(t, assignmentErr)
		assert.True(t, fleeterror.IsFailedPreconditionError(assignmentErr), "got %v", assignmentErr)
		require.NoError(t, awaitAsyncError(t, deleteDone, "lane deletion"))
		assert.Empty(t, channelMembers(t, db, orgID, lane.CurrentChannelID))
	})
}

func TestRolloutLaneArchiveBlocksActiveInitialAndRolloutWork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, initialState := range []string{"pending", "held", "dispatching", "dispatched", "verifying"} {
		t.Run("initial "+initialState, func(t *testing.T) {
			db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
			actorID := testOrganizationUserID(t, db, orgID)
			service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
			lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
				OrgID:                     orgID,
				Label:                     "Active initial " + initialState,
				ReleaseTargets:            []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "d")},
				DeviceIdentifiers:         deviceIdentifiers,
				IdempotencyKey:            "active-initial-" + initialState,
				ActorUserID:               actorID,
				ConfirmInitialEnforcement: true,
			})
			require.NoError(t, err)
			_, err = db.ExecContext(t.Context(), `
				UPDATE channel_firmware_enforcement enforcement
				SET state = $3
				FROM channel_firmware_authority authority
				WHERE authority.id = enforcement.authority_id
				  AND authority.org_id = enforcement.org_id
				  AND authority.org_id = $1
				  AND authority.authority_type = 'rollout_lane_initial'
				  AND authority.authority_reference = $2
			`, orgID, lane.ID.String(), initialState)
			require.NoError(t, err)

			err = service.DeleteLane(t.Context(), deleteLaneRequest(lane, orgID, actorID, "initial-"+initialState))
			require.Error(t, err)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
		})
	}

	for _, rolloutState := range []string{"running", "reverting", "aborted"} {
		t.Run("rollout "+rolloutState, func(t *testing.T) {
			db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
			actorID := testOrganizationUserID(t, db, orgID)
			service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
			lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
				OrgID:             orgID,
				Label:             "Active rollout " + rolloutState,
				ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "e")},
				DeviceIdentifiers: deviceIdentifiers,
				IdempotencyKey:    "active-rollout-" + rolloutState,
				ActorUserID:       actorID,
			})
			require.NoError(t, err)
			started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
				OrgID:          orgID,
				LaneID:         lane.ID,
				Name:           "Active work",
				ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "f")},
				Batches: []rollout.CreateBatch{{
					Label:   "all",
					Members: []rollout.CreateMember{{DeviceIdentifier: deviceIdentifiers[0]}},
				}},
				IdempotencyKey: "active-work-" + rolloutState,
				Reason:         "test active deletion guard",
				ActorUserID:    actorID,
			})
			require.NoError(t, err)
			_, err = db.ExecContext(t.Context(), `
				UPDATE firmware_rollout SET state = $3 WHERE id = $1 AND org_id = $2
			`, started.Rollout.ID, orgID, rolloutState)
			require.NoError(t, err)

			err = service.DeleteLane(
				t.Context(),
				deleteLaneRequest(started.Lane, orgID, actorID, "rollout-"+rolloutState),
			)
			require.Error(t, err)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
		})
	}

	t.Run("started rollout control", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:             orgID,
			Label:             "Active control lane",
			ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "7")},
			DeviceIdentifiers: deviceIdentifiers,
			IdempotencyKey:    "active-control-lane",
			ActorUserID:       actorID,
		})
		require.NoError(t, err)
		started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
			OrgID:          orgID,
			LaneID:         lane.ID,
			Name:           "Active control",
			ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "8")},
			Batches: []rollout.CreateBatch{{
				Label:   "all",
				Members: []rollout.CreateMember{{DeviceIdentifier: deviceIdentifiers[0]}},
			}},
			IdempotencyKey: "active-control-start",
			Reason:         "test active control deletion guard",
			ActorUserID:    actorID,
		})
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout
			SET state = 'completed', completed_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND org_id = $2
		`, started.Rollout.ID, orgID)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_member
			SET state = 'succeeded',
			    settled_at = CURRENT_TIMESTAMP,
			    owner_released_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1 AND org_id = $2
		`, started.Rollout.ID, orgID)
		require.NoError(t, err)
		haltRolloutAuthorities(t, db, orgID, started.Rollout.ID)
		_, err = db.ExecContext(t.Context(), `
			INSERT INTO firmware_rollout_control (
			    id, rollout_id, org_id, operation, idempotency_key,
			    request_fingerprint, expected_revision, resulting_revision,
			    status, created_by_user_id
			)
			VALUES (
			    gen_random_uuid(), $1, $2, 'complete', 'active-delete-control',
			    repeat('a', 64), 1, 2, 'started', $3
			)
		`, started.Rollout.ID, orgID, actorID)
		require.NoError(t, err)

		err = service.DeleteLane(
			t.Context(),
			deleteLaneRequest(started.Lane, orgID, actorID, "active-control"),
		)
		require.Error(t, err)
		assert.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	})
}

func TestRolloutLaneArchiveAllowsAttentionTerminalAndSettledAbort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("attention-required initial setup", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:                     orgID,
			Label:                     "Attention lane",
			ReleaseTargets:            []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "1")},
			DeviceIdentifiers:         deviceIdentifiers,
			IdempotencyKey:            "attention-lane",
			ActorUserID:               actorID,
			ConfirmInitialEnforcement: true,
		})
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE channel_firmware_enforcement enforcement
			SET state = 'attention_required',
			    attention_required_at = CURRENT_TIMESTAMP
			FROM channel_firmware_authority authority
			WHERE authority.id = enforcement.authority_id
			  AND authority.org_id = enforcement.org_id
			  AND authority.org_id = $1
			  AND authority.authority_reference = $2
		`, orgID, lane.ID.String())
		require.NoError(t, err)
		require.NoError(t, service.DeleteLane(
			t.Context(),
			deleteLaneRequest(lane, orgID, actorID, "attention-terminal"),
		))
	})

	t.Run("cancelled initial setup", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:                     orgID,
			Label:                     "Cancelled setup lane",
			ReleaseTargets:            []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "9")},
			DeviceIdentifiers:         deviceIdentifiers,
			IdempotencyKey:            "cancelled-setup-lane",
			ActorUserID:               actorID,
			ConfirmInitialEnforcement: true,
		})
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE channel_firmware_enforcement enforcement
			SET state = 'cancelled'
			FROM channel_firmware_authority authority
			WHERE authority.id = enforcement.authority_id
			  AND authority.org_id = enforcement.org_id
			  AND authority.org_id = $1
			  AND authority.authority_reference = $2
		`, orgID, lane.ID.String())
		require.NoError(t, err)
		require.NoError(t, service.DeleteLane(
			t.Context(),
			deleteLaneRequest(lane, orgID, actorID, "cancelled-terminal"),
		))
	})

	t.Run("fully settled aborted rollout", func(t *testing.T) {
		db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID:             orgID,
			Label:             "Settled abort lane",
			ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "2")},
			DeviceIdentifiers: deviceIdentifiers,
			IdempotencyKey:    "settled-abort-lane",
			ActorUserID:       actorID,
		})
		require.NoError(t, err)
		started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
			OrgID:          orgID,
			LaneID:         lane.ID,
			Name:           "Settled abort",
			ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "3")},
			Batches: []rollout.CreateBatch{{
				Label:   "all",
				Members: []rollout.CreateMember{{DeviceIdentifier: deviceIdentifiers[0]}},
			}},
			IdempotencyKey: "settled-abort-start",
			Reason:         "test settled abort",
			ActorUserID:    actorID,
		})
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout
			SET state = 'aborted', aborted_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND org_id = $2
		`, started.Rollout.ID, orgID)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_batch
			SET state = 'completed'
			WHERE rollout_id = $1 AND org_id = $2
		`, started.Rollout.ID, orgID)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_member
			SET state = 'cancelled',
			    settled_at = CURRENT_TIMESTAMP,
			    owner_released_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1 AND org_id = $2
		`, started.Rollout.ID, orgID)
		require.NoError(t, err)
		haltRolloutAuthorities(t, db, orgID, started.Rollout.ID)
		require.NoError(t, service.DeleteLane(
			t.Context(),
			deleteLaneRequest(started.Lane, orgID, actorID, "settled-abort"),
		))
	})

	for _, terminalState := range []string{"completed_with_failures", "reverted"} {
		t.Run(terminalState+" rollout", func(t *testing.T) {
			db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
			actorID := testOrganizationUserID(t, db, orgID)
			service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
			lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
				OrgID:             orgID,
				Label:             "Terminal " + terminalState,
				ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
				DeviceIdentifiers: deviceIdentifiers,
				IdempotencyKey:    "terminal-lane-" + terminalState,
				ActorUserID:       actorID,
			})
			require.NoError(t, err)
			started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
				OrgID:          orgID,
				LaneID:         lane.ID,
				Name:           "Terminal rollout",
				ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
				Batches: []rollout.CreateBatch{{
					Label:   "all",
					Members: []rollout.CreateMember{{DeviceIdentifier: deviceIdentifiers[0]}},
				}},
				IdempotencyKey: "terminal-start-" + terminalState,
				Reason:         "test terminal deletion",
				ActorUserID:    actorID,
			})
			require.NoError(t, err)
			_, err = db.ExecContext(t.Context(), `
				UPDATE firmware_rollout SET state = $3 WHERE id = $1 AND org_id = $2
			`, started.Rollout.ID, orgID, terminalState)
			require.NoError(t, err)
			_, err = db.ExecContext(t.Context(), `
				UPDATE firmware_rollout_member
				SET state = 'cancelled',
				    settled_at = CURRENT_TIMESTAMP,
				    owner_released_at = CURRENT_TIMESTAMP
				WHERE rollout_id = $1 AND org_id = $2
			`, started.Rollout.ID, orgID)
			require.NoError(t, err)
			haltRolloutAuthorities(t, db, orgID, started.Rollout.ID)
			require.NoError(t, service.DeleteLane(
				t.Context(),
				deleteLaneRequest(started.Lane, orgID, actorID, "terminal-"+terminalState),
			))
		})
	}
}

func TestRolloutLaneArchiveIsOrganizationScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Scoped lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "4")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "scoped-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	request := deleteLaneRequest(lane, orgID+1, actorID, "wrong-org")
	err = service.DeleteLane(t.Context(), request)
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "got %v", err)
	_, err = service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, deviceIdentifiers, channelMembers(t, db, orgID, lane.CurrentChannelID))
}

func setupSettledRolloutLane(
	t *testing.T,
	suffix string,
) (*sql.DB, int64, int64, *betweenchannel.Lane, *rollout.Rollout) {
	t.Helper()
	db, orgID, deviceIdentifiers := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	laneService := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Settled lane " + suffix,
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "2")},
		DeviceIdentifiers: deviceIdentifiers,
		IdempotencyKey:    "settled-lane-" + suffix,
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Settled rollout " + suffix,
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "3")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIdentifiers[0]}},
		}},
		IdempotencyKey: "settled-rollout-" + suffix,
		Reason:         "prepare deterministic serialization test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	sourceChannelID := *started.Rollout.SourceChannelID
	targetChannelID := *started.Rollout.TargetChannelID

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed'
		WHERE rollout_id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_member
		SET state = 'succeeded',
		    settled_at = CURRENT_TIMESTAMP,
		    owner_released_at = CURRENT_TIMESTAMP
		WHERE rollout_id = $1 AND org_id = $2
	`, started.Rollout.ID, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		DELETE FROM device_set_membership
		WHERE org_id = $1
		  AND device_set_id = $2
		  AND device_set_type = 'channel'
	`, orgID, sourceChannelID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO device_set_membership (
		    org_id, device_set_id, device_set_type, device_id, device_identifier
		)
		SELECT org_id, $2, 'channel', id, device_identifier
		FROM device
		WHERE org_id = $1 AND device_identifier = $3
	`, orgID, targetChannelID, deviceIdentifiers[0])
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane
		SET current_channel_id = $3,
		    revision = revision + 1
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID, targetChannelID)
	require.NoError(t, err)
	haltRolloutAuthorities(t, db, orgID, started.Rollout.ID)

	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	completed, err := sqlstores.NewSQLRolloutStore(db).Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateCompleted, completed.State)
	return db, orgID, actorID, lane, completed
}

func newChannelAssignmentService(db *sql.DB) *collectiondomain.Service {
	store := newCollectionStore(db)
	return collectiondomain.NewService(
		store,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
}

func lockDeviceForTest(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `
		SELECT 1
		FROM device
		WHERE org_id = $1 AND device_identifier = $2
		FOR UPDATE
	`, orgID, deviceIdentifier)
	require.NoError(t, err)
	return tx
}

func assertPostgresConstraint(t *testing.T, err error, constraintName string) {
	t.Helper()
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code)
	assert.Equal(t, constraintName, pgErr.ConstraintName)
}

func waitForLockedRow(
	t *testing.T,
	db *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tx, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), "SET LOCAL lock_timeout = '25ms'")
		require.NoError(t, err)
		var marker int
		lockErr := tx.QueryRowContext(t.Context(), query, args...).Scan(&marker)
		_ = tx.Rollback()

		var pgErr *pgconn.PgError
		if errors.As(lockErr, &pgErr) && pgErr.Code == "55P03" {
			return
		}
		if lockErr != nil {
			require.NoError(t, lockErr)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runAsyncError(fn func() error) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- fn()
	}()
	return result
}

func requireStillBlocked(t *testing.T, result <-chan error, operation string) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("%s returned before blocking transaction committed: %v", operation, err)
	case <-time.After(100 * time.Millisecond):
	}
}

func awaitAsyncError(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not complete", operation)
		return nil
	}
}

type laneHistoryCounts struct {
	physicalChannels int64
	attachments      int64
	rollouts         int64
	rolloutMembers   int64
	rolloutCauses    int64
	rolloutEvidence  int64
	releaseSets      int64
	authorities      int64
	enforcements     int64
}

func retainedLaneHistoryCounts(t *testing.T, db *sql.DB, orgID int64, laneID string) laneHistoryCounts {
	t.Helper()
	var result laneHistoryCounts
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT
		    (SELECT COUNT(*)
		     FROM device_set_channel channel
		     JOIN rollout_lane_channel attachment ON attachment.channel_id = channel.device_set_id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(*) FROM rollout_lane_channel WHERE org_id = $1 AND lane_id = $2),
		    (SELECT COUNT(*)
		     FROM firmware_rollout rollout
		     JOIN rollout_lane_channel attachment ON attachment.rollout_id = rollout.id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(*)
		     FROM firmware_rollout_member member
		     JOIN rollout_lane_channel attachment ON attachment.rollout_id = member.rollout_id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(*)
		     FROM firmware_rollout_cause cause
		     JOIN rollout_lane_channel attachment ON attachment.rollout_id = cause.rollout_id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(*)
		     FROM firmware_rollout_evidence evidence
		     JOIN rollout_lane_channel attachment ON attachment.rollout_id = evidence.rollout_id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(DISTINCT channel.release_set_id)
		     FROM device_set_channel channel
		     JOIN rollout_lane_channel attachment ON attachment.channel_id = channel.device_set_id
		     WHERE attachment.org_id = $1 AND attachment.lane_id = $2),
		    (SELECT COUNT(*)
		     FROM channel_firmware_authority
		     WHERE org_id = $1
		       AND (
		           (authority_type = 'rollout_lane_initial' AND authority_reference = $2::uuid::text)
		           OR (authority_type = 'rollout' AND authority_reference IN (
		               SELECT rollout_id::text
		               FROM rollout_lane_channel
		               WHERE org_id = $1 AND lane_id = $2 AND rollout_id IS NOT NULL
		           ))
		       )),
		    (SELECT COUNT(*)
		     FROM channel_firmware_enforcement enforcement
		     JOIN channel_firmware_authority authority ON authority.id = enforcement.authority_id
		     WHERE authority.org_id = $1
		       AND (
		           (authority.authority_type = 'rollout_lane_initial'
		               AND authority.authority_reference = $2::uuid::text)
		           OR (authority.authority_type = 'rollout' AND authority.authority_reference IN (
		               SELECT rollout_id::text
		               FROM rollout_lane_channel
		               WHERE org_id = $1 AND lane_id = $2 AND rollout_id IS NOT NULL
		           ))
		       ))
	`, orgID, laneID).Scan(
		&result.physicalChannels,
		&result.attachments,
		&result.rollouts,
		&result.rolloutMembers,
		&result.rolloutCauses,
		&result.rolloutEvidence,
		&result.releaseSets,
		&result.authorities,
		&result.enforcements,
	))
	return result
}

func rolloutLaneInitialAuthorityHalted(t *testing.T, db *sql.DB, orgID int64, laneID string) bool {
	t.Helper()
	var halted bool
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT bool_and(halted_at IS NOT NULL)
		FROM channel_firmware_authority
		WHERE org_id = $1
		  AND authority_type = 'rollout_lane_initial'
		  AND authority_reference = $2
	`, orgID, laneID).Scan(&halted))
	return halted
}

func haltRolloutAuthorities(t *testing.T, db *sql.DB, orgID int64, rolloutID uuid.UUID) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE channel_firmware_authority authority
		SET halted_at = COALESCE(authority.halted_at, CURRENT_TIMESTAMP),
		    revision = CASE
		        WHEN authority.halted_at IS NULL THEN authority.revision + 1
		        ELSE authority.revision
		    END
		FROM firmware_rollout rollout
		WHERE rollout.id = $1
		  AND rollout.org_id = $2
		  AND authority.org_id = rollout.org_id
		  AND authority.id IN (rollout.forward_authority_id, rollout.revert_authority_id)
	`, rolloutID, orgID)
	require.NoError(t, err)
}

func deleteLaneRequest(
	lane *betweenchannel.Lane,
	orgID int64,
	actorID int64,
	suffix string,
) betweenchannel.DeleteLaneRequest {
	return betweenchannel.DeleteLaneRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		ExpectedRevision: lane.Revision,
		IdempotencyKey:   "delete-" + suffix,
		Reason:           "archive test " + suffix,
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}
}
