package influxdb

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	influxModels "github.com/btc-mining/proto-fleet/server/internal/infrastructure/influxdb/models"
)

const (
	defaultRetryAttempts  = 3
	defaultRetryDelay     = 100 * time.Millisecond
	maxPointsPerWrite     = 1000
	defaultParamsLimit    = 1000
	defaultMaxAge         = 24 * time.Hour
	defaultStartTime      = -24 * time.Hour
	defaultPollInterval   = 1 * time.Second
	defaultBufferSize     = 100
	defaultWindowDuration = 2 * time.Minute
	defaultSlideInterval  = 10 * time.Second
)

var _ telemetry.TelemetryDataStore = &InfluxTelemetryStore{}

type InfluxTelemetryStore struct {
	client *influxdb3.Client
	config Config
	logger *slog.Logger
}

func NewTelemetryStore(config Config) (*InfluxTelemetryStore, error) {
	if err := validateConfig(config); err != nil {
		return nil, newTelemetryConfigError(err)
	}

	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:         config.URL,
		Token:        config.Token,
		Organization: config.Organization,
		Database:     config.Bucket,
	})
	if err != nil {
		return nil, newTelemetryConnectionError(err)
	}

	store := &InfluxTelemetryStore{
		client: client,
		config: config,
		logger: slog.With("component", "influx_telemetry_store"),
	}

	return store, nil
}

// executeWithFallback has been removed - we now only use device_metrics table

// queryDeviceMetrics executes a device_metrics query and returns DeviceMetrics
func (s *InfluxTelemetryStore) queryDeviceMetrics(
	ctx context.Context,
	queryTemplate string,
	deviceIDs []models.DeviceIdentifier,
	params influxdb3.QueryParameters,
	methodName string,
) ([]modelsV2.DeviceMetrics, error) {
	// Only format the query if it contains a placeholder
	var influxQuery string
	if strings.Contains(queryTemplate, "%s") {
		deviceIDsStr := s.buildDeviceIDsString(deviceIDs)
		influxQuery = fmt.Sprintf(queryTemplate, deviceIDsStr)
	} else {
		influxQuery = queryTemplate
	}

	iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
	if err != nil {
		return nil, fmt.Errorf("device_metrics query failed: %w", err)
	}

	var results []modelsV2.DeviceMetrics
	for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
		if err != nil {
			s.logger.Debug(fmt.Sprintf("error reading point in %s", methodName),
				slog.Any("error", err))
			continue
		}

		deviceMetrics, err := influxModels.ToDeviceMetrics(point)
		if err != nil {
			s.logger.Error(fmt.Sprintf("error converting point to DeviceMetrics in %s", methodName),
				slog.Any("error", err))
			continue
		}

		results = append(results, deviceMetrics)
	}

	return results, nil
}

// filterLatestByDevice keeps only the most recent DeviceMetrics per device
func filterLatestByDevice(metrics []modelsV2.DeviceMetrics) []modelsV2.DeviceMetrics {
	latestMetricsByDevice := make(map[models.DeviceIdentifier]modelsV2.DeviceMetrics)

	for _, deviceMetrics := range metrics {
		deviceID := models.DeviceIdentifier(deviceMetrics.DeviceID)
		if existing, exists := latestMetricsByDevice[deviceID]; !exists || deviceMetrics.Timestamp.After(existing.Timestamp) {
			latestMetricsByDevice[deviceID] = deviceMetrics
		}
	}

	var results []modelsV2.DeviceMetrics
	for _, metrics := range latestMetricsByDevice {
		results = append(results, metrics)
	}

	return results
}

// sortTelemetryByTimeDesc sorts telemetry by timestamp descending (most recent first)
func sortTelemetryByTimeDesc(data []models.Telemetry) {
	sort.Slice(data, func(i, j int) bool {
		return data[i].Timestamp.After(data[j].Timestamp)
	})
}

// sortTelemetryByTimeAsc sorts telemetry by timestamp ascending (oldest first)
func sortTelemetryByTimeAsc(data []models.Telemetry) {
	sort.Slice(data, func(i, j int) bool {
		return data[i].Timestamp.Before(data[j].Timestamp)
	})
}

func (s *InfluxTelemetryStore) Store(ctx context.Context, data ...models.Telemetry) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > maxPointsPerWrite {
		return newTelemetryWriteError(fmt.Errorf("too many points to write: %d, max is %d", len(data), maxPointsPerWrite), len(data))
	}

	startTime := time.Now()
	var finalErr error
	defer func() {
		duration := time.Since(startTime)
		s.logWrite(len(data), duration, finalErr)
	}()

	points := make([]*influxdb3.Point, 0, len(data))
	for _, telemetry := range data {
		point := influxModels.ToInfluxPoint(telemetry)
		points = append(points, point)
	}

	baseDelay := s.config.RetryDelay
	maxAttempts := s.config.RetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultRetryAttempts
	}
	if baseDelay <= 0 {
		baseDelay = defaultRetryDelay
	}

	var err error

	for attempt := range maxAttempts {
		err = s.client.WritePoints(ctx, points)
		if err == nil {
			s.logger.Debug("telemetry write succeeded",
				slog.Int("point_count", len(points)),
				slog.Int("retry_attempt", attempt))
			return nil
		}

		if !isRetryableError(err) {
			s.logger.Error("non-retryable error writing telemetry points",
				slog.Any("error", err),
				slog.Int("point_count", len(points)),
				slog.Int("attempt", attempt+1))
			finalErr = newTelemetryWriteError(err, len(points))
			return finalErr
		}

		multiplier := 1 << attempt
		delay := time.Duration(int64(baseDelay) * int64(multiplier))

		s.logger.Debug("retryable error writing telemetry points, retrying",
			slog.Any("error", err),
			slog.Int("point_count", len(points)),
			slog.Int("attempt", attempt+1),
			slog.Duration("retry_delay", delay))

		select {
		case <-ctx.Done():
			finalErr = newTelemetryWriteError(ctx.Err(), len(points))
			return finalErr
		case <-time.After(delay):
		}
	}

	s.logger.Error("failed to write telemetry points after all retries",
		slog.Any("error", err),
		slog.Int("point_count", len(points)),
		slog.Int("max_attempts", maxAttempts))
	finalErr = newTelemetryWriteErrorWithRetry(err, len(points), maxAttempts)
	return finalErr
}

