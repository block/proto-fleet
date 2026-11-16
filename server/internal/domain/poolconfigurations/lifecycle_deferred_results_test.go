package poolconfigurations

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeferredAutomaticOperationReportsPending(t *testing.T) {
	for _, kind := range []string{"apply", "clear"} {
		t.Run(kind, func(t *testing.T) {
			conn, scope, repo, target, config := defaultRetirementFixture(t)
			ctx := t.Context()
			q := sqlc.New(conn)
			require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{target.DeviceID}))
			require.NoError(t, q.EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: scope.OrganizationID, DeviceID: target.DeviceID}))
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET operation_kind='deactivate',status='pending' WHERE device_id=$1`, target.DeviceID)
			require.NoError(t, err)
			var configID, revisionID sql.NullString
			if kind == "apply" {
				configID = sql.NullString{String: config.ID, Valid: true}
				revisionID = sql.NullString{String: config.RevisionID, Valid: true}
			}
			var id uuid.UUID
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id)
 VALUES($1,$2,5,$2,'ownership',$3,$4) RETURNING transition_id`, scope.OrganizationID, target.DeviceID, configID, revisionID).Scan(&id))
			event, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "deferred", event.State, "the active deactivation must retain automatic policy without dispatching it")
			operation, err := repo.GetOperation(ctx, q, scope, id.String())
			require.NoError(t, err)
			require.Equal(t, kind, operation.Kind)
			require.Len(t, operation.Targets, 1)
			require.Equal(t, "pending", operation.Targets[0].State)
			require.Equal(t, TargetDevice, operation.Targets[0].Kind)
			require.Equal(t, target.DeviceID, operation.Targets[0].ID)
		})
	}
}
