package mininggroups

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type activationPoolMiner struct {
	*simulatedGroups
	stored         []dto.MiningPool
	writes         int
	loseActivation bool
	observeErr     bool
	running        bool
	beforeCall     func(context.Context, sdk.MiningGroupRequest)
}

func (m *activationPoolMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m *activationPoolMiner) UpdateMiningPools(_ context.Context, p dto.UpdateMiningPoolsPayload) error {
	m.writes++
	m.stored = []dto.MiningPool{p.DefaultPool}
	for _, p := range []*dto.MiningPool{p.Backup1Pool, p.Backup2Pool} {
		if p != nil {
			m.stored = append(m.stored, *p)
		}
	}
	return nil
}
func (m *activationPoolMiner) GetMiningPools(context.Context) ([]interfaces.MinerConfiguredPool, error) {
	var pools []interfaces.MinerConfiguredPool
	for i, p := range m.stored {
		pools = append(pools, interfaces.MinerConfiguredPool{Priority: []int32{0, 1, 2}[i], URL: p.URL, Username: p.Username})
	}
	return pools, nil
}
func (m *activationPoolMiner) GetStoredMiningPools(context.Context) ([]interfaces.MinerStoredPool, error) {
	var pools []interfaces.MinerStoredPool
	for i, p := range m.stored {
		pools = append(pools, interfaces.MinerStoredPool{Priority: []int32{0, 1, 2}[i], URL: p.URL, Username: p.Username, Password: &p.Password})
	}
	return pools, nil
}
func (m *activationPoolMiner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if m.beforeCall != nil {
		m.beforeCall(ctx, r)
	}
	switch r.Action {
	case "observe":
		if m.observeErr {
			return sdk.MiningGroupResult{}, errors.New("controller unavailable")
		}
	case "mining_status":
		return sdk.MiningGroupResult{MiningStatus: &sdk.MiningGroupMiningStatus{Mining: m.running}}, nil
	case "stop_legacy":
		m.running = false
		return sdk.MiningGroupResult{StopRequested: true}, nil
	case "activate":
		m.calls = append(m.calls, r)
		m.snapshot.Enabled = true
		m.snapshot.Groups[0].Pools = nil
		for i, p := range m.stored {
			m.snapshot.Groups[0].Pools = append(m.snapshot.Groups[0].Pools, sdk.MiningGroupPool{Priority: []int32{0, 1, 2}[i], URL: p.URL, Username: p.Username, Enabled: true})
		}
		if m.loseActivation {
			m.loseActivation = false
			return sdk.MiningGroupResult{}, errors.New("activation reply lost")
		}
		return sdk.MiningGroupResult{Enabled: true}, nil
	}
	return m.simulatedGroups.MiningGroups(ctx, r)
}

func newActivationPoolFixture(t *testing.T, extraBoard ...bool) (context.Context, retryFixture, *activationPoolMiner, *poolconfigurations.Service, poolconfigurations.Configuration, []int64, func(poolconfigurations.Review) poolconfigurations.ReviewedSave) {
	t.Helper()
	return newActivationPoolFixtureWithWorker(t, "controller-worker", extraBoard...)
}

