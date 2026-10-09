package devicewrite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/stretchr/testify/require"
)

func curtailmentBatch(t *testing.T, conn *sql.DB, org int64, sourceBatch string, kind commandtype.Type, count int32) (context.Context, string) {
	t.Helper()
	var actor int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT created_by FROM command_batch_log WHERE uuid=$1`, sourceBatch).Scan(&actor))
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: actor, OrganizationID: org, Actor: session.ActorCurtailment})
	batch := id.GenerateID()
	_, err := sqlc.New(conn).CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: actor, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: count, OrganizationID: sql.NullInt64{Int64: org, Valid: true}})
	require.NoError(t, err)
	return ctx, batch
}

func TestCurtailmentBatchIncludesEveryOwnershipState(t *testing.T) {
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		t.Run(kind.String(), func(t *testing.T) {
			database, org, first, processing, messages := queuedFenceFixture(t)
			q := sqlc.New(database.DB)
			_, err := q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: processing.ID, Status: sqlc.QueueStatusEnumFAILED})
			require.NoError(t, err)
			ids := []int64{first}
			for range 3 {
				ids = append(ids, database.CreateDevice(org, "proto").DatabaseID)
			}
			require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), ids))
			states := []string{"host", "reserved", "active", "quarantined"}
			for i, device := range ids {
				_, err := database.DB.ExecContext(t.Context(), `UPDATE device_write_fence SET state=$1,epoch=$2 WHERE device_id=$3`, states[i], i, device)
				require.NoError(t, err)
			}
			ctx, batch := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, kind, 4)
			require.NoError(t, messages.Enqueue(ctx, batch, kind, ids, nil))
			rows, err := messages.Dequeue(ctx, 4)
			require.NoError(t, err)
			require.Len(t, rows, len(ids))
			service := devicewrite.NewService(database.DB)
			for _, row := range rows {
				attempt, err := service.BeginQueuedHost(ctx, row)
				require.NoError(t, err)
				require.NoError(t, attempt.Recheck(ctx))
				require.NoError(t, attempt.Finish(ctx, nil))
			}
			for i, device := range ids {
				fence, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: device, OrganizationID: org})
				require.NoError(t, err)
				require.Equal(t, states[i], fence.State)
				require.Equal(t, int64(i), fence.Epoch)
			}
			// A mixed fleet must still reject an ordinary configuration batch atomically.
			_, ordinary := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, commandtype.Reboot, 4)
			require.Error(t, messages.Enqueue(t.Context(), ordinary, commandtype.Reboot, ids, nil))
			ordinaryRows, err := q.GetQueueMessagesByBatch(ctx, ordinary)
			require.NoError(t, err)
			require.Empty(t, ordinaryRows)
		})
	}
}

func TestCurtailmentSurvivesPendingAndRunningHandoffs(t *testing.T) {
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		t.Run(kind.String(), func(t *testing.T) {
			database, org, device, processing, messages := queuedFenceFixture(t)
			q := sqlc.New(database.DB)
			ctx, batch := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, kind, 1)
			require.NoError(t, messages.Enqueue(ctx, batch, kind, []int64{device}, nil))
			_, ordinary := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, commandtype.Reboot, 1)
			require.NoError(t, messages.Enqueue(t.Context(), ordinary, commandtype.Reboot, []int64{device}, nil))
			service := devicewrite.NewService(database.DB)
			epoch, err := service.Reserve(ctx, org, device)
			require.NoError(t, err)
			pending, err := q.GetQueueMessagesByBatch(ctx, batch)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			require.Equal(t, sqlc.QueueStatusEnumPENDING, pending[0].Status)
			canceled, err := q.GetQueueMessagesByBatch(ctx, ordinary)
			require.NoError(t, err)
			require.Equal(t, sqlc.QueueStatusEnumFAILED, canceled[0].Status)
			_, err = q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{ID: processing.ID, Status: sqlc.QueueStatusEnumFAILED})
			require.NoError(t, err)
			rows, err := messages.Dequeue(ctx, 1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Zero(t, rows[0].WriteEpoch)
			attempt, err := service.BeginQueuedHost(ctx, rows[0])
			require.NoError(t, err)
			// This handle retains the queue epoch even as ownership advances again.
			_, err = database.DB.ExecContext(ctx, `UPDATE device_write_fence SET state='active',epoch=epoch+1 WHERE device_id=$1`, device)
			require.NoError(t, err)
			require.NoError(t, attempt.Recheck(ctx))
			called := false
			require.NoError(t, service.WithCurtailmentAttempt(attempt.Context(ctx), org, device, rows[0].WriteEpoch, kind, func(ctx context.Context) error {
				return devicewrite.Dispatch(ctx, func(context.Context) error { called = true; return nil })
			}))
			require.True(t, called)
			drained, err := service.Drained(ctx, org, device, epoch+1)
			require.NoError(t, err)
			require.False(t, drained)
			require.NoError(t, attempt.Finish(ctx, nil))
			drained, err = service.Drained(ctx, org, device, epoch+1)
			require.NoError(t, err)
			require.False(t, drained, "global curtailment acknowledgement still requires physical completion evidence")
			require.Error(t, service.ReleaseHost(ctx, org, device, epoch+1))
			require.Error(t, devicewrite.Dispatch(attempt.Context(ctx), func(context.Context) error { t.Fatal("completed authority reused"); return nil }))
		})
	}
}

func TestCurtailmentAttemptCannotAuthorizeOtherWrites(t *testing.T) {
	database, org, device, processing, messages := queuedFenceFixture(t)
	ctx, batch := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, commandtype.Curtail, 1)
	require.NoError(t, messages.Enqueue(ctx, batch, commandtype.Curtail, []int64{device}, nil))
	q := sqlc.New(database.DB)
	_, err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{ID: processing.ID, Status: sqlc.QueueStatusEnumFAILED})
	require.NoError(t, err)
	rows, err := messages.Dequeue(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	service := devicewrite.NewService(database.DB)
	forged := rows[0]
	forged.CommandType = commandtype.Uncurtail
	_, err = service.BeginQueuedHost(ctx, forged)
	require.Error(t, err)
	attempt, err := service.BeginQueuedHost(ctx, rows[0])
	require.NoError(t, err)
	carried := attempt.Context(ctx)
	never := func(context.Context) error { t.Fatal("mismatched authority reached callback"); return nil }
	require.Error(t, service.RunHost(carried, org, device, 0, never))
	require.Error(t, service.WithCurtailmentAttempt(carried, org, device, 0, commandtype.Uncurtail, never))
	require.Error(t, service.WithCurtailmentAttempt(carried, org, device, 0, commandtype.Reboot, never))
	require.Error(t, service.WithCurtailmentAttempt(carried, org+1, device, 0, commandtype.Curtail, never))
	other := database.CreateDevice(org, "proto")
	require.Error(t, service.WithCurtailmentAttempt(carried, org, other.DatabaseID, 0, commandtype.Curtail, never))
	otherDatabase, _, _, _, _ := queuedFenceFixture(t)
	require.Error(t, devicewrite.NewService(otherDatabase.DB).WithCurtailmentAttempt(carried, org, device, 0, commandtype.Curtail, never))
	_, err = database.DB.ExecContext(ctx, `UPDATE queue_message SET command_type='Uncurtail' WHERE id=$1`, rows[0].ID)
	require.NoError(t, err)
	require.Error(t, attempt.Recheck(ctx), "persisted command substitution must invalidate authority")
	require.NoError(t, attempt.Finish(ctx, nil))
}

func TestCurtailmentRetainsPhysicalIdentityAndUnknownOutcomeFences(t *testing.T) {
	for _, change := range []string{"serial", "MAC", "deleted", "unknown outcome"} {
		t.Run(change, func(t *testing.T) {
			database, org, device, _, _ := queuedFenceFixture(t)
			service := devicewrite.NewService(database.DB)
			epoch, err := service.Reserve(t.Context(), org, device)
			require.NoError(t, err)
			err = service.WithCurtailmentAttempt(t.Context(), org, device, 0, commandtype.Curtail, func(ctx context.Context) error {
				if change == "unknown outcome" {
					return devicewrite.Dispatch(ctx, func(context.Context) error { return errors.New("physical response unavailable") })
				}
				var mutation string
				switch change {
				case "serial":
					mutation = `UPDATE device SET serial_number='replacement-serial' WHERE id=$1`
				case "MAC":
					mutation = `UPDATE device SET mac_address='00-00-00-00-00-99' WHERE id=$1`
				case "deleted":
					mutation = `UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1`
				}
				_, err := database.DB.ExecContext(ctx, mutation, device)
				require.NoError(t, err)
				require.Error(t, devicewrite.Dispatch(ctx, func(context.Context) error { t.Fatal("changed identity reached device"); return nil }))
				return nil
			})
			if change == "unknown outcome" {
				require.Error(t, err)
				drained, err := service.Drained(t.Context(), org, device, epoch)
				require.NoError(t, err)
				require.False(t, drained)
				// A retry is allowed for global control, but cannot erase the earlier ambiguity.
				require.NoError(t, service.WithCurtailmentAttempt(t.Context(), org, device, 0, commandtype.Curtail, func(ctx context.Context) error {
					return devicewrite.Dispatch(ctx, func(context.Context) error { return nil })
				}))
				drained, err = service.Drained(t.Context(), org, device, epoch)
				require.NoError(t, err)
				require.False(t, drained)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeletionStillCancelsPendingGlobalCurtailment(t *testing.T) {
	database, org, device, processing, messages := queuedFenceFixture(t)
	q := sqlc.New(database.DB)
	_, err := q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: processing.ID, Status: sqlc.QueueStatusEnumFAILED})
	require.NoError(t, err)
	batches := []string{}
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		ctx, batch := curtailmentBatch(t, database.DB, org, processing.BatchLogUUID, kind, 1)
		require.NoError(t, messages.Enqueue(ctx, batch, kind, []int64{device}, nil))
		batches = append(batches, batch)
	}
	var identifier string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, device).Scan(&identifier))
	count, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{identifier}, org)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	for _, batch := range batches {
		rows, err := q.GetQueueMessagesByBatch(t.Context(), batch)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, sqlc.QueueStatusEnumFAILED, rows[0].Status)
	}
}

func TestCurtailmentRequiresCommittedAdmission(t *testing.T) {
	database, org, device, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	require.NoError(t, db.WithTransactionNoResult(t.Context(), database.DB, func(q sqlc.Querier) error {
		require.Error(t, service.WithCurtailmentAttempt(db.WithTxQueries(t.Context(), q), org, device, 0, commandtype.Curtail, func(context.Context) error {
			t.Fatal("device I/O began inside a transaction")
			return nil
		}))
		return nil
	}))
}
