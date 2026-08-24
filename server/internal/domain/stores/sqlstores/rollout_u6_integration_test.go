package sqlstores_test

import (
	"database/sql"
	"fmt"
	"strings"
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

func TestModelChildBlockedControlDoesNotBlockSiblingControl(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "sibling-nonblocking")
	first := fixture.children[0]
	second := fixture.children[1]

	blocker, err := fixture.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	_, err = blocker.ExecContext(t.Context(), `
		SELECT 1
		FROM firmware_rollout
		WHERE id = $1 AND org_id = $2
		FOR UPDATE
	`, first.ID, fixture.orgID)
	require.NoError(t, err)

	firstDone := runAsyncError(func() error {
		_, controlErr := fixture.rolloutService.Pause(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        first.ID,
			ExpectedRevision: first.Revision,
			IdempotencyKey:   "u6-blocked-first-pause",
			Reason:           "hold first model control",
			ActorUserID:      fixture.actorID,
		})
		return controlErr
	})
	requireStillBlocked(t, firstDone, "first model control")

	secondDone := runAsyncError(func() error {
		_, controlErr := fixture.rolloutService.Pause(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        second.ID,
			ExpectedRevision: second.Revision,
			IdempotencyKey:   "u6-independent-second-pause",
			Reason:           "pause second model independently",
			ActorUserID:      fixture.actorID,
		})
		return controlErr
	})
	select {
	case controlErr := <-secondDone:
		require.NoError(t, controlErr)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sibling control blocked behind another model's child or lane lock")
	}

	require.NoError(t, blocker.Commit())
	require.NoError(t, awaitAsyncError(t, firstDone, "blocked first control"))
}

func TestListGroupsBulkHydratesChildrenInCanonicalOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "bulk-group-hydration")
	groups, err := sqlstores.NewSQLRolloutStore(fixture.db).ListGroups(t.Context(), fixture.orgID)
	require.NoError(t, err)
	var parent *rollout.Group
	for index := range groups {
		if groups[index].ID == fixture.parentID {
			parent = &groups[index]
			break
		}
	}
	require.NotNil(t, parent)
	require.Len(t, parent.Children, 2)
	assert.Less(t, parent.Children[0].ModelIdentityKey, parent.Children[1].ModelIdentityKey)
	require.Len(t, parent.Children[0].Members, 1)
	require.Len(t, parent.Children[1].Members, 1)
}

