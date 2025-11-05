package influxdb

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
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

// executeWithFallback tries the new device_metrics approach first, then falls back to legacy
// It handles the common pattern of trying new implementation with graceful degradation
func executeWithFallback[T any](
	ctx context.Context,
	logger *slog.Logger,
	methodName string,
	newFunc func(context.Context) (T, error),
	legacyFunc func(context.Context) (T, error),
	isEmpty func(T) bool,
) (T, error) {
	// Try the new device_metrics store first
	result, err := newFunc(ctx)
	if err == nil && !isEmpty(result) {
		return result, nil
	}

	if err != nil {
		logger.Error(fmt.Sprintf("failed to get %s from device_metrics, falling back to legacy", methodName),
			slog.Any("error", err))
	}

	return legacyFunc(ctx)
}

// queryDeviceMetrics executes a device_metrics query and returns DeviceMetrics
func (s *InfluxTelemetryStore) queryDeviceMetrics(
	ctx context.Context,
	queryTemplate string,
	deviceIDs []models.DeviceIdentifier,
	params influxdb3.QueryParameters,
	methodName string,
) ([]modelsV2.DeviceMetrics, error) {
	deviceIDsStr := s.buildDeviceIDsString(deviceIDs)
	influxQuery := fmt.Sprintf(queryTemplate, deviceIDsStr)

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

const getLatestTelemetryQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $max_age
ORDER BY time DESC
LIMIT 1000`

func (s *InfluxTelemetryStore) GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	return executeWithFallback(
		ctx,
		s.logger,
		"latest telemetry",
		func(ctx context.Context) ([]models.Telemetry, error) {
			return s.getLatestTelemetryFromDeviceMetrics(ctx, query)
		},
		func(ctx context.Context) ([]models.Telemetry, error) {
			return s.getLatestTelemetryLegacy(ctx, query)
		},
		func(results []models.Telemetry) bool {
			return len(results) == 0
		},
	)
}

func (s *InfluxTelemetryStore) getLatestTelemetryFromDeviceMetrics(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	params := s.getLatestTelemetryParamsForMeasurement(query)

	// Query device_metrics and get all matching points
	allMetrics, err := s.queryDeviceMetrics(ctx, getLatestDeviceMetricsForMultipleDevicesQuery, query.DeviceIDs, params, "getLatestTelemetryFromDeviceMetrics")
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

func (s *InfluxTelemetryStore) getLatestTelemetryLegacy(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	var allResults []models.Telemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.InfluxMeasurementName()

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

	sortTelemetryByTimeDesc(allResults)

	return allResults, nil
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

const getTimeSeriesTelemetryQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time >= $start_time
AND time <= $end_time
ORDER BY time ASC
LIMIT $limit`

func (s *InfluxTelemetryStore) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	return executeWithFallback(
		ctx,
		s.logger,
		"time series telemetry",
		func(ctx context.Context) ([]models.Telemetry, error) {
			return s.getTimeSeriesTelemetryFromDeviceMetrics(ctx, query)
		},
		func(ctx context.Context) ([]models.Telemetry, error) {
			return s.getTimeSeriesTelemetryLegacy(ctx, query)
		},
		func(results []models.Telemetry) bool {
			return len(results) == 0
		},
	)
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

func (s *InfluxTelemetryStore) getTimeSeriesTelemetryLegacy(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	var allResults []models.Telemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.InfluxMeasurementName()

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

	sortTelemetryByTimeAsc(allResults)

	return allResults, nil
}

const getDeviceMetricsMetadataQuery = `
SELECT device_id, time, health,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
ORDER BY time DESC
LIMIT 1000
`

const getTelemetryMetadataQuery = `SELECT device_id, time, device_type, location, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
ORDER BY time DESC
LIMIT 1000`

func (s *InfluxTelemetryStore) GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	return executeWithFallback(
		ctx,
		s.logger,
		"telemetry metadata",
		func(ctx context.Context) ([]models.DeviceMetadata, error) {
			return s.getTelemetryMetadataFromDeviceMetrics(ctx, query)
		},
		func(ctx context.Context) ([]models.DeviceMetadata, error) {
			return s.getTelemetryMetadataLegacy(ctx, query)
		},
		func(results []models.DeviceMetadata) bool {
			return len(results) == 0
		},
	)
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

func (s *InfluxTelemetryStore) getTelemetryMetadataLegacy(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
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
		measurementName := measurementType.InfluxMeasurementName()

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

const streamDeviceMetricsQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time > $last_timestamp
ORDER BY time ASC
`

const streamTelemetryUpdatesQuery = `SELECT device_id, time, value, '%s' as measurement
FROM %s
WHERE device_id IN (%s)
AND time > $last_timestamp
ORDER BY time ASC`

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

				// First, try querying the new device_metrics store
				deviceIDsStr := s.buildDeviceIDsString(query.DeviceIDs)
				influxQuery := fmt.Sprintf(streamDeviceMetricsQuery, deviceIDsStr)
				params := s.getStreamParamsForMeasurement(query, lastTimestamp)

				iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
				if err == nil {
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
				} else {
					s.logger.Debug("device_metrics stream query error, trying legacy",
						slog.Any("error", err))
				}

				// If no new data from device_metrics, fallback to legacy queries
				if !hasNewData {
					for _, measurementType := range query.MeasurementTypes {
						measurementName := measurementType.InfluxMeasurementName()

						legacyQuery := fmt.Sprintf(streamTelemetryUpdatesQuery, measurementName, measurementName, deviceIDsStr)
						params := s.getStreamParamsForMeasurement(query, lastTimestamp)

						iterator, err := s.client.QueryPointValueWithParameters(ctx, legacyQuery, params)
						if err != nil {
							s.logger.Debug("legacy stream query error",
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
	return executeWithFallback(
		ctx,
		s.logger,
		"aggregated telemetry",
		func(ctx context.Context) ([]models.AggregatedTelemetry, error) {
			return s.getAggregatedTelemetryFromDeviceMetrics(ctx, query)
		},
		func(ctx context.Context) ([]models.AggregatedTelemetry, error) {
			return s.getAggregatedTelemetryLegacy(ctx, query)
		},
		func(results []models.AggregatedTelemetry) bool {
			return len(results) == 0
		},
	)
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

func (s *InfluxTelemetryStore) getAggregatedTelemetryLegacy(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	var allResults []models.AggregatedTelemetry
	var allErrors []error
	totalSuccessCount := 0

	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.InfluxMeasurementName()
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
		params["stop_time"] = query.TimeRange.EndTime.UTC()
	} else {
		params["stop_time"] = time.Now()
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
		h.time BETWEEN (to_timestamp($start_time) - INTERVAL '{{.WindowDurationSecs}} second') AND $stop_time
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

func (s *InfluxTelemetryStore) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	return executeWithFallback(
		ctx,
		s.logger,
		"combined metrics",
		func(ctx context.Context) (models.CombinedMetric, error) {
			return s.getCombinedMetricsFromDeviceMetrics(ctx, query)
		},
		func(ctx context.Context) (models.CombinedMetric, error) {
			return s.getCombinedMetricsLegacy(ctx, query)
		},
		func(result models.CombinedMetric) bool {
			return len(result.Metrics) == 0
		},
	)
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

		// Simplified aggregation query
		influxQuery := fmt.Sprintf(`
SELECT
	date_bin_wallclock_gapfill(INTERVAL '%d second', time) AS bucket,
	AVG(%s) as avg_value,
	MIN(%s) as min_value,
	MAX(%s) as max_value,
	SUM(%s) as sum_value
FROM device_metrics
WHERE time BETWEEN $start_time AND $stop_time
%s
GROUP BY bucket
ORDER BY bucket ASC
LIMIT 1000
`, int(slideInterval.Seconds()), fieldName, fieldName, fieldName, fieldName, deviceIDsStr)

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

			bucketTime := point.Timestamp
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
				}
				allMetrics = append(allMetrics, metric)
			}
		}
	}

	if len(allMetrics) == 0 {
		return models.CombinedMetric{}, fmt.Errorf("no combined metrics found in device_metrics")
	}

	sort.Slice(allMetrics, func(i, j int) bool {
		return allMetrics[i].OpenTime.Before(allMetrics[j].OpenTime)
	})

	return models.CombinedMetric{
		Metrics:       allMetrics,
		NextPageToken: "", // Simplified - no pagination for device_metrics version
	}, nil
}

func (s *InfluxTelemetryStore) getCombinedMetricsLegacy(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	var allMetrics []models.Metric
	var allErrors []error
	totalSuccessCount := 0

	// Default slide interval if not specified
	slideInterval := defaultSlideInterval
	if query.SlideInterval != nil {
		slideInterval = *query.SlideInterval
	}

	// Default WindowDuration if not specified
	windowDuration := defaultWindowDuration
	if query.WindowDuration != nil {
		windowDuration = *query.WindowDuration
	}

	// Parse pagination token to get offset
	offset := 0
	if query.PaginationToken != "" {
		if parsedOffset, err := strconv.Atoi(query.PaginationToken); err == nil {
			offset = parsedOffset
		}
	}

	// Default limit for pagination
	limit := 100
	if query.PageSize > 0 {
		limit = query.PageSize
	}

	// Pre-parse both templates to avoid repeated parsing in the loop
	cumulativeTemplate, err := template.New("GetCombinedMetricCumulativeTemplate").Parse(GetCombinedMetricsWindowing + GetCombinedMetricCumulativeTemplate)
	if err != nil {
		s.logger.Error("failed to parse cumulative combined metrics template", slog.Any("error", err))
		return models.CombinedMetric{}, newTelemetryQueryError(err, "GetCombinedMetrics")
	}

	nonCumulativeTemplate, err := template.New("GetCombinedMetricsNonCumulativeTemplate").Parse(GetCombinedMetricsWindowing + GetCombinedMetricsNonCumulativeTemplate)
	if err != nil {
		s.logger.Error("failed to parse non-cumulative combined metrics template", slog.Any("error", err))
		return models.CombinedMetric{}, newTelemetryQueryError(err, "GetCombinedMetrics")
	}

	// Process each measurement type
	for _, measurementType := range query.MeasurementTypes {
		measurementName := measurementType.InfluxMeasurementName()

		// Prepare template parameters
		templateParams := CombinedMetricsQueryParams{
			Table:              measurementName,
			WindowDurationSecs: int(windowDuration.Seconds()),
			SlideIntervalSecs:  int(slideInterval.Seconds()),
			Limit:              limit,
			Offset:             offset,
		}

		// Set device IDs or organization
		if len(query.DeviceIDs) > 0 {
			templateParams.DeviceIDs = deviceIDsToStrings(query.DeviceIDs)
		}

		// Choose the appropriate template based on measurement type
		var selectedTemplate *template.Template
		isCumulative := isCumulativeMeasurement(measurementType)
		if isCumulative {
			selectedTemplate = cumulativeTemplate
		} else {
			selectedTemplate = nonCumulativeTemplate
		}

		// Execute the selected template to generate query
		var queryBuffer bytes.Buffer
		if err := selectedTemplate.Execute(&queryBuffer, templateParams); err != nil {
			s.logger.Error("failed to execute combined metrics template",
				slog.String("measurement", measurementName),
				slog.Bool("is_cumulative", isCumulative),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s template execute: %w", measurementName, err))
			continue
		}

		influxQuery := queryBuffer.String()

		// Prepare query parameters
		params := s.getCombinedMetricsParams(query)

		// Execute query
		iterator, err := s.client.QueryPointValueWithParameters(ctx, influxQuery, params)
		if err != nil {
			s.logger.Error("combined metrics query failed for measurement type",
				slog.String("measurement", measurementName),
				slog.Bool("is_cumulative", isCumulative),
				slog.Any("error", err))
			allErrors = append(allErrors, fmt.Errorf("measurement %s: %w", measurementName, err))
			continue
		}

		var measurementResults []models.Metric
		var iterationErrors []error
		successCount := 0

		// Process query results
		for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
			if err != nil {
				s.logger.Error("error reading point in GetCombinedMetrics",
					slog.String("measurement", measurementName),
					slog.Bool("is_cumulative", isCumulative),
					slog.Any("error", err))
				iterationErrors = append(iterationErrors, err)
				continue
			}

			// Extract bucket time (open time)
			bucketTime := point.Timestamp

			// Extract aggregated values from the point based on template type
			var aggregatedValues []models.AggregatedValue

			if isCumulative {
				// Handle cumulative template fields
				if totalField := point.GetField("total"); totalField != nil {
					if val, ok := totalField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeSum,
							Value: val,
						})
					}
				}

				if minTotalField := point.GetField("min_total"); minTotalField != nil {
					if val, ok := minTotalField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeMin,
							Value: val,
						})
					}
				}

				if maxTotalField := point.GetField("max_total"); maxTotalField != nil {
					if val, ok := maxTotalField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeMax,
							Value: val,
						})
					}
				}

				if meanChangeField := point.GetField("mean_change"); meanChangeField != nil {
					if val, ok := meanChangeField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeMeanChange,
							Value: val,
						})
					}
				}
			} else {
				// Handle non-cumulative template fields
				if sumField := point.GetField("v_sum"); sumField != nil {
					if val, ok := sumField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeSum,
							Value: val,
						})
					}
				}

				if avgField := point.GetField("v_avg"); avgField != nil {
					if val, ok := avgField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeAverage,
							Value: val,
						})
					}
				}

				if minField := point.GetField("v_min"); minField != nil {
					if val, ok := minField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeMin,
							Value: val,
						})
					}
				}

				if maxField := point.GetField("v_max"); maxField != nil {
					if val, ok := maxField.(float64); ok {
						aggregatedValues = append(aggregatedValues, models.AggregatedValue{
							Type:  models.AggregationTypeMax,
							Value: val,
						})
					}
				}
			}

			// Filter aggregated values based on requested aggregation types
			var filteredValues []models.AggregatedValue
			if len(query.AggregationTypes) > 0 {
				requestedTypes := make(map[models.AggregationType]bool)
				for _, aggType := range query.AggregationTypes {
					requestedTypes[aggType] = true
				}

				for _, aggValue := range aggregatedValues {
					if requestedTypes[aggValue.Type] {
						filteredValues = append(filteredValues, aggValue)
					}
				}
			} else {
				// If no specific aggregation types requested, return all
				filteredValues = aggregatedValues
			}

			if len(filteredValues) > 0 {
				metric := models.Metric{
					MeasurementType:  measurementType,
					AggregatedValues: filteredValues,
					OpenTime:         bucketTime,
				}
				measurementResults = append(measurementResults, metric)
				successCount++
			}
		}

		// Handle iteration errors
		if len(iterationErrors) > 0 {
			if successCount > 0 {
				s.logger.Warn("GetCombinedMetrics completed with partial data for measurement",
					slog.String("measurement", measurementName),
					slog.Bool("is_cumulative", isCumulative),
					slog.Int("success_count", successCount),
					slog.Int("error_count", len(iterationErrors)))
			} else {
				allErrors = append(allErrors, fmt.Errorf("measurement %s iteration failed: %w", measurementName, iterationErrors[0]))
				continue
			}
		}

		allMetrics = append(allMetrics, measurementResults...)
		totalSuccessCount += successCount
	}

	// Handle overall errors
	if len(allErrors) > 0 {
		if totalSuccessCount > 0 {
			s.logger.Warn("GetCombinedMetrics completed with partial data across measurements",
				slog.Int("total_success_count", totalSuccessCount),
				slog.Int("measurement_error_count", len(allErrors)))
		} else {
			return models.CombinedMetric{}, newTelemetryIterationError(allErrors[0], "GetCombinedMetrics", len(allErrors), totalSuccessCount > 0)
		}
	}

	// Sort metrics by open time
	sort.Slice(allMetrics, func(i, j int) bool {
		return allMetrics[i].OpenTime.Before(allMetrics[j].OpenTime)
	})

	// Generate next page token
	nextPageToken := ""
	if len(allMetrics) == limit {
		nextPageToken = strconv.Itoa(offset + limit)
	}

	return models.CombinedMetric{
		Metrics:       allMetrics,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *InfluxTelemetryStore) StoreDeviceMetrics(ctx context.Context, telemetry ...modelsV2.DeviceMetrics) error {
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
		results = append(results, models.Telemetry{
			Measurement: models.MeasurementTypeTemperature.InfluxMeasurementName(),
			Tags: map[string]string{
				"device_id": deviceMetrics.DeviceID,
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

	// Convert EfficiencyJH to efficiency_jth
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
