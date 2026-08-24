package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestStartRolloutLaneCreatesOneModelChildAndLeavesSiblingUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:       orgID,
		Label:       "One model child",
		Description: "U4 mixed lane",
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			testLaneTargetForModel("TestMinerB", "1.0.0", "b"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "u4-create-mixed-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID:            orgID,
		ExpectedRevision: readiness.Revision,
		IdempotencyKey:   "u4-enable-topology",
		Reason:           "start model child",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	protoModel := laneModelByName(t, lane, "TestMiner")
	antminerModel := laneModelByName(t, lane, "TestMinerB")
	antminerChannel := antminerModel.CurrentChannelID
	antminerRevision := antminerModel.Revision
	target := testLaneTargetForModel("TestMiner", "2.0.0", "c")

	start := betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Proto only",
		IdempotencyKey: "u4-parent-start",
		Reason:         "update Proto only",
		ActorUserID:    actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{{
			LaneModelID:           protoModel.ID,
			ExpectedModelRevision: protoModel.Revision,
			FirmwareFileID:        target.FirmwareFileID,
			ReleaseTarget:         target,
			ModelStartKey:         "u4-proto-child-start",
			Batches: []rollout.CreateBatch{{
				Label: "all",
				Members: []rollout.CreateMember{{
					DeviceIdentifier: deviceIDs[0],
				}},
			}},
		}},
	}
	started, err := laneService.StartRollout(t.Context(), start)
	require.NoError(t, err)
	require.NotNil(t, started.Parent)
	require.Len(t, started.Children, 1)
	require.NotNil(t, started.Children[0].Child)
	assert.Positive(t, started.Children[0].FirstBatchID)
	assert.Equal(t, started.Parent.ID, *started.Children[0].Child.GroupID)
	assert.Equal(t, protoModel.ID, *started.Children[0].Child.LaneModelID)
	assert.Equal(t, protoModel.ModelIdentityKey, started.Children[0].Child.ModelIdentityKey)

	replayed, err := laneService.StartRollout(t.Context(), start)
	require.NoError(t, err)
	assert.Equal(t, started.Parent.ID, replayed.Parent.ID)
	assert.Equal(t, started.Children[0].Child.ID, replayed.Children[0].Child.ID)
	assert.Equal(t, started.Children[0].FirstBatchID, replayed.Children[0].FirstBatchID)

	reloaded, err := laneService.GetLane(t.Context(), orgID, lane.ID, false, nil)
	require.NoError(t, err)
	reloadedAntminer := laneModelByName(t, reloaded, "TestMinerB")
	assert.Equal(t, antminerChannel, reloadedAntminer.CurrentChannelID)
	assert.Equal(t, antminerRevision, reloadedAntminer.Revision)
	assert.Contains(t, channelMembers(t, db, orgID, antminerChannel), deviceIDs[1])

	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	parent, err := rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	require.Len(t, parent.Children, 1)
	assert.Equal(t, started.Children[0].Child.ID, parent.Children[0].ID)

	failedAdmission := &definitiveRollbackStrategy{}
	failingService := rollout.NewService(sqlstores.NewSQLRolloutStore(db), failedAdmission)
	attemptZero := rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Children[0].Child.ID,
		BatchID:          started.Children[0].FirstBatchID,
		ExpectedRevision: started.Children[0].Child.Revision,
		IdempotencyKey:   "u4-proto-child-start:admit:0",
		Reason:           "admit Proto",
		ActorUserID:      actorID,
	}
	_, err = failingService.Admit(t.Context(), attemptZero)
	require.ErrorContains(t, err, "forced definitive rollback")
	rolledBack, err := rolloutService.Get(t.Context(), orgID, started.Children[0].Child.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCreated, rolledBack.State)
	require.Len(t, rolledBack.Batches, 1)
	assert.Equal(t, rollout.BatchStatePending, rolledBack.Batches[0].State)
	assert.Equal(t, int32(1), rolledBack.Batches[0].AdmissionAttempt)

	_, err = failingService.Admit(t.Context(), attemptZero)
	require.ErrorContains(t, err, "forced definitive rollback")
	assert.Equal(t, 1, failedAdmission.calls, "old attempt key must replay its failed audit")

	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Children[0].Child.ID,
		BatchID:          started.Children[0].FirstBatchID,
		ExpectedRevision: rolledBack.Revision,
		IdempotencyKey:   "u4-proto-child-start:admit:1",
		Reason:           "retry Proto admission",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)

	parentControls := []func() error{
		func() error {
			_, controlErr := rolloutService.Admit(t.Context(), rollout.AdmitRequest{
				OrgID: orgID, RolloutID: started.Parent.ID, BatchID: 1,
				ExpectedRevision: 1, IdempotencyKey: "parent-admit", Reason: "invalid", ActorUserID: actorID,
			})
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Continue(t.Context(), rollout.AdmitRequest{
				OrgID: orgID, RolloutID: started.Parent.ID,
				ExpectedRevision: 1, IdempotencyKey: "parent-continue", Reason: "invalid", ActorUserID: actorID,
			})
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Pause(t.Context(), parentControlRequest(
				orgID, actorID, started.Parent.ID, "parent-pause",
			))
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Resume(t.Context(), parentControlRequest(
				orgID, actorID, started.Parent.ID, "parent-resume",
			))
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Abort(t.Context(), parentControlRequest(
				orgID, actorID, started.Parent.ID, "parent-abort",
			))
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Complete(t.Context(), parentControlRequest(
				orgID, actorID, started.Parent.ID, "parent-complete",
			))
			return controlErr
		},
		func() error {
			_, controlErr := rolloutService.Revert(t.Context(), parentControlRequest(
				orgID, actorID, started.Parent.ID, "parent-revert",
			))
			return controlErr
		},
	}
	for _, control := range parentControls {
		require.ErrorIs(t, control(), rollout.ErrParentNotControllable)
	}
}

