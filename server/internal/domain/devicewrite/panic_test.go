package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDirectAttemptPanicRetainsPhysicalEvidence(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "host operation"
		if deleted {
			name = "deleted miner unpair"
		}
		t.Run(name, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			service := devicewrite.NewService(database.DB)
			q := sqlc.New(database.DB)
			require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), device.DatabaseID)
			require.NoError(t, err)
			if deleted {
				_, err = database.DB.ExecContext(t.Context(), "UPDATE device_write_fence SET epoch=1 WHERE device_id=$1", device.DatabaseID)
				require.NoError(t, err)
				_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", device.DatabaseID)
				require.NoError(t, err)
				require.NoError(t, q.CreateDeletionUnpairPermit(t.Context(), sqlc.CreateDeletionUnpairPermitParams{
					DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID,
					Epoch: 1, BindingHash: "panic-fixture", IdentityKeys: keys,
				}))
			}
			dispatched := false
			operation := func(context.Context) error {
				dispatched = true
				panic("transport panic")
			}
			require.PanicsWithValue(t, "transport panic", func() {
				if deleted {
					_ = service.RunDeletionUnpair(t.Context(), user.OrganizationID, device.DatabaseID, "panic-fixture", func(ctx context.Context) error {
						handled, err := service.DispatchDeletedUnpair(ctx, user.OrganizationID, device.DatabaseID, "panic-fixture", operation)
						require.True(t, handled)
						return err
					})
				} else {
					_ = service.RunHost(t.Context(), user.OrganizationID, device.DatabaseID, 0, operation)
				}
			})
			require.True(t, dispatched)
			var status string
			var unfinished bool
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,finished_at IS NULL FROM device_write_attempt WHERE device_id=$1", device.DatabaseID).Scan(&status, &unfinished))
			require.Equal(t, "running", status)
			require.True(t, unfinished)
			if deleted {
				drained, err := q.IsRetiredDeviceWriteDrained(t.Context(), keys)
				require.NoError(t, err)
				require.False(t, drained, "a recovered unpair panic must still block physical identity reuse")
				var claimed bool
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", device.DatabaseID).Scan(&claimed))
				require.True(t, claimed, "the single-use permit remains consumed")
			} else {
				epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
				require.NoError(t, err)
				drained, err := service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
				require.NoError(t, err)
				require.False(t, drained, "a recovered transport panic must still block ownership drain")
			}
		})
	}
}