const getLatestDeviceMetricsForMultipleDevicesQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time >= $max_age
ORDER BY time DESC
`

const getLatestDeviceMetricsForAllDevicesQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE time >= $max_age
ORDER BY time DESC
`

func (s *InfluxTelemetryStore) GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	return s.getLatestTelemetryFromDeviceMetrics(ctx, query)
}

func (s *InfluxTelemetryStore) getLatestTelemetryFromDeviceMetrics(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	params := s.getLatestTelemetryParamsForMeasurement(query)

	// Choose the appropriate query based on whether device IDs are specified
	var queryTemplate string
	if len(query.DeviceIDs) > 0 {
		queryTemplate = getLatestDeviceMetricsForMultipleDevicesQuery
	} else {
		// Use the query that doesn't filter by device_id for "all devices" case
		queryTemplate = getLatestDeviceMetricsForAllDevicesQuery
	}

	// Query device_metrics and get all matching points
	allMetrics, err := s.queryDeviceMetrics(ctx, queryTemplate, query.DeviceIDs, params, "getLatestTelemetryFromDeviceMetrics")
	if err != nil {
		return nil, err
	}

	// Keep only the latest metrics per device
	latestMetrics := filterLatestByDevice(allMetrics)

	// Convert DeviceMetrics to legacy Telemetry format
	var results []models.Telemetry
	for _, deviceMetrics := range latestMetrics {
		telemetryPoints := s.convertDeviceMetricsToTelemetry(deviceMetrics, query.MeasurementTypes)
		results = append(results, telemetryPoints...)
	}

	// Sort by timestamp descending (most recent first)
	sortTelemetryByTimeDesc(results)

	return results, nil
}

const getTimeSeriesDeviceMetricsQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time >= $start_time
AND time <= $end_time
ORDER BY time ASC
LIMIT 5000
`

func (s *InfluxTelemetryStore) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	return s.getTimeSeriesTelemetryFromDeviceMetrics(ctx, query)
}

func (s *InfluxTelemetryStore) getTimeSeriesTelemetryFromDeviceMetrics(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	params := s.getTimeSeriesParamsForMeasurement(query)

	// Query device_metrics and get all matching points
	allMetrics, err := s.queryDeviceMetrics(ctx, getTimeSeriesDeviceMetricsQuery, query.DeviceIDs, params, "getTimeSeriesTelemetryFromDeviceMetrics")
	if err != nil {
		return nil, err
	}

	// Convert DeviceMetrics to legacy Telemetry format
	var results []models.Telemetry
	for _, deviceMetrics := range allMetrics {
		telemetryPoints := s.convertDeviceMetricsToTelemetry(deviceMetrics, query.MeasurementTypes)
		results = append(results, telemetryPoints...)
	}

	// Sort by timestamp ascending
	sortTelemetryByTimeAsc(results)

	return results, nil
}

const getDeviceMetricsMetadataQuery = `
SELECT device_id, time, health,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
ORDER BY time DESC
LIMIT 1000
`

func (s *InfluxTelemetryStore) GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	return s.getTelemetryMetadataFromDeviceMetrics(ctx, query)
}

func (s *InfluxTelemetryStore) getTelemetryMetadataFromDeviceMetrics(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)
	influxQuery := fmt.Sprintf(getDeviceMetricsMetadataQuery, deviceIDsStr)
	params := s.getMetadataParamsForMeasurement(query)

	iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
	if err != nil {
		return nil, fmt.Errorf("device_metrics metadata query failed: %w", err)
	}

	deviceMetadataMap := make(map[models.DeviceIdentifier]*models.DeviceMetadata)

	for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
		if err != nil {
			s.logger.Debug("error reading point in getTelemetryMetadataFromDeviceMetrics",
				slog.Any("error", err))
			continue
		}

		deviceID := models.DeviceIdentifier("")
		if tagValue, exists := point.GetTag("device_id"); exists {
			deviceID = models.DeviceIdentifier(tagValue)
		}

		if deviceID == "" {
			continue
		}

		metadata, exists := deviceMetadataMap[deviceID]
		if !exists {
			metadata = &models.DeviceMetadata{
				DeviceID: deviceID,
			}
			deviceMetadataMap[deviceID] = metadata
		}

		if point.Timestamp.After(metadata.LastSeen) {
			metadata.LastSeen = point.Timestamp

			// Extract health status if available
			if healthTag, exists := point.GetTag("health"); exists && healthTag != "" {
				// We can store health status in DeviceType or Location field
				// since the DeviceMetadata model doesn't have a health field
				metadata.DeviceType = healthTag
			}
		}
	}

	var allResults []models.DeviceMetadata
	for _, metadata := range deviceMetadataMap {
		allResults = append(allResults, *metadata)
	}

	if len(allResults) == 0 {
		return nil, fmt.Errorf("no metadata found in device_metrics")
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].LastSeen.After(allResults[j].LastSeen)
	})

	return allResults, nil
}

const streamDeviceMetricsQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time > $last_timestamp
ORDER BY time ASC
`

func (s *InfluxTelemetryStore) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	// Try to use the new device_metrics store, with fallback to legacy
	return s.streamTelemetryUpdatesWithFallback(ctx, query)
}

func (s *InfluxTelemetryStore) streamTelemetryUpdatesWithFallback(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	updateChan := make(chan models.TelemetryUpdate, defaultBufferSize)

	go func() {
		defer close(updateChan)

		pollInterval := defaultPollInterval
		if query.HeartbeatInterval != nil && *query.HeartbeatInterval > 0 {
			pollInterval = *query.HeartbeatInterval
		}

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		lastTimestamp := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hasNewData := false

				// Query device_metrics store
				deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)
				influxQuery := fmt.Sprintf(streamDeviceMetricsQuery, deviceIDsStr)
				params := s.getStreamParamsForMeasurement(query, lastTimestamp)

				iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
				if err != nil {
					s.logger.Debug("device_metrics stream query error",
						slog.Any("error", err))
					updateChan <- models.TelemetryUpdate{
						Type:      models.UpdateTypeError,
						Timestamp: time.Now(),
						Error:     stringPtr(fmt.Sprintf("query error: %v", err)),
					}
				} else {
					// Process device_metrics results
					for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
						if err != nil {
							s.logger.Debug("error reading device_metrics stream point", slog.Any("error", err))
							continue
						}

						deviceMetrics, err := influxModels.ToDeviceMetrics(point)
						if err != nil {
							s.logger.Debug("error converting point to DeviceMetrics", slog.Any("error", err))
							continue
						}

						// Convert to legacy telemetry format and send updates
						telemetryPoints := s.convertDeviceMetricsToTelemetry(deviceMetrics, query.MeasurementTypes)
						for _, telemetryData := range telemetryPoints {
							updateChan <- models.TelemetryUpdate{
								Type:      models.UpdateTypeTelemetry,
								DeviceID:  models.DeviceIdentifier(deviceMetrics.DeviceID),
								Timestamp: telemetryData.Timestamp,
								Data:      &telemetryData,
							}

							if telemetryData.Timestamp.After(lastTimestamp) {
								lastTimestamp = telemetryData.Timestamp
							}
							hasNewData = true
						}
					}
				}

				if query.IncludeHeartbeat && !hasNewData {
					updateChan <- models.TelemetryUpdate{
						Type:      models.UpdateTypeHeartbeat,
						Timestamp: time.Now(),
					}
				}
			}
		}
	}()

	return updateChan, nil
}

