package fleetmanagement

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	minerInterfaces "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	telemetrypb "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

const (
	// defaultPageSize is the default number of items returned per page when not specified
	defaultPageSize = 50
	// maxPageSize is the maximum number of items that can be returned per page
	maxPageSize = 1000
)

// constructWebViewURL builds a web view URL
//
// Note: The port is intentionally omitted from the URL for display purposes, as web browsers
// will use the default port for the scheme (80 for http, 443 for https). This matches the
// behavior of GetWebViewURL().
func constructWebViewURL(scheme, ipAddress string) string {
	if ipAddress == "" || scheme == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, ipAddress)
}

type Service struct {
	deviceStore           interfaces.DeviceStore
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	telemetry             TelemetryCollector
	minerService          *miner.MinerService
}

func NewService(deviceStore interfaces.DeviceStore, discoveredDeviceStore interfaces.DiscoveredDeviceStore, t TelemetryCollector, minerService *miner.MinerService) *Service {
	return &Service{
		deviceStore:           deviceStore,
		discoveredDeviceStore: discoveredDeviceStore,
		telemetry:             t,
		minerService:          minerService,
	}
}

// validatePageSize validates and normalizes the requested page size
func validatePageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return defaultPageSize
	}
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

func (s *Service) ListPairedMiners(c context.Context, req *pb.ListPairedMinersRequest) (*pb.ListPairedMinersResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(c)
	if err != nil {
		return nil, err
	}

	// Validate and set page size
	pageSize := validatePageSize(req.PageSize)

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

	// Convert to internal request format and delegate to shared builder
	return s.buildSnapshotFromListRequest(ctx, claims.OrgID, req)
}

// buildSnapshotFromListRequest builds response from ListMinerStateSnapshotsRequest
func (s *Service) buildSnapshotFromListRequest(ctx context.Context, orgID int64, req *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error) {
	return s.buildSnapshot(ctx, orgID, req.PageSize, req.Cursor, req.Filter, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)
}

// buildSnapshot builds a ListMinerStateSnapshotsResponse with the given parameters
// This is the shared implementation used by ListMinerStateSnapshots and StreamMinerListUpdates
func (s *Service) buildSnapshot(
	ctx context.Context,
	orgID int64,
	pageSize int32,
	cursor string,
	filterProto *pb.MinerListFilter,
	dataMode pb.DataMode,
	timeSeriesConfig *commonpb.TimeSeriesConfig,
	measurementConfigs []*pb.MeasurementConfig,
) (*pb.ListMinerStateSnapshotsResponse, error) {
	filter, err := parseFilter(filterProto)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse filter parameters: %v", err)
	}

	// Validate and normalize page size
	pageSize = validatePageSize(pageSize)

	// Use unified query that handles all pairing statuses
	snapshots, nextCursor, total, err := s.buildSnapshotsFromUnifiedQuery(ctx, orgID, cursor, pageSize, filter, dataMode, timeSeriesConfig, measurementConfigs)
	if err != nil {
		return nil, err
	}

	// Get state counts (only relevant for paired devices or when filter includes paired)
	var stateCounts *telemetrypb.MinerStateCounts
	includePaired := len(filter.PairingStatuses) == 0 // Empty means all, which includes paired
	for _, status := range filter.PairingStatuses {
		if status == pb.PairingStatus_PAIRING_STATUS_PAIRED || status == pb.PairingStatus_PAIRING_STATUS_UNSPECIFIED {
			includePaired = true
			break
		}
	}
	if includePaired {
		stateCounts, err = s.deviceStore.GetMinerStateCounts(ctx, orgID, filter)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
		}
	}

	// Get available miner types
	availableTypes, err := s.deviceStore.GetAvailableMinerTypes(ctx, orgID)
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

