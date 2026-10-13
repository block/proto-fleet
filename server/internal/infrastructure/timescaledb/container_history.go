package timescaledb

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// Container charts read only the board-total/facility-meter series. Legacy
// share-estimated hashrate and coolant history remain stored with semantics=1;
// hourly/daily aggregates mixing these definitions must not serve these charts.
func (s *TimescaleTelemetryStore) containerHistory(ctx context.Context, query models.CombinedMetricsQuery, start, end time.Time) (models.CombinedMetric, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()
	width := rawMetricBucketDuration(query.SlideInterval, false)
	if width > 24*time.Hour || !start.Before(end) || end.Sub(start) > 366*24*time.Hour || rawMetricBucketCount(start, end, width) > maxRawMetricBuckets {
		return models.CombinedMetric{}, fmt.Errorf("invalid container chart range")
	}
	rows, err := s.queries.GetContainerPollHistory(ctx, sqlc.GetContainerPollHistoryParams{ContainerIdentifier: string(query.DeviceIDs[0]), OrgID: query.OrganizationID, StartTime: start, EndTime: end, GranularitySeconds: int32(width.Seconds())})
	if err != nil {
		return models.CombinedMetric{}, err
	}
	result := models.CombinedMetric{}
	for _, r := range rows {
		kind := models.MeasurementType(r.MeasurementType)
		if len(query.MeasurementTypes) > 0 && !slices.Contains(query.MeasurementTypes, kind) {
			continue
		}
		metric := models.Metric{MeasurementType: kind, OpenTime: time.Unix(r.Bucket, 0), ReportingCount: &r.ReportingCount, ExpectedCount: &r.ExpectedCount, CoverageComplete: &r.CoverageComplete, MetricSemantics: new(int32(2))}
		if r.HasValues {
			metric.DeviceCount = 1
			values := []models.AggregatedValue{{Type: models.AggregationTypeAverage, Value: r.Average}, {Type: models.AggregationTypeMin, Value: r.Minimum}, {Type: models.AggregationTypeMax, Value: r.Maximum}}
			for _, v := range values {
				if (len(query.AggregationTypes) == 0 && v.Type == models.AggregationTypeAverage) || slices.Contains(query.AggregationTypes, v.Type) {
					metric.AggregatedValues = append(metric.AggregatedValues, v)
				}
			}
		}
		result.Metrics = append(result.Metrics, metric)
	}
	return result, nil
}
