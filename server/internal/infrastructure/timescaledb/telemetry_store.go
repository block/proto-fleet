package timescaledb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	// Temperature thresholds for status counts (in Celsius)
	// Cold: temp < 0, Ok: 0 <= temp < 70, Hot: 70 <= temp < 90, Critical: temp >= 90
	tempThresholdCold     = 0.0  // Below this = Cold
	tempThresholdHot      = 70.0 // Below this = Ok, at or above = Hot
	tempThresholdCritical = 90.0 // At or above = Critical

	// Data source selection thresholds
	// Queries <= 1 day use raw data for highest resolution
	rawDataMaxDuration = 24 * time.Hour
	// Queries between 1 day and 10 days use hourly aggregates
	hourlyMaxDuration = 10 * 24 * time.Hour
	// Queries > 10 days use daily aggregates
	hourlyBucketDuration = time.Hour
	dailyBucketDuration  = 24 * time.Hour

	// Energy estimation constants.
	// Each telemetry data point represents one polling interval of device uptime.
	pollingIntervalSeconds = 10.0
	secondsPerHour         = 3600.0
	wattsPerKilowatt       = 1000.0
)

// nargActive flips a sqlc `narg(...) IS NULL` filter to its non-null branch.
// The string value is never read — sqlc checks Valid only.
var nargActive = sql.NullString{String: "1", Valid: true}

// estimateEnergyKWh computes estimated energy consumption in kilowatt-hours
// from average power and data point count. Unlike the old CAGG formula
// (SUM(power_w) / COUNT(*) * 24) which assumed 24h of uniform sampling,
// this scales by actual device uptime: each data point represents one polling
// interval (~10s), so devices offline for part of the day get proportionally
// less energy attributed.
//
// Intended for per-device daily energy rollups in handlers or domain logic
// once energy reporting is surfaced to the UI.
func estimateEnergyKWh(avgPowerW float64, dataPoints int64) float64 {
	activeHours := float64(dataPoints) * pollingIntervalSeconds / secondsPerHour
	return avgPowerW * activeHours / wattsPerKilowatt
}

// dataSource represents which table to query from based on time range
type dataSource int

const (
	dataSourceRaw dataSource = iota
	dataSourceHourly
	dataSourceDaily
)

func (ds dataSource) String() string {
	switch ds {
	case dataSourceRaw:
		return "raw"
	case dataSourceHourly:
		return "hourly"
	case dataSourceDaily:
		return "daily"
	default:
		return "unknown"
	}
}

// selectDataSource determines which table to query based on time range duration.
func selectDataSource(startTime, endTime *time.Time) dataSource {
	if startTime == nil || endTime == nil {
		return dataSourceRaw
	}
	duration := endTime.Sub(*startTime)
	if duration <= rawDataMaxDuration {
		return dataSourceRaw
	}
	if duration <= hourlyMaxDuration {
		return dataSourceHourly
	}
	return dataSourceDaily
}

// normalizeCompleteBucketRange returns a query range that only includes complete buckets.
// The SQL queries filter using `bucket <= end`, where `bucket` is the bucket start time.
// To exclude an in-progress last bucket, shift the end time back by one full bucket.
func normalizeCompleteBucketRange(startTime, endTime time.Time, bucketDuration time.Duration) (time.Time, time.Time, bool) {
	completeEndTime := endTime.Add(-bucketDuration)
	if completeEndTime.Before(startTime) {
		return time.Time{}, time.Time{}, false
	}
	return startTime, completeEndTime, true
}

// statusData holds a per-device temperature histogram for one bucket.
type statusData struct {
	bucket      time.Time
	tempBelow0  int32
	temp010     int32
	temp1020    int32
	temp2030    int32
	temp3040    int32
	temp4050    int32
	temp5060    int32
	temp6070    int32
	temp7080    int32
	temp8090    int32
	temp90100   int32
	temp100Plus int32
}

// toStatusCounts converts temperature histogram data to status counts.
// Maps histogram buckets to status categories using the same thresholds as
// calculateTemperatureStatusCount (tempThresholdCold=0, tempThresholdHot=70, tempThresholdCritical=90):
//   - Cold: temp < 0 → tempBelow0 bucket
//   - Ok: 0 <= temp < 70 → buckets 0-10 through 60-70
//   - Hot: 70 <= temp < 90 → buckets 70-80 and 80-90
//   - Critical: temp >= 90 → buckets 90-100 and 100+
func (d statusData) toStatusCounts() (cold, ok, hot, critical int32) {
	cold = d.tempBelow0
	ok = d.temp010 + d.temp1020 + d.temp2030 + d.temp3040 +
		d.temp4050 + d.temp5060 + d.temp6070
	hot = d.temp7080 + d.temp8090
	critical = d.temp90100 + d.temp100Plus
	return
}

func extractStatusDataHourly(row sqlc.DeviceStatusHourly) statusData {
	return statusData{
		bucket:      row.Bucket,
		tempBelow0:  row.TempBelow0,
		temp010:     row.Temp010,
		temp1020:    row.Temp1020,
		temp2030:    row.Temp2030,
		temp3040:    row.Temp3040,
		temp4050:    row.Temp4050,
		temp5060:    row.Temp5060,
		temp6070:    row.Temp6070,
		temp7080:    row.Temp7080,
		temp8090:    row.Temp8090,
		temp90100:   row.Temp90100,
		temp100Plus: row.Temp100Plus,
	}
}

func extractStatusDataDaily(row sqlc.DeviceStatusDaily) statusData {
	return statusData{
		bucket:      row.Bucket,
		tempBelow0:  row.TempBelow0,
		temp010:     row.Temp010,
		temp1020:    row.Temp1020,
		temp2030:    row.Temp2030,
		temp3040:    row.Temp3040,
		temp4050:    row.Temp4050,
		temp5060:    row.Temp5060,
		temp6070:    row.Temp6070,
		temp7080:    row.Temp7080,
		temp8090:    row.Temp8090,
		temp90100:   row.Temp90100,
		temp100Plus: row.Temp100Plus,
	}
}

// aggregateStatusRows counts each device once in its dominant temp category.
func aggregateStatusRows(rows []statusData) []models.TemperatureStatusCount {
	if len(rows) == 0 {
		return nil
	}

	type statusCounts struct {
		cold, ok, hot, critical int32
	}
	buckets := make(map[time.Time]*statusCounts)

	for _, row := range rows {
		counts, exists := buckets[row.bucket]
		if !exists {
			counts = &statusCounts{}
			buckets[row.bucket] = counts
		}

		cold, ok, hot, critical := row.toStatusCounts()
		maxTempCount := cold
		tempCategory := "cold"
		if ok > maxTempCount {
			maxTempCount = ok
			tempCategory = "ok"
		}
		if hot > maxTempCount {
			maxTempCount = hot
			tempCategory = "hot"
		}
		if critical > maxTempCount {
			tempCategory = "critical"
		}

		switch tempCategory {
		case "cold":
			counts.cold++
		case "ok":
			counts.ok++
		case "hot":
			counts.hot++
		case "critical":
			counts.critical++
		}
	}

	bucketTimes := make([]time.Time, 0, len(buckets))
	for t := range buckets {
		bucketTimes = append(bucketTimes, t)
	}
	sort.Slice(bucketTimes, func(i, j int) bool {
		return bucketTimes[i].Before(bucketTimes[j])
	})

	tempCounts := make([]models.TemperatureStatusCount, 0, len(buckets))
	for _, bucketTime := range bucketTimes {
		counts := buckets[bucketTime]
		tempCounts = append(tempCounts, models.TemperatureStatusCount{
			Timestamp:     bucketTime,
			ColdCount:     counts.cold,
			OkCount:       counts.ok,
			HotCount:      counts.hot,
			CriticalCount: counts.critical,
		})
	}

	return tempCounts
}

