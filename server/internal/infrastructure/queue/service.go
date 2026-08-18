package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlc-dev/pqtype"

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
	return d.enqueueEncoded(ctx, commandBatchLogUUID, commandType, messages, d.config.MaxFailureRetries)
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
	return d.enqueueEncoded(ctx, commandBatchLogUUID, commandType, encoded, d.config.MaxFailureRetries)
}

func (d DatabaseMessageQueue) EnqueueCommandBatch(ctx context.Context, batch CommandBatch) error {
	maxAttempts, err := d.resolveMaxAttempts(batch.MaxAttempts)
	if err != nil {
		return err
	}
	encoded := make([]encodedMessage, 0, len(batch.Messages))
	for _, message := range batch.Messages {
		payloadBytes, err := json.Marshal(message.Payload)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to marshal payload: %v", err)
		}
		encoded = append(encoded, encodedMessage{deviceID: message.DeviceID, payload: payloadBytes})
	}
	return db.WithTransactionTimeoutNoResult(ctx, d.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:           batch.Identifier,
			Type:           batch.CommandType.String(),
			CreatedBy:      batch.CreatedBy,
			CreatedAt:      time.Now(),
			Status:         sqlc.BatchStatusEnumPENDING,
			DevicesCount:   int32(len(batch.Messages)), //nolint:gosec // bounded by fleet size
			Payload:        pqtype.NullRawMessage{RawMessage: batch.LogPayload, Valid: len(batch.LogPayload) > 0},
			OrganizationID: sql.NullInt64{Int64: batch.OrgID, Valid: true},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create command batch: %v", err)
		}
		return createQueueMessages(ctx, q, batch.Identifier, batch.CommandType, encoded, maxAttempts)
	})
}

func (d DatabaseMessageQueue) enqueueEncoded(
	ctx context.Context,
	commandBatchLogUUID string,
	commandType commandtype.Type,
	messages []encodedMessage,
	maxAttempts int32,
) error {
	resolvedMaxAttempts, err := d.resolveMaxAttempts(maxAttempts)
	if err != nil {
		return err
	}
	return db.WithTransactionTimeoutNoResult(ctx, d.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		batchStatus, err := q.LockCommandBatch(ctx, commandBatchLogUUID)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to lock command batch: %v", err)
		}
		if batchStatus != sqlc.BatchStatusEnumPENDING {
			return fleeterror.NewInternalErrorf("cannot enqueue messages for command batch in %s status", batchStatus)
		}
		return createQueueMessages(ctx, q, commandBatchLogUUID, commandType, messages, resolvedMaxAttempts)
	})
}

// resolveMaxAttempts maps an explicit per-batch ceiling to a positive value.
// Zero means inherit the queue service configured default (MaxFailureRetries).
func (d DatabaseMessageQueue) resolveMaxAttempts(explicit int32) (int32, error) {
	if explicit > 0 {
		return explicit, nil
	}
	if d.config.MaxFailureRetries > 0 {
		return d.config.MaxFailureRetries, nil
	}
	return 0, fleeterror.NewInternalError("queue max attempts must be greater than zero")
}

func createQueueMessages(
	ctx context.Context,
	q sqlc.Querier,
	commandBatchLogUUID string,
	commandType commandtype.Type,
	messages []encodedMessage,
	maxAttempts int32,
) error {
	if maxAttempts <= 0 {
		return fleeterror.NewInternalError("queue max attempts must be greater than zero")
	}
	for _, message := range messages {
		err := q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
			CommandBatchLogUuid: commandBatchLogUUID,
			CommandType:         commandType.String(),
			DeviceID:            message.deviceID,
			Status:              sqlc.QueueStatusEnumPENDING,
			RetryCount:          0,
			MaxAttempts:         maxAttempts,
			Payload:             pqtype.NullRawMessage{RawMessage: message.payload, Valid: true},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to enqueue message: %v", err)
		}
	}
	return nil
}

func (d DatabaseMessageQueue) Dequeue(ctx context.Context, limit int32) ([]Message, error) {
	if limit <= 0 {
		return nil, nil
	}
	if d.config.DequeLimit > 0 {
		limit = min(limit, d.config.DequeLimit)
	}
	messages, err := db.WithTransactionTimeout(ctx, d.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) ([]Message, error) {
		dbMessages, err := q.GetMessagesToProcess(ctx, limit)
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
				MaxAttempts:  dbMsg.MaxAttempts,
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

func (d DatabaseMessageQueue) MarkSuccess(ctx context.Context, messageID int64) error {
	updated, err := db.WithTransaction(ctx, d.conn, func(q sqlc.Querier) (bool, error) {
		result, err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
			ID:     messageID,
			Status: sqlc.QueueStatusEnumSUCCESS,
		})
		if err != nil {
			return false, fleeterror.NewInternalErrorf("failed to mark message as a success: %v", err)
		}
		rowsAffected, _ := result.RowsAffected()
		return rowsAffected > 0, nil
	})
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("message %d: %w", messageID, ErrStale)
	}
	return nil
}

func (d DatabaseMessageQueue) MarkFailed(ctx context.Context, messageID int64, errorInfo string) error {
	updated, err := db.WithTransaction(ctx, d.conn, func(q sqlc.Querier) (bool, error) {
		result, err := q.UpdateMessageAfterFailure(ctx, sqlc.UpdateMessageAfterFailureParams{
			ID:        messageID,
			ErrorInfo: sql.NullString{String: errorInfo, Valid: true},
		})
		if err != nil {
			return false, fleeterror.NewInternalErrorf("failed to mark message as failed: %v", err)
		}
		rowsAffected, _ := result.RowsAffected()
		return rowsAffected > 0, nil
	})
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("message %d: %w", messageID, ErrStale)
	}
	return nil
}

func (d DatabaseMessageQueue) MarkPermanentlyFailed(ctx context.Context, messageID int64, errorInfo string) error {
	updated, err := db.WithTransaction(ctx, d.conn, func(q sqlc.Querier) (bool, error) {
		result, err := q.UpdateMessagePermanentlyFailed(ctx, sqlc.UpdateMessagePermanentlyFailedParams{
			ID:        messageID,
			ErrorInfo: sql.NullString{String: errorInfo, Valid: true},
		})
		if err != nil {
			return false, fleeterror.NewInternalErrorf("failed to mark message as permanently failed: %v", err)
		}
		rowsAffected, _ := result.RowsAffected()
		return rowsAffected > 0, nil
	})
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("message %d: %w", messageID, ErrStale)
	}
	return nil
}

type BatchStatusCheckFunc func(ctx context.Context, commandBatchLogID int64) (bool, error)

func (d DatabaseMessageQueue) IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	return db.WithTransaction(ctx, d.conn, func(q sqlc.Querier) (bool, error) {
		return q.IsBatchFinished(ctx, commandBatchLogUUID)
	})
}

func (d DatabaseMessageQueue) MaxFailureRetries() int32 {
	return d.config.MaxFailureRetries
}
