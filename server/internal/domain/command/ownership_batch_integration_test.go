package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func TestMixedOwnershipBatchRejectsAtomically(t *testing.T) {
	for _, ambient := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "caller transaction"}[ambient], func(t *testing.T) {
			svc, conn, identifier, finalizers := newTransactionalCommandFixture(t)
			ctx := manualSessionCtx(1)
			q := sqlc.New(conn)
			blockedIdentifier := "blocked-miner"
			discovery, err := q.UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{OrgID: 1, DeviceIdentifier: "discovered-blocked", DriverName: "proto", IpAddress: "192.0.2.11", Port: "443", UrlScheme: "https", IsActive: true})
			require.NoError(t, err)
			blockedID, err := q.InsertDevice(ctx, sqlc.InsertDeviceParams{OrgID: 1, DiscoveredDeviceID: discovery, DeviceIdentifier: blockedIdentifier, MacAddress: "00:11:22:33:44:56"})
			require.NoError(t, err)
			_, err = devicewrite.NewService(conn).Reserve(ctx, 1, blockedID)
			require.NoError(t, err)
			dispatch := func(callCtx context.Context) error {
				result, err := svc.Reboot(callCtx, includeSelector(identifier, blockedIdentifier))
				require.Nil(t, result, "rejection must not report accepted counts or a successful batch")
				return err
			}
			if ambient {
				err = sqlstores.NewSQLTransactor(conn).RunInTxNoRetry(ctx, dispatch)
			} else {
				err = dispatch(ctx)
			}
			require.True(t, fleeterror.IsFailedPreconditionError(err), "ownership rejection must remain an actionable caller error: %v", err)
			require.ErrorContains(t, err, "no selected commands were enqueued")
			var messages, results, pending int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM queue_message), (SELECT count(*) FROM command_on_device_log), (SELECT count(*) FROM command_batch_log WHERE status='PENDING')`).Scan(&messages, &results, &pending))
			require.Zero(t, messages)
			require.Zero(t, results)
			require.Zero(t, pending)
			require.Zero(t, *finalizers)
			if ambient {
				assertCommandDispatchRows(t, conn, 0)
			}
		})
	}
}