func TestListGroupsDoesNotLockLanesWithoutActiveClaims(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "historical-group-read")
	_, err := fixture.db.ExecContext(
		t.Context(),
		`DELETE FROM rollout_lane_active_parent WHERE lane_id = $1 AND org_id = $2`,
		fixture.lane.ID,
		fixture.orgID,
	)
	require.NoError(t, err)
	blocker, err := fixture.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	_, err = blocker.ExecContext(
		t.Context(),
		`SELECT 1 FROM rollout_lane WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		fixture.lane.ID,
		fixture.orgID,
	)
	require.NoError(t, err)

	done := runAsyncError(func() error {
		_, listErr := sqlstores.NewSQLRolloutStore(fixture.db).ListGroups(t.Context(), fixture.orgID)
		return listErr
	})
	select {
	case listErr := <-done:
		require.NoError(t, listErr)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("historical aggregate read blocked on the rollout lane row")
	}
}

func TestModelChildSameModelControlsSerializeWithConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startSingleModelChild(t, "u6-same-model-controls", 1)
	current, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	for index := range 2 {
		go func(controlIndex int) {
			<-start
			_, controlErr := fixture.rolloutService.Pause(t.Context(), rollout.ControlRequest{
				OrgID:            fixture.orgID,
				RolloutID:        current.ID,
				ExpectedRevision: current.Revision,
				IdempotencyKey:   fmt.Sprintf("u6-same-model-pause-%d", controlIndex),
				Reason:           "race same model controls safely",
				ActorUserID:      fixture.actorID,
			})
			results <- controlErr
		}(index)
	}
	close(start)

	var successes, conflicts int
	for range 2 {
		controlErr := <-results
		if controlErr == nil {
			successes++
			continue
		}
		if strings.Contains(controlErr.Error(), "revision") {
			conflicts++
			continue
		}
		t.Fatalf("unexpected same-model control error: %v", controlErr)
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
	persisted, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StatePaused, persisted.State)
}

func TestModelChildFinalizationControlAndMembershipRaceSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startSingleModelChild(t, "u6-finalize-race", 1)
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
	current, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	lane, err := fixture.laneService.GetLane(
		t.Context(), fixture.orgID, fixture.laneID, false, nil,
	)
	require.NoError(t, err)
	model := lane.Models[0]

	declarationBlocker, err := fixture.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = declarationBlocker.Rollback() })
	_, err = declarationBlocker.ExecContext(t.Context(), `
		SELECT 1
		FROM rollout_lane_model
		WHERE id = $1 AND lane_id = $2 AND org_id = $3
		FOR UPDATE
	`, model.ID, fixture.laneID, fixture.orgID)
	require.NoError(t, err)

	finalizeDone := runAsyncError(func() error {
		_, finalizeErr := fixture.laneStore.Finalize(t.Context(), finalizations[0])
		return finalizeErr
	})
	waitForLockedRow(t, fixture.db, `
		SELECT 1
		FROM firmware_rollout
		WHERE id = $1 AND org_id = $2
		FOR UPDATE
	`, fixture.child.ID, fixture.orgID)

	controlDone := runAsyncError(func() error {
		_, controlErr := fixture.rolloutService.Pause(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        current.ID,
			ExpectedRevision: current.Revision,
			IdempotencyKey:   "u6-pause-during-finalize",
			Reason:           "race finalization safely",
			ActorUserID:      fixture.actorID,
		})
		return controlErr
	})
	membershipDone := runAsyncError(func() error {
		_, membershipErr := fixture.laneService.UpdateModelMembership(
			t.Context(),
			betweenchannel.UpdateModelMembershipRequest{
				OrgID: fixture.orgID, LaneID: fixture.laneID, LaneModelID: model.ID,
				ExpectedRevision:  model.Revision,
				RemoveIdentifiers: []string{fixture.deviceID},
				IdempotencyKey:    "u6-membership-during-finalize",
				Reason:            "race membership safely",
				ActorUserID:       fixture.actorID,
				ActorType:         rollout.ActorTypeUser,
			},
		)
		return membershipErr
	})
	requireStillBlocked(t, controlDone, "same-model control")
	requireStillBlocked(t, membershipDone, "same-model membership")

	require.NoError(t, declarationBlocker.Commit())
	require.NoError(t, awaitAsyncError(t, finalizeDone, "same-model finalization"))
	require.Error(t, awaitAsyncError(t, controlDone, "same-model control"))
	require.Error(t, awaitAsyncError(t, membershipDone, "same-model membership"))

	persisted, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCompleted, persisted.State)
	assert.Equal(t, rollout.MemberStateSucceeded, persisted.Members[0].State)
	assert.Equal(t, fixture.targetChannelID, deviceChannel(
		t, fixture.db, fixture.orgID, fixture.deviceID,
	))
}

func TestModelChildAbortDurablyCancelsEvidenceAndMakesParentReady(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "abort-evidence")
	child := fixture.children[0]
	batch := child.Batches[0]
	member := batch.Members[0]
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := fixture.db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed',
		    completed_at = $2,
		    evidence_status = 'held'
		WHERE id = $1
	`, batch.ID, now.Add(-time.Minute))
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_evidence (
		    rollout_id, member_id, org_id, phase, window_start, window_end
		)
		VALUES
		    ($1, $2, $3, 'baseline', $4, $5),
		    ($1, $2, $3, 'post', $5, $6)
		ON CONFLICT (member_id, phase) DO UPDATE
		SET window_start = EXCLUDED.window_start,
		    window_end = EXCLUDED.window_end
	`, child.ID, member.ID, fixture.orgID, now.Add(-31*time.Minute), now.Add(-time.Minute), now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_control (
		    id, rollout_id, org_id, batch_id, operation, idempotency_key,
		    request_fingerprint, expected_revision, resulting_revision,
		    status, admission_attempt, created_by_user_id
		)
		VALUES (
		    gen_random_uuid(), $1, $2, $3, 'continue', $4,
		    repeat('e', 64), $5::bigint, $5::bigint + 1, 'started', 0, $6
		)
	`, child.ID, fixture.orgID, batch.ID,
		"rollout-evidence-auto-continue-batch-"+fmt.Sprint(batch.ID),
		child.Revision, fixture.actorID)
	require.NoError(t, err)

	aborted, err := fixture.rolloutService.Abort(t.Context(), rollout.ControlRequest{
		OrgID:            fixture.orgID,
		RolloutID:        child.ID,
		ExpectedRevision: child.Revision,
		IdempotencyKey:   "u6-abort-cancels-evidence",
		Reason:           "operator cancelled this model rollout",
		ActorUserID:      fixture.actorID,
	})
	require.NoError(t, err)
	assert.Equal(t, rollout.StateAborted, aborted.State)

	var (
		status       string
		reason       sql.NullString
		cancelledAt  sql.NullTime
		postFinal    bool
		postFinalAt  sql.NullTime
		evidenceOpen int64
		autoStatus   string
	)
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT evidence_status,
		       evidence_cancellation_reason,
		       evidence_cancelled_at,
		       post_window_finalized,
		       post_window_finalized_at
		FROM firmware_rollout_batch
		WHERE id = $1
	`, batch.ID).Scan(&status, &reason, &cancelledAt, &postFinal, &postFinalAt))
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_evidence
		WHERE rollout_id = $1
		  AND status = 'open'
	`, child.ID).Scan(&evidenceOpen))
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT status
		FROM firmware_rollout_control
		WHERE rollout_id = $1
		  AND idempotency_key = $2
	`, child.ID, "rollout-evidence-auto-continue-batch-"+fmt.Sprint(batch.ID)).Scan(&autoStatus))
	assert.Equal(t, "cancelled", status)
	require.True(t, reason.Valid)
	assert.Contains(t, reason.String, "operator cancelled this model rollout")
	assert.True(t, cancelledAt.Valid)
	assert.True(t, postFinal)
	assert.True(t, postFinalAt.Valid)
	assert.Zero(t, evidenceOpen)
	assert.Equal(t, string(rollout.ControlStatusFailed), autoStatus)

	for _, sibling := range fixture.children[1:] {
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout
			SET state = 'aborted', aborted_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND org_id = $2
		`, sibling.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE channel_firmware_enforcement enforcement
			SET state = 'cancelled'
			FROM firmware_rollout child
			WHERE child.id = $1
			  AND child.org_id = $2
			  AND enforcement.org_id = child.org_id
			  AND enforcement.authority_id IN (
			      child.forward_authority_id,
			      child.revert_authority_id
			  )
		`, sibling.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_batch
			SET state = 'cancelled',
			    evidence_status = 'cancelled',
			    evidence_cancellation_reason = 'test sibling settlement',
			    evidence_cancelled_at = CURRENT_TIMESTAMP,
			    post_window_finalized = TRUE,
			    post_window_finalized_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1 AND org_id = $2
		`, sibling.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_member
			SET state = 'cancelled',
			    settled_at = CURRENT_TIMESTAMP,
			    owner_released_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1 AND org_id = $2
		`, sibling.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_evidence
			SET status = 'cancelled',
			    cancellation_reason = 'test sibling settlement',
			    cancelled_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1
			  AND org_id = $2
			  AND status = 'open'
		`, sibling.ID, fixture.orgID)
		require.NoError(t, err)
		haltRolloutAuthorities(t, fixture.db, fixture.orgID, sibling.ID)
	}
	parent, err := fixture.rolloutService.GetGroup(
		t.Context(),
		fixture.orgID,
		fixture.parentID,
	)
	require.NoError(t, err)
	assert.True(t, parent.ResultReady)
	assert.Equal(t, rollout.GroupEvidenceReady, parent.EvidenceReadiness)
	settlement, err := sqlc.New(fixture.db).GetRolloutLaneSettlementState(
		t.Context(),
		sqlc.GetRolloutLaneSettlementStateParams{
			LaneID: uuid.NullUUID{UUID: fixture.lane.ID, Valid: true},
			OrgID:  fixture.orgID,
		},
	)
	require.NoError(t, err)
	assert.False(t, settlement.ChildUnsettled)
	assert.False(t, settlement.OwnerUnsettled)
	assert.False(t, settlement.ControlUnsettled)
	assert.False(t, settlement.AuthorityUnsettled)
	assert.False(t, settlement.EnforcementUnsettled)
	assert.False(t, settlement.FinalizationUnsettled)
	assert.False(t, settlement.RevertUnsettled)
	assert.False(t, settlement.EvidenceUnsettled)
	claimCount, err := sqlc.New(fixture.db).CountRolloutLaneActiveParentsForTest(
		t.Context(),
		sqlc.CountRolloutLaneActiveParentsForTestParams{
			LaneID: fixture.lane.ID,
			OrgID:  fixture.orgID,
		},
	)
	require.NoError(t, err)
	assert.Zero(t, claimCount)
}

