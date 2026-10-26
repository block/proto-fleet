package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const historicalBoardMAC = "02:00:00:00:00:02"

func addHistoricalPoolBoard(t *testing.T, ctx context.Context, f retryFixture, selected int64) int64 {
	t.Helper()
	var board int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&board))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT $1,module_id,2,now(),now(),jsonb_build_object('macAddress',$3::text) FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, board, selected, historicalBoardMAC)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at)
 SELECT module_id,$1,2,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id)
 SELECT p.organization_id,$1,p.device_id,i.id,$3,p.account_id,p.pool_revision_id,$4 FROM hashboard_pool_intent p JOIN hashboard_installation i ON i.hashboard_id=$1 AND i.removed_at IS NULL WHERE p.hashboard_id=$2`, board, selected, historicalBoardMAC, uuid.New())
	require.NoError(t, err)
	return board
}

func stalePoolBoard(t *testing.T, ctx context.Context, f retryFixture, board int64) {
	t.Helper()
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-INTERVAL '5 minutes' WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
}

func TestHistoricalUnavailableBoardDoesNotBlockUnrelatedAssignment(t *testing.T) {
	for _, reason := range []string{"stale", "unavailable"} {
		t.Run(reason, func(t *testing.T) {
			ctx, f, m, _, boards := newTransferFailureFixture(t)
			old := addHistoricalPoolBoard(t, ctx, f, boards[0])
			m.groups[1].MACs = []string{historicalBoardMAC}
			before, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			if reason == "stale" {
				stalePoolBoard(t, ctx, f, old)
			} else {
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET unavailable_reason='board fault' WHERE hashboard_id=$1`, old)
				require.NoError(t, err)
			}
			m.transferPhase = transferPhaseComplete
			m.calls = nil
			for range 3 {
				f.reconcile(t, ctx)
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			for _, call := range m.calls {
				if !call.ReadOnly() {
					require.Equal(t, "transfer", call.Action)
					require.Equal(t, []string{"02:00:00:00:00:01"}, call.MACs)
				}
			}
			after, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Equal(t, before, after, "stale historical intent must be preserved")
			require.ElementsMatch(t, []string{historicalBoardMAC, "02:00:00:00:00:01"}, m.groups[1].MACs)
		})
	}
}

func TestHistoricalBoardMustBeAvailableForAffectedStep(t *testing.T) {
	for _, action := range []string{"replace_pools", "start", "transfer"} {
		for _, stage := range []string{"planning", "dispatch"} {
			t.Run(action+"/"+stage, func(t *testing.T) {
				ctx, f, m, _, boards := newTransferFailureFixture(t)
				old := addHistoricalPoolBoard(t, ctx, f, boards[0])
				m.groups[1].MACs = []string{historicalBoardMAC}
				switch action {
				case "replace_pools":
					m.groups[1].Pools = nil
				case "start":
					m.groups[1].Mining = false
				case "transfer":
					m.groups[1].MACs = nil
					m.groups[0].MACs = append(m.groups[0].MACs, historicalBoardMAC)
				}
				if stage == "dispatch" {
					f.reconcile(t, ctx)
					require.True(t, f.state(t).Step.Valid)
				}
				before := f.state(t)
				stalePoolBoard(t, ctx, f, old)
				m.calls = nil
				f.reconcile(t, ctx)
				require.Equal(t, "blocked", f.state(t).Status)
				require.Equal(t, before.Step, f.state(t).Step)
				require.Equal(t, before.StepKey, f.state(t).StepKey)
				for _, call := range m.calls {
					require.True(t, call.ReadOnly(), "stale boards must not receive new writes")
				}
				_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
				require.Error(t, err, "blocked work must retain admission")
			})
		}
	}
}

func TestHistoricalReplacementDoesNotInheritIntent(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	old := addHistoricalPoolBoard(t, ctx, f, boards[0])
	m.groups[1].MACs = []string{historicalBoardMAC}
	detachPoolBoard(t, ctx, f, old)
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT hashboard_id,module_id,position,now(),now(),metadata FROM hashboard_installation WHERE hashboard_id=$1 ORDER BY id DESC LIMIT 1`, old)
	require.NoError(t, err)
	m.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents)
	for _, call := range m.calls {
		require.True(t, call.ReadOnly())
	}
}

func TestHistoricalStepChecksSurviveObservationRefreshAndRestart(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	old := addHistoricalPoolBoard(t, ctx, f, boards[0])
	m.groups[1].MACs = []string{historicalBoardMAC}
	m.groups[1].Mining = false
	f.reconcile(t, ctx) // Save a group start that affects the historical board.
	before := f.state(t)
	require.True(t, before.Step.Valid)
	stalePoolBoard(t, ctx, f, old)
	// A UI refresh can replace the observation while the saved step remains.
	m.groups[1].MACs = nil
	m.groups[1].Revision++
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.NotEqual(t, before.Observed, f.state(t).Observed)
	f.service = NewService(f.conn, m, f.service.pools, f.service.secrets)
	m.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	require.Equal(t, before.Step, f.state(t).Step)
	require.Equal(t, before.StepKey, f.state(t).StepKey)
	require.Empty(t, m.calls, "the saved affected membership must still gate dispatch")
}

func TestHistoricalUnknownTransferWaitsForFreshnessWithoutLosingIdentity(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	old := addHistoricalPoolBoard(t, ctx, f, boards[0])
	m.groups[0].MACs = append(m.groups[0].MACs, historicalBoardMAC)
	m.transferPhase = pendingTransferPhase
	m.loseReply = true
	f.reconcile(t, ctx)
	f.reconcile(t, ctx) // Accepted by controller, but the response is lost.
	before := f.state(t)
	require.True(t, before.Step.Valid)
	require.Empty(t, before.ControllerOperationID)
	stalePoolBoard(t, ctx, f, old)
	m.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	require.Equal(t, before.Step, f.state(t).Step)
	require.Equal(t, before.StepKey, f.state(t).StepKey)
	require.Empty(t, m.calls)
	_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.Error(t, err)
	// Recovery replays the original identity, never a replacement transfer.
	_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now() WHERE hashboard_id=$1`, old)
	require.NoError(t, err)
	m.loseReply = false
	f.reconcile(t, ctx)
	require.Len(t, m.calls, 1)
	require.Equal(t, before.StepKey.UUID.String(), m.calls[0].Key)
	require.Equal(t, before.Step, f.state(t).Step)
	require.Len(t, m.receipts, 1)
	// Once the receipt is known, read-only polling does not require freshness.
	stalePoolBoard(t, ctx, f, old)
	m.calls = nil
	f.reconcile(t, ctx)
	require.Len(t, m.calls, 1)
	require.Equal(t, "operation", m.calls[0].Action)
}