var _ telemetry.TelemetryDataStore = &TimescaleTelemetryStore{}

// TimescaleTelemetryStore implements TelemetryDataStore using TimescaleDB.
type TimescaleTelemetryStore struct {
	db      *sql.DB
	queries *sqlc.Queries
	config  Config
	logger  *slog.Logger
}

// NewTelemetryStore creates a new TimescaleDB telemetry store.
func NewTelemetryStore(db *sql.DB, config Config) (*TimescaleTelemetryStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	return &TimescaleTelemetryStore{
		db:      db,
		queries: sqlc.New(db),
		config:  config,
		logger:  slog.With("component", "timescale_telemetry_store"),
	}, nil
}

// StoreDeviceMetrics stores device metrics in TimescaleDB.
// The operation is atomic - if any insert fails, the entire transaction is rolled back.
func (s *TimescaleTelemetryStore) StoreDeviceMetrics(ctx context.Context, data ...modelsV2.DeviceMetrics) error {
	if len(data) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.WriteTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Warn("failed to rollback transaction", "error", err)
		}
	}()

	qtx := s.queries.WithTx(tx)

	for _, metrics := range data {
		params := sqlc.InsertDeviceMetricsParams{
			Time:             metrics.Timestamp,
			DeviceIdentifier: metrics.DeviceIdentifier,
			Health:           toNullString(metrics.Health.String()),
		}

		if metrics.HashrateHS != nil {
			params.HashRateHs = sql.NullFloat64{Float64: metrics.HashrateHS.Value, Valid: true}
			params.HashRateHsKind = toNullString(metrics.HashrateHS.Kind.String())
		}
		if metrics.TempC != nil {
			params.TempC = sql.NullFloat64{Float64: metrics.TempC.Value, Valid: true}
			params.TempCKind = toNullString(metrics.TempC.Kind.String())
		}
		if metrics.FanRPM != nil {
			params.FanRpm = sql.NullFloat64{Float64: metrics.FanRPM.Value, Valid: true}
			params.FanRpmKind = toNullString(metrics.FanRPM.Kind.String())
		}
		if metrics.PowerW != nil {
			params.PowerW = sql.NullFloat64{Float64: metrics.PowerW.Value, Valid: true}
			params.PowerWKind = toNullString(metrics.PowerW.Kind.String())
		}
		if metrics.EfficiencyJH != nil {
			params.EfficiencyJh = sql.NullFloat64{Float64: metrics.EfficiencyJH.Value, Valid: true}
			params.EfficiencyJhKind = toNullString(metrics.EfficiencyJH.Kind.String())
		}

		if err := qtx.InsertDeviceMetrics(ctx, params); err != nil {
			return fmt.Errorf("failed to insert metrics for device %s: %w", metrics.DeviceIdentifier, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetLatestDeviceMetricsBatch retrieves the latest metrics for multiple devices.
func (s *TimescaleTelemetryStore) GetLatestDeviceMetricsBatch(ctx context.Context, deviceIDs []models.DeviceIdentifier) (map[models.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	maxAge := time.Now().Add(-s.config.MaxAge)

	var rows []sqlc.DeviceMetric
	var err error

	if len(deviceIDs) == 0 {
		rows, err = s.queries.GetLatestAllDeviceMetrics(ctx, maxAge)
	} else {
		identifiers := make([]string, len(deviceIDs))
		for i, id := range deviceIDs {
			identifiers[i] = string(id)
		}
		rows, err = s.queries.GetLatestDeviceMetrics(ctx, sqlc.GetLatestDeviceMetricsParams{
			DeviceIdentifiers: identifiers,
			Time:              maxAge,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query latest metrics: %w", err)
	}

	result := make(map[models.DeviceIdentifier]modelsV2.DeviceMetrics, len(rows))
	for _, row := range rows {
		metrics := sqlcMetricsToDeviceMetrics(row)
		result[models.DeviceIdentifier(metrics.DeviceIdentifier)] = metrics
	}

	return result, nil
}

// GetTimeSeriesTelemetry retrieves time series metrics for devices.
func (s *TimescaleTelemetryStore) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]modelsV2.DeviceMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	endTime := time.Now()
	startTime := endTime.Add(-s.config.MaxAge)

	if query.TimeRange.StartTime != nil {
		startTime = *query.TimeRange.StartTime
	}
	if query.TimeRange.EndTime != nil {
		endTime = *query.TimeRange.EndTime
	}

	var rows []sqlc.DeviceMetric
	var err error

	maxRows := safeIntToInt32(s.config.MaxTimeSeriesRows)
	if maxRows <= 0 {
		maxRows = safeIntToInt32(DefaultConfig().MaxTimeSeriesRows)
	}

	if len(query.DeviceIDs) == 0 {
		rows, err = s.queries.GetAllDeviceMetricsTimeSeries(ctx, sqlc.GetAllDeviceMetricsTimeSeriesParams{
			Time:    startTime,
			Time_2:  endTime,
			MaxRows: maxRows,
		})
	} else {
		identifiers := make([]string, len(query.DeviceIDs))
		for i, id := range query.DeviceIDs {
			identifiers[i] = string(id)
		}
		rows, err = s.queries.GetDeviceMetricsTimeSeries(ctx, sqlc.GetDeviceMetricsTimeSeriesParams{
			DeviceIdentifiers: identifiers,
			Time:              startTime,
			Time_2:            endTime,
			MaxRows:           maxRows,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query time series: %w", err)
	}

	result := make([]modelsV2.DeviceMetrics, 0, len(rows))
	for _, row := range rows {
		result = append(result, sqlcMetricsToDeviceMetrics(row))
	}

	if query.Limit != nil && len(result) > *query.Limit {
		result = result[:*query.Limit]
	}

	return result, nil
}

// StreamTelemetryUpdates returns a channel that streams telemetry updates.
// Respects query.MeasurementTypes if specified, otherwise uses defaults.
func (s *TimescaleTelemetryStore) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	updateChan := make(chan models.TelemetryUpdate, s.config.BufferSize)

	measurementTypes := query.MeasurementTypes
	if len(measurementTypes) == 0 {
		measurementTypes = modelsV2.DefaultMeasurementTypes
	}

	lastSeen := make(map[models.DeviceIdentifier]time.Time)

	go func() {
		defer close(updateChan)

		ticker := time.NewTicker(s.config.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				metrics, err := s.GetLatestDeviceMetricsBatch(ctx, query.DeviceIDs)
				if err != nil {
					s.logger.Debug("telemetry stream query error", "error", err)
					errorMsg := fmt.Sprintf("query error: %v", err)
					select {
					case updateChan <- models.TelemetryUpdate{
						Type:      models.UpdateTypeError,
						Timestamp: time.Now(),
						Error:     &errorMsg,
					}:
					case <-ctx.Done():
						return
					default:
						s.logger.Warn("telemetry update channel full, dropping error update")
					}
					continue
				}

				for deviceID, m := range metrics {
					lastTime, exists := lastSeen[deviceID]

					if !exists || m.Timestamp.After(lastTime) {
						lastSeen[deviceID] = m.Timestamp

						for _, measurementType := range measurementTypes {
							value, _, ok := m.ExtractRawMeasurement(measurementType)
							if !ok {
								continue
							}

							update := models.TelemetryUpdate{
								Type:             models.UpdateTypeTelemetry,
								DeviceIdentifier: deviceID,
								Timestamp:        m.Timestamp,
								MeasurementName:  measurementType.String(),
								MeasurementValue: value,
							}

							select {
							case updateChan <- update:
							case <-ctx.Done():
								return
							default:
								s.logger.Warn("telemetry update channel full, dropping update", "device_id", deviceID)
							}
						}
					}
				}

				if query.IncludeHeartbeat {
					heartbeat := models.TelemetryUpdate{
						Type:      models.UpdateTypeHeartbeat,
						Timestamp: time.Now(),
					}
					select {
					case updateChan <- heartbeat:
					case <-ctx.Done():
						return
					default:
					}
				}
			}
		}
	}()

	return updateChan, nil
}

// GetCombinedMetrics retrieves aggregated metrics across devices.
// Routes queries to the appropriate data source based on time range:
// - Raw data (device_metrics) for queries <= 24h
// - Hourly aggregates (device_metrics_hourly) for queries 24h-10d
// - Daily aggregates (device_metrics_daily) for queries > 10d
func (s *TimescaleTelemetryStore) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	ds := selectDataSource(query.TimeRange.StartTime, query.TimeRange.EndTime)

	s.logger.Debug("selected data source for combined metrics",
		slog.String("source", ds.String()),
		slog.Any("start_time", query.TimeRange.StartTime),
		slog.Any("end_time", query.TimeRange.EndTime))

	switch ds {
	case dataSourceRaw:
		return s.getCombinedMetricsFromRaw(ctx, query)
	case dataSourceHourly:
		return s.getCombinedMetricsFromHourly(ctx, query)
	case dataSourceDaily:
		return s.getCombinedMetricsFromDaily(ctx, query)
	}
	return s.getCombinedMetricsFromRaw(ctx, query)
}

// getCombinedMetricsFromRaw queries raw device_metrics table (for short time ranges).
func (s *TimescaleTelemetryStore) getCombinedMetricsFromRaw(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	tsQuery := models.TimeSeriesTelemetryQuery{
		DeviceIDs:        query.DeviceIDs,
		MeasurementTypes: query.MeasurementTypes,
		TimeRange:        query.TimeRange,
	}

	data, err := s.GetTimeSeriesTelemetry(ctx, tsQuery)
	if err != nil {
		return models.CombinedMetric{}, err
	}

	if len(data) == 0 {
		return models.CombinedMetric{}, nil
	}

	bucketDuration := DefaultBucketDuration
	if query.SlideInterval != nil {
		bucketDuration = *query.SlideInterval
	}

	result := s.aggregateMetrics(data, query.MeasurementTypes, query.AggregationTypes, bucketDuration)
	startTime, endTime := s.getTimeRange(query.TimeRange)
	result.UptimeStatusCounts = s.uptimeCountsForQuery(ctx, query, startTime, endTime, bucketDuration)

	return result, nil
}

// getCombinedMetricsFromHourly queries device_metrics_hourly continuous aggregate.
func (s *TimescaleTelemetryStore) getCombinedMetricsFromHourly(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	startTime, endTime := s.getTimeRange(query.TimeRange)
	startTime, endTime, hasCompleteBucket := normalizeCompleteBucketRange(startTime, endTime, hourlyBucketDuration)
	if !hasCompleteBucket {
		return models.CombinedMetric{}, nil
	}

	var rows []sqlc.DeviceMetricsHourly
	var err error

	if len(query.DeviceIDs) == 0 {
		rows, err = s.queries.GetAllDeviceMetricsHourlyAggregates(ctx, sqlc.GetAllDeviceMetricsHourlyAggregatesParams{
			Bucket:   startTime,
			Bucket_2: endTime,
		})
	} else {
		identifiers := deviceIDsToStrings(query.DeviceIDs)
		rows, err = s.queries.GetDeviceMetricsHourlyAggregates(ctx, sqlc.GetDeviceMetricsHourlyAggregatesParams{
			DeviceIdentifiers: identifiers,
			Bucket:            startTime,
			Bucket_2:          endTime,
		})
	}

	if err != nil {
		return models.CombinedMetric{}, fmt.Errorf("failed to query hourly aggregates: %w", err)
	}

	if len(rows) == 0 {
		return models.CombinedMetric{}, nil
	}

	metrics := s.aggregateHourlyRows(rows, query.MeasurementTypes, query.AggregationTypes)

	tempCounts := s.getTemperatureCountsFromHourlyAggregates(ctx, query.DeviceIDs, startTime, endTime)
	uptimeCounts := s.uptimeCountsForQuery(ctx, query, startTime, endTime, hourlyBucketDuration)

	return models.CombinedMetric{
		Metrics:                 metrics,
		TemperatureStatusCounts: tempCounts,
		UptimeStatusCounts:      uptimeCounts,
	}, nil
}

// getCombinedMetricsFromDaily queries device_metrics_daily continuous aggregate.
func (s *TimescaleTelemetryStore) getCombinedMetricsFromDaily(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	startTime, endTime := s.getTimeRange(query.TimeRange)
	startTime, endTime, hasCompleteBucket := normalizeCompleteBucketRange(startTime, endTime, dailyBucketDuration)
	if !hasCompleteBucket {
		return models.CombinedMetric{}, nil
	}

	var rows []sqlc.DeviceMetricsDaily
	var err error

	if len(query.DeviceIDs) == 0 {
		rows, err = s.queries.GetAllDeviceMetricsDailyAggregates(ctx, sqlc.GetAllDeviceMetricsDailyAggregatesParams{
			Bucket:   startTime,
			Bucket_2: endTime,
		})
	} else {
		identifiers := deviceIDsToStrings(query.DeviceIDs)
		rows, err = s.queries.GetDeviceMetricsDailyAggregates(ctx, sqlc.GetDeviceMetricsDailyAggregatesParams{
			DeviceIdentifiers: identifiers,
			Bucket:            startTime,
			Bucket_2:          endTime,
		})
	}

	if err != nil {
		return models.CombinedMetric{}, fmt.Errorf("failed to query daily aggregates: %w", err)
	}

	if len(rows) == 0 {
		return models.CombinedMetric{}, nil
	}

	metrics := s.aggregateDailyRows(rows, query.MeasurementTypes, query.AggregationTypes)

	tempCounts := s.getTemperatureCountsFromDailyAggregates(ctx, query.DeviceIDs, startTime, endTime)
	uptimeCounts := s.uptimeCountsForQuery(ctx, query, startTime, endTime, dailyBucketDuration)

	return models.CombinedMetric{
		Metrics:                 metrics,
		TemperatureStatusCounts: tempCounts,
		UptimeStatusCounts:      uptimeCounts,
	}, nil
}

// uptimeCountsForQuery returns nil when OrganizationID is unset so callers
// without session context can't leak another org's counts. Callers pass the
// same start/end used for the surrounding metric query so uptime bars line up
// with metric bars (notably: hourly/daily callers pass a range normalized to
// complete buckets, not the raw request range).
func (s *TimescaleTelemetryStore) uptimeCountsForQuery(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time, bucketDuration time.Duration) []models.UptimeStatusCount {
	if query.OrganizationID == 0 {
		return nil
	}
	return s.getUptimeStatusCountsFromSnapshots(ctx, query.OrganizationID, query.DeviceIDs, startTime, endTime, bucketDuration)
}

// getTimeRange extracts start and end times from the query, using defaults if not set.
func (s *TimescaleTelemetryStore) getTimeRange(tr models.TimeRange) (time.Time, time.Time) {
	endTime := time.Now()
	startTime := endTime.Add(-s.config.MaxAge)

	if tr.StartTime != nil {
		startTime = *tr.StartTime
	}
	if tr.EndTime != nil {
		endTime = *tr.EndTime
	}
	return startTime, endTime
}

// deviceIDsToStrings converts device identifiers to strings.
func deviceIDsToStrings(ids []models.DeviceIdentifier) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = string(id)
	}
	return result
}

func (s *TimescaleTelemetryStore) getTemperatureCountsFromHourlyAggregates(
	ctx context.Context,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
) []models.TemperatureStatusCount {
	var rows []sqlc.DeviceStatusHourly
	var err error

	if len(deviceIDs) == 0 {
		rows, err = s.queries.GetAllDeviceStatusHourlyAggregates(ctx, sqlc.GetAllDeviceStatusHourlyAggregatesParams{
			Bucket:   startTime,
			Bucket_2: endTime,
		})
	} else {
		identifiers := deviceIDsToStrings(deviceIDs)
		rows, err = s.queries.GetDeviceStatusHourlyAggregates(ctx, sqlc.GetDeviceStatusHourlyAggregatesParams{
			DeviceIdentifiers: identifiers,
			Bucket:            startTime,
			Bucket_2:          endTime,
		})
	}

	if err != nil {
		s.logger.Error("failed to query hourly status aggregates", slog.String("error", err.Error()))
		return nil
	}

	statusRows := make([]statusData, len(rows))
	for i, row := range rows {
		statusRows[i] = extractStatusDataHourly(row)
	}
	return aggregateStatusRows(statusRows)
}

func (s *TimescaleTelemetryStore) getTemperatureCountsFromDailyAggregates(
	ctx context.Context,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
) []models.TemperatureStatusCount {
	var rows []sqlc.DeviceStatusDaily
	var err error

	if len(deviceIDs) == 0 {
		rows, err = s.queries.GetAllDeviceStatusDailyAggregates(ctx, sqlc.GetAllDeviceStatusDailyAggregatesParams{
			Bucket:   startTime,
			Bucket_2: endTime,
		})
	} else {
		identifiers := deviceIDsToStrings(deviceIDs)
		rows, err = s.queries.GetDeviceStatusDailyAggregates(ctx, sqlc.GetDeviceStatusDailyAggregatesParams{
			DeviceIdentifiers: identifiers,
			Bucket:            startTime,
			Bucket_2:          endTime,
		})
	}

	if err != nil {
		s.logger.Error("failed to query daily status aggregates", slog.String("error", err.Error()))
		return nil
	}

	statusRows := make([]statusData, len(rows))
	for i, row := range rows {
		statusRows[i] = extractStatusDataDaily(row)
	}
	return aggregateStatusRows(statusRows)
}

// aggregateHourlyRows aggregates hourly data rows into metrics.
func (s *TimescaleTelemetryStore) aggregateHourlyRows(
	rows []sqlc.DeviceMetricsHourly,
	measurementTypes []models.MeasurementType,
	aggregationTypes []models.AggregationType,
) []models.Metric {
	if len(measurementTypes) == 0 {
		measurementTypes = modelsV2.DefaultMeasurementTypes
	}
	if len(aggregationTypes) == 0 {
		aggregationTypes = []models.AggregationType{models.AggregationTypeAverage}
	}

	// Group by bucket time
	buckets := make(map[time.Time][]sqlc.DeviceMetricsHourly)
	for _, row := range rows {
		buckets[row.Bucket] = append(buckets[row.Bucket], row)
	}

	bucketTimes := make([]time.Time, 0, len(buckets))
	for t := range buckets {
		bucketTimes = append(bucketTimes, t)
	}
	sort.Slice(bucketTimes, func(i, j int) bool {
		return bucketTimes[i].Before(bucketTimes[j])
	})

	var allMetrics []models.Metric

	for _, bucketTime := range bucketTimes {
		bucketData := buckets[bucketTime]

		for _, measurementType := range measurementTypes {
			aggregatedValues, metricDeviceCount := s.aggregateHourlyBucket(bucketData, measurementType, aggregationTypes)
			if len(aggregatedValues) == 0 {
				continue
			}

			allMetrics = append(allMetrics, models.Metric{
				MeasurementType:  measurementType,
				AggregatedValues: aggregatedValues,
				OpenTime:         bucketTime,
				DeviceCount:      safeIntToInt32(metricDeviceCount),
			})
		}
	}

	return allMetrics
}

// aggregateHourlyBucket aggregates values from hourly rows for a single bucket.
// For non-cumulative metrics (temperature, efficiency, fan speed), averages are
// weighted by data_points so devices with more readings have proportionally more
// influence. Cumulative metrics (hashrate, power, current) sum per-device averages
// for fleet totals, unweighted.
func (s *TimescaleTelemetryStore) aggregateHourlyBucket(
	rows []sqlc.DeviceMetricsHourly,
	measurementType models.MeasurementType,
	aggregationTypes []models.AggregationType,
) ([]models.AggregatedValue, int) {
	isCumulative := isCumulativeMetric(measurementType)

	var avgSum float64
	var weightedSum float64
	var totalDataPoints int64
	var deviceCount int
	var realMinMaxCount int
	minOfMins := math.MaxFloat64
	maxOfMaxes := -math.MaxFloat64
	var cumulativeMinSum, cumulativeMaxSum float64

	for _, row := range rows {
		avg, minVal, maxVal, hasRealMinMax, ok := extractHourlyValues(row, measurementType)
		if !ok {
			continue
		}
		avgSum += avg
		weightedSum += avg * float64(row.DataPoints)
		totalDataPoints += row.DataPoints
		deviceCount++
		if hasRealMinMax {
			realMinMaxCount++
			if minVal < minOfMins {
				minOfMins = minVal
			}
			if maxVal > maxOfMaxes {
				maxOfMaxes = maxVal
			}
			cumulativeMinSum += minVal
			cumulativeMaxSum += maxVal
		}
	}

	if deviceCount == 0 {
		return nil, 0
	}

	// Emit MIN/MAX only when every contributing device had real min/max in the view —
	// otherwise a partial fleet sum (cumulative) or a biased extremum (non-cumulative)
	// would silently replace real data with a fabricated number.
	canEmitMinMax := realMinMaxCount == deviceCount && realMinMaxCount > 0

	var result []models.AggregatedValue
	for _, aggType := range aggregationTypes {
		var value float64
		switch aggType {
		case models.AggregationTypeAverage:
			if isCumulative {
				value = avgSum
			} else if totalDataPoints > 0 {
				value = weightedSum / float64(totalDataPoints)
			} else {
				value = avgSum / float64(deviceCount)
			}
		case models.AggregationTypeMin:
			if !canEmitMinMax {
				continue
			}
			if isCumulative {
				value = cumulativeMinSum
			} else {
				value = minOfMins
			}
		case models.AggregationTypeMax:
			if !canEmitMinMax {
				continue
			}
			if isCumulative {
				value = cumulativeMaxSum
			} else {
				value = maxOfMaxes
			}
		case models.AggregationTypeSum:
			value = avgSum
		case models.AggregationTypeCount:
			value = float64(deviceCount)
		case models.AggregationTypeUnknown, models.AggregationTypeTotal, models.AggregationTypeMeanChange:
			continue
		}
		result = append(result, models.AggregatedValue{
			Type:  aggType,
			Value: value,
		})
	}

	return result, deviceCount
}

// extractHourlyValues extracts avg, min, max values from an hourly row for a measurement type.
// hasRealMinMax reports whether the row's backing continuous aggregate stores true min/max for
// this measurement — when false, only avg is meaningful and min/max must be ignored.
func extractHourlyValues(row sqlc.DeviceMetricsHourly, mt models.MeasurementType) (avg, minVal, maxVal float64, hasRealMinMax, ok bool) {
	switch mt {
	case models.MeasurementTypeHashrate:
		if row.MaxHashRate.Valid && row.MinHashRate.Valid {
			return row.AvgHashRate, row.MinHashRate.Float64, row.MaxHashRate.Float64, true, true
		}
		return row.AvgHashRate, 0, 0, false, row.AvgHashRate > 0
	case models.MeasurementTypeTemperature:
		if row.MaxTemp.Valid && row.MinTemp.Valid {
			return row.AvgTemp, row.MinTemp.Float64, row.MaxTemp.Float64, true, true
		}
		return row.AvgTemp, 0, 0, false, row.AvgTemp > 0
	case models.MeasurementTypePower:
		return row.AvgPower, 0, 0, false, row.AvgPower > 0
	case models.MeasurementTypeEfficiency:
		return row.AvgEfficiency, 0, 0, false, row.AvgEfficiency > 0
	case models.MeasurementTypeFanSpeed:
		return row.AvgFanRpm, 0, 0, false, row.AvgFanRpm > 0
	case models.MeasurementTypeUnknown,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeCurrent,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return 0, 0, 0, false, false
	}
	return 0, 0, 0, false, false
}

// aggregateDailyRows aggregates daily data rows into metrics.
func (s *TimescaleTelemetryStore) aggregateDailyRows(
	rows []sqlc.DeviceMetricsDaily,
	measurementTypes []models.MeasurementType,
	aggregationTypes []models.AggregationType,
) []models.Metric {
	if len(measurementTypes) == 0 {
		measurementTypes = modelsV2.DefaultMeasurementTypes
	}
	if len(aggregationTypes) == 0 {
		aggregationTypes = []models.AggregationType{models.AggregationTypeAverage}
	}

	// Group by bucket time
	buckets := make(map[time.Time][]sqlc.DeviceMetricsDaily)
	for _, row := range rows {
		buckets[row.Bucket] = append(buckets[row.Bucket], row)
	}

	bucketTimes := make([]time.Time, 0, len(buckets))
	for t := range buckets {
		bucketTimes = append(bucketTimes, t)
	}
	sort.Slice(bucketTimes, func(i, j int) bool {
		return bucketTimes[i].Before(bucketTimes[j])
	})

	var allMetrics []models.Metric

	for _, bucketTime := range bucketTimes {
		bucketData := buckets[bucketTime]

		for _, measurementType := range measurementTypes {
			aggregatedValues, metricDeviceCount := s.aggregateDailyBucket(bucketData, measurementType, aggregationTypes)
			if len(aggregatedValues) == 0 {
				continue
			}

			allMetrics = append(allMetrics, models.Metric{
				MeasurementType:  measurementType,
				AggregatedValues: aggregatedValues,
				OpenTime:         bucketTime,
				DeviceCount:      safeIntToInt32(metricDeviceCount),
			})
		}
	}

	return allMetrics
}

// aggregateDailyBucket aggregates values from daily rows for a single bucket.
// For non-cumulative metrics (temperature, efficiency), averages are weighted by
// data_points so devices with more readings have proportionally more influence.
// Cumulative metrics (hashrate, power, current) sum per-device averages for fleet
// totals, unweighted.
func (s *TimescaleTelemetryStore) aggregateDailyBucket(
	rows []sqlc.DeviceMetricsDaily,
	measurementType models.MeasurementType,
	aggregationTypes []models.AggregationType,
) ([]models.AggregatedValue, int) {
	isCumulative := isCumulativeMetric(measurementType)

	var avgSum float64
	var weightedSum float64
	var totalDataPoints int64
	var deviceCount int
	var realMinMaxCount int
	minOfMins := math.MaxFloat64
	maxOfMaxes := -math.MaxFloat64
	var cumulativeMinSum, cumulativeMaxSum float64

	for _, row := range rows {
		avg, minVal, maxVal, hasRealMinMax, ok := extractDailyValues(row, measurementType)
		if !ok {
			continue
		}
		avgSum += avg
		weightedSum += avg * float64(row.DataPoints)
		totalDataPoints += row.DataPoints
		deviceCount++
		if hasRealMinMax {
			realMinMaxCount++
			if minVal < minOfMins {
				minOfMins = minVal
			}
			if maxVal > maxOfMaxes {
				maxOfMaxes = maxVal
			}
			cumulativeMinSum += minVal
			cumulativeMaxSum += maxVal
		}
	}

	if deviceCount == 0 {
		return nil, 0
	}

	canEmitMinMax := realMinMaxCount == deviceCount && realMinMaxCount > 0

	var result []models.AggregatedValue
	for _, aggType := range aggregationTypes {
		var value float64
		switch aggType {
		case models.AggregationTypeAverage:
			if isCumulative {
				value = avgSum
			} else if totalDataPoints > 0 {
				value = weightedSum / float64(totalDataPoints)
			} else {
				value = avgSum / float64(deviceCount)
			}
		case models.AggregationTypeMin:
			if !canEmitMinMax {
				continue
			}
			if isCumulative {
				value = cumulativeMinSum
			} else {
				value = minOfMins
			}
		case models.AggregationTypeMax:
			if !canEmitMinMax {
				continue
			}
			if isCumulative {
				value = cumulativeMaxSum
			} else {
				value = maxOfMaxes
			}
		case models.AggregationTypeSum:
			value = avgSum
		case models.AggregationTypeCount:
			value = float64(deviceCount)
		case models.AggregationTypeUnknown, models.AggregationTypeTotal, models.AggregationTypeMeanChange:
			continue
		}
		result = append(result, models.AggregatedValue{
			Type:  aggType,
			Value: value,
		})
	}

	return result, deviceCount
}

// extractDailyValues extracts avg, min, max values from a daily row for a measurement type.
// hasRealMinMax reports whether the row's backing continuous aggregate stores true min/max for
// this measurement — when false, only avg is meaningful and min/max must be ignored.
func extractDailyValues(row sqlc.DeviceMetricsDaily, mt models.MeasurementType) (avg, minVal, maxVal float64, hasRealMinMax, ok bool) {
	switch mt {
	case models.MeasurementTypeHashrate:
		if row.MaxHashRate.Valid && row.MinHashRate.Valid {
			return row.AvgHashRate, row.MinHashRate.Float64, row.MaxHashRate.Float64, true, true
		}
		return row.AvgHashRate, 0, 0, false, row.AvgHashRate > 0
	case models.MeasurementTypeTemperature:
		if row.MaxTemp.Valid && row.MinTemp.Valid {
			return row.AvgTemp, row.MinTemp.Float64, row.MaxTemp.Float64, true, true
		}
		return row.AvgTemp, 0, 0, false, row.AvgTemp > 0
	case models.MeasurementTypePower:
		return row.AvgPower, 0, 0, false, row.AvgPower > 0
	case models.MeasurementTypeEfficiency:
		return row.AvgEfficiency, 0, 0, false, row.AvgEfficiency > 0
	case models.MeasurementTypeFanSpeed,
		models.MeasurementTypeUnknown,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeCurrent,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return 0, 0, 0, false, false
	}
	return 0, 0, 0, false, false
}

// aggregateMetrics performs aggregations on the metrics data.
func (s *TimescaleTelemetryStore) aggregateMetrics(
	data []modelsV2.DeviceMetrics,
	measurementTypes []models.MeasurementType,
	aggregationTypes []models.AggregationType,
	windowDuration time.Duration,
) models.CombinedMetric {
	if len(data) == 0 {
		return models.CombinedMetric{}
	}

	sort.Slice(data, func(i, j int) bool {
		return data[i].Timestamp.Before(data[j].Timestamp)
	})

	buckets := make(map[time.Time][]modelsV2.DeviceMetrics)
	for _, m := range data {
		bucket := m.Timestamp.Truncate(windowDuration)
		buckets[bucket] = append(buckets[bucket], m)
	}

	bucketTimes := make([]time.Time, 0, len(buckets))
	for t := range buckets {
		bucketTimes = append(bucketTimes, t)
	}
	sort.Slice(bucketTimes, func(i, j int) bool {
		return bucketTimes[i].Before(bucketTimes[j])
	})

	if len(measurementTypes) == 0 {
		measurementTypes = modelsV2.DefaultMeasurementTypes
	}

	if len(aggregationTypes) == 0 {
		aggregationTypes = []models.AggregationType{models.AggregationTypeAverage}
	}

	allMetrics := make([]models.Metric, 0, len(buckets)*len(measurementTypes))
	tempCounts := make([]models.TemperatureStatusCount, 0, len(buckets))
	uptimeCounts := make([]models.UptimeStatusCount, 0, len(buckets))

	for _, bucketTime := range bucketTimes {
		bucketData := buckets[bucketTime]

		// Dedupe once per bucket — both status-count functions need a
		// per-device latest sample, with temperature using the latest
		// sample that actually has TempC populated.
		uptimeLatest, tempLatest := latestSamplesForStatusCounts(bucketData)

		tempCount := temperatureStatusCountFromLatest(tempLatest, bucketTime)
		tempCounts = append(tempCounts, tempCount)

		uptimeCount := uptimeStatusCountFromLatest(uptimeLatest, bucketTime)
		uptimeCounts = append(uptimeCounts, uptimeCount)

		for _, measurementType := range measurementTypes {
			var aggregatedValues []models.AggregatedValue
			var metricDeviceCount int

			if isCumulativeMetric(measurementType) {
				aggregatedValues, metricDeviceCount = calculateCumulativeAggregations(bucketData, measurementType, aggregationTypes)
			} else {
				values := extractValues(bucketData, measurementType)
				if len(values) == 0 {
					continue
				}
				metricDeviceCount = countUniqueDevicesWithMeasurement(bucketData, measurementType)
				aggregatedValues = make([]models.AggregatedValue, 0, len(aggregationTypes))
				for _, aggType := range aggregationTypes {
					aggValue := calculateAggregation(values, aggType)
					aggregatedValues = append(aggregatedValues, models.AggregatedValue{
						Type:  aggType,
						Value: aggValue,
					})
				}
			}

			if len(aggregatedValues) == 0 {
				continue
			}

			allMetrics = append(allMetrics, models.Metric{
				MeasurementType:  measurementType,
				AggregatedValues: aggregatedValues,
				OpenTime:         bucketTime,
				DeviceCount:      safeIntToInt32(metricDeviceCount),
			})
		}
	}

	return models.CombinedMetric{
		Metrics:                 allMetrics,
		TemperatureStatusCounts: tempCounts,
		UptimeStatusCounts:      uptimeCounts,
	}
}

// Ping checks if the database connection is alive.
func (s *TimescaleTelemetryStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *TimescaleTelemetryStore) InsertMinerStateSnapshot(ctx context.Context, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.WriteTimeout)
	defer cancel()

	if err := s.queries.InsertMinerStateSnapshot(ctx, at); err != nil {
		return fmt.Errorf("insert miner state snapshot: %w", err)
	}
	return nil
}

