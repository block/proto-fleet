import {
  AggregationType,
  GetCombinedMetricsResponse,
  MeasurementType,
  StreamCombinedMetricUpdatesResponse,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  AggregateStats,
  TimeSeriesDataPoint,
} from "@/shared/features/kpis/types";

/**
 * Helper function to determine if a measurement type is cumulative
 */
const isCumulativeMetric = (measurementType: MeasurementType): boolean => {
  return [MeasurementType.HASHRATE, MeasurementType.POWER].includes(
    measurementType,
  );
};

/**
 * Helper function to get the appropriate aggregation type for time series display
 */
const getTimeSeriesAggregationType = (
  measurementType: MeasurementType,
): AggregationType => {
  return isCumulativeMetric(measurementType)
    ? AggregationType.SUM
    : AggregationType.AVERAGE;
};

/**
 * Transform combined metrics response to time series data points
 */
export const transformCombinedMetricsToTimeSeries = (
  response: GetCombinedMetricsResponse,
  measurementType: MeasurementType,
  aggregationType?: AggregationType,
): TimeSeriesDataPoint[] => {
  const metrics = response.metrics.filter(
    (metric) => metric.measurementType === measurementType,
  );

  const targetAggregationType =
    aggregationType || getTimeSeriesAggregationType(measurementType);

  const transformedData = metrics
    .map((metric) => {
      const aggregatedValue = metric.aggregatedValues.find(
        (av) => av.aggregationType === targetAggregationType,
      );

      let value = aggregatedValue?.value || 0;

      return {
        datetime: metric.openTime
          ? Number(metric.openTime.seconds) * 1000
          : Date.now(),
        value: value,
      };
    })
    .sort((a, b) => (a.datetime || 0) - (b.datetime || 0));

  return transformedData;
};

/**
 * Transform streaming metrics response to time series data points
 */
export const transformStreamingMetricsToTimeSeries = (
  response: StreamCombinedMetricUpdatesResponse,
  measurementType: MeasurementType,
  aggregationType: AggregationType = AggregationType.AVERAGE,
): TimeSeriesDataPoint[] => {
  const metrics = response.metrics.filter(
    (metric) => metric.measurementType === measurementType,
  );

  return metrics
    .map((metric) => {
      const aggregatedValue = metric.aggregatedValues.find(
        (av) => av.aggregationType === aggregationType,
      );

      return {
        datetime: metric.openTime
          ? Number(metric.openTime.seconds) * 1000
          : Date.now(),
        value: aggregatedValue?.value || 0,
      };
    })
    .sort((a, b) => (a.datetime || 0) - (b.datetime || 0));
};

/**
 * Calculate aggregate statistics from combined metrics response
 */
export const calculateAggregateStats = (
  response: GetCombinedMetricsResponse,
  measurementType: MeasurementType,
): AggregateStats => {
  const metrics = response.metrics.filter(
    (metric) => metric.measurementType === measurementType,
  );

  if (metrics.length === 0) {
    return { avg: 0, max: 0, min: 0 };
  }

  if (isCumulativeMetric(measurementType)) {
    const sumValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.SUM,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    const minValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.MIN,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    const maxValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.MAX,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    return {
      avg: sumValues.reduce((sum, val) => sum + val, 0) / sumValues.length,
      max:
        maxValues.length > 0 ? Math.max(...maxValues) : Math.max(...sumValues),
      min:
        minValues.length > 0 ? Math.min(...minValues) : Math.min(...sumValues),
    };
  } else {
    const avgValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.AVERAGE,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    const minValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.MIN,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    const maxValues = metrics
      .map(
        (metric) =>
          metric.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.MAX,
          )?.value || 0,
      )
      .filter((value) => value !== undefined && value !== null);

    return {
      avg: avgValues.reduce((sum, val) => sum + val, 0) / avgValues.length,
      max:
        maxValues.length > 0 ? Math.max(...maxValues) : Math.max(...avgValues),
      min:
        minValues.length > 0 ? Math.min(...minValues) : Math.min(...avgValues),
    };
  }
};

/**
 * Get the latest aggregate value for a specific measurement type from streaming data
 */
export const getLatestAggregateValue = (
  response: StreamCombinedMetricUpdatesResponse,
  measurementType: MeasurementType,
  aggregationType?: AggregationType,
): number => {
  const metrics = response.metrics.filter(
    (metric) => metric.measurementType === measurementType,
  );

  if (metrics.length === 0) return 0;

  const targetAggregationType =
    aggregationType || getTimeSeriesAggregationType(measurementType);

  const latestMetric = metrics[metrics.length - 1];
  const aggregatedValue = latestMetric.aggregatedValues.find(
    (av) => av.aggregationType === targetAggregationType,
  );

  return aggregatedValue?.value || 0;
};

/**
 * Merge new streaming data point into existing time series
 */
export const mergeStreamingDataPoint = (
  existingData: TimeSeriesDataPoint[],
  newDataPoint: TimeSeriesDataPoint,
  maxDataPoints: number = 100,
): TimeSeriesDataPoint[] => {
  const updatedData = [...existingData];

  updatedData.push(newDataPoint);

  updatedData.sort((a, b) => (a.datetime || 0) - (b.datetime || 0));

  if (updatedData.length > maxDataPoints) {
    return updatedData.slice(-maxDataPoints);
  }

  return updatedData;
};
