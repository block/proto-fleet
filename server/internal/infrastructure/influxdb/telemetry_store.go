package influxdb

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	defaultRetryAttempts = 3
	defaultRetryDelay    = 100 * time.Millisecond
	maxPointsPerWrite    = 1000
	defaultParamsLimit   = 1000
	defaultMaxAge        = 24 * time.Hour
	defaultStartTime     = -24 * time.Hour
	defaultPollInterval  = 1 * time.Second
	defaultBufferSize    = 100
	defaultSlideInterval = 10 * time.Second

	// Query chunking configuration for large time ranges
	// Queries spanning more than this duration will be split into chunks
	queryChunkThreshold = 4 * time.Hour
	// Size of each chunk when splitting large queries
	queryChunkSize = 4 * time.Hour
	// Maximum number of concurrent chunk queries
	maxConcurrentChunkQueries = 16

	// LVC (Last Value Cache) configuration
	// Table and cache names
	deviceMetricsTableName = "device_metrics"
	deviceMetricsLVCName   = "device_metrics_latest"
	// Maximum lookback for using LVC optimization (matches InfluxDB LVC TTL)
	maxLVCLookback = 10 * time.Minute
	// Buffer to handle client/server time drift
	lvcDriftBuffer = 10 * time.Second
)

// timeChunk represents a single time range chunk for parallel query execution
type timeChunk struct {
	StartTime time.Time
	EndTime   time.Time
}

// chunkResult holds the result of a single chunk query along with any error
type chunkResult struct {
	Metrics                 []models.Metric
	TemperatureStatusCounts []models.TemperatureStatusCount
	UptimeStatusCounts      []models.UptimeStatusCount
	Error                   error
}

// splitTimeRange divides a large time range into smaller chunks for parallel querying.
// Returns a slice of timeChunks that together cover the entire original range.
//
// The chunks use non-overlapping boundaries to prevent duplicate data when the SQL
// queries use inclusive bounds (>= and <=). Each chunk after the first starts 1
// nanosecond after the previous chunk's end time, ensuring data at boundary
// timestamps is included in exactly one chunk.
//
// Example with 4-hour chunks from 0h to 10h:
//   - Chunk 1: [0h, 4h]
//   - Chunk 2: [4h + 1ns, 8h]
//   - Chunk 3: [8h + 1ns, 10h]
func splitTimeRange(startTime, endTime time.Time, chunkSize time.Duration) []timeChunk {
	var chunks []timeChunk
	current := startTime

	for current.Before(endTime) {
		chunkEnd := current.Add(chunkSize)
		if chunkEnd.After(endTime) {
			chunkEnd = endTime
		}
		chunks = append(chunks, timeChunk{
			StartTime: current,
			EndTime:   chunkEnd,
		})
		// Offset the next chunk's start by 1 nanosecond to prevent overlap
		// since SQL queries use inclusive bounds (>= and <=)
		current = chunkEnd.Add(time.Nanosecond)
	}

	return chunks
}

// needsChunking determines if a query time range should be split into chunks
func needsChunking(startTime, endTime *time.Time) bool {
	if startTime == nil || endTime == nil {
		return false
	}
	duration := endTime.Sub(*startTime)
	return duration > queryChunkThreshold
}

// mergeChunkResults combines results from multiple chunk queries into a single result
func mergeChunkResults(results []chunkResult) ([]models.Metric, []models.TemperatureStatusCount, []models.UptimeStatusCount, error) {
	var allMetrics []models.Metric
	var allTempCounts []models.TemperatureStatusCount
	var allUptimeCounts []models.UptimeStatusCount

	for _, result := range results {
		if result.Error != nil {
			// Return the first error encountered
			return nil, nil, nil, result.Error
		}
		allMetrics = append(allMetrics, result.Metrics...)
		allTempCounts = append(allTempCounts, result.TemperatureStatusCounts...)
		allUptimeCounts = append(allUptimeCounts, result.UptimeStatusCounts...)
	}

	// Sort metrics by OpenTime
	sort.Slice(allMetrics, func(i, j int) bool {
		return allMetrics[i].OpenTime.Before(allMetrics[j].OpenTime)
	})

	// Sort temperature counts by Timestamp
	sort.Slice(allTempCounts, func(i, j int) bool {
		return allTempCounts[i].Timestamp.Before(allTempCounts[j].Timestamp)
	})

	// Sort uptime counts by Timestamp
	sort.Slice(allUptimeCounts, func(i, j int) bool {
		return allUptimeCounts[i].Timestamp.Before(allUptimeCounts[j].Timestamp)
	})

	return allMetrics, allTempCounts, allUptimeCounts, nil
}

