package queue

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
)

type DatabaseMessageQueue struct {
	config *Config
	conn   *sql.DB
}

type encodedMessage struct {
	deviceID int64
	payload  []byte
}

var _ MessageQueue = DatabaseMessageQueue{}

func NewDatabaseMessageQueue(config *Config, conn *sql.DB) *DatabaseMessageQueue {
	return &DatabaseMessageQueue{
		config: config,
		conn:   conn,
	}
}

func (d DatabaseMessageQueue) Enqueue(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to marshal payload: %v", err)
	}
	messages := make([]encodedMessage, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		messages = append(messages, encodedMessage{deviceID: deviceID, payload: payloadBytes})
	}
	return d.enqueueEncoded(ctx, commandBatchLogUUID, commandType, messages)
}

func (d DatabaseMessageQueue) EnqueueMany(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, messages []EnqueueMessage) error {
	encoded := make([]encodedMessage, 0, len(messages))
	for _, message := range messages {
		payloadBytes, err := json.Marshal(message.Payload)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to marshal payload: %v", err)
		}
		encoded = append(encoded, encodedMessage{deviceID: message.DeviceID, payload: payloadBytes})
	}
	return d.enqueueEncoded(ctx, commandBatchLogUUID, commandType, encoded)
}

func (d DatabaseMessageQueue) enqueueEncoded(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, messages []encodedMessage) error {
	deviceIDs := make([]int64, len(messages))
	payloads := make([]string, len(messages))
	for i, message := range messages {
		deviceIDs[i] = message.deviceID
		payloads[i] = string(message.payload)
	}
	return db.WithTransactionTimeoutNoResult(ctx, d.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		batchStatus, err := q.LockCommandBatch(ctx, commandBatchLogUUID)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to lock command batch: %v", err)
		}
		if batchStatus != sqlc.BatchStatusEnumPENDING {
			return fleeterror.NewInternalErrorf("cannot enqueue messages for command batch in %s status", batchStatus)
		}

		err = q.CreateQueueMessages(ctx, sqlc.CreateQueueMessagesParams{
			CommandBatchLogUuid: commandBatchLogUUID,
			CommandType:         commandType.String(),
			DeviceIds:           deviceIDs,
			Payloads:            payloads,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to enqueue messages: %v", err)
		}
		return nil
	})
}

func (d DatabaseMessageQueue) Dequeue(ctx context.Context, limit int32) ([]Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	if d.config.DequeLimit > 0 {
		limit = min(limit, d.config.DequeLimit)
	}
	messages, err := db.WithTransactionTimeout(ctx, d.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) ([]Message, error) {
		dbMessages, err := q.GetMessagesToProcess(ctx, sqlc.GetMessagesToProcessParams{
			RetryCount: d.config.MaxFailureRetries,
			Limit:      limit,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get messages to process: %v", err)
		}

		var messages []Message
		for _, dbMsg := range dbMessages {
			result, err := q.ClaimMessageForProcessing(ctx, dbMsg.ID)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to claim message for processing: %v", err)
			}
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				continue // already claimed or no longer PENDING
			}

			cmdType, err := commandtype.FromString(dbMsg.CommandType)
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("invalid command type: %v", err)
			}

			messages = append(messages, Message{
				ID:           dbMsg.ID,
				BatchLogUUID: dbMsg.CommandBatchLogUuid,
				CommandType:  cmdType,
				DeviceID:     dbMsg.DeviceID,
				Payload:      dbMsg.Payload.RawMessage,
				RetryCount:   dbMsg.RetryCount,
				OrgID:        dbMsg.OrgID,
			})
		}

		return messages, nil
	})

	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (d DatabaseMessageQueue) IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	return db.WithTransaction(ctx, d.conn, func(q sqlc.Querier) (bool, error) {
		return q.IsBatchFinished(ctx, commandBatchLogUUID)
	})
}

func (d DatabaseMessageQueue) MaxFailureRetries() int32 {
	return d.config.MaxFailureRetries
}
