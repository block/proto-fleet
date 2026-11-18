package mininggroups

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type simulatedGroups struct {
	interfaces.Miner
	snapshot                sdk.MiningGroupResult
	receipts                map[string]sdk.MiningGroupResult
	calls                   []sdk.MiningGroupRequest
	lostCreate              bool
	loseReplaceAck          bool
	transfer                *sdk.MiningGroupRequest
	created                 int
	runtimePoolsRemain      bool
	runtimeDiagnosticsError error
}

func (m *simulatedGroups) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m *simulatedGroups) MiningGroups(_ context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m.calls = append(m.calls, r)
	if r.Action == "observe" {
		m.snapshot.ObservedAt = time.Now()
		return m.snapshot, nil
	}
	if prior, ok := m.receipts[r.Key]; ok && r.Key != "" {
		return prior, nil
	}
	result := sdk.MiningGroupResult{}
	switch r.Action {
	case sdk.MiningGroupReadDefault:
		if group, found := defaultGroup(m.snapshot); found {
			if group.Pools == nil {
				group.Pools = []sdk.MiningGroupPool{}
			}
			if len(r.DefaultURLSigningKey) != 0 {
				group.DefaultURLFingerprint, _ = digest(group.Pools)
			}
			result.Group = &group
		}
	case "create":
		id := "fleet-group"
		if m.created > 0 {
			id = fmt.Sprintf("fleet-group-%d", m.created+1)
		}
		m.created++
		g := sdk.MiningGroup{ID: id, Name: "Fleet group", Revision: 1}
		m.snapshot.Groups = append(m.snapshot.Groups, g)
		result.Group = &g
	case "replace_pools", "clear_pools":
		for i := range m.snapshot.Groups {
			g := &m.snapshot.Groups[i]
			if g.ID == r.GroupID {
				g.Revision++
				g.Pools = nil
				for j, p := range r.Pools {
					g.Pools = append(g.Pools, sdk.MiningGroupPool{ID: []int32{1, 2, 3}[j], URL: p.URL, Username: p.Username, Priority: []int32{0, 1, 2}[j], Enabled: true})
				}
				receipt := *g
				result.Group = &receipt
			}
		}
	case "start", "stop":
		for i := range m.snapshot.Groups {
			g := &m.snapshot.Groups[i]
			if g.ID == r.GroupID {
				g.Revision++
				g.Mining = r.Action == "start"
				receipt := *g
				result.Group = &receipt
			}
		}
	case "remove":
		m.snapshot.Groups = slices.DeleteFunc(m.snapshot.Groups, func(g sdk.MiningGroup) bool { return g.ID == r.GroupID })
		result.Removed = true
	case "verify_cleared":
		if m.runtimeDiagnosticsError != nil {
			return result, m.runtimeDiagnosticsError
		}
		cleared := false
		for _, g := range m.snapshot.Groups {
			if g.ID == r.GroupID && !g.Mining && len(g.Pools) == 0 && !m.runtimePoolsRemain {
				cleared = true
				for _, mac := range r.MACs {
					cleared = cleared && slices.Contains(g.MACs, mac)
				}
			}
		}
		result.PoolsCleared = &cleared
	case "transfer":
		m.transfer = &r
		m.snapshot.Pending = true
		result.Operation = &sdk.MiningGroupOperation{ID: "transfer-1", Phase: "QUIESCING", BlockedMACs: r.MACs}
	case "operation":
		if m.transfer != nil {
			r := m.transfer
			for i := range m.snapshot.Groups {
				g := &m.snapshot.Groups[i]
				if g.ID == r.SourceID {
					g.MACs = slices.DeleteFunc(g.MACs, func(mac string) bool { return slices.Contains(r.MACs, mac) })
					g.Revision++
				}
				if g.ID == r.GroupID {
					g.MACs = append(g.MACs, r.MACs...)
					g.Revision++
				}
			}
			m.transfer = nil
		}
		m.snapshot.Pending = false
		result.Operation = &sdk.MiningGroupOperation{ID: r.GroupID, Phase: "COMPLETE"}
	default:
		return result, fmt.Errorf("unexpected action %s", r.Action)
	}
	if r.Key != "" {
		m.receipts[r.Key] = result
	}
	if r.Action == "create" && !m.lostCreate {
		m.lostCreate = true
		return sdk.MiningGroupResult{}, fmt.Errorf("response lost after commit")
	}
	if r.Action == "replace_pools" && m.loseReplaceAck {
		m.loseReplaceAck = false
		return sdk.MiningGroupResult{}, fmt.Errorf("pool response lost after commit")
	}
	return result, nil
}
func TestDurableGroupReconciliationRecoversLostReplyAndWaitsForTransfer(t *testing.T) {
	testDurableGroupReconciliation(t, false)
}

func TestArchivedGroupConfigurationAllowsReconciliationButNotNewAssignments(t *testing.T) {
	testDurableGroupReconciliation(t, true)
}