var _ telemetry.TelemetryDataStore = &InfluxTelemetryStore{}

// QueryStats tracks LVC query path usage for observability.
// All fields are updated atomically and safe for concurrent access.
type QueryStats struct {
	// LVCHits counts successful LVC queries that returned results
	LVCHits int64
	// LVCMisses counts LVC queries that returned empty results (triggering fallback)
	LVCMisses int64
	// LVCErrors counts LVC queries that failed with errors (triggering fallback)
	LVCErrors int64
	// TableQueries counts direct table queries (fallback or non-LVC-eligible)
	TableQueries int64
}

type InfluxTelemetryStore struct {
	client *influxdb3.Client
	config Config
	logger *slog.Logger

	queryStats struct {
		lvcHits      atomic.Int64
		lvcMisses    atomic.Int64
		lvcErrors    atomic.Int64
		tableQueries atomic.Int64
	}
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

// GetQueryStats returns a snapshot of the current query statistics.
// Use this to monitor LVC effectiveness in production or verify LVC usage in tests.
func (s *InfluxTelemetryStore) GetQueryStats() QueryStats {
	return QueryStats{
		LVCHits:      s.queryStats.lvcHits.Load(),
		LVCMisses:    s.queryStats.lvcMisses.Load(),
		LVCErrors:    s.queryStats.lvcErrors.Load(),
		TableQueries: s.queryStats.tableQueries.Load(),
	}
}

// ResetQueryStats resets all query statistics to zero.
// Useful for testing to get clean stats for each test case.
func (s *InfluxTelemetryStore) ResetQueryStats() {
	s.queryStats.lvcHits.Store(0)
	s.queryStats.lvcMisses.Store(0)
	s.queryStats.lvcErrors.Store(0)
	s.queryStats.tableQueries.Store(0)
}

// queryDeviceMetrics executes a device_metrics query and returns DeviceMetrics.
// Performance optimized: pre-allocated slice, batched error logging, minimal allocations in hot path.
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

	// Pre-allocate results slice based on expected size (1 result per device typical)
	estimatedSize := len(deviceIDs)
	if estimatedSize == 0 {
		estimatedSize = 100 // Default for queries without explicit device list
	}
	results := make([]modelsV2.DeviceMetrics, 0, estimatedSize)

	// Batch error counting to avoid per-iteration logging overhead
	var readErrors, conversionErrors int

	for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
		if err != nil {
			readErrors++
			continue
		}

		deviceMetrics, err := influxModels.ToDeviceMetrics(point)
		if err != nil {
			conversionErrors++
			continue
		}

		results = append(results, deviceMetrics)
	}

	// Log errors once after loop completes (avoids hot path logging overhead)
	if readErrors > 0 || conversionErrors > 0 {
		s.logger.Warn("errors during device metrics query",
			slog.String("method", methodName),
			slog.Int("read_errors", readErrors),
			slog.Int("conversion_errors", conversionErrors),
			slog.Int("successful_results", len(results)))
	}

	return results, nil
}

// filterLatestByDevice keeps only the most recent DeviceMetrics per device.
// Pre-allocates map and result slice based on input size for efficiency.
func filterLatestByDevice(metrics []modelsV2.DeviceMetrics) []modelsV2.DeviceMetrics {
	if len(metrics) == 0 {
		return nil
	}

	// Pre-allocate map with estimated unique device count
	latestMetricsByDevice := make(map[models.DeviceIdentifier]modelsV2.DeviceMetrics, len(metrics))

	for _, deviceMetrics := range metrics {
		deviceID := models.DeviceIdentifier(deviceMetrics.DeviceID)
		if existing, exists := latestMetricsByDevice[deviceID]; !exists || deviceMetrics.Timestamp.After(existing.Timestamp) {
			latestMetricsByDevice[deviceID] = deviceMetrics
		}
	}

	// Pre-allocate results slice with exact size needed
	results := make([]modelsV2.DeviceMetrics, 0, len(latestMetricsByDevice))
	for _, m := range latestMetricsByDevice {
		results = append(results, m)
	}

	return results
}