func newActivationPoolFixtureWithWorker(t *testing.T, workerName string, extraBoard ...bool) (context.Context, retryFixture, *activationPoolMiner, *poolconfigurations.Service, poolconfigurations.Configuration, []int64, func(poolconfigurations.Review) poolconfigurations.ReviewedSave) {
	t.Helper()
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	if len(extraBoard) > 0 && extraBoard[0] {
		var module, second int64
		require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0]).Scan(&module))
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&second))
		_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}')`, second, module)
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,2,'occupied',now())`, module, second)
		require.NoError(t, err)
		boards = append(boards, second)
	}
	publishSharedBoardInventory(t, ctx, f)
	miner := &activationPoolMiner{simulatedGroups: &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}}
	if len(boards) == 2 {
		miner.snapshot.Groups[0].MACs = append(miner.snapshot.Groups[0].MACs, "02:00:00:00:00:02")
	}
	f.service.miners = miner
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
	f.service.WithPoolConfigurations(repo, reviewer)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	password := "private-pool-password"
	_, err = f.conn.ExecContext(ctx, `UPDATE device SET worker_name=$2 WHERE id=$1`, f.target.DeviceID, workerName)
	require.NoError(t, err)
	config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
	require.NoError(t, err)
	confirm := func(review poolconfigurations.Review) poolconfigurations.ReviewedSave {
		saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer, nil)
		})
		require.NoError(t, err)
		return saved
	}
	review, err := service.ReviewDevices(ctx, "", config.ID, config.Version, []string{f.target.DeviceIdentifier})
	require.NoError(t, err)
	saved := confirm(review)
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, miner, f.service.secrets)
	require.NoError(t, worker.Execute(ctx, info.OrganizationID, saved.Operation.ID, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: f.target.DeviceID, DeviceID: f.target.DeviceID}))
	service.WithChecker(poolapplications.NewChecker(f.conn, reviewer, miner, f.service.secrets, f.service.pools))
	return ctx, f, miner, service, config, boards, confirm
}

func TestActivationPreservesPoolAssignmentAndSubsequentEdit(t *testing.T) {
	ctx, f, miner, service, config, boards, confirm := newActivationPoolFixture(t)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	password := "private-pool-password"
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 8 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, assignments.Assignments, 1)
	require.Equal(t, config.ID, assignments.Assignments[0].ConfigurationID)
	require.Equal(t, "verified", assignments.Assignments[0].State)
	old, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: info.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, old.RetiredAt.Valid)
	require.Equal(t, 1, miner.writes)
	for _, call := range miner.calls {
		require.NotContains(t, []string{"replace_pools", "clear_pools", "transfer"}, call.Action)
	}
	edited, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
	require.NoError(t, err)
	require.Len(t, edited.Targets, 1)
	updated := confirm(edited)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, updated.Operation.ID, f.target.DeviceID))
	for range 20 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	operation, err := service.GetOperation(ctx, "", updated.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "verified", operation.Targets[0].State)
	require.Equal(t, "stratum+tcp://updated.example:3333", miner.snapshot.Groups[1].Pools[0].URL)
	require.Equal(t, "account.controller-worker", miner.snapshot.Groups[1].Pools[0].Username)
}

func TestActivationRetainedBoardNameCanCorrectInheritedPools(t *testing.T) {
	for _, savedWorker := range []string{"rack-one", ""} {
		for _, editFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("name=%q/edit=%t", savedWorker, editFirst), func(t *testing.T) {
				ctx, f, miner, service, config, boards, confirm := newActivationPoolFixture(t)
				// A relocated physical board retains its choice, while activation inherits
				// the destination controller's currently applied pool stack.
				_, err := f.conn.ExecContext(ctx, `UPDATE hashboard SET worker_name=$2,worker_name_mode=CASE WHEN $2='' THEN 'none' ELSE 'custom' END WHERE id=$1`, boards[0], savedWorker)
				require.NoError(t, err)
				_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
				require.NoError(t, err)
				for range 8 {
					if active(f.state(t).Status) {
						f.reconcile(t, ctx)
					}
				}
				require.Equal(t, statusComplete, f.state(t).Status)
				require.Equal(t, "account.controller-worker", miner.snapshot.Groups[0].Pools[0].Username)
				var savedName string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT worker_name FROM hashboard WHERE id=$1`, boards[0]).Scan(&savedName))
				require.Equal(t, savedWorker, savedName, "activation must preserve a physical board's explicit choice")
				if editFirst {
					password := "updated-password"
					edit, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: config.Name, Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
					require.NoError(t, err)
					updated := confirm(edit)
					config = updated.Configuration
					require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, updated.Operation.ID, f.target.DeviceID))
					for range 20 {
						if active(f.state(t).Status) {
							f.reconcile(t, ctx)
						}
					}
					require.Equal(t, statusComplete, f.state(t).Status)
					op, err := service.GetOperation(ctx, "", updated.Operation.ID)
					require.NoError(t, err)
					require.Equal(t, "verified", op.Targets[0].State)
					require.Equal(t, "account.controller-worker", miner.snapshot.Groups[1].Pools[0].Username)
					require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT worker_name FROM hashboard WHERE id=$1`, boards[0]).Scan(&savedName))
					require.Equal(t, savedWorker, savedName)
				}
				reviews, count, err := service.ReviewBoardWorkerNames(ctx, poolconfigurations.BoardSelection{HashboardIDs: boards}, poolconfigurations.WorkerNameOptions{Custom: savedWorker})
				require.NoError(t, err)
				require.Equal(t, 1, count)
				require.Len(t, reviews, 1, "matching saved metadata is not proof that inherited settings match")
				require.Equal(t, "controller-worker", reviews[0].WorkerNames[0].Before)
				require.Equal(t, savedWorker, reviews[0].WorkerNames[0].After)
				// A reviewed correction cannot replace intent changed after review.
				info, err := session.GetInfo(ctx)
				require.NoError(t, err)
				repo := poolconfigurations.NewRepository(f.service.secrets)
				tx, err := f.conn.BeginTx(ctx, nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = tx.Rollback() })
				_, err = tx.ExecContext(ctx, `UPDATE pool_configuration_assignment SET version=version+1 WHERE device_id=$1 AND target_kind='hashboard_installation'`, f.target.DeviceID)
				require.NoError(t, err)
				_, err = repo.ConfirmReview(ctx, sqlc.New(tx), poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, reviews[0].ID, f.service.sharedReviewer, nil)
				require.Error(t, err)
				require.NoError(t, tx.Rollback())
				var inherited string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT inherited_worker_name FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation'`, f.target.DeviceID).Scan(&inherited))
				require.Equal(t, "controller-worker", inherited)
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT worker_name FROM hashboard WHERE id=$1`, boards[0]).Scan(&savedName))
				require.Equal(t, savedWorker, savedName)
				reviews, _, err = service.ReviewBoardWorkerNames(ctx, poolconfigurations.BoardSelection{HashboardIDs: boards}, poolconfigurations.WorkerNameOptions{Custom: savedWorker})
				require.NoError(t, err)
				require.Len(t, reviews, 1)
				saved := confirm(reviews[0])
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
				for range 20 {
					if active(f.state(t).Status) {
						f.reconcile(t, ctx)
					}
				}
				require.Equal(t, statusComplete, f.state(t).Status)
				assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
				require.NoError(t, err)
				checked, err := service.CheckConfiguration(ctx, "", config.ID, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: assignments.Assignments[0].InstallationID})
				require.NoError(t, err)
				require.Equal(t, "verified", checked.State)
				expected := "account"
				if savedWorker != "" {
					expected += "." + savedWorker
				}
				require.Equal(t, expected, checked.Reported[0].Username)
				reviews, _, err = service.ReviewBoardWorkerNames(ctx, poolconfigurations.BoardSelection{HashboardIDs: boards}, poolconfigurations.WorkerNameOptions{Custom: savedWorker})
				require.NoError(t, err)
				require.Empty(t, reviews, "already applied names remain a no-op")
			})
		}
	}
}

func TestActivationPreservesEmptyControllerWorkerInput(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixtureWithWorker(t, "")
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 8 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	require.Equal(t, "account", miner.snapshot.Groups[0].Pools[0].Username)
	var name, mode, inherited string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT h.worker_name,h.worker_name_mode,a.inherited_worker_name FROM hashboard h JOIN hashboard_installation i ON i.hashboard_id=h.id JOIN pool_configuration_assignment a ON a.target_kind='hashboard_installation' AND a.target_id=i.id WHERE h.id=$1`, boards[0]).Scan(&name, &mode, &inherited))
	require.Empty(t, name)
	require.Equal(t, "none", mode)
	require.Empty(t, inherited)
}

