package hostedaccounts_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolUpdateWaitsForOlderQueuedCommand(t *testing.T) {
	for _, phase := range []string{"review", "confirmation", "dispatch"} {
		for _, processing := range []bool{false, true} {
			status := sqlc.QueueStatusEnumPENDING
			if processing {
				status = sqlc.QueueStatusEnumPROCESSING
			}
			t.Run(phase+"/"+string(status), func(t *testing.T) {
				f := newPoolStepUpFixture(t)
				_, _, _, cipher, _ := poolFixture(t, f.fixture)
				ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
				require.NoError(t, err)
				miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
				reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
				repo := poolconfigurations.NewRepository(cipher)
				service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
				config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "New pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://new.example", Username: "new"}}})
				require.NoError(t, err)
				target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
				review := func() (poolconfigurations.Review, error) {
					return service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
				}
				confirm := func(id string) (poolconfigurations.ReviewedSave, error) {
					return service.ConfirmReview(ctx, f.account.AccountID, id, poolconfigurations.Confirmation{Password: "actor-password"})
				}
				var reviewed poolconfigurations.Review
				var saved poolconfigurations.ReviewedSave
				if phase != "review" {
					reviewed, err = review()
					require.NoError(t, err)
				}
				if phase == "dispatch" {
					saved, err = confirm(reviewed.ID)
					require.NoError(t, err)
				}

				// Keep the older request outside physical admission until the new flow runs.
				q := sqlc.New(f.db)
				messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, f.db)
				batch := uuid.NewString()
				kind := commandtype.UpdateMiningPools
				_, err = q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: f.host, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: f.org, Valid: true}})
				require.NoError(t, err)
				oldPayload := dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://old.example", Username: "old"}}
				require.NoError(t, messages.Enqueue(ctx, batch, kind, []int64{miner}, oldPayload))
				var queued queue.Message
				dequeue := func() {
					rows, err := messages.Dequeue(ctx, 1)
					require.NoError(t, err)
					require.Len(t, rows, 1)
					queued = rows[0]
				}
				if processing {
					dequeue()
				}
				var attempts int
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, miner).Scan(&attempts))
				require.Zero(t, attempts, "the old command has not reached physical admission")
				device := &poolDevice{}
				worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
				switch phase {
				case "review":
					_, err = review()
				case "confirmation":
					_, err = confirm(reviewed.ID)
				case "dispatch":
					require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
					operation, readErr := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
					require.NoError(t, readErr)
					require.Equal(t, "failed", operation.Targets[0].State)
					require.Equal(t, "interrupted", operation.Targets[0].ResultCode)
				}
				if phase != "dispatch" {
					require.True(t, fleeterror.IsFailedPreconditionError(err), "expected queued-command rejection, got %v", err)
					require.ErrorContains(t, err, "unfinished configuration command")
				}
				require.Zero(t, device.writes, "new pools must not be written ahead of the older command")
				queuedRows, err := q.GetQueueMessagesByBatch(ctx, batch)
				require.NoError(t, err)
				require.Len(t, queuedRows, 1)
				require.Equal(t, status, queuedRows[0].Status, "review must not cancel the existing request")

				// Let the old command finish, then freshly review and apply the new settings.
				if !processing {
					dequeue()
				}
				attempt, err := devicewrite.NewService(f.db).BeginQueuedHost(ctx, queued)
				require.NoError(t, err)
				require.NoError(t, devicewrite.Dispatch(attempt.Context(ctx), func(callCtx context.Context) error { return device.UpdateMiningPools(callCtx, oldPayload) }))
				require.NoError(t, attempt.Finish(ctx, nil))
				_, err = q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{ID: queued.ID, Status: sqlc.QueueStatusEnumSUCCESS})
				require.NoError(t, err)
				reviewed, err = review()
				require.NoError(t, err)
				saved, err = confirm(reviewed.ID)
				require.NoError(t, err)
				require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
				operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
				require.NoError(t, err)
				require.Equal(t, "verified", operation.Targets[0].State)
				require.Equal(t, "stratum+tcp://new.example", device.pools[0].URL)
				require.Equal(t, 2, device.writes)
			})
		}
	}
}

func TestPoolQueuedCommandConflictScope(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	q := sqlc.New(f.db)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, f.db)
	createBatch := func(kind commandtype.Type) string {
		batch := uuid.NewString()
		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: f.host, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: f.org, Valid: true}})
		require.NoError(t, err)
		return batch
	}
	review := func(q sqlc.Querier) error {
		_, err := reviewer.ReviewTargets(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}}, []poolconfigurations.Endpoint{{URL: "stratum+tcp://new.example", Username: "new"}})
		return err
	}
	check := func() error { return db.WithTransactionNoResult(ctx, f.db, review) }
	for _, scenario := range []struct {
		kind   commandtype.Type
		blocks bool
	}{
		{commandtype.BlinkLED, false}, {commandtype.DownloadLogs, false},
		{commandtype.SetCoolingMode, false}, {commandtype.SetPowerTarget, false},
		{commandtype.UpdateMiningPools, true}, {commandtype.StartMining, true}, {commandtype.StopMining, true},
		{commandtype.Reboot, true}, {commandtype.FirmwareUpdate, true}, {commandtype.Unpair, true},
		{commandtype.UpdateMinerPassword, true}, {commandtype.Curtail, true}, {commandtype.Uncurtail, true},
		{commandtype.ApplyCurtailmentConfig, true}, {commandtype.SetContainerControl, true}, {commandtype.ContainerAlarmAction, true},
		{commandtype.SetTankMining, true}, {commandtype.SetModuleMining, true}, {commandtype.Type(-1), true},
	} {
		t.Run(scenario.kind.String(), func(t *testing.T) {
			batch := createBatch(scenario.kind)
			require.NoError(t, messages.Enqueue(ctx, batch, scenario.kind, []int64{miner}, nil))
			rows, err := q.GetQueueMessagesByBatch(ctx, batch)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			for _, status := range []sqlc.QueueStatusEnum{sqlc.QueueStatusEnumPENDING, sqlc.QueueStatusEnumPROCESSING} {
				if status == sqlc.QueueStatusEnumPROCESSING {
					_, err = q.ClaimMessageForProcessing(ctx, rows[0].ID)
					require.NoError(t, err)
				}
				err = check()
				if scenario.blocks {
					require.True(t, fleeterror.IsFailedPreconditionError(err), "%s: expected conflict, got %v", status, err)
				} else {
					require.NoError(t, err, "unrelated queued work must not block pools")
				}
			}
			_, err = q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{ID: rows[0].ID, Status: sqlc.QueueStatusEnumSUCCESS})
			require.NoError(t, err)
			require.NoError(t, check(), "completed commands no longer block review")
		})
	}
	batch := createBatch(commandtype.UpdateMiningPools)
	require.NoError(t, messages.Enqueue(ctx, batch, commandtype.UpdateMiningPools, []int64{other}, nil))
	require.NoError(t, check(), "commands for another device must not block review")

	// Enqueue cannot commit between a successful readiness check and releasing
	// its device fence. After it commits, the next review sees the command.
	batch = createBatch(commandtype.UpdateMiningPools)
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, review(sqlc.New(tx)))
	var backend int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&backend))
	enqueueCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- messages.Enqueue(enqueueCtx, batch, commandtype.UpdateMiningPools, []int64{miner}, nil)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, backend).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	require.True(t, fleeterror.IsFailedPreconditionError(check()))
}