// nolint:unqueryvet // SELECT * required: InfluxDB 3 is schemaless - columns are created dynamically
// as data is written. Querying for explicit columns fails if any column doesn't exist in the data.
const getLatestDeviceMetricsForMultipleDevicesQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE device_id IN (%s)
AND time >= $max_age
ORDER BY time DESC
`

// nolint:unqueryvet // SELECT * required: InfluxDB 3 is schemaless - columns are created dynamically
// as data is written. Querying for explicit columns fails if any column doesn't exist in the data.
const getLatestDeviceMetricsForAllDevicesQuery = `
SELECT
*,
'device_metrics' as measurement
FROM device_metrics
WHERE time >= $max_age
ORDER BY time DESC
`

// LVC (Last Value Cache) query templates
// These query the materialized LVC instead of the full table for faster results
// Syntax: last_cache('table_name', 'cache_name') per InfluxDB 3 docs
//
// nolint:unqueryvet // SELECT * required: InfluxDB 3 is schemaless - columns are created dynamically
// as data is written. Querying for explicit columns fails if any column doesn't exist in the data.
var (
	getLatestDeviceMetricsFromLVCQuery = fmt.Sprintf(`
SELECT
*,
'device_metrics' as measurement
FROM last_cache('%s', '%s')
WHERE device_id IN (%%s)
`, deviceMetricsTableName, deviceMetricsLVCName)

	getLatestDeviceMetricsFromLVCAllDevicesQuery = fmt.Sprintf(`
SELECT
*,
'device_metrics' as measurement
FROM last_cache('%s', '%s')
`, deviceMetricsTableName, deviceMetricsLVCName)

	getTimeSeriesDeviceMetricsFromLVCQuery = fmt.Sprintf(`