const getAggregatedDeviceMetricsQueryTemplate = `SELECT device_id,
%s(%s) as aggregated_value,
COUNT(*) as data_points,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time >= $start_time
AND time <= $end_time
GROUP BY device_id
ORDER BY device_id ASC`

func (s *InfluxTelemetryStore) GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	return s.getAggregatedTelemetryFromDeviceMetrics(ctx, query)
}

func (s *InfluxTelemetryStore) getAggregatedTelemetryFromDeviceMetrics(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	var allResults []models.AggregatedTelemetry
	aggFunc := getAggregationFunction(query.AggregationType)
	deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

	for _, measurementType := range query.MeasurementTypes {
		// Map measurement type to device_metrics field name
		fieldName := s.getMeasurementFieldName(measurementType)
		if fieldName == "" {
			s.logger.Debug("skipping unknown measurement type for aggregation in device_metrics")
			continue // Skip unknown measurement types
		}

		influxQuery := fmt.Sprintf(getAggregatedDeviceMetricsQueryTemplate, aggFunc, fieldName, deviceIDsStr)
		params := s.getAggregationParamsForMeasurement(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Debug("aggregated device_metrics query failed",
				slog.String("field", fieldName),
				slog.Any("error", err))
			continue
		}

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Debug("error reading point in getAggregatedTelemetryFromDeviceMetrics",
					slog.String("field", fieldName),
					slog.Any("error", err))
				continue
			}

			deviceID := models.DeviceIdentifier("")
			if tagValue, exists := point.GetTag("device_id"); exists {
				deviceID = models.DeviceIdentifier(tagValue)
			}

			aggregatedValue := float64(0)
			if valueField := point.GetField("aggregated_value"); valueField != nil {
				if val, ok := valueField.(float64); ok {
					aggregatedValue = val
					// Convert HashRate from H/s to MH/s if needed
					if measurementType == models.MeasurementTypeHashrate {
						aggregatedValue /= 1_000_000
					}
				}
			}

			dataPoints := int64(0)
			if countField := point.GetField("data_points"); countField != nil {
				if count, ok := countField.(int64); ok {
					dataPoints = count
				}
			}

			result := models.AggregatedTelemetry{
				DeviceID:        deviceID,
				MeasurementType: measurementType,
				Value:           aggregatedValue,
				DataPoints:      int(dataPoints),
				AggregationType: query.AggregationType,
			}

			allResults = append(allResults, result)
		}
	}

	if len(allResults) == 0 {
		return nil, fmt.Errorf("no aggregated data found in device_metrics")
	}

	sort.Slice(allResults, func(i, j int) bool {
		return string(allResults[i].DeviceID) < string(allResults[j].DeviceID)
	})

	return allResults, nil
}

// getMeasurementFieldName maps MeasurementType to device_metrics field name
func (s *InfluxTelemetryStore) getMeasurementFieldName(mt models.MeasurementType) string {
	switch mt {
	case models.MeasurementTypeHashrate:
		return "hash_rate_hs"
	case models.MeasurementTypeTemperature:
		return "temp_c"
	case models.MeasurementTypePower:
		return "power_w"
	case models.MeasurementTypeEfficiency:
		return "efficiency_jh"
	case models.MeasurementTypeFanSpeed:
		return "fan_rpm"
	case models.MeasurementTypeUnknown,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeCurrent,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return ""
	default:
		return ""
	}
}

func (s *InfluxTelemetryStore) Close() error {
	if err := s.client.Close(); err != nil {
		return newTelemetryCloseError(err)
	}
	return nil
}

func (s *InfluxTelemetryStore) Ping(ctx context.Context) error {
	iterator, err := s.client.Query(ctx, "SELECT 1")
	if err != nil {
		return newTelemetryPingError(err)
	}
	_ = iterator
	return nil
}

func deviceIDsToStrings(deviceIDs []models.DeviceIdentifier) []string {
	result := make([]string, len(deviceIDs))
	for i, id := range deviceIDs {
		result[i] = string(id)
	}
	return result
}

func (s *InfluxTelemetryStore) buildDeviceIDsString(deviceIDs []models.DeviceIdentifier) string {
	if len(deviceIDs) == 0 {
		return "''"
	}

	var parts []string
	for _, id := range deviceIDs {
		escapedID := strings.ReplaceAll(string(id), "'", "''")
		parts = append(parts, fmt.Sprintf("'%s'", escapedID))
	}
	return strings.Join(parts, ", ")
}

func measurementTypesToStrings(types []models.MeasurementType) []string {
	result := make([]string, len(types))
	for i, mt := range types {
		result[i] = mt.String()
	}
	return result
}

// getAggregationFunction returns the appropriate SQL aggregation function based on the AggregationType
//
//nolint:exhaustive // There are only a few types that we care about right now, and the default is AVG
func getAggregationFunction(aggType models.AggregationType) string {
	switch aggType {
	case models.AggregationTypeAverage:
		return "AVG"
	case models.AggregationTypeMin:
		return "MIN"
	case models.AggregationTypeMax:
		return "MAX"
	case models.AggregationTypeSum:
		return "SUM"
	case models.AggregationTypeCount:
		return "COUNT"
	case models.AggregationTypeUnknown:
		fallthrough
	default:
		return "AVG"
	}
}

