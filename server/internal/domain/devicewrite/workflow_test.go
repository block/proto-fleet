package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompleteWorkflowRetiresAdmissionAndPermitsHandoff(t *testing.T) {
	database, orgID, deviceID, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	ctx := t.Context()
	var attemptID uuid.UUID
	var attemptEpoch int64
	require.NoError(t, service.BeginWorkflow(ctx, orgID, deviceID, func(_ context.Context, _ sqlc.Querier, id uuid.UUID, epoch int64) error {
		attemptID, attemptEpoch = id, epoch
		return nil
	}))
	drained, err := service.Drained(ctx, orgID, deviceID, attemptEpoch)
	require.NoError(t, err)
	require.False(t, drained, "an unfinished workflow must block handoff")

	require.NoError(t, db.WithTransactionNoResult(ctx, database.DB, func(q sqlc.Querier) error {
		if _, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: deviceID, OrganizationID: orgID}); err != nil {
			return err
		}
		return devicewrite.CompleteWorkflow(ctx, q, attemptID)
	}))
	completed, err := sqlc.New(database.DB).LockDeviceWriteAttemptForRecovery(ctx, attemptID)
	require.NoError(t, err)
	require.Equal(t, "complete", completed.Status)
	require.True(t, completed.FinishedAt.Valid)
	require.JSONEq(t, "{}", string(completed.WriteOutcome))

	epoch, err := service.Reserve(ctx, orgID, deviceID)
	require.NoError(t, err)
	drained, err = service.Drained(ctx, orgID, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained, "a settled workflow must permit handoff")
	require.NoError(t, service.ReleaseHost(ctx, orgID, deviceID, epoch))
}
