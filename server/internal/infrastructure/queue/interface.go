package queue

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/commandtype"
)

type Message struct {
	ID           int64
	BatchLogUUID string
	CommandType  commandtype.Type
	DeviceID     int64
	Payload      []byte
}

//go:generate mockgen -source=interface.go -destination=mocks/mock_message_queue.go -package=mocks MessageQueue
type MessageQueue interface {
	// Enqueue adds a command to the queue
	Enqueue(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error

	// Dequeue retrieves and locks batch of commands for processing
	Dequeue(ctx context.Context) ([]Message, error)

	// MarkSuccess updates a command as successfully processed
	MarkSuccess(ctx context.Context, messageID int64) error

	// MarkFailed updates a command as failed with error info
	MarkFailed(ctx context.Context, messageID int64, errorInfo string) error

	IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error)

	IsBatchProcessing(ctx context.Context, commandBatchLogUUID string) (bool, error)
}
