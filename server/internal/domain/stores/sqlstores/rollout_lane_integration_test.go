package sqlstores_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	channelDomain "github.com/block/proto-fleet/server/internal/domain/channel"
	collectionDomain "github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestRolloutLaneForwardAbortAndReverseLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	strategy := betweenchannel.NewStrategy(laneStore)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	rolloutService := rollout.NewService(rolloutStore, strategy)

	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Stable",
		Description:       "Production lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "create-stable-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	require.Len(t, lane.Channels, 1)
	assert.Equal(t, lane.Channels[0].ChannelID, lane.CurrentChannelID)
	assert.ElementsMatch(t, deviceIDs, channelMembers(t, db, orgID, lane.CurrentChannelID))

	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Stable 2.0.0",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
		Batches: []rollout.CreateBatch{
			{Label: "first", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}}},
			{Label: "second", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}}},
		},
		IdempotencyKey: "start-stable-2",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	require.Len(t, started.Lane.Channels, 2)
	require.Len(t, started.Rollout.Batches, 2)
	sourceChannelID := *started.Rollout.SourceChannelID
	targetChannelID := *started.Rollout.TargetChannelID
	assert.Equal(t, sourceChannelID, started.Lane.CurrentChannelID)
	updated, err := sqlc.New(db).UpdateDiscoveredDeviceModelByDeviceIdentifier(
		t.Context(),
		sqlc.UpdateDiscoveredDeviceModelByDeviceIdentifierParams{
			OrgID:            orgID,
			DeviceIdentifier: deviceIDs[0],
			Model:            "Metadata Changed After Freeze",
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)

	admitFirst := rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "admit-first",
		Reason:           "integration test",
		ActorUserID:      actorID,
	}
	admitted, err := rolloutService.Admit(t.Context(), admitFirst)
	require.NoError(t, err)
	_, err = rolloutService.Admit(t.Context(), admitFirst)
	require.NoError(t, err)

	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	firstMember := memberByIdentifier(t, persisted, deviceIDs[0])
	require.NotNil(t, firstMember.EnforcementID)
	assert.Equal(t, rollout.MemberStateAdmitted, firstMember.State)
	assert.Equal(t, sourceChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))
	firstEnforcement := enforcementByID(t, db, *firstMember.EnforcementID)
	assert.Equal(t, channelDomain.EnforcementStatePending, firstEnforcement.State)

	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[1].ID,
		ExpectedRevision: admitted.Revision,
		IdempotencyKey:   "admit-second",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	firstEnforcement, err = sqlstores.NewSQLChannelEnforcementStore(db).Claim(
		t.Context(),
		firstEnforcement,
		"older-batch-after-second-admission",
		time.Now(),
	)
	require.NoError(t, err)

	confirmEnforcement(t, db, firstEnforcement.ID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	finalized, err := laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	assert.Equal(t, betweenchannel.FinalizationOutcomeMoved, finalized.Outcome)
	assert.True(t, finalized.ProjectActivity)
	assert.Equal(t, targetChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))
	replayedFinalization, err := laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	assert.False(t, replayedFinalization.ProjectActivity)

	persisted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateReview, persisted.State)
	_, err = rolloutService.Continue(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: persisted.Revision,
		IdempotencyKey:   "continue-without-pending-batch",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.Error(t, err)
	secondMember := memberByIdentifier(t, persisted, deviceIDs[1])
	require.NotNil(t, secondMember.EnforcementID)
	attentionEnforcement(t, db, *secondMember.EnforcementID)
	finalizations, err = laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	assert.Equal(t, sourceChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))

	reviewAfterFailure, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateReview, reviewAfterFailure.State)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	assert.Equal(t, sourceChannelID, lane.CurrentChannelID)

	_, err = rolloutService.Complete(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: reviewAfterFailure.Revision,
		IdempotencyKey:   "complete-failures-without-approval",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.Error(t, err)
	completed, err := rolloutService.Complete(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: reviewAfterFailure.Revision,
		IdempotencyKey:   "complete-failures-with-approval",
		Reason:           "integration test",
		ActorUserID:      actorID,
		WithFailures:     true,
	})
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCompletedWithFailures, completed.State)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	assert.Equal(t, targetChannelID, lane.CurrentChannelID)

	_, err = rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: completed.Revision,
		IdempotencyKey:   "revert-confirmed",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	persisted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	firstMember = memberByIdentifier(t, persisted, deviceIDs[0])
	require.Equal(t, rollout.MemberStateReverting, firstMember.State)
	require.NotNil(t, firstMember.EnforcementID)
	reverseEnforcement := enforcementByID(t, db, *firstMember.EnforcementID)
	assert.Equal(t, "between_channel_revert", reverseEnforcement.CauseType)
	assert.Equal(t, targetChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))

	confirmEnforcement(t, db, reverseEnforcement.ID)
	finalizations, err = laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	assert.Equal(t, sourceChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))

	reverted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateReverted, reverted.State)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	assert.Equal(t, sourceChannelID, lane.CurrentChannelID)
}

