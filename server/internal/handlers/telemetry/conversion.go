package telemetry

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	telemetryv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

const (
	defaultPageSize = 100
)

func deviceIDsToModels(deviceIDs []string) []models.DeviceIdentifier {
	result := make([]models.DeviceIdentifier, len(deviceIDs))
	for i, id := range deviceIDs {
		result[i] = models.DeviceIdentifier(id)
	}
	return result
}

func measurementTypesToModels(protoTypes []telemetryv1.MeasurementType) ([]models.MeasurementType, error) {
	measurementTypes := make([]models.MeasurementType, len(protoTypes))
	for i, mt := range protoTypes {
		domainType, err := measurementTypeToDomain(mt)
		if err != nil {
			return nil, err
		}
		measurementTypes[i] = domainType
	}
	return measurementTypes, nil
}

func timeRangeToModel(protoTimeRange *telemetryv1.TimeRange) models.TimeRange {
	timeRange := models.TimeRange{}
	if protoTimeRange != nil {
		if protoTimeRange.StartTime != nil {
			startTime := protoTimeRange.StartTime.AsTime()
			timeRange.StartTime = &startTime
		}
		if protoTimeRange.EndTime != nil {
			endTime := protoTimeRange.EndTime.AsTime()
			timeRange.EndTime = &endTime
		}
	}
	return timeRange
}

func optionalDuration(protoDuration interface{ AsDuration() time.Duration }) *time.Duration {
	if protoDuration == nil {
		return nil
	}
	duration := protoDuration.AsDuration()
	return &duration
}

func optionalInt32(protoInt32 *int32) *int {
	if protoInt32 == nil {
		return nil
	}
	result := int(*protoInt32)
	return &result
}

