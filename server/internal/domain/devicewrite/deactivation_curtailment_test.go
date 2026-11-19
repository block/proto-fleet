package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/stretchr/testify/require"
)

func TestDeactivationPreservesNativeCurtailmentAdmission(t *testing.T) {
	shared := newQueuedFenceDatabase(t)
	database, org := shared.database, shared.org
	for _, state := range []string{"pending", "applying", "blocked"} {
		for _, driver := range []string{"proto", "unclassified-pool-fallback"} {
			for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig, commandtype.UpdateMiningPools} {
				t.Run(state+"/"+driver+"/"+kind.String(), func(t *testing.T) {
					device, message := shared.queueDevice(t, kind)
					_, err := database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET driver_name=$1,device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, driver, device)
					require.NoError(t, err)
					_, err = database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate',$3)`, device, org, state)
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
						require.ErrorContains(t, err, "being disabled")
					}
					require.Equal(t, allowed, called, "direct admission")
					attempt, err := service.BeginQueuedHost(t.Context(), message)
					if !allowed {
						require.ErrorContains(t, err, "being disabled")
						return
					}
					require.NoError(t, err)
					called = false
					require.NoError(t, service.WithCurtailmentAttempt(attempt.Context(t.Context()), org, device, 0, kind, call))
					require.True(t, called, "queued admission and dispatch")
					_, err = database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET driver_name='unclassified-pool-fallback' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device)
					require.NoError(t, err)
					require.ErrorContains(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error {
						t.Fatal("a changed backend must not reuse native curtailment admission")
						return nil
					}), "being disabled")
					require.NoError(t, attempt.Finish(t.Context(), nil))
				})
			}
		}
	}
}
