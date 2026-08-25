package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	infrastructuredb "github.com/block/proto-fleet/server/internal/infrastructure/db"
)

var errCommitResponseLost = errors.New("commit response lost after server may have committed")

type ambiguousCommitTx struct {
	*sql.Tx
}

func (tx *ambiguousCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return fmt.Errorf("commit ambiguous test transaction: %w", err)
	}
	return errCommitResponseLost
}

type ambiguousCommitTransactor struct {
	db            *sql.DB
	callbackCalls int
}

func (t *ambiguousCommitTransactor) RunInTx(
	ctx context.Context,
	action func(context.Context) error,
) error {
	_, err := t.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		return nil, action(txCtx)
	})
	return err
}

func (t *ambiguousCommitTransactor) RunInTxWithResult(
	ctx context.Context,
	action func(context.Context) (any, error),
) (any, error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ambiguous test transaction: %w", err)
	}
	ambiguous := &ambiguousCommitTx{Tx: tx}
	defer func() { _ = ambiguous.Rollback() }()

	t.callbackCalls++
	txCtx := infrastructuredb.WithTxQueries(ctx, sqlc.New(ambiguous))
	result, err := action(txCtx)
	if err != nil {
		return nil, err
	}
	if err := ambiguous.Commit(); err != nil {
		return nil, &infrastructuredb.TransactionOutcomeUnknownError{Err: err}
	}
	return result, nil
}

type recordingConcreteAdmissionStrategy struct {
	delegate *betweenchannel.Strategy
	calls    int
	result   rollout.AdmissionResult
}

func (s *recordingConcreteAdmissionStrategy) Key() string {
	return s.delegate.Key()
}

func (s *recordingConcreteAdmissionStrategy) Admit(
	ctx context.Context,
	req rollout.AdmissionRequest,
) rollout.AdmissionResult {
	s.calls++
	s.result = s.delegate.Admit(ctx, req)
	return s.result
}

func (s *recordingConcreteAdmissionStrategy) Revert(
	ctx context.Context,
	req rollout.RevertRequest,
) rollout.RevertResult {
	return s.delegate.Revert(ctx, req)
}