func TestActivationPoolHandoffRetriesAndObservations(t *testing.T) {
	for _, scenario := range []string{"restart", "different", "missing board", "ambiguous membership", "read unavailable", "saved worker", "legacy journal", "running"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
			expectedState := "verified"
			requestID := uuid.NewString()
			if scenario == "saved worker" || scenario == "legacy journal" {
				_, err := f.conn.ExecContext(ctx, `UPDATE device SET worker_name='changed-after-application' WHERE id=$1`, f.target.DeviceID)
				require.NoError(t, err)
			}
			miner.running = scenario == "running"
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, requestID)
			require.NoError(t, err)
			if scenario == "legacy journal" {
				_, err = f.conn.ExecContext(ctx, `UPDATE mining_group_pool_handoff SET captured=captured-'WorkerName' WHERE device_id=$1`, f.target.DeviceID)
				require.NoError(t, err)
			}
			f.prepareActivation(t, ctx)
			miner.loseActivation = scenario == "restart"
			f.reconcile(t, ctx)
			switch scenario {
			case "restart":
				f.service = NewService(f.conn, miner, f.service.pools, f.service.secrets)
				_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, requestID)
				require.NoError(t, err)
			case "different":
				miner.snapshot.Groups[0].Pools[0].Username = "external-worker"
				expectedState = "different"
			case "missing board":
				miner.snapshot.Groups[0].MACs = nil
				expectedState = "applied_unverified"
			case "ambiguous membership":
				miner.snapshot.Groups = append(miner.snapshot.Groups, sdk.MiningGroup{ID: "foreign", MACs: []string{"02:00:00:00:00:01"}})
				expectedState = "applied_unverified"
			case "read unavailable":
				miner.snapshot.Pending = true
				f.reconcile(t, ctx)
				old, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
				require.NoError(t, err)
				require.False(t, old.RetiredAt.Valid)
				miner.snapshot.Pending = false
			}
			for range 8 {
				if active(f.state(t).Status) {
					f.reconcile(t, ctx)
				}
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
			require.NoError(t, err)
			require.Equal(t, config.ID, got.Assignments[0].ConfigurationID)
			require.Equal(t, expectedState, got.Assignments[0].State)
			var inherited string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT inherited_worker_name FROM pool_configuration_assignment WHERE target_kind='hashboard_installation' AND target_id=$1`, got.Assignments[0].InstallationID).Scan(&inherited))
			require.Equal(t, "controller-worker", inherited)
			require.Equal(t, scenario == "running", miner.snapshot.Groups[0].Mining)
			checked, err := service.CheckConfiguration(ctx, "", config.ID, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: got.Assignments[0].InstallationID})
			require.NoError(t, err)
			require.Equal(t, expectedState, checked.State)
			_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, requestID)
			require.NoError(t, err)
			var version int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT version FROM pool_configuration_assignment WHERE target_kind='hashboard_installation' AND target_id=$1`, got.Assignments[0].InstallationID).Scan(&version))
			require.EqualValues(t, 1, version)
			require.Equal(t, 1, miner.writes)
		})
	}
}

