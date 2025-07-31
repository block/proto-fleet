package fleetmanagement

import (
	"context"
	"log/slog"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

type Service struct {
	deviceStore  interfaces.DeviceStore
	telemetry    TelemetryCollector
	minerService *miner.MinerService
}

func NewService(deviceStore interfaces.DeviceStore, t TelemetryCollector, minerService *miner.MinerService) *Service {
	return &Service{
		deviceStore:  deviceStore,
		telemetry:    t,
		minerService: minerService,
	}
}

func (s *Service) ListPairedMiners(c context.Context, req *pb.ListPairedMinersRequest) (*pb.ListPairedMinersResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(c)
	if err != nil {
		return nil, err
	}

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
	total, err := s.deviceStore.GetTotalPairedDevices(c, claims.OrgID, nil)
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

	filter, err := parseFilter(req.Filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse filter parameters: %v", err)
	}

	// Get paired miners with their basic info
	miners, nextCursor, err := s.deviceStore.ListPairedMinersWithStatus(ctx, claims.OrgID, req.Cursor, req.PageSize, filter)
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

		minerInfo, err := s.minerService.BuildMinerInfo(ctx, miner.DeviceIdentifier, claims.OrgID, miner.IpAddress, miner.Port, miner.UrlScheme, miner.Type)
		if err != nil {
			slog.Error("failed to get miner info", "device_id", miner.DeviceIdentifier, "error", err)
			continue
		}

		snapshot := &pb.MinerStateSnapshot{
			DeviceIdentifier: minerInfo.GetID().String(),
			Name:             miner.Model,
			MacAddress:       miner.MacAddress,
			SerialNumber:     miner.SerialNumber,
			IpAddress:        minerInfo.GetConnectionInfo().IPAddress.String(),
			Url:              minerInfo.GetWebViewURL().String(),
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
	total, err := s.deviceStore.GetTotalPairedDevices(ctx, claims.OrgID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	// Get state counts
	stateCounts, err := s.deviceStore.GetMinerStateCounts(ctx, claims.OrgID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
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

func parseFilter(pbFilter *pb.MinerListFilter) (*interfaces.MinerFilter, error) {
	if pbFilter == nil {
		return nil, nil
	}

	filter := &interfaces.MinerFilter{}

	if len(pbFilter.Status) > 0 {
		statusFilters := make([]string, 0, len(pbFilter.Status))
		for _, status := range pbFilter.Status {
			dbStatus, exists := componentStatusMap[status]
			if exists {
				statusFilters = append(statusFilters, dbStatus)
			} else {
				return nil, fleeterror.NewInternalErrorf("unsupported miner status: %v", status)
			}
		}
		filter.StatusFilter = statusFilters
	}

	var minerType mm.Type

	switch pbFilter.Type {
	case pb.MinerType_MINER_TYPE_PROTO_RIG:
		minerType = mm.TypeProto
	case pb.MinerType_MINER_TYPE_BITMAIN:
		minerType = mm.TypeAntminer
	case pb.MinerType_MINER_TYPE_UNSPECIFIED:
		return filter, nil
	default:
		return nil, fleeterror.NewInternalErrorf("unsupported miner type: %v", pbFilter.Type)
	}

	filter.MinerType = []mm.Type{minerType}

	return filter, nil
}

var componentStatusMap = map[pb.ComponentStatus]string{
	pb.ComponentStatus_COMPONENT_STATUS_OK:      "ONLINE",
	pb.ComponentStatus_COMPONENT_STATUS_WARNING: "MAINTENANCE",
	pb.ComponentStatus_COMPONENT_STATUS_ERROR:   "ERROR",
	pb.ComponentStatus_COMPONENT_STATUS_OFFLINE: "OFFLINE",
}
