import type { SegmentedBarChartData } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";

/**
 * Generate temperature-specific headline based on processed data
 * @param processedData - Array of arrays of processed chart data (multi-day format)
 * @returns Formatted headline string
 */
export const generateTemperatureHeadline = (
  processedData: SegmentedBarChartData[][],
): string => {
  // Flatten all data points across all charts
  const allDataPoints = processedData.flat();

  if (allDataPoints.length === 0) {
    return "No data";
  }

  // Get the most recent data point
  const latestPoint = allDataPoints[allDataPoints.length - 1];

  // Calculate miners outside safe range (everything except 'ok')
  const outsideSafeRange =
    (latestPoint.cold || 0) +
    (latestPoint.hot || 0) +
    (latestPoint.critical || 0);

  if (outsideSafeRange > 0) {
    // Case 1: There are miners outside safe range
    const minerText = outsideSafeRange === 1 ? "miner" : "miners";
    return `${outsideSafeRange} ${minerText} outside of safe range`;
  }

  // Case 2: All miners are healthy, find how long they've been stable
  // Work backwards through the data to find when miners were last outside safe range
  let stableDataPoints = 0;

  for (let i = allDataPoints.length - 1; i >= 0; i--) {
    const point = allDataPoints[i];
    const unhealthy =
      (point.cold || 0) + (point.hot || 0) + (point.critical || 0);

    if (unhealthy > 0) {
      break; // Found a point with unhealthy miners
    }
    stableDataPoints++;
  }

  // If all data points are stable, we've been stable for the entire duration we have data for
  if (stableDataPoints === allDataPoints.length) {
    // Find first and last data points with actual values (not all zeros)
    let firstDataIndex = -1;
    let lastDataIndex = -1;

    for (let i = 0; i < allDataPoints.length; i++) {
      const point = allDataPoints[i];
      const total =
        (point.cold || 0) +
        (point.ok || 0) +
        (point.hot || 0) +
        (point.critical || 0);
      if (total > 0) {
        if (firstDataIndex === -1) firstDataIndex = i;
        lastDataIndex = i;
      }
    }

    if (firstDataIndex === -1 || lastDataIndex === -1) {
      // If we have no actual miner data, just return "No data"
      return "No data";
    }

    // Calculate the duration based on first and last actual data timestamps
    const firstTime = allDataPoints[firstDataIndex].datetime;
    const lastTime = allDataPoints[lastDataIndex].datetime;

    // If first and last are the same (only one data point), use interval to next point to estimate
    if (firstDataIndex === lastDataIndex && allDataPoints.length > 1) {
      // Find the interval between points
      let interval = 0;
      if (firstDataIndex > 0) {
        interval =
          allDataPoints[firstDataIndex].datetime -
          allDataPoints[firstDataIndex - 1].datetime;
      } else if (firstDataIndex < allDataPoints.length - 1) {
        interval =
          allDataPoints[firstDataIndex + 1].datetime -
          allDataPoints[firstDataIndex].datetime;
      }

      if (interval > 0) {
        const minutes = Math.floor(interval / (1000 * 60));
        if (minutes >= 60) {
          const hours = Math.floor(minutes / 60);
          return `Stable for ${hours} hour${hours !== 1 ? "s" : ""}`;
        } else if (minutes >= 1) {
          return `Stable for ${minutes} minute${minutes !== 1 ? "s" : ""}`;
        }
      }
      return "All miners healthy";
    }

    const totalDurationMs = lastTime - firstTime;
    const totalHours = Math.floor(totalDurationMs / (1000 * 60 * 60));

    if (totalHours >= 1) {
      return `Stable for ${totalHours} hour${totalHours !== 1 ? "s" : ""}`;
    } else {
      const totalMinutes = Math.floor(totalDurationMs / (1000 * 60));
      if (totalMinutes >= 1) {
        return `Stable for ${totalMinutes} minute${totalMinutes !== 1 ? "s" : ""}`;
      }
    }
    return "All miners healthy";
  }

  // Calculate time stable based on data point intervals
  if (stableDataPoints < 2) {
    return "All miners healthy";
  }

  // Get the timestamp of the last stable point and the first unstable point
  const lastStableIndex = allDataPoints.length - 1;
  const firstUnstableIndex = allDataPoints.length - stableDataPoints - 1;

  if (
    firstUnstableIndex >= 0 &&
    firstUnstableIndex < allDataPoints.length - 1
  ) {
    // Calculate duration from the first stable point after the last unstable point
    const firstStableTime = allDataPoints[firstUnstableIndex + 1].datetime;
    const lastStableTime = allDataPoints[lastStableIndex].datetime;
    const stableDurationMs = lastStableTime - firstStableTime;

    const stableHours = stableDurationMs / (1000 * 60 * 60);
    const stableMinutes = stableDurationMs / (1000 * 60);

    if (stableHours >= 1) {
      const hours = Math.floor(stableHours);
      return `Stable for ${hours} hour${hours !== 1 ? "s" : ""}`;
    } else if (stableMinutes >= 1) {
      const minutes = Math.floor(stableMinutes);
      return `Stable for ${minutes} minute${minutes !== 1 ? "s" : ""}`;
    }
  }

  return "All miners healthy";
};