func TestModelChildRestartReconstructsCompositeRuntimeStateWithoutDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "restart-composite")
	running := fixture.children[0]
	created := fixture.children[1]
	completedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	_, err := fixture.db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed',
		    completed_at = $2
		WHERE id = $1
	`, running.Batches[0].ID, completedAt)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `
		UPDATE firmware_rollout
		SET state = 'created',
		    started_at = NULL
		WHERE id = $1 AND org_id = $2
	`, created.ID, fixture.orgID)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_control (
		    id, rollout_id, org_id, batch_id, operation, idempotency_key,
		    request_fingerprint, expected_revision, resulting_revision,
		    status, admission_attempt, created_by_user_id
		)
		VALUES (
		    gen_random_uuid(), $1, $2, $3, 'admit', $4,
		    repeat('f', 64), $5::bigint, $5::bigint + 1, 'started', 0, $6
		)
	`, created.ID, fixture.orgID, created.Batches[0].ID,
		"u6-restart-ambiguous-admit", created.Revision, fixture.actorID)
	require.NoError(t, err)

	var beforeChildren, beforeControls int64
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM firmware_rollout WHERE group_id = $1 AND org_id = $2
	`, fixture.parentID, fixture.orgID).Scan(&beforeChildren))
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_control
		WHERE rollout_id = $1 AND idempotency_key = $2
	`, created.ID, "u6-restart-ambiguous-admit").Scan(&beforeControls))

	restartedEvidence := sqlstores.NewSQLRolloutEvidenceStore(fixture.db)
	candidates, err := restartedEvidence.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	require.NotEmpty(t, candidates)
	assert.Equal(t, running.ID, candidates[0].RolloutID)

	restartedService := rollout.NewService(
		sqlstores.NewSQLRolloutStore(fixture.db),
		betweenchannel.NewStrategy(sqlstores.NewSQLRolloutLaneStore(fixture.db)),
	)
	parent, err := restartedService.GetGroup(t.Context(), fixture.orgID, fixture.parentID)
	require.NoError(t, err)
	require.Len(t, parent.Children, 2)
	assert.Equal(t, rollout.StateRunning, parent.Children[0].State)
	assert.Equal(t, rollout.StateCreated, parent.Children[1].State)

	var afterChildren, afterControls int64
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM firmware_rollout WHERE group_id = $1 AND org_id = $2
	`, fixture.parentID, fixture.orgID).Scan(&afterChildren))
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_control
		WHERE rollout_id = $1 AND idempotency_key = $2
	`, created.ID, "u6-restart-ambiguous-admit").Scan(&afterControls))
	assert.Equal(t, beforeChildren, afterChildren)
	assert.Equal(t, beforeControls, afterControls)

	claimCount, err := sqlc.New(fixture.db).CountRolloutLaneActiveParentsForTest(
		t.Context(),
		sqlc.CountRolloutLaneActiveParentsForTestParams{
			LaneID: fixture.lane.ID,
			OrgID:  fixture.orgID,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), claimCount)
}

