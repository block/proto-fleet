package sqlstores_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/migrations"
)

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

func TestRolloutLaneTopologyMigrationToleratesAndRepairsSeededLegacyAnomalies(t *testing.T) {
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
	require.NoError(t, err)

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
	declaration, err := queries.GetRolloutLaneModelForTest(
		t.Context(),
		sqlc.GetRolloutLaneModelForTestParams{
			LaneID:       lane.ID,
			OrgID:        orgID,
			Manufacturer: "TestCorp",
			Model:        "TestMinerB",
		},
	)
	require.NoError(t, err)
	_, err = service.RepairModelBinding(t.Context(), betweenchannel.RepairModelBindingRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		LaneModelID:      declaration.ID,
		DeviceIdentifier: deviceIDs[1],
		ExpectedRevision: declaration.Revision,
		IdempotencyKey:   "repair-seeded-ambiguous-model",
		Reason:           "select the canonical declaration for the ambiguous legacy target",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)

	readiness, err = service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	assert.Zero(t, readiness.AnomalyCount)
	assert.Empty(t, readiness.Anomalies)
	assert.Equal(t, legacyHistoryBefore, rolloutLaneChannelHistoryJSON(t, db, lane.ID, orgID))

	enableRequest := betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "enable-repaired-seeded-topology",
		Reason:           "all migration anomalies are repaired",
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
