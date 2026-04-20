package fleetmanagement

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

const (
	exportUnsupportedMetricValue = "N/A"
	temperaturePlaceholder       = "{{temperature}}"
	csvStatusNeedsAttention      = "Needs attention"
)

var exportHeaders = []string{
	"Name",
	"Worker Name",
	"Groups",
	"Rack",
	"Model",
	"MAC Address",
	"IP Address",
	"Status",
	"Issues",
	"Hashrate (TH/s)",
	"Efficiency (J/TH)",
	"Power (kW)",
	temperaturePlaceholder,
	"Firmware",
}

func (s *Service) ExportMinerListCsv(ctx context.Context, req *pb.ExportMinerListCsvRequest, send func(*pb.ExportMinerListCsvResponse) error) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	filter, err := parseFilter(req.Filter)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to parse filter parameters: %v", err)
	}

	filter.PairingStatuses = []pb.PairingStatus{
		pb.PairingStatus_PAIRING_STATUS_PAIRED,
		pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
	}
	// Export uses default name-ASC order for cross-page consistency.
	temperatureUnit := normalizeCSVTemperatureUnit(req.TemperatureUnit)

	cursor := ""
	isFirstChunk := true

	for {
		snapshots, nextCursor, _, err := s.buildSnapshotsFromUnifiedQuery(ctx, info.OrganizationID, cursor, maxPageSize, filter, nil)
		if err != nil {
			return err
		}

		pairedIDs := collectPairedDeviceIdentifiers(snapshots)
		allIDs := collectAllDeviceIdentifiers(snapshots)
		s.populateTelemetryData(ctx, snapshots, pairedIDs)
		s.populateGroupLabels(ctx, info.OrganizationID, snapshots, allIDs)
		s.populateRackDetails(ctx, info.OrganizationID, snapshots, allIDs)

		errorsByDevice, err := s.listOpenErrorsByDevice(ctx, info.OrganizationID, allIDs)
		if err != nil {
			return err
		}

		buffer := &bytes.Buffer{}

		if isFirstChunk {
			buffer.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM for Excel compatibility
		}

		writer := csv.NewWriter(buffer)

		if isFirstChunk {
			if err := writer.Write(buildExportHeaders(temperatureUnit)); err != nil {
				return fleeterror.NewInternalErrorf("failed to write csv header: %v", err)
			}
			isFirstChunk = false
		}

		for _, snapshot := range snapshots {
			if err := writer.Write(buildMinerCSVRow(snapshot, errorsByDevice[snapshot.DeviceIdentifier], temperatureUnit)); err != nil {
				return fleeterror.NewInternalErrorf("failed to write csv row: %v", err)
			}
		}

		writer.Flush()
		if err := writer.Error(); err != nil {
			return fleeterror.NewInternalErrorf("failed to flush csv data: %v", err)
		}

		chunk := &pb.ExportMinerListCsvResponse{
			CsvData: buffer.Bytes(),
		}

		if err := send(chunk); err != nil {
			return err
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return nil
}

func collectAllDeviceIdentifiers(snapshots []*pb.MinerStateSnapshot) []string {
	ids := make([]string, 0, len(snapshots))
	for _, s := range snapshots {
		ids = append(ids, s.DeviceIdentifier)
	}
	return ids
}

func (s *Service) listOpenErrorsByDevice(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
) (map[string][]diagnosticsmodels.ErrorMessage, error) {
	errorsByDevice := make(map[string][]diagnosticsmodels.ErrorMessage)
	if len(deviceIdentifiers) == 0 {
		return errorsByDevice, nil
	}

	pageToken := ""
	for {
		errors, err := s.errorStore.QueryErrors(ctx, &diagnosticsmodels.QueryOptions{
			OrgID:     orgID,
			PageSize:  diagnostics.MaxPageSize,
			PageToken: pageToken,
			Filter: &diagnosticsmodels.QueryFilter{
				DeviceIdentifiers: deviceIdentifiers,
				IncludeClosed:     false,
				Logic:             diagnosticsmodels.FilterLogicAND,
			},
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to query miner errors: %v", err)
		}

		for _, errorMessage := range errors {
			errorsByDevice[errorMessage.DeviceID] = append(errorsByDevice[errorMessage.DeviceID], errorMessage)
		}

		nextPageToken := diagnostics.BuildNextPageToken(errors, diagnostics.MaxPageSize)
		if nextPageToken == "" {
			break
		}
		pageToken = nextPageToken
	}

	return errorsByDevice, nil
}

func buildMinerCSVRow(
	snapshot *pb.MinerStateSnapshot,
	errors []diagnosticsmodels.ErrorMessage,
	temperatureUnit pb.CsvTemperatureUnit,
) []string {
	return []string{
		sanitizeOrFallback(snapshot.Name, sanitizeCSVField(snapshot.DeviceIdentifier)),
		sanitizeOrFallback(snapshot.WorkerName, ""),
		sanitizeCSVField(strings.Join(snapshot.GroupLabels, ", ")),
		sanitizeOrFallback(snapshot.RackLabel, ""),
		sanitizeOrFallback(snapshot.Model, "-"),
		sanitizeOrFallback(snapshot.MacAddress, "-"),
		sanitizeOrFallback(snapshot.IpAddress, "-"),
		minerStatusCSVValue(snapshot, errors),
		minerIssuesCSVValue(snapshot, errors),
		measurementCSVValue(snapshot, snapshot.Hashrate),
		efficiencyCSVValue(snapshot),
		powerCSVValue(snapshot),
		temperatureCSVValue(snapshot, temperatureUnit),
		sanitizeOrFallback(snapshot.FirmwareVersion, "-"),
	}
}

func buildExportHeaders(temperatureUnit pb.CsvTemperatureUnit) []string {
	headers := make([]string, len(exportHeaders))
	for i, h := range exportHeaders {
		if h == temperaturePlaceholder {
			headers[i] = temperatureHeader(temperatureUnit)
		} else {
			headers[i] = h
		}
	}
	return headers
}

func minerStatusCSVValue(snapshot *pb.MinerStateSnapshot, errors []diagnosticsmodels.ErrorMessage) string {
	if snapshot.PairingStatus == pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED {
		return csvStatusNeedsAttention
	}

	switch snapshot.DeviceStatus {
	case pb.DeviceStatus_DEVICE_STATUS_OFFLINE:
		return "Offline"
	case pb.DeviceStatus_DEVICE_STATUS_INACTIVE, pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return "Sleeping"
	case pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL, pb.DeviceStatus_DEVICE_STATUS_ERROR:
		return csvStatusNeedsAttention
	case pb.DeviceStatus_DEVICE_STATUS_UPDATING:
		return csvStatusNeedsAttention
	case pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return csvStatusNeedsAttention
	case pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, pb.DeviceStatus_DEVICE_STATUS_ONLINE:
		// fall through to error/hashing check below
	}

	if len(errors) > 0 {
		return csvStatusNeedsAttention
	}

	return "Hashing"
}

func minerIssuesCSVValue(snapshot *pb.MinerStateSnapshot, errors []diagnosticsmodels.ErrorMessage) string {
	if snapshot.PairingStatus == pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED {
		return "Authentication required"
	}

	if snapshot.DeviceStatus == pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL {
		return "Pool required"
	}

	grouped := make(map[diagnosticsmodels.ComponentType][]diagnosticsmodels.ErrorMessage)
	for _, errorMessage := range errors {
		grouped[errorMessage.ComponentType] = append(grouped[errorMessage.ComponentType], errorMessage)
	}

	if len(grouped) == 0 {
		return ""
	}

	if len(grouped) > 1 {
		return "Multiple failures"
	}

	for componentType, groupedErrors := range grouped {
		if componentType == diagnosticsmodels.ComponentTypeUnspecified {
			if len(groupedErrors) == 1 {
				return "1 issue"
			}
			return fmt.Sprintf("%d issues", len(groupedErrors))
		}

		if len(groupedErrors) > 1 {
			return fmt.Sprintf("Multiple %s failures", minerIssueSingularName(componentType))
		}

		return fmt.Sprintf("%s failure", minerIssueDisplayName(componentType, groupedErrors[0].ComponentID))
	}

	return ""
}

func efficiencyCSVValue(snapshot *pb.MinerStateSnapshot) string {
	if snapshot.Capabilities == nil || snapshot.Capabilities.Telemetry == nil || !snapshot.Capabilities.Telemetry.EfficiencyReported {
		return exportUnsupportedMetricValue
	}

	return measurementCSVValue(snapshot, snapshot.Efficiency)
}

func powerCSVValue(snapshot *pb.MinerStateSnapshot) string {
	if snapshot.Capabilities == nil || snapshot.Capabilities.Telemetry == nil || !snapshot.Capabilities.Telemetry.PowerUsageReported {
		return exportUnsupportedMetricValue
	}

	return measurementCSVValue(snapshot, snapshot.PowerUsage)
}

func temperatureCSVValue(snapshot *pb.MinerStateSnapshot, temperatureUnit pb.CsvTemperatureUnit) string {
	if snapshot.PairingStatus == pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED {
		return ""
	}

	switch snapshot.DeviceStatus {
	case pb.DeviceStatus_DEVICE_STATUS_OFFLINE, pb.DeviceStatus_DEVICE_STATUS_INACTIVE, pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return "-"
	case pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return ""
	case pb.DeviceStatus_DEVICE_STATUS_UPDATING, pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return "-"
	case pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, pb.DeviceStatus_DEVICE_STATUS_ONLINE, pb.DeviceStatus_DEVICE_STATUS_ERROR:
		// fall through to measurement lookup below
	}

	value, ok := latestMeasurementValue(snapshot.Temperature)
	if !ok {
		return "-"
	}

	if temperatureUnit == pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT {
		value = (value * 9 / 5) + 32
		return formatDecimal(value)
	}

	return formatDecimal(value)
}

func measurementCSVValue(snapshot *pb.MinerStateSnapshot, measurements []*commonpb.Measurement) string {
	if snapshot.PairingStatus == pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED {
		return ""
	}

	switch snapshot.DeviceStatus {
	case pb.DeviceStatus_DEVICE_STATUS_OFFLINE, pb.DeviceStatus_DEVICE_STATUS_INACTIVE, pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return "-"
	case pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return ""
	case pb.DeviceStatus_DEVICE_STATUS_UPDATING, pb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return "-"
	case pb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED, pb.DeviceStatus_DEVICE_STATUS_ONLINE, pb.DeviceStatus_DEVICE_STATUS_ERROR:
		// fall through to measurement lookup below
	}

	value, ok := latestMeasurementValue(measurements)
	if !ok {
		return "-"
	}

	return formatDecimal(value)
}

func latestMeasurementValue(measurements []*commonpb.Measurement) (float64, bool) {
	var latest *commonpb.Measurement
	for _, measurement := range measurements {
		if measurement == nil {
			continue
		}
		if latest == nil {
			latest = measurement
			continue
		}

		currentTimestamp := measurement.GetTimestamp()
		latestTimestamp := latest.GetTimestamp()
		if currentTimestamp != nil && latestTimestamp != nil && currentTimestamp.AsTime().After(latestTimestamp.AsTime()) {
			latest = measurement
		}
	}

	if latest == nil {
		return 0, false
	}

	return latest.Value, true
}

func normalizeCSVTemperatureUnit(unit pb.CsvTemperatureUnit) pb.CsvTemperatureUnit {
	if unit == pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT {
		return unit
	}
	return pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS
}

func temperatureHeader(unit pb.CsvTemperatureUnit) string {
	if unit == pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT {
		return "Temp (°F)"
	}
	return "Temp (°C)"
}

func formatDecimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.3f", value)
}

func sanitizeOrFallback(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return sanitizeCSVField(value)
}

func sanitizeCSVField(value string) string {
	if len(value) == 0 {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + value
	}

	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		switch r {
		case '=', '+', '-', '@':
			return "'" + value
		}
		break
	}

	return value
}

func minerIssueDisplayName(componentType diagnosticsmodels.ComponentType, componentID *string) string {
	baseName := map[diagnosticsmodels.ComponentType]string{
		diagnosticsmodels.ComponentTypeHashBoards:   "Hashboard",
		diagnosticsmodels.ComponentTypePSU:          "PSU",
		diagnosticsmodels.ComponentTypeFans:         "Fan",
		diagnosticsmodels.ComponentTypeControlBoard: "Control board",
	}[componentType]

	if componentID == nil || *componentID == "" {
		return baseName
	}

	slot, err := strconv.Atoi(*componentID)
	if err != nil {
		return baseName
	}

	return fmt.Sprintf("%s %d", baseName, slot)
}

func minerIssueSingularName(componentType diagnosticsmodels.ComponentType) string {
	switch componentType {
	case diagnosticsmodels.ComponentTypeHashBoards:
		return "hashboard"
	case diagnosticsmodels.ComponentTypePSU:
		return "PSU"
	case diagnosticsmodels.ComponentTypeFans:
		return "fan"
	case diagnosticsmodels.ComponentTypeControlBoard:
		return "control board"
	case diagnosticsmodels.ComponentTypeUnspecified:
		return "issue"
	}
	return "issue"
}