func TestModelChildPostTerminalRevertUsesModelPointerAndRejectsStaleWork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("successful child restores source only after revert settles", func(t *testing.T) {
		fixture := startSingleModelChild(t, "u6-success-revert", 1)
		completed := completeModelChildSuccessfully(t, fixture)
		lane, err := fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, fixture.targetChannelID, lane.Models[0].CurrentChannelID)

		reverting, err := fixture.rolloutService.Revert(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        completed.ID,
			ExpectedRevision: completed.Revision,
			IdempotencyKey:   "u6-success-revert",
			Reason:           "restore successful child source",
			ActorUserID:      fixture.actorID,
		})
		require.NoError(t, err)
		assert.Equal(t, rollout.StateReverting, reverting.State)
		var evidenceStatus, cancellationReason string
		var cancelledAt time.Time
		require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
			SELECT evidence_status, evidence_cancellation_reason, evidence_cancelled_at
			FROM firmware_rollout_batch
			WHERE rollout_id = $1 AND org_id = $2
		`, completed.ID, fixture.orgID).Scan(
			&evidenceStatus,
			&cancellationReason,
			&cancelledAt,
		))
		assert.Equal(t, "cancelled", evidenceStatus)
		assert.Contains(t, cancellationReason, "restore successful child source")
		assert.False(t, cancelledAt.IsZero())
		lane, err = fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, fixture.targetChannelID, lane.Models[0].CurrentChannelID)

		reverting, err = fixture.rolloutService.Get(t.Context(), fixture.orgID, completed.ID)
		require.NoError(t, err)
		member := reverting.Members[0]
		require.NotNil(t, member.EnforcementID)
		confirmEnforcement(t, fixture.db, *member.EnforcementID)
		finalizations, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, finalizations, 1)
		_, err = fixture.laneStore.Finalize(t.Context(), finalizations[0])
		require.NoError(t, err)

		reverted, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, completed.ID)
		require.NoError(t, err)
		assert.Equal(t, rollout.StateReverted, reverted.State)
		lane, err = fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, fixture.sourceChannelID, lane.Models[0].CurrentChannelID)
	})

	t.Run("pointer movement rejects old successful child", func(t *testing.T) {
		fixture := startSingleModelChild(t, "u6-pointer-moved", 1)
		completed := completeModelChildSuccessfully(t, fixture)
		_, err := fixture.db.ExecContext(t.Context(), `
			UPDATE rollout_lane_model
			SET current_channel_id = $4,
			    current_release_set_id = $5,
			    current_release_target_id = $6,
			    revision = revision + 1
			WHERE lane_id = $1
			  AND id = $2
			  AND org_id = $3
		`, fixture.laneID, *completed.LaneModelID, fixture.orgID,
			fixture.sourceChannelID, *completed.SourceReleaseSetID, *completed.SourceReleaseTargetID)
		require.NoError(t, err)

		_, err = fixture.rolloutService.Revert(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        completed.ID,
			ExpectedRevision: completed.Revision,
			IdempotencyKey:   "u6-reject-moved-pointer",
			Reason:           "old child must not overwrite newer pointer",
			ActorUserID:      fixture.actorID,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "revision")
	})

	t.Run("newer model child rejects old successful child", func(t *testing.T) {
		fixture := startSingleModelChild(t, "u6-newer-work", 1)
		completed := completeModelChildSuccessfully(t, fixture)
		require.NotNil(t, completed.GroupID)
		_, err := fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_batch
			SET evidence_status = 'finalized',
			    post_window_finalized = TRUE,
			    post_window_finalized_at = CURRENT_TIMESTAMP
			WHERE rollout_id = $1 AND org_id = $2
		`, completed.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_evidence
			SET status = 'completed'
			WHERE rollout_id = $1 AND org_id = $2 AND status = 'open'
		`, completed.ID, fixture.orgID)
		require.NoError(t, err)
		_, err = fixture.rolloutService.GetGroup(
			t.Context(), fixture.orgID, *completed.GroupID,
		)
		require.NoError(t, err)

		lane, err := fixture.laneService.GetLane(
			t.Context(), fixture.orgID, fixture.laneID, false, nil,
		)
		require.NoError(t, err)
		model := lane.Models[0]
		nextTarget := testLaneTargetForModel("TestMiner", "3.0.0", "e")
		_, err = fixture.laneService.StartRollout(
			t.Context(),
			betweenchannel.StartRolloutRequest{
				OrgID: fixture.orgID, LaneID: fixture.laneID, Name: "newer model work",
				IdempotencyKey: "u6-newer-parent", Reason: "prove old revert rejection",
				ActorUserID: fixture.actorID,
				ModelPlans: []betweenchannel.StartRolloutModelPlan{{
					LaneModelID: model.ID, ExpectedModelRevision: model.Revision,
					FirmwareFileID: nextTarget.FirmwareFileID,
					ReleaseTarget:  nextTarget,
					ModelStartKey:  "u6-newer-child",
					Batches: []rollout.CreateBatch{{
						Label: "all",
						Members: []rollout.CreateMember{{
							DeviceIdentifier: fixture.deviceID,
						}},
					}},
				}},
			},
		)
		require.NoError(t, err)

		_, err = fixture.rolloutService.Revert(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        completed.ID,
			ExpectedRevision: completed.Revision,
			IdempotencyKey:   "u6-reject-newer-work",
			Reason:           "old child must not replace newer work",
			ActorUserID:      fixture.actorID,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "revision")
	})
}

func TestModelChildSplitRevertSelectsOnlyTargetBoundSuccesses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startSingleModelChild(t, "u6-split-revert", 2)
	current, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	require.Len(t, current.Members, 2)
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
	require.NotNil(t, current.Members[0].EnforcementID)
	require.NotNil(t, current.Members[1].EnforcementID)
	attentionEnforcement(t, fixture.db, *current.Members[1].EnforcementID)
	finalizations, err := fixture.laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 2)
	for _, finalization := range finalizations {
		_, err = fixture.laneStore.Finalize(t.Context(), finalization)
		require.NoError(t, err)
	}
	current, err = fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []rollout.MemberState{
		rollout.MemberStateSucceeded,
		rollout.MemberStateAttentionRequired,
	}, []rollout.MemberState{current.Members[0].State, current.Members[1].State})
	split, err := fixture.rolloutService.Complete(t.Context(), rollout.ControlRequest{
		OrgID:            fixture.orgID,
		RolloutID:        current.ID,
		ExpectedRevision: current.Revision,
		IdempotencyKey:   "u6-complete-split",
		Reason:           "accept one failed member",
		ActorUserID:      fixture.actorID,
		WithFailures:     true,
	})
	require.NoError(t, err)
	assert.Equal(t, rollout.StateCompletedWithFailures, split.State)

	reverting, err := fixture.rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            fixture.orgID,
		RolloutID:        split.ID,
		ExpectedRevision: split.Revision,
		IdempotencyKey:   "u6-revert-split",
		Reason:           "restore only target-bound success",
		ActorUserID:      fixture.actorID,
	})
	require.NoError(t, err)
	var revertingCount, attentionCount int
	for _, member := range reverting.Members {
		switch member.State {
		case rollout.MemberStateReverting:
			revertingCount++
			require.NotNil(t, member.EnforcementID)
			confirmEnforcement(t, fixture.db, *member.EnforcementID)
		case rollout.MemberStateAttentionRequired:
			attentionCount++
		case rollout.MemberStatePending,
			rollout.MemberStateAdmitted,
			rollout.MemberStateSucceeded,
			rollout.MemberStateFailed,
			rollout.MemberStateCancelled,
			rollout.MemberStateReverted:
			t.Fatalf("unexpected split member state %q", member.State)
		}
	}
	assert.Equal(t, 1, revertingCount)
	assert.Equal(t, 1, attentionCount)
	lane, err := fixture.laneService.GetLane(
		t.Context(), fixture.orgID, fixture.laneID, false, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, fixture.sourceChannelID, lane.Models[0].CurrentChannelID)

	finalizations, err = fixture.laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = fixture.laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)
	reverted, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	assert.Equal(t, rollout.StateReverted, reverted.State)
	lane, err = fixture.laneService.GetLane(
		t.Context(), fixture.orgID, fixture.laneID, false, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, fixture.sourceChannelID, lane.Models[0].CurrentChannelID)
	require.NotNil(t, reverted.GroupID)
	_, err = fixture.rolloutService.GetGroup(t.Context(), fixture.orgID, *reverted.GroupID)
	require.NoError(t, err)
	retryTarget := testLaneTargetForModel("TestMiner", "3.0.0", "9")
	_, err = fixture.laneService.StartRollout(
		t.Context(),
		betweenchannel.StartRolloutRequest{
			OrgID: fixture.orgID, LaneID: fixture.laneID, Name: "retry split model",
			IdempotencyKey: "u6-split-retry-parent", Reason: "split is closed",
			ActorUserID: fixture.actorID,
			ModelPlans: []betweenchannel.StartRolloutModelPlan{{
				LaneModelID: lane.Models[0].ID, ExpectedModelRevision: lane.Models[0].Revision,
				FirmwareFileID: retryTarget.FirmwareFileID,
				ReleaseTarget:  retryTarget,
				ModelStartKey:  "u6-split-retry-child",
				Batches: []rollout.CreateBatch{{
					Label: "all",
					Members: []rollout.CreateMember{
						{DeviceIdentifier: fixture.deviceIDs[0]},
						{DeviceIdentifier: fixture.deviceIDs[1]},
					},
				}},
			}},
		},
	)
	require.NoError(t, err)
}

func TestRolloutLaneArchiveRejectsEachUnsettledChildWorkClass(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	for _, blocker := range []string{
		"control",
		"enforcement",
		"finalization",
		"revert",
		"ownership",
		"authority",
		"evidence",
	} {
		t.Run(blocker, func(t *testing.T) {
			db, orgID, actorID, lane, completed := setupSettledRolloutLane(t, "u6-"+blocker)
			seedArchiveBlocker(t, db, orgID, actorID, completed, blocker)
			service := betweenchannel.NewService(sqlstores.NewSQLRolloutLaneStore(db), nil)

			err := service.DeleteLane(
				t.Context(),
				deleteLaneRequest(lane, orgID, actorID, "u6-"+blocker),
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "child "+blocker)
		})
	}
}

func TestRolloutLaneArchiveSettledModelChildrenPreservesHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	fixture := startTwoModelChildren(t, "archive-history")
	for index, child := range fixture.children {
		_, err := fixture.rolloutService.Abort(t.Context(), rollout.ControlRequest{
			OrgID:            fixture.orgID,
			RolloutID:        child.ID,
			ExpectedRevision: child.Revision,
			IdempotencyKey:   fmt.Sprintf("u6-archive-abort-%d", index),
			Reason:           "settle child before archive",
			ActorUserID:      fixture.actorID,
		})
		require.NoError(t, err)
	}
	parent, err := fixture.rolloutService.GetGroup(
		t.Context(), fixture.orgID, fixture.parentID,
	)
	require.NoError(t, err)
	require.True(t, parent.ResultReady)
	auditCounts := func() (int64, int64, int64, int64) {
		var parents, children, controls, evidence int64
		require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
			SELECT
			    (SELECT COUNT(*) FROM firmware_rollout_group WHERE id = $1 AND org_id = $2),
			    (SELECT COUNT(*) FROM firmware_rollout WHERE group_id = $1 AND org_id = $2),
			    (
			        SELECT COUNT(*)
			        FROM firmware_rollout_control control
			        JOIN firmware_rollout child
			          ON child.id = control.rollout_id
			         AND child.org_id = control.org_id
			        WHERE child.group_id = $1 AND child.org_id = $2
			    ),
			    (
			        SELECT COUNT(*)
			        FROM firmware_rollout_evidence evidence
			        JOIN firmware_rollout child
			          ON child.id = evidence.rollout_id
			         AND child.org_id = evidence.org_id
			        WHERE child.group_id = $1 AND child.org_id = $2
			    )
		`, fixture.parentID, fixture.orgID).Scan(&parents, &children, &controls, &evidence))
		return parents, children, controls, evidence
	}
	beforeParent, beforeChildren, beforeControls, beforeEvidence := auditCounts()
	assert.Equal(t, int64(1), beforeParent)
	assert.Equal(t, int64(2), beforeChildren)
	assert.Positive(t, beforeControls)
	assert.Positive(t, beforeEvidence)

	laneService := betweenchannel.NewService(
		sqlstores.NewSQLRolloutLaneStore(fixture.db),
		nil,
	)
	require.NoError(t, laneService.DeleteLane(
		t.Context(),
		deleteLaneRequest(fixture.lane, fixture.orgID, fixture.actorID, "u6-model-history"),
	))

	var activeBindings, physicalMemberships int64
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM rollout_lane_model_binding
		WHERE lane_id = $1 AND org_id = $2 AND ended_at IS NULL
	`, fixture.lane.ID, fixture.orgID).Scan(&activeBindings))
	require.NoError(t, fixture.db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM device_set_membership membership
		JOIN rollout_lane_channel channel
		  ON channel.channel_id = membership.device_set_id
		 AND channel.org_id = membership.org_id
		WHERE channel.lane_id = $1
		  AND channel.org_id = $2
		  AND membership.device_set_type = 'channel'
	`, fixture.lane.ID, fixture.orgID).Scan(&physicalMemberships))
	assert.Zero(t, activeBindings)
	assert.Zero(t, physicalMemberships)
	afterParent, afterChildren, afterControls, afterEvidence := auditCounts()
	assert.Equal(t, beforeParent, afterParent)
	assert.Equal(t, beforeChildren, afterChildren)
	assert.Equal(t, beforeControls, afterControls)
	assert.Equal(t, beforeEvidence, afterEvidence)

	archived, err := laneService.GetLaneForRollout(
		t.Context(), fixture.orgID, fixture.parentID,
	)
	require.NoError(t, err)
	assert.Equal(t, fixture.lane.ID, archived.ID)
	assert.Equal(t, fixture.lane.Label, archived.Label)
	require.Len(t, archived.Models, 2)
	for _, model := range archived.Models {
		assert.Zero(t, model.Bindings.ActiveCount)
		assert.Positive(t, model.Bindings.HistoricalCount)
	}
	archivedChild, err := fixture.rolloutService.Get(
		t.Context(), fixture.orgID, fixture.children[0].ID,
	)
	require.NoError(t, err)
	_, err = fixture.rolloutService.Revert(t.Context(), rollout.ControlRequest{
		OrgID:            fixture.orgID,
		RolloutID:        archivedChild.ID,
		ExpectedRevision: archivedChild.Revision,
		IdempotencyKey:   "u6-archived-child-revert-rejected",
		Reason:           "archived lane is immutable",
		ActorUserID:      fixture.actorID,
	})
	require.Error(t, err)
}