func TestActivationPoolHandoffNeverOverwritesChangedEvidence(t *testing.T) {
	for _, scenario := range []string{"source version", "board replaced", "ownership generation", "permission revoked", "new board assignment"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			f.prepareActivation(t, ctx)
			f.reconcile(t, ctx) // device activated; assignment transaction has not run
			var statement string
			switch scenario {
			case "source version":
				statement = `UPDATE pool_configuration_assignment SET version=version+1 WHERE device_id=$1`
			case "board replaced":
				statement = `UPDATE hashboard_installation SET removed_at=now() WHERE device_id=$1`
			case "ownership generation": // Ownership revisions detect recall and return to the host.
			case "permission revoked":
				setMiningGroupPermissions(t, ctx, f, []string{"site:read", "fleet:read", "miner:read", "miner:activate_mining_groups", "miner:start_mining", "miner:stop_mining"})
			case "new board assignment":
				// Live container/board assignments are mutually exclusive at the database
				// boundary. A newer retired board intent can coexist and must also win.
				_, conflictErr := f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at) SELECT a.organization_id,'hashboard_installation',i.id,a.device_id,a.configuration_id,a.revision_id,1,now() FROM pool_configuration_assignment a JOIN hashboard_installation i ON i.device_id=a.device_id WHERE a.device_id=$1 AND a.target_kind='device'`, f.target.DeviceID)
				require.ErrorContains(t, conflictErr, "retire whole-device intent")
				statement = `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at,retired_at) SELECT a.organization_id,'hashboard_installation',i.id,a.device_id,a.configuration_id,a.revision_id,1,now(),now() FROM pool_configuration_assignment a JOIN hashboard_installation i ON i.device_id=a.device_id WHERE a.device_id=$1 AND a.target_kind='device'`
			}
			if scenario == "ownership generation" {
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,revision) VALUES($1,7,$2,$2,$3,2)`, f.target.OrganizationID, boards[0], f.target.DeviceID)
			} else if statement != "" {
				_, err = f.conn.ExecContext(ctx, statement, f.target.DeviceID)
			}
			require.NoError(t, err)
			before := len(miner.calls)
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			for _, call := range miner.calls[before:] {
				require.True(t, call.ReadOnly())
			}
			old, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
			require.NoError(t, err)
			require.False(t, old.RetiredAt.Valid)
			if scenario == "new board assignment" {
				var count int
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation' AND version=1 AND retired_at IS NOT NULL`, f.target.DeviceID).Scan(&count))
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestActivationAlreadyEnabledRequiresReviewedRecovery(t *testing.T) {
	ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
	miner.snapshot.Enabled = true
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "keeping device settings")
	require.Equal(t, "idle", f.state(t).Status)
}

