package fleetmanagement

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

type Service struct {
	deviceStore interfaces.DeviceStore
	telemetry   TelemetryCollector
}

func NewService(deviceStore interfaces.DeviceStore, t TelemetryCollector) *Service {
	return &Service{
		deviceStore: deviceStore,
		telemetry:   t,
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

	// Query the database
	devices, nextCursor, err := s.deviceStore.ListPairedDevices(c, req.Cursor, pageSize)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list miners: %v", err)
	}

	// Get total count
	total, err := s.deviceStore.GetTotalPairedDevices(c)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	// Prepare response
	resp := &pb.ListPairedMinersResponse{
		Miners:      devices,
		Cursor:      nextCursor,
		TotalMiners: int32(total), //nolint:gosec
	}

	return resp, nil
}

// ListMinerStateSnapshots returns a paginated list of miners with their operational status and metrics
func (s *Service) ListMinerStateSnapshots(ctx context.Context, req *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Get paired miners with their basic info
	miners, nextCursor, err := s.deviceStore.ListPairedMinersWithStatus(ctx, claims.OrgID, req.Cursor, req.PageSize)
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
			Name:             miner.Model,
			MacAddress:       miner.MacAddress,
			SerialNumber:     miner.SerialNumber,
			IpAddress:        miner.IpAddress,
			// TODO(DASH-491) read url scheme from miner data once we start persisting
			Url:         fmt.Sprintf("http://%s", net.JoinHostPort(miner.IpAddress, miner.Port)),
			PowerUsage:  telemetry.PowerUsage,
			Temperature: telemetry.Temperature,
			Hashrate:    telemetry.Hashrate,
			Efficiency:  telemetry.Efficiency,
			Status:      status,
			Timestamp:   telemetry.Timestamp,
		}
		snapshots = append(snapshots, snapshot)
	}

	// Get total count
	total, err := s.deviceStore.GetTotalPairedDevices(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:      snapshots,
		Cursor:      nextCursor,
		TotalMiners: int32(total), //nolint:gosec
	}, nil

}

// StreamMinerUpdates streams real-time measurement updates for miners
func (s *Service) StreamMinerUpdates(ctx context.Context, req *pb.StreamMinerUpdatesRequest) (<-chan *pb.StreamMinerUpdatesResponse, error) {
	_, err := tokenDomain.GetClientAuthJWTClaims(ctx)
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
