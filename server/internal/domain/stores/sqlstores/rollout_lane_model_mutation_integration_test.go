package sqlstores_test

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func memberIdentifiers(members []betweenchannel.LaneMember) []string {
	result := make([]string, 0, len(members))
	for _, member := range members {
		result = append(result, member.DeviceIdentifier)
	}
	return result
}

func TestRolloutLaneModelPhysicalAndBindingWritesMustRemainAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane := createMembershipTestLane(t, service, orgID, actorID, "Atomic topology guard", deviceIDs)
	lane = enableModelTopologyForMutationTest(t, service, lane, orgID, actorID, "atomic-topology")
	model := laneModelByName(t, lane, "TestMiner")

	physicalOnly, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = physicalOnly.ExecContext(t.Context(), `
		DELETE FROM device_set_membership
		WHERE org_id = $1 AND device_set_id = $2 AND device_identifier = $3
	`, orgID, model.CurrentChannelID, deviceIDs[0])
	require.NoError(t, err)
	err = physicalOnly.Commit()
	require.ErrorContains(t, err, "active rollout lane model binding requires matching physical membership")

	bindingOnly, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	operationID := uuid.New()
	_, err = bindingOnly.ExecContext(t.Context(), `
		INSERT INTO rollout_lane_topology_admin_operation (
		    id, org_id, operation, lane_id, lane_model_id, idempotency_key,
		    request_fingerprint, expected_revision, resulting_revision, reason,
		    requested, applied, actor_user_id, actor_type
		)
		VALUES ($1, $2, 'update_membership', $3, $4, $5, repeat('a', 64),
		        $6::bigint, $6::bigint + 1, 'invalid binding-only test', '{}'::jsonb, '{}'::jsonb,
		        $7, 'user')
	`, operationID, orgID, lane.ID, model.ID, "binding-only-guard", model.Revision, actorID)
	require.NoError(t, err)
	_, err = bindingOnly.ExecContext(t.Context(), `
		UPDATE rollout_lane_model_binding
		SET ended_at = CURRENT_TIMESTAMP,
		    ended_by_operation_id = $1,
		    revision = revision + 1
		WHERE org_id = $2 AND lane_model_id = $3 AND ended_at IS NULL
	`, operationID, orgID, model.ID)
	require.NoError(t, err)
	err = bindingOnly.Commit()
	require.ErrorContains(t, err, "ended rollout lane model binding cannot retain physical membership")

	reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), laneModelByName(t, reloaded, "TestMiner").MemberCount)
	assert.Equal(t, model.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))
}