func testDurableGroupReconciliation(t *testing.T, archive bool) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	controller := database.CreateDevice(user.OrganizationID, "proto")
	conn := database.DB
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: user.DatabaseID, OrganizationID: user.OrganizationID, AuthMethod: session.AuthMethodSession})
	_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, controller.DatabaseID)
	require.NoError(t, err)
	_, err = sqlc.New(conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: controller.ID, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tank, module, board int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller.DatabaseID).Scan(&tank))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module-a',now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, user.OrganizationID).Scan(&board))
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),'{"macAddress":"02:00:00:00:00:01"}')`, board, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now())`, module, board)
	require.NoError(t, err)
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: cfg.ServiceMasterKey})
	require.NoError(t, err)
	pools := hostedaccounts.NewPoolService(hostedaccounts.NewService(conn, nil), secrets, nil, nil)
	password := "private-password"
	config := seedLegacyHostPoolRevision(t, ctx, sqlc.New(conn), secrets, user.OrganizationID, poolconfigurations.RevisionOptions{Name: "Customer pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://primary.example:3333", Username: "customer.worker", Password: &password}, {URL: "stratum+tcp://backup.example:3333", Username: "customer.worker"}}})
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 289, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}}}}, receipts: map[string]sdk.MiningGroupResult{}, loseReplaceAck: true}
	service := NewService(conn, firmware, pools, secrets)
	state, err := service.GetState(ctx, controller.ID)
	require.NoError(t, err)
	requestID := uuid.NewString()
	initialRevision := state.Record.Revision
	_, err = service.submitLegacyRequest(ctx, controller.ID, requestID, config.RevisionID, []int64{board}, state.Record.Revision)
	require.NoError(t, err)
	_, err = service.submitLegacyRequest(ctx, controller.ID, requestID, config.RevisionID, []int64{board}, state.Record.Revision)
	require.NoError(t, err)
	q := sqlc.New(conn)
	if archive {
		host, err := q.GetHostPoolAccount(ctx, user.OrganizationID)
		require.NoError(t, err)
		// Host-owned configurations currently have no public archive action.
		// Exercise the stored archive state without broadening account permissions.
		rows, err := q.ArchiveHostedAccountPool(ctx, sqlc.ArchiveHostedAccountPoolParams{OrganizationID: user.OrganizationID, AccountID: host.ID, PoolID: config.PoolID})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
		_, err = q.GetHostMiningGroupPoolRevision(ctx, sqlc.GetHostMiningGroupPoolRevisionParams{OrganizationID: user.OrganizationID, RevisionID: config.RevisionID})
		require.Error(t, err, "archived configurations cannot be selected for new assignments")
	}
	// Independent replicas may advance the same journal concurrently; database
	// locks must still serialize the committed step and exact replay key.
	outcomes := make(chan error, 2)
	for range 2 {
		go func() {
			outcomes <- NewService(conn, firmware, pools, secrets).Reconcile(ctx, user.OrganizationID, controller.DatabaseID, controller.ID)
		}()
	}
	for range 2 {
		require.NoError(t, <-outcomes)
	}
	sawTransfer := false
	for range 16 {
		// A new service instance on each tick exercises persistence, not process memory.
		service = NewService(conn, firmware, pools, secrets)
		require.NoError(t, service.Reconcile(ctx, user.OrganizationID, controller.DatabaseID, controller.ID))
		row, err := q.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: user.OrganizationID, DeviceID: controller.DatabaseID})
		require.NoError(t, err)
		require.NotContains(t, string(row.Step.RawMessage), password)
		require.NotContains(t, string(row.Observed), password)
		if row.ControllerOperationID != "" {
			sawTransfer = true
			require.NotEqual(t, "complete", row.Status)
			_, err := devicewrite.NewService(conn).Reserve(ctx, user.OrganizationID, controller.DatabaseID)
			require.Error(t, err, "custody must not move during an accepted asynchronous transfer")
		}
		if row.Status == "complete" {
			break
		}
	}
	require.True(t, sawTransfer)
	state, err = service.GetState(ctx, controller.ID)
	require.NoError(t, err)
	require.Equal(t, "complete", state.Record.Status)
	if archive {
		_, err = service.submitLegacyRequest(ctx, controller.ID, uuid.NewString(), config.RevisionID, []int64{board}, state.Record.Revision)
		require.ErrorContains(t, err, "pool configuration not found", "new assignments must reject archived revisions")
		// Idempotent replay is the already accepted operation, not a new assignment.
		_, err = service.submitLegacyRequest(ctx, controller.ID, requestID, config.RevisionID, []int64{board}, initialRevision)
		require.NoError(t, err)
	}
	queued, err := q.ListMiningGroupControllers(ctx)
	require.NoError(t, err)
	require.Empty(t, queued, "completed journals must not starve pending work")
	require.Equal(t, []string{"02:00:00:00:00:02"}, firmware.snapshot.Groups[0].MACs)
	require.Equal(t, []string{"02:00:00:00:00:01"}, firmware.snapshot.Groups[1].MACs)
	require.Len(t, firmware.snapshot.Groups[1].Pools, 2)
	var creates, replacements []sdk.MiningGroupRequest
	for _, call := range firmware.calls {
		switch call.Action {
		case "create":
			creates = append(creates, call)
		case "replace_pools":
			replacements = append(replacements, call)
		}
	}
	require.Len(t, creates, 2)
	require.Equal(t, creates[0].Key, creates[1].Key)
	require.Len(t, replacements, 2)
	require.Equal(t, []int32{0, 0}, replacements[0].PoolIDs)
	require.Equal(t, []string{"Pool 1", "Pool 2"}, replacements[0].PoolNames)
	require.Equal(t, replacements[0], replacements[1])
	_, err = service.submitLegacyRequest(ctx, controller.ID, requestID, config.RevisionID, []int64{board + 1}, state.Record.Revision)
	require.Error(t, err)
	var privateSlots string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT slots::text FROM hosted_account_pool_revision WHERE revision_id=$1`, config.RevisionID).Scan(&privateSlots))
	require.NotContains(t, privateSlots, password)
	// Same config identity stays mapped to the exact installed board.
	require.Len(t, state.Desired, 1)
	require.Equal(t, board, state.Desired[0].HashboardID)
	observation, err := json.Marshal(state.Observed)
	require.NoError(t, err)
	require.NotContains(t, string(observation), password)
	if archive {
		replacement := seedLegacyHostPoolRevision(t, ctx, sqlc.New(conn), secrets, user.OrganizationID, poolconfigurations.RevisionOptions{Name: "Replacement", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://replacement.example:3333", Username: "replacement.worker"}}})
		_, err = service.submitLegacyRequest(ctx, controller.ID, uuid.NewString(), replacement.RevisionID, []int64{board}, state.Record.Revision)
		require.NoError(t, err)
		for range 8 {
			require.NoError(t, service.Reconcile(ctx, user.OrganizationID, controller.DatabaseID, controller.ID))
			state, err = service.GetState(ctx, controller.ID)
			require.NoError(t, err)
			if state.Record.Status == "complete" {
				break
			}
		}
		require.Equal(t, "complete", state.Record.Status, "an archived existing binding must not block switching to an active configuration")
		require.Equal(t, replacement.RevisionID, state.Desired[0].PoolRevisionID)
		require.Equal(t, "replacement.worker", firmware.snapshot.Groups[1].Pools[0].Username)
	}
	other := database.CreateSuperAdminUser2()
	foreign := authn.SetInfo(ctx, &session.Info{UserID: other.DatabaseID, OrganizationID: other.OrganizationID, AuthMethod: session.AuthMethodSession})
	_, err = service.GetState(foreign, controller.ID)
	require.Error(t, err, "observations must be organization-scoped")
	apiKey := authn.SetInfo(ctx, &session.Info{UserID: user.DatabaseID, OrganizationID: user.OrganizationID, AuthMethod: session.AuthMethodAPIKey, AccountKey: true})
	_, err = service.submitLegacyRequest(apiKey, controller.ID, uuid.NewString(), config.RevisionID, []int64{board}, state.Record.Revision)
	require.Error(t, err, "customer keys cannot borrow host authority")
	// A removed board cannot be selected through its former module position.
	_, err = conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
	_, err = service.submitLegacyRequest(ctx, controller.ID, uuid.NewString(), config.RevisionID, []int64{board}, state.Record.Revision)
	require.Error(t, err)
	var status string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, state.Record.AttemptID.UUID).Scan(&status))
	require.Equal(t, "complete", status)

}

func TestMiningGroupReplacementPreservesNamesAfterReorder(t *testing.T) {
	existing := []sdk.MiningGroupPool{
		{ID: 1, Name: "Pool 1", Priority: 1},
		{ID: 2, Name: "Pool 2", Priority: 0},
	}
	ids, names := replacementPoolSlots(existing, 2)
	require.Equal(t, []int32{2, 1}, ids)
	require.Equal(t, []string{"Pool 2", "Pool 1"}, names)

	existing[0].Name = "Pool 3"
	ids, names = replacementPoolSlots(existing, 3)
	require.Equal(t, []int32{2, 1, 0}, ids)
	require.Equal(t, []string{"Pool 2", "Pool 3", "Pool 3 (2)"}, names)
}

// seedLegacyHostPoolRevision models an encrypted pre-cutover revision retained by
// durable mining-group workflows; no removed library API is exercised here.
func seedLegacyHostPoolRevision(t *testing.T, ctx context.Context, q sqlc.Querier, cipher *encrypt.Service, orgID int64, options poolconfigurations.RevisionOptions) sqlc.HostedAccountPoolRevision {
	t.Helper()
	host, err := q.GetHostPoolAccount(ctx, orgID)
	require.NoError(t, err)
	pool, err := q.CreateHostedAccountPool(ctx, sqlc.CreateHostedAccountPoolParams{PoolID: uuid.NewString(), OrganizationID: orgID, AccountID: host.ID})
	require.NoError(t, err)
	slots, err := poolconfigurations.EncryptSlots(cipher, options.Slots, nil)
	require.NoError(t, err)
	revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: uuid.NewString(), OrganizationID: orgID, AccountID: host.ID, PoolID: pool.PoolID, Number: 1, Name: options.Name, Slots: slots})
	require.NoError(t, err)
	return revision
}
