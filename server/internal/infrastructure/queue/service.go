package queue

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/sqlc-dev/pqtype"

	"github.com/proto-at-block/proto-fleet/server/generated/sqlc"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/commandtype"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/db"
)

type DatabaseMessageQueue struct {
	config *Config
	conn   *sql.DB
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
	return db.WithTransactionNoResult(ctx, d.conn, func(q *sqlc.Queries) error {
		for _, deviceID := range deviceIDs {
			err := q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: commandBatchLogUUID,
				CommandType:         commandType.String(),
				DeviceID:            deviceID,
				Status:              sqlc.QueueStatusEnumPENDING,
				RetryCount:          0,
				Payload:             pqtype.NullRawMessage{RawMessage: payloadBytes, Valid: true},
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("failed to enqueue message: %v", err)
			}
		}
		return nil
	})
}

func (d DatabaseMessageQueue) Dequeue(ctx context.Context) ([]Message, error) {
	messages, err := db.WithTransaction(ctx, d.conn, func(q *sqlc.Queries) ([]Message, error) {
		dbMessages, err := q.GetMessagesToProcess(ctx, sqlc.GetMessagesToProcessParams{
			RetryCount: d.config.MaxFailureRetries,
			Limit:      d.config.DequeLimit,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get messages to process: %v", err)
		}

		var messages []Message
		for _, dbMsg := range dbMessages {
			err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
				ID:     dbMsg.ID,
				Status: sqlc.QueueStatusEnumPROCESSING,
			})
			if err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to update message status: %v", err)
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
	return db.WithTransactionNoResult(ctx, d.conn, func(q *sqlc.Queries) error {
		err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
			ID:     messageID,
			Status: sqlc.QueueStatusEnumSUCCESS,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to mark message as a success: %v", err)
		}
		return nil
	})
}

func (d DatabaseMessageQueue) MarkFailed(ctx context.Context, messageID int64, errorInfo string) error {
	return db.WithTransactionNoResult(ctx, d.conn, func(q *sqlc.Queries) error {
		err := q.UpdateMessageAfterFailure(ctx, sqlc.UpdateMessageAfterFailureParams{
			ID:         messageID,
			RetryCount: d.config.MaxFailureRetries,
			ErrorInfo:  sql.NullString{String: errorInfo, Valid: true},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to mark message as failed; %v", err)
		}

		return nil
	})
}

func (d DatabaseMessageQueue) MarkPermanentlyFailed(ctx context.Context, messageID int64, errorInfo string) error {
	return db.WithTransactionNoResult(ctx, d.conn, func(q *sqlc.Queries) error {
		err := q.UpdateMessagePermanentlyFailed(ctx, sqlc.UpdateMessagePermanentlyFailedParams{
			ID:        messageID,
			ErrorInfo: sql.NullString{String: errorInfo, Valid: true},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to mark message as permanently failed: %v", err)
		}
		return nil
	})
}

type BatchStatusCheckFunc func(ctx context.Context, commandBatchLogID int64) (bool, error)

func (d DatabaseMessageQueue) IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	return db.WithTransaction(ctx, d.conn, func(q *sqlc.Queries) (bool, error) {
		return q.IsBatchFinished(ctx, commandBatchLogUUID)
	})
}

func (d DatabaseMessageQueue) IsBatchProcessing(ctx context.Context, commandBatchLogUUID string) (bool, error) {
	return db.WithTransaction(ctx, d.conn, func(q *sqlc.Queries) (bool, error) {
		return q.IsBatchProcessing(ctx, commandBatchLogUUID)
	})
}