func TestModelChildAdmissionCommitOutcomeUnknownReconcilesAndReplaysWithoutDuplicateDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupRolloutLaneTestData(t, 1)
	actorID := testOrganizationUserID(t, db, orgID)
	normalLaneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(normalLaneStore, nil)
	created, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Ambiguous commit lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTargetForModel("TestMiner", "1.0.0", "a")},
		DeviceIdentifiers: deviceIDs,
		IdempotencyKey:    "ambiguous-commit-create",
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
		IdempotencyKey:   "ambiguous-commit-enable",
		Reason:           "exercise commit ambiguity",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	lane, err := laneService.GetLane(t.Context(), orgID, created.ID, false, nil)
	require.NoError(t, err)
	model := laneModelByName(t, lane, "TestMiner")
	target := testLaneTargetForModel("TestMiner", "2.0.0", "b")
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Ambiguous commit child",
		IdempotencyKey: "ambiguous-commit-parent",
		Reason:         "exercise commit ambiguity",
		ActorUserID:    actorID,
		ModelPlans: []betweenchannel.StartRolloutModelPlan{{
			LaneModelID:           model.ID,
			ExpectedModelRevision: model.Revision,
			FirmwareFileID:        target.FirmwareFileID,
			ReleaseTarget:         target,
			ModelStartKey:         "ambiguous-commit-child",
			Batches: []rollout.CreateBatch{{
				Label:   "all",
				Members: []rollout.CreateMember{{DeviceIdentifier: deviceIDs[0]}},
			}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, started.Children, 1)
	child := started.Children[0].Child
	admissionKey := "ambiguous-commit-child:admit:0"

	transactor := &ambiguousCommitTransactor{db: db}
	ambiguousLaneStore := sqlstores.NewSQLRolloutLaneStoreWithTransactor(db, transactor)
	strategy := &recordingConcreteAdmissionStrategy{
		delegate: betweenchannel.NewStrategy(ambiguousLaneStore),
	}
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	service := rollout.NewService(rolloutStore, strategy)
	request := rollout.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        child.ID,
		BatchID:          started.Children[0].FirstBatchID,
		ExpectedRevision: child.Revision,
		IdempotencyKey:   admissionKey,
		Reason:           "exercise commit ambiguity",
		ActorUserID:      actorID,
	}

	_, err = service.Admit(t.Context(), request)
	require.ErrorContains(t, err, "replay the same idempotency key")
	require.ErrorIs(t, strategy.result.Err, errCommitResponseLost)
	assert.Equal(t, rollout.AdmissionOutcomeUnknown, strategy.result.Outcome)
	assert.Equal(t, 1, strategy.calls)
	assert.Equal(t, 1, transactor.callbackCalls)

	persisted, err := service.Get(t.Context(), orgID, child.ID)
	require.NoError(t, err)
	require.Len(t, persisted.Batches, 1)
	assert.Equal(t, rollout.StateRunning, persisted.State)
	assert.Equal(t, rollout.BatchStateAdmitted, persisted.Batches[0].State)
	assert.Equal(t, int32(0), persisted.Batches[0].AdmissionAttempt)

	var (
		controlStatus    string
		controlAttempt   int32
		controlCount     int64
		enforcementCount int64
		attachedCount    int64
	)
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT MIN(status), MIN(admission_attempt), COUNT(*)
		FROM firmware_rollout_control
		WHERE rollout_id = $1 AND org_id = $2 AND idempotency_key = $3
	`, child.ID, orgID, admissionKey).Scan(&controlStatus, &controlAttempt, &controlCount))
	assert.Equal(t, string(rollout.ControlStatusStarted), controlStatus)
	assert.Equal(t, int32(0), controlAttempt)
	assert.Equal(t, int64(1), controlCount)
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM channel_firmware_enforcement
		WHERE org_id = $1
		  AND authority_id = $2
		  AND cause_type = 'between_channel_forward'
	`, orgID, child.ForwardAuthorityID).Scan(&enforcementCount))
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_member
		WHERE rollout_id = $1 AND org_id = $2 AND enforcement_id IS NOT NULL
	`, child.ID, orgID).Scan(&attachedCount))
	assert.Equal(t, int64(1), enforcementCount)
	assert.Equal(t, int64(1), attachedCount)

	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_control
		SET updated_at = CURRENT_TIMESTAMP - INTERVAL '1 minute'
		WHERE rollout_id = $1 AND org_id = $2 AND idempotency_key = $3
	`, child.ID, orgID, admissionKey)
	require.NoError(t, err)
	reconciler := rollout.NewControlReconciler(
		rollout.ControlReconcilerConfig{BatchSize: 10, StaleAfter: time.Nanosecond},
		rolloutStore,
	)
	reconciler.RunOnce(t.Context())
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT status
		FROM firmware_rollout_control
		WHERE rollout_id = $1 AND org_id = $2 AND idempotency_key = $3
	`, child.ID, orgID, admissionKey).Scan(&controlStatus))
	assert.Equal(t, string(rollout.ControlStatusSucceeded), controlStatus)

	replayed, err := service.Admit(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, child.ID, replayed.ID)
	assert.Equal(t, 1, strategy.calls, "settled same-key replay must not redispatch strategy work")
	assert.Equal(t, 1, transactor.callbackCalls)
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM channel_firmware_enforcement
		WHERE org_id = $1
		  AND authority_id = $2
		  AND cause_type = 'between_channel_forward'
	`, orgID, child.ForwardAuthorityID).Scan(&enforcementCount))
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_member
		WHERE rollout_id = $1 AND org_id = $2 AND enforcement_id IS NOT NULL
	`, child.ID, orgID).Scan(&attachedCount))
	assert.Equal(t, int64(1), enforcementCount)
	assert.Equal(t, int64(1), attachedCount)
}