func TestActivationPoolHandoffRollsBackRetirementAndRetries(t *testing.T) {
	ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.prepareActivation(t, ctx)
	f.reconcile(t, ctx)
	// Force the final binding write to reject after retirement and board inserts.
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings='{"default":{"config_id":"conflict"}}' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	require.Error(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	old, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, old.RetiredAt.Valid)
	got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Empty(t, got.Assignments[0].ConfigurationID)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings='{}' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	f.service = NewService(f.conn, miner, f.service.pools, f.service.secrets)
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
	got, err = service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Equal(t, config.ID, got.Assignments[0].ConfigurationID)
	snapshot, err := sqlc.New(f.conn).GetAdoptedPoolBoardSnapshot(ctx, sqlc.GetAdoptedPoolBoardSnapshotParams{OrganizationID: f.target.OrganizationID, TargetID: got.Assignments[0].InstallationID})
	require.NoError(t, err)
	slots, err := poolconfigurations.DecodeSlots(snapshot)
	require.NoError(t, err)
	secret, err := f.service.secrets.Decrypt(slots[0].PasswordCiphertext)
	require.NoError(t, err)
	require.Equal(t, miner.stored[0].Password, string(secret))
}

func TestActivationWithoutFleetAssignmentDoesNotInferOwnership(t *testing.T) {
	ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
	_, err := f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 8 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Empty(t, got.Assignments[0].ConfigurationID)
	require.Equal(t, 1, miner.writes)
}

func TestActivationPoolHandoffRejectsExternallyEnabledControllerWithStaleCache(t *testing.T) {
	ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
	miner.snapshot.Enabled = true // Cached GetState still says disabled.
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Empty(t, got.Assignments[0].ConfigurationID)
}

func TestActivationPoolHandoffRechecksOwnershipBeforeResume(t *testing.T) {
	for _, scenario := range []string{"ownership generation", "board replaced", "permission revoked"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			miner.running = true
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			f.prepareActivation(t, ctx)
			f.reconcile(t, ctx) // activate
			f.reconcile(t, ctx) // handoff; journal start-default before dispatch
			require.True(t, f.state(t).Step.Valid)
			switch scenario {
			case "ownership generation":
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,revision) VALUES($1,7,$2,$2,$3,2)`, f.target.OrganizationID, boards[0], f.target.DeviceID)
			case "board replaced":
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE device_id=$1`, f.target.DeviceID)
			case "permission revoked":
				setMiningGroupPermissions(t, ctx, f, []string{"site:read", "fleet:read", "miner:read", "miner:activate_mining_groups", "miner:start_mining", "miner:stop_mining"})
			}
			require.NoError(t, err)
			before := len(miner.calls)
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status, "the prepared start was never sent and can release admission")
			require.False(t, f.state(t).Step.Valid)
			for _, call := range miner.calls[before:] {
				require.True(t, call.ReadOnly())
			}
			require.False(t, miner.snapshot.Groups[0].Mining)
		})
	}
}

func TestActivationHandoffPreservesUnselectedBoardOnLaterAssignment(t *testing.T) {
	ctx, f, miner, service, config, boards, confirm := newActivationPoolFixture(t, true)
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 8 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, got.Assignments, 2)
	for _, assignment := range got.Assignments {
		require.Equal(t, config.ID, assignment.ConfigurationID)
		require.Equal(t, "verified", assignment.State)
	}
	changed, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Other", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://other.example:3333", Username: "other"}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, "", changed.ID, changed.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[:1]})
	require.NoError(t, err)
	saved := confirm(review)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
	for range 20 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	require.Equal(t, []string{"02:00:00:00:00:02"}, miner.snapshot.Groups[0].MACs)
	require.False(t, miner.snapshot.Groups[0].Mining)
	require.Equal(t, []string{"02:00:00:00:00:01"}, miner.snapshot.Groups[1].MACs)
	require.Equal(t, 1, miner.created, "unchanged inherited boards must not consume another group")
	got, err = service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Equal(t, changed.ID, got.Assignments[0].ConfigurationID)
	require.Equal(t, config.ID, got.Assignments[1].ConfigurationID)
}