var (
	measurementTypeToProtoMap = map[models.MeasurementType]telemetryv1.MeasurementType{
		models.MeasurementTypeTemperature: telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
		models.MeasurementTypeHashrate:    telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
		models.MeasurementTypePower:       telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
		models.MeasurementTypeEfficiency:  telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
		models.MeasurementTypeFanSpeed:    telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED,
		models.MeasurementTypeVoltage:     telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE,
		models.MeasurementTypeCurrent:     telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT,
		models.MeasurementTypeUptime:      telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME,
		models.MeasurementTypeErrorRate:   telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE,
		models.MeasurementTypeUnknown:     telemetryv1.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED,
	}

	protoToMeasurementTypeMap = map[telemetryv1.MeasurementType]models.MeasurementType{
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE: models.MeasurementTypeTemperature,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE:    models.MeasurementTypeHashrate,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER:       models.MeasurementTypePower,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY:  models.MeasurementTypeEfficiency,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED:   models.MeasurementTypeFanSpeed,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE:     models.MeasurementTypeVoltage,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT:     models.MeasurementTypeCurrent,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME:      models.MeasurementTypeUptime,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE:  models.MeasurementTypeErrorRate,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED: models.MeasurementTypeUnknown,
	}

	aggregationTypeToProtoMap = map[models.AggregationType]telemetryv1.AggregationType{
		models.AggregationTypeAverage:    telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
		models.AggregationTypeMin:        telemetryv1.AggregationType_AGGREGATION_TYPE_MIN,
		models.AggregationTypeMax:        telemetryv1.AggregationType_AGGREGATION_TYPE_MAX,
		models.AggregationTypeSum:        telemetryv1.AggregationType_AGGREGATION_TYPE_SUM,
		models.AggregationTypeCount:      telemetryv1.AggregationType_AGGREGATION_TYPE_SUM,
		models.AggregationTypeTotal:      telemetryv1.AggregationType_AGGREGATION_TYPE_SUM,
		models.AggregationTypeMeanChange: telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
		models.AggregationTypeUnknown:    telemetryv1.AggregationType_AGGREGATION_TYPE_UNSPECIFIED,
	}

	protoToAggregationTypeMap = map[telemetryv1.AggregationType]models.AggregationType{
		telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE:        models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_MIN:            models.AggregationTypeMin,
		telemetryv1.AggregationType_AGGREGATION_TYPE_MAX:            models.AggregationTypeMax,
		telemetryv1.AggregationType_AGGREGATION_TYPE_SUM:            models.AggregationTypeSum,
		telemetryv1.AggregationType_AGGREGATION_TYPE_FIRST_QUARTILE: models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_MEDIAN:         models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_THIRD_QUARTILE: models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_FIRST:          models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_LAST:           models.AggregationTypeAverage,
		telemetryv1.AggregationType_AGGREGATION_TYPE_UNSPECIFIED:    models.AggregationTypeUnknown,
	}

	componentStatusToProtoMap = map[models.ComponentStatus]telemetryv1.ComponentStatus{
		models.ComponentStatusHealthy:  telemetryv1.ComponentStatus_COMPONENT_STATUS_HEALTHY,
		models.ComponentStatusWarning:  telemetryv1.ComponentStatus_COMPONENT_STATUS_WARNING,
		models.ComponentStatusCritical: telemetryv1.ComponentStatus_COMPONENT_STATUS_CRITICAL,
		models.ComponentStatusOffline:  telemetryv1.ComponentStatus_COMPONENT_STATUS_OFFLINE,
		models.ComponentStatusUnknown:  telemetryv1.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED,
	}

	protoToComponentStatusMap = map[telemetryv1.ComponentStatus]models.ComponentStatus{
		telemetryv1.ComponentStatus_COMPONENT_STATUS_HEALTHY:     models.ComponentStatusHealthy,
		telemetryv1.ComponentStatus_COMPONENT_STATUS_WARNING:     models.ComponentStatusWarning,
		telemetryv1.ComponentStatus_COMPONENT_STATUS_CRITICAL:    models.ComponentStatusCritical,
		telemetryv1.ComponentStatus_COMPONENT_STATUS_OFFLINE:     models.ComponentStatusOffline,
		telemetryv1.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED: models.ComponentStatusUnknown,
	}

	updateTypeToProtoMap = map[models.UpdateType]telemetryv1.UpdateType{
		models.UpdateTypeTelemetry:    telemetryv1.UpdateType_UPDATE_TYPE_TELEMETRY,
		models.UpdateTypeHeartbeat:    telemetryv1.UpdateType_UPDATE_TYPE_HEARTBEAT,
		models.UpdateTypeError:        telemetryv1.UpdateType_UPDATE_TYPE_ERROR,
		models.UpdateTypeDeviceStatus: telemetryv1.UpdateType_UPDATE_TYPE_DEVICE_STATUS,
		models.UpdateTypeUnknown:      telemetryv1.UpdateType_UPDATE_TYPE_UNSPECIFIED,
	}

	measurementStringToTypeMap = map[string]models.MeasurementType{
		"temperature_c":  models.MeasurementTypeTemperature,
		"hashrate_mhs":   models.MeasurementTypeHashrate,
		"power_w":        models.MeasurementTypePower,
		"efficiency_jth": models.MeasurementTypeEfficiency,
		"fan_rpm":        models.MeasurementTypeFanSpeed,
		"voltage_mv":     models.MeasurementTypeVoltage,
		"current_ma":     models.MeasurementTypeCurrent,
		"uptime":         models.MeasurementTypeUptime,
		"error_rate":     models.MeasurementTypeErrorRate,
	}

	measurementTypeToUnitMap = map[telemetryv1.MeasurementType]commonv1.MeasurementUnit{
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE: commonv1.MeasurementUnit_MEASUREMENT_UNIT_CELSIUS,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE:    commonv1.MeasurementUnit_MEASUREMENT_UNIT_TERAHASH_PER_SECOND,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER:       commonv1.MeasurementUnit_MEASUREMENT_UNIT_KILOWATT,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_JOULES_PER_TERAHASH,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_UPTIME:      commonv1.MeasurementUnit_MEASUREMENT_UNIT_HOURS,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_ERROR_RATE:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_PERCENTAGE,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_FAN_SPEED:   commonv1.MeasurementUnit_MEASUREMENT_UNIT_UNSPECIFIED,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_VOLTAGE:     commonv1.MeasurementUnit_MEASUREMENT_UNIT_UNSPECIFIED,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_CURRENT:     commonv1.MeasurementUnit_MEASUREMENT_UNIT_UNSPECIFIED,
		telemetryv1.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED: commonv1.MeasurementUnit_MEASUREMENT_UNIT_UNSPECIFIED,
	}
)

