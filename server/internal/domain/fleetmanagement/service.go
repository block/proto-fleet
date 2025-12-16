package fleetmanagement

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	diagnosticsmodels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	telemetrypb "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
)

const (
	// defaultPageSize is the default number of items returned per page when not specified
	defaultPageSize = 50
	// maxPageSize is the maximum number of items that can be returned per page
	maxPageSize = 1000
	// maxPageSizeForTracking is the maximum page size for internal tracking operations
	maxPageSizeForTracking = 10000

	// defaultHeartbeatIntervalSeconds is the default heartbeat interval in seconds
	defaultHeartbeatIntervalSeconds = 30

	// Channel buffer sizes
	measurementChannelBuffer = 100
	listUpdatesChannelBuffer = 10

	// Standard HTTP ports
	defaultHTTPPort  = "80"
	defaultHTTPSPort = "443"
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
	minerService          *miner.Service
}

func NewService(deviceStore interfaces.DeviceStore, discoveredDeviceStore interfaces.DiscoveredDeviceStore, t TelemetryCollector, minerService *miner.Service) *Service {
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

// ListMinerStateSnapshots returns a paginated list of miners with their metadata (no telemetry)
func (s *Service) ListMinerStateSnapshots(ctx context.Context, req *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	return s.buildSnapshot(ctx, info.OrganizationID, req.PageSize, req.Cursor, req.Filter)
}

// GetMinerStateCounts returns counts of miners in different states without fetching miner data
func (s *Service) GetMinerStateCounts(ctx context.Context, _ *pb.GetMinerStateCountsRequest) (*pb.GetMinerStateCountsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	total, err := s.deviceStore.GetTotalPairedDevices(ctx, info.OrganizationID, nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get total count: %v", err)
	}

	stateCounts, err := s.deviceStore.GetMinerStateCounts(ctx, info.OrganizationID, nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
	}

	return &pb.GetMinerStateCountsResponse{
		TotalMiners: int32(total), //nolint:gosec
		StateCounts: stateCounts,
	}, nil
}

// GetBatchMinerTelemetry returns telemetry data for multiple miners by their device identifiers.
// This is optimized for fetching telemetry after an initial metadata-only list load.
// Returns an authorization error if any device identifier does not belong to the user's organization.
// Note: Proto validation enforces min_items: 1, so empty requests are rejected at the handler level.
func (s *Service) GetBatchMinerTelemetry(ctx context.Context, req *pb.GetBatchMinerTelemetryRequest) (*pb.GetBatchMinerTelemetryResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Verify all requested devices belong to the user's organization
	allBelong, err := s.deviceStore.AllDevicesBelongToOrg(ctx, req.DeviceIdentifiers, info.OrganizationID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to validate device identifiers: %v", err)
	}
	if !allBelong {
		return nil, fleeterror.NewForbiddenError("access denied to one or more requested devices")
	}

	telemetryMap, err := s.telemetry.GetBatchMinerTelemetry(
		ctx,
		req.DeviceIdentifiers,
		req.DataMode,
		req.TimeSeriesConfig,
		req.MeasurementConfigs,
	)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get batch telemetry: %v", err)
	}

	miners := make([]*pb.MinerTelemetry, 0, len(telemetryMap))
	for deviceID, telemetry := range telemetryMap {
		minerTelemetry := &pb.MinerTelemetry{
			DeviceIdentifier: deviceID,
			PowerUsage:       telemetry.PowerUsage,
			Temperature:      telemetry.Temperature,
			Hashrate:         telemetry.Hashrate,
			Efficiency:       telemetry.Efficiency,
			Timestamp:        telemetry.Timestamp,
		}
		miners = append(miners, minerTelemetry)
	}

	return &pb.GetBatchMinerTelemetryResponse{
		Miners: miners,
	}, nil
}