func (s *TimescaleTelemetryStore) getUptimeStatusCountsFromSnapshots(
	ctx context.Context,
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
	bucketDuration time.Duration,
) []models.UptimeStatusCount {
	if orgID == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	switch selectUptimeDataSource(&startTime, &endTime) {
	case uptimeDataSourceDaily:
		// Daily rollup has 1d granularity.
		if bucketDuration < dailyBucketDuration {
			bucketDuration = dailyBucketDuration
		}
		return s.queryUptimeDaily(ctx, orgID, deviceIDs, startTime, endTime, bucketDuration)
	case uptimeDataSourceHourly:
		// Hourly rollup has 1h granularity.
		if bucketDuration < hourlyBucketDuration {
			bucketDuration = hourlyBucketDuration
		}
		return s.queryUptimeHourly(ctx, orgID, deviceIDs, startTime, endTime, bucketDuration)
	case uptimeDataSourceRaw:
		fallthrough
	default:
		// Snapshot cadence is ~60s, so finer buckets would yield empty slots.
		if bucketDuration < time.Minute {
			bucketDuration = time.Minute
		}
		return s.queryUptimeRaw(ctx, orgID, deviceIDs, startTime, endTime, bucketDuration)
	}
}

// uptimeDataSource mirrors dataSource for miner_state_snapshots — choosing
// between the raw hypertable and its hourly or daily continuous aggregates.
type uptimeDataSource int

