package hostedaccounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/jackc/pgx/v5/pgconn"

	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestReadDevicePoolsObservesControllerOnceFor150Boards(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	first, controller := f.miningModule(t)
	modules := []int64{first}
	for position := 2; position <= 50; position++ {
		var module int64
		require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) SELECT tank_id,$2,now(),last_seen_at,'active',now() FROM container_module WHERE id=$1 RETURNING id`, first, position).Scan(&module))
		for board := 1; board <= 3; board++ {
			f.miningBoard(t, module, controller, board)
		}
		modules = append(modules, module)
	}
	_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','board-' || id::text) WHERE module_id=ANY($1::bigint[])`, modules)
	require.NoError(t, err)
	selections := make([]*hostedpb.MiningAssetSelection, len(modules))
	for i, module := range modules {
		selections[i] = selected(6, module, 0)
	}
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selections...))
	boards, err := sqlc.New(f.db).ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: f.org, ModuleIds: modules})
	require.NoError(t, err)
	require.Len(t, boards, 150)
	targets := make([]poolconfigurations.AssignmentTarget, len(boards))
	macs := make([]string, len(boards))
	for i, board := range boards {
		require.True(t, board.Ready, fmt.Sprintf("board %d must be readable", board.InstallationID))
		targets[i] = poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID}
		macs[i] = board.MacAddress
	}
	device := &poolContainer{result: sdk.MiningGroupResult{Supported: true, Enabled: true, Groups: []sdk.MiningGroup{{MACs: macs, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://pool.example", Username: "payout.worker", Enabled: true}}}}}}
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools))
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); require.NoError(t, provider.Shutdown(context.Background())) })
	ctx, span := provider.Tracer("batch-read-test").Start(ctx, "read")
	defer span.End()
	results, err := service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.NoError(t, err)
	require.Len(t, results, 150)
	for _, result := range results {
		require.True(t, result.ReadbackAvailable)
		require.Equal(t, "device_configuration", result.State)
		require.Equal(t, []poolconfigurations.PoolReading{{URL: "stratum+tcp://pool.example", Username: "payout.worker"}}, result.Reported)
	}
	require.Equal(t, 1, device.observations, "all boards in a controller share one fresh observation")
	require.Zero(t, device.writes)
	counts := map[string]int{}
	for _, recorded := range recorder.Ended() {
		if recorded.SpanContext().TraceID() == span.SpanContext().TraceID() {
			counts[recorded.Name()]++
		}
	}
	require.Equal(t, 2, counts["db.ListPoolEquipmentModuleOwners"], "once per authorization pass")
	require.Equal(t, 2, counts["db.ListActivePoolAssignmentsForTargets"], "once per authorization pass")
	require.Equal(t, 2, counts["db.ListPoolEquipmentInstallations"], "lock all selected installations in each pass")
	require.Zero(t, counts["db.GetPoolConfigurationAssignment"], "no per-board assignment queries")
	require.Zero(t, counts["db.GetPoolEquipmentInstallation"], "no per-board installation queries")

	// A subsequent refresh must obtain new evidence rather than reuse a cache.
	device.result.Groups[0].Pools[0].Username = "changed.worker"
	results, err = service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.NoError(t, err)
	require.Equal(t, 2, device.observations)
	require.Equal(t, "changed.worker", results[0].Reported[0].Username)
	var assignments, operations int
	require.NoError(t, f.db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM pool_configuration_assignment), (SELECT count(*) FROM pool_configuration_operation)`).Scan(&assignments, &operations))
	require.Zero(t, assignments)
	require.Zero(t, operations)
}

func TestReadDevicePoolsFiltersUnauthorizedAndStaleBoards(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	module, controller := f.miningModule(t)
	_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','board-' || id::text) WHERE module_id=$1`, module)
	require.NoError(t, err)
	boards, err := sqlc.New(f.db).ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: f.org, ModuleIds: []int64{module}})
	require.NoError(t, err)
	require.Len(t, boards, 3)
	// Only the first two boards are allocated to this account. Sharing a
	// controller with them must not expose the third board or global settings.
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, boards[0].HashboardID, 0), selected(7, boards[1].HashboardID, 0)))
	_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{}' WHERE hashboard_id=$1`, boards[1].HashboardID)
	require.NoError(t, err)
	device := &poolContainer{result: sdk.MiningGroupResult{Supported: true, Enabled: true}}
	targets := []poolconfigurations.AssignmentTarget{}
	for i, board := range boards {
		targets = append(targets, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID})
		device.result.Groups = append(device.result.Groups, sdk.MiningGroup{MACs: []string{board.MacAddress}, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://pool.example", Username: fmt.Sprintf("private-%d", i), Enabled: true}}})
	}
	targets = append(targets, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller}, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: 999999})
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools))
	results, err := service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.NoError(t, err)
	require.Len(t, results, 5)
	require.True(t, results[0].ReadbackAvailable, "%+v", results)
	require.Equal(t, "private-0", results[0].Reported[0].Username)
	for _, result := range results[1:] {
		require.Equal(t, "unavailable", result.State)
		require.Empty(t, result.Reported)
		require.False(t, result.ReadbackAvailable)
	}
	require.Equal(t, 1, device.observations)
	require.Zero(t, device.writes)
	// Read admission holds inventory and device locks until the shared I/O returns.
	device.observe = func(context.Context) (sdk.MiningGroupResult, error) {
		for _, query := range []string{`SELECT id FROM device WHERE id=$1 FOR UPDATE NOWAIT`, `SELECT device_id FROM container_inventory WHERE device_id=$1 FOR UPDATE NOWAIT`} {
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), query, controller)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "55P03", pgErr.Code)
			require.NoError(t, tx.Rollback())
		}
		return device.result, nil
	}
	_, err = service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT device_id FROM container_inventory WHERE device_id=$1 FOR UPDATE NOWAIT`, controller)
	require.NoError(t, err, "inventory lock is released after observation")
}

type batchMinerGetter map[int64]interfaces.Miner

func (g batchMinerGetter) GetMinerForCommand(_ context.Context, id int64) (interfaces.Miner, error) {
	return g[id], nil
}

func TestReadDevicePoolsPreservesHealthyControllersAndSkipsAssignedTargets(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	getter := batchMinerGetter{}
	targets := []poolconfigurations.AssignmentTarget{}
	devices := []*poolContainer{}
	for i := range 3 {
		module, controller := f.miningModule(t)
		require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
		device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}, poolDevice: poolDevice{pools: []dto.MiningPool{{Username: fmt.Sprintf("worker-%d", i)}}}}
		getter[controller] = device
		devices = append(devices, device)
		targets = append(targets, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller})
	}
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)).WithReadiness(poolapplications.NewReadiness(f.db, reviewer, getter))
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Canonical", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "account"}}})
	require.NoError(t, err)
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, targets[2:])
	require.NoError(t, err)
	_, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
	require.NoError(t, err)
	for _, device := range devices {
		device.observations = 0
	}
	devices[0].readError = errors.New("private driver error")
	results, err := service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.Equal(t, "unavailable", results[0].State)
	require.Empty(t, results[0].Reported)
	require.Equal(t, "worker-1", results[1].Reported[0].Username)
	require.Equal(t, "unavailable", results[2].State)
	require.Empty(t, results[2].Reported)
	require.Equal(t, 1, devices[0].observations)
	require.Equal(t, 1, devices[1].observations)
	require.Zero(t, devices[2].observations, "canonical assignments use explicit verification only")
	for _, device := range devices {
		require.Zero(t, device.writes)
	}
	for _, invalid := range [][]poolconfigurations.AssignmentTarget{nil, {targets[0], targets[0]}, {{Kind: "module", ID: 1}}, {{Kind: poolconfigurations.TargetDevice, ID: 0}}, make([]poolconfigurations.AssignmentTarget, 501)} {
		_, err = service.ReadDevicePools(ctx, f.account.AccountID, invalid)
		require.True(t, fleeterror.IsInvalidArgumentError(err))
	}
	machineCtx, _, _ := f.keyContext(t, hostedaccounts.View|hostedaccounts.ChangePools)
	machineCtx, err = hostedaccounts.WithRequestAccountID(machineCtx, f.account.AccountID)
	require.NoError(t, err)
	machineResults, err := service.ReadDevicePools(machineCtx, f.account.AccountID, targets)
	require.NoError(t, err)
	for _, result := range machineResults {
		require.Equal(t, "unavailable", result.State)
		require.Empty(t, result.Reported)
	}
	require.Equal(t, 1, devices[1].observations, "invalid requests do not perform I/O")
}

// The callback runs after initial authorization has committed, before the final
// binding transaction takes its locks. The underlying authority is still real.
type changingBatchAuthority struct {
	poolconfigurations.Authority
	calls  int
	change func()
}

func (a *changingBatchAuthority) AuthorizePoolConfiguration(ctx context.Context, q sqlc.Querier, account string, access poolconfigurations.Access) (poolconfigurations.Principal, error) {
	a.calls++
	if a.calls == 2 {
		a.change()
	}
	return a.Authority.AuthorizePoolConfiguration(ctx, q, account, access)
}

func TestReadDevicePoolsRechecksBindingAndAuthorityBeforeObservation(t *testing.T) {
	for _, change := range []string{"MAC", "ownership", "permission", "stale inventory", "removed installation", "assignment"} {
		t.Run(change, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			module, _ := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"original"}' WHERE module_id=$1`, module)
			require.NoError(t, err)
			boards, err := sqlc.New(f.db).ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: f.org, ModuleIds: []int64{module}})
			require.NoError(t, err)
			board := boards[0]
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true, Enabled: true, Groups: []sdk.MiningGroup{{MACs: []string{"original"}, Pools: []sdk.MiningGroupPool{{Username: "private", Enabled: true}}}}}}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			assignmentService := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithReadiness(poolapplications.NewReadiness(f.db, reviewer, poolMinerGetter{device}))
			var assignmentReviewID string
			if change == "assignment" {
				config, createErr := assignmentService.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Canonical", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "account"}}})
				require.NoError(t, createErr)
				review, reviewErr := assignmentService.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID, DeviceID: board.DeviceID}})
				require.NoError(t, reviewErr)
				assignmentReviewID = review.ID
				device.observations = 0
			}
			authority := &changingBatchAuthority{Authority: f.pools, change: func() {
				switch change {
				case "assignment":
					_, err = assignmentService.ConfirmReview(ctx, f.account.AccountID, assignmentReviewID, "actor-password")
				case "stale inventory":
					_, err = f.db.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-interval '3 minutes' WHERE hashboard_id=$1`, board.HashboardID)
				case "removed installation":
					_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, board.InstallationID)
				case "MAC":
					_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata='{"macAddress":"replacement"}' WHERE id=$1`, board.InstallationID)
				case "ownership":
					_, err = f.db.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL WHERE kind=7 AND asset_id=$1`, board.HashboardID)
				case "permission":
					err = f.svc.ConfigureChild(ctx, f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false)
				}
				require.NoError(t, err)
			}}
			service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, authority))
			results, err := service.ReadDevicePools(ctx, f.account.AccountID, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetInstallation, ID: board.InstallationID}})
			require.NoError(t, err)
			require.Equal(t, "unavailable", results[0].State)
			require.Empty(t, results[0].Reported)
			require.Zero(t, device.observations)
		})
	}
}

func TestReadDevicePoolsBoundsControllersAndContinuesAfterTimeout(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	getter := batchMinerGetter{}
	var targets []poolconfigurations.AssignmentTarget
	var devices []*poolContainer
	identityStripes := []int32{}
	for i := range 5 {
		module, controller := f.miningModule(t)
		// The shared fixture gives every controller the same endpoint. Give these
		// independent controllers distinct identity stripes so admission locks do
		// not intentionally serialize their observations.
		var ip string
		var stripe int32
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT ip, stripe FROM (
   SELECT '192.0.2.' || n::text AS ip,
    (hashtextextended('org:' || $1::bigint::text || ':endpoint:local:192.0.2.' || n::text || ':8080',0) & 255)::integer AS stripe
   FROM generate_series(1,254) n
  ) candidates WHERE NOT stripe=ANY($2::integer[]) ORDER BY ip LIMIT 1`, f.org, identityStripes).Scan(&ip, &stripe))
		identityStripes = append(identityStripes, stripe)
		_, err = f.db.ExecContext(ctx, `UPDATE discovered_device SET ip_address=$2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, controller, ip)
		require.NoError(t, err)
		require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
		device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}, poolDevice: poolDevice{pools: []dto.MiningPool{{Username: "healthy.worker"}}}}
		if i < 4 {
			device.observe = func(ctx context.Context) (sdk.MiningGroupResult, error) {
				<-ctx.Done()
				return sdk.MiningGroupResult{}, ctx.Err()
			}
		}
		getter[controller] = device
		devices = append(devices, device)
		// Supplied routing cannot bypass the server's controller bound.
		targets = append(targets, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: 1})
	}
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools))
	_, err = service.ReadDevicePools(ctx, f.account.AccountID, targets)
	require.ErrorContains(t, err, "at most 4 controllers")
	for _, device := range devices {
		require.Zero(t, device.observations)
	}
	results, err := service.ReadDevicePools(ctx, f.account.AccountID, targets[:4])
	require.NoError(t, err)
	for i, result := range results {
		require.False(t, result.ReadbackAvailable)
		require.Equal(t, 1, devices[i].observations, "each controller must be attempted in this request")
	}
	results, err = service.ReadDevicePools(ctx, f.account.AccountID, targets[4:])
	require.NoError(t, err)
	require.True(t, results[0].ReadbackAvailable)
	require.Equal(t, "healthy.worker", results[0].Reported[0].Username)
	require.Equal(t, 1, devices[4].observations)
}

func TestReadDevicePoolsIsolatesAndLogsControllerFailures(t *testing.T) {
	for _, scenario := range []string{"error", "pool read error", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			getter := batchMinerGetter{}
			var targets []poolconfigurations.AssignmentTarget
			for i := range 2 {
				module, controller := f.miningModule(t)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
				device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}, poolDevice: poolDevice{pools: []dto.MiningPool{{Username: "healthy-worker"}}}}
				if i == 0 && scenario == "pool read error" {
					device.poolDevice.readError = errors.New("private-driver-secret")
				} else if i == 0 {
					device.observe = func(context.Context) (sdk.MiningGroupResult, error) {
						if scenario == "panic" {
							panic("private-driver-secret")
						}
						return sdk.MiningGroupResult{}, errors.New("private-driver-secret")
					}
				}
				getter[controller] = device
				targets = append(targets, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller})
			}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).WithChecker(poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools))
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			results, err := service.ReadDevicePools(ctx, f.account.AccountID, targets)
			require.NoError(t, err)
			require.Len(t, results, 2)
			require.Equal(t, "unavailable", results[0].State)
			require.False(t, results[0].ReadbackAvailable)
			require.Empty(t, results[0].Reported)
			require.True(t, results[1].ReadbackAvailable)
			require.Equal(t, "healthy-worker", results[1].Reported[0].Username)
			require.NotContains(t, logs.String(), "private-driver-secret")
			var entry map[string]any
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry))
			require.Equal(t, float64(targets[0].DeviceID), entry["device_id"])
			require.NotEmpty(t, entry["error_type"])
			// A panic must unwind the transaction and release the controller binding.
			tx, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `SELECT id FROM device WHERE id=$1 FOR UPDATE NOWAIT`, targets[0].DeviceID)
			require.NoError(t, err)
		})
	}
}