func seedArchiveBlocker(
	t *testing.T,
	db *sql.DB,
	orgID int64,
	actorID int64,
	child *rollout.Rollout,
	blocker string,
) {
	t.Helper()
	switch blocker {
	case "control":
		_, err := db.ExecContext(t.Context(), `
			INSERT INTO firmware_rollout_control (
			    id, rollout_id, org_id, operation, idempotency_key,
			    request_fingerprint, expected_revision, resulting_revision,
			    status, created_by_user_id
			)
			VALUES (
			    gen_random_uuid(), $1, $2, 'complete', $3,
			    repeat('a', 64), $4::bigint, $4::bigint + 1, 'started', $5
			)
		`, child.ID, orgID, "u6-archive-control", child.Revision, actorID)
		require.NoError(t, err)
	case "enforcement":
		result, err := db.ExecContext(t.Context(), `
			INSERT INTO channel_firmware_enforcement (
			    org_id, device_id, desired_release_set_id,
			    desired_release_target_id, desired_firmware_file_id,
			    desired_firmware_version, cause_type, cause_reference,
			    authority_id, authority_revision, state
			)
			SELECT child.org_id,
			       member.device_id,
			       child.target_release_set_id,
			       member.target_release_target_id,
			       target.firmware_file_id,
			       target.firmware_version,
			       'between_channel_rollout',
			       child.id::text,
			       child.forward_authority_id,
			       child.forward_authority_revision,
			       'pending'
			FROM firmware_rollout child
			JOIN firmware_rollout_member member
			  ON member.rollout_id = child.id AND member.org_id = child.org_id
			JOIN firmware_release_target target
			  ON target.id = member.target_release_target_id
			 AND target.release_set_id = child.target_release_set_id
			 AND target.org_id = child.org_id
			WHERE child.id = $1 AND child.org_id = $2
			LIMIT 1
		`, child.ID, orgID)
		require.NoError(t, err)
		rows, err := result.RowsAffected()
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
	case "finalization":
		_, err := db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_member
			SET state = 'admitted', owner_released_at = NULL
			WHERE rollout_id = $1 AND org_id = $2
		`, child.ID, orgID)
		require.NoError(t, err)
	case "revert":
		_, err := db.ExecContext(t.Context(), `
			UPDATE firmware_rollout
			SET state = 'reverting', reverting_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND org_id = $2
		`, child.ID, orgID)
		require.NoError(t, err)
	case "ownership":
		_, err := db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_member
			SET owner_released_at = NULL
			WHERE rollout_id = $1 AND org_id = $2
		`, child.ID, orgID)
		require.NoError(t, err)
	case "authority":
		_, err := db.ExecContext(t.Context(), `
			UPDATE channel_firmware_authority
			SET halted_at = NULL
			WHERE id = $1 AND org_id = $2
		`, child.ForwardAuthorityID, orgID)
		require.NoError(t, err)
	case "evidence":
		_, err := db.ExecContext(t.Context(), `
			INSERT INTO firmware_rollout_evidence (
			    rollout_id, member_id, org_id, phase, window_start, window_end
			)
			SELECT rollout_id, id, org_id, 'baseline',
			       CURRENT_TIMESTAMP - INTERVAL '2 minutes',
			       CURRENT_TIMESTAMP - INTERVAL '1 minute'
			FROM firmware_rollout_member
			WHERE rollout_id = $1 AND org_id = $2
			LIMIT 1
		`, child.ID, orgID)
		require.NoError(t, err)
	default:
		t.Fatalf("unknown archive blocker %q", blocker)
	}
}

