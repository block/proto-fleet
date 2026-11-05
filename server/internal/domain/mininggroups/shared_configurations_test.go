package mininggroups

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type sharedCapabilities struct{}

func (sharedCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true}
}

func TestSharedBoardConfigurationAssignmentAndEdit(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	var module, installation int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT module_id,id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0]).Scan(&module, &installation))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,position,occupancy,observed_at) VALUES($1,3,'empty',now())`, module)
	require.NoError(t, err)
	var secondBoard int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&secondBoard))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}')`, secondBoard, module)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,2,'occupied',now())`, module, secondBoard)
	require.NoError(t, err)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	f.service.miners = firmware
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
	f.service.WithPoolConfigurations(repo, reviewer)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	scope := poolconfigurations.Scope{OrganizationID: info.OrganizationID}
	password := "shared-private"
	config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://primary.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}, HashboardIDs: boards})
	require.NoError(t, err)
	require.Len(t, review.Targets, 2, "module plus its board must not duplicate targets")
	require.Equal(t, installation, review.Targets[0].ID)
	confirm := func(review poolconfigurations.Review) poolconfigurations.ReviewedSave {
		saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, info.UserID, review.ID, reviewer, nil)
		})
		require.NoError(t, err)
		return saved
	}
	apply := func(saved poolconfigurations.ReviewedSave) {
		require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
		for i := 0; i < 20 && active(f.state(t).Status); i++ {
			require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
		}
		require.Equal(t, "complete", f.state(t).Status)
		result, err := service.GetOperation(ctx, "", saved.Operation.ID)
		require.NoError(t, err)
		require.Equal(t, "verified", result.Targets[0].State, "readable pool settings match; firmware does not expose passwords")
	}
	saved := confirm(review)
	apply(saved)
	assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
	require.NoError(t, err)
	require.Len(t, assignments.Assignments, 2)
	require.Equal(t, config.ID, assignments.Assignments[0].ConfigurationID)
	edited, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "account", WorkerTemplate: "{miner_name}", Password: &password}}})
	require.NoError(t, err)
	require.Len(t, edited.Targets, 2)
	updated := confirm(edited)
	apply(updated)
	require.Equal(t, "stratum+tcp://updated.example:3333", firmware.snapshot.Groups[1].Pools[0].URL)
	// Reopening with a new service reads the persisted assignment and latest state.
	fresh := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	got, err := fresh.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	require.Equal(t, "verified", got.Assignments[0].State)
	var count int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_pool_revision`).Scan(&count))
	require.Equal(t, 1, count, "shared assignments must not create legacy configurations")
	// A group can acquire an untracked member through an out-of-band firmware
	// change. Reusing that group must not start the unknown board implicitly.
	reapply, err := service.ReviewBoards(ctx, "", config.ID, updated.Configuration.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	reapplied := confirm(reapply)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, reapplied.Operation.ID, f.target.DeviceID))
	firmware.snapshot.Groups[1].Mining = false
	firmware.snapshot.Groups[1].MACs = append(firmware.snapshot.Groups[1].MACs, "02:00:00:00:00:ff")
	before := len(firmware.calls)
	for range 3 {
		require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	}
	require.Equal(t, "blocked", f.state(t).Status)
	for _, call := range firmware.calls[before:] {
		require.Equal(t, "observe", call.Action, "an untracked affected board prevents group mutations")
	}

}

