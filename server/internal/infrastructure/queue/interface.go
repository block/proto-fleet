package queue

import (
	"context"
	"errors"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

// ErrStale is returned when a terminal update finds that a message is no
// longer in PROCESSING state, for example because it was already reaped.
var ErrStale = errors.New("stale: message no longer PROCESSING")

type Message struct {
	ID           int64
	BatchLogUUID string
	CommandType  commandtype.Type
	DeviceID     int64
	Payload      []byte
	RetryCount   int32
	MaxAttempts  int32
	OrgID        int64
}

type EnqueueMessage struct {
	DeviceID int64
	Payload  interface{}
}

// CommandBatch describes a command batch and its queue messages that must
// become durable in one transaction.
type CommandBatch struct {
	Identifier  string
	CommandType commandtype.Type
	CreatedBy   int64
	OrgID       int64
	LogPayload  []byte
	Messages    []EnqueueMessage
	MaxAttempts int32
}

//go:generate go run go.uber.org/mock/mockgen -source=interface.go -destination=mocks/mock_message_queue.go -package=mocks MessageQueue
type MessageQueue interface {
	// Enqueue adds a command to the queue
	Enqueue(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error

	// EnqueueMany adds commands with per-device payloads in one atomic operation.
	EnqueueMany(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, messages []EnqueueMessage) error

	// EnqueueCommandBatch creates the command log and queue messages in one
	// transaction. On error, neither side is durable.
	EnqueueCommandBatch(ctx context.Context, batch CommandBatch) error

	// Dequeue retrieves and locks at most limit commands for processing.
	Dequeue(ctx context.Context, limit int32) ([]Message, error)

	IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error)

	// MaxFailureRetries returns the configured maximum number of retry attempts
	// before a message is permanently marked FAILED.
	MaxFailureRetries() int32
}