func TestRolloutLaneFinalizerAdvancesBatchesAndCompletesRollout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Automatic progression lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "1")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "automatic-progression-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Automatic progression target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "2")},
		Batches: []rollout.CreateBatch{
			{Label: "canary", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}}},
			{Label: "fleet", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}}},
		},
		IdempotencyKey: "automatic-progression-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)

	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "automatic-progression-admit-canary",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	admitted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	canary := memberByIdentifier(t, admitted, deviceIDs[0])
	require.NotNil(t, canary.EnforcementID)
	confirmEnforcement(t, db, *canary.EnforcementID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)

	review, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateReview, review.State)
	require.Equal(t, rollout.BatchStateCompleted, review.Batches[0].State)
	require.Equal(t, rollout.BatchStatePending, review.Batches[1].State)

	running, err := rolloutService.Continue(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: review.Revision,
		IdempotencyKey:   "automatic-progression-continue",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	require.Equal(t, rollout.StateRunning, running.State)
	running, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)

	fleet := memberByIdentifier(t, running, deviceIDs[1])
	require.NotNil(t, fleet.EnforcementID)
	confirmEnforcement(t, db, *fleet.EnforcementID)
	finalizations, err = laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)

	completed, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateCompleted, completed.State)
	require.Equal(t, rollout.BatchStateCompleted, completed.Batches[1].State)
	require.True(t, authorityHalted(t, db, completed.ForwardAuthorityID))
	for _, member := range completed.Members {
		require.NotNil(t, member.OwnerReleasedAt)
	}
	require.Equal(t, int64(1), rolloutCauseCount(t, db, orgID, started.Rollout.ID, "complete"))
	var automaticActorType string
	var automaticCredentialID sql.NullString
	var automaticUserID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT actor_type, actor_credential_id, actor_user_id
		FROM firmware_rollout_cause
		WHERE org_id = $1
		  AND rollout_id = $2
		  AND operation = 'complete'
	`, orgID, started.Rollout.ID).Scan(
		&automaticActorType,
		&automaticCredentialID,
		&automaticUserID,
	))
	assert.Equal(t, string(rollout.ActorTypeSystem), automaticActorType)
	assert.False(t, automaticCredentialID.Valid)
	assert.Equal(t, actorID, automaticUserID)
	replayed, err := laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	require.False(t, replayed.ProjectActivity)
	require.Equal(t, int64(1), rolloutCauseCount(t, db, orgID, started.Rollout.ID, "complete"))
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	require.Equal(t, *started.Rollout.TargetChannelID, lane.CurrentChannelID)

	_, err = rolloutService.Continue(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: completed.Revision,
		IdempotencyKey:   "automatic-progression-no-pending",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.Error(t, err)
}

func TestRolloutLaneFinalizerCompletesFailedRevertWithoutMovingLane(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Failed revert lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "3")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "failed-revert-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Failed revert target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "4")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "failed-revert-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "failed-revert-admit",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	current, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	member := memberByIdentifier(t, current, deviceIDs[0])
	require.NotNil(t, member.EnforcementID)
	confirmEnforcement(t, db, *member.EnforcementID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)

	completed, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateCompleted, completed.State)
	_, err = rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: completed.Revision,
		IdempotencyKey:   "failed-revert-start-revert",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	reverting, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	member = memberByIdentifier(t, reverting, deviceIDs[0])
	require.NotNil(t, member.EnforcementID)
	attentionEnforcement(t, db, *member.EnforcementID)
	finalizations, err = laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)

	failed, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateCompletedWithFailures, failed.State)
	require.NotNil(t, failed.RevertAuthorityID)
	require.True(t, authorityHalted(t, db, *failed.RevertAuthorityID))
	member = memberByIdentifier(t, failed, deviceIDs[0])
	require.Equal(t, rollout.MemberStateAttentionRequired, member.State)
	require.NotNil(t, member.OwnerReleasedAt)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	require.Equal(t, *started.Rollout.TargetChannelID, lane.CurrentChannelID)
}

func TestRolloutLaneRejectsRevertWithoutSucceededMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Zero success revert lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "0")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "zero-success-revert-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Zero success revert target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "1")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "zero-success-revert-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	aborted, err := rolloutService.Abort(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "zero-success-abort",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	require.Equal(t, rollout.StateAborted, aborted.State)

	_, err = rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: aborted.Revision,
		IdempotencyKey:   "zero-success-revert",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.Error(t, err)
	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateAborted, persisted.State)
	assert.Nil(t, persisted.RevertAuthorityID)
}

func TestRolloutLaneAbortCompletesFullyCancelledBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Cancelled batch lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "5")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "cancelled-batch-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Cancelled batch target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "6")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "cancelled-batch-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	admitted, err := rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "cancelled-batch-admit",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	aborted, err := rolloutService.Abort(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: admitted.Revision,
		IdempotencyKey:   "cancelled-batch-abort",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	require.Equal(t, rollout.StateAborted, aborted.State)
	aborted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.BatchStateCompleted, aborted.Batches[0].State)
	require.Equal(t, rollout.MemberStateCancelled, aborted.Members[0].State)
	require.NotNil(t, aborted.Members[0].OwnerReleasedAt)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, finalizations)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	require.Equal(t, *started.Rollout.SourceChannelID, lane.CurrentChannelID)
}

func TestRolloutLaneAbortBoundaryLetsOnlyPreAbortClaimSettle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Abort lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "c")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "abort-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Abort target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "d")},
		Batches: []rollout.CreateBatch{{
			Label: "all",
			Members: []rollout.CreateMember{
				{DeviceIdentifier: deviceIDs[0]},
				{DeviceIdentifier: deviceIDs[1]},
				{DeviceIdentifier: deviceIDs[2]},
			},
		}},
		IdempotencyKey: "abort-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	collectionStore := newCollectionStore(db)
	manualRelease := createTestReleaseSet(t, collectionStore, orgID, "manual-conflict")
	manualChannel := createTestChannel(t, collectionStore, orgID, manualRelease.Id, "Manual")
	collectionService := collectionDomain.NewService(
		collectionStore,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
	_, err = collectionService.AssignDevicesToChannel(
		t.Context(),
		collectionDomain.AssignDevicesToChannelParams{
			OrgID:             orgID,
			TargetChannelID:   &manualChannel.Id,
			DeviceIdentifiers: deviceIDs,
		},
	)
	require.Error(t, err)
	assert.Equal(t, *started.Rollout.SourceChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))
	admitted, err := rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "abort-admit",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	first := memberByIdentifier(t, persisted, deviceIDs[0])
	second := memberByIdentifier(t, persisted, deviceIDs[1])
	third := memberByIdentifier(t, persisted, deviceIDs[2])
	require.NotNil(t, first.EnforcementID)
	require.NotNil(t, second.EnforcementID)
	require.NotNil(t, third.EnforcementID)
	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	firstClaim, err := enforcementStore.Claim(
		t.Context(),
		enforcementByID(t, db, *first.EnforcementID),
		"claimed-before-abort",
		time.Now(),
	)
	require.NoError(t, err)
	thirdClaim, err := enforcementStore.Claim(
		t.Context(),
		enforcementByID(t, db, *third.EnforcementID),
		"returned-after-abort",
		time.Now(),
	)
	require.NoError(t, err)

	aborted, err := rolloutService.Abort(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: admitted.Revision,
		IdempotencyKey:   "abort-now",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	assert.Equal(t, rollout.StateAborted, aborted.State)
	secondEnforcement := enforcementByID(t, db, *second.EnforcementID)
	assert.Equal(t, channelDomain.EnforcementStateCancelled, secondEnforcement.State)
	assert.Equal(t, *started.Rollout.SourceChannelID, deviceChannel(t, db, orgID, deviceIDs[1]))
	first = memberByIdentifier(t, aborted, deviceIDs[0])
	second = memberByIdentifier(t, aborted, deviceIDs[1])
	third = memberByIdentifier(t, aborted, deviceIDs[2])
	assert.Equal(t, rollout.MemberStateAdmitted, first.State)
	assert.Nil(t, first.OwnerReleasedAt)
	assert.Equal(t, rollout.MemberStateCancelled, second.State)
	assert.NotNil(t, second.OwnerReleasedAt)
	assert.Equal(t, rollout.MemberStateAdmitted, third.State)
	assert.Nil(t, third.OwnerReleasedAt)

	_, err = rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: aborted.Revision,
		IdempotencyKey:   "revert-before-claims-settle",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.Error(t, err)
	persisted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateAborted, persisted.State)
	assert.Nil(t, persisted.RevertAuthorityID)
	require.NoError(t, enforcementStore.ReturnPending(
		t.Context(),
		thirdClaim,
		"enqueue failed after abort",
	))
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	thirdEnforcement := enforcementByID(t, db, *third.EnforcementID)
	assert.Equal(t, channelDomain.EnforcementStateCancelled, thirdEnforcement.State)
	assert.Equal(t, *started.Rollout.SourceChannelID, deviceChannel(t, db, orgID, deviceIDs[2]))
	persisted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	third = memberByIdentifier(t, persisted, deviceIDs[2])
	assert.Equal(t, rollout.MemberStateCancelled, third.State)
	assert.NotNil(t, third.OwnerReleasedAt)

	confirmEnforcement(t, db, firstClaim.ID)
	finalizations, err = laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	assert.Equal(t, *started.Rollout.TargetChannelID, deviceChannel(t, db, orgID, deviceIDs[0]))
	persisted, err = rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateAborted, persisted.State)
	first = memberByIdentifier(t, persisted, deviceIDs[0])
	assert.Equal(t, rollout.MemberStateSucceeded, first.State)
	assert.NotNil(t, first.OwnerReleasedAt)
	assert.Equal(t, rollout.BatchStateCompleted, persisted.Batches[0].State)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	assert.Equal(t, *started.Rollout.SourceChannelID, lane.CurrentChannelID)

	_, err = laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Blocked split target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("3.0.0", "e")},
		Batches: []rollout.CreateBatch{{
			Label: "remaining-source-members",
			Members: []rollout.CreateMember{
				{DeviceIdentifier: deviceIDs[1]},
				{DeviceIdentifier: deviceIDs[2]},
			},
		}},
		IdempotencyKey: "abort-split-next-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.Error(t, err)
}

func TestRolloutLaneManualMembershipMoveConflictsInsteadOfOverwriting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Conflict lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "e")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "conflict-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Conflict target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "f")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "conflict-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	_, err = rolloutService.Admit(t.Context(), rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "conflict-admit",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	member := memberByIdentifier(t, persisted, deviceIDs[0])
	confirmEnforcement(t, db, *member.EnforcementID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	completed, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)

	collectionStore := newCollectionStore(db)
	manualRelease := createTestReleaseSet(t, collectionStore, orgID, "manual-after-complete")
	manualChannel := createTestChannel(t, collectionStore, orgID, manualRelease.Id, "Manual")
	collectionService := collectionDomain.NewService(
		collectionStore,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
	_, err = collectionService.AssignDevicesToChannel(
		t.Context(),
		collectionDomain.AssignDevicesToChannelParams{
			OrgID:             orgID,
			TargetChannelID:   &manualChannel.Id,
			DeviceIdentifiers: deviceIDs,
		},
	)
	require.NoError(t, err)

	revertRequest := rollout.ControlRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		ExpectedRevision: completed.Revision,
		IdempotencyKey:   "conflict-revert",
		Reason:           "integration test",
		ActorUserID:      actorID,
	}
	_, err = rolloutService.Revert(t.Context(), revertRequest)
	require.NoError(t, err)
	settled, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCompletedWithFailures, settled.State)
	require.NotNil(t, settled.RevertAuthorityID)
	assert.True(t, authorityHalted(t, db, *settled.RevertAuthorityID))
	member = memberByIdentifier(t, settled, deviceIDs[0])
	assert.Equal(t, rollout.MemberStateAttentionRequired, member.State)
	assert.Equal(t, manualChannel.Id, deviceChannel(t, db, orgID, deviceIDs[0]))

	restartedLaneStore := sqlstores.NewSQLRolloutLaneStore(db)
	restartedService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(restartedLaneStore),
	)
	replayed, err := restartedService.Revert(t.Context(), revertRequest)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCompletedWithFailures, replayed.State)
	lane, err = laneService.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	assert.Equal(t, *started.Rollout.TargetChannelID, lane.CurrentChannelID)
}

func TestRolloutLaneRejectsMissingAndNoopReleaseTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	store := sqlstores.NewSQLRolloutLaneStore(db)
	service := betweenchannel.NewService(store, nil)

	_, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID,
		Label: "Missing initial model",
		ReleaseTargets: []betweenchannel.ReleaseTarget{{
			FirmwareFileID:  "wrong-model",
			Manufacturer:    "OtherCorp",
			Model:           "OtherMiner",
			FirmwareVersion: "1.0.0",
			SHA256:          strings.Repeat("7", 64),
		}},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "missing-initial-model",
		ActorUserID:       actorID,
	})
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)

	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Validation lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "8")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "validation-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	_, err = service.GetLane(t.Context(), orgID+1, lane.ID)
	require.Error(t, err)
	_, err = db.ExecContext(
		t.Context(),
		"UPDATE device_set SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1",
		lane.CurrentChannelID,
	)
	require.Error(t, err)

	batches := []rollout.CreateBatch{{
		Label:   "all",
		Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
	}}
	_, err = service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:  orgID,
		LaneID: lane.ID,
		Name:   "Noop target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{{
			FirmwareFileID:  "different-file-same-version",
			Manufacturer:    "TestCorp",
			Model:           "TestMiner",
			FirmwareVersion: "1.0.0",
			SHA256:          strings.Repeat("9", 64),
		}},
		Batches:        batches,
		IdempotencyKey: "noop-target",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)

	_, err = service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:  orgID,
		LaneID: lane.ID,
		Name:   "Missing target model",
		ReleaseTargets: []betweenchannel.ReleaseTarget{{
			FirmwareFileID:  "missing-target",
			Manufacturer:    "OtherCorp",
			Model:           "OtherMiner",
			FirmwareVersion: "2.0.0",
			SHA256:          strings.Repeat("a", 64),
		}},
		Batches:        batches,
		IdempotencyKey: "missing-target-model",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.ErrorIs(t, err, betweenchannel.ErrCompatibility)

	lane, err = service.GetLane(t.Context(), orgID, lane.ID)
	require.NoError(t, err)
	require.Len(t, lane.Channels, 1)
}

func TestRolloutLaneInitialEnforcementPreviewAndConfirmedCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 3)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	setDiscoveredModel(t, db, orgID, deviceIDs[2], "TestMinerB")
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[0], "1.0.0")
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[1], "1.9.0")
	setDiscoveredFirmwareVersion(t, db, orgID, deviceIDs[2], "")
	targets := []betweenchannel.ReleaseTarget{
		testLaneTarget("1.0.0", "c"),
		testLaneTargetForModel("TestMinerB", "2.0.0", "d"),
	}

	preview, err := service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID,
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), preview.MatchingCount)
	assert.Equal(t, int32(1), preview.MismatchedCount)
	assert.Equal(t, int32(1), preview.UnknownCount)
	require.Len(t, preview.Miners, 3)
	previewByIdentifier := make(map[string]betweenchannel.InitialFirmwareMiner, len(preview.Miners))
	for _, miner := range preview.Miners {
		previewByIdentifier[miner.DeviceIdentifier] = miner
	}
	assert.Equal(t, betweenchannel.InitialFirmwareMatch, previewByIdentifier[deviceIDs[0]].Status)
	assert.Equal(t, "1.0.0", previewByIdentifier[deviceIDs[0]].TargetFirmwareVersion)
	assert.Equal(t, betweenchannel.InitialFirmwareMismatch, previewByIdentifier[deviceIDs[1]].Status)
	assert.Equal(t, "2.0.0", previewByIdentifier[deviceIDs[1]].TargetFirmwareVersion)
	assert.Equal(t, betweenchannel.InitialFirmwareUnknown, previewByIdentifier[deviceIDs[2]].Status)
	assert.Equal(t, "2.0.0", previewByIdentifier[deviceIDs[2]].TargetFirmwareVersion)

	_, err = service.PreviewLane(t.Context(), betweenchannel.PreviewLaneRequest{
		OrgID:             orgID + 1,
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
	})
	require.ErrorIs(t, err, betweenchannel.ErrMembershipConflict)

	request := betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Enforced stable",
		ReleaseTargets:    targets,
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "create-enforced-stable",
		ActorUserID:       actorID,
	}
	beforeUnconfirmed := rolloutLaneCreateStateCounts(t, db, orgID)
	_, err = service.CreateLane(t.Context(), request)
	require.ErrorIs(t, err, betweenchannel.ErrInitialEnforcementConfirmationRequired)
	assert.Equal(t, beforeUnconfirmed, rolloutLaneCreateStateCounts(t, db, orgID))
	assert.Zero(t, rolloutLaneCountByCreateKey(t, db, orgID, request.IdempotencyKey))
	assert.Zero(t, laneInitialEnforcementCount(t, db, orgID, ""))

	request.ConfirmInitialEnforcement = true
	lane, err := service.CreateLane(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, int32(3), lane.InitialEnforcement.TotalCount)
	assert.Equal(t, int32(2), lane.InitialEnforcement.PendingCount)
	assert.Equal(t, int32(0), lane.InitialEnforcement.UpdatingCount)
	assert.Equal(t, int32(1), lane.InitialEnforcement.ConfirmedCount)
	assert.Equal(t, int32(0), lane.InitialEnforcement.AttentionCount)
	lanes, err := service.ListLanes(t.Context(), orgID)
	require.NoError(t, err)
	require.Len(t, lanes, 1)
	assert.Equal(t, lane.InitialEnforcement, lanes[0].InitialEnforcement)

	var confirmed, pending int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FILTER (WHERE enforcement.state = 'confirmed'),
		       COUNT(*) FILTER (WHERE enforcement.state = 'pending')
		FROM channel_firmware_enforcement enforcement
		JOIN channel_firmware_authority authority
		  ON authority.id = enforcement.authority_id
		 AND authority.org_id = enforcement.org_id
		WHERE authority.org_id = $1
		  AND authority.authority_type = 'rollout_lane_initial'
		  AND authority.authority_reference = $2
	`, orgID, lane.ID.String()).Scan(&confirmed, &pending))
	assert.Equal(t, int64(1), confirmed)
	assert.Equal(t, int64(2), pending)

	reconcileRows, err := sqlc.New(db).ListChannelFirmwareEnforcementsForReconcile(t.Context(), 10)
	require.NoError(t, err)
	assert.Len(t, reconcileRows, 2, "matching reported firmware must never enter dispatch")
	reconcileByIdentifier := make(map[string]sqlc.ListChannelFirmwareEnforcementsForReconcileRow, len(reconcileRows))
	for _, row := range reconcileRows {
		assert.NotEqual(t, deviceIDs[0], row.DeviceIdentifier)
		reconcileByIdentifier[row.DeviceIdentifier] = row
	}
	assert.Equal(t, "2.0.0", reconcileByIdentifier[deviceIDs[1]].DesiredFirmwareVersion)
	assert.Equal(t, "1.9.0", reconcileByIdentifier[deviceIDs[1]].LastObservedFirmwareVersion.String)
	assert.True(t, reconcileByIdentifier[deviceIDs[1]].FirmwareObservedAt.Valid)
	assert.Equal(t, "2.0.0", reconcileByIdentifier[deviceIDs[2]].DesiredFirmwareVersion)
	assert.False(t, reconcileByIdentifier[deviceIDs[2]].LastObservedFirmwareVersion.Valid)
	assert.False(t, reconcileByIdentifier[deviceIDs[2]].FirmwareObservedAt.Valid)

	request.ConfirmInitialEnforcement = false
	replayed, err := service.CreateLane(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, lane.ID, replayed.ID)
	assert.Equal(t, int64(3), laneInitialEnforcementCount(t, db, orgID, lane.ID.String()))

	_, err = service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Blocked while initial enforcement is active",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "d")},
		Batches: []rollout.CreateBatch{{
			Label: "all",
			Members: []rollout.CreateMember{
				{DeviceIdentifier: deviceIDs[0]},
				{DeviceIdentifier: deviceIDs[1]},
				{DeviceIdentifier: deviceIDs[2]},
			},
		}},
		IdempotencyKey: "blocked-initial-enforcement",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.ErrorIs(t, err, betweenchannel.ErrInitialEnforcementActive)
}

