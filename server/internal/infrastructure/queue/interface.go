package queue

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

type Message struct {
	ID           int64
	BatchLogUUID string
	CommandType  commandtype.Type
	DeviceID     int64
	Payload      []byte
	RetryCount   int32
	OrgID        int64
}

type EnqueueMessage struct {
	DeviceID int64
	Payload  interface{}
}

//go:generate go run go.uber.org/mock/mockgen -source=interface.go -destination=mocks/mock_message_queue.go -package=mocks MessageQueue
type MessageQueue interface {
	// Enqueue adds a command to the queue
	Enqueue(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error

	// EnqueueMany adds commands with per-device payloads in one atomic operation.
	EnqueueMany(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, messages []EnqueueMessage) error

	// Dequeue retrieves and locks at most limit commands for processing.
	Dequeue(ctx context.Context, limit int32) ([]Message, error)

	IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error)

	// MaxFailureRetries returns the configured maximum number of retry attempts
	// before a message is permanently marked FAILED.
	MaxFailureRetries() int32
}
