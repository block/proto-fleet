package fleetmanagement

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/deviceresolver"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	telemetryModels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// defaultPageSize is the default number of items returned per page when not specified
	defaultPageSize = 50
	// maxPageSize is the maximum number of items that can be returned per page
	maxPageSize = 1000

	// Standard HTTP ports
	defaultHTTPPort  = "80"
	defaultHTTPSPort = "443"

	// concurrentClearAuthKeyLimit bounds the number of parallel ClearAuthKey RPCs
	// fired in the background after a delete operation
	concurrentClearAuthKeyLimit = 20

	// clearAuthKeyTimeout is the per-device timeout for best-effort ClearAuthKey calls
	clearAuthKeyTimeout = 5 * time.Second
)

// bracketIPv6Host wraps bare IPv6 addresses in brackets for use in URLs.
func bracketIPv6Host(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// constructWebViewURL builds a web view URL
//
// Note: The port is intentionally omitted from the URL for display purposes, as web browsers
// will use the default port for the scheme (80 for http, 443 for https). This matches the
// behavior of GetWebViewURL().
func constructWebViewURL(scheme, ipAddress string) string {
	if ipAddress == "" || scheme == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, bracketIPv6Host(ipAddress))
}

// CapabilitiesProvider provides miner capabilities from plugins.
// Implementations should return device-specific capabilities based on the
// manufacturer, model, and type information in the provided Device.
// Returns nil if capabilities cannot be determined for the device.
type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities
}

type WorkerNamePoolReapplyService interface {
	VerifyCredentials(ctx context.Context, userUsername string, userPassword string) error

	ReapplyCurrentPoolsWithWorkerNames(
		ctx context.Context,
		desiredWorkerNamesByDeviceIdentifier map[string]string,
	) (batchIdentifier string, err error)
}

type Service struct {
	deviceStore           interfaces.DeviceStore
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	telemetry             TelemetryCollector
	minerService          *miner.Service
	capabilitiesProvider  CapabilitiesProvider
	capabilitiesCache     sync.Map
	poolStore             interfaces.PoolStore
	errorStore            interfaces.ErrorStore
	collectionStore       interfaces.CollectionStore
	workerNamePoolService WorkerNamePoolReapplyService
	deviceResolver        *deviceresolver.Resolver
	activitySvc           *activity.Service

	// backgroundWg tracks in-flight background ClearAuthKey goroutines so they can
	// be awaited during graceful shutdown via WaitForPendingClearAuthKeys.
	backgroundWg sync.WaitGroup

	// clearAuthKeySem bounds the total number of concurrent ClearAuthKey RPCs
	// across all delete operations. Shared at the service level so that multiple
	// concurrent DeleteMiners calls don't exceed the limit.
	clearAuthKeySem chan struct{}
}

func NewService(
	deviceStore interfaces.DeviceStore,
	discoveredDeviceStore interfaces.DiscoveredDeviceStore,
	t TelemetryCollector,
	minerService *miner.Service,
	capabilitiesProvider CapabilitiesProvider,
	poolStore interfaces.PoolStore,
	errorStore interfaces.ErrorStore,
	collectionStore interfaces.CollectionStore,
	workerNamePoolService WorkerNamePoolReapplyService,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		deviceStore:           deviceStore,
		discoveredDeviceStore: discoveredDeviceStore,
		telemetry:             t,
		minerService:          minerService,
		capabilitiesProvider:  capabilitiesProvider,
		poolStore:             poolStore,
		errorStore:            errorStore,
		collectionStore:       collectionStore,
		workerNamePoolService: workerNamePoolService,
		activitySvc:           activitySvc,
		deviceResolver:        deviceresolver.New(deviceStore),
		clearAuthKeySem:       make(chan struct{}, concurrentClearAuthKeyLimit),
	}
}

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

// WaitForPendingClearAuthKeys blocks until all background ClearAuthKey goroutines
// complete or the timeout expires. Call during graceful server shutdown.
func (s *Service) WaitForPendingClearAuthKeys(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.backgroundWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("timed out waiting for pending ClearAuthKey operations during shutdown")
	}
}

