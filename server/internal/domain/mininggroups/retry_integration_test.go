package mininggroups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type retryMiner struct {
	interfaces.Miner
	enabled        bool
	fail           bool
	wait           bool
	calls          []sdk.MiningGroupRequest
	activationErr  error
	mining         bool
	activeBoards   int32
	holdStop       bool
	loseStop       bool
	loseActivate   bool
	loseDeactivate bool
	deactivatedKey string
	loseStart      bool
	statusErr      bool
	startConflict  bool
	revision       int64
}

func (m *retryMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *retryMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m.calls = append(m.calls, request)
	if m.wait {
		<-ctx.Done()
		return sdk.MiningGroupResult{}, fmt.Errorf("controller call canceled: %w", ctx.Err())
	}
	if m.fail {
		return sdk.MiningGroupResult{}, errors.New("controller unavailable")
	}
	switch request.Action {
	case operationDeactivate:
		m.enabled = false
		m.deactivatedKey = request.Key
		if m.loseDeactivate {
			m.loseDeactivate = false
			return sdk.MiningGroupResult{}, errors.New("lost deactivation reply")
		}
		return sdk.MiningGroupResult{Deactivated: true}, nil
	case "mining_status":
		if m.statusErr {
			return sdk.MiningGroupResult{}, errors.New("status unavailable")
		}
		return sdk.MiningGroupResult{MiningStatus: &sdk.MiningGroupMiningStatus{Mining: m.mining, ActiveBoards: m.activeBoards}}, nil
	case "stop_legacy":
		if !m.holdStop {
			m.mining = false
			m.activeBoards = 0
		}
		if m.loseStop {
			m.loseStop = false
			return sdk.MiningGroupResult{}, errors.New("lost stop reply")
		}
		return sdk.MiningGroupResult{StopRequested: true}, nil
	case operationActivate:
		if m.activationErr != nil {
			return sdk.MiningGroupResult{}, m.activationErr
		}
		m.enabled = true
		if m.loseActivate {
			m.loseActivate = false
			return sdk.MiningGroupResult{}, errors.New("lost activation reply")
		}
		return sdk.MiningGroupResult{Enabled: true}, nil
	case "start":
		if m.startConflict {
			m.startConflict = false
			m.revision++
			return sdk.MiningGroupResult{RevisionConflict: true}, nil
		}
		m.mining = true
		if m.loseStart {
			m.loseStart = false
			return sdk.MiningGroupResult{}, errors.New("lost start reply")
		}
		return sdk.MiningGroupResult{Group: &sdk.MiningGroup{ID: "default", Revision: m.revision, Mining: true}}, nil
	}
	out := sdk.MiningGroupResult{Supported: true, Enabled: m.enabled, Revision: 1, MaxGroups: 289}
	if m.enabled {
		out.Groups = []sdk.MiningGroup{{ID: "default", Revision: m.revision, Mining: m.mining}}
	}
	return out, nil
}

type retryFixture struct {
	service *Service
	miner   *retryMiner
	conn    *sql.DB
	target  sqlc.ListMiningGroupControllersRow
}

func newIdleRetryFixture(t *testing.T) (context.Context, retryFixture) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	controller := database.CreateDevice(user.OrganizationID, "proto")
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: user.DatabaseID, OrganizationID: user.OrganizationID, AuthMethod: session.AuthMethodSession})
	_, err = database.DB.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, controller.DatabaseID)
	require.NoError(t, err)
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: cfg.ServiceMasterKey})
	require.NoError(t, err)
	pools := hostedaccounts.NewPoolService(hostedaccounts.NewService(database.DB, nil), secrets, nil, nil)
	miner := &retryMiner{revision: 1}
	service := NewService(database.DB, miner, pools, secrets)
	_, err = service.GetState(ctx, controller.ID)
	require.NoError(t, err)
	return ctx, retryFixture{service: service, miner: miner, conn: database.DB, target: sqlc.ListMiningGroupControllersRow{OrganizationID: user.OrganizationID, DeviceID: controller.DatabaseID, DeviceIdentifier: controller.ID}}
}

func newRetryFixture(t *testing.T) (context.Context, retryFixture) {
	t.Helper()
	ctx, f := newIdleRetryFixture(t)
	_, err := f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), "", nil, 0)
	require.NoError(t, err)
	rows, err := f.service.queries.ListMiningGroupControllers(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	f.target = rows[0]
	return ctx, f
}

func (f retryFixture) state(t *testing.T) sqlc.ContainerMiningGroupState {
	t.Helper()
	row, err := f.service.queries.GetMiningGroupState(t.Context(), sqlc.GetMiningGroupStateParams{DeviceID: f.target.DeviceID, OrganizationID: f.target.OrganizationID})
	require.NoError(t, err)
	return row
}

