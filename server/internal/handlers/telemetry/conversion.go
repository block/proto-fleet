package telemetry

import (
	"fmt"
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

	updateTypeToProtoMap = map[models.UpdateType]telemetryv1.UpdateType{
		models.UpdateTypeTelemetry:        telemetryv1.UpdateType_UPDATE_TYPE_TELEMETRY,
		models.UpdateTypeHeartbeat:        telemetryv1.UpdateType_UPDATE_TYPE_HEARTBEAT,
		models.UpdateTypeError:            telemetryv1.UpdateType_UPDATE_TYPE_ERROR,
		models.UpdateTypeDeviceStatus:     telemetryv1.UpdateType_UPDATE_TYPE_DEVICE_STATUS,
		models.UpdateTypeMinerStateCounts: telemetryv1.UpdateType_UPDATE_TYPE_MINER_STATE_COUNTS,
		models.UpdateTypeUnknown:          telemetryv1.UpdateType_UPDATE_TYPE_UNSPECIFIED,
	}

	measurementStringToTypeMap = map[string]models.MeasurementType{
		"temperature_c": models.MeasurementTypeTemperature,
		"hashrate_mhs":  models.MeasurementTypeHashrate,
		"power_w":       models.MeasurementTypePower,
		"efficiency_jh": models.MeasurementTypeEfficiency,
		"fan_rpm":       models.MeasurementTypeFanSpeed,
		"voltage_mv":    models.MeasurementTypeVoltage,
		"current_ma":    models.MeasurementTypeCurrent,
		"uptime":        models.MeasurementTypeUptime,
		"error_rate":    models.MeasurementTypeErrorRate,
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

func toStreamQuery(req *telemetryv1.StreamUpdatesRequest) (models.StreamQuery, error) {
	deviceIDs := models.ToDeviceIdentifiers(req.DeviceIds)

	measurementTypes, err := measurementTypesToModels(req.MeasurementTypes)
	if err != nil {
		return models.StreamQuery{}, err
	}

	query := models.StreamQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: measurementTypes,
		IncludeHeartbeat: req.IncludeHeartbeat,
		Tags:             req.Tags,
		HeartbeatInterval: func() *time.Duration {
			if req.HeartbeatInterval == nil {
				return nil
			}
			d := req.HeartbeatInterval.AsDuration()
			return &d
		}(),
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
				deviceIDs = models.ToDeviceIdentifiers(selector.DeviceList.DeviceIds)
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
		SlideInterval:    &granularity,
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
				deviceIDs = models.ToDeviceIdentifiers(selector.DeviceList.DeviceIds)
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

func fromTelemetryUpdate(update models.TelemetryUpdate) (*telemetryv1.StreamUpdatesResponse, error) {
	updateType, err := updateTypeToProto(update.Type)
	if err != nil {
		return nil, err
	}

	telemetryUpdate := &telemetryv1.TelemetryUpdate{
		Type:      updateType,
		Timestamp: timestamppb.New(update.Timestamp),
	}

	// Note: proto API uses "device_id" field but it actually contains the device identifier string,
	// not the database primary key. This naming is kept for backwards compatibility.
	if update.DeviceIdentifier != "" {
		deviceID := string(update.DeviceIdentifier)
		telemetryUpdate.DeviceId = &deviceID
	}

	if update.MeasurementName != "" {
		domainMeasurementType := getMeasurementTypeFromString(update.MeasurementName)
		measurementType, err := measurementTypeToProto(domainMeasurementType)
		if err != nil {
			return nil, err
		}
		deviceID := string(update.DeviceIdentifier)
		// Convert raw storage units to display units (H/s → TH/s, W → kW, J/H → J/TH)
		displayValue := models.ConvertToDisplayUnits(update.MeasurementValue, domainMeasurementType)
		telemetryUpdate.Data = &telemetryv1.TelemetryData{
			DeviceId:        deviceID,
			MeasurementType: measurementType,
			Value:           displayValue,
			Unit:            getUnitForMeasurementType(measurementType),
			Timestamp:       timestamppb.New(update.Timestamp),
			Tags:            map[string]string{"device_id": deviceID},
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
		case mm.MinerStatusNeedsMiningPool:
			deviceStatus = telemetryv1.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
		}
		telemetryUpdate.DeviceStatus = &deviceStatus
	}

	if update.MinerStateCounts != nil {
		telemetryUpdate.MinerStateCounts = &telemetryv1.MinerStateCounts{
			HashingCount:  update.MinerStateCounts.Hashing,
			BrokenCount:   update.MinerStateCounts.Broken,
			OfflineCount:  update.MinerStateCounts.Offline,
			SleepingCount: update.MinerStateCounts.Sleeping,
		}
	}

	return &telemetryv1.StreamUpdatesResponse{
		Update: telemetryUpdate,
	}, nil
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
	metrics, err := convertMetricsToProto(combinedMetrics.Metrics)
	if err != nil {
		return nil, err
	}

	return &telemetryv1.GetCombinedMetricsResponse{
		Metrics:                 metrics,
		NextPageToken:           combinedMetrics.NextPageToken,
		TemperatureStatusCounts: convertTemperatureStatusCounts(combinedMetrics.TemperatureStatusCounts),
		UptimeStatusCounts:      convertUptimeStatusCounts(combinedMetrics.UptimeStatusCounts),
	}, nil
}

func convertMetricsToProto(domainMetrics []models.Metric) ([]*telemetryv1.Metric, error) {
	metrics := make([]*telemetryv1.Metric, len(domainMetrics))

	for i, metric := range domainMetrics {
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

			// Convert raw storage units to display units (H/s → TH/s, W → kW, J/H → J/TH)
			displayValue := models.ConvertToDisplayUnits(aggValue.Value, metric.MeasurementType)

			aggregatedValues[j] = &telemetryv1.AggregatedValue{
				AggregationType: aggregationType,
				Value:           displayValue,
			}
		}

		metrics[i] = &telemetryv1.Metric{
			MeasurementType:  measurementType,
			OpenTime:         timestamppb.New(metric.OpenTime),
			AggregatedValues: aggregatedValues,
			DeviceCount:      metric.DeviceCount,
		}
	}

	return metrics, nil
}

func convertTemperatureStatusCounts(statusCounts []models.TemperatureStatusCount) []*telemetryv1.TemperatureStatusCount {
	if len(statusCounts) == 0 {
		return nil
	}

	result := make([]*telemetryv1.TemperatureStatusCount, len(statusCounts))
	for i, statusCount := range statusCounts {
		result[i] = &telemetryv1.TemperatureStatusCount{
			Timestamp:     timestamppb.New(statusCount.Timestamp),
			ColdCount:     statusCount.ColdCount,
			OkCount:       statusCount.OkCount,
			HotCount:      statusCount.HotCount,
			CriticalCount: statusCount.CriticalCount,
		}
	}
	return result
}

func convertUptimeStatusCounts(statusCounts []models.UptimeStatusCount) []*telemetryv1.UptimeStatusCount {
	if len(statusCounts) == 0 {
		return nil
	}

	result := make([]*telemetryv1.UptimeStatusCount, len(statusCounts))
	for i, statusCount := range statusCounts {
		result[i] = &telemetryv1.UptimeStatusCount{
			Timestamp:       timestamppb.New(statusCount.Timestamp),
			HashingCount:    statusCount.HashingCount,
			NotHashingCount: statusCount.NotHashingCount,
		}
	}
	return result
}