// getCachedCapabilities retrieves capabilities from cache or fetches and caches them
func (s *Service) getCachedCapabilities(ctx context.Context, manufacturer, model, driverName string) *capabilitiespb.MinerCapabilities {
	if s.capabilitiesProvider == nil || manufacturer == "" || model == "" {
		return nil
	}

	cacheKey := manufacturer + "|" + model + "|" + driverName

	if cached, found := s.capabilitiesCache.Load(cacheKey); found {
		if capabilities, ok := cached.(*capabilitiespb.MinerCapabilities); ok {
			return capabilities
		}
		return nil
	}

	device := &pairingpb.Device{
		Manufacturer: manufacturer,
		Model:        model,
		DriverName:   driverName,
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

// GetMinerModelGroups returns model groups with counts, optionally filtered by the provided fleet filter.
func (s *Service) GetMinerModelGroups(ctx context.Context, req *pb.GetMinerModelGroupsRequest) (*pb.GetMinerModelGroupsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := parseFilter(req.Filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse filter: %v", err)
	}

	groups, err := s.deviceStore.GetMinerModelGroups(ctx, info.OrganizationID, filter)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get miner model groups: %v", err)
	}

	pbGroups := make([]*pb.MinerModelGroup, 0, len(groups))
	for _, g := range groups {
		pbGroups = append(pbGroups, &pb.MinerModelGroup{
			Model:        g.Model,
			Manufacturer: g.Manufacturer,
			Count:        g.Count,
		})
	}

	return &pb.GetMinerModelGroupsResponse{Groups: pbGroups}, nil
}

// buildSnapshot builds a ListMinerStateSnapshotsResponse with metadata and telemetry data.
// This is the shared implementation used by ListMinerStateSnapshots.
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

	// Enrich snapshots with telemetry and collection labels for paired devices
	pairedDeviceIDs := collectPairedDeviceIdentifiers(snapshots)
	s.populateTelemetryData(ctx, snapshots, pairedDeviceIDs)
	s.populateGroupLabels(ctx, orgID, snapshots, pairedDeviceIDs)
	s.populateRackDetails(ctx, orgID, snapshots, pairedDeviceIDs)

	var stateCounts *telemetrypb.MinerStateCounts
	if shouldIncludeStateCounts(filter.PairingStatuses) {
		stateCounts, err = s.deviceStore.GetMinerStateCounts(ctx, orgID, filter)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get state counts: %v", err)
		}
	}

	// TODO(#125): These option-array hydrations run on every list page request,
	// not just page 1. The Fleet UI fans out to multiple list calls per render
	// (main list + total + auth-needed total), so each page nav pays N DISTINCT
	// scans. Accepted for now to keep deep-links working without client coupling;
	// a per-org TTL cache (or equivalent) should replace this.
	availableModels, err := s.deviceStore.GetAvailableModels(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available models: %v", err)
	}

	availableFirmwareVersions, err := s.deviceStore.GetAvailableFirmwareVersions(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get available firmware versions: %v", err)
	}

	return &pb.ListMinerStateSnapshotsResponse{
		Miners:           snapshots,
		Cursor:           nextCursor,
		TotalMiners:      int32(total), //nolint:gosec
		TotalStateCounts: stateCounts,
		Models:           availableModels,
		FirmwareVersions: availableFirmwareVersions,
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
			DriverName:       row.DriverName,
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
		if row.WorkerName.Valid {
			snapshot.WorkerName = row.WorkerName.String
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

		snapshot.Name = ComposeDeviceName(row.CustomName.String, snapshot.Manufacturer, snapshot.Model)

		if isPaired {
			snapshot.MacAddress = row.MacAddress
			if row.SerialNumber.Valid {
				snapshot.SerialNumber = row.SerialNumber.String
			}
			snapshot.IpAddress = row.IpAddress
			snapshot.Url = constructWebViewURL(row.UrlScheme, row.IpAddress)

			if row.DeviceStatus.Valid {
				snapshot.DeviceStatus = convertDeviceStatusStringToProto(string(row.DeviceStatus.DeviceStatusEnum))
			}
		} else {
			snapshot.IpAddress = row.IpAddress

			url := row.UrlScheme + "://" + bracketIPv6Host(row.IpAddress)
			if row.Port != "" && row.Port != defaultHTTPPort && row.Port != defaultHTTPSPort {
				url += ":" + row.Port
			}
			snapshot.Url = url
			snapshot.DeviceStatus = pb.DeviceStatus_DEVICE_STATUS_INACTIVE
		}

		capabilities := s.getCachedCapabilities(ctx, snapshot.Manufacturer, snapshot.Model, row.DriverName)
		if capabilities != nil {
			snapshot.Capabilities = capabilities
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nextCursor, total, nil
}

// Unit conversion constants for telemetry data
const (
	// hashToTeraHashConversion converts H/s to TH/s (1 TH = 10^12 H)
	hashToTeraHashConversion = 1e12
	// wattsToKilowattsConversion converts W to kW
	wattsToKilowattsConversion = 1000.0
	// joulesPerHashToJoulesPerTeraHashMultiplier converts J/H to J/TH
	// Since 1 TH = 1e12 H, efficiency in J/H * 1e12 = efficiency in J/TH
	joulesPerHashToJoulesPerTeraHashMultiplier = 1e12
)

func collectPairedDeviceIdentifiers(snapshots []*pb.MinerStateSnapshot) []string {
	var ids []string
	for _, s := range snapshots {
		if s.PairingStatus == pb.PairingStatus_PAIRING_STATUS_PAIRED {
			ids = append(ids, s.DeviceIdentifier)
		}
	}
	return ids
}

// populateTelemetryData fetches telemetry data for paired devices and populates the snapshot fields.
func (s *Service) populateTelemetryData(ctx context.Context, snapshots []*pb.MinerStateSnapshot, pairedIDs []string) {
	if len(pairedIDs) == 0 {
		return
	}

	deviceIDs := make([]mm.DeviceIdentifier, len(pairedIDs))
	for i, id := range pairedIDs {
		deviceIDs[i] = mm.DeviceIdentifier(id)
	}

	telemetryData, err := s.telemetry.GetLatestDeviceMetrics(ctx, deviceIDs)
	if err != nil {
		slog.Warn("failed to fetch telemetry data for snapshots", "error", err)
		return
	}

	// Populate telemetry fields on snapshots
	for _, snapshot := range snapshots {
		metrics, ok := telemetryData[mm.DeviceIdentifier(snapshot.DeviceIdentifier)]
		if !ok {
			continue
		}

		snapshot.Timestamp = timestamppb.New(metrics.Timestamp)

		if metrics.HashrateHS != nil {
			snapshot.Hashrate = []*commonpb.Measurement{
				convertToMeasurement(metrics.HashrateHS, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_TERAHASH_PER_SECOND, hashToTeraHashConversion),
			}
		}

		if metrics.TempC != nil {
			snapshot.Temperature = []*commonpb.Measurement{
				convertToMeasurement(metrics.TempC, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_CELSIUS, 1.0),
			}
		}

		if metrics.PowerW != nil {
			snapshot.PowerUsage = []*commonpb.Measurement{
				convertToMeasurement(metrics.PowerW, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_KILOWATT, wattsToKilowattsConversion),
			}
		}

		if metrics.EfficiencyJH != nil {
			snapshot.Efficiency = []*commonpb.Measurement{
				convertToMeasurementWithMultiplier(metrics.EfficiencyJH, metrics.Timestamp, commonpb.MeasurementUnit_MEASUREMENT_UNIT_JOULES_PER_TERAHASH, joulesPerHashToJoulesPerTeraHashMultiplier),
			}
		}
	}
}

// populateGroupLabels fetches group labels for paired devices and populates the GroupLabels field.
func (s *Service) populateGroupLabels(ctx context.Context, orgID int64, snapshots []*pb.MinerStateSnapshot, pairedDeviceIDs []string) {
	if len(pairedDeviceIDs) == 0 {
		return
	}

	groupLabels, err := s.collectionStore.GetGroupLabelsForDevices(ctx, orgID, pairedDeviceIDs)
	if err != nil {
		slog.Warn("failed to fetch group labels for snapshots", "error", err)
		return
	}

	// Populate group labels on snapshots
	for _, snapshot := range snapshots {
		if labels, ok := groupLabels[snapshot.DeviceIdentifier]; ok {
			snapshot.GroupLabels = labels
		}
	}
}

// populateRackDetails fetches rack labels and slot positions for paired devices.
func (s *Service) populateRackDetails(ctx context.Context, orgID int64, snapshots []*pb.MinerStateSnapshot, pairedDeviceIDs []string) {
	if len(pairedDeviceIDs) == 0 {
		return
	}

	rackDetails, err := s.collectionStore.GetRackDetailsForDevices(ctx, orgID, pairedDeviceIDs)
	if err != nil {
		slog.Warn("failed to fetch rack details for snapshots", "error", err)
		return
	}

	// Populate rack details on snapshots
	for _, snapshot := range snapshots {
		if details, ok := rackDetails[snapshot.DeviceIdentifier]; ok {
			snapshot.RackLabel = details.Label
			snapshot.RackPosition = details.Position
		}
	}
}

// convertToMeasurement converts a MetricValue to a proto Measurement by dividing by the conversion factor.
func convertToMeasurement(metric *modelsV2.MetricValue, timestamp time.Time, unit commonpb.MeasurementUnit, divisor float64) *commonpb.Measurement {
	return &commonpb.Measurement{
		Timestamp: timestamppb.New(timestamp),
		Value:     metric.Value / divisor,
		Unit:      unit,
	}
}

// convertToMeasurementWithMultiplier converts a MetricValue to a proto Measurement by multiplying by the conversion factor.
func convertToMeasurementWithMultiplier(metric *modelsV2.MetricValue, timestamp time.Time, unit commonpb.MeasurementUnit, multiplier float64) *commonpb.Measurement {
	return &commonpb.Measurement{
		Timestamp: timestamppb.New(timestamp),
		Value:     metric.Value * multiplier,
		Unit:      unit,
	}
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
			case pb.DeviceStatus_DEVICE_STATUS_UPDATING:
				statusFilters = append(statusFilters, mm.MinerStatusUpdating)
			case pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
				statusFilters = append(statusFilters, mm.MinerStatusRebootRequired)
			default:
				return nil, fleeterror.NewInternalErrorf("unsupported miner status: %v", status)
			}
		}
		filter.DeviceStatusFilter = statusFilters
	}

	if len(pbFilter.Models) > 0 {
		filter.ModelNames = pbFilter.Models
	}

	if len(pbFilter.GroupIds) > 0 {
		filter.GroupIDs = pbFilter.GroupIds
	}

	if len(pbFilter.RackIds) > 0 {
		filter.RackIDs = pbFilter.RackIds
	}

	if len(pbFilter.FirmwareVersions) > 0 {
		if len(pbFilter.FirmwareVersions) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"firmware_versions exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		filter.FirmwareVersions = pbFilter.FirmwareVersions
	}

	if len(pbFilter.Zones) > 0 {
		if len(pbFilter.Zones) > maxFreeFormFilterValues {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"zones exceeds maximum of %d values", maxFreeFormFilterValues)
		}
		filter.Zones = pbFilter.Zones
	}

	return filter, nil
}