func TestRolloutLaneCreateFailureRollsBackInitialEnforcementGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	_, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Existing lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "e")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "existing-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	before := rolloutLaneCreateStateCounts(t, db, orgID)

	_, err = service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Conflicting lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "f")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "conflicting-lane",
		ActorUserID:       actorID,
	})
	require.Error(t, err)
	assert.Equal(t, before, rolloutLaneCreateStateCounts(t, db, orgID))
	assert.Zero(t, rolloutLaneCountByCreateKey(t, db, orgID, "conflicting-lane"))
}

func TestRolloutLaneSortedChannelLocksAvoidOppositeDirectionDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	store := sqlstores.NewSQLRolloutLaneStore(db)
	service := betweenchannel.NewService(store, nil)
	lane, err := service.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Lock lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "1")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "lock-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := service.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Lock target",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "2")},
		Batches: []rollout.CreateBatch{{
			Label:   "all",
			Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
		}},
		IdempotencyKey: "lock-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	channelIDs := []int64{*started.Rollout.SourceChannelID, *started.Rollout.TargetChannelID}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, order := range [][]int64{channelIDs, {channelIDs[1], channelIDs[0]}} {
		wg.Add(1)
		go func(ids []int64) {
			defer wg.Done()
			tx, txErr := db.BeginTx(ctx, &sql.TxOptions{})
			if txErr != nil {
				errs <- txErr
				return
			}
			defer tx.Rollback()
			<-start
			q := sqlc.New(tx)
			_, txErr = q.LockBetweenChannelChannels(
				ctx,
				sqlc.LockBetweenChannelChannelsParams{
					OrgID:      orgID,
					ChannelIds: ids,
				},
			)
			if txErr == nil {
				txErr = tx.Commit()
			}
			errs <- txErr
		}(append([]int64(nil), order...))
	}
	close(start)
	wg.Wait()
	close(errs)
	for lockErr := range errs {
		require.NoError(t, lockErr)
	}
}

