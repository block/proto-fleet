import { AggregateStats, TimeSeriesDataPoint, Value } from "../types";
import {
  AggregationType,
  GetCombinedMetricsResponse,
  MeasurementType,
  StreamCombinedMetricUpdatesResponse,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { Duration } from "@/shared/components/DurationSelector";

// ProtoFleet server returns data in desired units so no need to convert
const conversionFn = (value: Value) => value;

/**
 * Convert duration string to milliseconds
 */
const getDurationMs = (duration: Duration): number => {
  switch (duration) {
    case "1h":
      return 3600 * 1000;
    case "12h":
      return 12 * 3600 * 1000;
    case "24h":
      return 24 * 3600 * 1000;
    case "48h":
      return 48 * 3600 * 1000;
    case "5d":
      return 5 * 24 * 3600 * 1000;
    default:
      return 3600 * 1000;
  }
};

// Helper to ensure exactly 180 data points with start padding but no false end data
const normalizeToFixedPoints = (
  data: TimeSeriesDataPoint[],
  duration: Duration,
): TimeSeriesDataPoint[] => {
  const targetPoints = 180;

  const now = Date.now();
  const durationMs = getDurationMs(duration);
  const expectedStartTime = now - durationMs;

  if (data.length === 0) {
    // No data - create 180 zero points across the full duration
    const intervalMs = durationMs / (targetPoints - 1);
    const points: TimeSeriesDataPoint[] = [];

    for (let i = 0; i < targetPoints; i++) {
      points.push({
        datetime: expectedStartTime + i * intervalMs,
        value: 0,
      });
    }
    return points;
  }

  const actualStartTime = data[0]?.datetime ?? now;
  const actualEndTime = data[data.length - 1]?.datetime ?? now;

  // Calculate how much start padding we need (if data starts late)
  const needsStartPadding = actualStartTime > expectedStartTime;
  const startGapDuration = needsStartPadding
    ? actualStartTime - expectedStartTime
    : 0;
  const dataTimespan = actualEndTime - actualStartTime;
  const totalTimespan = needsStartPadding
    ? actualEndTime - expectedStartTime
    : dataTimespan;

  // Calculate points allocation
  const startPaddingPoints = Math.floor(
    (startGapDuration / totalTimespan) * targetPoints,
  );
  const dataPoints = targetPoints - startPaddingPoints;

  const result: TimeSeriesDataPoint[] = [];

  // Add start padding points if needed
  if (startPaddingPoints > 0) {
    const startInterval = startGapDuration / startPaddingPoints;
    for (let i = 0; i < startPaddingPoints; i++) {
      result.push({
        datetime: expectedStartTime + i * startInterval,
        value: 0,
      });
    }
  }

  // Resample actual data to fit remaining points
  if (dataPoints > 0) {
    if (data.length <= dataPoints) {
      // Not enough data points - interpolate to fill
      const targetInterval = dataTimespan / (dataPoints - 1);
      for (let i = 0; i < dataPoints; i++) {
        const targetTime = actualStartTime + i * targetInterval;
        const interpolatedValue = interpolateValue(data, targetTime);
        result.push({
          datetime: targetTime,
          value: interpolatedValue,
        });
      }
    } else {
      // Too many data points - downsample
      const step = data.length / dataPoints;
      for (let i = 0; i < dataPoints; i++) {
        const index = Math.floor(i * step);
        result.push(data[Math.min(index, data.length - 1)]);
      }
    }
  }

  return result;
};

// Helper to interpolate value at a specific time
const interpolateValue = (
  data: TimeSeriesDataPoint[],
  targetTime: number,
): number => {
  if (data.length === 0) return 0;
  if (data.length === 1) return data[0].value || 0;

  // Find the two points to interpolate between
  for (let i = 0; i < data.length - 1; i++) {
    const current = data[i];
    const next = data[i + 1];

    if (
      targetTime >= (current.datetime || 0) &&
      targetTime <= (next.datetime || 0)
    ) {
      const t1 = current.datetime || 0;
      const t2 = next.datetime || 0;
      const v1 = current.value || 0;
      const v2 = next.value || 0;

      if (t2 === t1) return v1;

      // Linear interpolation
      const ratio = (targetTime - t1) / (t2 - t1);
      return v1 + ratio * (v2 - v1);
    }
  }

  // Outside range - return closest value
  const firstTime = data[0].datetime || 0;
  const lastTime = data[data.length - 1].datetime || 0;

  if (targetTime < firstTime) return data[0].value || 0;
  if (targetTime > lastTime) return data[data.length - 1].value || 0;

  return 0;
};

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
    .reduce((acc, metric) => {
      const aggregatedValue = metric.aggregatedValues.find(
        (av) => av.aggregationType === aggregationType,
      );

      // Occasionally server will send updates with no value
      if (
        aggregatedValue?.value === null ||
        aggregatedValue?.value === undefined
      ) {
        acc.push({
          datetime: metric.openTime
            ? Number(metric.openTime.seconds) * 1000
            : Date.now(),
          value: aggregatedValue?.value || null,
        });
      }

      return acc;
    }, [] as TimeSeriesDataPoint[])
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

  // Normalize to exactly 180 points with smart start padding
  const normalizedData = normalizeToFixedPoints(rawData, duration);

  // Apply conversion function to values
  const timeSeries = normalizedData.map((point) => ({
    datetime: point.datetime,
    value: conversionFn(point.value),
  }));

  // Calculate aggregates
  const rawAggregates = calculateAggregateStats(
    combinedMetricsData,
    measurementType,
  );
  const aggregates = {
    avg: conversionFn(rawAggregates.avg || null),
    max: conversionFn(rawAggregates.max || null),
    min: conversionFn(rawAggregates.min || null),
  };

  return { timeSeries, aggregates };
};