// maxFreeFormFilterValues caps the size of free-form repeated-string filter
// arrays (firmware_versions, zones). Real fleets have a handful of distinct
// firmware versions or zones; arbitrarily large arrays from a misbehaving or
// hostile client would balloon Postgres planner cost on `= ANY($N::text[])`.
const maxFreeFormFilterValues = 1024

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
	case "UPDATING":
		return pb.DeviceStatus_DEVICE_STATUS_UPDATING
	case "REBOOT_REQUIRED":
		return pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED
	default:
		return pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
	}
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
// Username matching tries the exact stored username first, then falls back to the
// base username before the first dot so normalized Fleet pools still match miner
// usernames that include an appended worker suffix.
func findMatchingFleetPoolID(url, username string, fleetPools []*poolspb.Pool) *int64 {
	for _, candidate := range poolUsernameMatchCandidates(username) {
		for _, pool := range fleetPools {
			if pool.Url == url && candidate == pool.Username {
				return &pool.PoolId
			}
		}
	}
	return nil
}

func poolUsernameMatchCandidates(username string) []string {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return []string{""}
	}

	firstSeparator := strings.Index(trimmed, ".")
	if firstSeparator < 0 {
		return []string{trimmed}
	}

	baseUsername := strings.TrimSpace(trimmed[:firstSeparator])
	if baseUsername == trimmed {
		return []string{trimmed}
	}

	return []string{trimmed, baseUsername}
}

