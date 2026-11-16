package mininggroups

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const pendingTransferPhase = "QUIESCING"

// The controller is simulated; the journal, intents and custody admission use
// real Postgres. Each request identity retains its original transfer receipt.
type transferFailureMiner struct {
	*retryMiner
	groups         []sdk.MiningGroup
	receipts       map[string]*sdk.MiningGroupOperation
	transferPhase  string
	loseReply      bool
	refuseMutation bool
	observeErr     bool
	pending        bool
}

func (m *transferFailureMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *transferFailureMiner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m.calls = append(m.calls, r)
	if m.refuseMutation && !r.ReadOnly() {
		return sdk.MiningGroupResult{}, devicewrite.NotDispatched(errors.New("mutation refused before dispatch"))
	}
	switch r.Action {
	case "observe":
		if m.observeErr {
			return sdk.MiningGroupResult{}, errors.New("observation unavailable")
		}
		return sdk.MiningGroupResult{Supported: true, Enabled: true, MaxGroups: 289, Groups: m.groups, Pending: m.pending}, nil
	case "transfer":
		op, ok := m.receipts[r.Key]
		if !ok {
			op = &sdk.MiningGroupOperation{ID: r.Key, Phase: m.transferPhase}
			m.receipts[r.Key] = op
			for i := range m.groups {
				if m.groups[i].ID == r.GroupID || m.groups[i].ID == r.SourceID {
					m.groups[i].Revision++
				}
				if op.Phase == transferPhaseComplete {
					if m.groups[i].ID == r.SourceID {
						m.groups[i].MACs = nil
					}
					if m.groups[i].ID == r.GroupID {
						m.groups[i].MACs = append(m.groups[i].MACs, r.MACs...)
					}
				}
			}
		}
		if m.loseReply {
			return sdk.MiningGroupResult{}, errors.New("lost transfer response")
		}
		return sdk.MiningGroupResult{Operation: op}, nil
	case "operation":
		return sdk.MiningGroupResult{Operation: m.receipts[r.GroupID]}, nil
	default:
		return sdk.MiningGroupResult{}, errors.New("unexpected mutation: " + r.Action)
	}
}

func newIdlePoolAssignmentFixture(t *testing.T) (context.Context, retryFixture, *transferFailureMiner, string, []int64) {
	t.Helper()
	ctx, f := newIdleRetryFixture(t)
	config, boards := preparePermissionPoolAssignment(t, ctx, f)
	pools, err := f.service.pools.HostGroupCredentials(ctx, f.service.queries, f.target.OrganizationID, config)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	target := sdk.MiningGroup{ID: "destination", Revision: 1, Mining: true, Pools: []sdk.MiningGroupPool{{URL: pools[0].URL, Username: pools[0].Username, Priority: 0, Enabled: true}}}
	m := &transferFailureMiner{retryMiner: f.miner, groups: []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{"02:00:00:00:00:01"}}, target}, receipts: map[string]*sdk.MiningGroupOperation{}, transferPhase: transferPhaseFailed}
	// Replace only controller I/O; retain the production service and database.
	f.service.miners = m
	row := f.state(t)
	row.Bindings, err = encode(map[string]binding{"destination": {ConfigID: config, Configured: true}})
	require.NoError(t, err)
	require.NoError(t, save(ctx, f.service.queries, row))
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	return ctx, f, m, config, boards
}

func newTransferFailureFixture(t *testing.T) (context.Context, retryFixture, *transferFailureMiner, string, []int64) {
	t.Helper()
	ctx, f, m, config, boards := newIdlePoolAssignmentFixture(t)
	_, err := f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
	require.NoError(t, err)
	return ctx, f, m, config, boards
}

func TestFailedTransferRefreshesAndRetriesWithNewIdentity(t *testing.T) {
	ctx, f, m, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx) // Save the first request.
	first := f.state(t)
	m.transferPhase = pendingTransferPhase
	f.reconcile(t, ctx) // Receive a pending operation ID.
	require.NotEmpty(t, f.state(t).ControllerOperationID)
	m.receipts[first.StepKey.UUID.String()].Phase = transferPhaseFailed
	f.reconcile(t, ctx) // Poll the terminal FAILED receipt.
	require.False(t, f.state(t).Step.Valid, "a terminal receipt must not be polled forever")
	require.Empty(t, f.state(t).ControllerOperationID)
	require.Equal(t, first.Bindings, f.state(t).Bindings, "completed configuration must survive")
	// Recovery must observe before preparing a new mutation, even after restart.
	f.service = NewService(f.conn, m, f.service.pools, f.service.secrets)
	m.observeErr = true
	f.reconcile(t, ctx)
	require.False(t, f.state(t).Step.Valid)
	_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.Error(t, err, "unobserved recovery must retain admission")
	m.observeErr = false
	f.reconcile(t, ctx)
	next := f.state(t)
	require.True(t, next.Step.Valid)
	require.NotEqual(t, first.StepKey, next.StepKey)
	m.transferPhase = transferPhaseComplete
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
	require.Len(t, m.receipts, 2)
	var requests []sdk.MiningGroupRequest
	for _, r := range m.calls {
		if r.Action == "transfer" {
			requests = append(requests, r)
		}
	}
	require.Len(t, requests, 2)
	require.Greater(t, requests[1].Revision, requests[0].Revision, "retry uses freshly observed revisions")
}

