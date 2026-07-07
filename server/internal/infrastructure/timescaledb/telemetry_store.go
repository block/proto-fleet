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
	maxRawMetricBuckets  = 10000

	// Wide selectors scan one raw row per device per poll interval, so an
	// org-wide 24h request at a 5k fleet reads ~43M rows; past these exact
	// limits (no estimates) such requests serve from the hourly aggregates.
	rawAllDevicesMaxDuration = 5 * time.Hour
	maxRawDeviceList         = 500

	// hourlyRawTailCoverage is how much of a raw-window request's right edge
	// the hourly path serves from raw data instead of the aggregate. The
	// hourly CAGG refresh policy (migration 000006) materializes on a
	// 30 minute schedule with a 1 hour end_offset, so hour buckets starting
	// within the last ~2.5h can be unmaterialized. A 2h tail whose start is
	// truncated to the hour keeps the body at buckets starting >=3h before
	// the request end, clear of that worst case. Must track those policy
	// values.
	hourlyRawTailCoverage = 2 * time.Hour

	// Raw snapshot scans cost one row per device per minute, and rollup gaps
	// come from the same writer outages as raw gaps, so an oversized raw
	// fallback pays a huge scan to recover nothing. All-devices requests are
	// bounded by range alone (fleet size is unknown here); device lists by
	// estimated scanned rows (600k ~ the 2h cap at a 5k-device fleet).
	maxRawUptimeFallbackRange = 2 * time.Hour
	maxRawUptimeFallbackRows  = 600_000

	// fleetMetricRollupReadMaxDuration keeps the new 90s dashboard rollup on
	// the raw-window paths. Longer ranges already use hourly/daily aggregates.
	fleetMetricRollupReadMaxDuration = rawDataMaxDuration

	// Energy estimation constants.
	// Each telemetry data point represents one polling interval of device uptime.
	pollingIntervalSeconds = 10.0
	secondsPerHour         = 3600.0
	wattsPerKilowatt       = 1000.0
)

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