// isCumulativeMeasurement determines if a measurement type is cumulative (like power, hashrate)
// or non-cumulative (like temperature, voltage)
func isCumulativeMeasurement(measurementType models.MeasurementType) bool {
	switch measurementType {
	case models.MeasurementTypePower:
		return true
	case models.MeasurementTypeHashrate:
		return true
	case models.MeasurementTypeUptime:
		return true
	case models.MeasurementTypeEfficiency:
		return false
	case models.MeasurementTypeTemperature:
		return false
	case models.MeasurementTypeVoltage:
		return false
	case models.MeasurementTypeCurrent:
		return false
	case models.MeasurementTypeFanSpeed:
		return false
	case models.MeasurementTypeErrorRate:
		return false
	case models.MeasurementTypeUnknown:
		fallthrough
	default:
		return false // Default to non-cumulative for unknown types
	}
}

func (s *InfluxTelemetryStore) getLatestTelemetryParamsForMeasurement(query models.LatestTelemetryQuery) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)

	if query.MaxAge != nil {
		params["max_age"] = time.Now().Add(-*query.MaxAge)
	} else {
		params["max_age"] = time.Now().Add(-defaultMaxAge)
	}

	return params
}

func (s *InfluxTelemetryStore) getTimeSeriesParamsForMeasurement(query models.TimeSeriesTelemetryQuery) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)

	if query.TimeRange.StartTime != nil {
		params["start_time"] = *query.TimeRange.StartTime
	} else {
		params["start_time"] = time.Now().Add(defaultStartTime)
	}

	if query.TimeRange.EndTime != nil {
		params["end_time"] = *query.TimeRange.EndTime
	} else {
		params["end_time"] = time.Now()
	}

	if query.Limit != nil {
		params["limit"] = *query.Limit
	} else {
		params["limit"] = defaultParamsLimit
	}

	return params
}

func (s *InfluxTelemetryStore) getMetadataParamsForMeasurement(_ models.MetadataQuery) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)
	return params
}

func (s *InfluxTelemetryStore) getStreamParamsForMeasurement(_ models.StreamQuery, lastTimestamp time.Time) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)
	params["last_timestamp"] = lastTimestamp
	return params
}

func (s *InfluxTelemetryStore) getAggregationParamsForMeasurement(query models.AggregationQuery) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)

	if query.TimeRange.StartTime != nil {
		params["start_time"] = *query.TimeRange.StartTime
	} else {
		params["start_time"] = time.Now().Add(-24 * time.Hour)
	}

	if query.TimeRange.EndTime != nil {
		params["end_time"] = *query.TimeRange.EndTime
	} else {
		params["end_time"] = time.Now()
	}

	return params
}

func (s *InfluxTelemetryStore) getCombinedMetricsParams(query models.CombinedMetricsQuery) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)

	// Time is all in New York

	if query.TimeRange.StartTime != nil {
		params["start_time"] = query.TimeRange.StartTime.UTC()
	} else {
		params["start_time"] = time.Now().Add(-24 * time.Hour)
	}

	if query.TimeRange.EndTime != nil {
		params["end_time"] = query.TimeRange.EndTime.UTC()
	} else {
		params["end_time"] = time.Now()
	}

	return params
}

func stringPtr(s string) *string {
	return &s
}

func (s *InfluxTelemetryStore) logWrite(pointCount int, duration time.Duration, err error) {
	if err != nil {
		s.logger.Error("telemetry write failed",
			slog.Int("point_count", pointCount),
			slog.Int64("duration_ms", duration.Milliseconds()),
			slog.Any("error", err))
	} else {
		s.logger.Debug("telemetry write successful",
			slog.Int("point_count", pointCount),
			slog.Int64("duration_ms", duration.Milliseconds()))
	}
}

const GetCombinedMetricsWindowing = `
{{define "GetCombinedMetricsWindowing"}}
WITH steps AS (
    SELECT
        date_bin_wallclock_gapfill(INTERVAL '{{.SlideIntervalSecs}} second', tz(h.time, 'UTC')) AS bucket,
        h.device_id,
        avg(value) AS mean,
        max(value) AS _max,
        min(value) AS _min,
        locf(last_value(value ORDER BY h.time)) AS _last,
        max(h.time) AS last_time
    FROM
        '{{.Table}}' as h
    WHERE
		h.time BETWEEN (to_timestamp($start_time) - INTERVAL '{{.WindowDurationSecs}} second') AND $end_time
		{{- if .DeviceIDs }}
			AND h.device_id IN ({{range $i, $id := .DeviceIDs}}{{if $i}}, {{end}}'{{$id}}'{{end}})
		{{- end }}
    GROUP BY bucket, h.device_id
),
device_roll_up AS (
    SELECT
        to_timestamp(bucket) AS _time,
        device_id,
		CASE
		WHEN isnan(
			avg (mean) OVER (
				PARTITION BY device_id
				ORDER BY bucket
				RANGE INTERVAL '{{.WindowDurationSecs}} second' PRECEDING
			) )
		THEN NULL
		ELSE
			avg (mean) OVER (
				PARTITION BY device_id
				ORDER BY bucket
				RANGE INTERVAL '{{.WindowDurationSecs}} second' PRECEDING
			)
		END AS mean,
        max (_max) OVER (
			PARTITION BY device_id
			ORDER BY bucket
			RANGE INTERVAL '{{.WindowDurationSecs}} second' PRECEDING
		) AS _max,
        min (_min) OVER (
			PARTITION BY device_id
			ORDER BY bucket
			RANGE INTERVAL '{{.WindowDurationSecs}} second' PRECEDING
		) AS _min,
        last_value (_last) OVER (
			PARTITION BY device_id
			ORDER BY bucket
			RANGE INTERVAL '{{.WindowDurationSecs}} second' PRECEDING
		) AS latest_value
    FROM steps
	GROUP BY bucket, device_id
    ORDER by bucket
),
{{end}}
`
const GetCombinedMetricsNonCumulativeTemplate = `
{{template "GetCombinedMetricsWindowing" .}}
bucket_stats AS (
  SELECT
    _time as time,
    SUM(latest_value)                         AS v_sum,
    AVG(latest_value)                         AS v_avg,
    MIN(latest_value)                         AS v_min,
    MAX(latest_value)                         AS v_max,
    approx_percentile_cont(latest_value,0.25) AS v_q1,
    approx_percentile_cont(latest_value,0.50) AS v_med,
    approx_percentile_cont(latest_value,0.75) AS v_q3,
    selector_first(latest_value, _time)['value'] AS v_first
  FROM device_roll_up
  GROUP BY _time
)
SELECT *
FROM bucket_stats
ORDER BY time
LIMIT  {{.Limit}}
OFFSET {{.Offset}};
`