func toLatestTelemetryQuery(req *telemetryv1.GetSnapshotRequest) (models.LatestTelemetryQuery, error) {
	deviceIDs := deviceIDsToModels(req.DeviceIds)

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.LatestTelemetryQuery{}, err
	}

	query := models.LatestTelemetryQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		Tags:             req.Tags,
		MaxAge:           optionalDuration(req.MaxAge),
	}

	return query, nil
}

func toTimeSeriesTelemetryQuery(req *telemetryv1.GetTimeSeriesRequest) (models.TimeSeriesTelemetryQuery, error) {
	deviceIDs := deviceIDsToModels(req.DeviceIds)

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.TimeSeriesTelemetryQuery{}, err
	}

	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		TimeRange:        timeRangeToModel(req.TimeRange),
		Tags:             req.Tags,
		Limit:            optionalInt32(req.Limit),
	}

	return query, nil
}

func toMetadataQuery(req *telemetryv1.GetMetadataRequest) (models.MetadataQuery, error) {
	deviceIDs := deviceIDsToModels(req.DeviceIds)

	query := models.MetadataQuery{
		DeviceIDs: deviceIDs,
	}

	if req.StatusFilter != nil {
		status, err := componentStatusToDomain(*req.StatusFilter)
		if err != nil {
			return models.MetadataQuery{}, err
		}
		filter := &models.MetadataFilter{
			Tags:   req.TagFilters,
			Status: &status,
		}
		query.Filter = filter
	} else if len(req.TagFilters) > 0 {
		filter := &models.MetadataFilter{
			Tags: req.TagFilters,
		}
		query.Filter = filter
	}

	return query, nil
}

func toStreamQuery(req *telemetryv1.StreamUpdatesRequest) (models.StreamQuery, error) {
	deviceIDs := deviceIDsToModels(req.DeviceIds)

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.StreamQuery{}, err
	}

	query := models.StreamQuery{
		DeviceIDs:         deviceIDs,
		MeasurementTypes:  measurementTypes,
		IncludeHeartbeat:  req.IncludeHeartbeat,
		Tags:              req.Tags,
		HeartbeatInterval: optionalDuration(req.HeartbeatInterval),
	}

	return query, nil
}

func toAggregationQuery(req *telemetryv1.GetAggregatedSnapshotRequest) (models.AggregationQuery, error) {
	deviceIDs := deviceIDsToModels(req.DeviceIds)

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.AggregationQuery{}, err
	}

	aggregationType, err := aggregationTypeToDomain(req.AggregationType)
	if err != nil {
		return models.AggregationQuery{}, err
	}

	query := models.AggregationQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		TimeRange:        timeRangeToModel(req.TimeRange),
		AggregationType:  aggregationType,
		Tags:             req.Tags,
		GroupByInterval:  optionalDuration(req.GroupByInterval),
	}

	return query, nil
}

func toCombinedMetricsQuery(req *telemetryv1.GetCombinedMetricsRequest) (models.CombinedMetricsQuery, error) {
	var deviceIDs []models.DeviceIdentifier

	if req.DeviceSelector != nil {
		switch selector := req.DeviceSelector.SelectorValue.(type) {
		case *telemetryv1.DeviceSelector_AllDevices:
			deviceIDs = []models.DeviceIdentifier{}
		case *telemetryv1.DeviceSelector_DeviceList:
			if selector.DeviceList != nil {
				deviceIDs = deviceIDsToModels(selector.DeviceList.DeviceIds)
			}
		default:
			return models.CombinedMetricsQuery{}, fmt.Errorf("invalid device selector")
		}
	}

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.CombinedMetricsQuery{}, err
	}

	aggregationTypes, err := aggregationTypesToModels(req.Aggregations)
	if err != nil {
		return models.CombinedMetricsQuery{}, err
	}

	timeRange := models.TimeRange{}
	if req.StartTime != nil {
		startTime := req.StartTime.AsTime()
		timeRange.StartTime = &startTime
	}
	if req.EndTime != nil {
		endTime := req.EndTime.AsTime()
		timeRange.EndTime = &endTime
	}

	granularity := time.Duration(0)
	if req.Granularity != nil {
		granularity = req.Granularity.AsDuration()
	}

	pageSize := int(req.PageSize)
	if pageSize == 0 {
		pageSize = defaultPageSize
	}

	query := models.CombinedMetricsQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		AggregationTypes: aggregationTypes,
		TimeRange:        timeRange,
		Granularity:      granularity,
		PaginationToken:  req.PageToken,
		PageSize:         pageSize,
	}

	return query, nil
}

