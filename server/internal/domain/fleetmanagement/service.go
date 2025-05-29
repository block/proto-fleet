package fleetmanagement

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
)

type Service struct {
	conn      *sql.DB
	telemetry TelemetryCollector
}

func NewService(conn *sql.DB, t TelemetryCollector) *Service {
	return &Service{
		conn:      conn,
		telemetry: t,
	}
}

func (s *Service) ListPairedMiners(c context.Context, req *pb.ListPairedMinersRequest) (*pb.ListPairedMinersResponse, error) {
	// Validate and set page size
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50 // default page size
	}
	if pageSize > 1000 {
		pageSize = 1000 // maximum page size
	}

	// Decode cursor if provided
	cursor, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, err
	}

	// Prepare query parameters
	params := sqlc.ListPairedDevicesParams{
		CursorID:       sql.NullInt64{Int64: cursor.ID, Valid: cursor.ID > 0},
		DeviceCursorID: sql.NullInt64{Int64: cursor.DeviceID, Valid: cursor.DeviceID > 0},
		Limit:          pageSize + 1, // request one extra to determine if there are more pages
	}

	return db.WithTransaction(c, s.conn, func(q *sqlc.Queries) (*pb.ListPairedMinersResponse, error) {

		// Query the database
		devices, err := q.ListPairedDevices(c, params)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to list miners: %v", err)
		}

		// Prepare response
		resp := &pb.ListPairedMinersResponse{}

		// Handle pagination
		if len(devices) > int(pageSize) {
			// We got an extra record, so there are more pages
			resp.Miners = make([]*pb.PairedDevice, pageSize)
			for i, d := range devices[:pageSize] {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}

			// Create next page token from last visible item
			lastDevice := devices[pageSize-1]
			cursor = Cursor{
				ID:       lastDevice.CursorID,
				DeviceID: lastDevice.DeviceID,
			}
			resp.Cursor = encodeCursor(cursor)
		} else {
			// This is the last page
			resp.Miners = make([]*pb.PairedDevice, len(devices))
			for i, d := range devices {
				resp.Miners[i] = &pb.PairedDevice{
					DeviceIdentifier: d.DeviceIdentifier,
					MacAddress:       d.MacAddress,
					SerialNumber:     d.SerialNumber.String,
				}
			}
		}

		// Get total count
		total, err := q.GetTotalPairedDevices(c)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
		}
		resp.TotalMiners = int32(total) //nolint:gosec
		return resp, nil

	})
}

type Cursor struct {
	ID       int64
	DeviceID int64
}

func encodeCursor(c Cursor) string {
	raw := fmt.Sprintf("%d:%d", c.ID, c.DeviceID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fleeterror.NewErrorWithServiceCode(
			fmt.Sprintf("invalid page token, invalid cursor encoding: %v", err),
			connect.CodeInvalidArgument,
			int32(pb.FleetManagementServiceErrorCode_FLEET_MANAGEMENT_SERVICE_ERROR_CODE_INVALID_PAGINATION_CURSOR),
		)
	}

	var cursor Cursor
	_, err = fmt.Sscanf(string(b), "%d:%d", &cursor.ID, &cursor.DeviceID)
	if err != nil {
		return Cursor{}, fleeterror.NewErrorWithServiceCode(
			fmt.Sprintf("invalid page token, invalid cursor values: %v", err),
			connect.CodeInvalidArgument,
			int32(pb.FleetManagementServiceErrorCode_FLEET_MANAGEMENT_SERVICE_ERROR_CODE_INVALID_PAGINATION_CURSOR),
		)
	}

	return cursor, nil
}

