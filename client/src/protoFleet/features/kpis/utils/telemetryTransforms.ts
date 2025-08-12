import {
  AggregationType,
  GetCombinedMetricsResponse,
  MeasurementType,
  StreamCombinedMetricUpdatesResponse,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  conversionFns,
  convertValues,
  downsample,
} from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";
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

/**
 * Process telemetry data for a specific measurement type including downsampling and unit conversion
 */
export const processMetricData = (
  combinedMetricsData: GetCombinedMetricsResponse,
  measurementType: MeasurementType,
  duration: Duration,
): { timeSeries: TimeSeriesDataPoint[]; aggregates: AggregateStats } => {
  // Transform raw data
  const rawData = transformCombinedMetricsToTimeSeries(
    combinedMetricsData,
    measurementType,
  );

  // Convert to downsampling format (datetime in seconds)
  const dataForDownsample = rawData.map((point) => ({
    datetime: (point.datetime || 0) / 1000, // Convert ms to seconds
    value: point.value,
  }));

  // Downsample with downtime insertion
  const downsampledData = downsample(dataForDownsample, duration, true);

  // Get conversion function for this measurement type
  let conversionFn: (value?: number) => number;
  switch (measurementType) {
    case MeasurementType.HASHRATE:
      conversionFn = conversionFns.hashrate;
      break;
    case MeasurementType.POWER:
      conversionFn = conversionFns.powerUsage;
      break;
    case MeasurementType.EFFICIENCY:
      conversionFn = conversionFns.efficiency;
      break;
    case MeasurementType.TEMPERATURE:
      conversionFn = conversionFns.temperature;
      break;
    default:
      conversionFn = (value) => value || 0;
  }

  // Convert values and format for display
  const timeSeries = convertValues(downsampledData, conversionFn).map(
    (point) => ({
      datetime: (point.datetime || 0) * 1000, // Convert back to ms
      value: point.value,
    }),
  );

  // Calculate aggregates
  const rawAggregates = calculateAggregateStats(
    combinedMetricsData,
    measurementType,
  );
  const aggregates = {
    avg: conversionFn(rawAggregates.avg),
    max: conversionFn(rawAggregates.max),
    min: conversionFn(rawAggregates.min),
  };

  return { timeSeries, aggregates };
};

/**
 * Process telemetry data from raw time series points for a specific measurement type
 */
export const processRawTimeSeriesData = (
  rawData: TimeSeriesDataPoint[],
  measurementType: MeasurementType,
  duration: Duration,
): { timeSeries: TimeSeriesDataPoint[]; aggregates: AggregateStats } => {
  // Convert to downsampling format (datetime in seconds)
  const dataForDownsample = rawData.map((point) => ({
    datetime: (point.datetime || 0) / 1000, // Convert ms to seconds
    value: point.value,
  }));

  // Downsample with downtime insertion
  const downsampledData = downsample(dataForDownsample, duration, true);

  // Get conversion function for this measurement type
  let conversionFn: (value?: number) => number;
  switch (measurementType) {
    case MeasurementType.HASHRATE:
      conversionFn = conversionFns.hashrate;
      break;
    case MeasurementType.POWER:
      conversionFn = conversionFns.powerUsage;
      break;
    case MeasurementType.EFFICIENCY:
      conversionFn = conversionFns.efficiency;
      break;
    case MeasurementType.TEMPERATURE:
      conversionFn = conversionFns.temperature;
      break;
    default:
      conversionFn = (value) => value || 0;
  }

  // Convert values and format for display
  const timeSeries = convertValues(downsampledData, conversionFn).map(
    (point) => ({
      datetime: (point.datetime || 0) * 1000, // Convert back to ms
      value: point.value,
    }),
  );

  // Calculate aggregates from raw data
  const values = rawData
    .map((point) => point.value || 0)
    .filter((val) => val > 0);
  const aggregates =
    values.length > 0
      ? {
          avg: conversionFn(
            values.reduce((sum, val) => sum + val, 0) / values.length,
          ),
          max: conversionFn(Math.max(...values)),
          min: conversionFn(Math.min(...values)),
        }
      : { avg: 0, max: 0, min: 0 };

  return { timeSeries, aggregates };
};

/**
 * Process all telemetry metrics for KPI display
 */
export const processAllMetrics = (
  combinedMetricsData: GetCombinedMetricsResponse | null,
  duration: Duration,
) => {
  if (!combinedMetricsData) {
    const emptyStats = { avg: 0, max: 0, min: 0 };
    return {
      hashrate: { timeSeries: [], aggregates: emptyStats },
      power: { timeSeries: [], aggregates: emptyStats },
      efficiency: { timeSeries: [], aggregates: emptyStats },
      temperature: { timeSeries: [], aggregates: emptyStats },
    };
  }

  return {
    hashrate: processMetricData(
      combinedMetricsData,
      MeasurementType.HASHRATE,
      duration,
    ),
    power: processMetricData(
      combinedMetricsData,
      MeasurementType.POWER,
      duration,
    ),
    efficiency: processMetricData(
      combinedMetricsData,
      MeasurementType.EFFICIENCY,
      duration,
    ),
    temperature: processMetricData(
      combinedMetricsData,
      MeasurementType.TEMPERATURE,
      duration,
    ),
  };
};

/**
 * Process all metrics with additional streaming data merged in
 */
export const processAllMetricsWithStreaming = (
  combinedMetricsData: GetCombinedMetricsResponse | null,
  streamingData: StreamCombinedMetricUpdatesResponse,
  duration: Duration,
) => {
  const durationSeconds =
    duration === "1h"
      ? 3600
      : duration === "12h"
        ? 12 * 3600
        : duration === "24h"
          ? 24 * 3600
          : duration === "48h"
            ? 48 * 3600
            : duration === "5d"
              ? 5 * 24 * 3600
              : 3600;

  const cutoffTime = Date.now() - durationSeconds * 1000;

  // Helper to merge and trim data
  const mergeAndTrimData = (measurementType: MeasurementType) => {
    // Get historical data
    const historicalData = combinedMetricsData
      ? transformCombinedMetricsToTimeSeries(
          combinedMetricsData,
          measurementType,
        )
      : [];

    // Get streaming data
    const streamingTimeSeries = transformStreamingMetricsToTimeSeries(
      streamingData,
      measurementType,
    );

    // Merge and sort by datetime
    const allData = [...historicalData, ...streamingTimeSeries]
      .filter((point) => (point.datetime || 0) >= cutoffTime) // Trim old data
      .sort((a, b) => (a.datetime || 0) - (b.datetime || 0));

    // Remove duplicates (keep latest for same timestamp)
    const uniqueData: TimeSeriesDataPoint[] = [];
    const seen = new Set<number>();
    for (const point of allData.reverse()) {
      const timestamp = point.datetime || 0;
      if (!seen.has(timestamp)) {
        seen.add(timestamp);
        uniqueData.unshift(point);
      }
    }

    return processRawTimeSeriesData(uniqueData, measurementType, duration);
  };

  return {
    hashrate: mergeAndTrimData(MeasurementType.HASHRATE),
    power: mergeAndTrimData(MeasurementType.POWER),
    efficiency: mergeAndTrimData(MeasurementType.EFFICIENCY),
    temperature: mergeAndTrimData(MeasurementType.TEMPERATURE),
  };
};
