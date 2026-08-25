package sqlstores_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/migrations"
)

func TestTopologyReadinessBoundsThousandsOfAnomaliesWithStableCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupRolloutLaneTestData(t, 1)
	laneID := uuid.New()
	_, err := db.ExecContext(t.Context(), `DROP VIEW rollout_lane_topology_anomaly`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE rollout_lane_topology_anomaly (
		    anomaly_id UUID NOT NULL,
		    lane_id UUID NOT NULL,
		    org_id BIGINT NOT NULL,
		    device_id BIGINT NOT NULL,
		    device_identifier TEXT NOT NULL,
		    lane_model_id UUID NULL,
		    lane_model_revision BIGINT NULL,
		    anomaly_type TEXT NOT NULL,
		    supported_repair_actions TEXT[] NOT NULL,
		    details JSONB NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO rollout_lane_topology_anomaly (
		    anomaly_id, lane_id, org_id, device_id, device_identifier,
		    anomaly_type, supported_repair_actions, details
		)
		SELECT md5(n::text)::uuid, $1, $2, n,
		       'miner-' || lpad(n::text, 5, '0'),
		       'null_identity', ARRAY['confirm_identity']::text[], '{}'::jsonb
		FROM generate_series(1, 2500) AS n
	`, laneID, orgID)
	require.NoError(t, err)

	store := sqlstores.NewSQLRolloutLaneStore(db)
	var (
		cursor *betweenchannel.TopologyAnomalyCursor
		seen   = make(map[uuid.UUID]struct{}, 2500)
	)
	for {
		page, pageErr := store.GetTopologyReadinessPage(
			t.Context(),
			orgID,
			betweenchannel.TopologyReadinessRequest{Limit: 100, After: cursor},
		)
		require.NoError(t, pageErr)
		assert.Equal(t, int64(2500), page.AnomalyCount)
		assert.LessOrEqual(t, len(page.Anomalies), 100)
		for _, anomaly := range page.Anomalies {
			_, duplicate := seen[anomaly.ID]
			assert.False(t, duplicate)
			seen[anomaly.ID] = struct{}{}
		}
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	assert.Len(t, seen, 2500)
}

func TestRolloutLaneActiveParentConcurrentClaimsHaveOneWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Concurrent parent claim lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "concurrent-parent-claim-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	queries := sqlc.New(db)
	groupIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for i, groupID := range groupIDs {
		_, err = queries.TestCreateFirmwareRolloutGroup(
			t.Context(),
			sqlc.TestCreateFirmwareRolloutGroupParams{
				GroupID:           groupID,
				LaneID:            lane.ID,
				OrgID:             orgID,
				Name:              fmt.Sprintf("Concurrent parent %d", i),
				IdempotencyKey:    fmt.Sprintf("concurrent-parent-%d", i),
				CreateFingerprint: strings.Repeat(fmt.Sprintf("%x", i+1), 64),
				Reason:            "prove the lane claim constraint",
				CreatedByUserID:   actorID,
			},
		)
		require.NoError(t, err)
	}

	type claimResult struct {
		groupID uuid.UUID
		err     error
	}
	start := make(chan struct{})
	results := make(chan claimResult, len(groupIDs))
	for i, groupID := range groupIDs {
		go func(index int, candidate uuid.UUID) {
			tx, txErr := db.BeginTx(t.Context(), nil)
			if txErr != nil {
				results <- claimResult{groupID: candidate, err: txErr}
				return
			}
			defer tx.Rollback()

			<-start
			_, txErr = sqlc.New(tx).TestClaimRolloutLaneActiveParent(
				t.Context(),
				sqlc.TestClaimRolloutLaneActiveParentParams{
					LaneID:              lane.ID,
					OrgID:               orgID,
					GroupID:             candidate,
					ClaimIdempotencyKey: fmt.Sprintf("concurrent-claim-%d", index),
					ClaimFingerprint:    strings.Repeat(fmt.Sprintf("%x", index+3), 64),
				},
			)
			if txErr == nil {
				txErr = tx.Commit()
			}
			results <- claimResult{groupID: candidate, err: txErr}
		}(i, groupID)
	}
	close(start)

	var winner uuid.UUID
	var successes, failures int
	for range groupIDs {
		result := <-results
		if result.err == nil {
			successes++
			winner = result.groupID
		} else {
			failures++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, failures)

	claimCount, err := queries.CountRolloutLaneActiveParentsForTest(
		t.Context(),
		sqlc.CountRolloutLaneActiveParentsForTestParams{
			LaneID: lane.ID,
			OrgID:  orgID,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), claimCount)

	relationship, err := queries.GetRolloutLaneActiveParentForTest(
		t.Context(),
		sqlc.GetRolloutLaneActiveParentForTestParams{
			LaneID: lane.ID,
			OrgID:  orgID,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, winner, relationship.GroupID)
	assert.Equal(t, lane.ID, relationship.LaneID)
	assert.Equal(t, lane.ID, relationship.ParentLaneID)
	assert.Equal(t, orgID, relationship.OrgID)
	assert.Equal(t, orgID, relationship.ParentOrgID)
}

func TestFirmwareRolloutGroupResultRevisionOnlyTracksPublishedResultChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Parent result revision lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "parent-result-revision-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	queries := sqlc.New(db)
	groupID := uuid.New()
	group, err := queries.TestCreateFirmwareRolloutGroup(
		t.Context(),
		sqlc.TestCreateFirmwareRolloutGroupParams{
			GroupID:           groupID,
			LaneID:            lane.ID,
			OrgID:             orgID,
			Name:              "Original parent metadata",
			IdempotencyKey:    "parent-result-revision",
			CreateFingerprint: strings.Repeat("a", 64),
			Reason:            "prove result revision semantics",
			CreatedByUserID:   actorID,
		},
	)
	require.NoError(t, err)
	assert.Zero(t, group.ResultRevision)

	group, err = queries.TestUpdateFirmwareRolloutGroupMetadata(
		t.Context(),
		sqlc.TestUpdateFirmwareRolloutGroupMetadataParams{
			Name:    "Updated parent metadata",
			GroupID: groupID,
			OrgID:   orgID,
		},
	)
	require.NoError(t, err)
	assert.Zero(t, group.ResultRevision, "unrelated metadata must not publish a new result")

	group = updateFirmwareRolloutGroupResult(t, queries, groupID, orgID, "successful", false)
	assert.Equal(t, int64(1), group.ResultRevision)

	group = updateFirmwareRolloutGroupResult(t, queries, groupID, orgID, "successful", false)
	assert.Equal(t, int64(1), group.ResultRevision, "replaying a terminal outcome must be idempotent")

	group = updateFirmwareRolloutGroupResult(t, queries, groupID, orgID, "successful", true)
	assert.Equal(t, int64(2), group.ResultRevision)

	group = updateFirmwareRolloutGroupResult(t, queries, groupID, orgID, "successful", true)
	assert.Equal(t, int64(2), group.ResultRevision, "replaying result readiness must be idempotent")
}

func TestTopologyBackfillReconcilesSettledLegacyRolloutBeforeCutover(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Settled legacy reconciliation",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "settled-legacy-reconciliation-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	before, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMiner",
		},
	)
	require.NoError(t, err)

	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Settled legacy rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
		Batches: []rolloutDomain.CreateBatch{{
			Label:   "all",
			Members: []rolloutDomain.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "settled-legacy-rollout",
		Reason:         "exercise pre-cutover reconciliation",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	require.NotNil(t, started.Rollout)
	require.Nil(t, started.Parent)
	require.NotNil(t, started.Rollout.TargetChannelID)
	require.NotNil(t, started.Rollout.TargetReleaseSetID)

	targetChannelID := *started.Rollout.TargetChannelID
	targetReleaseSetID := *started.Rollout.TargetReleaseSetID
	var targetReleaseTargetID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT id
		FROM firmware_release_target
		WHERE release_set_id = $1
		  AND org_id = $2
		  AND lower(btrim(target_manufacturer)) = 'testcorp'
		  AND lower(btrim(target_model)) = 'testminer'
	`, targetReleaseSetID, orgID).Scan(&targetReleaseTargetID))

	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE device_set_membership
		SET device_set_id = $3
		WHERE org_id = $1
		  AND device_identifier = $2
		  AND device_set_type = 'channel'
	`, orgID, deviceIDs[0], targetChannelID)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE rollout_lane
		SET current_channel_id = $3,
		    revision = revision + 1
		WHERE id = $2 AND org_id = $1
	`, orgID, lane.ID, targetChannelID)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND org_id = $1
	`, orgID, started.Rollout.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP,
		    evidence_status = 'finalized',
		    post_window_finalized = TRUE,
		    post_window_finalized_at = CURRENT_TIMESTAMP
		WHERE rollout_id = $2 AND org_id = $1
	`, orgID, started.Rollout.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE firmware_rollout_member
		SET state = 'succeeded',
		    settled_at = CURRENT_TIMESTAMP,
		    owner_released_at = CURRENT_TIMESTAMP
		WHERE rollout_id = $2 AND org_id = $1
	`, orgID, started.Rollout.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE channel_firmware_authority authority
		SET halted_at = CURRENT_TIMESTAMP,
		    revision = authority.revision + 1
		FROM firmware_rollout rollout
		WHERE rollout.id = $2
		  AND rollout.org_id = $1
		  AND authority.id IN (rollout.forward_authority_id, rollout.revert_authority_id)
		  AND authority.org_id = rollout.org_id
		  AND authority.halted_at IS NULL
	`, orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))

	after, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMiner",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "legacy_backfill", after.Origin)
	assert.Equal(t, targetChannelID, after.CurrentChannelID)
	assert.Equal(t, targetReleaseSetID, after.CurrentReleaseSetID)
	assert.Equal(t, targetReleaseTargetID, after.CurrentReleaseTargetID)
	assert.Greater(t, after.Revision, before.Revision)

	var historyCount, activeCount, endedCount int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM rollout_lane_model_channel
		WHERE lane_model_id = $1
	`, after.ID).Scan(&historyCount))
	assert.Equal(t, int64(2), historyCount)
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FILTER (WHERE ended_at IS NULL),
		       COUNT(*) FILTER (WHERE ended_at IS NOT NULL)
		FROM rollout_lane_model_binding
		WHERE lane_model_id = $1 AND device_id = (
			SELECT id FROM device WHERE org_id = $2 AND device_identifier = $3
		)
	`, after.ID, orgID, deviceIDs[0]).Scan(&activeCount, &endedCount))
	assert.Equal(t, int64(1), activeCount)
	assert.Equal(t, int64(1), endedCount)

	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	assert.Zero(t, readiness.AnomalyCount)
	enabled, err := service.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "enable-settled-legacy-reconciliation",
		Reason:           "reconciled legacy rollout is ready",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	assert.True(t, enabled.Readiness.Enabled)
}

