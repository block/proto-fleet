import type {
  DashboardAudience,
  SegmentedBarChartData,
} from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";

/**
 * Generate uptime-specific headline based on processed data
 * @param processedData - Array of arrays of processed chart data (multi-day format)
 * @returns Formatted headline string
 */
export const generateUptimeHeadline = (
  processedData: SegmentedBarChartData[][],
  {
    assetLabel = "miner",
    audience = "fleet",
    expectedAssetCount,
  }: {
    assetLabel?: string;
    audience?: DashboardAudience;
    expectedAssetCount?: number;
  } = {},
): string => {
  const allDataPoints = processedData.flat();

  if (allDataPoints.length === 0) {
    return "No data";
  }

  const latestPoint = allDataPoints[allDataPoints.length - 1];

  const hashingCount = latestPoint.hashing || 0;
  const brokenCount = latestPoint.broken || 0;
  const notHashingCount = latestPoint.notHashing || 0;
  const totalMiners = hashingCount + brokenCount + notHashingCount;

  if (audience === "account" && expectedAssetCount !== undefined && totalMiners < expectedAssetCount) {
    return `${totalMiners.toLocaleString()} of ${expectedAssetCount.toLocaleString()} ${assetLabel}${expectedAssetCount === 1 ? "" : "s"} reporting`;
  }

  if (totalMiners === 0) {
    return audience === "account" ? "No hashing-state readings" : "No miners";
  }

  // Surface the most severe non-healthy bucket first.
  if (notHashingCount > 0) {
    const percentage = Math.round((notHashingCount / totalMiners) * 100);
    return `${percentage}% not hashing`;
  }

  if (brokenCount > 0) {
    const percentage = Math.round((brokenCount / totalMiners) * 100);
    return `${percentage}% need attention`;
  }

  return `All ${assetLabel}s hashing`;
};
