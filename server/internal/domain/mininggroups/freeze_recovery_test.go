package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations/migration"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestFreezeKeepsLegacyRecoveryAvailableUntilAllGapsResolved(t *testing.T) {
	ctx, f, _, _, _ := newTransferFailureFixture(t)
	freeze := func() error {
		return db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
			return migration.FreezeCutover(ctx, q, f.target.OrganizationID)
		})
	}
	before := f.state(t)
	require.ErrorContains(t, freeze(), "unresolved", "freezing this workflow would prevent its audited cleanup")
	frozen, err := f.service.queries.LegacyPoolWritersDisabled(ctx, f.target.OrganizationID)
	require.NoError(t, err)
	require.False(t, frozen, "a rejected freeze must leave recovery available")
	require.Equal(t, before, f.state(t))
	_, err = f.service.writes.ResolveOffline(ctx, recoveryEvidence(t, ctx, f), true)
	require.NoError(t, err)
	require.Equal(t, "failed", f.state(t).Status)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents, "recovered legacy intent must not be adopted by the later import")
	var recoveries int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_recovery WHERE attempt_id=$1`, before.AttemptID.UUID).Scan(&recoveries))
	require.Equal(t, 1, recoveries)
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		_, err := migration.Import(ctx, q, f.service.secrets, f.target.OrganizationID)
		return err
	}))
	require.ErrorContains(t, freeze(), "unmanaged_device", "recovery removed failed intent; importing bindings must not fabricate a replacement assignment")
	frozen, err = f.service.queries.LegacyPoolWritersDisabled(ctx, f.target.OrganizationID)
	require.NoError(t, err)
	require.False(t, frozen, "a replacement assignment must remain possible after recovery")
}

func TestFreezeRejectsPendingCanonicalAssignmentUntilItFinishes(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
	freeze := func() error {
		return db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
			return migration.FreezeCutover(ctx, q, f.target.OrganizationID)
		})
	}
	calls := len(firmware.calls)
	require.ErrorContains(t, freeze(), "pending_configuration")
	require.Len(t, firmware.calls, calls, "inspection never dispatches the pending update")
	frozen, err := f.service.queries.LegacyPoolWritersDisabled(ctx, f.target.OrganizationID)
	require.NoError(t, err)
	require.False(t, frozen)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
	for iteration := 0; iteration < 30 && active(f.state(t).Status); iteration++ {
		f.reconcile(t, ctx)
	}
	require.Equal(t, "complete", f.state(t).Status)
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		_, err := migration.Import(ctx, q, f.service.secrets, f.target.OrganizationID)
		return err
	}))
	require.NoError(t, freeze(), "completed assignments and imported libraries can proceed to maintenance")
}