func TestRolloutLaneConcurrentCreateReplaysIdempotently(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)
	request := betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Concurrent lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "6")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "concurrent-lane-create",
		ActorUserID:       testOrganizationUserID(t, db, orgID),
	}

	type createResult struct {
		lane *betweenchannel.Lane
		err  error
	}
	start := make(chan struct{})
	results := make(chan createResult, 2)
	for range 2 {
		go func() {
			<-start
			lane, err := service.CreateLane(t.Context(), request)
			results <- createResult{lane: lane, err: err}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotNil(t, first.lane)
	require.NotNil(t, second.lane)
	assert.Equal(t, first.lane.ID, second.lane.ID)
}

func setupRolloutLaneTestData(
	t *testing.T,
	deviceCount int,
) (db *sql.DB, orgID int64, deviceIDs []string) {
	t.Helper()
	db, orgID, deviceIDs = setupCollectionTestData(t, deviceCount)
	for _, deviceIdentifier := range deviceIDs {
		setDiscoveredFirmwareVersion(t, db, orgID, deviceIdentifier, "1.0.0")
	}
	return db, orgID, deviceIDs
}

func testLaneTarget(version, shaCharacter string) betweenchannel.ReleaseTarget {
	return testLaneTargetForModel("TestMiner", version, shaCharacter)
}

func testLaneTargetForModel(
	model string,
	version string,
	shaCharacter string,
) betweenchannel.ReleaseTarget {
	return betweenchannel.ReleaseTarget{
		FirmwareFileID:  "firmware-" + model + "-" + version + "-" + shaCharacter,
		Manufacturer:    "TestCorp",
		Model:           model,
		FirmwareVersion: version,
		SHA256:          strings.Repeat(shaCharacter, 64),
	}
}

func setDiscoveredModel(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
	model string,
) {
	t.Helper()
	rows, err := sqlc.New(db).UpdateDiscoveredDeviceModelByDeviceIdentifier(
		t.Context(),
		sqlc.UpdateDiscoveredDeviceModelByDeviceIdentifierParams{
			OrgID:            orgID,
			DeviceIdentifier: deviceIdentifier,
			Model:            model,
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func setDiscoveredFirmwareVersion(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
	version string,
) {
	t.Helper()
	result, err := db.ExecContext(t.Context(), `
		UPDATE discovered_device discovered
		SET firmware_version = NULLIF($3, '')
		FROM device
		WHERE device.discovered_device_id = discovered.id
		  AND device.org_id = discovered.org_id
		  AND device.org_id = $1
		  AND device.device_identifier = $2
	`, orgID, deviceIdentifier, version)
	require.NoError(t, err)
	rows, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

func rolloutLaneCountByCreateKey(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	idempotencyKey string,
) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM rollout_lane
		WHERE org_id = $1
		  AND idempotency_key = $2
	`, orgID, idempotencyKey).Scan(&count))
	return count
}

func laneInitialEnforcementCount(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	laneID string,
) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM channel_firmware_enforcement enforcement
		JOIN channel_firmware_authority authority
		  ON authority.id = enforcement.authority_id
		 AND authority.org_id = enforcement.org_id
		WHERE authority.org_id = $1
		  AND authority.authority_type = 'rollout_lane_initial'
		  AND ($2 = '' OR authority.authority_reference = $2)
	`, orgID, laneID).Scan(&count))
	return count
}

type rolloutLaneStateCounts struct {
	ReleaseSets  int64
	Channels     int64
	Memberships  int64
	Lanes        int64
	Authorities  int64
	Enforcements int64
}

func rolloutLaneCreateStateCounts(
	t *testing.T,
	db *sql.DB,
	orgID int64,
) rolloutLaneStateCounts {
	t.Helper()
	var result rolloutLaneStateCounts
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT
		    (SELECT COUNT(*) FROM firmware_release_set WHERE org_id = $1),
		    (SELECT COUNT(*) FROM device_set_channel WHERE org_id = $1),
		    (SELECT COUNT(*) FROM device_set_membership
		     WHERE org_id = $1 AND device_set_type = 'channel'),
		    (SELECT COUNT(*) FROM rollout_lane WHERE org_id = $1),
		    (SELECT COUNT(*) FROM channel_firmware_authority WHERE org_id = $1),
		    (SELECT COUNT(*) FROM channel_firmware_enforcement WHERE org_id = $1)
	`, orgID).Scan(
		&result.ReleaseSets,
		&result.Channels,
		&result.Memberships,
		&result.Lanes,
		&result.Authorities,
		&result.Enforcements,
	))
	return result
}

func memberByIdentifier(
	t *testing.T,
	value *rollout.Rollout,
	deviceIdentifier string,
) rollout.Member {
	t.Helper()
	for _, member := range value.Members {
		if member.DeviceIdentifier == deviceIdentifier {
			return member
		}
	}
	t.Fatalf("rollout member %s not found", deviceIdentifier)
	return rollout.Member{}
}

func channelMembers(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	channelID int64,
) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `
		SELECT device_identifier
		FROM device_set_membership
		WHERE org_id = $1
		  AND device_set_id = $2
		  AND device_set_type = 'channel'
		ORDER BY device_identifier
	`, orgID, channelID)
	require.NoError(t, err)
	defer rows.Close()
	var result []string
	for rows.Next() {
		var identifier string
		require.NoError(t, rows.Scan(&identifier))
		result = append(result, identifier)
	}
	require.NoError(t, rows.Err())
	return result
}

func deviceChannel(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	deviceIdentifier string,
) int64 {
	t.Helper()
	var channelID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT device_set_id
		FROM device_set_membership
		WHERE org_id = $1
		  AND device_identifier = $2
		  AND device_set_type = 'channel'
	`, orgID, deviceIdentifier).Scan(&channelID))
	return channelID
}