// DeleteMiners soft-deletes devices from the fleet and attempts best-effort ClearAuthKey on Proto devices.
// The DB deletion always succeeds immediately. ClearAuthKey runs in background goroutines and
// failures are logged but never surfaced to the caller.
func (s *Service) DeleteMiners(ctx context.Context, req *pb.DeleteMinersRequest) (*pb.DeleteMinersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.ResolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	if len(deviceIdentifiers) == 0 {
		return &pb.DeleteMinersResponse{DeletedCount: 0}, nil
	}

	// Collect Proto miner objects BEFORE soft-delete (lookups filter deleted_at IS NULL)
	miners := s.collectProtoMinersForClearAuthKey(ctx, deviceIdentifiers)

	// SoftDeleteDevices verifies ownership and deletes in a single transaction
	// to prevent TOCTOU races between the check and the delete.
	deletedCount, err := s.deviceStore.SoftDeleteDevices(ctx, deviceIdentifiers, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	for _, id := range deviceIdentifiers {
		s.minerService.InvalidateMiner(mm.DeviceIdentifier(id))
	}

	if err := s.telemetry.RemoveDevices(ctx, telemetryModels.ToDeviceIdentifiers(deviceIdentifiers)...); err != nil {
		slog.Warn("failed to remove devices from telemetry scheduler", "error", err)
	}

	count := int(deletedCount)
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "unpair_miners",
		Description:    "Unpaired miners",
		ScopeCount:     &count,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	// Best-effort background ClearAuthKey for Proto rigs using a bounded worker pool.
	// Workers are tracked by s.backgroundWg so the server can await completion
	// during graceful shutdown via WaitForPendingClearAuthKeys.
	// The shared semaphore limits total concurrent RPCs across all delete calls.
	if len(miners) > 0 {
		minerCh := make(chan minerInterfaces.Miner, len(miners))
		for _, m := range miners {
			minerCh <- m
		}
		close(minerCh)

		numWorkers := min(len(miners), concurrentClearAuthKeyLimit)
		for range numWorkers {
			s.backgroundWg.Add(1)
			go func() {
				defer s.backgroundWg.Done()
				for miner := range minerCh {
					s.clearAuthKeySem <- struct{}{}

					clearCtx, cancel := context.WithTimeout(context.Background(), clearAuthKeyTimeout)
					err := miner.Unpair(clearCtx)
					cancel()
					if err != nil {
						slog.Warn("best-effort ClearAuthKey failed", "deviceID", miner.GetID(), "error", err)
					}

					<-s.clearAuthKeySem
				}
			}()
		}
	}

	cappedCount := min(deletedCount, math.MaxInt32)

	// #nosec G115 -- Capped to math.MaxInt32 on the line above, safe for int32
	return &pb.DeleteMinersResponse{DeletedCount: int32(cappedCount)}, nil
}

// ResolveDeviceIdentifiers resolves a fleetmanagement DeviceSelector (with rich filter support)
// into a list of device identifiers.
func (s *Service) ResolveDeviceIdentifiers(ctx context.Context, selector *pb.DeviceSelector, orgID int64) ([]string, error) {
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}

	switch sel := selector.SelectionType.(type) {
	case *pb.DeviceSelector_IncludeDevices:
		return s.deviceResolver.ResolveExplicitDevices(ctx, sel.IncludeDevices, orgID)

	case *pb.DeviceSelector_AllDevices:
		filter, err := parseFilter(sel.AllDevices)
		if err != nil {
			return nil, err
		}
		return s.deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, filter)

	default:
		return nil, fleeterror.NewInvalidArgumentError("device_selector must specify a selection_type")
	}
}

// collectProtoMinersForClearAuthKey collects Miner objects only for Proto rigs.
// Per the RFC, ClearAuthKey is only attempted for Proto devices; 3rd-party miners
// (Antminer, etc.) require no device communication on delete.
func (s *Service) collectProtoMinersForClearAuthKey(ctx context.Context, deviceIdentifiers []string) []minerInterfaces.Miner {
	var miners []minerInterfaces.Miner
	for _, id := range deviceIdentifiers {
		m, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(id))
		if err != nil {
			slog.Debug("skipping ClearAuthKey for device", "deviceID", id, "error", err)
			continue
		}
		if m.GetDriverName() != mm.DriverNameProto {
			continue
		}
		miners = append(miners, m)
	}
	return miners
}

// isMinerNotFoundError checks if an error from the miner service indicates the device was not found.
func isMinerNotFoundError(err error) bool {
	return fleeterror.IsNotFoundError(err)
}
