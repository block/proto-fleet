package timescaledb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	// Temperature thresholds for status counts (in Celsius)
	// Cold: temp < 0, Ok: 0 <= temp < 70, Hot: 70 <= temp < 90, Critical: temp >= 90
	tempThresholdCold     = 0.0  // Below this = Cold
	tempThresholdHot      = 70.0 // Below this = Ok, at or above = Hot
	tempThresholdCritical = 90.0 // At or above = Critical
)

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
func (s *TimescaleTelemetryStore) GetCombinedMetrics(ctx context.Context, query models.CombinedMetricsQuery) (models.CombinedMetric, error) {
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

	return result, nil
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

		tempCount := calculateTemperatureStatusCount(bucketData, bucketTime)
		tempCounts = append(tempCounts, tempCount)

		uptimeCount := calculateUptimeStatusCount(bucketData, bucketTime)
		uptimeCounts = append(uptimeCounts, uptimeCount)

		for _, measurementType := range measurementTypes {
			var aggregatedValues []models.AggregatedValue

			if isCumulativeMetric(measurementType) {
				aggregatedValues = calculateCumulativeAggregations(bucketData, measurementType, aggregationTypes)
			} else {
				values := extractValues(bucketData, measurementType)
				if len(values) == 0 {
					continue
				}
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
				DeviceCount:      safeIntToInt32(len(bucketData)),
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

func calculateTemperatureStatusCount(data []modelsV2.DeviceMetrics, timestamp time.Time) models.TemperatureStatusCount {
	var cold, ok, hot, critical int32

	for _, m := range data {
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

func calculateUptimeStatusCount(data []modelsV2.DeviceMetrics, timestamp time.Time) models.UptimeStatusCount {
	var hashing, notHashing int32

	for _, m := range data {
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

// calculateCumulativeAggregations handles cumulative metrics (hashrate, power) by:
// 1. Grouping values by device
// 2. Calculating per-device aggregates (avg, min, max, latest)
// 3. Summing across all devices for fleet totals
func calculateCumulativeAggregations(
	data []modelsV2.DeviceMetrics,
	measurementType models.MeasurementType,
	aggregationTypes []models.AggregationType,
) []models.AggregatedValue {
	deviceValues := make(map[string][]float64)
	for _, m := range data {
		if value, _, ok := m.ExtractRawMeasurement(measurementType); ok {
			deviceValues[m.DeviceIdentifier] = append(deviceValues[m.DeviceIdentifier], value)
		}
	}

	if len(deviceValues) == 0 {
		return nil
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

	return result
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