const (
	uptimeDataSourceRaw uptimeDataSource = iota
	uptimeDataSourceHourly
	uptimeDataSourceDaily
)

func (ds uptimeDataSource) String() string {
	switch ds {
	case uptimeDataSourceRaw:
		return "raw"
	case uptimeDataSourceHourly:
		return "hourly"
	case uptimeDataSourceDaily:
		return "daily"
	default:
		return "unknown"
	}
}

// selectUptimeDataSource mirrors selectDataSource: route by window duration,
// shorter → higher resolution. Raw retention (30d) always covers rawDataMaxDuration
// and hourly retention (3 months) covers hourlyMaxDuration, so the duration
// thresholds never outrun the source data.
func selectUptimeDataSource(startTime, endTime *time.Time) uptimeDataSource {
	if startTime == nil || endTime == nil {
		return uptimeDataSourceRaw
	}
	duration := endTime.Sub(*startTime)
	if duration <= rawDataMaxDuration {
		return uptimeDataSourceRaw
	}
	if duration <= hourlyMaxDuration {
		return uptimeDataSourceHourly
	}
	return uptimeDataSourceDaily
}

func (s *TimescaleTelemetryStore) queryUptimeRaw(
	ctx context.Context,
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
	bucketDuration time.Duration,
) []models.UptimeStatusCount {
	params := sqlc.GetMinerStateSnapshotsParams{
		BucketInterval: fmt.Sprintf("%d seconds", int64(bucketDuration.Seconds())),
		OrgID:          orgID,
		StartTime:      startTime,
		EndTime:        endTime,
	}
	if len(deviceIDs) > 0 {
		params.DeviceIdentifiersFilter = nargActive
		params.DeviceIdentifierValues = deviceIDsToStrings(deviceIDs)
	}

	rows, err := s.queries.GetMinerStateSnapshots(ctx, params)
	if err != nil {
		s.logger.Error("failed to query miner state snapshots",
			slog.Int64("org_id", orgID),
			slog.String("error", err.Error()))
		return nil
	}

	if len(rows) == 0 {
		return nil
	}

	result := make([]models.UptimeStatusCount, 0, len(rows))
	for _, row := range rows {
		result = append(result, models.UptimeStatusCount{
			Timestamp:       row.Bucket,
			HashingCount:    row.HashingCount,
			BrokenCount:     row.BrokenCount,
			NotHashingCount: row.OfflineCount + row.SleepingCount,
		})
	}
	return result
}