func TestRolloutLaneModelMembershipConcurrencyIsDeclarationScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("same model has one winner", func(t *testing.T) {
		db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
		actorID := testOrganizationUserID(t, db, orgID)
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane := createMembershipTestLane(t, service, orgID, actorID, "Same model concurrency", deviceIDs[:1])
		lane = enableModelTopologyForMutationTest(t, service, lane, orgID, actorID, "same-model")
		model := laneModelByName(t, lane, "TestMiner")

		start := make(chan struct{})
		results := make(chan error, 2)
		var wait sync.WaitGroup
		for index, identifier := range deviceIDs[1:] {
			wait.Add(1)
			go func(index int, identifier string) {
				defer wait.Done()
				<-start
				_, err := service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
					OrgID: orgID, LaneID: lane.ID, LaneModelID: model.ID,
					ExpectedRevision: model.Revision, AddIdentifiers: []string{identifier},
					IdempotencyKey: "same-model-concurrent-" + string(rune('a'+index)),
					Reason:         "prove declaration concurrency", ActorUserID: actorID,
					ActorType: rollout.ActorTypeUser,
				})
				results <- err
			}(index, identifier)
		}
		close(start)
		wait.Wait()
		close(results)
		var succeeded, conflicted int
		for err := range results {
			if err == nil {
				succeeded++
			} else if errors.Is(err, betweenchannel.ErrDeclarationConflict) {
				conflicted++
			} else {
				require.NoError(t, err)
			}
		}
		assert.Equal(t, 1, succeeded)
		assert.Equal(t, 1, conflicted)
	})

	t.Run("disjoint models both succeed", func(t *testing.T) {
		db, orgID, deviceIDs := setupRolloutLaneTestData(t, 4)
		actorID := testOrganizationUserID(t, db, orgID)
		setDiscoveredModel(t, db, orgID, deviceIDs[2], "Antminer S21")
		setDiscoveredModel(t, db, orgID, deviceIDs[3], "Antminer S21")
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane := createMembershipTestLane(t, service, orgID, actorID, "Disjoint model concurrency", deviceIDs[:1])
		lane = enableModelTopologyForMutationTest(t, service, lane, orgID, actorID, "disjoint-model")
		declared, err := service.CreateModelDeclaration(t.Context(), betweenchannel.CreateModelDeclarationRequest{
			OrgID: orgID, LaneID: lane.ID, ExpectedRevision: 0,
			ReleaseTargets: []betweenchannel.ReleaseTarget{
				testLaneTargetForModel("Antminer S21", "1.0.0", "d"),
			},
			DeviceIdentifiers: []string{deviceIDs[2]},
			IdempotencyKey:    "declare-disjoint-antminer", Reason: "declare disjoint model",
			ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
		})
		require.NoError(t, err)
		protoModel := laneModelByName(t, declared, "TestMiner")
		antminerModel := laneModelByName(t, declared, "Antminer S21")

		requests := []betweenchannel.UpdateModelMembershipRequest{
			{
				OrgID: orgID, LaneID: lane.ID, LaneModelID: protoModel.ID,
				ExpectedRevision: protoModel.Revision, AddIdentifiers: []string{deviceIDs[1]},
				IdempotencyKey: "disjoint-proto-add", Reason: "add Proto",
				ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
			},
			{
				OrgID: orgID, LaneID: lane.ID, LaneModelID: antminerModel.ID,
				ExpectedRevision: antminerModel.Revision, AddIdentifiers: []string{deviceIDs[3]},
				IdempotencyKey: "disjoint-antminer-add", Reason: "add Antminer",
				ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
			},
		}
		start := make(chan struct{})
		results := make(chan error, len(requests))
		var wait sync.WaitGroup
		for _, request := range requests {
			wait.Add(1)
			go func(request betweenchannel.UpdateModelMembershipRequest) {
				defer wait.Done()
				<-start
				_, err := service.UpdateModelMembership(t.Context(), request)
				results <- err
			}(request)
		}
		close(start)
		wait.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
		require.NoError(t, err)
		assert.Equal(t, int32(2), laneModelByName(t, reloaded, "TestMiner").MemberCount)
		assert.Equal(t, int32(2), laneModelByName(t, reloaded, "Antminer S21").MemberCount)
		listed, err := service.ListLanes(t.Context(), orgID, false)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, *reloaded, listed[0], "bulk lane hydration must preserve the detailed response shape")

		protoMembers, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
			OrgID: orgID, LaneID: lane.ID, LaneModelID: protoModel.ID, Limit: 100,
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, deviceIDs[:2], memberIdentifiers(protoMembers.Members))

		antminerMembers, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
			OrgID: orgID, LaneID: lane.ID, LaneModelID: antminerModel.ID, Limit: 100,
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, deviceIDs[2:], memberIdentifiers(antminerMembers.Members))

		allMembers, err := service.ListMembers(t.Context(), betweenchannel.ListMembersRequest{
			OrgID: orgID, LaneID: lane.ID, Limit: 100,
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, deviceIDs, memberIdentifiers(allMembers.Members))
	})

	t.Run("active child blocks only its model", func(t *testing.T) {
		db, orgID, deviceIDs := setupRolloutLaneTestData(t, 4)
		actorID := testOrganizationUserID(t, db, orgID)
		setDiscoveredModel(t, db, orgID, deviceIDs[2], "Antminer S21")
		setDiscoveredModel(t, db, orgID, deviceIDs[3], "Antminer S21")
		service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
		lane := createMembershipTestLane(t, service, orgID, actorID, "Model child membership gate", deviceIDs[:1])
		lane = enableModelTopologyForMutationTest(t, service, lane, orgID, actorID, "model-child-gate")
		declared, err := service.CreateModelDeclaration(t.Context(), betweenchannel.CreateModelDeclarationRequest{
			OrgID: orgID, LaneID: lane.ID, ExpectedRevision: 0,
			ReleaseTargets: []betweenchannel.ReleaseTarget{
				testLaneTargetForModel("Antminer S21", "1.0.0", "e"),
			},
			DeviceIdentifiers: []string{deviceIDs[2]},
			IdempotencyKey:    "declare-child-gate-antminer", Reason: "declare sibling model",
			ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
		})
		require.NoError(t, err)
		protoModel := laneModelByName(t, declared, "TestMiner")
		antminerModel := laneModelByName(t, declared, "Antminer S21")
		seedActiveModelChild(t, db, orgID, actorID, lane.ID, protoModel)

		_, err = service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
			OrgID: orgID, LaneID: lane.ID, LaneModelID: protoModel.ID,
			ExpectedRevision: protoModel.Revision, AddIdentifiers: []string{deviceIDs[1]},
			IdempotencyKey: "blocked-proto-child", Reason: "same model child active",
			ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
		})
		require.ErrorIs(t, err, betweenchannel.ErrModelWorkActive)

		updated, err := service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
			OrgID: orgID, LaneID: lane.ID, LaneModelID: antminerModel.ID,
			ExpectedRevision: antminerModel.Revision, AddIdentifiers: []string{deviceIDs[3]},
			IdempotencyKey: "allowed-antminer-sibling", Reason: "disjoint model remains writable",
			ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
		})
		require.NoError(t, err)
		assert.Equal(t, int32(2), laneModelByName(t, updated.Lane, "Antminer S21").MemberCount)
	})
}