func TestTopologyBackfillNeverRewritesTopologyOriginHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Topology origin preservation",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "topology-origin-preservation-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	declaration, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMiner",
		},
	)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane_model
		SET origin = 'topology'
		WHERE id = $1
	`, declaration.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane_model_channel
		SET origin = 'topology'
		WHERE lane_model_id = $1
	`, declaration.ID)
	require.NoError(t, err)

	nextChannelID := createLegacyAttachmentChannel(
		t,
		db,
		orgID,
		lane.CurrentChannelID,
		"topology-origin",
	)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO rollout_lane_channel (lane_id, org_id, channel_id, position)
		VALUES ($1, $2, $3, 1)
	`, lane.ID, orgID, nextChannelID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE rollout_lane
		SET current_channel_id = $3,
		    revision = revision + 1
		WHERE id = $1 AND org_id = $2
	`, lane.ID, orgID, nextChannelID)
	require.NoError(t, err)

	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	after, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMiner",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, declaration.CurrentChannelID, after.CurrentChannelID)
	assert.Equal(t, declaration.CurrentReleaseSetID, after.CurrentReleaseSetID)
	assert.Equal(t, declaration.CurrentReleaseTargetID, after.CurrentReleaseTargetID)
	assert.Equal(t, declaration.Revision, after.Revision)

	var topologyHistoryCount int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM rollout_lane_model_channel
		WHERE lane_model_id = $1 AND origin = 'topology'
	`, declaration.ID).Scan(&topologyHistoryCount))
	assert.Equal(t, int64(1), topologyHistoryCount)
}

func TestRolloutTopologyCompositeIntegrityRejectsCrossWiredSnapshots(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Composite integrity lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "composite-integrity-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = service.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "composite-integrity-enable",
		Reason:           "exercise composite foreign keys",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	lane, err = service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	require.Len(t, lane.Models, 1)
	model := lane.Models[0]

	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Composite integrity child",
		IdempotencyKey: "composite-integrity-parent",
		Reason:         "exercise composite foreign keys",
		ActorUserID:    actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{{
			LaneModelID:           model.ID,
			ExpectedModelRevision: model.Revision,
			FirmwareFileID:        "composite-integrity-target",
			ReleaseTarget:         testLaneTarget("2.0.0", "b"),
			ModelStartKey:         "composite-integrity-child",
			Batches: []rolloutDomain.CreateBatch{{
				Label:   "all",
				Members: []rolloutDomain.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
			}},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, started.Parent)
	require.Len(t, started.Children, 1)
	child := started.Children[0].Child
	require.NotNil(t, child)
	require.NotNil(t, child.TargetChannelID)

	otherGroupID := uuid.New()
	_, err = sqlc.New(db).TestCreateFirmwareRolloutGroup(
		t.Context(),
		sqlc.TestCreateFirmwareRolloutGroupParams{
			GroupID:           otherGroupID,
			LaneID:            lane.ID,
			OrgID:             orgID,
			Name:              "Other composite group",
			IdempotencyKey:    "other-composite-group",
			CreateFingerprint: strings.Repeat("c", 64),
			Reason:            "negative composite child attachment",
			CreatedByUserID:   actorID,
		},
	)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_group_model (
			group_id, lane_id, lane_model_id, org_id, model_identity_key,
			source_channel_id, source_release_set_id, source_release_target_id,
			target_channel_id, target_release_set_id, target_release_target_id,
			snapshot
		)
		SELECT $1, lane_id, lane_model_id, org_id, model_identity_key,
		       source_channel_id, source_release_set_id, source_release_target_id,
		       target_channel_id, target_release_set_id, target_release_target_id,
		       snapshot
		FROM firmware_rollout_group_model
		WHERE group_id = $2 AND lane_model_id = $3 AND org_id = $4
	`, otherGroupID, started.Parent.ID, model.ID, orgID)
	require.NoError(t, err)

	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		UPDATE firmware_rollout_group_model
		SET child_rollout_id = NULL
		WHERE group_id = $1 AND lane_model_id = $2 AND org_id = $3
	`, started.Parent.ID, model.ID, orgID)
	require.NoError(t, err)
	_, attachErr := tx.ExecContext(t.Context(), `
		UPDATE firmware_rollout_group_model
		SET child_rollout_id = $1
		WHERE group_id = $2 AND lane_model_id = $3 AND org_id = $4
	`, child.ID, otherGroupID, model.ID, orgID)
	require.NoError(t, tx.Rollback())
	require.Error(t, attachErr, "a child cannot attach to a different group snapshot")
	assert.Contains(t, attachErr.Error(), "fk_firmware_rollout_group_model_child")

	tx, err = db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `
		DELETE FROM rollout_lane_model_channel
		WHERE lane_model_id = $1 AND channel_id = $2
	`, model.ID, *child.TargetChannelID)
	require.NoError(t, err)
	_, mismatchErr := tx.ExecContext(t.Context(), `
		INSERT INTO rollout_lane_model_channel (
			lane_model_id, lane_id, org_id, channel_id,
			release_set_id, release_target_id, position, origin
		)
		VALUES ($1, $2, $3, $4, $5, $6, 99, 'topology')
	`, model.ID, lane.ID, orgID, *child.TargetChannelID,
		model.CurrentFirmwareTarget.ReleaseSetID, model.CurrentFirmwareTarget.ReleaseTargetID)
	require.NoError(t, tx.Rollback())
	require.Error(t, mismatchErr, "a channel snapshot cannot name another channel's release set")
	assert.Contains(t, mismatchErr.Error(), "fk_rollout_lane_model_channel_physical_release")
}

func TestRolloutLaneTopologyMigrationDownRefusesAfterAdminHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Topology rollback guard lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "topology-rollback-guard-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)

	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	declaration, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMiner",
		},
	)
	require.NoError(t, err)
	const operationKey = "topology-rollback-guard-repair"
	_, err = service.RepairModelBinding(t.Context(), betweenchannel.RepairModelBindingRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		LaneModelID:      declaration.ID,
		DeviceIdentifier: deviceIDs[0],
		ExpectedRevision: declaration.Revision,
		IdempotencyKey:   operationKey,
		Reason:           "create immutable topology administration history",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)

	before, err := queries.GetRolloutLaneTopologyAdminOperationByKey(
		t.Context(),
		sqlc.GetRolloutLaneTopologyAdminOperationByKeyParams{
			OrgID:          orgID,
			IdempotencyKey: operationKey,
		},
	)
	require.NoError(t, err)

	downSQL, err := migrations.Migrations.ReadFile("000156_multi_model_rollout_topology.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(downSQL))
	require.ErrorContains(t, err, "cannot downgrade after rollout lane model topology history exists")

	after, err := queries.GetRolloutLaneTopologyAdminOperationByKey(
		t.Context(),
		sqlc.GetRolloutLaneTopologyAdminOperationByKeyParams{
			OrgID:          orgID,
			IdempotencyKey: operationKey,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the rejected down migration must preserve immutable history")
}

func TestMigrateDownFrom160FailsBeforeDestroyingTopologyOrCancellationHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Actual migrate-down guard",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "actual-migrate-down-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = service.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "actual-migrate-down-enable",
		Reason:           "create immutable admin history",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	lane, err = service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	require.Len(t, lane.Models, 1)

	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Actual migrate-down child",
		IdempotencyKey: "actual-migrate-down-parent",
		Reason:         "create group and child history",
		ActorUserID:    actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{{
			LaneModelID:           lane.Models[0].ID,
			ExpectedModelRevision: lane.Models[0].Revision,
			FirmwareFileID:        "actual-migrate-down-target",
			ReleaseTarget:         testLaneTarget("2.0.0", "b"),
			ModelStartKey:         "actual-migrate-down-child",
			Batches: []rolloutDomain.CreateBatch{{
				Label:   "all",
				Members: []rolloutDomain.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
			}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, started.Children, 1)
	child := started.Children[0].Child
	require.NotNil(t, child)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'cancelled',
		    evidence_cancellation_reason = 'rollback guard cancellation history',
		    evidence_cancelled_at = CURRENT_TIMESTAMP,
		    post_window_finalized = TRUE,
		    post_window_finalized_at = CURRENT_TIMESTAMP
		WHERE rollout_id = $1 AND org_id = $2
	`, child.ID, orgID)
	require.NoError(t, err)

	migration := newEmbeddedMigration(t, db)
	version, dirty, err := migration.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(161), version)
	assert.False(t, dirty)

	require.NoError(t, migration.Steps(-1), "the additive accumulator migration must roll back cleanly first")
	version, dirty, err = migration.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(160), version)
	assert.False(t, dirty)

	err = migration.Steps(-1)
	require.ErrorContains(t, err, "cannot downgrade after rollout topology or cancellation history exists")
	version, dirty, versionErr := migration.Version()
	require.NoError(t, versionErr)
	assert.Equal(t, uint(159), version)
	assert.True(t, dirty, "golang-migrate must expose the failed down as dirty")

	var historyIntact bool
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT to_regclass('firmware_rollout_group') IS NOT NULL
		   AND to_regclass('rollout_lane_topology_admin_operation') IS NOT NULL
		   AND EXISTS (
		       SELECT 1
		       FROM information_schema.columns
		       WHERE table_schema = current_schema()
		         AND table_name = 'firmware_rollout'
		         AND column_name = 'lane_model_id'
		   )
		   AND EXISTS (
		       SELECT 1
		       FROM information_schema.columns
		       WHERE table_schema = current_schema()
		         AND table_name = 'firmware_rollout_batch'
		         AND column_name = 'evidence_cancellation_reason'
		   )
	`).Scan(&historyIntact))
	assert.True(t, historyIntact, "160.down must fail before any 160 or 159 mutation")

	require.NoError(t, migration.Force(160))
	version, dirty, err = migration.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(160), version)
	assert.False(t, dirty, "the test must leave migration metadata recoverable")
}

func TestRolloutLaneTopologyMigrationBlocksSeededLegacyAnomaliesUntilAuditedCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID,
		Label: "Pre-migration anomaly lane",
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			testLaneTargetForModel("TestMinerB", "1.0.0", "b"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "pre-migration-anomaly-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	legacyHistoryBefore := rolloutLaneChannelHistoryJSON(t, db, lane.ID, orgID)

	for _, migration := range []string{
		"000160_rollout_evidence_cancellation.down.sql",
		"000159_rollout_model_child_runtime.down.sql",
		"000158_discovered_model_identity_index.down.sql",
		"000157_multi_model_rollout_indexes.down.sql",
		"000156_multi_model_rollout_topology.down.sql",
	} {
		executeEmbeddedMigration(t, db, migration)
	}
	var isPreTopologySchema bool
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT to_regclass('rollout_lane_model') IS NULL
		   AND NOT EXISTS (
		       SELECT 1
		       FROM information_schema.columns
		       WHERE table_schema = current_schema()
		         AND table_name = 'discovered_device'
		         AND column_name = 'model_identity_observed_at'
		   )
	`).Scan(&isPreTopologySchema))
	require.True(t, isPreTopologySchema, "legacy anomalies must be seeded against the pre-000156 schema")

	_, err = db.ExecContext(t.Context(), `
		UPDATE discovered_device
		SET model = NULL
		WHERE org_id = $1 AND device_identifier = $2
	`, orgID, deviceIDs[0])
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_release_target (
			release_set_id,
			org_id,
			firmware_file_id,
			target_manufacturer,
			target_model,
			firmware_version,
			sha256
		)
		SELECT channel.release_set_id,
		       history.org_id,
		       'ambiguous-legacy-model-b',
		       ' TestCorp ',
		       ' TestMinerB ',
		       '1.0.0',
		       $3
		FROM rollout_lane_channel history
		JOIN rollout_lane lane
		  ON lane.id = history.lane_id
		 AND lane.org_id = history.org_id
		 AND lane.current_channel_id = history.channel_id
		JOIN device_set_channel channel
		  ON channel.device_set_id = history.channel_id
		 AND channel.org_id = history.org_id
		WHERE history.lane_id = $1
		  AND history.org_id = $2
	`, lane.ID, orgID, strings.Repeat("c", 64))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uq_firmware_release_target_model")

	_, err = db.ExecContext(t.Context(), `DROP INDEX uq_firmware_release_target_model`)
	require.NoError(t, err)
	var ambiguousTargetID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO firmware_release_target (
			release_set_id,
			org_id,
			firmware_file_id,
			target_manufacturer,
			target_model,
			firmware_version,
			sha256
		)
		SELECT channel.release_set_id,
		       history.org_id,
		       'ambiguous-partial-feature-model-b',
		       ' TestCorp ',
		       ' TestMinerB ',
		       '1.0.0',
		       $3
		FROM rollout_lane_channel history
		JOIN rollout_lane lane
		  ON lane.id = history.lane_id
		 AND lane.org_id = history.org_id
		 AND lane.current_channel_id = history.channel_id
		JOIN device_set_channel channel
		  ON channel.device_set_id = history.channel_id
		 AND channel.org_id = history.org_id
		WHERE history.lane_id = $1
		  AND history.org_id = $2
		RETURNING id
	`, lane.ID, orgID, strings.Repeat("c", 64)).Scan(&ambiguousTargetID))

	for _, migration := range []string{
		"000156_multi_model_rollout_topology.up.sql",
		"000157_multi_model_rollout_indexes.up.sql",
		"000158_discovered_model_identity_index.up.sql",
		"000159_rollout_model_child_runtime.up.sql",
		"000160_rollout_evidence_cancellation.up.sql",
	} {
		executeEmbeddedMigration(t, db, migration)
	}
	assert.Equal(t, legacyHistoryBefore, rolloutLaneChannelHistoryJSON(t, db, lane.ID, orgID))

	service = betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	assert.False(t, readiness.Enabled)
	assert.Equal(t, int64(2), readiness.AnomalyCount)
	assertTopologyAnomalyTypes(
		t,
		readiness,
		betweenchannel.TopologyAnomalyNullIdentity,
		betweenchannel.TopologyAnomalyAmbiguousTargetMatch,
	)

	setDiscoveredModel(t, db, orgID, deviceIDs[0], "TestMiner")
	readiness, err = service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), readiness.AnomalyCount, "ordinary readiness reads must not refresh persisted backfill state")
	assertTopologyAnomalyTypes(
		t,
		readiness,
		betweenchannel.TopologyAnomalyMissingBinding,
		betweenchannel.TopologyAnomalyAmbiguousTargetMatch,
	)

	queries := sqlc.New(db)
	_, err = db.ExecContext(t.Context(), `
		ALTER TABLE firmware_release_target
		    DISABLE TRIGGER firmware_release_target_immutable
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		DELETE FROM firmware_release_target WHERE id = $1
	`, ambiguousTargetID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		ALTER TABLE firmware_release_target
		    ENABLE TRIGGER firmware_release_target_immutable
	`)
	require.NoError(t, err)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))

	readiness, err = service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	assert.Zero(t, readiness.AnomalyCount)
	assert.Empty(t, readiness.Anomalies)
	assert.Equal(t, legacyHistoryBefore, rolloutLaneChannelHistoryJSON(t, db, lane.ID, orgID))

	enableRequest := betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "enable-cleaned-seeded-topology",
		Reason:           "specific ambiguous target was removed by audited cleanup",
		ActorUserID:      actorID,
	}
	enabled, err := service.EnableTopology(t.Context(), enableRequest)
	require.NoError(t, err)
	assert.True(t, enabled.Readiness.Enabled)
	assert.False(t, enabled.Replayed)

	replayed, err := service.EnableTopology(t.Context(), enableRequest)
	require.NoError(t, err)
	assert.True(t, replayed.Readiness.Enabled)
	assert.True(t, replayed.Replayed)
	assert.Equal(t, enabled.Readiness.Revision, replayed.Readiness.Revision)
	assert.Equal(t, legacyHistoryBefore, rolloutLaneChannelHistoryJSON(t, db, lane.ID, orgID))
}

func TestCompletedLegacyMixedRolloutStaysParentlessAcrossTopologyMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(
		t.Context(),
		rolloutCreateRequest(t, db, orgID, "u7-completed-legacy-mixed", [][]string{deviceIDs}),
	)
	require.NoError(t, err)
	require.NotNil(t, created.Rollout)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND org_id = $2
	`, created.Rollout.ID, orgID)
	require.NoError(t, err)

	for _, migration := range []string{
		"000160_rollout_evidence_cancellation.down.sql",
		"000159_rollout_model_child_runtime.down.sql",
		"000158_discovered_model_identity_index.down.sql",
		"000157_multi_model_rollout_indexes.down.sql",
		"000156_multi_model_rollout_topology.down.sql",
		"000156_multi_model_rollout_topology.up.sql",
		"000157_multi_model_rollout_indexes.up.sql",
		"000158_discovered_model_identity_index.up.sql",
		"000159_rollout_model_child_runtime.up.sql",
		"000160_rollout_evidence_cancellation.up.sql",
	} {
		executeEmbeddedMigration(t, db, migration)
	}

	restartedStore := sqlstores.NewSQLRolloutStore(db)
	parents, err := restartedStore.ListGroups(t.Context(), orgID)
	require.NoError(t, err)
	assert.Empty(t, parents, "migration must not synthesize an aggregate parent for legacy history")

	legacyHistory, err := restartedStore.List(t.Context(), orgID, []rolloutDomain.State{
		rolloutDomain.StateCompleted,
	})
	require.NoError(t, err)
	require.Len(t, legacyHistory, 1)
	assert.Equal(t, created.Rollout.ID, legacyHistory[0].ID)
	assert.Nil(t, legacyHistory[0].GroupID)
	assert.Nil(t, legacyHistory[0].LaneModelID)
	assert.Empty(t, legacyHistory[0].ModelIdentityKey)
	require.Len(t, legacyHistory[0].Members, 2)
}

