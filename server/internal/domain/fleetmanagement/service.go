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

	// Get device statuses for all miners
	deviceIdentifiers := make([]mm.DeviceIdentifier, len(miners))
	for i, miner := range miners {
		deviceIdentifiers[i] = mm.DeviceIdentifier(miner.DeviceIdentifier)
	}

	deviceStatuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(ctx, deviceIdentifiers)
	if err != nil {
		slog.Error("failed to get device statuses", "error", err)
		deviceStatuses = make(map[mm.DeviceIdentifier]mm.MinerStatus) // Empty map as fallback
	}

	// Convert to state snapshots
	var snapshots []*pb.MinerStateSnapshot
	for _, miner := range miners {
		// Get latest telemetry data for the miner
		telemetry, err := s.telemetry.GetMinerTelemetry(ctx, miner.DeviceIdentifier, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)
		if err != nil {
			slog.Error("failed to get telemetry for miner", "device_id", miner.DeviceIdentifier, "error", err)
		}

		// Get component status
		status, err := s.telemetry.GetMinerComponentStatus(ctx, miner.DeviceIdentifier)
		if err != nil {
			slog.Error("failed to get component status for miner", "device_id", miner.DeviceIdentifier, "error", err)
		}

		minerInfo, err := s.minerService.BuildMinerInfo(ctx, miner.DeviceIdentifier, claims.OrgID, miner.IpAddress, miner.Port, miner.UrlScheme, miner.Type, miner.SerialNumber)
		if err != nil {
			slog.Error("failed to get miner info", "device_id", miner.DeviceIdentifier, "error", err)
		}

		// Get device status for this miner
		deviceID := mm.DeviceIdentifier(miner.DeviceIdentifier)
		minerStatus, ok := deviceStatuses[deviceID]
		if !ok {
			slog.Warn("device status not found for miner", "device_id", deviceID)
			minerStatus = mm.MinerStatusUnknown // Default to unknown if not found
		}
		deviceStatus := convertMinerStatusToDeviceStatus(minerStatus)

		snapshot := &pb.MinerStateSnapshot{
			Name:         miner.Manufacturer + " " + miner.Model,
			MacAddress:   miner.MacAddress,
			SerialNumber: miner.SerialNumber,
			DeviceStatus: deviceStatus,
		}

		if minerInfo != nil {
			snapshot.DeviceIdentifier = minerInfo.GetID().String()
			snapshot.IpAddress = minerInfo.GetConnectionInfo().IPAddress.String()
			snapshot.Url = minerInfo.GetWebViewURL().String()
		}

		if telemetry != nil {
			snapshot.PowerUsage = telemetry.PowerUsage
			snapshot.Temperature = telemetry.Temperature
			snapshot.Hashrate = telemetry.Hashrate
			snapshot.Efficiency = telemetry.Efficiency
			snapshot.Timestamp = telemetry.Timestamp
		}

		if status != nil {
			snapshot.Status = status
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

	// Get available miner types
	availableTypes, err := s.deviceStore.GetAvailableMinerTypes(ctx, claims.OrgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available miner types: %v", err)
	}

	// Convert miner types to proto enum
	pbMinerTypes := make([]pb.MinerType, 0, len(availableTypes))
	for _, minerType := range availableTypes {
		switch minerType {
		case mm.TypeProto:
			pbMinerTypes = append(pbMinerTypes, pb.MinerType_MINER_TYPE_PROTO_RIG)
		case mm.TypeAntminer:
			pbMinerTypes = append(pbMinerTypes, pb.MinerType_MINER_TYPE_BITMAIN)
		case mm.TypeUnknown, mm.TypeWhatsminer, mm.TypeAvalon:
			// Skip types that don't have corresponding proto enum values
		}
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
		MinerTypes:       pbMinerTypes,
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

	// Handle component_filters field
	if len(pbFilter.ComponentFilters) > 0 {
		componentFilters := make([]interfaces.ComponentFilter, 0, len(pbFilter.ComponentFilters))
		for _, cf := range pbFilter.ComponentFilters {
			componentType, err := convertComponentType(cf.Component)
			if err != nil {
				return nil, err
			}

			statuses := make([]string, 0, len(cf.Statuses))
			for _, status := range cf.Statuses {
				dbStatus, exists := componentStatusMap[status]
				if exists {
					statuses = append(statuses, dbStatus)
				} else {
					return nil, fleeterror.NewInternalErrorf("unsupported component status: %v", status)
				}
			}

			componentFilters = append(componentFilters, interfaces.ComponentFilter{
				ComponentType: componentType,
				Statuses:      statuses,
			})
		}
		filter.ComponentFilters = componentFilters
	}

	if len(pbFilter.DeviceStatus) > 0 {
		statusFilters := make([]mm.MinerStatus, 0, len(pbFilter.DeviceStatus))
		for _, status := range pbFilter.DeviceStatus {
			switch status {
			case pb.DeviceStatus_DEVICE_STATUS_ONLINE:
				statusFilters = append(statusFilters, mm.MinerStatusActive)
			case pb.DeviceStatus_DEVICE_STATUS_OFFLINE:
				statusFilters = append(statusFilters, mm.MinerStatusOffline)
			case pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
				statusFilters = append(statusFilters, mm.MinerStatusMaintenance)
			case pb.DeviceStatus_DEVICE_STATUS_ERROR:
				statusFilters = append(statusFilters, mm.MinerStatusError)
			case pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
				statusFilters = append(statusFilters, mm.MinerStatusUnknown)
			case pb.DeviceStatus_DEVICE_STATUS_INACTIVE:
				statusFilters = append(statusFilters, mm.MinerStatusInactive)
			default:
				return nil, fleeterror.NewInternalErrorf("unsupported miner status: %v", status)
			}
		}
		filter.DeviceStatusFilter = statusFilters
	}

	// Handle types filter
	if len(pbFilter.Types) > 0 {
		minerTypes := make([]mm.Type, 0, len(pbFilter.Types))
		for _, t := range pbFilter.Types {
			switch t {
			case pb.MinerType_MINER_TYPE_PROTO_RIG:
				minerTypes = append(minerTypes, mm.TypeProto)
			case pb.MinerType_MINER_TYPE_BITMAIN:
				minerTypes = append(minerTypes, mm.TypeAntminer)
			case pb.MinerType_MINER_TYPE_UNSPECIFIED:
				// Skip unspecified types
				continue
			default:
				return nil, fleeterror.NewInternalErrorf("unsupported miner type: %v", t)
			}
		}
		filter.MinerType = minerTypes
	}

	return filter, nil
}

func convertComponentType(ct pb.ComponentType) (string, error) {
	switch ct {
	case pb.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
		return "control_board", nil
	case pb.ComponentType_COMPONENT_TYPE_FANS:
		return "fans", nil
	case pb.ComponentType_COMPONENT_TYPE_HASH_BOARDS:
		return "hash_boards", nil
	case pb.ComponentType_COMPONENT_TYPE_PSU:
		return "psu", nil
	case pb.ComponentType_COMPONENT_TYPE_UNSPECIFIED:
		return "", fleeterror.NewInternalErrorf("component type must be specified")
	default:
		return "", fleeterror.NewInternalErrorf("unsupported component type: %v", ct)
	}
}

var componentStatusMap = map[pb.ComponentStatus]string{
	pb.ComponentStatus_COMPONENT_STATUS_OK:      "ONLINE",
	pb.ComponentStatus_COMPONENT_STATUS_WARNING: "MAINTENANCE",
	pb.ComponentStatus_COMPONENT_STATUS_ERROR:   "ERROR",
	pb.ComponentStatus_COMPONENT_STATUS_OFFLINE: "OFFLINE",
}

func convertMinerStatusToDeviceStatus(minerStatus mm.MinerStatus) pb.DeviceStatus {
	switch minerStatus {
	case mm.MinerStatusActive:
		return pb.DeviceStatus_DEVICE_STATUS_ONLINE
	case mm.MinerStatusOffline:
		return pb.DeviceStatus_DEVICE_STATUS_OFFLINE
	case mm.MinerStatusMaintenance:
		return pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE
	case mm.MinerStatusError:
		return pb.DeviceStatus_DEVICE_STATUS_ERROR
	case mm.MinerStatusInactive:
		return pb.DeviceStatus_DEVICE_STATUS_INACTIVE
	case mm.MinerStatusUnknown:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	default:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	}
}