func TestSharedBoardApplicationRechecksPermissionAndInstallation(t *testing.T) {
	for _, phase := range []string{"before admission", "after admission", "during transfer", "board removed"} {
		t.Run(phase, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, boards := preparePermissionPoolAssignment(t, ctx, f)
			publishSharedBoardInventory(t, ctx, f)
			firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
			f.service.miners = firmware
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(f.service.secrets)
			reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
			f.service.WithPoolConfigurations(repo, reviewer)
			service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
			config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
			require.NoError(t, err)
			review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
			require.NoError(t, err)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
				return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer, nil)
			})
			require.NoError(t, err)
			if phase != "before admission" {
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			}
			if phase == "during transfer" {
				for i := 0; i < 20 && f.state(t).ControllerOperationID == ""; i++ {
					require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				}
				require.NotEmpty(t, f.state(t).ControllerOperationID)
			}
			if phase == "board removed" {
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE hashboard_id=$1`, boards[0])
				require.NoError(t, err)
				// Simulate the explicit inventory lifecycle action after this raw fixture update.
				require.NoError(t, sqlc.New(f.conn).RetirePoolConfigurationEquipmentAssignments(ctx, sqlc.RetirePoolConfigurationEquipmentAssignmentsParams{OrganizationID: info.OrganizationID, InstallationIds: []int64{saved.Operation.Targets[0].ID}}))
			} else {
				setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerStartMining, authz.PermMinerStopMining})
			}
			before := len(firmware.calls)
			if phase == "before admission" {
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			} else {
				for i := 0; i < 3 && active(f.state(t).Status); i++ {
					require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				}
			}
			for _, call := range firmware.calls[before:] {
				require.Contains(t, []string{"observe", "operation"}, call.Action, "only readback may continue after access or installation changes")
			}
			result, err := service.GetOperation(ctx, "", saved.Operation.ID)
			require.NoError(t, err)
			if phase == "board removed" {
				require.Equal(t, "superseded", result.Targets[0].State, "a removed installation retires its assignment")
			} else {
				require.Equal(t, "failed", result.Targets[0].State)
			}
			require.False(t, active(f.state(t).Status))
		})
	}
}

// Shared pool authorization uses the same complete board membership as account allocation.
func publishSharedBoardInventory(t *testing.T, ctx context.Context, f retryFixture) {
	t.Helper()
	for _, statement := range []string{
		`UPDATE container_module m SET last_seen_at=inventory.observed_at FROM container_tank tank,container_inventory inventory WHERE tank.id=m.tank_id AND inventory.device_id=tank.device_id AND tank.device_id=$1`,
		`INSERT INTO hashboard_slot(module_id,position,occupancy,observed_at) SELECT m.id,slots.position,'empty',m.last_seen_at FROM container_module m JOIN container_tank tank ON tank.id=m.tank_id CROSS JOIN generate_series(1,3) slots(position) WHERE tank.device_id=$1 ON CONFLICT (module_id,position) DO NOTHING`,
		`UPDATE hashboard_slot s SET observed_at=m.last_seen_at FROM container_module m,container_tank tank WHERE s.module_id=m.id AND tank.id=m.tank_id AND tank.device_id=$1`,
		`INSERT INTO hashboard_observation(hashboard_id,device_id,site_id,last_seen_at) SELECT i.hashboard_id,i.device_id,i.site_id,i.last_seen_at FROM hashboard_installation i WHERE i.device_id=$1 AND i.removed_at IS NULL ON CONFLICT (hashboard_id) DO UPDATE SET device_id=EXCLUDED.device_id,site_id=EXCLUDED.site_id,released_at=NULL,last_seen_at=EXCLUDED.last_seen_at`,
	} {
		_, err := f.conn.ExecContext(ctx, statement, f.target.DeviceID)
		require.NoError(t, err)
	}
}

func newConfirmedSharedBoardApplication(t *testing.T, ctx context.Context, f retryFixture) (*poolconfigurations.Service, poolconfigurations.ReviewedSave, *simulatedGroups) {
	t.Helper()
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	// The legacy fixture omits observation sites; publish inventory at the controller's current site.
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_installation i SET site_id=d.site_id FROM device d WHERE i.device_id=d.id AND d.id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_module m SET observation_site_id=d.site_id FROM container_tank tank,device d WHERE m.tank_id=tank.id AND tank.device_id=d.id AND d.id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	f.service.miners = firmware
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
	f.service.WithPoolConfigurations(repo, reviewer)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.NoError(t, err)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer, nil)
	})
	require.NoError(t, err)
	return service, saved, firmware
}

func TestSharedBoardApplicationWaitsForConflictingCommand(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	service, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	_, err = f.service.queries.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{Uuid: "shared-board-reboot", Type: "Reboot", CreatedBy: info.UserID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true}})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('shared-board-reboot',$1,'Reboot','PENDING',0)`, f.target.DeviceID)
	require.NoError(t, err)
	firmware.calls = nil
	for _, status := range []string{"PENDING", "PROCESSING"} {
		_, err = f.conn.ExecContext(ctx, `UPDATE queue_message SET status=$1 WHERE device_id=$2`, status, f.target.DeviceID)
		require.NoError(t, err)
		err = f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID)
		require.True(t, devicewrite.IsMutationConflict(err), "conflict should remain retryable: %v", err)
		operation, err := service.GetOperation(ctx, "", saved.Operation.ID)
		require.NoError(t, err)
		require.Len(t, operation.Targets, 1)
		require.Equal(t, "pending", operation.Targets[0].State)
		require.False(t, active(f.state(t).Status))
		require.Empty(t, groupActivities(t, f), "no workflow was admitted")
		var attempts int
		require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND command_type='MiningGroups'`, f.target.DeviceID).Scan(&attempts))
		require.Zero(t, attempts)
	}
	for _, call := range firmware.calls {
		require.Equal(t, "observe", call.Action, "contention must never dispatch a mutation")
	}
	_, err = f.conn.ExecContext(ctx, `UPDATE queue_message SET status='SUCCESS' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
	for i := 0; i < 20 && active(f.state(t).Status); i++ {
		f.reconcile(t, ctx)
	}
	operation, err := service.GetOperation(ctx, "", saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "verified", operation.Targets[0].State, "the same request completes after contention clears")
}

func TestSharedBoardApplicationActivityRetainsControllerSite(t *testing.T) {
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			q := f.service.queries
			site, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: f.target.OrganizationID, Name: "Shared board site", Slug: "shared-board-site"})
			require.NoError(t, err)
			_, err = f.conn.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, site.ID, f.target.DeviceID)
			require.NoError(t, err)
			_, saved, _ := newConfirmedSharedBoardApplication(t, ctx, f)
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			if outcome == "failed" {
				setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerStartMining, authz.PermMinerStopMining})
			}
			for i := 0; i < 20 && active(f.state(t).Status); i++ {
				f.reconcile(t, ctx)
			}
			events := groupActivities(t, f)
			require.Len(t, events, 2)
			require.Equal(t, "mining_groups.pools.accepted", events[0].Type)
			require.Equal(t, "mining_groups.pools."+outcome, events[1].Type)
			for _, event := range events {
				require.Equal(t, sql.NullInt64{Int64: site.ID, Valid: true}, event.SiteID)
			}
			filter := sqlc.ListActivityLogsParams{OrgID: sql.NullInt64{Int64: f.target.OrganizationID, Valid: true}, SiteIds: []int64{site.ID}, OrgLevelCategories: models.OrgLevelCategories(), PageSize: 100}
			visible, err := q.ListActivityLogs(ctx, filter)
			require.NoError(t, err)
			var workflowEvents int
			for _, event := range visible {
				if event.EventType == "mining_groups.pools.accepted" || event.EventType == "mining_groups.pools."+outcome {
					workflowEvents++
				}
			}
			require.Equal(t, 2, workflowEvents, "both workflow events appear in the controller's site feed")
			filter.SiteIds = []int64{site.ID + 1}
			visible, err = q.ListActivityLogs(ctx, filter)
			require.NoError(t, err)
			require.Empty(t, visible)
		})
	}
}