func TestStartRolloutLaneCreatesMultipleModelChildrenAtomically(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID, Label: "Two model children",
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			testLaneTargetForModel("TestMinerB", "1.0.0", "b"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "u5-create-mixed-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "u5-enable-topology", Reason: "start two children", ActorUserID: actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	protoModel := laneModelByName(t, lane, "TestMiner")
	antminerModel := laneModelByName(t, lane, "TestMinerB")

	start := betweenchannel.StartRolloutRequest{
		OrgID: orgID, LaneID: lane.ID, Name: "Two model rollout",
		IdempotencyKey: "u5-parent-start", Reason: "update both models", ActorUserID: actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{
			{
				LaneModelID: protoModel.ID, ExpectedModelRevision: protoModel.Revision,
				FirmwareFileID: "proto-target", ReleaseTarget: testLaneTargetForModel("TestMiner", "2.0.0", "c"),
				ModelStartKey:  "u5-proto-start",
				HashratePolicy: &rollout.HashratePolicy{MaxDropBasisPoints: 100, HealthyDurationSeconds: 30},
				Batches: []rollout.CreateBatch{{
					Label: "Proto pilot", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
				}},
			},
			{
				LaneModelID: antminerModel.ID, ExpectedModelRevision: antminerModel.Revision,
				FirmwareFileID: "antminer-target",
				ReleaseTarget:  testLaneTargetForModel("TestMinerB", "3.0.0", "d"),
				ModelStartKey:  "u5-antminer-start",
				Batches: []rollout.CreateBatch{{
					Label: "Antminer wave", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}},
				}},
			},
		},
	}
	started, err := laneService.StartRollout(t.Context(), start)
	require.NoError(t, err)
	require.NotNil(t, started.Parent)
	require.Len(t, started.Children, 2)
	childModelKeys := []string{
		started.Children[0].Child.ModelIdentityKey,
		started.Children[1].Child.ModelIdentityKey,
	}
	assert.LessOrEqual(t, childModelKeys[0], childModelKeys[1])
	assert.ElementsMatch(t, []string{
		protoModel.ModelIdentityKey,
		antminerModel.ModelIdentityKey,
	}, childModelKeys)
	assert.NotEqual(t, started.Children[0].Child.ID, started.Children[1].Child.ID)
	assert.Positive(t, started.Children[0].FirstBatchID)
	assert.Positive(t, started.Children[1].FirstBatchID)
	assert.Equal(t, rollout.StateCreated, started.Children[0].Child.State)
	assert.Equal(t, rollout.StateCreated, started.Children[1].Child.State)

	replayed, err := laneService.StartRollout(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, replayed.Children, 2)
	for index := range started.Children {
		assert.Equal(t, started.Children[index].Child.ID, replayed.Children[index].Child.ID)
		assert.Equal(t, started.Children[index].FirstBatchID, replayed.Children[index].FirstBatchID)
	}

	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	admittedFirst, err := rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID: orgID, RolloutID: started.Children[0].Child.ID,
		BatchID: started.Children[0].FirstBatchID, ExpectedRevision: started.Children[0].Child.Revision,
		IdempotencyKey: "u5-proto-start:admit:0", Reason: "admit Proto", ActorUserID: actorID,
	})
	require.NoError(t, err)
	parent, err := rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	require.Len(t, parent.Children, 2)
	assert.Equal(t, rollout.StateRunning, parent.Children[0].State)
	assert.Equal(t, rollout.StateCreated, parent.Children[1].State)

	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID: orgID, RolloutID: started.Children[1].Child.ID,
		BatchID: started.Children[1].FirstBatchID, ExpectedRevision: started.Children[1].Child.Revision,
		IdempotencyKey: "u5-second-start:admit:0", Reason: "admit second model", ActorUserID: actorID,
	})
	require.NoError(t, err)
	pausedFirst, err := rolloutService.Pause(t.Context(), rollout.ControlRequest{
		OrgID: orgID, RolloutID: admittedFirst.ID, ExpectedRevision: admittedFirst.Revision,
		IdempotencyKey: "u5-first-pause", Reason: "review first model", ActorUserID: actorID,
	})
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StatePaused, parent.Children[0].State)
	assert.Equal(t, rollout.StateRunning, parent.Children[1].State)

	resumedFirst, err := rolloutService.Resume(t.Context(), rollout.ControlRequest{
		OrgID: orgID, RolloutID: pausedFirst.ID, ExpectedRevision: pausedFirst.Revision,
		IdempotencyKey: "u7-first-resume", Reason: "continue first model independently", ActorUserID: actorID,
	})
	require.NoError(t, err)
	secondRunning := parent.Children[1]
	_, err = rolloutService.Abort(t.Context(), rollout.ControlRequest{
		OrgID: orgID, RolloutID: secondRunning.ID, ExpectedRevision: secondRunning.Revision,
		IdempotencyKey: "u7-second-abort", Reason: "abort only the second model", ActorUserID: actorID,
	})
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	require.Len(t, parent.Children, 2)
	assert.Equal(t, resumedFirst.ID, parent.Children[0].ID)
	assert.Equal(t, rollout.StateRunning, parent.Children[0].State)
	assert.Equal(t, rollout.StateAborted, parent.Children[1].State)
}

