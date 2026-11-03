package poolconfigurations

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPendingWorkSkipsBusyControllerBeforeBatchLimit(t *testing.T) {
	conn, scope, repo, controller := assignmentFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	_, err := conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, controller.DeviceID)
	require.NoError(t, err)
	var identifier string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, controller.DeviceID).Scan(&identifier))
	_, err = q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tank, module int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller.DeviceID).Scan(&tank))
	changes := make([]AssignmentChange, 100)
	for i := range changes {
		if i%3 == 0 {
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,$2,$3,now(),now()) RETURNING id`, tank, i/3+1, fmt.Sprintf("module-%d", i/3)).Scan(&module))
		}
		var board, installation int64
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, scope.OrganizationID).Scan(&board))
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,$3,now(),now()) RETURNING id`, board, module, i%3+1).Scan(&installation))
		changes[i] = AssignmentChange{Target: AssignmentTarget{Kind: TargetInstallation, ID: installation, DeviceID: controller.DeviceID}}
	}
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Pools", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
	})
	require.NoError(t, err)
	assign := func(changes []AssignmentChange) Operation {
		t.Helper()
		op, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
			return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, changes)
		})
		require.NoError(t, err)
		return op
	}
	queued := assign(changes)
	// Explicit timestamps keep ordering independent of database/host clock skew.
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET updated_at=now()-INTERVAL '1 hour' WHERE operation_id=$1`, queued.ID)
	require.NoError(t, err)
	var discovery, miner int64
	minerIdentifier := uuid.NewString()
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.2','80','http','antminer',1) RETURNING id`, scope.OrganizationID, minerIdentifier).Scan(&discovery))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:02',$3) RETURNING id`, scope.OrganizationID, minerIdentifier, discovery).Scan(&miner))
	ready := assign([]AssignmentChange{{Target: AssignmentTarget{Kind: TargetDevice, ID: miner, DeviceID: miner}}})
	require.NoError(t, q.EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: scope.OrganizationID, DeviceID: controller.DeviceID}))

	for _, status := range []string{"pending", "applying", "blocked"} {
		t.Run(status, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$2 WHERE device_id=$1`, controller.DeviceID, status)
			require.NoError(t, err)
			work, err := q.ListPendingPoolConfigurationWork(ctx)
			require.NoError(t, err)
			require.Len(t, work, 1, "100 waiting boards must not hide an unrelated ready miner")
			require.Equal(t, ready.ID, work[0].OperationID)
			require.Equal(t, miner, work[0].DeviceID)
		})
	}
	retained, err := repo.GetOperation(ctx, q, scope, queued.ID)
	require.NoError(t, err)
	require.Len(t, retained.Targets, 100)
	for _, target := range retained.Targets {
		require.Equal(t, "pending", target.State, "skipped boards must remain queued")
	}
	for _, status := range []string{"idle", "complete", "failed"} {
		t.Run(status, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$2 WHERE device_id=$1`, controller.DeviceID, status)
			require.NoError(t, err)
			work, err := q.ListPendingPoolConfigurationWork(ctx)
			require.NoError(t, err)
			require.Len(t, work, 100, "waiting boards become eligible once the controller is free")
			for _, row := range work {
				require.Equal(t, queued.ID, row.OperationID)
				require.Equal(t, controller.DeviceID, row.DeviceID)
			}
		})
	}
}