// ListMinerStateSnapshots returns a paginated list of miners with their operational status and metrics
func (s *Service) ListMinerStateSnapshots(ctx context.Context, req *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error) {
	claims, err := tokenDomain.GetJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (*pb.ListMinerStateSnapshotsResponse, error) {
		// Get paired miners with their basic info
		miners, err := q.ListPairedMinersWithStatus(ctx, sqlc.ListPairedMinersWithStatusParams{
			OrgID: claims.OrgID,
			Limit: req.PageSize,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to list miners: %v", err)
		}

		// Convert to state snapshots
		var snapshots []*pb.MinerStateSnapshot
		for _, miner := range miners {
			// Get latest telemetry data for the miner
			telemetry, err := s.telemetry.GetMinerTelemetry(ctx, miner.DeviceIdentifier, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)
			if err != nil {
				slog.Error("failed to get telemetry for miner", "device_id", miner.DeviceIdentifier, "error", err)
				continue
			}

			// Get component status
			status, err := s.telemetry.GetMinerComponentStatus(ctx, miner.DeviceIdentifier)
			if err != nil {
				slog.Error("failed to get component status for miner", "device_id", miner.DeviceIdentifier, "error", err)
				continue
			}

			snapshot := &pb.MinerStateSnapshot{
				DeviceIdentifier: miner.DeviceIdentifier,
				Name:             miner.Model.String,
				MacAddress:       miner.MacAddress,
				SerialNumber:     miner.SerialNumber.String,
				IpAddress:        miner.IpAddress.String,
				PowerUsage:       telemetry.PowerUsage,
				Temperature:      telemetry.Temperature,
				Hashrate:         telemetry.Hashrate,
				Efficiency:       telemetry.Efficiency,
				Status:           status,
				Timestamp:        telemetry.Timestamp,
			}
			snapshots = append(snapshots, snapshot)
		}

		// Get total count
		total, err := q.GetTotalPairedDevices(ctx)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
		}

		// Handle case where no miners are returned
		var cursor string
		if len(miners) > 0 {
			cursor = miners[len(miners)-1].DeviceIdentifier // Use last device ID as cursor
		} else {
			cursor = "" // No miners, so cursor is empty
		}
		return &pb.ListMinerStateSnapshotsResponse{
			Miners:      snapshots,
			Cursor:      cursor,
			TotalMiners: int32(total), //nolint:gosec
		}, nil
	})
}

// StreamMinerUpdates streams real-time measurement updates for miners
func (s *Service) StreamMinerUpdates(ctx context.Context, req *pb.StreamMinerUpdatesRequest) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	_, err := tokenDomain.GetJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	responseChan := make(chan *pb.StreamMinerUpdatesResponse, 100)

	// Start measurement stream
	measurementChan, err := s.telemetry.StreamMeasurements(ctx, req.DeviceIdentifiers, req.MeasurementTypes)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to start measurement stream: %v", err)
	}

	// Start status stream if requested
	var statusChan <-chan *pb.StreamMinerUpdatesResponse
	if req.IncludeStatusUpdates {
		statusChan, err = s.telemetry.StreamComponentStatus(ctx, req.DeviceIdentifiers)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to start status stream: %v", err)
		}
	}

	// Start goroutine to handle all streams
	go func() {
		defer close(responseChan)

		// Create a ticker for heartbeats if requested
		var heartbeatTicker *time.Ticker
		if req.HeartbeatIntervalSeconds > 0 {
			heartbeatTicker = time.NewTicker(time.Duration(req.HeartbeatIntervalSeconds) * time.Second)
			defer heartbeatTicker.Stop()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case measurement := <-measurementChan:
				select {
				case <-ctx.Done():
					return
				case responseChan <- measurement:
				}
			case status := <-statusChan:
				select {
				case <-ctx.Done():
					return
				case responseChan <- status:
				}
			}
			// Include heartbeatTicker case only if it is initialized
			if heartbeatTicker != nil {
				select {
				case <-heartbeatTicker.C:
					resp := &pb.StreamMinerUpdatesResponse{
						Timestamp: timestamppb.Now(),
						Update: &pb.StreamMinerUpdatesResponse_Heartbeat{
							Heartbeat: &pb.Heartbeat{},
						},
					}
					select {
					case <-ctx.Done():
						return
					case responseChan <- resp:
					}
				default:
				}
			}
		}
	}()

	return responseChan, nil
}