func completeModelChildSuccessfully(
	t *testing.T,
	fixture modelChildFixture,
) *rollout.Rollout {
	t.Helper()
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
	require.Len(t, finalizations, len(fixture.deviceIDs))
	for _, finalization := range finalizations {
		_, err = fixture.laneStore.Finalize(t.Context(), finalization)
		require.NoError(t, err)
	}
	completed, err := fixture.rolloutService.Get(t.Context(), fixture.orgID, fixture.child.ID)
	require.NoError(t, err)
	require.Equal(t, rollout.StateCompleted, completed.State)
	return completed
}

type twoModelChildrenFixture struct {
	db             *sql.DB
	orgID          int64
	actorID        int64
	lane           *betweenchannel.Lane
	parentID       uuid.UUID
	children       []*rollout.Rollout
	rolloutService *rollout.Service
}

func startTwoModelChildren(t *testing.T, suffix string) twoModelChildrenFixture {
	t.Helper()
	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	setDiscoveredModel(t, db, orgID, deviceIDs[1], "TestMinerB")
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID: orgID, Label: "U6 " + suffix,
		ReleaseTargets: []betweenchannel.ReleaseTarget{
			testLaneTargetForModel("TestMiner", "1.0.0", "a"),
			testLaneTargetForModel("TestMinerB", "1.0.0", "b"),
		},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "u6-create-" + suffix,
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	queries := sqlc.New(db)
	require.NoError(t, queries.RunRolloutLaneTopologyBackfill(t.Context(), orgID))
	readiness, err := laneService.GetTopologyReadiness(t.Context(), orgID)
	require.NoError(t, err)
	_, err = laneService.EnableTopology(t.Context(), betweenchannel.EnableTopologyRequest{
		OrgID: orgID, ExpectedRevision: readiness.Revision,
		IdempotencyKey: "u6-enable-" + suffix, Reason: "U6 fixture", ActorUserID: actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	firstModel := laneModelByName(t, lane, "TestMiner")
	secondModel := laneModelByName(t, lane, "TestMinerB")
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID: orgID, LaneID: lane.ID, Name: "U6 two model children",
		IdempotencyKey: "u6-parent-" + suffix, Reason: "U6 fixture", ActorUserID: actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{
			{
				LaneModelID: firstModel.ID, ExpectedModelRevision: firstModel.Revision,
				FirmwareFileID: "u6-first-target",
				ReleaseTarget:  testLaneTargetForModel("TestMiner", "2.0.0", "c"),
				ModelStartKey:  "u6-first-" + suffix,
				HashratePolicy: &rollout.HashratePolicy{
					MaxDropBasisPoints: 100, HealthyDurationSeconds: 10,
				},
				Batches: []rollout.CreateBatch{{
					Label: "first", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
				}},
			},
			{
				LaneModelID: secondModel.ID, ExpectedModelRevision: secondModel.Revision,
				FirmwareFileID: "u6-second-target",
				ReleaseTarget:  testLaneTargetForModel("TestMinerB", "2.0.0", "d"),
				ModelStartKey:  "u6-second-" + suffix,
				Batches: []rollout.CreateBatch{{
					Label: "second", Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[1]}},
				}},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, started.Parent)
	service := rollout.NewService(
		sqlstores.NewSQLRolloutStore(db),
		betweenchannel.NewStrategy(laneStore),
	)
	children := make([]*rollout.Rollout, 0, len(started.Children))
	for index, item := range started.Children {
		admitted, admitErr := service.Admit(t.Context(), rollout.AdmitRequest{
			OrgID: orgID, RolloutID: item.Child.ID, BatchID: item.FirstBatchID,
			ExpectedRevision: item.Child.Revision,
			IdempotencyKey:   item.Child.ID.String() + ":admit:0",
			Reason:           "admit U6 fixture child",
			ActorUserID:      actorID,
		})
		require.NoError(t, admitErr, "admit child %d", index)
		children = append(children, admitted)
	}
	return twoModelChildrenFixture{
		db: db, orgID: orgID, actorID: actorID, lane: lane,
		parentID: started.Parent.ID, children: children, rolloutService: service,
	}
}