func (f retryFixture) makeDue(t *testing.T) sqlc.ListMiningGroupControllersRow {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET next_reconcile_at=clock_timestamp()-INTERVAL '1 second' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	rows, err := f.service.queries.ListMiningGroupControllers(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}

func TestScheduledReconciliationPreservesRequestsAndRecovers(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.prepareActivation(t, ctx)
	prepared := f.state(t)
	require.True(t, prepared.Step.Valid)
	f.miner.fail = true
	for count := int32(1); count <= 3; count++ {
		// A new service exercises persistence across active-server failover.
		f.service = NewService(f.conn, f.miner, f.service.pools, f.service.secrets)
		require.NoError(t, f.service.reconcileScheduled(ctx, f.makeDue(t)))
		blocked := f.state(t)
		require.Equal(t, "blocked", blocked.Status)
		require.Equal(t, count, blocked.RetryCount)
		require.True(t, blocked.NextReconcileAt.After(time.Now()))
		require.Equal(t, prepared.Step, blocked.Step)
		require.Equal(t, prepared.StepKey, blocked.StepKey)
		require.Equal(t, prepared.AttemptID, blocked.AttemptID)
		rows, err := f.service.queries.ListMiningGroupControllers(ctx)
		require.NoError(t, err)
		require.Empty(t, rows, "blocked work is ineligible until its retry time")
		f.miner.fail = false
		_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
		require.NoError(t, err)
		require.Equal(t, blocked.RetryCount, f.state(t).RetryCount)
		require.Equal(t, blocked.NextReconcileAt, f.state(t).NextReconcileAt, "UI observations must not reset backoff")
		f.miner.fail = true
	}
	f.miner.fail = false
	require.NoError(t, f.service.reconcileScheduled(ctx, f.makeDue(t)))
	progress := f.state(t)
	require.Equal(t, statusApplying, progress.Status)
	require.Zero(t, progress.RetryCount)
	require.WithinDuration(t, time.Now(), progress.NextReconcileAt, time.Second, "healthy progress is eligible on the next normal worker tick")
	require.NoError(t, f.service.reconcileScheduled(ctx, f.makeDue(t)))
	require.Equal(t, statusComplete, f.state(t).Status)
	var attemptStatus string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, prepared.AttemptID.UUID).Scan(&attemptStatus))
	require.Equal(t, statusComplete, attemptStatus)
	for _, call := range f.miner.calls {
		if call.Action == operationActivate {
			require.Equal(t, prepared.StepKey.UUID.String(), call.Key, "every replay must use the committed request identity")
		}
	}
	_, err := f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), "", nil, 0)
	require.NoError(t, err)
	fresh := f.state(t)
	require.Zero(t, fresh.RetryCount)
	// Delayed scheduling for the old operation cannot postpone a new one.
	require.NoError(t, f.service.queries.ScheduleMiningGroupReconciliation(ctx, sqlc.ScheduleMiningGroupReconciliationParams{DeviceID: f.target.DeviceID, OrganizationID: f.target.OrganizationID, AttemptID: prepared.AttemptID, Failed: true, FailureDelayMs: time.Minute.Milliseconds()}))
	require.Equal(t, fresh.NextReconcileAt, f.state(t).NextReconcileAt)
	rows, err := f.service.queries.ListMiningGroupControllers(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "a fresh operation must be eligible immediately")
}

func TestScheduledReconciliationBacksOffErrorsBeforeBlock(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.prepareActivation(t, ctx)
	_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET step='{}' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	require.Error(t, f.service.reconcileScheduled(ctx, f.makeDue(t)))
	row := f.state(t)
	require.Equal(t, statusApplying, row.Status, "failed transaction did not write blocked status")
	require.EqualValues(t, 1, row.RetryCount)
	require.True(t, row.NextReconcileAt.After(time.Now()))
	require.JSONEq(t, `{}`, string(row.Step.RawMessage), "scheduling must not discard unresolved work")
}

func TestScheduledReconciliationBacksOffTimeout(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.miner.wait = true
	err := f.service.reconcileScheduledWithTimeout(ctx, f.target, time.Second)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotEmpty(t, f.miner.calls, "the deadline must expire during controller I/O")
	require.NoError(t, ctx.Err(), "the active lifetime must outlive the timed-out attempt")
	row := f.state(t)
	require.EqualValues(t, 1, row.RetryCount, "an expired attempt must still persist backoff using the active lifetime")
	require.True(t, row.NextReconcileAt.After(time.Now()))
}

// Capture initial state, request stop, and journal activation in separate commits.
func (f retryFixture) prepareActivation(t *testing.T, ctx context.Context) {
	t.Helper()
	for range 3 {
		f.reconcile(t, ctx)
	}
	require.True(t, f.state(t).Step.Valid)
}
func (f retryFixture) reconcile(t *testing.T, ctx context.Context) {
	t.Helper()
	require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
}
