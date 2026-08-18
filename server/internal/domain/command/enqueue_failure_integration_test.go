package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type failingEnqueueQueue struct {
	err          error
	delegate     queue.MessageQueue
	batchUUID    *string
	statusChecks chan string
}

func (q failingEnqueueQueue) Enqueue(ctx context.Context, batchUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error {
	if q.batchUUID != nil {
		*q.batchUUID = batchUUID
	}
	if q.delegate != nil {
		if err := q.delegate.Enqueue(ctx, batchUUID, commandType, deviceIDs, payload); err != nil {
			return err
		}
	}
	return q.err
}

func (q failingEnqueueQueue) EnqueueMany(ctx context.Context, batchUUID string, commandType commandtype.Type, messages []queue.EnqueueMessage) error {
	if q.batchUUID != nil {
		*q.batchUUID = batchUUID
	}
	if q.delegate != nil {
		if err := q.delegate.EnqueueMany(ctx, batchUUID, commandType, messages); err != nil {
			return err
		}
	}
	return q.err
}

func (q failingEnqueueQueue) EnqueueCommandBatch(ctx context.Context, batch queue.CommandBatch) error {
	if q.batchUUID != nil {
		*q.batchUUID = batch.Identifier
	}
	if q.delegate != nil {
		if err := q.delegate.EnqueueCommandBatch(ctx, batch); err != nil {
			return err
		}
	}
	return q.err
}

func (failingEnqueueQueue) Dequeue(context.Context, int32) ([]queue.Message, error) {
	return nil, nil
}

func (q failingEnqueueQueue) IsBatchFinished(_ context.Context, batchUUID string) (bool, error) {
	if q.statusChecks != nil {
		select {
		case q.statusChecks <- batchUUID:
		default:
		}
	}
	return false, nil
}

func (failingEnqueueQueue) MaxFailureRetries() int32 {
	return 0
}

func TestCommandEnqueueFailureFinishesCreatedBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn, dbService, user := setupRetentionTest(t)
	device := dbService.CreateDevice(user.OrganizationID, "proto")
	enqueueErr := errors.New("queue unavailable")
	var batchUUID string
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{err: enqueueErr, batchUUID: &batchUUID})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)

	// Act
	result, err := svc.BlinkLED(ctx, &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{device.ID}},
		},
	})

	// Assert
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, enqueueErr.Error())
	queries := sqlc.New(conn)
	batch, err := queries.GetBatchLog(t.Context(), batchUUID)
	require.NoError(t, err)
	assert.Equal(t, sqlc.BatchStatusEnumFINISHED, batch.Status)
	messages, err := queries.GetQueueMessagesByBatch(t.Context(), batchUUID)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestCommandEnqueueCommittedBeforeErrorReturnsSuccessAndTracksBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	conn, dbService, user := setupRetentionTest(t)
	device := dbService.CreateDevice(user.OrganizationID, "proto")
	statusChecks := make(chan string, 1)
	messageQueue := failingEnqueueQueue{
		err: errors.New("connection lost after commit"),
		delegate: queue.NewDatabaseMessageQueue(&queue.Config{
			MaxFailureRetries: 5,
		}, conn),
		statusChecks: statusChecks,
	}
	svc := newDispatchIntegrationTestService(t, conn, messageQueue)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)

	// Act
	result, err := svc.BlinkLED(ctx, &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{device.ID}},
		},
	})

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	messages, err := sqlc.New(conn).GetQueueMessagesByBatch(t.Context(), result.BatchIdentifier)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	select {
	case trackedBatch := <-statusChecks:
		assert.Equal(t, result.BatchIdentifier, trackedBatch)
	case <-time.After(time.Second):
		t.Fatal("batch status tracking did not start")
	}
}
