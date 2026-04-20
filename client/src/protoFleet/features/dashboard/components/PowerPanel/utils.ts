import { AggregationType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { normalizePowerToKW } from "@/protoFleet/features/dashboard/utils/metricNormalization";
import type { ChartData } from "@/shared/components/LineChart/types";

/**
 * Transform power metrics from the API to chart data format
 * @param metrics - Array of Metric objects from GetCombinedMetricsResponse
 * @returns Array of ChartData objects for LineChart
 */
export function transformPowerMetricsToChartData(metrics: Metric[]): ChartData[] {
  if (!metrics || metrics.length === 0) {
    return [];
  }

  return metrics.map((metric) => {
    // Find the AVERAGE aggregation value, default to the first value if not found
    const avgValue =
      metric.aggregatedValues.find((agg) => agg.aggregationType === AggregationType.AVERAGE)?.value ??
      metric.aggregatedValues[0]?.value ??
      0;
    const normalizedPower = normalizePowerToKW(avgValue, metric.deviceCount);

    return {
      datetime: Number(metric.openTime?.seconds ?? 0) * 1000, // Convert seconds to milliseconds
      power: normalizedPower,
    };
  });
}
