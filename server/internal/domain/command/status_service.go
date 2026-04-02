package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StatusService struct {
	conn         *sql.DB
	messageQueue queue.MessageQueue
}

func NewStatusService(conn *sql.DB, messageQueue queue.MessageQueue) *StatusService {
	return &StatusService{conn: conn, messageQueue: messageQueue}
}

// getIdsFromJsonArray extracts string IDs from a JSON array, filtering out null/empty values.
// This handles JSON_ARRAYAGG results which may include null values.
func getIdsFromJsonArray(jsonData any) []string {
	if jsonData == nil {
		return nil
	}

	jsonBytes, ok := jsonData.([]byte)
	if !ok {
		return nil
	}

	var ids []string
	if err := json.Unmarshal(jsonBytes, &ids); err != nil {
		return nil
	}

	// Filter out null/empty values from JSON_ARRAYAGG
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			filtered = append(filtered, id)
		}
	}

	return filtered
}

func getDeviceCount(row *sqlc.GetBatchStatusAndDeviceCountsRow) *pb.CommandBatchUpdateDeviceCount {
	deviceCount := &pb.CommandBatchUpdateDeviceCount{
		Total:   int64(row.DevicesCount),
		Success: row.SuccessfulDevices,
		Failure: row.FailedDevices,
	}

	// Parse success and failure device identifiers from JSON
	deviceCount.SuccessDeviceIdentifiers = getIdsFromJsonArray(row.SuccessDeviceIdentifiers)
	deviceCount.FailureDeviceIdentifiers = getIdsFromJsonArray(row.FailureDeviceIdentifiers)

	return deviceCount
}

func getStatus(sqlcStatus sqlc.BatchStatusEnum) pb.CommandBatchUpdateStatus_CommandBatchUpdateStatusType {
	switch sqlcStatus {
	case sqlc.BatchStatusEnumPENDING:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_PENDING
	case sqlc.BatchStatusEnumPROCESSING:
		return pb.CommandBatchUpdateStatus_COMMAND_BATCH_UPDATE_STATUS_TYPE_PROCESSING
	case sqlc.BatchStatusEnumFINISHED:
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

				if statusAndCount.Status == sqlc.BatchStatusEnumFINISHED {
					return
				}
			}
		}
	}()

	return channel, nil
}