// buildSnapshot builds a ListMinerStateSnapshotsResponse with metadata only (no telemetry)
// This is the shared implementation used by ListMinerStateSnapshots and StreamMinerListUpdates
func (s *Service) buildSnapshot(
	ctx context.Context,
	orgID int64,
	pageSize int32,
	cursor string,
	filterProto *pb.MinerListFilter,
) (*pb.ListMinerStateSnapshotsResponse, error) {
	filter, err := parseFilter(filterProto)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse filter parameters: %v", err)
	}

	pageSize = validatePageSize(pageSize)

	snapshots, nextCursor, total, err := s.buildSnapshotsFromUnifiedQuery(ctx, orgID, cursor, pageSize, filter)
	if err != nil {
		return nil, err
	}

	var stateCounts *telemetrypb.MinerStateCounts
	if shouldIncludeStateCounts(filter.PairingStatuses) {
		stateCounts, err = s.deviceStore.GetMinerStateCounts(ctx, orgID, filter)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
		}
	}

	availableTypes, err := s.deviceStore.GetAvailableMinerTypes(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available miner types: %v", err)
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
		MinerTypes:       convertMinerTypesToProto(availableTypes),
	}, nil
}

func (s *Service) buildSnapshotsFromUnifiedQuery(
	ctx context.Context,
	orgID int64,
	cursor string,
	pageSize int32,
	filter *interfaces.MinerFilter,
) ([]*pb.MinerStateSnapshot, string, int64, error) {
	rows, nextCursor, total, err := s.deviceStore.ListMinerStateSnapshots(ctx, orgID, cursor, pageSize, filter)
	if err != nil {
		return nil, "", 0, err
	}

	snapshots := make([]*pb.MinerStateSnapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := &pb.MinerStateSnapshot{
			DeviceIdentifier: row.DeviceIdentifier,
			Type:             row.Type,
		}

		if row.Model.Valid {
			snapshot.Model = row.Model.String
		}
		if row.Manufacturer.Valid {
			snapshot.Manufacturer = row.Manufacturer.String
		}

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
			snapshot.PairingStatus = pb.PairingStatus_PAIRING_STATUS_UNPAIRED
		}

		isPaired := row.PairingStatus == "PAIRED"

		if isPaired {
			snapshot.MacAddress = row.MacAddress
			if row.SerialNumber.Valid {
				snapshot.SerialNumber = row.SerialNumber.String
			}
			snapshot.Name = snapshot.Manufacturer + " " + snapshot.Model

			// Component status removed - now tracked via errors API
			// status, err := s.telemetry.GetMinerComponentStatus(ctx, row.DeviceIdentifier)
			// if err == nil && status != nil {
			// 	snapshot.Status = status
			// }

			snapshot.IpAddress = row.IpAddress
			snapshot.Url = constructWebViewURL(row.UrlScheme, row.IpAddress)

			if row.DeviceStatus.Valid {
				snapshot.DeviceStatus = convertDeviceStatusStringToProto(string(row.DeviceStatus.DeviceStatusStatus))
			}
		} else {
			snapshot.Name = snapshot.Manufacturer + " " + snapshot.Model
			snapshot.IpAddress = row.IpAddress

			url := row.UrlScheme + "://" + row.IpAddress
			if row.Port != "" && row.Port != defaultHTTPPort && row.Port != defaultHTTPSPort {
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
	_, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	responseChan := make(chan *pb.StreamMinerUpdatesResponse, measurementChannelBuffer)

	measurementChan, err := s.telemetry.StreamMeasurements(ctx, req.DeviceIdentifiers, req.MeasurementTypes)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to start measurement stream: %v", err)
	}

	// Component status streaming removed - now tracked via errors API
	// var statusChan <-chan *pb.StreamMinerUpdatesResponse
	// if req.IncludeStatusUpdates {
	// 	statusChan, err = s.telemetry.StreamComponentStatus(ctx, req.DeviceIdentifiers)
	// 	if err != nil {
	// 		return nil, fleeterror.NewInternalErrorf("failed to start status stream: %v", err)
	// 	}
	// }

	go func() {
		defer close(responseChan)

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

// shouldIncludeStateCounts determines if state counts should be fetched based on pairing status filter.
// State counts are only meaningful for devices that have telemetry data (PAIRED and AUTHENTICATION_NEEDED).
// Per proto definition: empty slice means "no filter" (include all), UNSPECIFIED means "all statuses".
func shouldIncludeStateCounts(pairingStatuses []pb.PairingStatus) bool {
	if len(pairingStatuses) == 0 {
		return true
	}
	for _, status := range pairingStatuses {
		switch status {
		case pb.PairingStatus_PAIRING_STATUS_PAIRED,
			pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			pb.PairingStatus_PAIRING_STATUS_UNSPECIFIED:
			return true
		case pb.PairingStatus_PAIRING_STATUS_UNPAIRED,
			pb.PairingStatus_PAIRING_STATUS_PENDING,
			pb.PairingStatus_PAIRING_STATUS_FAILED:
			// These statuses don't have telemetry data, skip
		}
	}
	return false
}

func parseFilter(pbFilter *pb.MinerListFilter) (*interfaces.MinerFilter, error) {
	filter := &interfaces.MinerFilter{
		PairingStatuses: []pb.PairingStatus{},
	}

	if pbFilter == nil {
		return filter, nil
	}

	if len(pbFilter.PairingStatuses) > 0 {
		filter.PairingStatuses = pbFilter.PairingStatuses
	}

	// Parse error component types - filter for devices that have errors for specific component types
	if len(pbFilter.ErrorComponentTypes) > 0 {
		componentTypes := make([]diagnosticsmodels.ComponentType, 0, len(pbFilter.ErrorComponentTypes))
		for _, ct := range pbFilter.ErrorComponentTypes {
			componentTypes = append(componentTypes, convertErrorComponentType(ct))
		}
		filter.ErrorComponentTypes = componentTypes
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

// convertErrorComponentType converts a proto ComponentType to domain ComponentType.
func convertErrorComponentType(ct errorsv1.ComponentType) diagnosticsmodels.ComponentType {
	switch ct {
	case errorsv1.ComponentType_COMPONENT_TYPE_PSU:
		return diagnosticsmodels.ComponentTypePSU
	case errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD:
		return diagnosticsmodels.ComponentTypeHashBoards
	case errorsv1.ComponentType_COMPONENT_TYPE_FAN:
		return diagnosticsmodels.ComponentTypeFans
	case errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
		return diagnosticsmodels.ComponentTypeControlBoard
	case errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
		errorsv1.ComponentType_COMPONENT_TYPE_EEPROM,
		errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE:
		return diagnosticsmodels.ComponentTypeUnspecified
	}
	return diagnosticsmodels.ComponentTypeUnspecified
}

// convertMinerTypesToProto converts domain miner types to protobuf enum values.
// Types without corresponding proto enum values are skipped.
func convertMinerTypesToProto(minerTypes []mm.Type) []pb.MinerType {
	result := make([]pb.MinerType, 0, len(minerTypes))
	for _, minerType := range minerTypes {
		switch minerType {
		case mm.TypeProto:
			result = append(result, pb.MinerType_MINER_TYPE_PROTO_RIG)
		case mm.TypeAntminer:
			result = append(result, pb.MinerType_MINER_TYPE_BITMAIN)
		case mm.TypeUnknown, mm.TypeWhatsminer, mm.TypeAvalon:
			// Skip types that don't have corresponding proto enum values
		}
	}
	return result
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	responseChan := make(chan *pb.StreamMinerListUpdatesResponse, listUpdatesChannelBuffer)

	heartbeatInterval := req.HeartbeatIntervalSeconds
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatIntervalSeconds
	}

	go func() {
		defer close(responseChan)

		currentMatchingDevices := make(map[string]bool)
		sortedDeviceIDs := []string{}

		// Build initial tracking state of ALL miners matching the filter
		// This is not sent to the client - they use ListMinerStateSnapshots for initial display
		buildInitialTrackingState := func() error {
			snapshot, err := s.buildSnapshot(ctx, info.OrganizationID, maxPageSizeForTracking, "", req.Filter)
			if err != nil {
				return err
			}

			for _, miner := range snapshot.Miners {
				currentMatchingDevices[miner.DeviceIdentifier] = true
				sortedDeviceIDs = append(sortedDeviceIDs, miner.DeviceIdentifier)
			}

			slog.Info("initialized miner list tracking",
				"orgID", info.OrganizationID,
				"matchingMiners", len(currentMatchingDevices))

			return nil
		}

		if err := buildInitialTrackingState(); err != nil {
			slog.Error("failed to build initial tracking state", "error", err)
			return
		}

		// Subscribe to device status change events for ALL devices in org
		// We need to monitor all devices to detect when they enter/exit filter criteria
		telemetryUpdateChan, unsubscribe, err := s.telemetry.SubscribeToTelemetryUpdates(
			ctx,
			info.OrganizationID,
			nil, // All devices in org
			[]telemetryModels.UpdateType{telemetryModels.UpdateTypeDeviceStatus},
		)
		if err != nil {
			slog.Error("failed to subscribe to device status updates", "error", err)
			return
		}
		defer unsubscribe()

		heartbeatTicker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
		defer heartbeatTicker.Stop()

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

				deviceID := string(update.DeviceID)

				device, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, deviceID, info.OrganizationID)
				if err != nil {
					slog.Error("failed to get device", "deviceID", deviceID, "error", err)
					continue
				}

				nowMatches := s.deviceMatchesFilter(device, filter, update.DeviceStatus)
				wasMatching := currentMatchingDevices[deviceID]

				if nowMatches && !wasMatching {
					currentMatchingDevices[deviceID] = true

					snapshot := s.buildMinerSnapshotWithTelemetry(ctx, device, req.DataMode, req.TimeSeriesConfig, req.MeasurementConfigs)

					position := len(sortedDeviceIDs)
					sortedDeviceIDs = append(sortedDeviceIDs, deviceID)

					total, err := s.deviceStore.GetTotalPairedDevices(ctx, info.OrganizationID, filter)
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
					delete(currentMatchingDevices, deviceID)

					for i, id := range sortedDeviceIDs {
						if id == deviceID {
							sortedDeviceIDs = append(sortedDeviceIDs[:i], sortedDeviceIDs[i+1:]...)
							break
						}
					}

					total, err := s.deviceStore.GetTotalPairedDevices(ctx, info.OrganizationID, filter)
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
		return true
	}

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

	if len(filter.MinerType) > 0 {
		deviceType := mm.ParseDeviceTypeOrUnknown(device.Type, device.Model)

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

	// Component filtering is handled at the SQL level through JOIN with errors table
	// No need for application-level filtering here

	return true
}

// buildMinerSnapshotWithTelemetry builds a snapshot with optional telemetry for streaming updates
func (s *Service) buildMinerSnapshotWithTelemetry(
	ctx context.Context,
	device *pairingpb.Device,
	dataMode pb.DataMode,
	timeSeriesConfig *commonpb.TimeSeriesConfig,
	measurementConfigs []*pb.MeasurementConfig,
) *pb.MinerStateSnapshot {
	telemetry, err := s.telemetry.GetMinerTelemetry(ctx, device.DeviceIdentifier, dataMode, timeSeriesConfig, measurementConfigs)
	if err != nil {
		telemetry = nil
	}

	// Component status removed - now tracked via errors API
	// status, err := s.telemetry.GetMinerComponentStatus(ctx, device.DeviceIdentifier)
	// if err != nil {
	// 	status = nil
	// }

	deviceStatuses, err := s.deviceStore.GetDeviceStatusForDeviceIdentifiers(ctx, []mm.DeviceIdentifier{mm.DeviceIdentifier(device.DeviceIdentifier)})
	if err != nil {
		deviceStatuses = make(map[mm.DeviceIdentifier]mm.MinerStatus)
	}

	minerStatus, ok := deviceStatuses[mm.DeviceIdentifier(device.DeviceIdentifier)]
	if !ok {
		minerStatus = mm.MinerStatusUnknown
	}
	deviceStatus := convertMinerStatusToDeviceStatus(minerStatus)

	snapshot := &pb.MinerStateSnapshot{
		Name:             device.Manufacturer + " " + device.Model,
		MacAddress:       device.MacAddress,
		SerialNumber:     device.SerialNumber,
		DeviceStatus:     deviceStatus,
		DeviceIdentifier: device.DeviceIdentifier,
		IpAddress:        device.IpAddress,
		Url:              constructWebViewURL(device.UrlScheme, device.IpAddress),
	}

	if telemetry != nil {
		snapshot.PowerUsage = telemetry.PowerUsage
		snapshot.Temperature = telemetry.Temperature
		snapshot.Hashrate = telemetry.Hashrate
		snapshot.Efficiency = telemetry.Efficiency
		snapshot.Timestamp = telemetry.Timestamp
	}

	// Component status removed - now tracked via errors API
	// if status != nil {
	// 	snapshot.Status = status
	// }

	return snapshot
}