func aggregationTypesToModels(protoTypes []telemetryv1.AggregationType) ([]models.AggregationType, error) {
	aggregationTypes := make([]models.AggregationType, len(protoTypes))
	for i, at := range protoTypes {
		domainType, err := aggregationTypeToDomain(at)
		if err != nil {
			return nil, err
		}
		aggregationTypes[i] = domainType
	}
	return aggregationTypes, nil
}

func toStreamCombinedMetricsQuery(req *telemetryv1.StreamCombinedMetricUpdatesRequest) (models.StreamCombinedMetricsQuery, error) {
	var deviceIDs []models.DeviceIdentifier

	if req.DeviceSelector != nil {
		switch selector := req.DeviceSelector.SelectorValue.(type) {
		case *telemetryv1.DeviceSelector_AllDevices:
			deviceIDs = []models.DeviceIdentifier{}
		case *telemetryv1.DeviceSelector_DeviceList:
			if selector.DeviceList != nil {
				deviceIDs = deviceIDsToModels(selector.DeviceList.DeviceIds)
			}
		default:
			return models.StreamCombinedMetricsQuery{}, fmt.Errorf("invalid device selector")
		}
	}

	measurementTypes, err := measurementTypesToModels(req.Metrics)
	if err != nil {
		return models.StreamCombinedMetricsQuery{}, err
	}

	aggregationTypes, err := aggregationTypesToModels(req.Aggregations)
	if err != nil {
		return models.StreamCombinedMetricsQuery{}, err
	}

	granularity := time.Minute
	if req.Granularity != nil {
		granularity = req.Granularity.AsDuration()
	}

	updateInterval := granularity
	if req.UpdateInterval != nil {
		updateInterval = req.UpdateInterval.AsDuration()
	}

	query := models.StreamCombinedMetricsQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		AggregationTypes: aggregationTypes,
		Granularity:      granularity,
		UpdateInterval:   updateInterval,
	}

	return query, nil
}

func fromTelemetryData(telemetryData []models.Telemetry) ([]*telemetryv1.TelemetryData, error) {
	result := make([]*telemetryv1.TelemetryData, len(telemetryData))

	for i, data := range telemetryData {
		measurementType, err := measurementTypeToProto(getMeasurementTypeFromString(data.Measurement))
		if err != nil {
			return nil, err
		}

		deviceID, ok := data.Tags["device_id"]
		if !ok {
			return nil, fmt.Errorf("missing device_id tag for measurement %s", data.Measurement)
		}

		val, ok := data.Fields["value"]
		if !ok {
			return nil, fmt.Errorf("missing value field for measurement %s on device_id: %s", data.Measurement, deviceID)
		}
		value, ok := val.(float64)
		if !ok {
			return nil, fmt.Errorf("invalid value type for measurement %s on device_id: %s expected float64, got %T", data.Measurement, deviceID, val)
		}

		if measurementType == telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE {
			value /= 1e6 // Convert hashrate from MHS to THS
		}

		result[i] = &telemetryv1.TelemetryData{
			DeviceId:        deviceID,
			MeasurementType: measurementType,
			Value:           value,
			Unit:            getUnitForMeasurementType(measurementType),
			Timestamp:       timestamppb.New(data.Timestamp),
			Tags:            data.Tags,
		}
	}

	return result, nil
}

func fromDeviceMetadata(metadata []models.DeviceMetadata) ([]*telemetryv1.DeviceMetadata, error) {
	result := make([]*telemetryv1.DeviceMetadata, len(metadata))

	for i, meta := range metadata {
		status, err := componentStatusToProto(meta.Status)
		if err != nil {
			return nil, err
		}

		deviceMetadata := &telemetryv1.DeviceMetadata{
			DeviceId:     string(meta.DeviceID),
			LastSeen:     timestamppb.New(meta.LastSeen),
			Status:       status,
			Tags:         meta.Tags,
			Capabilities: meta.Capabilities,
		}

		if meta.DeviceType != "" {
			deviceMetadata.DeviceType = &meta.DeviceType
		}
		if meta.Location != "" {
			deviceMetadata.Location = &meta.Location
		}

		result[i] = deviceMetadata
	}

	return result, nil
}

