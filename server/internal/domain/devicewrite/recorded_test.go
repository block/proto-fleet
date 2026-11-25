package devicewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRecordedAdmissionCommitsBeforeIOAndRollsBackFailedClaim(t *testing.T) {
	database, org, device, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	ctx := t.Context()
	var recorded uuid.UUID
	rejected := errors.New("work claim rejected")
	err := service.WithRecordedHostAttempt(ctx, org, device, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, id uuid.UUID) error {
		recorded = id
		row, err := q.LockDeviceWriteAttemptForRecovery(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "running", row.Status)
		return rejected
	}, func(context.Context) error { t.Fatal("a failed claim must not dispatch"); return nil })
	require.ErrorIs(t, err, rejected)
	var count int
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE attempt_id=$1`, recorded).Scan(&count))
	require.Zero(t, count, "failed claims must roll back their admission too")
	err = service.WithRecordedHostAttempt(ctx, org, device, 0, commandtype.UpdateMiningPools, func(_ sqlc.Querier, id uuid.UUID) error {
		recorded = id
		return nil
	}, func(ctx context.Context) error {
		var state string
		require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, recorded).Scan(&state))
		require.Equal(t, "running", state, "admission must be visible outside the claiming transaction before dispatch")
		require.Error(t, service.WithRecordedHostAttempt(ctx, org, device, 0, commandtype.UpdateMiningPools, func(sqlc.Querier, uuid.UUID) error {
			t.Fatal("nested recorded admissions must not claim work")
			return nil
		}, func(context.Context) error { return nil }))
		return devicewrite.Dispatch(ctx, func(context.Context) error { return nil })
	})
	require.NoError(t, err)
	var status string
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, recorded).Scan(&status))
	require.Equal(t, "complete", status)
}