const GetCombinedMetricCumulativeTemplate = `
{{template "GetCombinedMetricsWindowing" .}}
bucket_stats AS (
  SELECT
    _time as time,
    SUM(mean)                      AS total,
    SUM(_min)                       AS min_total,
    SUM(_max)                       AS max_total,
    AVG(_max - _min)             AS mean_change,
    STDDEV_SAMP(_max - _min)     AS stddev_change
  FROM device_roll_up
  GROUP BY _time
)
SELECT
	*
FROM bucket_stats
ORDER BY time
LIMIT  {{.Limit}}
OFFSET {{.Offset}};`

type CombinedMetricsQueryParams struct {
	Table              string   // measurement table, e.g. "power_w"
	DeviceIDs          []string // nil if selecting by org
	WindowDurationSecs int      // granularity in seconds
	Limit              int      // page_size (≤1000)
	Offset             int      // decoded page_token
	SlideIntervalSecs  int      // step in seconds, used for windowing
}

// isCumulativeMetric determines if a measurement type represents a cumulative metric
// that should be summed across devices at each timestamp.
//
// Cumulative metrics (hashrate, power): Represent rates/flows that should be summed across
// devices at each point in time before aggregating over the window.
//
// Non-cumulative metrics (temperature, efficiency, fan speed): Represent individual measurements
// that should be aggregated per device first, then combined.
func isCumulativeMetric(measurementType models.MeasurementType) bool {
	switch measurementType {
	case models.MeasurementTypeHashrate,
		models.MeasurementTypePower,
		models.MeasurementTypeCurrent: // Current is also additive
		return true
	case models.MeasurementTypeUnknown,
		models.MeasurementTypeTemperature,
		models.MeasurementTypeEfficiency,
		models.MeasurementTypeFanSpeed,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return false
	default:
		return false
	}
}

func (s *InfluxTelemetryStore) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	return s.getCombinedMetricsFromDeviceMetrics(ctx, query)
}