// selectDataSource routes by resolved duration and selector width. Raw scan
// cost grows with devices x duration, so wide selectors (all devices, or
// device lists past maxRawDeviceList) serve raw only up to
// rawAllDevicesMaxDuration and fall to the continuous aggregates beyond it.
func selectDataSource(startTime, endTime time.Time, deviceCount int) dataSource {
	duration := endTime.Sub(startTime)
	narrow := deviceCount > 0 && deviceCount <= maxRawDeviceList
	if duration <= rawDataMaxDuration && (narrow || duration <= rawAllDevicesMaxDuration) {
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

func rawMetricBucketDuration(slideInterval *time.Duration, allDevices bool) time.Duration {
	bucketDuration := DefaultBucketDuration
	if slideInterval != nil && *slideInterval > 0 {
		bucketDuration = *slideInterval
	}
	if bucketDuration < time.Second {
		return DefaultBucketDuration
	}
	if allDevices && bucketDuration < DefaultBucketDuration {
		return DefaultBucketDuration
	}
	return bucketDuration
}

// timeBucketOrigin is TimescaleDB's default time_bucket origin for
// sub-month intervals. Bucket counting must use the same grid: an unaligned
// window straddles one more boundary than its duration implies, so counting
// duration/width+1 undercounts and can let a request past the bucket cap.
var timeBucketOrigin = time.Date(2000, time.January, 3, 0, 0, 0, 0, time.UTC)

func rawMetricBucketCount(startTime, endTime time.Time, bucketDuration time.Duration) int64 {
	if bucketDuration <= 0 || endTime.Before(startTime) {
		return 0
	}
	width := bucketDuration.Nanoseconds()
	firstBucket := floorDiv(startTime.Sub(timeBucketOrigin).Nanoseconds(), width)
	lastBucket := floorDiv(endTime.Sub(timeBucketOrigin).Nanoseconds(), width)
	return lastBucket - firstBucket + 1
}

// floorDiv floors instead of truncating toward zero so pre-origin timestamps
// still map to the correct bucket index.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// completeRawBucketWindow shrinks a window to whole time_bucket cells: the
// start rounds up and the end rounds down to the grid. Partial edge buckets
// only contain devices sampled inside the clipped fragment, so fleet sums sag
// at both window edges and the newest point rewrites itself as the bucket
// fills. ok is false when no complete bucket fits.
func completeRawBucketWindow(startTime, endTime time.Time, bucketDuration time.Duration) (time.Time, time.Time, bool) {
	if bucketDuration <= 0 || endTime.Before(startTime) {
		return time.Time{}, time.Time{}, false
	}
	width := bucketDuration.Nanoseconds()
	startOffset := startTime.Sub(timeBucketOrigin).Nanoseconds()
	firstBucket := floorDiv(startOffset, width)
	if startOffset%width != 0 {
		firstBucket++
	}
	// The queries filter time <= end, so a cell is fully covered once end
	// reaches its last representable instant, one nanosecond short of the
	// next boundary
	lastBucket := floorDiv(endTime.Sub(timeBucketOrigin).Nanoseconds()+1, width) - 1
	if lastBucket < firstBucket {
		return time.Time{}, time.Time{}, false
	}
	alignedStart := timeBucketOrigin.Add(time.Duration(firstBucket * width))
	// The queries filter time <= end, so the end lands just inside the last
	// complete bucket
	alignedEnd := timeBucketOrigin.Add(time.Duration((lastBucket+1)*width) - time.Nanosecond)
	return alignedStart, alignedEnd, true
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
// tempThresholdCold=0, tempThresholdHot=70, tempThresholdCritical=90:
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

	if s.config.AsyncMetricCommit {
		if err := qtx.DisableSyncCommit(ctx); err != nil {
			return fmt.Errorf("failed to set async commit: %w", err)
		}
	}

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
	startTime, endTime := s.getTimeRange(query.TimeRange)
	ds := selectDataSource(startTime, endTime, len(query.DeviceIDs))

	s.logger.Debug("selected data source for combined metrics",
		slog.String("source", ds.String()),
		slog.Time("start_time", startTime),
		slog.Time("end_time", endTime),
		slog.Int("device_count", len(query.DeviceIDs)))

	switch ds {
	case dataSourceRaw:
		return s.getCombinedMetricsFromRaw(ctx, query, startTime, endTime)
	case dataSourceHourly:
		return s.getCombinedMetricsFromHourly(ctx, query, startTime, endTime)
	case dataSourceDaily:
		return s.getCombinedMetricsFromDaily(ctx, query, startTime, endTime)
	}
	return s.getCombinedMetricsFromRaw(ctx, query, startTime, endTime)
}

// getCombinedMetricsFromRaw queries raw device_metrics table (for short time ranges).
func (s *TimescaleTelemetryStore) getCombinedMetricsFromRaw(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	if endTime.Before(startTime) {
		return models.CombinedMetric{}, fmt.Errorf("raw combined metrics end time must be after start time")
	}
	if endTime.Sub(startTime) > rawDataMaxDuration {
		return models.CombinedMetric{}, fmt.Errorf("raw combined metrics range exceeds %s", rawDataMaxDuration)
	}

	bucketDuration := rawMetricBucketDuration(query.SlideInterval, len(query.DeviceIDs) == 0)
	if rawMetricBucketCount(startTime, endTime, bucketDuration) > maxRawMetricBuckets {
		return models.CombinedMetric{}, fmt.Errorf("raw combined metrics bucket count exceeds %d", maxRawMetricBuckets)
	}
	startTime, endTime, hasCompleteBucket := completeRawBucketWindow(startTime, endTime, bucketDuration)
	if !hasCompleteBucket {
		return models.CombinedMetric{}, nil
	}
	buckets, err := s.bucketAggregatesPreferRollup(ctx, query, startTime, endTime, bucketDuration)
	if err != nil {
		return models.CombinedMetric{}, err
	}

	includeUptimeCounts := models.ShouldIncludeUptimeStatusCounts(query.MeasurementTypes)
	result := aggregateRawMetricBuckets(buckets, query.MeasurementTypes, query.AggregationTypes)
	if includeUptimeCounts {
		result.UptimeStatusCounts = s.uptimeCountsForQuery(ctx, query, startTime, endTime, bucketDuration, dataSourceRaw)
	}

	return result, nil
}

// rawBucketAggregates runs the bucketed raw device_metrics query for the
// selector in the query and returns the rows ordered by bucket ascending.
func (s *TimescaleTelemetryStore) rawBucketAggregates(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time, bucketDuration time.Duration) ([]rawMetricBucket, error) {
	bucketSeconds := bucketDuration.Seconds()
	if len(query.DeviceIDs) == 0 {
		if query.OrganizationID != 0 {
			rows, err := s.queries.GetOrgDeviceMetricsRawBucketAggregates(ctx, sqlc.GetOrgDeviceMetricsRawBucketAggregatesParams{
				BucketSeconds: bucketSeconds,
				StartTime:     startTime,
				EndTime:       endTime,
				OrgID:         query.OrganizationID,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to query org raw bucket aggregates: %w", err)
			}
			buckets := make([]rawMetricBucket, 0, len(rows))
			for _, row := range rows {
				buckets = append(buckets, rawMetricBucketFromOrgRaw(row))
			}
			return buckets, nil
		}

		rows, err := s.queries.GetAllDeviceMetricsRawBucketAggregates(ctx, sqlc.GetAllDeviceMetricsRawBucketAggregatesParams{
			BucketSeconds: bucketSeconds,
			StartTime:     startTime,
			EndTime:       endTime,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query raw bucket aggregates: %w", err)
		}
		buckets := make([]rawMetricBucket, 0, len(rows))
		for _, row := range rows {
			buckets = append(buckets, rawMetricBucketFromAllDevices(row))
		}
		return buckets, nil
	}

	rows, err := s.queries.GetDeviceMetricsRawBucketAggregates(ctx, sqlc.GetDeviceMetricsRawBucketAggregatesParams{
		BucketSeconds:     bucketSeconds,
		DeviceIdentifiers: deviceIDsToStrings(query.DeviceIDs),
		StartTime:         startTime,
		EndTime:           endTime,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query raw bucket aggregates: %w", err)
	}
	buckets := make([]rawMetricBucket, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, rawMetricBucketFromDevices(row))
	}
	return buckets, nil
}

// bucketAggregatesPreferRollup serves dashboard-shaped org-wide fleet requests
// from the app-maintained 90s rollup, plus the two newest complete buckets
// from raw telemetry. Device-list and site-scoped queries keep the raw path
// because the rollup cannot enforce the service-resolved device predicate.
func (s *TimescaleTelemetryStore) bucketAggregatesPreferRollup(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time, bucketDuration time.Duration) ([]rawMetricBucket, error) {
	if !fleetMetricRollupEligible(query, startTime, endTime, bucketDuration) {
		return s.rawBucketAggregates(ctx, query, startTime, endTime, bucketDuration)
	}

	bodyStart, bodyEndExclusive, ok := fleetMetricRollupWindows(startTime, endTime)
	if !ok {
		return s.rawBucketAggregates(ctx, query, startTime, endTime, bucketDuration)
	}

	coverage, err := s.queries.GetFleetMetricRollupCoverage(ctx)
	if err != nil {
		s.logger.Warn("fleet metric rollup coverage lookup failed, falling back to raw",
			slog.Int64("org_id", query.OrganizationID),
			slog.Time("start_time", bodyStart),
			slog.Time("end_time", bodyEndExclusive),
			slog.String("error", err.Error()))
		return s.rawBucketAggregates(ctx, query, startTime, endTime, bucketDuration)
	}
	requiredLatest := bodyEndExclusive.Add(-models.FleetMetricRollupBucketDuration)
	if coverage.EarliestBucket.After(bodyStart) || coverage.LatestBucket.Before(requiredLatest) {
		s.logger.Warn("fleet metric rollup coverage incomplete, falling back to raw",
			slog.Int64("org_id", query.OrganizationID),
			slog.Bool("site_scoped", query.DeviceListFromSiteScope),
			slog.Time("start_time", bodyStart),
			slog.Time("end_time", bodyEndExclusive),
			slog.Time("coverage_earliest_bucket", coverage.EarliestBucket),
			slog.Time("coverage_latest_bucket", coverage.LatestBucket),
			slog.Time("required_latest_bucket", requiredLatest))
		return s.rawBucketAggregates(ctx, query, startTime, endTime, bucketDuration)
	}

	body, err := s.readFleetMetricRollupBuckets(ctx, query, bodyStart, bodyEndExclusive)
	if err != nil {
		s.logger.Warn("fleet metric rollup read failed, falling back to raw",
			slog.Int64("org_id", query.OrganizationID),
			slog.Bool("site_scoped", query.DeviceListFromSiteScope),
			slog.Time("start_time", bodyStart),
			slog.Time("end_time", bodyEndExclusive),
			slog.String("error", err.Error()))
		return s.rawBucketAggregates(ctx, query, startTime, endTime, bucketDuration)
	}

	tail, err := s.rawBucketAggregates(ctx, query, bodyEndExclusive, endTime, bucketDuration)
	if err != nil {
		return nil, err
	}
	return append(body, tail...), nil
}

func fleetMetricRollupEligible(query models.CombinedMetricsQuery, startTime, endTime time.Time, bucketDuration time.Duration) bool {
	if query.OrganizationID == 0 {
		return false
	}
	if bucketDuration != models.FleetMetricRollupBucketDuration {
		return false
	}
	if endTime.Sub(startTime) > fleetMetricRollupReadMaxDuration {
		return false
	}
	if query.DeviceListFromSiteScope {
		return false
	}
	return len(query.DeviceIDs) == 0
}

func fleetMetricRollupWindows(startTime, endTime time.Time) (bodyStart, bodyEndExclusive time.Time, ok bool) {
	lastBucketStart := models.TruncateToFleetRollupBucket(endTime)
	bodyEndExclusive = lastBucketStart.Add(-time.Duration(models.FleetMetricRollupRawTailBuckets-1) * models.FleetMetricRollupBucketDuration)
	if !bodyEndExclusive.After(startTime) {
		return time.Time{}, time.Time{}, false
	}
	return startTime, bodyEndExclusive, true
}

func fleetRollupBucketCountExclusive(startTime, endTime time.Time) int64 {
	if !endTime.After(startTime) {
		return 0
	}
	return rawMetricBucketCount(startTime, endTime.Add(-time.Nanosecond), models.FleetMetricRollupBucketDuration)
}

func (s *TimescaleTelemetryStore) readFleetMetricRollupBuckets(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time) ([]rawMetricBucket, error) {
	rows, err := s.queries.GetOrgFleetMetricRollups(ctx, sqlc.GetOrgFleetMetricRollupsParams{
		OrgID:     query.OrganizationID,
		StartTime: startTime,
		EndTime:   endTime,
	})
	if err != nil {
		return nil, fmt.Errorf("query org fleet metric rollups: %w", err)
	}
	buckets := make([]rawMetricBucket, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, rawMetricBucketFromOrgRollup(row))
	}
	return buckets, nil
}

// getCombinedMetricsFromHourly queries device_metrics_hourly continuous aggregate.
func (s *TimescaleTelemetryStore) getCombinedMetricsFromHourly(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	// Requests inside the raw window get their right edge served from raw
	// data because the CAGG materializes on a delay (see
	// hourlyRawTailCoverage). The body is normalized against the tail start
	// so body buckets end exactly where the tail begins. Longer ranges keep
	// the long-standing complete-bucket behavior with no tail.
	bodyWindowEnd := endTime
	var tailStart time.Time
	hasTail := endTime.After(startTime) && endTime.Sub(startTime) <= rawDataMaxDuration
	if hasTail {
		tailStart = endTime.Add(-hourlyRawTailCoverage).Truncate(time.Hour)
		if tailStart.Before(startTime) {
			tailStart = startTime
		}
		bodyWindowEnd = tailStart
	}

	var result models.CombinedMetric
	bodyStart, bodyEnd, hasCompleteBucket := normalizeCompleteBucketRange(startTime, bodyWindowEnd, hourlyBucketDuration)
	if hasCompleteBucket {
		var rows []sqlc.DeviceMetricsHourly
		var err error

		if len(query.DeviceIDs) == 0 {
			if query.OrganizationID != 0 {
				rows, err = s.queries.GetOrgDeviceMetricsHourlyAggregates(ctx, sqlc.GetOrgDeviceMetricsHourlyAggregatesParams{
					OrgID:     query.OrganizationID,
					StartTime: bodyStart,
					EndTime:   bodyEnd,
				})
			} else {
				rows, err = s.queries.GetAllDeviceMetricsHourlyAggregates(ctx, sqlc.GetAllDeviceMetricsHourlyAggregatesParams{
					Bucket:   bodyStart,
					Bucket_2: bodyEnd,
				})
			}
		} else {
			identifiers := deviceIDsToStrings(query.DeviceIDs)
			rows, err = s.queries.GetDeviceMetricsHourlyAggregates(ctx, sqlc.GetDeviceMetricsHourlyAggregatesParams{
				DeviceIdentifiers: identifiers,
				Bucket:            bodyStart,
				Bucket_2:          bodyEnd,
			})
		}

		if err != nil {
			return models.CombinedMetric{}, fmt.Errorf("failed to query hourly aggregates: %w", err)
		}

		if len(rows) > 0 {
			result.Metrics = s.aggregateHourlyRows(rows, query.MeasurementTypes, query.AggregationTypes)
		}
		result.TemperatureStatusCounts = s.getTemperatureCountsFromHourlyAggregates(ctx, query.OrganizationID, query.DeviceIDs, bodyStart, bodyEnd)
	}

	if hasTail {
		if err := s.appendHourlyRawTail(ctx, query, &result, tailStart, endTime); err != nil {
			return models.CombinedMetric{}, err
		}
	}

	// Uptime is computed independently of the metric body: a lagging CAGG can
	// leave the body empty while the raw tail still returns recent metrics.
	// It has its own rollup and raw tail merge, so tail-enabled requests pass
	// the true request end and the uptime series covers the same right edge
	// as the metric series.
	if models.ShouldIncludeUptimeStatusCounts(query.MeasurementTypes) {
		uptimeEnd := endTime.Add(-hourlyBucketDuration)
		if hasTail {
			uptimeEnd = endTime
		}
		result.UptimeStatusCounts = s.uptimeCountsForQuery(ctx, query, startTime, uptimeEnd, hourlyBucketDuration, dataSourceHourly)
	}

	return result, nil
}

// appendHourlyRawTail merges raw-granularity buckets over [tailStart, endTime]
// onto the hourly aggregate body. The tail spans at most hourlyRawTailCoverage
// plus one hour of alignment slack, so it is bounded by construction and skips
// the maxRawMetricBuckets check. UptimeStatusCounts stay untouched:
// uptimeCountsForQuery does its own raw tail merge. Tail failures propagate:
// the tail covers hours the materialized-only CAGG may not carry yet, so a
// body-only success would present incomplete recent data as fresh.
func (s *TimescaleTelemetryStore) appendHourlyRawTail(ctx context.Context, query models.CombinedMetricsQuery, body *models.CombinedMetric, tailStart, endTime time.Time) error {
	bucketDuration := rawMetricBucketDuration(query.SlideInterval, len(query.DeviceIDs) == 0)
	// Buckets wider than an hour get time_bucket grid labels that can precede
	// the hour-truncated tailStart, colliding with or preceding the hourly
	// body labels. Cap at the body granularity so OpenTime stays ascending
	// across the seam.
	if bucketDuration > hourlyBucketDuration {
		bucketDuration = hourlyBucketDuration
	}
	// Drop the in-progress final bucket (keep the seam start so the tail
	// stays contiguous with the body): a partial bucket undercounts the fleet
	// and rewrites itself on the next poll.
	_, alignedEnd, hasCompleteBucket := completeRawBucketWindow(tailStart, endTime, bucketDuration)
	if !hasCompleteBucket {
		return nil
	}
	buckets, err := s.bucketAggregatesPreferRollup(ctx, query, tailStart, alignedEnd, bucketDuration)
	if err != nil {
		return fmt.Errorf("failed to query raw tail for hourly combined metrics: %w", err)
	}

	tail := aggregateRawMetricBuckets(buckets, query.MeasurementTypes, query.AggregationTypes)
	// Body buckets end at tailStart while tail buckets start at it, and both
	// sides are bucket-ascending, so appending keeps the response ordered by
	// OpenTime.
	body.Metrics = append(body.Metrics, tail.Metrics...)
	body.TemperatureStatusCounts = append(body.TemperatureStatusCounts, tail.TemperatureStatusCounts...)
	return nil
}

// getCombinedMetricsFromDaily queries device_metrics_daily continuous aggregate.
func (s *TimescaleTelemetryStore) getCombinedMetricsFromDaily(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

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
	var uptimeCounts []models.UptimeStatusCount
	if models.ShouldIncludeUptimeStatusCounts(query.MeasurementTypes) {
		uptimeCounts = s.uptimeCountsForQuery(ctx, query, startTime, endTime, dailyBucketDuration, dataSourceDaily)
	}

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
func (s *TimescaleTelemetryStore) uptimeCountsForQuery(ctx context.Context, query models.CombinedMetricsQuery, startTime, endTime time.Time, bucketDuration time.Duration, ds dataSource) []models.UptimeStatusCount {
	if query.OrganizationID == 0 {
		return nil
	}
	bucketDuration = normalizedUptimeBucketDuration(bucketDuration)

	rollupCounts := s.getUptimeStatusCountsFromDeviceRollups(ctx, query.OrganizationID, query.DeviceIDs, startTime, endTime, bucketDuration, ds)
	complete, rawTailStart, canMergeTail := uptimeRollupCoverage(rollupCounts, startTime, endTime, bucketDuration)
	if complete {
		return rollupCounts
	}
	rawStart := startTime
	if canMergeTail {
		rawStart = rawTailStart
	}
	if rawUptimeFallbackTooLarge(len(query.DeviceIDs), rawStart, endTime) {
		s.logger.Warn("uptime rollup coverage gap exceeds raw fallback budget, returning partial rollup counts",
			slog.Int64("org_id", query.OrganizationID),
			slog.Int("device_count", len(query.DeviceIDs)),
			slog.Time("start_time", rawStart),
			slog.Time("end_time", endTime),
			slog.Int("rollup_bucket_count", len(rollupCounts)))
		return rollupCounts
	}
	rawCounts := s.getUptimeStatusCountsFromSnapshots(ctx, query.OrganizationID, query.DeviceIDs, rawStart, endTime, bucketDuration)
	if canMergeTail {
		if len(rawCounts) == 0 {
			return rollupCounts
		}
		return mergeUptimeStatusCounts(rollupCounts, rawCounts)
	}
	return rawCounts
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
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
) []models.TemperatureStatusCount {
	var rows []sqlc.DeviceStatusHourly
	var err error

	if len(deviceIDs) == 0 {
		if orgID != 0 {
			rows, err = s.queries.GetOrgDeviceStatusHourlyAggregates(ctx, sqlc.GetOrgDeviceStatusHourlyAggregatesParams{
				OrgID:     orgID,
				StartTime: startTime,
				EndTime:   endTime,
			})
		} else {
			rows, err = s.queries.GetAllDeviceStatusHourlyAggregates(ctx, sqlc.GetAllDeviceStatusHourlyAggregatesParams{
				Bucket:   startTime,
				Bucket_2: endTime,
			})
		}
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

type rawMetricBucket struct {
	bucket                time.Time
	avgHashRate           float64
	minHashRate           float64
	maxHashRate           float64
	latestHashRate        float64
	hashRateDeviceCount   int64
	avgTemp               float64
	minTemp               float64
	maxTemp               float64
	sumTemp               float64
	tempPoints            int64
	tempDeviceCount       int64
	tempColdCount         int32
	tempOkCount           int32
	tempHotCount          int32
	tempCriticalCount     int32
	avgFanRpm             float64
	minFanRpm             float64
	maxFanRpm             float64
	sumFanRpm             float64
	fanRpmPoints          int64
	fanRpmDeviceCount     int64
	avgPower              float64
	minPower              float64
	maxPower              float64
	latestPower           float64
	powerDeviceCount      int64
	avgEfficiency         float64
	minEfficiency         float64
	maxEfficiency         float64
	sumEfficiency         float64
	efficiencyPoints      int64
	efficiencyDeviceCount int64
}

func rawMetricBucketFromAllDevices(row sqlc.GetAllDeviceMetricsRawBucketAggregatesRow) rawMetricBucket {
	return rawMetricBucket{
		bucket:                row.Bucket,
		avgHashRate:           row.AvgHashRate,
		minHashRate:           row.MinHashRate,
		maxHashRate:           row.MaxHashRate,
		latestHashRate:        row.LatestHashRate,
		hashRateDeviceCount:   row.HashRateDeviceCount,
		avgTemp:               row.AvgTemp,
		minTemp:               row.MinTemp,
		maxTemp:               row.MaxTemp,
		sumTemp:               row.SumTemp,
		tempPoints:            row.TempPoints,
		tempDeviceCount:       row.TempDeviceCount,
		tempColdCount:         row.TempColdCount,
		tempOkCount:           row.TempOkCount,
		tempHotCount:          row.TempHotCount,
		tempCriticalCount:     row.TempCriticalCount,
		avgFanRpm:             row.AvgFanRpm,
		minFanRpm:             row.MinFanRpm,
		maxFanRpm:             row.MaxFanRpm,
		sumFanRpm:             row.SumFanRpm,
		fanRpmPoints:          row.FanRpmPoints,
		fanRpmDeviceCount:     row.FanRpmDeviceCount,
		avgPower:              row.AvgPower,
		minPower:              row.MinPower,
		maxPower:              row.MaxPower,
		latestPower:           row.LatestPower,
		powerDeviceCount:      row.PowerDeviceCount,
		avgEfficiency:         row.AvgEfficiency,
		minEfficiency:         row.MinEfficiency,
		maxEfficiency:         row.MaxEfficiency,
		sumEfficiency:         row.SumEfficiency,
		efficiencyPoints:      row.EfficiencyPoints,
		efficiencyDeviceCount: row.EfficiencyDeviceCount,
	}
}

func rawMetricBucketFromOrgRaw(row sqlc.GetOrgDeviceMetricsRawBucketAggregatesRow) rawMetricBucket {
	return rawMetricBucket{
		bucket:                row.Bucket,
		avgHashRate:           row.AvgHashRate,
		minHashRate:           row.MinHashRate,
		maxHashRate:           row.MaxHashRate,
		latestHashRate:        row.LatestHashRate,
		hashRateDeviceCount:   row.HashRateDeviceCount,
		avgTemp:               row.AvgTemp,
		minTemp:               row.MinTemp,
		maxTemp:               row.MaxTemp,
		sumTemp:               row.SumTemp,
		tempPoints:            row.TempPoints,
		tempDeviceCount:       row.TempDeviceCount,
		tempColdCount:         row.TempColdCount,
		tempOkCount:           row.TempOkCount,
		tempHotCount:          row.TempHotCount,
		tempCriticalCount:     row.TempCriticalCount,
		avgFanRpm:             row.AvgFanRpm,
		minFanRpm:             row.MinFanRpm,
		maxFanRpm:             row.MaxFanRpm,
		sumFanRpm:             row.SumFanRpm,
		fanRpmPoints:          row.FanRpmPoints,
		fanRpmDeviceCount:     row.FanRpmDeviceCount,
		avgPower:              row.AvgPower,
		minPower:              row.MinPower,
		maxPower:              row.MaxPower,
		latestPower:           row.LatestPower,
		powerDeviceCount:      row.PowerDeviceCount,
		avgEfficiency:         row.AvgEfficiency,
		minEfficiency:         row.MinEfficiency,
		maxEfficiency:         row.MaxEfficiency,
		sumEfficiency:         row.SumEfficiency,
		efficiencyPoints:      row.EfficiencyPoints,
		efficiencyDeviceCount: row.EfficiencyDeviceCount,
	}
}

func rawMetricBucketFromDevices(row sqlc.GetDeviceMetricsRawBucketAggregatesRow) rawMetricBucket {
	return rawMetricBucket{
		bucket:                row.Bucket,
		avgHashRate:           row.AvgHashRate,
		minHashRate:           row.MinHashRate,
		maxHashRate:           row.MaxHashRate,
		latestHashRate:        row.LatestHashRate,
		hashRateDeviceCount:   row.HashRateDeviceCount,
		avgTemp:               row.AvgTemp,
		minTemp:               row.MinTemp,
		maxTemp:               row.MaxTemp,
		sumTemp:               row.SumTemp,
		tempPoints:            row.TempPoints,
		tempDeviceCount:       row.TempDeviceCount,
		tempColdCount:         row.TempColdCount,
		tempOkCount:           row.TempOkCount,
		tempHotCount:          row.TempHotCount,
		tempCriticalCount:     row.TempCriticalCount,
		avgFanRpm:             row.AvgFanRpm,
		minFanRpm:             row.MinFanRpm,
		maxFanRpm:             row.MaxFanRpm,
		sumFanRpm:             row.SumFanRpm,
		fanRpmPoints:          row.FanRpmPoints,
		fanRpmDeviceCount:     row.FanRpmDeviceCount,
		avgPower:              row.AvgPower,
		minPower:              row.MinPower,
		maxPower:              row.MaxPower,
		latestPower:           row.LatestPower,
		powerDeviceCount:      row.PowerDeviceCount,
		avgEfficiency:         row.AvgEfficiency,
		minEfficiency:         row.MinEfficiency,
		maxEfficiency:         row.MaxEfficiency,
		sumEfficiency:         row.SumEfficiency,
		efficiencyPoints:      row.EfficiencyPoints,
		efficiencyDeviceCount: row.EfficiencyDeviceCount,
	}
}

func rawMetricBucketFromOrgRollup(row sqlc.GetOrgFleetMetricRollupsRow) rawMetricBucket {
	return rawMetricBucket{
		bucket:                row.Bucket,
		avgHashRate:           row.AvgHashRate,
		minHashRate:           row.MinHashRate,
		maxHashRate:           row.MaxHashRate,
		latestHashRate:        row.LatestHashRate,
		hashRateDeviceCount:   row.HashRateDeviceCount,
		avgTemp:               row.AvgTemp,
		minTemp:               row.MinTemp,
		maxTemp:               row.MaxTemp,
		sumTemp:               row.SumTemp,
		tempPoints:            row.TempPoints,
		tempDeviceCount:       row.TempDeviceCount,
		tempColdCount:         row.TempColdCount,
		tempOkCount:           row.TempOkCount,
		tempHotCount:          row.TempHotCount,
		tempCriticalCount:     row.TempCriticalCount,
		avgFanRpm:             row.AvgFanRpm,
		minFanRpm:             row.MinFanRpm,
		maxFanRpm:             row.MaxFanRpm,
		sumFanRpm:             row.SumFanRpm,
		fanRpmPoints:          row.FanRpmPoints,
		fanRpmDeviceCount:     row.FanRpmDeviceCount,
		avgPower:              row.AvgPower,
		minPower:              row.MinPower,
		maxPower:              row.MaxPower,
		latestPower:           row.LatestPower,
		powerDeviceCount:      row.PowerDeviceCount,
		avgEfficiency:         row.AvgEfficiency,
		minEfficiency:         row.MinEfficiency,
		maxEfficiency:         row.MaxEfficiency,
		sumEfficiency:         row.SumEfficiency,
		efficiencyPoints:      row.EfficiencyPoints,
		efficiencyDeviceCount: row.EfficiencyDeviceCount,
	}
}

type rawMetricValues struct {
	avg         float64
	min         float64
	max         float64
	sum         float64
	count       int64
	deviceCount int64
}

func aggregateRawMetricBuckets(rows []rawMetricBucket, measurementTypes []models.MeasurementType, aggregationTypes []models.AggregationType) models.CombinedMetric {
	if len(rows) == 0 {
		return models.CombinedMetric{}
	}
	if len(measurementTypes) == 0 {
		measurementTypes = modelsV2.DefaultMeasurementTypes
	}
	if len(aggregationTypes) == 0 {
		aggregationTypes = []models.AggregationType{models.AggregationTypeAverage}
	}

	metrics := make([]models.Metric, 0, len(rows)*len(measurementTypes))
	tempCounts := make([]models.TemperatureStatusCount, 0, len(rows))

	for _, row := range rows {
		tempCounts = append(tempCounts, models.TemperatureStatusCount{
			Timestamp:     row.bucket,
			ColdCount:     row.tempColdCount,
			OkCount:       row.tempOkCount,
			HotCount:      row.tempHotCount,
			CriticalCount: row.tempCriticalCount,
		})

		for _, measurementType := range measurementTypes {
			values, ok := rawMetricValuesForMeasurement(row, measurementType)
			if !ok {
				continue
			}

			aggregatedValues := make([]models.AggregatedValue, 0, len(aggregationTypes))
			for _, aggType := range aggregationTypes {
				value, ok := rawMetricAggregationValue(values, aggType)
				if !ok {
					continue
				}
				aggregatedValues = append(aggregatedValues, models.AggregatedValue{
					Type:  aggType,
					Value: value,
				})
			}
			if len(aggregatedValues) == 0 {
				continue
			}
			metrics = append(metrics, models.Metric{
				MeasurementType:  measurementType,
				AggregatedValues: aggregatedValues,
				OpenTime:         row.bucket,
				DeviceCount:      safeInt64ToInt32(values.deviceCount),
			})
		}
	}

	return models.CombinedMetric{
		Metrics:                 metrics,
		TemperatureStatusCounts: tempCounts,
	}
}

func rawMetricValuesForMeasurement(row rawMetricBucket, mt models.MeasurementType) (rawMetricValues, bool) {
	switch mt {
	case models.MeasurementTypeHashrate:
		return rawMetricValues{
			avg:         row.avgHashRate,
			min:         row.minHashRate,
			max:         row.maxHashRate,
			sum:         row.latestHashRate,
			count:       row.hashRateDeviceCount,
			deviceCount: row.hashRateDeviceCount,
		}, row.hashRateDeviceCount > 0
	case models.MeasurementTypeTemperature:
		return rawMetricValues{
			avg:         row.avgTemp,
			min:         row.minTemp,
			max:         row.maxTemp,
			sum:         row.sumTemp,
			count:       row.tempPoints,
			deviceCount: row.tempDeviceCount,
		}, row.tempPoints > 0
	case models.MeasurementTypePower:
		return rawMetricValues{
			avg:         row.avgPower,
			min:         row.minPower,
			max:         row.maxPower,
			sum:         row.latestPower,
			count:       row.powerDeviceCount,
			deviceCount: row.powerDeviceCount,
		}, row.powerDeviceCount > 0
	case models.MeasurementTypeEfficiency:
		return rawMetricValues{
			avg:         row.avgEfficiency,
			min:         row.minEfficiency,
			max:         row.maxEfficiency,
			sum:         row.sumEfficiency,
			count:       row.efficiencyPoints,
			deviceCount: row.efficiencyDeviceCount,
		}, row.efficiencyPoints > 0
	case models.MeasurementTypeFanSpeed:
		return rawMetricValues{
			avg:         row.avgFanRpm,
			min:         row.minFanRpm,
			max:         row.maxFanRpm,
			sum:         row.sumFanRpm,
			count:       row.fanRpmPoints,
			deviceCount: row.fanRpmDeviceCount,
		}, row.fanRpmPoints > 0
	case models.MeasurementTypeUnknown,
		models.MeasurementTypeVoltage,
		models.MeasurementTypeCurrent,
		models.MeasurementTypeUptime,
		models.MeasurementTypeErrorRate:
		return rawMetricValues{}, false
	}
	return rawMetricValues{}, false
}

func rawMetricAggregationValue(values rawMetricValues, aggType models.AggregationType) (float64, bool) {
	switch aggType {
	case models.AggregationTypeAverage:
		return values.avg, true
	case models.AggregationTypeMin:
		return values.min, true
	case models.AggregationTypeMax:
		return values.max, true
	case models.AggregationTypeSum:
		return values.sum, true
	case models.AggregationTypeCount:
		return float64(values.count), true
	case models.AggregationTypeUnknown, models.AggregationTypeTotal, models.AggregationTypeMeanChange:
		return 0, false
	}
	return 0, false
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

func (s *TimescaleTelemetryStore) UpsertFleetMetricRollups(ctx context.Context, startTime, endTime time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.WriteTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fleet metric rollup tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Warn("failed to rollback fleet metric rollup tx", "error", err)
		}
	}()

	qtx := s.queries.WithTx(tx)
	if err := qtx.DeleteFleetMetricRollupsForWindow(ctx, sqlc.DeleteFleetMetricRollupsForWindowParams{
		StartTime: startTime,
		EndTime:   endTime,
	}); err != nil {
		return fmt.Errorf("delete fleet metric rollups for window: %w", err)
	}
	if err := qtx.UpsertFleetMetricRollups(ctx, sqlc.UpsertFleetMetricRollupsParams{
		StartTime: startTime,
		EndTime:   endTime,
	}); err != nil {
		return fmt.Errorf("upsert fleet metric rollups: %w", err)
	}
	if err := qtx.AdvanceFleetMetricRollupProgress(ctx, sqlc.AdvanceFleetMetricRollupProgressParams{
		EarliestBucket: models.TruncateToFleetRollupBucket(startTime),
		LatestBucket:   models.TruncateToFleetRollupBucket(endTime.Add(-time.Nanosecond)),
	}); err != nil {
		return fmt.Errorf("advance fleet metric rollup progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fleet metric rollup tx: %w", err)
	}
	return nil
}

func (s *TimescaleTelemetryStore) GetLatestFleetMetricRollupBucket(ctx context.Context) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	bucket, err := s.queries.GetLatestFleetMetricRollupBucket(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("get latest fleet metric rollup bucket: %w", err)
	}
	return bucket, nil
}

func (s *TimescaleTelemetryStore) getUptimeStatusCountsFromDeviceRollups(
	ctx context.Context,
	orgID int64,
	deviceIDs []models.DeviceIdentifier,
	startTime, endTime time.Time,
	bucketDuration time.Duration,
	ds dataSource,
) []models.UptimeStatusCount {
	if orgID == 0 {
		return nil
	}
	bucketDuration = normalizedUptimeBucketDuration(bucketDuration)

	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	switch ds {
	case dataSourceRaw:
		bucketInterval := fmt.Sprintf("%d seconds", int64(bucketDuration.Seconds()))
		if len(deviceIDs) == 0 {
			rows, err := s.queries.GetAllMinerStateSnapshotDeviceRollups1m(ctx, sqlc.GetAllMinerStateSnapshotDeviceRollups1mParams{
				BucketInterval: bucketInterval,
				OrgID:          orgID,
				StartTime:      startTime,
				EndTime:        endTime,
			})
			if err != nil {
				s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
				return nil
			}
			result := make([]models.UptimeStatusCount, 0, len(rows))
			for _, row := range rows {
				result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
			}
			return result
		}
		rows, err := s.queries.GetMinerStateSnapshotDeviceRollups1m(ctx, sqlc.GetMinerStateSnapshotDeviceRollups1mParams{
			BucketInterval:         bucketInterval,
			DeviceIdentifierValues: deviceIDsToStrings(deviceIDs),
			OrgID:                  orgID,
			StartTime:              startTime,
			EndTime:                endTime,
		})
		if err != nil {
			s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
			return nil
		}
		result := make([]models.UptimeStatusCount, 0, len(rows))
		for _, row := range rows {
			result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
		}
		return result

	case dataSourceHourly:
		if len(deviceIDs) == 0 {
			rows, err := s.queries.GetAllMinerStateSnapshotDeviceRollupsHourly(ctx, sqlc.GetAllMinerStateSnapshotDeviceRollupsHourlyParams{
				OrgID:     orgID,
				StartTime: startTime,
				EndTime:   endTime,
			})
			if err != nil {
				s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
				return nil
			}
			result := make([]models.UptimeStatusCount, 0, len(rows))
			for _, row := range rows {
				result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
			}
			return result
		}
		rows, err := s.queries.GetMinerStateSnapshotDeviceRollupsHourly(ctx, sqlc.GetMinerStateSnapshotDeviceRollupsHourlyParams{
			DeviceIdentifierValues: deviceIDsToStrings(deviceIDs),
			OrgID:                  orgID,
			StartTime:              startTime,
			EndTime:                endTime,
		})
		if err != nil {
			s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
			return nil
		}
		result := make([]models.UptimeStatusCount, 0, len(rows))
		for _, row := range rows {
			result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
		}
		return result

	case dataSourceDaily:
		if len(deviceIDs) == 0 {
			rows, err := s.queries.GetAllMinerStateSnapshotDeviceRollupsDaily(ctx, sqlc.GetAllMinerStateSnapshotDeviceRollupsDailyParams{
				OrgID:     orgID,
				StartTime: startTime,
				EndTime:   endTime,
			})
			if err != nil {
				s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
				return nil
			}
			result := make([]models.UptimeStatusCount, 0, len(rows))
			for _, row := range rows {
				result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
			}
			return result
		}
		rows, err := s.queries.GetMinerStateSnapshotDeviceRollupsDaily(ctx, sqlc.GetMinerStateSnapshotDeviceRollupsDailyParams{
			DeviceIdentifierValues: deviceIDsToStrings(deviceIDs),
			OrgID:                  orgID,
			StartTime:              startTime,
			EndTime:                endTime,
		})
		if err != nil {
			s.logUptimeRollupQueryError(orgID, len(deviceIDs), ds, err)
			return nil
		}
		result := make([]models.UptimeStatusCount, 0, len(rows))
		for _, row := range rows {
			result = appendUptimeStatusCount(result, row.Bucket, row.HashingCount, row.BrokenCount, row.OfflineCount, row.SleepingCount)
		}
		return result
	default:
		return nil
	}
}

func (s *TimescaleTelemetryStore) logUptimeRollupQueryError(orgID int64, deviceCount int, ds dataSource, err error) {
	s.logger.Error("failed to query miner state snapshot device rollups",
		slog.Int64("org_id", orgID),
		slog.Int("device_count", deviceCount),
		slog.String("source", ds.String()),
		slog.String("error", err.Error()))
}

func appendUptimeStatusCount(result []models.UptimeStatusCount, bucket time.Time, hashing, broken, offline, sleeping int32) []models.UptimeStatusCount {
	return append(result, models.UptimeStatusCount{
		Timestamp:       bucket,
		HashingCount:    hashing,
		BrokenCount:     broken,
		NotHashingCount: offline + sleeping,
	})
}

func normalizedUptimeBucketDuration(bucketDuration time.Duration) time.Duration {
	// Snapshot cadence is ~60s, so finer buckets would yield empty slots.
	if bucketDuration < time.Minute {
		return time.Minute
	}
	return bucketDuration
}

func rawUptimeFallbackTooLarge(deviceCount int, startTime, endTime time.Time) bool {
	timeRange := endTime.Sub(startTime)
	if deviceCount == 0 {
		return timeRange > maxRawUptimeFallbackRange
	}
	estimatedRows := int64(deviceCount) * int64(timeRange/time.Minute)
	return estimatedRows > maxRawUptimeFallbackRows
}

func uptimeRollupCoverage(counts []models.UptimeStatusCount, startTime, endTime time.Time, bucketDuration time.Duration) (complete bool, rawTailStart time.Time, canMergeTail bool) {
	if len(counts) == 0 {
		return false, time.Time{}, false
	}

	ordered := append([]models.UptimeStatusCount(nil), counts...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})

	first := ordered[0].Timestamp
	last := first
	for i, count := range ordered {
		if i > 0 && count.Timestamp.Sub(last) > bucketDuration {
			return false, time.Time{}, false
		}
		last = count.Timestamp
	}

	if first.After(startTime) && first.Sub(startTime) >= bucketDuration {
		return false, time.Time{}, false
	}
	if endTime.After(last) && endTime.Sub(last) >= bucketDuration {
		// The tail starts AT the last rollup bucket, not after it: with chart
		// buckets wider than the 1m rollup cadence, refresh lag can leave that
		// bucket built from only its first minute, so the raw merge must
		// recompute it (the merge overwrites base entries by timestamp).
		return false, last, true
	}
	return true, time.Time{}, false
}

func mergeUptimeStatusCounts(base, tail []models.UptimeStatusCount) []models.UptimeStatusCount {
	if len(base) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return base
	}

	byTimestamp := make(map[time.Time]models.UptimeStatusCount, len(base)+len(tail))
	for _, count := range base {
		byTimestamp[count.Timestamp] = count
	}
	for _, count := range tail {
		byTimestamp[count.Timestamp] = count
	}

	result := make([]models.UptimeStatusCount, 0, len(byTimestamp))
	for _, count := range byTimestamp {
		result = append(result, count)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
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
	bucketDuration = normalizedUptimeBucketDuration(bucketDuration)

	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()

	params := sqlc.GetMinerStateSnapshotsParams{
		BucketInterval: fmt.Sprintf("%d seconds", int64(bucketDuration.Seconds())),
		OrgID:          orgID,
		StartTime:      startTime,
		EndTime:        endTime,
	}
	if len(deviceIDs) > 0 {
		params.DeviceIdentifiersFilter = sql.NullString{String: "1", Valid: true}
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

// safeIntToInt32 converts an int to int32, clamping to math.MaxInt32 if needed.
func safeIntToInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds checked above
}

func safeInt64ToInt32(n int64) int32 {
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
