import { timestampMs } from "@bufbuild/protobuf/wkt";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { metricAverage } from "@/protoFleet/features/dashboard/utils/metricAverage";
import { normalizeEfficiencyToJTH } from "@/protoFleet/features/dashboard/utils/metricNormalization";
import type { ChartData } from "@/shared/components/LineChart/types";

/**
 * Transform efficiency metrics from the API to chart data format
 * @param metrics - Array of Metric objects from GetCombinedMetricsResponse
 * @returns Array of ChartData objects for LineChart
 */
export function transformEfficiencyMetricsToChartData(metrics: Metric[]): ChartData[] {
  if (!metrics || metrics.length === 0) {
    return [];
  }

  return metrics.map((metric) => {
    const avgValue = metricAverage(metric);
    const normalizedEfficiency = avgValue !== undefined ? normalizeEfficiencyToJTH(avgValue) : null;

    return {
      datetime: metric.openTime ? timestampMs(metric.openTime) : 0,
      efficiency: normalizedEfficiency,
    };
  });
}
