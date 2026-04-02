package queue

import (
	"context"
	"errors"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

// ErrStale is returned when a MarkSuccess/MarkFailed/MarkPermanentlyFailed update
// finds 0 rows affected because the message is no longer in PROCESSING state (e.g., already reaped).
var ErrStale = errors.New("stale: message no longer PROCESSING")

type Message struct {
	ID           int64
	BatchLogUUID string
	CommandType  commandtype.Type
	DeviceID     int64
	Payload      []byte
}

//go:generate go run go.uber.org/mock/mockgen -source=interface.go -destination=mocks/mock_message_queue.go -package=mocks MessageQueue
type MessageQueue interface {
	// Enqueue adds a command to the queue
	Enqueue(ctx context.Context, commandBatchLogUUID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error

	// Dequeue retrieves and locks batch of commands for processing
	Dequeue(ctx context.Context) ([]Message, error)

	// MarkSuccess updates a command as successfully processed.
	// Returns ErrStale if the message is no longer PROCESSING.
	MarkSuccess(ctx context.Context, messageID int64) error

	// MarkFailed updates a command as failed with error info (may retry if under max retries).
	// Returns ErrStale if the message is no longer PROCESSING.
	MarkFailed(ctx context.Context, messageID int64, errorInfo string) error

	// MarkPermanentlyFailed marks a command as failed with no retries (for permanent errors like unsupported capabilities).
	// Returns ErrStale if the message is no longer PROCESSING.
	MarkPermanentlyFailed(ctx context.Context, messageID int64, errorInfo string) error

	IsBatchFinished(ctx context.Context, commandBatchLogUUID string) (bool, error)

	IsBatchProcessing(ctx context.Context, commandBatchLogUUID string) (bool, error)

	// MaxFailureRetries returns the configured maximum number of retry attempts
	// before a message is permanently marked FAILED.
	MaxFailureRetries() int32
}
