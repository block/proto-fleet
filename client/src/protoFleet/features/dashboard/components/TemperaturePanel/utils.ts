import type {
  DashboardAudience,
  SegmentedBarChartData,
} from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";

/**
 * Generate temperature-specific headline based on processed data
 * @param processedData - Array of arrays of processed chart data (multi-day format)
 * @returns Formatted headline string
 */
export const generateTemperatureHeadline = (
  processedData: SegmentedBarChartData[][],
  { assetLabel = "miner", audience = "fleet" }: { assetLabel?: string; audience?: DashboardAudience } = {},
): string => {
  // Flatten all data points across all charts
  const allDataPoints = processedData.flat();

  if (allDataPoints.length === 0) {
    return "No data";
  }

  // Get the most recent data point
  const latestPoint = allDataPoints[allDataPoints.length - 1];

  // Calculate miners outside safe range (everything except 'ok')
  const outsideSafeRange = (latestPoint.cold || 0) + (latestPoint.hot || 0) + (latestPoint.critical || 0);
  if (audience === "account" && outsideSafeRange + (latestPoint.ok || 0) === 0) return "No temperature readings";

  if (outsideSafeRange > 0) {
    // There are miners outside safe range
    const minerText = outsideSafeRange === 1 ? assetLabel : `${assetLabel}s`;
    return `${outsideSafeRange} ${minerText} outside of safe range`;
  }

  // All miners are healthy
  return `All ${assetLabel}s within optimal range`;
};
