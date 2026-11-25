import { timestampMs } from "@bufbuild/protobuf/wkt";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { metricAverage } from "@/protoFleet/features/dashboard/utils/metricAverage";
import { normalizePowerToKW } from "@/protoFleet/features/dashboard/utils/metricNormalization";
import type { ChartData } from "@/shared/components/LineChart/types";

/**
 * Transform power metrics from the API to chart data format
 * @param metrics - Array of Metric objects from GetCombinedMetricsResponse
 * @returns Array of ChartData objects for LineChart
 */
export function transformPowerMetricsToChartData(metrics: Metric[], metricsInKW = false): ChartData[] {
  if (!metrics || metrics.length === 0) {
    return [];
  }

  return metrics.map((metric) => {
    const avgValue = metricAverage(metric);
    const normalizedPower =
      avgValue !== undefined ? (metricsInKW ? avgValue : normalizePowerToKW(avgValue, metric.deviceCount)) : null;

    return {
      datetime: metric.openTime ? timestampMs(metric.openTime) : 0,
      power: normalizedPower,
    };
  });
}