func TestActivationPoolHandoffRejectsDuplicateBoardIdentity(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t, true)
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"02:00:00:00:00:01"}' WHERE hashboard_id=$1`, boards[1])
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "ambiguous")
	require.Equal(t, "idle", f.state(t).Status)
}

func TestActivationControllerIODoesNotBlockHostedReads(t *testing.T) {
	for _, action := range []string{"observe", "stop_legacy", "activate", "start"} {
		t.Run(action, func(t *testing.T) {
			ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
			miner.running = true
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			switch action {
			case "stop_legacy":
				f.reconcile(t, ctx)
			case "activate":
				f.prepareActivation(t, ctx)
			case "start":
				f.prepareActivation(t, ctx)
				f.reconcile(t, ctx)
				f.reconcile(t, ctx)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			miner.beforeCall = func(ctx context.Context, r sdk.MiningGroupRequest) {
				if r.Action == action {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- f.service.Reconcile(runCtx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
			}()
			select {
			case <-entered:
			case <-runCtx.Done():
				t.Fatal("controller call did not start")
			}
			readCtx, readCancel := context.WithTimeout(ctx, time.Second)
			_, readErr := sqlc.New(f.conn).LockHostedAccountOrganizationForShare(readCtx, f.target.OrganizationID)
			readCancel()
			close(release)
			require.NoError(t, <-done)
			require.NoError(t, readErr, "unrelated hosted reads must complete while controller I/O is stalled")
		})
	}
}

func TestActivationMissingHandoffRequiresRecovery(t *testing.T) {
	ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.prepareActivation(t, ctx)
	f.reconcile(t, ctx)
	_, err = f.conn.ExecContext(ctx, `DELETE FROM mining_group_pool_handoff WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	before := len(miner.calls)
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	require.Contains(t, f.state(t).Message, "keeping device settings")
	for _, call := range miner.calls[before:] {
		require.True(t, call.ReadOnly())
	}
	got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Empty(t, got.Assignments[0].ConfigurationID)
	var retired bool
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT retired_at IS NOT NULL FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='device'`, f.target.DeviceID).Scan(&retired))
	require.False(t, retired)
}

func TestActivationPoolHandoffVerifiesOnlyFreshReadback(t *testing.T) {
	for _, scenario := range []string{"matching", "different", "missing board", "unavailable", "pending", "suspended"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			f.prepareActivation(t, ctx)
			f.reconcile(t, ctx)
			encoded, err := encode(miner.snapshot)
			require.NoError(t, err)
			var observed sdk.MiningGroupResult
			require.NoError(t, decode(encoded, &observed))
			ready := &activationPoolHandoffReady{row: f.state(t), observed: observed, observedAt: time.Now()}
			expected := "applied_unverified"
			switch scenario {
			case "matching":
				expected = "verified"
			case "different":
				miner.snapshot.Groups[0].Pools[0].Username = "changed-during-lock-gap"
				expected = "different"
			case "missing board":
				miner.snapshot.Groups[0].MACs = nil
			case "unavailable":
				miner.observeErr = true
			case "pending":
				miner.snapshot.Pending = true
			case "suspended":
				miner.snapshot.Suspended = true
			}
			attempt, err := f.service.writes.ResumeWorkflow(ctx, f.target.OrganizationID, f.target.DeviceID, ready.row.AttemptID.UUID)
			require.NoError(t, err)
			before := len(miner.calls)
			applied, err := f.service.commitActivationPoolHandoff(attempt.Context(ctx), ready)
			require.NoError(t, err)
			require.True(t, applied)
			require.Len(t, miner.calls, before, "handoff must remain database-only")
			assertState := func(want string) {
				t.Helper()
				got, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
				require.NoError(t, err)
				require.Len(t, got.Assignments, 1)
				require.Equal(t, want, got.Assignments[0].State)
				var verified bool
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT last_verified_at IS NOT NULL FROM pool_configuration_observation WHERE target_kind='hashboard_installation' AND target_id=$1`, got.Assignments[0].InstallationID).Scan(&verified))
				require.Equal(t, want == "verified", verified)
			}
			assertState("applied_unverified")
			// A restart between adoption and verification must use fresh readback.
			f.service = NewService(f.conn, miner, f.service.pools, f.service.secrets)
			f.reconcile(t, ctx)
			assertState(expected)
			if scenario == "unavailable" || scenario == "pending" || scenario == "suspended" {
				require.NotEqual(t, statusComplete, f.state(t).Status)
				miner.observeErr = false
				miner.snapshot.Pending = false
				miner.snapshot.Suspended = false
				f.reconcile(t, ctx)
				assertState("verified")
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			require.Equal(t, 1, miner.writes, "verification must not rewrite pools")
		})
	}
}

func TestActivationPoolHandoffRevalidatesAfterReleasingObservationLocks(t *testing.T) {
	for _, change := range []string{"source", "ownership", "installation", "permission", "journal", "expired observation"} {
		t.Run(change, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			f.prepareActivation(t, ctx)
			f.reconcile(t, ctx)
			ready := &activationPoolHandoffReady{row: f.state(t), observed: miner.snapshot, observedAt: time.Now()}
			switch change {
			case "source":
				_, err = f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET version=version+1 WHERE device_id=$1`, f.target.DeviceID)
			case "ownership":
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,revision) VALUES($1,7,$2,$2,$3,2)`, f.target.OrganizationID, boards[0], f.target.DeviceID)
			case "installation":
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE device_id=$1`, f.target.DeviceID)
			case "permission":
				setMiningGroupPermissions(t, ctx, f, []string{"site:read", "fleet:read", "miner:read", "miner:update_pools", "miner:start_mining", "miner:stop_mining"})
			case "journal":
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET revision=revision+1 WHERE device_id=$1`, f.target.DeviceID)
			case "expired observation":
				ready.observedAt = time.Now().Add(-2 * reconciliationTimeout)
			}
			require.NoError(t, err)
			attempt, err := f.service.writes.ResumeWorkflow(ctx, f.target.OrganizationID, f.target.DeviceID, ready.row.AttemptID.UUID)
			require.NoError(t, err)
			before := len(miner.calls)
			applied, err := f.service.commitActivationPoolHandoff(attempt.Context(ctx), ready)
			if change == "journal" || change == "expired observation" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.False(t, applied)
			require.Len(t, miner.calls, before, "exclusive handoff transaction must never perform controller I/O")
			var retired bool
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT retired_at IS NOT NULL FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='device'`, f.target.DeviceID).Scan(&retired))
			require.False(t, retired)
			var adopted int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation'`, f.target.DeviceID).Scan(&adopted))
			require.Zero(t, adopted)
		})
	}
}