/**
 * Process telemetry data from raw time series points
 */
export const processRawTimeSeriesData = (
  rawData: TimeSeriesDataPoint[],
  duration: Duration,
): { timeSeries: TimeSeriesDataPoint[]; aggregates: AggregateStats } => {
  // Normalize to exactly 180 points with smart start padding
  const normalizedData = normalizeToFixedPoints(rawData, duration);

  // Apply conversion function to values
  const timeSeries = normalizedData.map((point) => ({
    datetime: point.datetime,
    value: conversionFn(point.value),
  }));

  // Calculate aggregates from raw data
  const values = rawData
    .map((point) => point.value)
    .filter((val) => val !== undefined && val !== null);
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
  const durationMs = getDurationMs(duration);
  const cutoffTime = Date.now() - durationMs;

  // Helper to merge and trim data
  const mergeAndTrimData = (measurementType: MeasurementType) => {
    // Get historical data
    const historicalData = combinedMetricsData
      ? transformCombinedMetricsToTimeSeries(
          combinedMetricsData,
          measurementType,
        )
      : [];

    // Get streaming data with correct aggregation type
    const targetAggregationType = getTimeSeriesAggregationType(measurementType);
    const streamingTimeSeries = transformStreamingMetricsToTimeSeries(
      streamingData,
      measurementType,
      targetAggregationType,
    );

    // Remove duplicates - prefer streaming data over historical data for same timestamp
    const uniqueData: TimeSeriesDataPoint[] = [];
    const timestampMap = new Map<number, TimeSeriesDataPoint>();

    // First pass: add all historical data
    for (const point of historicalData) {
      const timestamp = point.datetime || 0;
      timestampMap.set(timestamp, point);
    }

    // Second pass: streaming data overwrites historical data for same timestamps
    // but only if streaming data has valid (non-zero) values
    for (const point of streamingTimeSeries) {
      const timestamp = point.datetime || 0;
      const existingPoint = timestampMap.get(timestamp);

      // Only overwrite if streaming data has a valid value or no historical data exists
      if (
        !existingPoint ||
        (point.value !== undefined && point.value !== null)
      ) {
        timestampMap.set(timestamp, point);
      }
    }

    // Convert back to array and sort by timestamp
    const allUniqueData = Array.from(timestampMap.values())
      .sort((a, b) => (a.datetime || 0) - (b.datetime || 0))
      .filter((point) => (point.datetime || 0) >= cutoffTime);

    uniqueData.push(...allUniqueData);

    return processRawTimeSeriesData(uniqueData, duration);
  };

  return {
    hashrate: mergeAndTrimData(MeasurementType.HASHRATE),
    power: mergeAndTrimData(MeasurementType.POWER),
    efficiency: mergeAndTrimData(MeasurementType.EFFICIENCY),
    temperature: mergeAndTrimData(MeasurementType.TEMPERATURE),
  };
};