func enforcementByID(
	t *testing.T,
	db *sql.DB,
	enforcementID int64,
) channelDomain.Enforcement {
	t.Helper()
	store := sqlstores.NewSQLChannelEnforcementStore(db)
	enforcement, err := store.GetEnforcement(t.Context(), enforcementID)
	require.NoError(t, err)
	return enforcement
}

func confirmEnforcement(t *testing.T, db *sql.DB, enforcementID int64) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE channel_firmware_enforcement
		SET state = 'confirmed',
		    confirmed_at = CURRENT_TIMESTAMP,
		    revision = revision + 1
		WHERE id = $1
	`, enforcementID)
	require.NoError(t, err)
}

func attentionEnforcement(t *testing.T, db *sql.DB, enforcementID int64) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE channel_firmware_enforcement
		SET state = 'attention_required',
		    attention_required_at = CURRENT_TIMESTAMP,
		    last_error = 'injected ambiguous firmware result',
		    revision = revision + 1
		WHERE id = $1
	`, enforcementID)
	require.NoError(t, err)
}

func authorityHalted(t *testing.T, db *sql.DB, authorityID uuid.UUID) bool {
	t.Helper()
	var halted bool
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT halted_at IS NOT NULL
		FROM channel_firmware_authority
		WHERE id = $1
	`, authorityID).Scan(&halted))
	return halted
}

func rolloutCauseCount(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	rolloutID uuid.UUID,
	operation string,
) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_cause
		WHERE org_id = $1
		  AND rollout_id = $2
		  AND operation = $3
	`, orgID, rolloutID, operation).Scan(&count))
	return count
}
