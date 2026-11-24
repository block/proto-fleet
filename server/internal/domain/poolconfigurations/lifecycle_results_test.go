package poolconfigurations

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticClearReportsPhysicalWriteRecovery(t *testing.T) {
	const applying = "applying"
	conn, scope, repo, target, _ := defaultRetirementFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	var id uuid.UUID
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,5,$2,'ownership') RETURNING transition_id`, scope.OrganizationID, target.DeviceID).Scan(&id))
	_, err := q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: target.Kind, TargetID: target.ID, ReviewedBinding: "{}"})
	require.NoError(t, err)
	var attempt uuid.UUID
	err = devicewrite.NewService(conn).WithRecordedHostAttempt(devicewrite.WithPoolOperation(ctx, id), scope.OrganizationID, target.DeviceID, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, physical uuid.UUID) error {
		attempt = physical
		_, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: id, PhysicalAttemptID: uuid.NullUUID{UUID: physical, Valid: true}})
		return err
	}, func(ctx context.Context) error {
		return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(context.DeadlineExceeded) })
	})
	require.Error(t, err)
	physical, err := q.LockDeviceWriteAttemptForRecovery(ctx, attempt)
	require.NoError(t, err)
	require.Equal(t, "unknown", physical.Status)
	for _, status := range []string{"unknown", "running", "complete"} {
		for _, state := range []string{applying, "uncertain", "completed", "superseded"} {
			t.Run(status+"/"+state, func(t *testing.T) {
				_, err := conn.ExecContext(ctx, `UPDATE device_write_attempt SET status=$2,finished_at=CASE WHEN $2='running' THEN NULL ELSE clock_timestamp() END WHERE attempt_id=$1`, attempt, status)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state=$2,result_code='clear_verification_unavailable' WHERE transition_id=$1`, id, state)
				require.NoError(t, err)
				operation, err := repo.GetOperation(ctx, q, scope, id.String())
				require.NoError(t, err)
				require.Equal(t, "clear", operation.Kind)
				require.Len(t, operation.Targets, 1)
				expected := "clear_verification_unavailable"
				if status == "unknown" && (state == applying || state == "uncertain") {
					expected = "operator_recovery_required"
				}
				require.Equal(t, expected, operation.Targets[0].ResultCode)
				require.Equal(t, state, operation.Targets[0].State)
			})
		}
	}
}