func TestFailedTransfersEventuallyReleaseWorkflow(t *testing.T) {
	ctx, f, m, config, boards := newTransferFailureFixture(t)
	original := f.state(t)
	// Three confirmed failures allow two automatic retries. Service recreation
	// ensures the budget is durable rather than a process-local counter.
	for range 3 {
		f.service = NewService(f.conn, m, f.service.pools, f.service.secrets)
		f.reconcile(t, ctx)
		require.True(t, f.state(t).Step.Valid)
		f.reconcile(t, ctx)
	}
	m.pending = true
	f.reconcile(t, ctx)
	require.True(t, active(f.state(t).Status), "pending controller work prevents terminal release")
	m.pending = false
	m.observeErr = true
	f.reconcile(t, ctx)
	require.True(t, active(f.state(t).Status), "an unavailable observation cannot release admission")
	m.observeErr = false
	f.reconcile(t, ctx)
	row := f.state(t)
	require.Equal(t, "failed", row.Status)
	require.False(t, row.Step.Valid)
	require.Equal(t, original.Bindings, row.Bindings)
	require.Len(t, m.receipts, 3)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents, "abandoned assignments must not reappear in a later operation")
	var attemptStatus string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, original.AttemptID.UUID).Scan(&attemptStatus))
	require.Equal(t, "complete", attemptStatus, "physical admission is settled even though the requested change failed")
	epoch, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err, "terminal failure must no longer block custody reservation")
	// Reservation made no controller changes; return the unchanged configuration.
	require.NoError(t, f.service.writes.ReleaseHost(ctx, f.target.OrganizationID, f.target.DeviceID, epoch))
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, row.Revision)
	require.NoError(t, err)
	f.reconcile(t, ctx)
	require.True(t, f.state(t).Step.Valid, "new user requests get a fresh failure budget")
}

func TestUnresolvedTransferNeverConsumesFailureBudget(t *testing.T) {
	for _, phase := range []string{pendingTransferPhase, "lost reply"} {
		t.Run(phase, func(t *testing.T) {
			ctx, f, m, _, _ := newTransferFailureFixture(t)
			m.transferPhase = pendingTransferPhase
			m.loseReply = phase == "lost reply"
			f.reconcile(t, ctx)
			original := f.state(t)
			for range 5 {
				f.reconcile(t, ctx)
			}
			row := f.state(t)
			require.True(t, active(row.Status))
			require.Equal(t, original.Step, row.Step)
			require.Equal(t, original.StepKey, row.StepKey)
			require.Len(t, m.receipts, 1)
			_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err)
		})
	}
}

func TestFailedTransferPreservesAppliedAssignments(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	// Another board is already at the intended destination. Its completed intent
	// must survive even though this workflow cannot move the remaining board.
	var appliedBoard int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&appliedBoard))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT $1,module_id,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}' FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, appliedBoard, boards[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at)
 SELECT module_id,$1,2,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, appliedBoard)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id)
 SELECT p.organization_id,$1,p.device_id,i.id,'02:00:00:00:00:02',p.account_id,p.pool_revision_id,$3 FROM hashboard_pool_intent p JOIN hashboard_installation i ON i.hashboard_id=$1 AND i.removed_at IS NULL WHERE p.hashboard_id=$2`, appliedBoard, boards[0], uuid.New())
	require.NoError(t, err)
	m.groups[1].MACs = []string{"02:00:00:00:00:02"}
	for range 7 {
		f.reconcile(t, ctx)
	}
	require.Equal(t, "failed", f.state(t).Status)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, appliedBoard, intents[0].HashboardID)
	require.Equal(t, []string{"02:00:00:00:00:02"}, m.groups[1].MACs)
	require.Len(t, m.groups, 2, "completed group setup is retained")
}

func TestFailedTransferSettlementIsAtomic(t *testing.T) {
	ctx, f, m, _, _ := newTransferFailureFixture(t)
	for range 6 {
		f.reconcile(t, ctx)
	}
	original := f.state(t)
	// A journal-write failure must roll back both intent cleanup and admission
	// release. Otherwise a custody change could race an apparently active workflow.
	_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_failed_journal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='failed' THEN RAISE EXCEPTION 'journal unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_failed_journal BEFORE UPDATE ON container_mining_group_state FOR EACH ROW EXECUTE FUNCTION reject_failed_journal()`)
	require.NoError(t, err)
	err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
	require.ErrorContains(t, err, "journal unavailable")
	require.Equal(t, original.Status, f.state(t).Status)
	var attemptStatus string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, original.AttemptID.UUID).Scan(&attemptStatus))
	require.Equal(t, "running", attemptStatus)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Len(t, m.receipts, 3)
}