func TestActivationPoolHandoffCommitFailurePreservesEvidence(t *testing.T) {
	ctx, f, _, _, _, _, _ := newActivationPoolFixture(t)
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.prepareActivation(t, ctx)
	f.reconcile(t, ctx)
	_, err = f.conn.ExecContext(ctx, `CREATE FUNCTION reject_handoff_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'handoff commit rejected'; END $$;
 CREATE CONSTRAINT TRIGGER reject_handoff_commit AFTER UPDATE ON mining_group_pool_handoff DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_handoff_commit()`)
	require.NoError(t, err)
	err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
	require.ErrorContains(t, err, "handoff commit rejected")
	var retired, completed bool
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT a.retired_at IS NOT NULL,h.completed_at IS NOT NULL FROM pool_configuration_assignment a JOIN mining_group_pool_handoff h ON h.device_id=a.device_id WHERE a.device_id=$1 AND a.target_kind='device'`, f.target.DeviceID).Scan(&retired, &completed))
	require.False(t, retired)
	require.False(t, completed)
	var adopted int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='hashboard_installation'`, f.target.DeviceID).Scan(&adopted))
	require.Zero(t, adopted)
	_, err = f.conn.ExecContext(ctx, `DROP TRIGGER reject_handoff_commit ON mining_group_pool_handoff; DROP FUNCTION reject_handoff_commit()`)
	require.NoError(t, err)
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
}
