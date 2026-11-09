package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/stretchr/testify/require"
)

func TestPoolRemovalPreservesNativeCurtailmentAdmission(t *testing.T) {
	for _, state := range []string{"applying", "uncertain"} {
		for _, driver := range []string{"proto", "unclassified-pool-fallback"} {
			for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig, commandtype.UpdateMiningPools} {
				t.Run(state+"/"+driver+"/"+kind.String(), func(t *testing.T) {
					database, org, device, message, _ := queuedFenceFixture(t, kind)
					_, err := database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET driver_name=$1,device_class=1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, driver, device)
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
					_, err = database.DB.ExecContext(t.Context(), `INSERT INTO pool_configuration_operation_target(organization_id,operation_id,target_kind,target_id,assignment_version,state,reviewed_binding) VALUES($1,$2,'device',$3,1,$4,'{}')`, org, operation, device, state)
					require.NoError(t, err)
					service := devicewrite.NewService(database.DB)
					allowed := driver == "proto" && kind.IsCurtailmentControl()
					called := false
					call := func(ctx context.Context) error {
						return devicewrite.Dispatch(ctx, func(context.Context) error { called = true; return nil })
					}
					if kind.IsCurtailmentControl() {
						err = service.WithCurtailmentAttempt(t.Context(), org, device, 0, kind, call)
					} else {
						err = service.WithHostAttempt(t.Context(), org, device, 0, kind, call)
					}
					if allowed {
						require.NoError(t, err)
					} else {
						require.ErrorContains(t, err, "pool removal is unfinished")
					}
					require.Equal(t, allowed, called, "direct admission")
					attempt, err := service.BeginQueuedHost(t.Context(), message)
					if !allowed {
						require.ErrorContains(t, err, "pool removal is unfinished")
						return
					}
					require.NoError(t, err)
					called = false
					require.NoError(t, service.WithCurtailmentAttempt(attempt.Context(t.Context()), org, device, 0, kind, call))
					require.True(t, called, "queued admission must retain the real dispatch guard")
					_, err = database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET driver_name='unclassified-pool-fallback' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device)
					require.NoError(t, err)
					require.Error(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error {
						t.Fatal("a changed backend must not reuse native curtailment admission")
						return nil
					}))
					require.NoError(t, attempt.Finish(t.Context(), nil))
				})
			}
		}
	}
}
