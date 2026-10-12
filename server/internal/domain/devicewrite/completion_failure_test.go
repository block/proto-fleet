package devicewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDirectCompletionFailurePreservesPhysicalOutcome(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "host operation"
		if deleted {
			name = "deleted miner unpair"
		}
		t.Run(name, func(t *testing.T) {
			for _, transportFails := range []bool{false, true} {
				name := "accepted"
				if transportFails {
					name = "transport failed"
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
							Epoch: 1, BindingHash: "completion-failure", IdentityKeys: keys,
						}))
					}
					// A real PostgreSQL failure rolls back Finish while leaving the
					// separately committed admission intact.
					_, err = database.DB.ExecContext(t.Context(), `CREATE FUNCTION reject_attempt_completion() RETURNS trigger
						LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'completion unavailable'; END; $$;
						CREATE TRIGGER reject_attempt_completion BEFORE UPDATE ON device_write_attempt
						FOR EACH ROW EXECUTE FUNCTION reject_attempt_completion()`)
					require.NoError(t, err)
					var physicalOutcome error
					if transportFails {
						physicalOutcome = errors.New("transport outcome unavailable")
					}
					calls := 0
					operation := func(context.Context) error { calls++; return physicalOutcome }
					if deleted {
						err = service.RunDeletionUnpair(t.Context(), user.OrganizationID, device.DatabaseID, "completion-failure", func(ctx context.Context) error {
							handled, err := service.DispatchDeletedUnpair(ctx, user.OrganizationID, device.DatabaseID, "completion-failure", operation)
							require.True(t, handled)
							return err
						})
					} else {
						err = service.RunHost(t.Context(), user.OrganizationID, device.DatabaseID, 0, operation)
					}
					require.Equal(t, physicalOutcome, err, "completion failure must preserve the adapter result")
					require.Equal(t, 1, calls)
					var status string
					var unfinished bool
					require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,finished_at IS NULL FROM device_write_attempt WHERE device_id=$1", device.DatabaseID).Scan(&status, &unfinished))
					require.Equal(t, "running", status)
					require.True(t, unfinished)
					if deleted {
						drained, err := q.IsRetiredDeviceWriteDrained(t.Context(), keys)
						require.NoError(t, err)
						require.False(t, drained, "completion failure cannot release retired identity reuse")
					} else {
						epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
						require.NoError(t, err)
						drained, err := service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
						require.NoError(t, err)
						require.False(t, drained, "completion failure cannot release ownership drain")
					}
				})
			}
		})
	}
}
