package command

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StatusService struct {
	conn         *sql.DB
	messageQueue queue.MessageQueue
}

func NewStatusService(conn *sql.DB, messageQueue queue.MessageQueue) *StatusService {
	return &StatusService{conn: conn, messageQueue: messageQueue}
}

func toInt64(value interface{}) int64 {
	if value == nil {
		return -1
	}

	switch v := value.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int16:
		return int64(v)
	case int8:
		return int64(v)
	case int:
		return int64(v)
	default:
		return -1
	}
}

func getDeviceCount(row *sqlc.GetBatchStatusAndDeviceCountsRow) *pb.CommandBatchUpdateDeviceCount {
	return &pb.CommandBatchUpdateDeviceCount{
		Total:   toInt64(row.DevicesCount),
		Success: toInt64(row.SuccessfulDevices),
		Failure: toInt64(row.FailedDevices),
	}
}

func getStatus(sqlcStatus sqlc.CommandBatchLogStatus) pb.CommandBatchUpdateStatus_CommandBatchUpdateStatusType {
	switch sqlcStatus {
	case sqlc.CommandBatchLogStatusPENDING:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_PENDING
	case sqlc.CommandBatchLogStatusPROCESSING:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_PROCESSING
	case sqlc.CommandBatchLogStatusFINISHED:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_FINISHED
	default:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_UNSPECIFIED
	}
}

func (ss *StatusService) StreamCommandBatchUpdates(ctx context.Context, batchIdentifier string) (<-chan *pb.StreamCommandBatchUpdatesResponse, error) {
	channel := make(chan *pb.StreamCommandBatchUpdatesResponse, 100)

	go func() {
		defer close(channel)

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				statusAndCount, err := db.WithTransaction[sqlc.GetBatchStatusAndDeviceCountsRow](ctx, ss.conn, func(q *sqlc.Queries) (sqlc.GetBatchStatusAndDeviceCountsRow, error) {
					return q.GetBatchStatusAndDeviceCounts(ctx, batchIdentifier)
				})
				if err != nil {
					slog.Error("error querying DB", "error", err)
					return
				}

				resp := &pb.StreamCommandBatchUpdatesResponse{
					Timestamp:              timestamppb.Now(),
					CommandBatchIdentifier: batchIdentifier,
					Status: &pb.CommandBatchUpdateStatus{
						CommandBatchUpdateStatus: getStatus(statusAndCount.Status),
						CommandBatchDeviceCount:  getDeviceCount(&statusAndCount),
					},
				}
				select {
				case <-ctx.Done():
					return
				case channel <- resp:
				}
			}
		}
	}()

	return channel, nil
}