// buildSnapshotsFromUnifiedQuery uses the unified SQL query to fetch both paired and unpaired devices
func (s *Service) buildSnapshotsFromUnifiedQuery(
	ctx context.Context,
	orgID int64,
	cursor string,
	pageSize int32,
	filter *interfaces.MinerFilter,
	dataMode pb.DataMode,
	timeSeriesConfig *commonpb.TimeSeriesConfig,
	measurementConfigs []*pb.MeasurementConfig,
) ([]*pb.MinerStateSnapshot, string, int64, error) {
	// Call device store to get unified results
	rows, nextCursor, total, err := s.deviceStore.ListMinerStateSnapshots(ctx, orgID, cursor, pageSize, filter)
	if err != nil {
		return nil, "", 0, err
	}

	// Convert to snapshots
	snapshots := make([]*pb.MinerStateSnapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := &pb.MinerStateSnapshot{
			DeviceIdentifier: row.DeviceIdentifier,
			Type:             row.Type,
		}

		// Handle nullable fields
		if row.Model.Valid {
			snapshot.Model = row.Model.String
		}
		if row.Manufacturer.Valid {
			snapshot.Manufacturer = row.Manufacturer.String
		}

		// Map pairing status from database to protobuf enum
		switch row.PairingStatus {
		case "PAIRED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_PAIRED
		case "AUTHENTICATION_NEEDED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED
		case "PENDING":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_PENDING
		case "FAILED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_FAILED
		case "UNPAIRED":
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_UNPAIRED
		default:
			// Default to unpaired for unknown statuses
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_UNPAIRED
		}

		// Determine if device is paired (for telemetry data fetching)
		isPaired := row.PairingStatus == "PAIRED"

		// For paired devices, fetch telemetry and additional data
		if isPaired {
			snapshot.MacAddress = row.MacAddress
			if row.SerialNumber.Valid {
				snapshot.SerialNumber = row.SerialNumber.String
			}
			snapshot.Name = snapshot.Manufacturer + " " + snapshot.Model

			// Get telemetry data
			telemetry, err := s.telemetry.GetMinerTelemetry(ctx, row.DeviceIdentifier, dataMode, timeSeriesConfig, measurementConfigs)
			if err == nil && telemetry != nil {
				snapshot.PowerUsage = telemetry.PowerUsage
				snapshot.Temperature = telemetry.Temperature
				snapshot.Hashrate = telemetry.Hashrate
				snapshot.Efficiency = telemetry.Efficiency
				snapshot.Timestamp = telemetry.Timestamp
			}

			// Get component status
			status, err := s.telemetry.GetMinerComponentStatus(ctx, row.DeviceIdentifier)
			if err == nil && status != nil {
				snapshot.Status = status
			}

			// Build miner info for URL
			minerType, err := mm.TypeFromString(row.Type)
			if err == nil {
				minerInfo, err := s.minerService.BuildMinerInfo(ctx, row.DeviceIdentifier, orgID, row.IpAddress, row.Port, row.UrlScheme, minerType, snapshot.SerialNumber)
				if err == nil && minerInfo != nil {
					snapshot.IpAddress = minerInfo.GetConnectionInfo().IPAddress.String()
					snapshot.Url = minerInfo.GetWebViewURL().String()
				} else {
					// Fallback: Build URL from database fields when BuildMinerInfo fails
					snapshot.IpAddress = row.IpAddress
					snapshot.Url = constructWebViewURL(row.UrlScheme, row.IpAddress)
				}
			}

			// Set device status
			if row.DeviceStatus.Valid {
				snapshot.DeviceStatus = convertDeviceStatusStringToProto(string(row.DeviceStatus.DeviceStatusStatus))
			}
		} else {
			// For unpaired devices
			snapshot.Name = snapshot.Manufacturer + " " + snapshot.Model
			snapshot.IpAddress = row.IpAddress

			// Build URL
			url := row.UrlScheme + "://" + row.IpAddress
			if row.Port != "" && row.Port != "80" && row.Port != "443" {
				url += ":" + row.Port
			}
			snapshot.Url = url
			snapshot.DeviceStatus = pb.DeviceStatus_DEVICE_STATUS_INACTIVE
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nextCursor, total, nil
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
	// Initialize filter with defaults
	filter := &interfaces.MinerFilter{
		PairingStatuses: []pb.PairingStatus{}, // Empty means all
	}

	if pbFilter == nil {
		return filter, nil
	}

	// Handle new pairing_statuses field (preferred)
	if len(pbFilter.PairingStatuses) > 0 {
		filter.PairingStatuses = pbFilter.PairingStatuses
	}

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

// convertDeviceStatusStringToProto converts a database device status string to proto enum
func convertDeviceStatusStringToProto(status string) pb.DeviceStatus {
	switch strings.ToUpper(status) {
	case "ACTIVE":
		return pb.DeviceStatus_DEVICE_STATUS_ONLINE
	case "OFFLINE":
		return pb.DeviceStatus_DEVICE_STATUS_OFFLINE
	case "MAINTENANCE":
		return pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE
	case "ERROR":
		return pb.DeviceStatus_DEVICE_STATUS_ERROR
	case "INACTIVE":
		return pb.DeviceStatus_DEVICE_STATUS_INACTIVE
	default:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	}
}

// StreamMinerListUpdates streams incremental updates (additions/removals) for filtered miner list
// Only sends changes when miners enter/exit filter criteriafleetmanagement.test
func (s *Service) StreamMinerListUpdates(ctx context.Context, req *pb.StreamMinerListUpdatesRequest) (<-chan *pb.StreamMinerListUpdatesResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	responseChan := make(chan *pb.StreamMinerListUpdatesResponse, 10)

	// Set default heartbeat interval if not specified
	heartbeatInterval := req.HeartbeatIntervalSeconds
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 // default 30 seconds
	}

	go func() {
		defer close(responseChan)

		// Track current set of device IDs that match the filter with their positions
		// We maintain a sorted list to provide accurate positions for additions
		currentMatchingDevices := make(map[string]bool)
		sortedDeviceIDs := []string{} // Maintain sorted order

		// Build initial tracking state of ALL miners matching the filter
		// This is not sent to the client - they use ListMinerStateSnapshots for initial display
		buildInitialTrackingState := func() error {
			// Get ALL miners matching the filter (no pagination)
			// We use a large page size to get all miners in one query
			const maxPageSize int32 = 10000
			snapshot, err := s.buildSnapshot(ctx, claims.OrgID, maxPageSize, "", req.Filter, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)
			if err != nil {
				return err
			}

			// Track initial device IDs for delta calculation
			// Miners are already sorted from buildSnapshot
			for _, miner := range snapshot.Miners {
				currentMatchingDevices[miner.DeviceIdentifier] = true
				sortedDeviceIDs = append(sortedDeviceIDs, miner.DeviceIdentifier)
			}

			slog.Info("initialized miner list tracking",
				"orgID", claims.OrgID,
				"matchingMiners", len(currentMatchingDevices))

			return nil
		}

		// Initialize tracking state
		if err := buildInitialTrackingState(); err != nil {
			slog.Error("failed to build initial tracking state", "error", err)
			return
		}

		// Subscribe to device status change events for ALL devices in org
		// We need to monitor all devices to detect when they enter/exit filter criteria
		telemetryUpdateChan, unsubscribe, err := s.telemetry.SubscribeToTelemetryUpdates(
			ctx,
			claims.OrgID,
			nil, // All devices in org
			[]telemetryModels.UpdateType{telemetryModels.UpdateTypeDeviceStatus},
		)
		if err != nil {
			slog.Error("failed to subscribe to device status updates", "error", err)
			return
		}
		defer unsubscribe()

		// Set up ticker for heartbeats
		heartbeatTicker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
		defer heartbeatTicker.Stop()

		// Parse filter once for reuse
		filter, err := parseFilter(req.Filter)
		if err != nil {
			slog.Error("failed to parse filter", "error", err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return

			case update, ok := <-telemetryUpdateChan:
				if !ok {
					return
				}

				// Device status changed - check if it affects filter membership
				deviceID := string(update.DeviceID)

				// Get the device's current information
				device, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, deviceID, claims.OrgID)
				if err != nil {
					slog.Error("failed to get device", "deviceID", deviceID, "error", err)
					continue
				}

				// Check if device now matches the filter
				nowMatches := s.deviceMatchesFilter(device, filter, update.DeviceStatus)
				wasMatching := currentMatchingDevices[deviceID]

				// Generate delta if membership changed
				if nowMatches && !wasMatching {
					// Device added to filtered list
					currentMatchingDevices[deviceID] = true

					// Build miner snapshot for the added device
					snapshot := s.buildMinerSnapshot(ctx, device, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)

					// Find the correct position to insert based on sort order
					// For now, append to end. In production, you'd determine position based on
					// the same sorting logic used by ListMinerStateSnapshots
					position := len(sortedDeviceIDs)
					sortedDeviceIDs = append(sortedDeviceIDs, deviceID)

					// Get updated total count
					total, err := s.deviceStore.GetTotalPairedDevices(ctx, claims.OrgID, filter)
					if err != nil {
						slog.Error("failed to get total count", "error", err)
						total = int64(len(currentMatchingDevices))
					}

					deltaResp := &pb.StreamMinerListUpdatesResponse{
						Timestamp: timestamppb.Now(),
						Update: &pb.StreamMinerListUpdatesResponse_Delta{
							Delta: &pb.MinerListDelta{
								Additions: []*pb.MinerAddition{
									{
										Miner:    snapshot,
										Position: int32(position), //nolint:gosec
									},
								},
								TotalMiners: int32(total), //nolint:gosec
							},
						},
					}

					select {
					case <-ctx.Done():
						return
					case responseChan <- deltaResp:
					}

				} else if !nowMatches && wasMatching {
					// Device removed from filtered list
					delete(currentMatchingDevices, deviceID)

					// Remove from sorted list
					for i, id := range sortedDeviceIDs {
						if id == deviceID {
							sortedDeviceIDs = append(sortedDeviceIDs[:i], sortedDeviceIDs[i+1:]...)
							break
						}
					}

					// Get updated total count
					total, err := s.deviceStore.GetTotalPairedDevices(ctx, claims.OrgID, filter)
					if err != nil {
						slog.Error("failed to get total count", "error", err)
						total = int64(len(currentMatchingDevices))
					}

					deltaResp := &pb.StreamMinerListUpdatesResponse{
						Timestamp: timestamppb.Now(),
						Update: &pb.StreamMinerListUpdatesResponse_Delta{
							Delta: &pb.MinerListDelta{
								Removals:    []string{deviceID},
								TotalMiners: int32(total), //nolint:gosec
							},
						},
					}

					select {
					case <-ctx.Done():
						return
					case responseChan <- deltaResp:
					}
				}

			case <-heartbeatTicker.C:
				// Send heartbeat
				resp := &pb.StreamMinerListUpdatesResponse{
					Timestamp: timestamppb.Now(),
					Update: &pb.StreamMinerListUpdatesResponse_Heartbeat{
						Heartbeat: &pb.Heartbeat{},
					},
				}

				select {
				case <-ctx.Done():
					return
				case responseChan <- resp:
				}
			}
		}
	}()

	return responseChan, nil
}

// deviceMatchesFilter checks if a device matches the given filter criteria
func (s *Service) deviceMatchesFilter(device *pairingpb.Device, filter *interfaces.MinerFilter, status *mm.MinerStatus) bool {
	if filter == nil {
		return true // No filter means all devices match
	}

	// Check device status filter
	if len(filter.DeviceStatusFilter) > 0 {
		deviceStatus := mm.MinerStatusUnknown
		if status != nil {
			deviceStatus = *status
		}

		statusMatches := false
		for _, allowedStatus := range filter.DeviceStatusFilter {
			if deviceStatus == allowedStatus {
				statusMatches = true
				break
			}
		}
		if !statusMatches {
			return false
		}
	}

	// Check miner type filter
	if len(filter.MinerType) > 0 {
		// Convert device type string to mm.Type
		deviceType, err := mm.TypeFromString(device.Type)
		if err != nil {
			deviceType = mm.TypeUnknown
		}

		typeMatches := false
		for _, allowedType := range filter.MinerType {
			if deviceType == allowedType {
				typeMatches = true
				break
			}
		}
		if !typeMatches {
			return false
		}
	}

	// TODO: Add component filter checks when component status is available
	// This would require fetching component status from telemetry

	return true
}

// buildMinerSnapshot builds a MinerStateSnapshot for a single device
func (s *Service) buildMinerSnapshot(
	ctx context.Context,
	device *pairingpb.Device,
	dataMode pb.DataMode,
	timeSeriesConfig *commonpb.TimeSeriesConfig,
	measurementConfigs []*pb.MeasurementConfig,
) *pb.MinerStateSnapshot {
	// Get telemetry data
	telemetry, err := s.telemetry.GetMinerTelemetry(ctx, device.DeviceIdentifier, dataMode, timeSeriesConfig, measurementConfigs)
	if err != nil {
		// Continue without telemetry - non-critical for snapshot
		telemetry = nil
	}

	// Get component status
	status, err := s.telemetry.GetMinerComponentStatus(ctx, device.DeviceIdentifier)
	if err != nil {
		// Continue without component status
		status = nil
	}

	// Get org ID from claims
	claims, _ := tokenDomain.GetClientAuthJWTClaims(ctx)
	orgID := claims.OrgID

	// Get miner info
	var minerInfo minerInterfaces.MinerInfo
	minerType, err := mm.TypeFromString(device.Type)
	if err == nil {
		minerInfo, err = s.minerService.BuildMinerInfo(ctx, device.DeviceIdentifier, orgID, device.IpAddress, device.Port, device.UrlScheme, minerType, device.SerialNumber)
		if err != nil {
			// Continue without miner info
			minerInfo = nil
		}
	}

	// Get device status
	deviceStatuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(ctx, []mm.DeviceIdentifier{mm.DeviceIdentifier(device.DeviceIdentifier)})
	if err != nil {
		// Continue with empty status map
		deviceStatuses = make(map[mm.DeviceIdentifier]mm.MinerStatus)
	}

	minerStatus, ok := deviceStatuses[mm.DeviceIdentifier(device.DeviceIdentifier)]
	if !ok {
		minerStatus = mm.MinerStatusUnknown
	}
	deviceStatus := convertMinerStatusToDeviceStatus(minerStatus)

	snapshot := &pb.MinerStateSnapshot{
		Name:         device.Manufacturer + " " + device.Model,
		MacAddress:   device.MacAddress,
		SerialNumber: device.SerialNumber,
		DeviceStatus: deviceStatus,
	}

	if minerInfo != nil {
		snapshot.DeviceIdentifier = minerInfo.GetID().String()
		snapshot.IpAddress = minerInfo.GetConnectionInfo().IPAddress.String()
		snapshot.Url = minerInfo.GetWebViewURL().String()
	} else {
		// Fallback: construct URL from database fields when minerInfo is unavailable
		snapshot.DeviceIdentifier = device.DeviceIdentifier
		snapshot.IpAddress = device.IpAddress
		snapshot.Url = constructWebViewURL(device.UrlScheme, device.IpAddress)
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

	return snapshot
}
