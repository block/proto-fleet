package fleetmanagement

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	diagnosticsmodels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	"google.golang.org/protobuf/types/known/timestamppb"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	poolspb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
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

	// defaultQueryTimeout is the maximum time allowed for expensive database queries.
	// Set to 5 seconds as a balance between:
	// - Allowing sufficient time for complex queries on large fleets (100s of miners)
	// - Preventing slow queries from holding DB connections during rapid user interactions
	// - Being short enough that cancelled requests release connections quickly
	defaultQueryTimeout = 5 * time.Second
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

// CapabilitiesProvider provides miner capabilities from plugins.
// Implementations should return device-specific capabilities based on the
// manufacturer, model, and type information in the provided Device.
// Returns nil if capabilities cannot be determined for the device.
type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities
}

type Service struct {
	deviceStore           interfaces.DeviceStore
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	telemetry             TelemetryCollector
	minerService          *miner.Service
	capabilitiesProvider  CapabilitiesProvider
	capabilitiesCache     sync.Map
	poolStore             interfaces.PoolStore

	// Stream deduplication: ensures only one active StreamMinerListUpdates per session.
	// When a new stream request arrives for a session, the previous stream is cancelled
	// to prevent connection exhaustion from rapid scrolling.
	activeStreams   map[string]*activeStream
	activeStreamsMu sync.Mutex
}

// activeStream tracks an active streaming goroutine with its cancel function and unique ID.
type activeStream struct {
	cancel func()
	id     uint64
}

// minerListStreamIDCounter generates unique IDs for active streams.
var minerListStreamIDCounter uint64

func NewService(
	deviceStore interfaces.DeviceStore,
	discoveredDeviceStore interfaces.DiscoveredDeviceStore,
	t TelemetryCollector,
	minerService *miner.Service,
	capabilitiesProvider CapabilitiesProvider,
	poolStore interfaces.PoolStore,
) *Service {
	return &Service{
		deviceStore:           deviceStore,
		discoveredDeviceStore: discoveredDeviceStore,
		telemetry:             t,
		minerService:          minerService,
		capabilitiesProvider:  capabilitiesProvider,
		poolStore:             poolStore,
		activeStreams:         make(map[string]*activeStream),
	}
}

// getCachedCapabilities retrieves capabilities from cache or fetches and caches them
func (s *Service) getCachedCapabilities(ctx context.Context, manufacturer, model, deviceType string) *capabilitiespb.MinerCapabilities {
	if s.capabilitiesProvider == nil || manufacturer == "" || model == "" {
		return nil
	}

	cacheKey := manufacturer + "|" + model + "|" + deviceType

	if cached, found := s.capabilitiesCache.Load(cacheKey); found {
		if capabilities, ok := cached.(*capabilitiespb.MinerCapabilities); ok {
			return capabilities
		}
		return nil
	}

	device := &pairingpb.Device{
		Manufacturer: manufacturer,
		Model:        model,
		Type:         deviceType,
	}
	capabilities := s.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, device)

	if capabilities != nil {
		s.capabilitiesCache.Store(cacheKey, capabilities)
	}

	return capabilities
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

	sortConfig := parseSortConfig(req.Sort)

	return s.buildSnapshot(ctx, info.OrganizationID, req.PageSize, req.Cursor, req.Filter, sortConfig)
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
	sortConfig *interfaces.SortConfig,
) (*pb.ListMinerStateSnapshotsResponse, error) {
	filter, err := parseFilter(filterProto)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse filter parameters: %v", err)
	}

	pageSize = validatePageSize(pageSize)

	snapshots, nextCursor, total, err := s.buildSnapshotsFromUnifiedQuery(ctx, orgID, cursor, pageSize, filter, sortConfig)
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

	availableModels, err := s.deviceStore.GetAvailableModels(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available models: %v", err)
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
		Models:           availableModels,
	}, nil
}

func (s *Service) buildSnapshotsFromUnifiedQuery(
	ctx context.Context,
	orgID int64,
	cursor string,
	pageSize int32,
	filter *interfaces.MinerFilter,
	sortConfig *interfaces.SortConfig,
) ([]*pb.MinerStateSnapshot, string, int64, error) {
	rows, nextCursor, total, err := s.deviceStore.ListMinerStateSnapshots(ctx, orgID, cursor, pageSize, filter, sortConfig)
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
		if row.FirmwareVersion.Valid {
			snapshot.FirmwareVersion = row.FirmwareVersion.String
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
			snapshot.IpAddress = row.IpAddress
			snapshot.Url = constructWebViewURL(row.UrlScheme, row.IpAddress)

			if row.DeviceStatus.Valid {
				snapshot.DeviceStatus = convertDeviceStatusStringToProto(string(row.DeviceStatus.DeviceStatusEnum))
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

		capabilities := s.getCachedCapabilities(ctx, snapshot.Manufacturer, snapshot.Model, snapshot.Type)
		if capabilities != nil {
			snapshot.Capabilities = capabilities
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
			case pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
				statusFilters = append(statusFilters, mm.MinerStatusNeedsMiningPool)
			default:
				return nil, fleeterror.NewInternalErrorf("unsupported miner status: %v", status)
			}
		}
		filter.DeviceStatusFilter = statusFilters
	}

	if len(pbFilter.Models) > 0 {
		filter.ModelNames = pbFilter.Models
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
		case mm.TypeUnknown, mm.TypeWhatsminer, mm.TypeAvalon, mm.TypeVirtual:
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
	case mm.MinerStatusNeedsMiningPool:
		return pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
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
	case "NEEDS_MINING_POOL":
		return pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	default:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	}
}

