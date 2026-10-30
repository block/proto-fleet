import { timestampMs } from "@bufbuild/protobuf/wkt";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { metricAverage } from "@/protoFleet/features/dashboard/utils/metricAverage";
import { normalizeHashrateToTHs } from "@/protoFleet/features/dashboard/utils/metricNormalization";
import type { ChartData } from "@/shared/components/LineChart/types";
import { TH_TO_PH_DIVISOR, TH_TO_PH_THRESHOLD } from "@/shared/utils/telemetryFormat";

/**
 * Transform hashrate metrics from the API to chart data format
 * @param metrics - Array of Metric objects from GetCombinedMetricsResponse
 * @returns Array of ChartData objects for LineChart
 */
export function transformHashrateMetricsToChartData(metrics: Metric[]): ChartData[] {
  if (!metrics || metrics.length === 0) {
    return [];
  }

  return metrics.map((metric) => {
    const avgValue = metricAverage(metric);
    const normalizedHashrate = avgValue !== undefined ? normalizeHashrateToTHs(avgValue, metric.deviceCount) : null;

    return {
      datetime: metric.openTime ? timestampMs(metric.openTime) : 0,
      hashrate: normalizedHashrate,
    };
  });
}

/**
 * Transform hashrate metrics to chart data with appropriate unit scaling.
 * Automatically converts TH/S to PH/S when values exceed 1000 TH/S.
 * @param metrics - Array of Metric objects from GetCombinedMetricsResponse
 * @returns Object containing scaled chart data and the appropriate unit string
 */
export function transformHashrateMetricsWithUnits(metrics: Metric[]): {
  chartData: ChartData[];
  unit: string;
} {
  const rawData = transformHashrateMetricsToChartData(metrics);

  if (rawData.length === 0) {
    return { chartData: [], unit: "TH/S" };
  }

  // Find max value to determine if we should use PH/S
  const maxValue = Math.max(...rawData.map((d) => d.hashrate ?? 0));

  if (maxValue > TH_TO_PH_THRESHOLD) {
    // Convert all values to PH/S
    return {
      chartData: rawData.map((d) => ({
        ...d,
        hashrate: d.hashrate !== null ? d.hashrate / TH_TO_PH_DIVISOR : null,
      })),
      unit: "PH/S",
    };
  }

  return {
    chartData: rawData,
    unit: "TH/S",
  };
}