func fromTelemetryUpdate(update models.TelemetryUpdate) (*telemetryv1.StreamUpdatesResponse, error) {
	updateType, err := updateTypeToProto(update.Type)
	if err != nil {
		return nil, err
	}

	telemetryUpdate := &telemetryv1.TelemetryUpdate{
		Type:      updateType,
		Timestamp: timestamppb.New(update.Timestamp),
	}

	if update.DeviceID != "" {
		deviceID := string(update.DeviceID)
		telemetryUpdate.DeviceId = &deviceID
	}

	if update.Data != nil {
		telemetryData, err := fromTelemetryData([]models.Telemetry{*update.Data})
		if err != nil {
			return nil, err
		}
		if len(telemetryData) > 0 {
			telemetryUpdate.Data = telemetryData[0]
		}
	}

	if update.Error != nil {
		telemetryUpdate.ErrorMessage = update.Error
	}

	if update.Status != nil {
		status, err := componentStatusToProto(*update.Status)
		if err != nil {
			return nil, err
		}
		telemetryUpdate.Status = &status
	}

	if update.DeviceStatus != nil {
		deviceStatus := telemetryv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
		switch *update.DeviceStatus {
		case mm.MinerStatusActive:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_ONLINE
		case mm.MinerStatusInactive:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_INACTIVE
		case mm.MinerStatusError:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_ERROR
		case mm.MinerStatusMaintenance:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_MAINTENANCE
		case mm.MinerStatusUnknown:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_UNSPECIFIED
		case mm.MinerStatusOffline:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_OFFLINE
		}
		telemetryUpdate.DeviceStatus = &deviceStatus
	}

	return &telemetryv1.StreamUpdatesResponse{
		Update: telemetryUpdate,
	}, nil
}

func fromAggregatedTelemetry(aggregatedData []models.AggregatedTelemetry) ([]*telemetryv1.AggregatedTelemetry, error) {
	result := make([]*telemetryv1.AggregatedTelemetry, len(aggregatedData))

	for i, data := range aggregatedData {
		measurementType, err := measurementTypeToProto(data.MeasurementType)
		if err != nil {
			return nil, err
		}

		aggregationType, err := aggregationTypeToProto(data.AggregationType)
		if err != nil {
			return nil, err
		}

		var dataPoints int32
		if data.DataPoints <= math.MaxInt32 && data.DataPoints >= 0 {
			dataPoints = int32(data.DataPoints)
		} else if data.DataPoints > math.MaxInt32 {
			slog.Debug("Data points exceed max int32, setting to max value",
				"device_id", data.DeviceID,
				"measurement_type", data.MeasurementType,
				"aggregation_type", data.AggregationType,
				"data_points", data.DataPoints,
			)
			dataPoints = math.MaxInt32
		} else {
			slog.Debug("Data points are negative, setting to 0",
				"device_id", data.DeviceID,
				"measurement_type", data.MeasurementType,
				"aggregation_type", data.AggregationType,
				"data_points", data.DataPoints,
			)
			dataPoints = 0
		}

		timeWindow := &telemetryv1.TimeRange{}
		if !data.TimeWindow.StartTime.IsZero() {
			timeWindow.StartTime = timestamppb.New(data.TimeWindow.StartTime)
		}
		if !data.TimeWindow.EndTime.IsZero() {
			timeWindow.EndTime = timestamppb.New(data.TimeWindow.EndTime)
		}

		result[i] = &telemetryv1.AggregatedTelemetry{
			DeviceId:        string(data.DeviceID),
			MeasurementType: measurementType,
			Value:           data.Value,
			AggregationType: aggregationType,
			DataPoints:      dataPoints,
			TimeWindow:      timeWindow,
			Tags:            data.Tags,
		}
	}

	return result, nil
}