func updateFirmwareRolloutGroupResult(
	t *testing.T,
	queries *sqlc.Queries,
	groupID uuid.UUID,
	orgID int64,
	terminalOutcome string,
	resultReady bool,
) sqlc.FirmwareRolloutGroup {
	t.Helper()
	group, err := queries.TestUpdateFirmwareRolloutGroupResult(
		t.Context(),
		sqlc.TestUpdateFirmwareRolloutGroupResultParams{
			TerminalOutcome: sql.NullString{String: terminalOutcome, Valid: true},
			ResultReady:     resultReady,
			GroupID:         groupID,
			OrgID:           orgID,
		},
	)
	require.NoError(t, err)
	return group
}

func executeEmbeddedMigration(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	contents, err := migrations.Migrations.ReadFile(name)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(contents))
	require.NoError(t, err, "executing %s", name)
}

func newEmbeddedMigration(t *testing.T, db *sql.DB) *migrate.Migrate {
	t.Helper()
	source, err := iofs.New(migrations.Migrations, ".")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	require.NoError(t, err)
	instance, err := migrate.NewWithInstance("migration-test", source, "migration-test", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := instance.Close()
		assert.NoError(t, sourceErr)
		assert.NoError(t, databaseErr)
	})
	return instance
}

func rolloutLaneChannelHistoryJSON(
	t *testing.T,
	db *sql.DB,
	laneID uuid.UUID,
	orgID int64,
) string {
	t.Helper()
	var history string
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COALESCE(jsonb_agg(to_jsonb(entry) ORDER BY entry.channel_id), '[]'::jsonb)::text
		FROM rollout_lane_channel entry
		WHERE entry.lane_id = $1
		  AND entry.org_id = $2
	`, laneID, orgID).Scan(&history))
	return history
}

func assertTopologyAnomalyTypes(
	t *testing.T,
	readiness betweenchannel.TopologyReadiness,
	expected ...betweenchannel.TopologyAnomalyType,
) {
	t.Helper()
	actual := make([]betweenchannel.TopologyAnomalyType, 0, len(readiness.Anomalies))
	for _, anomaly := range readiness.Anomalies {
		actual = append(actual, anomaly.Type)
	}
	assert.ElementsMatch(t, expected, actual)
}