func (s *TimescaleTelemetryStore) queryUptimeHourly(
	ctx context.Context,
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
	bucketDuration time.Duration,
) []models.UptimeStatusCount {
	params := sqlc.GetMinerStateSnapshotsHourlyParams{
		BucketInterval: fmt.Sprintf("%d seconds", int64(bucketDuration.Seconds())),
		OrgID:          orgID,
		StartTime:      startTime,
		EndTime:        endTime,
	}
	if len(deviceIDs) > 0 {
		params.DeviceIdentifiersFilter = nargActive
		params.DeviceIdentifierValues = deviceIDsToStrings(deviceIDs)
	}

	rows, err := s.queries.GetMinerStateSnapshotsHourly(ctx, params)
	if err != nil {
		s.logger.Error("failed to query hourly miner state snapshots",
			slog.Int64("org_id", orgID),
			slog.String("error", err.Error()))
		return nil
	}

	if len(rows) == 0 {
		return nil
	}

	result := make([]models.UptimeStatusCount, 0, len(rows))
	for _, row := range rows {
		result = append(result, models.UptimeStatusCount{
			Timestamp:       row.Bucket,
			HashingCount:    row.HashingCount,
			BrokenCount:     row.BrokenCount,
			NotHashingCount: row.OfflineCount + row.SleepingCount,
		})
	}
	return result
}

