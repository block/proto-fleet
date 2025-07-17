package influxdb

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	influxModels "github.com/btc-mining/proto-fleet/server/internal/infrastructure/influxdb/models"
)

const (
	defaultRetryAttempts = 3
	defaultRetryDelay    = 100 * time.Millisecond
	maxPointsPerWrite    = 1000
	defaultParamsLimit   = 1000
	defaultMaxAge        = 24 * time.Hour
	defaultStartTime     = -24 * time.Hour
	defaultPollInterval  = 1 * time.Second
	defaultBufferSize    = 100
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

const getLatestTelemetryQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $max_age
ORDER BY time DESC
LIMIT 1000`

func (s *InfluxTelemetryStore) GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	var allResults []models.Telemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.String()

		deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

		influxQuery := fmt.Sprintf(getLatestTelemetryQuery, measurementName, measurementName, deviceIDsStr)
		params := s.getLatestTelemetryParamsForMeasurement(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Error("query failed for measurement type",
				slog.String("measurement", measurementName),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s: %w", measurementName, err))
			continue
		}

		var measurementResults []models.Telemetry
		var iterationErrors []error
		successCount := 0

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Error("error reading point in GetLatestTelemetry",
					slog.String("measurement", measurementName),
					slog.Any("error", err))
				iterationErrors = append(iterationErrors, err)
				continue
			}
			telemetry := influxModels.ToTelemetry(point)
			measurementResults = append(measurementResults, telemetry)
			successCount++
		}

		if len(iterationErrors) > 0 {
			if successCount > 0 {
				s.logger.Warn("GetLatestTelemetry completed with partial data for measurement",
					slog.String("measurement", measurementName),
					slog.Int("success_count", successCount),
					slog.Int("error_count", len(iterationErrors)))
			} else {
				allErrors = append(allErrors, fmt.Errorf("measurement %s iteration failed: %w", measurementName, iterationErrors[0]))
				continue
			}
		}

		allResults = append(allResults, measurementResults...)
		totalSuccessCount += successCount
	}

	if len(allErrors) > 0 {
		if totalSuccessCount > 0 {
			s.logger.Warn("GetLatestTelemetry completed with partial data across measurements",
				slog.Int("total_success_count", totalSuccessCount),
				slog.Int("measurement_error_count", len(allErrors)))
		} else {
			return nil, newTelemetryIterationError(allErrors[0], "GetLatestTelemetry", len(allErrors), totalSuccessCount > 0)
		}
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Timestamp.After(allResults[j].Timestamp)
	})

	return allResults, nil
}

const getTimeSeriesTelemetryQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $start_time
AND time <= $end_time
ORDER BY time ASC
LIMIT $limit`

func (s *InfluxTelemetryStore) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	var allResults []models.Telemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.String()

		deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

		influxQuery := fmt.Sprintf(getTimeSeriesTelemetryQuery, measurementName, measurementName, deviceIDsStr)
		params := s.getTimeSeriesParamsForMeasurement(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Error("query failed for measurement type",
				slog.String("measurement", measurementName),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s: %w", measurementName, err))
			continue
		}

		var measurementResults []models.Telemetry
		var iterationErrors []error
		successCount := 0

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Error("error reading point in GetTimeSeriesTelemetry",
					slog.String("measurement", measurementName),
					slog.Any("error", err))
				iterationErrors = append(iterationErrors, err)
				continue
			}
			telemetry := influxModels.ToTelemetry(point)
			measurementResults = append(measurementResults, telemetry)
			successCount++
		}

		if len(iterationErrors) > 0 {
			if successCount > 0 {
				s.logger.Warn("GetTimeSeriesTelemetry completed with partial data for measurement",
					slog.String("measurement", measurementName),
					slog.Int("success_count", successCount),
					slog.Int("error_count", len(iterationErrors)))
			} else {
				allErrors = append(allErrors, fmt.Errorf("measurement %s iteration failed: %w", measurementName, iterationErrors[0]))
				continue
			}
		}

		allResults = append(allResults, measurementResults...)
		totalSuccessCount += successCount
	}

	// Handle overall errors
	if len(allErrors) > 0 {
		if totalSuccessCount > 0 {
			s.logger.Warn("GetTimeSeriesTelemetry completed with partial data across measurements",
				slog.Int("total_success_count", totalSuccessCount),
				slog.Int("measurement_error_count", len(allErrors)))
		} else {
			return nil, newTelemetryIterationError(allErrors[0], "GetTimeSeriesTelemetry", len(allErrors), totalSuccessCount > 0)
		}
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Timestamp.Before(allResults[j].Timestamp)
	})

	return allResults, nil
}

const getTelemetryMetadataQuery = `SELECT device_id, time, device_type, location, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
ORDER BY time DESC
LIMIT 1000`