func TestRolloutGroupPersistsProjectionAndResultRevisionAcrossTwoChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID, Label: "Persisted parent projection",
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			testLaneTargetForModel("TestMinerB", "1.0.0", "b"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "u5-parent-projection-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "u5-parent-projection-enable", Reason: "test parent projection", ActorUserID: actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	protoModel := laneModelByName(t, lane, "TestMiner")
	antminerModel := laneModelByName(t, lane, "TestMinerB")

	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID: orgID, LaneID: lane.ID, Name: "Persisted two-model projection",
		IdempotencyKey: "u5-parent-projection-start", Reason: "exercise parent result projection", ActorUserID: actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{
			{
				LaneModelID: protoModel.ID, ExpectedModelRevision: protoModel.Revision,
				FirmwareFileID: "projection-proto-target",
				ReleaseTarget:  testLaneTargetForModel("TestMiner", "2.0.0", "c"),
				ModelStartKey:  "u5-parent-projection-proto",
				HashratePolicy: &rollout.HashratePolicy{MaxDropBasisPoints: 100, HealthyDurationSeconds: 30},
				Batches: []rollout.CreateBatch{{
					Label: "Proto batch", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
				}},
			},
			{
				LaneModelID: antminerModel.ID, ExpectedModelRevision: antminerModel.Revision,
				FirmwareFileID: "projection-antminer-target",
				ReleaseTarget:  testLaneTargetForModel("TestMinerB", "3.0.0", "d"),
				ModelStartKey:  "u5-parent-projection-antminer",
				Batches: []rollout.CreateBatch{{
					Label: "Antminer batch", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}},
				}},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, started.Parent)
	require.Len(t, started.Children, 2)

	firstID := started.Children[0].Child.ID
	secondID := started.Children[1].Child.ID
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = CASE id WHEN $1 THEN 'running' ELSE 'review' END
		WHERE org_id = $2 AND id IN ($1, $3)
	`, firstID, orgID, secondID)
	require.NoError(t, err)

	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	parent, err := rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupLifecycleActive, parent.Lifecycle)
	assert.Equal(t, rollout.GroupActivityReview, parent.Activity)
	assert.True(t, parent.NeedsAction)
	assert.Equal(t, rollout.GroupTerminalOutcomePending, parent.TerminalOutcome)
	assert.False(t, parent.ResultReady)
	assert.Zero(t, parent.ResultRevision)

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed'
		WHERE org_id = $1 AND id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_member
		SET state = 'succeeded',
		    settled_at = CURRENT_TIMESTAMP,
		    owner_released_at = CURRENT_TIMESTAMP
		WHERE org_id = $1 AND rollout_id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE channel_firmware_authority authority
		SET halted_at = COALESCE(halted_at, CURRENT_TIMESTAMP),
		    revision = CASE
		        WHEN authority.halted_at IS NULL THEN authority.revision + 1
		        ELSE authority.revision
		    END
		FROM firmware_rollout child
		WHERE child.org_id = $1
		  AND child.id IN ($2, $3)
		  AND authority.org_id = child.org_id
		  AND authority.id = child.forward_authority_id
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed',
		    completed_at = CURRENT_TIMESTAMP
		WHERE org_id = $1 AND rollout_id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupLifecycleTerminal, parent.Lifecycle)
	assert.Equal(t, rollout.GroupTerminalOutcomeSuccessful, parent.TerminalOutcome)
	assert.Equal(t, rollout.GroupEvidencePending, parent.EvidenceReadiness)
	assert.False(t, parent.ResultReady, "policy evidence must be finalized before publishing the result")
	assert.Equal(t, int64(1), parent.ResultRevision)

	unchanged, err := rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, parent.ResultRevision, unchanged.ResultRevision, "unchanged refresh must not publish a new revision")

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'finalized',
		    post_window_finalized = TRUE,
		    post_window_finalized_at = CURRENT_TIMESTAMP
		WHERE org_id = $1 AND rollout_id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupEvidenceReady, parent.EvidenceReadiness)
	assert.True(t, parent.ResultReady)
	assert.Equal(t, int64(2), parent.ResultRevision, "readiness change must increment exactly once")
	readyRevision := parent.ResultRevision
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, readyRevision, parent.ResultRevision)

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed_with_failures'
		WHERE org_id = $1 AND id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupTerminalOutcomeCompletedWithFailures, parent.TerminalOutcome)
	assert.NotEqual(t, rollout.GroupTerminalOutcomeMixed, parent.TerminalOutcome)
	assert.Equal(t, readyRevision+1, parent.ResultRevision)

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'aborted'
		WHERE org_id = $1 AND id IN ($2, $3)
	`, orgID, firstID, secondID)
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupTerminalOutcomeAborted, parent.TerminalOutcome)
	assert.NotEqual(t, rollout.GroupTerminalOutcomeMixed, parent.TerminalOutcome)
	assert.Equal(t, readyRevision+2, parent.ResultRevision)

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'completed'
		WHERE org_id = $1 AND id = $2
	`, orgID, secondID)
	require.NoError(t, err)
	parent, err = rolloutService.GetGroup(t.Context(), orgID, started.Parent.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.GroupTerminalOutcomeMixed, parent.TerminalOutcome)
	assert.True(t, parent.ResultReady)
	assert.Equal(t, readyRevision+3, parent.ResultRevision, "outcome change must increment exactly once")
}

func TestModelChildFinalizationUsesFreshIdentityAndModelPointer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("fresh matching identity advances only the model pointer", func(t *testing.T) {
		fixture := startSingleModelChild(t, "success", 1)
		completedAt := time.Now().UTC()
		setChildEnforcementOutcome(
			t,
			fixture.db,
			fixture.orgID,
			fixture.child.ID,
			"confirmed",
			completedAt,
			"TestCorp",
			"TestMiner",
			completedAt.Add(time.Second),
		)
		finalizations, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, finalizations, 1)
		result, err := fixture.laneStore.Finalize(t.Context(), finalizations[0])
		require.NoError(t, err)
		assert.True(t, result.ProjectActivity)

		lane, err := fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		model := laneModelByName(t, lane, "TestMiner")
		assert.Equal(t, fixture.targetChannelID, model.CurrentChannelID)
		assert.Contains(
			t,
			channelMembers(t, fixture.db, fixture.orgID, fixture.targetChannelID),
			fixture.deviceID,
		)
	})

	t.Run("stale then mismatched identity never advances split pointer", func(t *testing.T) {
		fixture := startSingleModelChild(t, "mismatch", 1)
		completedAt := time.Now().UTC()
		setChildEnforcementOutcome(
			t,
			fixture.db,
			fixture.orgID,
			fixture.child.ID,
			"confirmed",
			completedAt,
			"TestCorp",
			"TestMiner",
			completedAt,
		)
		finalizations, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, finalizations, 1)
		deferred, err := fixture.laneStore.Finalize(t.Context(), finalizations[0])
		require.NoError(t, err)
		assert.False(t, deferred.ProjectActivity)

		setDiscoveredIdentityObservation(
			t,
			fixture.db,
			fixture.orgID,
			fixture.deviceID,
			"OtherCorp",
			"OtherMiner",
			completedAt.Add(time.Second),
		)
		finalizations, err = fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, finalizations, 1)
		mismatched, err := fixture.laneStore.Finalize(t.Context(), finalizations[0])
		require.NoError(t, err)
		assert.Equal(t, betweenchannel.FinalizationOutcomeAttention, mismatched.Outcome)

		reloaded, err := fixture.rolloutService.Get(
			t.Context(), fixture.orgID, fixture.child.ID,
		)
		require.NoError(t, err)
		_, err = fixture.rolloutService.Complete(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        fixture.child.ID,
			ExpectedRevision: reloaded.Revision,
			IdempotencyKey:   "complete-split-" + t.Name(),
			Reason:           "accept split result",
			ActorUserID:      fixture.actorID,
			WithFailures:     true,
		})
		require.NoError(t, err)

		lane, err := fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		model := laneModelByName(t, lane, "TestMiner")
		assert.Equal(t, fixture.sourceChannelID, model.CurrentChannelID)
	})

	t.Run("multi-member success permits target bindings before pointer advance", func(t *testing.T) {
		fixture := startSingleModelChild(t, "multi-success", 2)
		completedAt := time.Now().UTC()
		setChildEnforcementOutcome(
			t,
			fixture.db,
			fixture.orgID,
			fixture.child.ID,
			"confirmed",
			completedAt,
			"TestCorp",
			"TestMiner",
			completedAt.Add(time.Second),
		)
		for _, deviceID := range fixture.deviceIDs {
			setDiscoveredIdentityObservation(
				t,
				fixture.db,
				fixture.orgID,
				deviceID,
				"TestCorp",
				"TestMiner",
				completedAt.Add(time.Second),
			)
		}
		finalizations, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, finalizations, 2)
		_, err = fixture.laneStore.Finalize(t.Context(), finalizations[0])
		require.NoError(t, err)

		lane, err := fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, fixture.sourceChannelID, laneModelByName(t, lane, "TestMiner").CurrentChannelID)

		remaining, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, remaining, 1)
		_, err = fixture.laneStore.Finalize(t.Context(), remaining[0])
		require.NoError(t, err)
		lane, err = fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, fixture.targetChannelID, laneModelByName(t, lane, "TestMiner").CurrentChannelID)
	})
}

func TestModelChildAtomicStartFailureReleasesParentClaim(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	createLane := func(label, key, deviceID string) *betweenchannel.Lane {
		created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
			OrgID: orgID, Label: label,
			ReleaseTargets: []betweenchannel.ReleaseTarget{
				testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			},
			DeviceIdentifiers: []string{deviceID},
			IdempotencyKey:    key,
			ActorUserID:       actorID,
		})
		require.NoError(t, err)
		return created
	}
	first := createLane("First atomic lane", "atomic-first-lane", deviceIDs[0])
	second := createLane("Second atomic lane", "atomic-second-lane", deviceIDs[1])
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "atomic-enable", Reason: "atomic start proof", ActorUserID: actorID,
	})
	require.NoError(t, err)

	start := func(laneID uuid.UUID, parentKey, deviceID string) error {
		lane, getErr := laneService.GetLane(t.Context(), orgID, laneID, false, nil)
		require.NoError(t, getErr)
		model := laneModelByName(t, lane, "TestMiner")
		target := testLaneTargetForModel("TestMiner", "2.0.0", "f")
		_, startErr := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
			OrgID: orgID, LaneID: laneID, Name: "Atomic child",
			IdempotencyKey: parentKey, Reason: "atomic start proof", ActorUserID: actorID,
			ModelPlans: []betweenchannel.StartRolloutModelPlan{{
				LaneModelID: model.ID, ExpectedModelRevision: model.Revision,
				FirmwareFileID: target.FirmwareFileID, ReleaseTarget: target,
				ModelStartKey: "shared-child-start-key",
				Batches: []rollout.CreateBatch{{
					Label: "all", Members: []rollout.CreateMember{{DeviceIdentifier: deviceID}},
				}},
			}},
		})
		return startErr
	}
	require.NoError(t, start(first.ID, "atomic-first-parent", deviceIDs[0]))
	require.Error(t, start(second.ID, "atomic-second-parent", deviceIDs[1]))

	claims, err := queries.CountRolloutLaneActiveParentsForTest(
		t.Context(),
		sqlc.CountRolloutLaneActiveParentsForTestParams{LaneID: second.ID, OrgID: orgID},
	)
	require.NoError(t, err)
	assert.Zero(t, claims, "failed atomic start must not leak its lane claim")
}

type modelChildFixture struct {
	db              *sql.DB
	orgID           int64
	actorID         int64
	deviceID        string
	deviceIDs       []string
	laneID          uuid.UUID
	sourceChannelID int64
	targetChannelID int64
	child           *rollout.Rollout
	laneStore       *sqlstores.SQLRolloutLaneStore
	laneService     *betweenchannel.Service
	rolloutService  *rollout.Service
}

func startSingleModelChild(t *testing.T, suffix string, deviceCount int) modelChildFixture {
	t.Helper()
	db, orgID, deviceIDs := setupRolloutLaneTestData(t, deviceCount)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID, Label: "Identity " + suffix,
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "identity-create-" + suffix,
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "identity-enable-" + suffix,
		Reason:         "identity finalization", ActorUserID: actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	model := laneModelByName(t, lane, "TestMiner")
	target := testLaneTargetForModel("TestMiner", "2.0.0", "f")
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID: orgID, LaneID: lane.ID, Name: "Identity child",
		IdempotencyKey: "identity-parent-" + suffix,
		Reason:         "identity finalization", ActorUserID: actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{{
			LaneModelID: model.ID, ExpectedModelRevision: model.Revision,
			FirmwareFileID: target.FirmwareFileID, ReleaseTarget: target,
			ModelStartKey: "identity-child-" + suffix,
			Batches: []rollout.CreateBatch{{
				Label: "all",
				Members: func() []rollout.CreateMember {
					members := make([]rollout.CreateMember, 0, len(deviceIDs))
					for _, deviceID := range deviceIDs {
						members = append(members, rollout.CreateMember{DeviceIdentifier: deviceID})
					}
					return members
				}(),
			}},
		}},
	})
	require.NoError(t, err)
	child := started.Children[0].Child
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID: orgID, RolloutID: child.ID,
		BatchID: started.Children[0].FirstBatchID, ExpectedRevision: child.Revision,
		IdempotencyKey: "identity-admit-" + suffix,
		Reason:         "admit identity child", ActorUserID: actorID,
	})
	require.NoError(t, err)
	return modelChildFixture{
		db: db, orgID: orgID, actorID: actorID, deviceID: deviceIDs[0], deviceIDs: deviceIDs,
		laneID: lane.ID, sourceChannelID: model.CurrentChannelID,
		targetChannelID: *child.TargetChannelID, child: child,
		laneStore: laneStore, laneService: laneService, rolloutService: rolloutService,
	}
}

func setChildEnforcementOutcome(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	childID uuid.UUID,
	state string,
	completedAt time.Time,
	manufacturer string,
	model string,
	observedAt time.Time,
) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE channel_firmware_enforcement enforcement
		SET state = $3,
		    command_completed_at = $4,
		    confirmed_at = CASE
		        WHEN $3 = 'confirmed' THEN $4
		        ELSE enforcement.confirmed_at
		    END,
		    revision = enforcement.revision + 1
		FROM firmware_rollout_member member
		WHERE member.rollout_id = $1
		  AND member.org_id = $2
		  AND member.enforcement_id = enforcement.id
		  AND enforcement.org_id = member.org_id
	`, childID, orgID, state, completedAt)
	require.NoError(t, err)
	var deviceID string
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT device.device_identifier
		FROM firmware_rollout_member member
		JOIN device ON device.id = member.device_id AND device.org_id = member.org_id
		WHERE member.rollout_id = $1 AND member.org_id = $2
	`, childID, orgID).Scan(&deviceID))
	setDiscoveredIdentityObservation(t, db, orgID, deviceID, manufacturer, model, observedAt)
}

func setDiscoveredIdentityObservation(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
	manufacturer string,
	model string,
	observedAt time.Time,
) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE discovered_device discovered
		SET manufacturer = $3,
		    model = $4,
		    model_identity_observed_at = $5
		FROM device
		WHERE device.discovered_device_id = discovered.id
		  AND device.org_id = discovered.org_id
		  AND device.org_id = $1
		  AND device.device_identifier = $2
	`, orgID, deviceIdentifier, manufacturer, model, observedAt)
	require.NoError(t, err)
}

type definitiveRollbackStrategy struct {
	calls int
}

func (s *definitiveRollbackStrategy) Key() string {
	return betweenchannel.StrategyKey
}

func (s *definitiveRollbackStrategy) Admit(
	context.Context,
	rollout.AdmissionRequest,
) rollout.AdmissionResult {
	s.calls++
	return rollout.AdmissionResult{
		Outcome: rollout.AdmissionOutcomeDefinitivelyRolledBack,
		Err:     errors.New("forced definitive rollback"),
	}
}

func (s *definitiveRollbackStrategy) Revert(context.Context, rollout.RevertRequest) error {
	return errors.New("unexpected revert")
}

func parentControlRequest(
	orgID int64,
	actorID int64,
	parentID uuid.UUID,
	key string,
) rollout.ControlRequest {
	return rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        parentID,
		ExpectedRevision: 1,
		IdempotencyKey:   key,
		Reason:           "parent controls are invalid",
		ActorUserID:      actorID,
	}
}
