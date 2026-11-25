package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/stretchr/testify/require"
)

// A pool removal can start while a Frigate update has the controllers stopped.
// It cannot finish until they return, so it must not refuse the update calls
// that bring them back.
func TestUnfinishedPoolRemovalDoesNotBlockFrigateUpdateCalls(t *testing.T) {
	database, org, device, _, _ := queuedFenceFixture(t)
	_, err := database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET driver_name='proto',device_class=1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device)
	require.NoError(t, err)
	config := id.GenerateID()
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration(configuration_id,organization_id,name) VALUES($1,$2,'Removal fixture')`, config, org)
	require.NoError(t, err)
	revision, operation := id.GenerateID(), id.GenerateID()
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration_revision(revision_id,organization_id,configuration_id,number,slots) VALUES($1,$2,$3,1,'[{"id":"slot","url":"stratum+tcp://pool.example","username":"worker","worker_template":"","has_password":false,"password_ciphertext":"encrypted"}]')`, revision, org, config)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id,kind) VALUES($1,$2,$3,$4,1,'clear')`, operation, org, config, revision)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at) VALUES($1,'device',$2,$2,$3,$4,1,now())`, org, device, config, revision)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration_operation_target(organization_id,operation_id,target_kind,target_id,assignment_version,state,reviewed_binding) VALUES($1,$2,'device',$3,1,'uncertain','{}')`, org, operation, device)
	require.NoError(t, err)

	service := devicewrite.NewService(database.DB)
	run := func(kind commandtype.Type) (bool, error) {
		called := false
		err := service.WithHostAttempt(t.Context(), org, device, 0, kind, func(ctx context.Context) error {
			return devicewrite.Dispatch(ctx, func(context.Context) error { called = true; return nil })
		})
		return called, err
	}

	called, err := run(commandtype.UpdateMiningPools)
	require.ErrorContains(t, err, "pool removal is unfinished", "a pool change still waits for the removal")
	require.False(t, called)

	called, err = run(commandtype.FrigateServerUpdate)
	require.NoError(t, err)
	require.True(t, called)
}