func (s *InfluxTelemetryStore) getCombinedMetricsFromDeviceMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	// For device_metrics, we'll use a simpler approach with aggregation over time windows
	// This is a simplified version compared to the complex windowing in legacy queries
	var allMetrics []models.Metric

	// Default slide interval if not specified
	slideInterval := defaultSlideInterval
	if query.SlideInterval != nil {
		slideInterval = *query.SlideInterval
	}

	// Note: WindowDuration is not used in the simplified device_metrics query
	// It's retained in the legacy query for complex windowing calculations

	for _, measurementType := range query.MeasurementTypes {
		fieldName := s.getMeasurementFieldName(measurementType)
		if fieldName == "" {
			continue
		}

		// Build a time-series aggregation query for device_metrics
		deviceIDsStr := ""
		if len(query.DeviceIDs) > 0 {
			deviceIDsStr = fmt.Sprintf("AND device_id IN (%s)", s.buildDeviceIDsString(query.DeviceIDs))
		}

		// Aggregation query strategy differs between cumulative and non-cumulative metrics:
		//
		// For CUMULATIVE metrics (hashrate, power, current):
		//   - These represent rates/flows that should be summed across devices
		//   - Semantics: For each device, calculate AVG/MIN/MAX over the window
		//   - Then SUM those per-device aggregations across all devices
		//   - Example: DeviceA=[100,200], DeviceB=[300,200]
		//     - DeviceA_AVG=150, DeviceB_AVG=250 → Final_AVG=400 (sum of averages)
		//     - DeviceA_MIN=100, DeviceB_MIN=200 → Final_MIN=300 (sum of minimums)
		//     - DeviceA_MAX=200, DeviceB_MAX=300 → Final_MAX=500 (sum of maximums)
		//     - DeviceA_latest=200, DeviceB_latest=200 → Final_SUM=400 (sum of latest)
		//
		// For NON-CUMULATIVE metrics (temperature, efficiency, fan speed):
		//   - These are independent measurements per device
		//   - Get latest value per device in each bucket
		//   - Then aggregate normally (AVG, MIN, MAX, SUM)
		//
		// IMPORTANT: Filter by field IS NOT NULL to avoid aggregations including NULL values

		var influxQuery string
		if isCumulativeMetric(measurementType) {
			// Cumulative metrics: aggregate per device, then sum across devices
			influxQuery = fmt.Sprintf(`
WITH per_device_aggregations AS (
	SELECT
		date_bin_wallclock_gapfill(INTERVAL '%d second', time) AS bucket,
		device_id,
		AVG(%s) as device_avg,
		MIN(%s) as device_min,
		MAX(%s) as device_max,
		last_value(%s ORDER BY time) as device_latest
	FROM device_metrics
	WHERE time BETWEEN $start_time AND $end_time
	AND %s IS NOT NULL
	%s
	GROUP BY bucket, device_id
)
SELECT
	to_timestamp(bucket) AS time,
	SUM(device_avg) as avg_value,
	SUM(device_min) as min_value,
	SUM(device_max) as max_value,
	SUM(device_latest) as sum_value,
	COUNT(DISTINCT device_id) as device_count
FROM per_device_aggregations
GROUP BY bucket
ORDER BY bucket DESC
LIMIT 1000
`, int(slideInterval.Seconds()), fieldName, fieldName, fieldName, fieldName, fieldName, deviceIDsStr)
		} else {
			// Non-cumulative metrics: get latest per device, then aggregate normally
			influxQuery = fmt.Sprintf(`
WITH latest_per_device AS (
	SELECT
		date_bin_wallclock_gapfill(INTERVAL '%d second', time) AS bucket,
		device_id,
		last_value(%s ORDER BY time) as latest_value
	FROM device_metrics
	WHERE time BETWEEN $start_time AND $end_time
	AND %s IS NOT NULL
	%s
	GROUP BY bucket, device_id
)
SELECT
	to_timestamp(bucket) AS time,
	AVG(latest_value) as avg_value,
	MIN(latest_value) as min_value,
	MAX(latest_value) as max_value,
	SUM(latest_value) as sum_value,
	COUNT(DISTINCT device_id) as device_count
FROM latest_per_device
GROUP BY bucket
ORDER BY bucket DESC
LIMIT 1000
`, int(slideInterval.Seconds()), fieldName, fieldName, deviceIDsStr)
		}

		params := s.getCombinedMetricsParams(query)
		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Debug("combined metrics device_metrics query failed",
				slog.String("field", fieldName),
				slog.Any("error", err))
			continue
		}

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Debug("error reading point in getCombinedMetricsFromDeviceMetrics",
					slog.String("field", fieldName),
					slog.Any("error", err))
				continue
			}

			// Extract the bucket timestamp from point.Timestamp
			// The query selects to_timestamp(bucket) AS time, which populates point.Timestamp
			bucketTime := point.Timestamp
			if bucketTime.IsZero() {
				s.logger.Debug("bucket time is zero (point.Timestamp)",
					slog.String("field", fieldName))
				continue
			}

			var aggregatedValues []models.AggregatedValue

			if avgField := point.GetField("avg_value"); avgField != nil {
				if val, ok := avgField.(float64); ok {
					// Convert hashrate from H/s to MH/s
					if measurementType == models.MeasurementTypeHashrate {
						val /= 1_000_000
					}
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeAverage,
						Value: val,
					})
				}
			}

			if minField := point.GetField("min_value"); minField != nil {
				if val, ok := minField.(float64); ok {
					if measurementType == models.MeasurementTypeHashrate {
						val /= 1_000_000
					}
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeMin,
						Value: val,
					})
				}
			}

			if maxField := point.GetField("max_value"); maxField != nil {
				if val, ok := maxField.(float64); ok {
					if measurementType == models.MeasurementTypeHashrate {
						val /= 1_000_000
					}
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeMax,
						Value: val,
					})
				}
			}

			if sumField := point.GetField("sum_value"); sumField != nil {
				if val, ok := sumField.(float64); ok {
					if measurementType == models.MeasurementTypeHashrate {
						val /= 1_000_000
					}
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeSum,
						Value: val,
					})
				}
			}

			// Extract device count
			var deviceCount int32
			if countField := point.GetField("device_count"); countField != nil {
				if val, ok := countField.(int64); ok {
					if val > math.MaxInt32 {
						s.logger.Warn("device count exceeds max int32, capping",
							slog.String("field", fieldName),
							slog.Int64("count", val))
						deviceCount = math.MaxInt32
					} else if val < 0 {
						s.logger.Warn("device count is negative, setting to 0",
							slog.String("field", fieldName),
							slog.Int64("count", val))
						deviceCount = 0
					} else {
						// #nosec G115 -- Conversion is safe: explicitly validated val is within [0, MaxInt32] range
						deviceCount = int32(val)
					}
				}
			}

			// Filter by requested aggregation types
			if len(query.AggregationTypes) > 0 {
				requestedTypes := make(map[models.AggregationType]bool)
				for _, aggType := range query.AggregationTypes {
					requestedTypes[aggType] = true
				}

				var filteredValues []models.AggregatedValue
				for _, aggValue := range aggregatedValues {
					if requestedTypes[aggValue.Type] {
						filteredValues = append(filteredValues, aggValue)
					}
				}
				aggregatedValues = filteredValues
			}

			if len(aggregatedValues) > 0 {
				metric := models.Metric{
					MeasurementType:  measurementType,
					AggregatedValues: aggregatedValues,
					OpenTime:         bucketTime,
					DeviceCount:      deviceCount,
				}
				allMetrics = append(allMetrics, metric)
			}
		}
	}

	// Sort metrics if we have any
	if len(allMetrics) > 0 {
		sort.Slice(allMetrics, func(i, j int) bool {
			return allMetrics[i].OpenTime.Before(allMetrics[j].OpenTime)
		})
	}

	// Query temperature status counts if temperature is in the measurement types
	var temperatureStatusCounts []models.TemperatureStatusCount
	hasTemperature := false
	for _, mt := range query.MeasurementTypes {
		if mt == models.MeasurementTypeTemperature {
			hasTemperature = true
			break
		}
	}

	if hasTemperature {
		// Build device IDs filter
		deviceIDsStr := ""
		if len(query.DeviceIDs) > 0 {
			deviceIDsStr = fmt.Sprintf("AND device_id IN (%s)", s.buildDeviceIDsString(query.DeviceIDs))
		}

		// Query to get temperature status counts grouped by time buckets
		// Use DATE_BIN to create regular time buckets (without gap filling)
		// Categorize temperatures using CASE statement from device_metrics table
		// Use window function to get the latest reading per device per bucket to avoid duplicate counting
		// Temperature thresholds use the domain layer constants for consistency
		temperatureStatusQuery := fmt.Sprintf(`
		WITH ranked_readings AS (
			SELECT
				device_id,
				DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) AS bucket_time,
				temp_c,
				ROW_NUMBER() OVER (PARTITION BY device_id, DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) ORDER BY time DESC) as rn
			FROM device_metrics
			WHERE time >= $start_time
			AND time <= $end_time
			AND temp_c IS NOT NULL
			%s
		),
		latest_per_device AS (
			SELECT
				device_id,
				bucket_time,
				temp_c
			FROM ranked_readings
			WHERE rn = 1
		)
		SELECT
			bucket_time,
			CASE
				WHEN temp_c < %f THEN 'cold'
				WHEN temp_c >= %f AND temp_c <= %f THEN 'ok'
				WHEN temp_c > %f AND temp_c <= %f THEN 'hot'
				WHEN temp_c > %f THEN 'critical'
			END AS temperature_status,
			COUNT(DISTINCT device_id) as count
		FROM latest_per_device
		GROUP BY bucket_time, temperature_status
		ORDER BY bucket_time ASC
		`, int(slideInterval.Seconds()), int(slideInterval.Seconds()), deviceIDsStr,
			telemetry.TempColdMaxC,                     // < 0 = cold
			telemetry.TempOkMinC, telemetry.TempOkMaxC, // 0-70 = ok
			telemetry.TempOkMaxC, telemetry.TempHotMaxC, // 70-90 = hot
			telemetry.TempHotMaxC) // > 90 = critical

		// Use the same parameter logic as metrics queries
		params := s.getCombinedMetricsParams(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, temperatureStatusQuery, params)
		if err != nil {
			// This is a legitimate error - query failed
			return models.CombinedMetric{}, fmt.Errorf("failed to query temperature status counts: %w", err)
		}

		// Process pre-grouped results from InfluxDB
		// Results come back as: bucket_time, temperature_status, count
		statusCountsByTime := make(map[time.Time]map[string]int32)
		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				// Skip invalid points
				continue
			}

			// Get the bucket_time from the query result (returned as arrow.Timestamp)
			var pointTime time.Time
			if timeField := point.GetField("bucket_time"); timeField != nil {
				if arrowTS, ok := timeField.(arrow.Timestamp); ok {
					// Arrow timestamp is in nanoseconds since Unix epoch
					pointTime = time.Unix(0, int64(arrowTS))
				}
			}

			if pointTime.IsZero() {
				// Skip points without valid timestamp
				continue
			}

			// Get the temperature status (from CASE statement, this is a field)
			status := ""
			if statusField := point.GetField("temperature_status"); statusField != nil {
				if s, ok := statusField.(string); ok {
					status = s
				}
			}

			// Get the count from the aggregation
			var count int32
			if countField := point.GetField("count"); countField != nil {
				switch v := countField.(type) {
				case float64:
					count = int32(v)
				case int64:
					// Validate int64 is within int32 bounds before conversion
					if v > math.MaxInt32 || v < math.MinInt32 {
						// Skip this point if value is out of bounds
						continue
					}
					count = int32(v)
				case int32:
					count = v
				}
			}

			// Initialize map for this time if needed
			if statusCountsByTime[pointTime] == nil {
				statusCountsByTime[pointTime] = make(map[string]int32)
			}
			// Store the count for this status at this time
			statusCountsByTime[pointTime][status] = count
		}

		// Convert to TemperatureStatusCount array
		for timestamp, counts := range statusCountsByTime {
			temperatureStatusCounts = append(temperatureStatusCounts, models.TemperatureStatusCount{
				Timestamp:     timestamp,
				ColdCount:     counts["cold"],
				OkCount:       counts["ok"],
				HotCount:      counts["hot"],
				CriticalCount: counts["critical"],
			})
		}

		// Sort by timestamp
		sort.Slice(temperatureStatusCounts, func(i, j int) bool {
			return temperatureStatusCounts[i].Timestamp.Before(temperatureStatusCounts[j].Timestamp)
		})
	}

	// Query uptime status counts if uptime is in the measurement types
	var uptimeStatusCounts []models.UptimeStatusCount
	hasUptime := false
	for _, mt := range query.MeasurementTypes {
		if mt == models.MeasurementTypeUptime {
			hasUptime = true
			break
		}
	}

	if hasUptime {
		// Build device IDs filter (same as temperature)
		deviceIDsStr := ""
		if len(query.DeviceIDs) > 0 {
			deviceIDsStr = fmt.Sprintf("AND device_id IN (%s)", s.buildDeviceIDsString(query.DeviceIDs))
		}

		// Query to get uptime/hashing status counts grouped by time buckets
		// Use DATE_BIN to create regular time buckets (without gap filling)
		// Categorize health: health_healthy_active = hashing, all others = not hashing
		// Use window function to get the latest reading per device per bucket
		uptimeStatusQuery := fmt.Sprintf(`
	WITH ranked_readings AS (
		SELECT
			device_id,
			DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) AS bucket_time,
			health,
			ROW_NUMBER() OVER (PARTITION BY device_id, DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) ORDER BY time DESC) as rn
		FROM device_metrics
		WHERE time >= $start_time
		AND time <= $end_time
		AND health IS NOT NULL
		%s
	),
	latest_per_device AS (
		SELECT
			device_id,
			bucket_time,
			health
		FROM ranked_readings
		WHERE rn = 1
	)
	SELECT
		bucket_time,
		CASE
			WHEN health = 'health_healthy_active' THEN 'hashing'
			ELSE 'not_hashing'
		END AS uptime_status,
		COUNT(DISTINCT device_id) as count
	FROM latest_per_device
	GROUP BY bucket_time, uptime_status
	ORDER BY bucket_time ASC
		`, int(slideInterval.Seconds()), int(slideInterval.Seconds()), deviceIDsStr)

		// Use the same parameter logic as metrics queries
		params := s.getCombinedMetricsParams(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, uptimeStatusQuery, params)
		if err != nil {
			// Log but don't fail - uptime counts are optional
			s.logger.Warn("failed to query uptime status counts", "error", err)
		} else {
			// Process pre-grouped results from InfluxDB
			uptimeStatusCountsByTime := make(map[time.Time]map[string]int32)
			for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
				if err != nil {
					continue
				}

				// Get bucket_time
				var pointTime time.Time
				if timeField := point.GetField("bucket_time"); timeField != nil {
					if arrowTS, ok := timeField.(arrow.Timestamp); ok {
						pointTime = time.Unix(0, int64(arrowTS))
					}
				}

				if pointTime.IsZero() {
					continue
				}

				// Get uptime status
				status := ""
				if statusField := point.GetField("uptime_status"); statusField != nil {
					if s, ok := statusField.(string); ok {
						status = s
					}
				}

				// Get count
				var count int32
				if countField := point.GetField("count"); countField != nil {
					switch v := countField.(type) {
					case float64:
						count = int32(v)
					case int64:
						if v > math.MaxInt32 || v < math.MinInt32 {
							continue
						}
						count = int32(v)
					case int32:
						count = v
					}
				}

				// Store count
				if uptimeStatusCountsByTime[pointTime] == nil {
					uptimeStatusCountsByTime[pointTime] = make(map[string]int32)
				}
				uptimeStatusCountsByTime[pointTime][status] = count
			}

			// Convert to UptimeStatusCount array
			for timestamp, counts := range uptimeStatusCountsByTime {
				uptimeStatusCounts = append(uptimeStatusCounts, models.UptimeStatusCount{
					Timestamp:       timestamp,
					HashingCount:    counts["hashing"],
					NotHashingCount: counts["not_hashing"],
				})
			}

			// Sort by timestamp
			sort.Slice(uptimeStatusCounts, func(i, j int) bool {
				return uptimeStatusCounts[i].Timestamp.Before(uptimeStatusCounts[j].Timestamp)
			})
		}
	}

	// Sort metrics by OpenTime in ascending order (oldest to newest)
	// Query uses DESC to prioritize recent data within LIMIT, but clients expect ASC for charting
	sort.Slice(allMetrics, func(i, j int) bool {
		return allMetrics[i].OpenTime.Before(allMetrics[j].OpenTime)
	})

	// Ensure we have at least some data to return
	if len(allMetrics) == 0 && len(temperatureStatusCounts) == 0 && len(uptimeStatusCounts) == 0 {
		return models.CombinedMetric{}, fmt.Errorf("no combined metrics found in device_metrics")
	}

	return models.CombinedMetric{
		Metrics:                 allMetrics,
		TemperatureStatusCounts: temperatureStatusCounts,
		UptimeStatusCounts:      uptimeStatusCounts,
		NextPageToken:           "", // Simplified - no pagination for device_metrics version
	}, nil
}

