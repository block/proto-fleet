import { AggregationType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Prefer the average, fall back to the first aggregation, and preserve gaps.
export function metricAverage(metric: Metric): number | undefined {
  const value =
    metric.aggregatedValues.find((aggregation) => aggregation.aggregationType === AggregationType.AVERAGE)?.value ??
    metric.aggregatedValues[0]?.value;
  return value !== undefined && Number.isFinite(value) ? value : undefined;
}