SELECT
*,
'device_metrics' as measurement
FROM last_cache('%s', '%s')
WHERE device_id IN (%%s)
ORDER BY time ASC
`, deviceMetricsTableName, deviceMetricsLVCName)
)

// canUseLVCForTimeRange determines if a time range is fresh enough to use LVC.
// LVC is only valid for recent data (within maxLVCLookback of now).
// The drift buffer expands the window slightly to handle clock drift.
func canUseLVCForTimeRange(startTime *time.Time) bool {
	now := time.Now()
	lvcCutoff := now.Add(-maxLVCLookback).Add(-lvcDriftBuffer)

	// If no start time specified, assume "recent" query - LVC is valid
	if startTime == nil {
		return true
	}

	// If start time is older than LVC lookback, can't use LVC
	if startTime.Before(lvcCutoff) {
		return false
	}

	return true
}

// getLatestDeviceMetricsFromLVC queries the LVC for latest device metrics.
// Returns nil if LVC query fails (caller should fall back to table query).
func (s *InfluxTelemetryStore) getLatestDeviceMetricsFromLVC(ctx context.Context, deviceIDs []models.DeviceIdentifier) ([]modelsV2.DeviceMetrics, error) {
	var queryTemplate string
	if len(deviceIDs) > 0 {
		queryTemplate = getLatestDeviceMetricsFromLVCQuery
	} else {
		queryTemplate = getLatestDeviceMetricsFromLVCAllDevicesQuery
	}

	// LVC doesn't need time filter - it already contains only the latest values
	params := influxdb3.QueryParameters{}

	metrics, err := s.queryDeviceMetrics(ctx, queryTemplate, deviceIDs, params, "getLatestDeviceMetricsFromLVC")
	if err != nil {
		return nil, err
	}

	// LVC caches up to 60 values per device (--count 60), so filter to get only the latest
	return filterLatestByDevice(metrics), nil
}

// getLatestDeviceMetricsFromTable queries the device_metrics table directly (fallback for cold cache).
func (s *InfluxTelemetryStore) getLatestDeviceMetricsFromTable(ctx context.Context, deviceIDs []models.DeviceIdentifier) ([]modelsV2.DeviceMetrics, error) {
	var queryTemplate string
	if len(deviceIDs) > 0 {
		queryTemplate = getLatestDeviceMetricsForMultipleDevicesQuery
	} else {
		queryTemplate = getLatestDeviceMetricsForAllDevicesQuery
	}
	params := influxdb3.QueryParameters{
		"max_age": time.Now().Add(-defaultMaxAge),
	}

	allMetrics, err := s.queryDeviceMetrics(ctx, queryTemplate, deviceIDs, params, "getLatestDeviceMetricsFromTable")
	if err != nil {
		return nil, err
	}

	// Table query returns all rows - filter to get latest per device
	return filterLatestByDevice(allMetrics), nil
}

// GetLatestDeviceMetricsBatch returns the latest metrics for each device.
// Uses LVC (Last Value Cache) for fast queries, with automatic fallback to table query.
// Returns a map for O(1) device lookup.
func (s *InfluxTelemetryStore) GetLatestDeviceMetricsBatch(
	ctx context.Context,
	deviceIDs []models.DeviceIdentifier,
) (map[models.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	// Try LVC first for fast path
	metrics, err := s.getLatestDeviceMetricsFromLVC(ctx, deviceIDs)
	if err != nil {
		lvcErrors := s.queryStats.lvcErrors.Add(1)
		tableQueries := s.queryStats.tableQueries.Add(1)
		s.logger.Error("LVC query failed, falling back to table query",
			slog.String("error", err.Error()),
			slog.Int("device_count", len(deviceIDs)),
			slog.Int64("lvc_errors", lvcErrors),
			slog.Int64("table_queries", tableQueries))
		// Fall back to table query
		metrics, err = s.getLatestDeviceMetricsFromTable(ctx, deviceIDs)
		if err != nil {
			return nil, err
		}
	} else if len(metrics) == 0 {
		// LVC returned no results, fall back to table (LVC might be cold)
		lvcMisses := s.queryStats.lvcMisses.Add(1)
		tableQueries := s.queryStats.tableQueries.Add(1)
		s.logger.Debug("LVC cache miss, falling back to table query",
			slog.Int("device_count", len(deviceIDs)),
			slog.Int64("lvc_misses", lvcMisses),
			slog.Int64("table_queries", tableQueries))
		metrics, err = s.getLatestDeviceMetricsFromTable(ctx, deviceIDs)
		if err != nil {
			return nil, err
		}
	} else {
		// LVC query succeeded with results
		lvcHits := s.queryStats.lvcHits.Add(1)
		s.logger.Debug("LVC cache hit",
			slog.Int("device_count", len(deviceIDs)),
			slog.Int("result_count", len(metrics)),
			slog.Int64("lvc_hits", lvcHits))
	}

	// Build map for O(1) device lookup
	result := make(map[models.DeviceIdentifier]modelsV2.DeviceMetrics, len(metrics))
	for _, m := range metrics {
		result[models.DeviceIdentifier(m.DeviceID)] = m
	}

	return result, nil
}

// nolint:unqueryvet // SELECT * required: InfluxDB 3 is schemaless - columns are created dynamically
// as data is written. Querying for explicit columns fails if any column doesn't exist in the data.
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

// getTimeSeriesFromLVC queries the LVC for time series data (sparklines).
// Returns nil if LVC query fails (caller should fall back to table query).
func (s *InfluxTelemetryStore) getTimeSeriesFromLVC(ctx context.Context, deviceIDs []models.DeviceIdentifier) ([]modelsV2.DeviceMetrics, error) {
	// LVC doesn't need time filter - it already contains only the latest values
	params := influxdb3.QueryParameters{}

	metrics, err := s.queryDeviceMetrics(ctx, getTimeSeriesDeviceMetricsFromLVCQuery, deviceIDs, params, "getTimeSeriesFromLVC")
	if err != nil {
		return nil, err
	}

	return metrics, nil
}

// GetTimeSeriesTelemetry returns time series telemetry data.
// Uses LVC for recent time ranges, with automatic fallback to table query.
func (s *InfluxTelemetryStore) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]modelsV2.DeviceMetrics, error) {
	var allMetrics []modelsV2.DeviceMetrics
	var err error
	var usedLVC bool

	// Check if we can use LVC for this time range
	if canUseLVCForTimeRange(query.TimeRange.StartTime) {
		allMetrics, err = s.getTimeSeriesFromLVC(ctx, query.DeviceIDs)
		if err != nil {
			lvcErrors := s.queryStats.lvcErrors.Add(1)
			tableQueries := s.queryStats.tableQueries.Add(1)
			s.logger.Error("LVC time series query failed, falling back to table query",
				slog.String("error", err.Error()),
				slog.Int("device_count", len(query.DeviceIDs)),
				slog.Int64("lvc_errors", lvcErrors),
				slog.Int64("table_queries", tableQueries))
			// Fall through to table query
			allMetrics = nil
		} else if len(allMetrics) > 0 {
			usedLVC = true
			lvcHits := s.queryStats.lvcHits.Add(1)
			s.logger.Debug("LVC time series cache hit",
				slog.Int("device_count", len(query.DeviceIDs)),
				slog.Int("result_count", len(allMetrics)),
				slog.Int64("lvc_hits", lvcHits))
		} else {
			lvcMisses := s.queryStats.lvcMisses.Add(1)
			tableQueries := s.queryStats.tableQueries.Add(1)
			s.logger.Debug("LVC time series cache miss, falling back to table query",
				slog.Int("device_count", len(query.DeviceIDs)),
				slog.Int64("lvc_misses", lvcMisses),
				slog.Int64("table_queries", tableQueries))
		}
	} else {
		tableQueries := s.queryStats.tableQueries.Add(1)
		// Log with time range context for debugging
		logAttrs := []any{
			slog.Int("device_count", len(query.DeviceIDs)),
			slog.Duration("lvc_window", maxLVCLookback),
		}
		if query.TimeRange.StartTime != nil {
			logAttrs = append(logAttrs, slog.Time("query_start", *query.TimeRange.StartTime))
		}
		logAttrs = append(logAttrs, slog.Int64("table_queries", tableQueries))
		s.logger.Debug("LVC not applicable for time range, using table query", logAttrs...)
	}

	// Fall back to table query if LVC failed, returned empty, or wasn't applicable
	if allMetrics == nil || (len(allMetrics) == 0 && !usedLVC) {
		params := s.getTimeSeriesParamsForMeasurement(query)
		allMetrics, err = s.queryDeviceMetrics(ctx, getTimeSeriesDeviceMetricsQuery, query.DeviceIDs, params, "GetTimeSeriesTelemetry")
		if err != nil {
			return nil, err
		}
	}

	// Sort by timestamp ascending
	sort.Slice(allMetrics, func(i, j int) bool {
		return allMetrics[i].Timestamp.Before(allMetrics[j].Timestamp)
	})

	return allMetrics, nil
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
						slog.String("error", err.Error()))
					errorMsg := fmt.Sprintf("query error: %v", err)
					updateChan <- models.TelemetryUpdate{
						Type:      models.UpdateTypeError,
						Timestamp: time.Now(),
						Error:     &errorMsg,
					}
				} else {
					// Process device_metrics results with batched error tracking
					var readErrors, conversionErrors int

					for point, err := iterator.Next(); err != influxdb3.Done; point, err = iterator.Next() {
						if err != nil {
							readErrors++
							continue
						}

						deviceMetrics, err := influxModels.ToDeviceMetrics(point)
						if err != nil {
							conversionErrors++
							continue
						}

						// Extract measurements and send updates
						for _, measurementType := range query.MeasurementTypes {
							value, timestamp, ok := deviceMetrics.ExtractRawMeasurement(measurementType)
							if !ok {
								continue
							}

							updateChan <- models.TelemetryUpdate{
								Type:             models.UpdateTypeTelemetry,
								DeviceID:         models.DeviceIdentifier(deviceMetrics.DeviceID),
								Timestamp:        timestamp,
								MeasurementName:  measurementType.InfluxMeasurementName(),
								MeasurementValue: value,
							}

							if timestamp.After(lastTimestamp) {
								lastTimestamp = timestamp
							}
							hasNewData = true
						}
					}

					// Log errors once per poll cycle instead of per-point
					if readErrors > 0 || conversionErrors > 0 {
						s.logger.Debug("errors during stream poll",
							slog.Int("read_errors", readErrors),
							slog.Int("conversion_errors", conversionErrors))
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

func (s *InfluxTelemetryStore) buildDeviceIDsString(deviceIDs []models.DeviceIdentifier) string {
	if len(deviceIDs) == 0 {
		return "''"
	}

	// Pre-allocate builder capacity: ~40 chars per device ID (UUID + quotes + comma)
	var sb strings.Builder
	sb.Grow(len(deviceIDs) * 40)

	for i, id := range deviceIDs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('\'')
		// Escape single quotes by doubling them
		sb.WriteString(strings.ReplaceAll(string(id), "'", "''"))
		sb.WriteByte('\'')
	}
	return sb.String()
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

func (s *InfluxTelemetryStore) getStreamParamsForMeasurement(_ models.StreamQuery, lastTimestamp time.Time) influxdb3.QueryParameters {
	params := make(influxdb3.QueryParameters)
	params["last_timestamp"] = lastTimestamp
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
	// Check if the query needs to be chunked for large time ranges
	if needsChunking(query.TimeRange.StartTime, query.TimeRange.EndTime) {
		return s.getCombinedMetricsChunked(ctx, query)
	}
	return s.getCombinedMetricsFromDeviceMetrics(ctx, query)
}

// getCombinedMetricsChunked splits a large time range query into smaller chunks,
// executes them in parallel, and merges the results
func (s *InfluxTelemetryStore) getCombinedMetricsChunked(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	chunks := splitTimeRange(*query.TimeRange.StartTime, *query.TimeRange.EndTime, queryChunkSize)

	s.logger.Info("splitting combined metrics query into chunks",
		slog.Int("chunk_count", len(chunks)),
		slog.Duration("total_duration", query.TimeRange.EndTime.Sub(*query.TimeRange.StartTime)),
		slog.Duration("chunk_size", queryChunkSize))

	// Create a channel to collect results and a semaphore for concurrency control
	resultsChan := make(chan chunkResult, len(chunks))
	sem := make(chan struct{}, maxConcurrentChunkQueries)

	var wg sync.WaitGroup

	for _, chunk := range chunks {
		wg.Add(1)
		go func(c timeChunk) {
			defer wg.Done()

			// Check context cancellation before acquiring semaphore
			select {
			case <-ctx.Done():
				resultsChan <- chunkResult{Error: ctx.Err()}
				return
			default:
			}

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check context cancellation after acquiring semaphore
			select {
			case <-ctx.Done():
				resultsChan <- chunkResult{Error: ctx.Err()}
				return
			default:
			}

			// Create a sub-query for this chunk
			chunkQuery := query
			chunkQuery.TimeRange = models.TimeRange{
				StartTime: &c.StartTime,
				EndTime:   &c.EndTime,
			}

			// Execute the chunk query
			result, err := s.getCombinedMetricsFromDeviceMetrics(ctx, chunkQuery)
			if err != nil {
				// "no combined metrics found" is expected for chunks with no data
				// This should not fail the entire query - just return empty results for this chunk
				if strings.Contains(err.Error(), "no combined metrics found") {
					s.logger.Debug("chunk has no data",
						slog.Time("start", c.StartTime),
						slog.Time("end", c.EndTime))
					resultsChan <- chunkResult{} // Empty result, no error
					return
				}
				resultsChan <- chunkResult{Error: fmt.Errorf("chunk query failed for range %v-%v: %w", c.StartTime, c.EndTime, err)}
				return
			}

			resultsChan <- chunkResult{
				Metrics:                 result.Metrics,
				TemperatureStatusCounts: result.TemperatureStatusCounts,
				UptimeStatusCounts:      result.UptimeStatusCounts,
			}
		}(chunk)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect all results
	var results []chunkResult
	for result := range resultsChan {
		results = append(results, result)
	}

	// Merge results
	allMetrics, allTempCounts, allUptimeCounts, err := mergeChunkResults(results)
	if err != nil {
		return models.CombinedMetric{}, err
	}

	// Check if we have any data across all chunks
	if len(allMetrics) == 0 && len(allTempCounts) == 0 && len(allUptimeCounts) == 0 {
		return models.CombinedMetric{}, fmt.Errorf("no combined metrics found in device_metrics")
	}

	return models.CombinedMetric{
		Metrics:                 allMetrics,
		TemperatureStatusCounts: allTempCounts,
		UptimeStatusCounts:      allUptimeCounts,
		NextPageToken:           "",
	}, nil
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
			if !isTableNotFoundError(err) {
				s.logger.Debug("combined metrics device_metrics query failed",
					slog.String("field", fieldName),
					slog.Any("error", err))
			}
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
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeAverage,
						Value: val,
					})
				}
			}

			if minField := point.GetField("min_value"); minField != nil {
				if val, ok := minField.(float64); ok {
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeMin,
						Value: val,
					})
				}
			}

			if maxField := point.GetField("max_value"); maxField != nil {
				if val, ok := maxField.(float64); ok {
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  models.AggregationTypeMax,
						Value: val,
					})
				}
			}

			if sumField := point.GetField("sum_value"); sumField != nil {
				if val, ok := sumField.(float64); ok {
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
	hasTemperature := slices.Contains(query.MeasurementTypes, models.MeasurementTypeTemperature)

	if hasTemperature {
		// Build device IDs filter
		deviceIDsStr := ""
		if len(query.DeviceIDs) > 0 {
			deviceIDsStr = fmt.Sprintf("AND device_id IN (%s)", s.buildDeviceIDsString(query.DeviceIDs))
		}

		// Query to get temperature status counts grouped by time buckets
		// Use DATE_BIN to create regular time buckets (without gap filling)
		// Categorize temperatures using CASE statement from device_metrics table
		// Use last_value aggregation (more efficient than ROW_NUMBER window function)
		// to get the latest reading per device per bucket
		// Temperature thresholds use the domain layer constants for consistency
		temperatureStatusQuery := fmt.Sprintf(`
		WITH latest_per_device AS (
			SELECT
				DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) AS bucket_time,
				device_id,
				last_value(temp_c ORDER BY time) as temp_c
			FROM device_metrics
			WHERE time >= $start_time
			AND time <= $end_time
			AND temp_c IS NOT NULL
			%s
			GROUP BY bucket_time, device_id
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
		`, int(slideInterval.Seconds()), deviceIDsStr,
			telemetry.TempColdMaxC,                     // < 0 = cold
			telemetry.TempOkMinC, telemetry.TempOkMaxC, // 0-70 = ok
			telemetry.TempOkMaxC, telemetry.TempHotMaxC, // 70-90 = hot
			telemetry.TempHotMaxC) // > 90 = critical

		// Use the same parameter logic as metrics queries
		params := s.getCombinedMetricsParams(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, temperatureStatusQuery, params)
		if err != nil {
			if !isTableNotFoundError(err) {
				s.logger.Warn("failed to query temperature status counts", "error", err)
			}
		} else {
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
	}

	// Query uptime status counts if uptime is in the measurement types
	var uptimeStatusCounts []models.UptimeStatusCount
	hasUptime := slices.Contains(query.MeasurementTypes, models.MeasurementTypeUptime)

	if hasUptime {
		// Build device IDs filter (same as temperature)
		deviceIDsStr := ""
		if len(query.DeviceIDs) > 0 {
			deviceIDsStr = fmt.Sprintf("AND device_id IN (%s)", s.buildDeviceIDsString(query.DeviceIDs))
		}

		// Query to get uptime/hashing status counts grouped by time buckets
		// Use DATE_BIN to create regular time buckets (without gap filling)
		// Categorize health: health_healthy_active = hashing, all others = not hashing
		// Use last_value aggregation (more efficient than ROW_NUMBER window function)
		// to get the latest reading per device per bucket
		uptimeStatusQuery := fmt.Sprintf(`
	WITH latest_per_device AS (
		SELECT
			DATE_BIN(INTERVAL '%d seconds', time, '2020-01-01T00:00:00Z'::TIMESTAMP) AS bucket_time,
			device_id,
			last_value(health ORDER BY time) as health
		FROM device_metrics
		WHERE time >= $start_time
		AND time <= $end_time
		AND health IS NOT NULL
		%s
		GROUP BY bucket_time, device_id
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
		`, int(slideInterval.Seconds()), deviceIDsStr)

		// Use the same parameter logic as metrics queries
		params := s.getCombinedMetricsParams(query)

		iterator, err := s.client.QueryPointValueWithParameters(ctx, uptimeStatusQuery, params)
		if err != nil {
			if !isTableNotFoundError(err) {
				// Log but don't fail - uptime counts are optional
				s.logger.Warn("failed to query uptime status counts", "error", err)
			}
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
