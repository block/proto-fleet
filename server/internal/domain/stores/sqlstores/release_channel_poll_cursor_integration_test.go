package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_PollCursorIncludesLateCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	late := f.rollout(f.channel("late commit"), "Bitmain", "S19")
	visible := f.rollout(f.channel("visible commit"), "Bitmain", "S19")
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	// A savepoint must still record the top-level transaction ID: snapshots
	// describe top-level transactions, not subtransaction allocation order.
	_, err = tx.ExecContext(t.Context(), `SAVEPOINT nested_change`)
	require.NoError(t, err)
	q := sqlc.New(tx)
	require.NoError(t, q.RecordFirmwareRolloutAction(t.Context(), sqlc.RecordFirmwareRolloutActionParams{
		RolloutID: late, ActorType: "system", ActorName: "late change",
	}))
	lateRow, err := q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{OrgID: f.org, RolloutID: late})
	require.NoError(t, err)
	var topLevelTxid int64
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT pg_current_xact_id()::text::bigint`).Scan(&topLevelTxid))
	require.Equal(t, topLevelTxid, lateRow.RevisionTxid)
	_, err = tx.ExecContext(t.Context(), `RELEASE SAVEPOINT nested_change`)
	require.NoError(t, err)

	// A different rollout commits after the late writer's timestamp but
	// before the poll. A maximum updated_at watermark would skip that writer.
	require.NoError(t, f.q.RecordFirmwareRolloutAction(t.Context(), sqlc.RecordFirmwareRolloutActionParams{
		RolloutID: visible, ActorType: "system", ActorName: "visible change",
	}))
	visibleRow, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{OrgID: f.org, RolloutID: visible})
	require.NoError(t, err)
	require.True(t, lateRow.UpdatedAt.Before(visibleRow.UpdatedAt))
	watermark, err := f.q.GetFirmwareRolloutPollWatermark(t.Context())
	require.NoError(t, err)
	require.LessOrEqual(t, watermark, topLevelTxid)
	first, err := f.q.ListFirmwareRollouts(t.Context(), sqlc.ListFirmwareRolloutsParams{OrgID: f.org, PageLimit: 100})
	require.NoError(t, err)
	for _, row := range first {
		require.NotEqual(t, "late change", row.FirmwareRollout.LastActionByName)
	}
	require.NoError(t, tx.Commit())

	dated, err := f.q.ListFirmwareRollouts(t.Context(), sqlc.ListFirmwareRolloutsParams{
		OrgID: f.org, PageLimit: 100, UpdatedAfter: sql.NullTime{Time: visibleRow.UpdatedAt, Valid: true},
	})
	require.NoError(t, err)
	for _, row := range dated {
		require.NotEqual(t, late, row.FirmwareRollout.ID, "date filtering cannot detect this late commit")
	}
	changes, err := f.q.ListFirmwareRollouts(t.Context(), sqlc.ListFirmwareRolloutsParams{
		OrgID: f.org, PageLimit: 100, AfterRevisionTxid: sql.NullInt64{Int64: watermark, Valid: true},
	})
	require.NoError(t, err)
	byID := map[int64]sqlc.FirmwareRollout{}
	for _, row := range changes {
		byID[row.FirmwareRollout.ID] = row.FirmwareRollout
	}
	require.Equal(t, "late change", byID[late].LastActionByName)
	require.Equal(t, lateRow.Revision, byID[late].Revision)
	// The cutoff is inclusive, and old revisions are actually filtered.
	changes, err = f.q.ListFirmwareRollouts(t.Context(), sqlc.ListFirmwareRolloutsParams{
		OrgID: f.org, PageLimit: 100, AfterRevisionTxid: sql.NullInt64{Int64: visibleRow.RevisionTxid, Valid: true},
	})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, visible, changes[0].FirmwareRollout.ID)
}