func (s *TimescaleTelemetryStore) queryUptimeDaily(
	ctx context.Context,
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
	bucketDuration time.Duration,
) []models.UptimeStatusCount {
	params := sqlc.GetMinerStateSnapshotsDailyParams{
		BucketInterval: fmt.Sprintf("%d seconds", int64(bucketDuration.Seconds())),
		OrgID:          orgID,
		StartTime:      startTime,
		EndTime:        endTime,
	}
	if len(deviceIDs) > 0 {
		params.DeviceIdentifiersFilter = nargActive
		params.DeviceIdentifierValues = deviceIDsToStrings(deviceIDs)
	}

	rows, err := s.queries.GetMinerStateSnapshotsDaily(ctx, params)
	if err != nil {
		s.logger.Error("failed to query daily miner state snapshots",
			slog.Int64("org_id", orgID),
			slog.String("error", err.Error()))
		return nil
	}

	if len(rows) == 0 {
		return nil
	}

	result := make([]models.UptimeStatusCount, 0, len(rows))
	for _, row := range rows {
		result = append(result, models.UptimeStatusCount{
			Timestamp:       row.Bucket,
			HashingCount:    row.HashingCount,
			BrokenCount:     row.BrokenCount,
			NotHashingCount: row.OfflineCount + row.SleepingCount,
		})
	}
	return result
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func sqlcMetricsToDeviceMetrics(row sqlc.DeviceMetric) modelsV2.DeviceMetrics {
	m := modelsV2.DeviceMetrics{
		DeviceIdentifier: row.DeviceIdentifier,
		Timestamp:        row.Time,
	}

	if row.Health.Valid {
		health, err := modelsV2.ParseHealthStatus(row.Health.String)
		if err == nil {
			m.Health = health
		}
	}

	if row.HashRateHs.Valid {
		kind := parseMetricKindOrDefault(row.HashRateHsKind.String)
		m.HashrateHS = &modelsV2.MetricValue{
			Value: row.HashRateHs.Float64,
			Kind:  kind,
		}
	}
	if row.TempC.Valid {
		kind := parseMetricKindOrDefault(row.TempCKind.String)
		m.TempC = &modelsV2.MetricValue{
			Value: row.TempC.Float64,
			Kind:  kind,
		}
	}
	if row.FanRpm.Valid {
		kind := parseMetricKindOrDefault(row.FanRpmKind.String)
		m.FanRPM = &modelsV2.MetricValue{
			Value: row.FanRpm.Float64,
			Kind:  kind,
		}
	}
	if row.PowerW.Valid {
		kind := parseMetricKindOrDefault(row.PowerWKind.String)
		m.PowerW = &modelsV2.MetricValue{
			Value: row.PowerW.Float64,
			Kind:  kind,
		}
	}
	if row.EfficiencyJh.Valid {
		kind := parseMetricKindOrDefault(row.EfficiencyJhKind.String)
		m.EfficiencyJH = &modelsV2.MetricValue{
			Value: row.EfficiencyJh.Float64,
			Kind:  kind,
		}
	}

	return m
}

func parseMetricKindOrDefault(s string) modelsV2.MetricKind {
	kind, err := modelsV2.ParseMetricKind(s)
	if err != nil {
		return modelsV2.MetricKindGauge
	}
	return kind
}

// latestSamplePerDevice returns one DeviceMetrics per device — the sample with
// the latest Timestamp. Raw buckets contain many samples per device (~10s
// polling cadence), so callers that classify devices into status categories
// must dedupe first to avoid counting one device multiple times.
func latestSamplePerDevice(data []modelsV2.DeviceMetrics) []modelsV2.DeviceMetrics {
	if len(data) == 0 {
		return nil
	}
	latest := make(map[string]modelsV2.DeviceMetrics, len(data))
	for _, m := range data {
		existing, ok := latest[m.DeviceIdentifier]
		if !ok || m.Timestamp.After(existing.Timestamp) {
			latest[m.DeviceIdentifier] = m
		}
	}
	out := make([]modelsV2.DeviceMetrics, 0, len(latest))
	for _, m := range latest {
		out = append(out, m)
	}
	return out
}

// latestSamplesForStatusCounts walks the bucket once and returns two deduped
// views: the latest sample per device (for uptime), and the latest sample per
// device that has a TempC reading (for temperature). The TempC-aware view
// avoids dropping a device just because its very latest sample happens to be
// missing a temperature — TempC reporting can be intermittent, while Health
// is set on every sample.
func latestSamplesForStatusCounts(data []modelsV2.DeviceMetrics) (uptime, temperature []modelsV2.DeviceMetrics) {
	if len(data) == 0 {
		return nil, nil
	}
	allLatest := make(map[string]modelsV2.DeviceMetrics, len(data))
	tempLatest := make(map[string]modelsV2.DeviceMetrics, len(data))
	for _, m := range data {
		if existing, ok := allLatest[m.DeviceIdentifier]; !ok || m.Timestamp.After(existing.Timestamp) {
			allLatest[m.DeviceIdentifier] = m
		}
		if m.TempC == nil {
			continue
		}
		if existing, ok := tempLatest[m.DeviceIdentifier]; !ok || m.Timestamp.After(existing.Timestamp) {
			tempLatest[m.DeviceIdentifier] = m
		}
	}
	uptime = make([]modelsV2.DeviceMetrics, 0, len(allLatest))
	for _, m := range allLatest {
		uptime = append(uptime, m)
	}
	temperature = make([]modelsV2.DeviceMetrics, 0, len(tempLatest))
	for _, m := range tempLatest {
		temperature = append(temperature, m)
	}
	return uptime, temperature
}

// calculateTemperatureStatusCount dedupes the bucket and counts each device
// once, using its latest sample that has a TempC reading. Prefer
// temperatureStatusCountFromLatest in hot loops where the caller has already
// deduped.
func calculateTemperatureStatusCount(data []modelsV2.DeviceMetrics, timestamp time.Time) models.TemperatureStatusCount {
	_, temp := latestSamplesForStatusCounts(data)
	return temperatureStatusCountFromLatest(temp, timestamp)
}

// temperatureStatusCountFromLatest classifies an already-deduped slice (one
// DeviceMetrics per device, latest sample with TempC populated).
func temperatureStatusCountFromLatest(latestPerDevice []modelsV2.DeviceMetrics, timestamp time.Time) models.TemperatureStatusCount {
	var cold, ok, hot, critical int32

	for _, m := range latestPerDevice {
		if m.TempC == nil {
			continue
		}
		temp := m.TempC.Value
		switch {
		case temp < tempThresholdCold:
			cold++
		case temp < tempThresholdHot:
			ok++
		case temp < tempThresholdCritical:
			hot++
		default:
			critical++
		}
	}

	return models.TemperatureStatusCount{
		Timestamp:     timestamp,
		ColdCount:     cold,
		OkCount:       ok,
		HotCount:      hot,
		CriticalCount: critical,
	}
}

// calculateUptimeStatusCount dedupes the bucket and counts each device once.
// Prefer uptimeStatusCountFromLatest in hot loops where the caller has
// already deduped.
func calculateUptimeStatusCount(data []modelsV2.DeviceMetrics, timestamp time.Time) models.UptimeStatusCount {
	return uptimeStatusCountFromLatest(latestSamplePerDevice(data), timestamp)
}

// uptimeStatusCountFromLatest classifies an already-deduped slice (one
// DeviceMetrics per device, latest sample in the bucket).
func uptimeStatusCountFromLatest(latestPerDevice []modelsV2.DeviceMetrics, timestamp time.Time) models.UptimeStatusCount {
	var hashing, notHashing int32

	for _, m := range latestPerDevice {
		if m.Health == modelsV2.HealthHealthyActive {
			hashing++
		} else {
			notHashing++
		}
	}

	return models.UptimeStatusCount{
		Timestamp:       timestamp,
		HashingCount:    hashing,
		NotHashingCount: notHashing,
	}
}

func extractValues(data []modelsV2.DeviceMetrics, measurementType models.MeasurementType) []float64 {
	var values []float64
	for _, m := range data {
		if value, _, ok := m.ExtractRawMeasurement(measurementType); ok {
			values = append(values, value)
		}
	}
	return values
}

// countUniqueDevicesWithMeasurement returns the number of distinct devices that
// have data for the given measurement type. Raw data may contain multiple
// readings per device per bucket, so a simple len(extractValues) overcounts.
func countUniqueDevicesWithMeasurement(data []modelsV2.DeviceMetrics, mt models.MeasurementType) int {
	seen := make(map[string]struct{})
	for _, m := range data {
		if _, _, ok := m.ExtractRawMeasurement(mt); ok {
			seen[m.DeviceIdentifier] = struct{}{}
		}
	}
	return len(seen)
}

// calculateCumulativeAggregations handles cumulative metrics (hashrate, power) by:
// 1. Grouping values by device
// 2. Calculating per-device aggregates (avg, min, max, latest)
// 3. Summing across all devices for fleet totals
func calculateCumulativeAggregations(
	data []modelsV2.DeviceMetrics,
	measurementType models.MeasurementType,
	aggregationTypes []models.AggregationType,
) ([]models.AggregatedValue, int) {
	deviceValues := make(map[string][]float64)
	for _, m := range data {
		if value, _, ok := m.ExtractRawMeasurement(measurementType); ok {
			deviceValues[m.DeviceIdentifier] = append(deviceValues[m.DeviceIdentifier], value)
		}
	}

	if len(deviceValues) == 0 {
		return nil, 0
	}

	type perDeviceAggs struct {
		avg, min, max, latest float64
	}
	deviceAggs := make([]perDeviceAggs, 0, len(deviceValues))

	for _, values := range deviceValues {
		if len(values) == 0 {
			continue
		}

		var sum float64
		minVal := values[0]
		maxVal := values[0]
		for _, v := range values {
			sum += v
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}

		deviceAggs = append(deviceAggs, perDeviceAggs{
			avg:    sum / float64(len(values)),
			min:    minVal,
			max:    maxVal,
			latest: values[len(values)-1],
		})
	}

	// Sum across all devices for fleet totals
	var totalAvg, totalMin, totalMax, totalSum float64
	for _, agg := range deviceAggs {
		totalAvg += agg.avg
		totalMin += agg.min
		totalMax += agg.max
		totalSum += agg.latest
	}

	result := make([]models.AggregatedValue, 0, len(aggregationTypes))
	for _, aggType := range aggregationTypes {
		var value float64
		switch aggType {
		case models.AggregationTypeAverage:
			value = totalAvg
		case models.AggregationTypeMin:
			value = totalMin
		case models.AggregationTypeMax:
			value = totalMax
		case models.AggregationTypeSum:
			value = totalSum
		case models.AggregationTypeCount:
			value = float64(len(deviceAggs))
		case models.AggregationTypeUnknown, models.AggregationTypeTotal, models.AggregationTypeMeanChange:
			continue
		}
		result = append(result, models.AggregatedValue{
			Type:  aggType,
			Value: value,
		})
	}

	return result, len(deviceAggs)
}

func calculateAggregation(values []float64, aggType models.AggregationType) float64 {
	if len(values) == 0 {
		return 0
	}

	// Pre-calculate all aggregates in a single pass for efficiency
	sum := values[0]
	minVal := values[0]
	maxVal := values[0]
	for _, v := range values[1:] {
		sum += v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	switch aggType {
	case models.AggregationTypeAverage:
		return sum / float64(len(values))
	case models.AggregationTypeSum:
		return sum
	case models.AggregationTypeMin:
		return minVal
	case models.AggregationTypeMax:
		return maxVal
	case models.AggregationTypeCount:
		return float64(len(values))
	case models.AggregationTypeUnknown, models.AggregationTypeTotal, models.AggregationTypeMeanChange:
		return 0
	default:
		return 0
	}
}

// safeIntToInt32 converts an int to int32, clamping to math.MaxInt32 if needed.
func safeIntToInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds checked above
}

// isCumulativeMetric returns true if the metric type represents a value that should be
// summed across devices for fleet totals (hashrate, power, current).
// Non-cumulative metrics (temperature, efficiency, fan speed) are averaged.
func isCumulativeMetric(measurementType models.MeasurementType) bool {
	switch measurementType {
	case models.MeasurementTypeHashrate,
		models.MeasurementTypePower,
		models.MeasurementTypeCurrent:
		return true
	case models.MeasurementTypeUnknown,
		models.MeasurementTypeTemperature,
		models.MeasurementTypeEfficiency,
		models.MeasurementTypeFanSpeed,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return false
	}
	return false
}