func measurementTypeToDomain(protoType telemetryv1.MeasurementType) (models.MeasurementType, error) {
	if domainType, ok := protoToMeasurementTypeMap[protoType]; ok {
		return domainType, nil
	}
	return models.MeasurementTypeUnknown, fmt.Errorf("unknown measurement type: %v", protoType)
}

func measurementTypeToProto(domainType models.MeasurementType) (telemetryv1.MeasurementType, error) {
	if protoType, ok := measurementTypeToProtoMap[domainType]; ok {
		return protoType, nil
	}
	return telemetryv1.MeasurementType_MEASUREMENT_TYPE_UNSPECIFIED, fmt.Errorf("unknown measurement type: %v", domainType)
}

func aggregationTypeToDomain(protoType telemetryv1.AggregationType) (models.AggregationType, error) {
	if domainType, ok := protoToAggregationTypeMap[protoType]; ok {
		return domainType, nil
	}
	return models.AggregationTypeUnknown, fmt.Errorf("unknown aggregation type: %v", protoType)
}

func aggregationTypeToProto(domainType models.AggregationType) (telemetryv1.AggregationType, error) {
	if protoType, ok := aggregationTypeToProtoMap[domainType]; ok {
		return protoType, nil
	}
	return telemetryv1.AggregationType_AGGREGATION_TYPE_UNSPECIFIED, fmt.Errorf("unknown aggregation type: %v", domainType)
}

func componentStatusToDomain(protoStatus telemetryv1.ComponentStatus) (models.ComponentStatus, error) {
	if domainStatus, ok := protoToComponentStatusMap[protoStatus]; ok {
		return domainStatus, nil
	}
	return models.ComponentStatusUnknown, fmt.Errorf("unknown component status: %v", protoStatus)
}

func componentStatusToProto(domainStatus models.ComponentStatus) (telemetryv1.ComponentStatus, error) {
	if protoStatus, ok := componentStatusToProtoMap[domainStatus]; ok {
		return protoStatus, nil
	}
	return telemetryv1.ComponentStatus_COMPONENT_STATUS_UNSPECIFIED, fmt.Errorf("unknown component status: %v", domainStatus)
}

func updateTypeToProto(domainType models.UpdateType) (telemetryv1.UpdateType, error) {
	if protoType, ok := updateTypeToProtoMap[domainType]; ok {
		return protoType, nil
	}
	return telemetryv1.UpdateType_UPDATE_TYPE_UNSPECIFIED, fmt.Errorf("unknown update type: %v", domainType)
}

func getMeasurementTypeFromString(measurement string) models.MeasurementType {
	if measurementType, ok := measurementStringToTypeMap[measurement]; ok {
		return measurementType
	}
	return models.MeasurementTypeUnknown
}

func getUnitForMeasurementType(measurementType telemetryv1.MeasurementType) commonv1.MeasurementUnit {
	if unit, ok := measurementTypeToUnitMap[measurementType]; ok {
		return unit
	}
	return commonv1.MeasurementUnit_MEASUREMENT_UNIT_UNSPECIFIED
}

func fromCombinedMetrics(combinedMetrics models.CombinedMetric) (*telemetryv1.GetCombinedMetricsResponse, error) {
	metrics := make([]*telemetryv1.Metric, len(combinedMetrics.Metrics))

	for i, metric := range combinedMetrics.Metrics {
		measurementType, err := measurementTypeToProto(metric.MeasurementType)
		if err != nil {
			return nil, err
		}

		aggregatedValues := make([]*telemetryv1.AggregatedValue, len(metric.AggregatedValues))
		for j, aggValue := range metric.AggregatedValues {
			aggregationType, err := aggregationTypeToProto(aggValue.Type)
			if err != nil {
				return nil, err
			}

			aggregatedValues[j] = &telemetryv1.AggregatedValue{
				AggregationType: aggregationType,
				Value:           aggValue.Value,
			}
		}

		metrics[i] = &telemetryv1.Metric{
			MeasurementType:  measurementType,
			OpenTime:         timestamppb.New(metric.OpenTime),
			AggregatedValues: aggregatedValues,
		}
	}

	return &telemetryv1.GetCombinedMetricsResponse{
		Metrics:       metrics,
		NextPageToken: combinedMetrics.NextPageToken,
	}, nil
}