func TestRolloutLaneModelDeclarationMembershipAndZeroMemberPublication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "Antminer S21")
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Model mutation lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs[:1],
		IdempotencyKey:    "model-mutation-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	enabled, err := service.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "enable-model-mutation-lane",
		Reason:           "exercise authoritative model writes",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	require.Zero(t, enabled.Readiness.AnomalyCount)

	target := testLaneTargetForModel("Antminer S21", "1.0.0", "b")
	createRequest := betweenchannel.CreateModelDeclarationRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		ExpectedRevision: 0,
		ReleaseTargets:   []betweenchannel.ReleaseTarget{target},
		IdempotencyKey:   "declare-antminer-s21",
		Reason:           "declare Antminer without miners",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}
	declared, err := service.CreateModelDeclaration(t.Context(), createRequest)
	require.NoError(t, err)
	require.Len(t, declared.Models, 2)
	antminer := laneModelByName(t, declared, "Antminer S21")
	assert.Zero(t, antminer.MemberCount)
	assert.Equal(t, int64(1), antminer.Revision)

	replayed, err := service.CreateModelDeclaration(t.Context(), createRequest)
	require.NoError(t, err)
	assert.Equal(t, declared.ID, replayed.ID)
	assert.Equal(t, antminer.ID, laneModelByName(t, replayed, "Antminer S21").ID)

	_, err = service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
		OrgID: orgID, LaneID: lane.ID, LaneModelID: antminer.ID,
		ExpectedRevision: antminer.Revision, AddIdentifiers: []string{deviceIDs[0]},
		ConfirmReassign: true, IdempotencyKey: "reject-proto-as-antminer",
		Reason: "reject canonical model mismatch", ActorUserID: actorID,
		ActorType: rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)

	added, err := service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		LaneModelID:      antminer.ID,
		ExpectedRevision: antminer.Revision,
		AddIdentifiers:   []string{deviceIDs[1]},
		IdempotencyKey:   "add-antminer-s21",
		Reason:           "add compatible Antminer",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	})
	require.NoError(t, err)
	antminer = laneModelByName(t, added.Lane, "Antminer S21")
	assert.Equal(t, int32(1), antminer.MemberCount)
	assert.Equal(t, int64(2), antminer.Revision)
	assert.Equal(t, antminer.CurrentChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))

	removed, err := service.UpdateModelMembership(t.Context(), betweenchannel.UpdateModelMembershipRequest{
		OrgID:             orgID,
		LaneID:            lane.ID,
		LaneModelID:       antminer.ID,
		ExpectedRevision:  antminer.Revision,
		RemoveIdentifiers: []string{deviceIDs[1]},
		IdempotencyKey:    "remove-antminer-s21",
		Reason:            "end Antminer binding",
		ActorUserID:       actorID,
		ActorType:         rollout.ActorTypeUser,
	})
	require.NoError(t, err)
	antminer = laneModelByName(t, removed.Lane, "Antminer S21")
	assert.Zero(t, antminer.MemberCount)
	assert.Equal(t, int64(3), antminer.Revision)
	assert.Equal(t, int64(1), antminer.Bindings.HistoricalCount)

	publishedTarget := testLaneTargetForModel("Antminer S21", "2.0.0", "c")
	publishRequest := betweenchannel.PublishModelTargetRequest{
		OrgID:            orgID,
		LaneID:           lane.ID,
		LaneModelID:      antminer.ID,
		ExpectedRevision: antminer.Revision,
		ReleaseTargets:   []betweenchannel.ReleaseTarget{publishedTarget},
		IdempotencyKey:   "publish-empty-antminer-s21",
		Reason:           "publish empty Antminer target",
		ActorUserID:      actorID,
		ActorType:        rollout.ActorTypeUser,
	}
	published, err := service.PublishModelTarget(t.Context(), publishRequest)
	require.NoError(t, err)
	antminer = laneModelByName(t, published, "Antminer S21")
	assert.Equal(t, int64(4), antminer.Revision)
	require.NotNil(t, antminer.CurrentFirmwareTarget)
	assert.Equal(t, "2.0.0", antminer.CurrentFirmwareTarget.FirmwareVersion)
	require.Len(t, antminer.Channels, 2)

	replayedPublish, err := service.PublishModelTarget(t.Context(), publishRequest)
	require.NoError(t, err)
	assert.Equal(t, antminer.CurrentChannelID, laneModelByName(t, replayedPublish, "Antminer S21").CurrentChannelID)

	_, err = service.UpdateMembership(t.Context(), betweenchannel.UpdateMembershipRequest{
		OrgID: orgID, LaneID: lane.ID, ExpectedRevision: published.Revision,
		AddIdentifiers: []string{deviceIDs[1]}, IdempotencyKey: "reject-divergent-flat-membership",
		Reason:      "legacy flat write must stop after pointer divergence",
		ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrScalarProjectionUnavailable)

	var parents, rollouts, evidence int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT
		    (SELECT COUNT(*) FROM firmware_rollout_group WHERE org_id = $1 AND lane_id = $2),
		    (SELECT COUNT(*) FROM firmware_rollout WHERE org_id = $1),
		    (SELECT COUNT(*) FROM firmware_rollout_evidence WHERE org_id = $1)
	`, orgID, lane.ID).Scan(&parents, &rollouts, &evidence))
	assert.Zero(t, parents)
	assert.Zero(t, rollouts)
	assert.Zero(t, evidence)

	_, err = service.PublishModelTarget(t.Context(), betweenchannel.PublishModelTargetRequest{
		OrgID: orgID, LaneID: lane.ID, LaneModelID: antminer.ID,
		ExpectedRevision: 3, ReleaseTargets: []betweenchannel.ReleaseTarget{publishedTarget},
		IdempotencyKey: "stale-empty-antminer-s21", Reason: "stale writer",
		ActorUserID: actorID, ActorType: rollout.ActorTypeUser,
	})
	require.ErrorIs(t, err, betweenchannel.ErrDeclarationConflict)

	counts, err := sqlc.New(db).GetRolloutLaneTopologyCountsForTest(
		t.Context(),
		sqlc.GetRolloutLaneTopologyCountsForTestParams{LaneID: lane.ID, OrgID: orgID},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), counts.Declarations)
	assert.Equal(t, int64(3), counts.HistoryRows)
}

func laneModelByName(
	t *testing.T,
	lane *betweenchannel.Lane,
	model string,
) betweenchannel.LaneModel {
	t.Helper()
	for _, declaration := range lane.Models {
		if strings.EqualFold(declaration.Model, model) {
			return declaration
		}
	}
	t.Fatalf("rollout lane model %q not found", model)
	return betweenchannel.LaneModel{}
}

func enableModelTopologyForMutationTest(
	t *testing.T,
	service *betweenchannel.Service,
	lane *betweenchannel.Lane,
	orgID int64,
	actorID int64,
	suffix string,
) *betweenchannel.Lane {
	t.Helper()
	readiness, err := service.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	enabled, err := service.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "enable-" + suffix, Reason: "enable model mutation test",
		ActorUserID: actorID,
	})
	require.NoError(t, err)
	require.Zero(t, enabled.Readiness.AnomalyCount)
	reloaded, err := service.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	return reloaded
}

func seedActiveModelChild(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	actorID int64,
	laneID uuid.UUID,
	model betweenchannel.LaneModel,
) {
	t.Helper()
	authorityID := uuid.New()
	childID := uuid.New()
	groupID := uuid.New()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO channel_firmware_authority (
		    id, org_id, authority_type, authority_reference, created_by_user_id
		)
		VALUES ($1, $2, 'rollout', $3, $4)
	`, authorityID, orgID, childID.String(), actorID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout (
		    id, org_id, name, strategy_key, state, forward_authority_id,
		    forward_authority_revision, source_channel_id, target_channel_id,
		    source_release_set_id, target_release_set_id, idempotency_key,
		    create_fingerprint, reason, created_by_user_id
		)
		VALUES ($1, $2, 'Seeded active child', 'between_channel', 'running', $3,
		        1, $4, $4, $5, $5, $6, repeat('b', 64), 'membership gate test', $7)
	`, childID, orgID, authorityID, model.CurrentChannelID, model.CurrentReleaseSetID,
		"seed-active-child-"+childID.String(), actorID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_group (
		    id, lane_id, org_id, name, idempotency_key, create_fingerprint,
		    reason, created_by_user_id, actor_type
		)
		VALUES ($1, $2, $3, 'Seeded parent', $4, repeat('c', 64),
		        'membership gate test', $5, 'user')
	`, groupID, laneID, orgID, "seed-parent-"+groupID.String(), actorID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_group_model (
		    group_id, lane_id, lane_model_id, org_id, model_identity_key,
		    source_channel_id, source_release_set_id, source_release_target_id,
		    target_channel_id, target_release_set_id, target_release_target_id,
		    child_rollout_id, snapshot
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6, $7, $8, $9, '{}'::jsonb)
	`, groupID, laneID, model.ID, orgID, model.ModelIdentityKey, model.CurrentChannelID,
		model.CurrentReleaseSetID, model.CurrentReleaseTargetID, childID)
	require.NoError(t, err)
}
