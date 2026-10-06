package rollout

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestChannelMutationsWaitForOwnershipAdmission(t *testing.T) {
	for _, operation := range []string{"update", "apply", "clear", "rollback", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t, 2)
			f.channel(t, allAtOnce, "miner-0")
			var latest Rollout
			if operation == "clear" || operation == "rollback" {
				latest = f.apply(t, "fw-1")
			}
			if operation == "rollback" {
				latest = f.apply(t, "fw-2")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			// Manual firmware admission holds this transaction-scoped lock
			// from its ownership check through enqueue. Channel ownership must
			// stay unchanged for that whole interval.
			admission, err := f.conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = admission.Rollback() }()
			require.NoError(t, sqlc.New(admission).LockReleaseChannelScopes(ctx, f.orgID))
			var admissionPID int
			require.NoError(t, admission.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&admissionPID))
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "update":
					_, err = f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
						Name: "Updated", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}},
					})
				case "apply":
					_, err = f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
				case "clear":
					_, err = f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{rigAssignment("")}, nil)
				case "rollback":
					_, _, err = f.svc.RollbackFirmware(ctx, f.orgID, latest.ID, byOperator)
				case "delete":
					err = f.svc.DeleteChannel(ctx, f.orgID, f.channelID)
				}
				done <- err
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.conn.QueryRowContext(ctx, `SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
				)`, admissionPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "ownership mutation must wait for admission to commit")

			// Scope admission must precede the channel row lock. Otherwise an
			// existing scope writer could deadlock while updating this row.
			var channelID int64
			require.NoError(t, admission.QueryRowContext(ctx, `SELECT id FROM release_channel WHERE id = $1 FOR UPDATE NOWAIT`, f.channelID).Scan(&channelID))
			require.NoError(t, admission.Commit())
			require.NoError(t, <-done)
			switch operation {
			case "update":
				channel, err := f.svc.GetChannel(ctx, f.orgID, f.channelID)
				require.NoError(t, err)
				require.Equal(t, []string{"miner-1"}, channel.Scope.DeviceIdentifiers)
			case "apply":
				require.Equal(t, "fw-2", f.assignedFirmware(t))
			case "clear":
				require.Empty(t, f.assignedFirmware(t))
			case "rollback":
				require.Equal(t, "fw-1", f.assignedFirmware(t))
			case "delete":
				_, err := f.svc.GetChannel(ctx, f.orgID, f.channelID)
				require.ErrorContains(t, err, "release channel not found")
			}
		})
	}
}
