import type { SegmentedBarChartData } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";

/**
 * Generate uptime-specific headline based on processed data
 * @param processedData - Array of arrays of processed chart data (multi-day format)
 * @returns Formatted headline string
 */
export const generateUptimeHeadline = (processedData: SegmentedBarChartData[][]): string => {
  // Flatten all data points across all charts
  const allDataPoints = processedData.flat();

  if (allDataPoints.length === 0) {
    return "No data";
  }

  // Get the most recent data point
  const latestPoint = allDataPoints[allDataPoints.length - 1];

  const notHashingCount = latestPoint.notHashing || 0;
  const totalMiners = (latestPoint.hashing || 0) + notHashingCount;

  if (totalMiners === 0) {
    return "No miners";
  }

  if (notHashingCount === 0) {
    // All miners are hashing
    return "All miners hashing";
  }

  // Calculate percentage of miners not hashing
  const notHashingPercentage = Math.round((notHashingCount / totalMiners) * 100);

  return `${notHashingPercentage}% not hashing`;
};