func (s *InfluxTelemetryStore) GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	var allResults []models.DeviceMetadata
	var allErrors []error
	totalSuccessCount := 0

	measurementTypes := []models.MeasurementType{
		models.MeasurementTypeTemperature,
		models.MeasurementTypeHashrate,
		models.MeasurementTypePower,
	}

	deviceMetadataMap := make(map[models.DeviceIdentifier]*models.DeviceMetadata)

	for _, measurementType := range measurementTypes {
		measurementName := measurementType.String()

		deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

		influxQuery := fmt.Sprintf(getTelemetryMetadataQuery, measurementName, measurementName, deviceIDsStr)
		params := s.getMetadataParamsForMeasurement(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Error("metadata query failed for measurement type",
				slog.String("measurement", measurementName),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s: %w", measurementName, err))
			continue
		}

		var iterationErrors []error
		successCount := 0

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Error("error reading point in GetTelemetryMetadata",
					slog.String("measurement", measurementName),
					slog.Any("error", err))
				iterationErrors = append(iterationErrors, err)
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

				if deviceTypeTag, exists := point.GetTag("device_type"); exists && deviceTypeTag != "" {
					metadata.DeviceType = deviceTypeTag
				}

				if locationTag, exists := point.GetTag("location"); exists && locationTag != "" {
					metadata.Location = locationTag
				}
			}

			successCount++
		}

		if len(iterationErrors) > 0 {
			if successCount > 0 {
				s.logger.Warn("GetTelemetryMetadata completed with partial data for measurement",
					slog.String("measurement", measurementName),
					slog.Int("success_count", successCount),
					slog.Int("error_count", len(iterationErrors)))
			} else {
				allErrors = append(allErrors, fmt.Errorf("measurement %s iteration failed: %w", measurementName, iterationErrors[0]))
				continue
			}
		}

		totalSuccessCount += successCount
	}

	for _, metadata := range deviceMetadataMap {
		allResults = append(allResults, *metadata)
	}

	// Handle overall errors
	if len(allErrors) > 0 {
		if totalSuccessCount > 0 {
			s.logger.Warn("GetTelemetryMetadata completed with partial data across measurements",
				slog.Int("total_success_count", totalSuccessCount),
				slog.Int("measurement_error_count", len(allErrors)))
		} else {
			return nil, newTelemetryIterationError(allErrors[0], "GetTelemetryMetadata", len(allErrors), totalSuccessCount > 0)
		}
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].LastSeen.After(allResults[j].LastSeen)
	})

	return allResults, nil
}

const streamTelemetryUpdatesQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time > $last_timestamp
ORDER BY time ASC`

func (s *InfluxTelemetryStore) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
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

				for _, measurementType := range query.MeasurementTypes {
					measurementName := measurementType.String()

					deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

					influxQuery := fmt.Sprintf(streamTelemetryUpdatesQuery, measurementName, measurementName, deviceIDsStr)
					params := s.getStreamParamsForMeasurement(query, lastTimestamp)

					iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
					if err != nil {
						s.logger.Debug("stream query error",
							slog.String("measurement", measurementName),
							slog.Any("error", err))
						updateChan <- models.TelemetryUpdate{
							Type:      models.UpdateTypeError,
							Timestamp: time.Now(),
							Error:     stringPtr(fmt.Sprintf("query error for %s: %v", measurementName, err)),
						}
						continue
					}

					for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
						if err != nil {
							updateChan <- models.TelemetryUpdate{
								Type:      models.UpdateTypeError,
								Timestamp: time.Now(),
								Error:     stringPtr(err.Error()),
							}
							continue
						}

						telemetryData := influxModels.ToTelemetry(point)

						deviceID := ""
						if tagValue, exists := point.GetTag("device_id"); exists {
							deviceID = tagValue
						}

						updateChan <- models.TelemetryUpdate{
							Type:      models.UpdateTypeTelemetry,
							DeviceID:  models.DeviceIdentifier(deviceID),
							Timestamp: telemetryData.Timestamp,
							Data:      &telemetryData,
						}

						if telemetryData.Timestamp.After(lastTimestamp) {
							lastTimestamp = telemetryData.Timestamp
						}
						hasNewData = true
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

const getAggregatedTelemetryQueryTemplate = `SELECT device_id,
%s(value) as aggregated_value,
COUNT(*) as data_points,
'%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $start_time
AND time <= $end_time
GROUP BY device_id
ORDER BY device_id ASC`

func (s *InfluxTelemetryStore) GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	var allResults []models.AggregatedTelemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.String()
		aggFunc := getAggregationFunction(query.AggregationType)

		deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)

		influxQuery := fmt.Sprintf(getAggregatedTelemetryQueryTemplate, aggFunc, measurementName, measurementName, deviceIDsStr)
		params := s.getAggregationParamsForMeasurement(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Error("aggregation query failed for measurement type",
				slog.String("measurement", measurementName),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s: %w", measurementName, err))
			continue
		}

		var measurementResults []models.AggregatedTelemetry
		var iterationErrors []error
		successCount := 0

		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Error("error reading point in GetAggregatedTelemetry",
					slog.String("measurement", measurementName),
					slog.Any("error", err))
				iterationErrors = append(iterationErrors, err)
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

			measurementResults = append(measurementResults, result)
			successCount++
		}

		if len(iterationErrors) > 0 {
			if successCount > 0 {
				s.logger.Warn("GetAggregatedTelemetry completed with partial data for measurement",
					slog.String("measurement", measurementName),
					slog.Int("success_count", successCount),
					slog.Int("error_count", len(iterationErrors)))
			} else {
				allErrors = append(allErrors, fmt.Errorf("measurement %s iteration failed: %w", measurementName, iterationErrors[0]))
				continue
			}
		}

		allResults = append(allResults, measurementResults...)
		totalSuccessCount += successCount
	}

	// Handle overall errors
	if len(allErrors) > 0 {
		if totalSuccessCount > 0 {
			s.logger.Warn("GetAggregatedTelemetry completed with partial data across measurements",
				slog.Int("total_success_count", totalSuccessCount),
				slog.Int("measurement_error_count", len(allErrors)))
		} else {
			return nil, newTelemetryIterationError(allErrors[0], "GetAggregatedTelemetry", len(allErrors), totalSuccessCount > 0)
		}
	}

	sort.Slice(allResults, func(i, j int) bool {
		return string(allResults[i].DeviceID) < string(allResults[j].DeviceID)
	})

	return allResults, nil
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