// StreamMinerListUpdates streams incremental updates (additions/removals) for filtered miner list.
// Only sends changes when miners enter/exit filter criteria.
//
// Stream deduplication: Only one active stream is allowed per session+connection. When a new stream
// request arrives with the same session and connection_id, the previous stream is cancelled.
// This prevents connection exhaustion from rapid scrolling while allowing multiple browser tabs
// to maintain independent streams.
func (s *Service) StreamMinerListUpdates(ctx context.Context, req *pb.StreamMinerListUpdatesRequest) (<-chan *pb.StreamMinerListUpdatesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Build deduplication key from session ID and connection ID
	// This allows multiple tabs (different connection IDs) to have independent streams
	// while still deduplicating rapid requests within the same tab
	dedupeKey := info.SessionID
	if req.ConnectionId != "" {
		dedupeKey = info.SessionID + ":" + req.ConnectionId
	}

	// Cancel any existing stream for this session+connection to prevent connection exhaustion
	s.activeStreamsMu.Lock()
	if existing, exists := s.activeStreams[dedupeKey]; exists {
		existing.cancel()
		slog.Debug("cancelled existing stream", "dedupeKey", dedupeKey)
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	streamID := atomic.AddUint64(&minerListStreamIDCounter, 1)
	s.activeStreams[dedupeKey] = &activeStream{
		cancel: cancelStream,
		id:     streamID,
	}
	s.activeStreamsMu.Unlock()

	responseChan := make(chan *pb.StreamMinerListUpdatesResponse, listUpdatesChannelBuffer)

	heartbeatInterval := req.HeartbeatIntervalSeconds
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatIntervalSeconds
	}

	go func() {
		defer func() {
			// Cancel the stream context to ensure all child operations are cleaned up
			cancelStream()
			// Clean up the active stream entry
			s.activeStreamsMu.Lock()
			// Only delete if this stream is still the active one
			// (avoids deleting a newer stream's entry)
			if existing, exists := s.activeStreams[dedupeKey]; exists && existing.id == streamID {
				delete(s.activeStreams, dedupeKey)
				slog.Debug("cleaned up miner list update stream", "dedupeKey", dedupeKey, "streamID", streamID)
			}
			s.activeStreamsMu.Unlock()
			close(responseChan)
		}()

		currentMatchingDevices := make(map[string]bool)
		sortedDeviceIDs := []string{}
		sortConfig := parseSortConfig(req.Sort)

		// Build initial tracking state of ALL miners matching the filter
		// This is not sent to the client - they use ListMinerStateSnapshots for initial display
		buildInitialTrackingState := func() error {
			// Use timeout context to prevent slow queries from holding connections
			queryCtx, cancel := context.WithTimeout(streamCtx, defaultQueryTimeout)
			defer cancel()

			snapshot, err := s.buildSnapshot(queryCtx, info.OrganizationID, maxPageSizeForTracking, "", req.Filter, sortConfig)
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
			streamCtx,
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
			case <-streamCtx.Done():
				return

			case update, ok := <-telemetryUpdateChan:
				if !ok {
					return
				}

				// Check context before expensive DB query
				select {
				case <-streamCtx.Done():
					return
				default:
				}

				deviceID := string(update.DeviceIdentifier)

				device, err := s.deviceStore.GetDeviceByDeviceIdentifier(streamCtx, deviceID, info.OrganizationID)
				if err != nil {
					slog.Error("failed to get device", "deviceID", deviceID, "error", err)
					continue
				}

				nowMatches := s.deviceMatchesFilter(device, filter, update.DeviceStatus)
				wasMatching := currentMatchingDevices[deviceID]

				if nowMatches && !wasMatching {
					currentMatchingDevices[deviceID] = true

					// Check context before expensive operation
					select {
					case <-streamCtx.Done():
						return
					default:
					}

					snapshot := s.buildMinerSnapshotWithTelemetry(streamCtx, device, req.DataMode, req.MeasurementConfigs)

					position := len(sortedDeviceIDs)
					sortedDeviceIDs = append(sortedDeviceIDs, deviceID)

					// Check context before expensive DB query
					select {
					case <-streamCtx.Done():
						return
					default:
					}

					total, err := s.deviceStore.GetTotalPairedDevices(streamCtx, info.OrganizationID, filter)
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
					case <-streamCtx.Done():
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

					// Check context before expensive DB query
					select {
					case <-streamCtx.Done():
						return
					default:
					}

					total, err := s.deviceStore.GetTotalPairedDevices(streamCtx, info.OrganizationID, filter)
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
					case <-streamCtx.Done():
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
				case <-streamCtx.Done():
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

	// Model filter: case-sensitive exact match.
	// Filter values should come from availableModels returned by the API.
	if len(filter.ModelNames) > 0 {
		modelMatches := false
		for _, allowedModel := range filter.ModelNames {
			if device.Model == allowedModel {
				modelMatches = true
				break
			}
		}
		if !modelMatches {
			return false
		}
	}

	return true
}

// buildMinerSnapshotWithTelemetry builds a snapshot with optional telemetry for streaming updates
func (s *Service) buildMinerSnapshotWithTelemetry(
	ctx context.Context,
	device *pairingpb.Device,
	dataMode pb.DataMode,
	measurementConfigs []*pb.MeasurementConfig,
) *pb.MinerStateSnapshot {
	telemetry, err := s.telemetry.GetMinerTelemetry(ctx, device.DeviceIdentifier, dataMode, measurementConfigs)
	if err != nil {
		telemetry = nil
	}

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

	return snapshot
}

// GetMinerPoolAssignments retrieves the currently configured pools from a miner
// and matches them with fleet pool definitions to return pool IDs
func (s *Service) GetMinerPoolAssignments(ctx context.Context, req *pb.GetMinerPoolAssignmentsRequest) (*pb.GetMinerPoolAssignmentsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Get the miner by device identifier
	minerDevice, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(req.DeviceIdentifier))
	if err != nil {
		if isMinerNotFoundError(err) {
			return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get miner: %v", err)
	}

	// Verify the miner belongs to the user's organization
	if minerDevice.GetOrgID() != info.OrganizationID {
		return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
	}

	// Get currently configured pools from the miner
	configuredPools, err := minerDevice.GetMiningPools(ctx)
	if err != nil {
		slog.Error("failed to get mining pools from miner", "deviceID", req.DeviceIdentifier, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to get mining pools from miner: %v", err)
	}

	// If no pools configured, return empty response
	if len(configuredPools) == 0 {
		return &pb.GetMinerPoolAssignmentsResponse{}, nil
	}

	// Get all fleet pools for matching
	fleetPools, err := s.poolStore.ListPools(ctx, info.OrganizationID)
	if err != nil {
		slog.Error("failed to list fleet pools", "orgID", info.OrganizationID, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to list fleet pools: %v", err)
	}

	// Sort pools by priority to ensure consistent ordering
	// (miner API does not guarantee order)
	sort.Slice(configuredPools, func(i, j int) bool {
		return configuredPools[i].Priority < configuredPools[j].Priority
	})

	pools := make([]*pb.PoolAssignment, 0, len(configuredPools))
	for _, configuredPool := range configuredPools {
		assignment := &pb.PoolAssignment{
			Url:      configuredPool.URL,
			Username: configuredPool.Username,
			PoolId:   findMatchingFleetPoolID(configuredPool.URL, configuredPool.Username, fleetPools),
		}
		pools = append(pools, assignment)
	}

	return &pb.GetMinerPoolAssignmentsResponse{Pools: pools}, nil
}

// GetMinerCoolingMode retrieves the currently configured cooling mode from a miner.
func (s *Service) GetMinerCoolingMode(ctx context.Context, req *pb.GetMinerCoolingModeRequest) (*pb.GetMinerCoolingModeResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Get the miner by device identifier
	minerDevice, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(req.DeviceIdentifier))
	if err != nil {
		if isMinerNotFoundError(err) {
			return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get miner: %v", err)
	}

	// Verify the miner belongs to the user's organization
	if minerDevice.GetOrgID() != info.OrganizationID {
		return nil, fleeterror.NewNotFoundErrorf("miner not found: %s", req.DeviceIdentifier)
	}

	// Get current cooling mode from the miner
	coolingMode, err := minerDevice.GetCoolingMode(ctx)
	if err != nil {
		slog.Error("failed to get cooling mode from miner", "deviceID", req.DeviceIdentifier, "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to get cooling mode from miner: %v", err)
	}

	return &pb.GetMinerCoolingModeResponse{CoolingMode: coolingMode}, nil
}

// findMatchingFleetPoolID finds a fleet pool that matches the given URL and username.
// Username matching extracts the base username (before the first ".") since miners
// append device identifiers to worker names (e.g., "pool_user" becomes "pool_user.device123").
func findMatchingFleetPoolID(url, username string, fleetPools []*poolspb.Pool) *int64 {
	baseUsername, _, _ := strings.Cut(username, ".")
	for _, pool := range fleetPools {
		if pool.Url == url && baseUsername == pool.Username {
			return &pool.PoolId
		}
	}
	return nil
}

// isMinerNotFoundError checks if an error from the miner service indicates the device was not found.
func isMinerNotFoundError(err error) bool {
	return fleeterror.IsNotFoundError(err)
}