func (s *InfluxTelemetryStore) StoreDeviceMetrics(ctx context.Context, telemetry ...modelsV2.DeviceMetrics) error {
	if len(telemetry) == 0 {
		return nil
	}

	points := make([]*influxdb3.Point, 0, len(telemetry))
	for _, tm := range telemetry {
		devicePoints := influxModels.DeviceMetricsToPoints(tm)
		points = append(points, devicePoints...)
	}

	if err := s.client.WritePoints(ctx, points); err != nil {
		return newTelemetryWriteError(err, len(points))
	}

	return nil
}

// convertDeviceMetricsToTelemetry converts a DeviceMetrics instance to legacy Telemetry format
// It filters by the requested measurement types
func (s *InfluxTelemetryStore) convertDeviceMetricsToTelemetry(deviceMetrics modelsV2.DeviceMetrics, requestedTypes []models.MeasurementType) []models.Telemetry {
	var results []models.Telemetry
	deviceID := models.DeviceIdentifier(deviceMetrics.DeviceID)

	// Create a map of requested types for quick lookup
	requestedTypesMap := make(map[models.MeasurementType]bool)
	for _, mt := range requestedTypes {
		requestedTypesMap[mt] = true
	}

	// Helper function to check if a measurement type is requested
	isRequested := func(mt models.MeasurementType) bool {
		if len(requestedTypesMap) == 0 {
			return true // If no specific types requested, return all
		}
		return requestedTypesMap[mt]
	}

	// Convert HashrateHS (H/s) to hashrate_mhs (MH/s)
	if deviceMetrics.HashrateHS != nil && isRequested(models.MeasurementTypeHashrate) {
		valueInMhs := deviceMetrics.HashrateHS.Value / 1_000_000 // H/s to MH/s
		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypeHashrate.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id": deviceMetrics.DeviceID,
			},
			Fields: map[string]interface{}{
				"value": valueInMhs,
			},
			Timestamp: deviceMetrics.Timestamp,
		})
	}

	// Convert TempC to temperature_c
	if deviceMetrics.TempC != nil && isRequested(models.MeasurementTypeTemperature) {
		// Calculate temperature status using domain logic
		tempStatus := telemetry.GetTemperatureStatusString(deviceMetrics.TempC.Value)

		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypeTemperature.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id":          deviceMetrics.DeviceID,
				"temperature_status": tempStatus,
			},
			Fields: map[string]interface{}{
				"value": deviceMetrics.TempC.Value,
			},
			Timestamp: deviceMetrics.Timestamp,
		})
	}

	// Convert PowerW to power_w
	if deviceMetrics.PowerW != nil && isRequested(models.MeasurementTypePower) {
		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypePower.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id": deviceMetrics.DeviceID,
			},
			Fields: map[string]interface{}{
				"value": deviceMetrics.PowerW.Value,
			},
			Timestamp: deviceMetrics.Timestamp,
		})
	}

	// Convert EfficiencyJH to efficiency_jh (stored in J/H, converted to J/TH at API layer)
	if deviceMetrics.EfficiencyJH != nil && isRequested(models.MeasurementTypeEfficiency) {
		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypeEfficiency.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id": deviceMetrics.DeviceID,
			},
			Fields: map[string]interface{}{
				"value": deviceMetrics.EfficiencyJH.Value,
			},
			Timestamp: deviceMetrics.Timestamp,
		})
	}

	// Convert FanRPM to fan_speed_rpm (if FanSpeed measurement type exists)
	if deviceMetrics.FanRPM != nil && isRequested(models.MeasurementTypeFanSpeed) {
		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypeFanSpeed.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id": deviceMetrics.DeviceID,
			},
			Fields: map[string]interface{}{
				"value": deviceMetrics.FanRPM.Value,
			},
			Timestamp: deviceMetrics.Timestamp,
		})
	}

	// Set device_id as DeviceIdentifier for all results
	for i := range results {
		results[i].Tags["device_id"] = string(deviceID)
	}

	return results
}

const getLatestDeviceMetricsQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id = $device_id
ORDER BY time DESC
LIMIT 1
`

func (s *InfluxTelemetryStore) GetLatestDeviceMetrics(ctx context.Context, deviceID models.DeviceIdentifier) (modelsV2.DeviceMetrics, error) {
	influxQuery := getLatestDeviceMetricsQuery
	params := influxdb3.QueryParameters{
		"device_id": string(deviceID),
	}

	iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
	if err != nil {
		s.logger.Error("latest device metrics query failed",
			slog.String("device_id", string(deviceID)),
			slog.Any("error", err))
		return modelsV2.DeviceMetrics{}, newTelemetryQueryError(err, "GetLatestDeviceMetrics")
	}

	point, err := iterator.Next()
	if err == influxdb3.Done {
		return modelsV2.DeviceMetrics{}, fleeterror.NewInternalErrorf("no device metrics found for device_id %s", deviceID)
	}
	if err != nil {
		s.logger.Error("error reading point in GetLatestDeviceMetrics",
			slog.String("device_id", string(deviceID)),
			slog.Any("error", err))
		return modelsV2.DeviceMetrics{}, newTelemetryIterationError(err, "GetLatestDeviceMetrics", 1, false)
	}
	metrics, err := influxModels.ToDeviceMetrics(point)
	if err != nil {
		s.logger.Error("error converting point to DeviceMetrics",
			slog.String("device_id", string(deviceID)),
			slog.Any("error", err))
		return modelsV2.DeviceMetrics{}, newTelemetryIterationError(err, "GetLatestDeviceMetrics", 1, false)
	}
	return metrics, nil
}
