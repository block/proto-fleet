package telemetry

import (
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
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
)

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

// TODO: implement long-term solution to prevent NaN

// sanitizeFloat64 replaces NaN and Inf with zero at the protobuf serialization boundary.
// PostgreSQL can store NaN as DOUBLE PRECISION, and COALESCE(NaN, 0) returns NaN (NaN is
// not NULL). A single NaN in device_metrics poisons the entire CAGG bucket via AVG(), and
// flows unchecked through the Go aggregation layer to the protobuf response.
func sanitizeFloat64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
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
			displayValue := sanitizeFloat64(models.ConvertToDisplayUnits(aggValue.Value, metric.MeasurementType))

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
			BrokenCount:     statusCount.BrokenCount,
		}
	}
	return result
}
