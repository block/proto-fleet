import type { SegmentConfig, SegmentedBarChartData } from "./types";
import type { TemperatureStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

/**
 * Convert duration string to hours
 */
export const durationToHours = (duration: string): number => {
  const value = parseInt(duration.slice(0, -1));
  const unit = duration.slice(-1);

  switch (unit) {
    case "h":
      return value;
    case "d":
      return value * 24;
    default:
      return 12; // Default to 12 hours
  }
};

/**
 * Generate timestamps for chart intervals with appropriate granularity
 */
export const getHourlyIntervals = (duration: string): number[] => {
  const hours = durationToHours(duration);
  const now = new Date();
  const intervals: number[] = [];

  // Always try to show 12 intervals
  const intervalCount = 12;

  // Calculate interval in minutes
  const totalMinutes = hours * 60;
  let minutesPerInterval = totalMinutes / intervalCount;

  // Round to clean boundaries for better readability
  if (minutesPerInterval <= 5) {
    minutesPerInterval = 5;
  } else if (minutesPerInterval <= 10) {
    minutesPerInterval = 10;
  } else if (minutesPerInterval <= 15) {
    minutesPerInterval = 15;
  } else if (minutesPerInterval <= 30) {
    minutesPerInterval = 30;
  } else if (minutesPerInterval <= 60) {
    minutesPerInterval = 60;
  } else if (minutesPerInterval <= 120) {
    minutesPerInterval = 120;
  } else if (minutesPerInterval <= 240) {
    minutesPerInterval = 240;
  } else if (minutesPerInterval <= 600) {
    minutesPerInterval = 600;
  } else {
    // For very long durations, round to nearest hour
    minutesPerInterval = Math.ceil(minutesPerInterval / 60) * 60;
  }

  // Round current time UP to the next interval boundary
  const endTime = new Date(now);
  endTime.setSeconds(0, 0);
  const currentMinutes = endTime.getMinutes();
  const roundedMinutes = Math.ceil(currentMinutes / minutesPerInterval) * minutesPerInterval;

  // If we rounded up to 60 minutes, move to the next hour
  if (roundedMinutes === 60) {
    endTime.setHours(endTime.getHours() + 1);
    endTime.setMinutes(0);
  } else {
    endTime.setMinutes(roundedMinutes);
  }

  // Calculate the start time (going back from the rounded end time)
  const startTime = endTime.getTime() - totalMinutes * 60 * 1000;

  // Generate intervals from start to end
  for (let i = 0; i < intervalCount; i++) {
    const intervalTime = startTime + i * minutesPerInterval * 60 * 1000;
    intervals.push(intervalTime);
  }

  return intervals;
};

/**
 * Find the data point immediately before or at a given timestamp
 */
export const findDataPointBefore = (
  data: TemperatureStatusCount[],
  timestamp: number,
): TemperatureStatusCount | null => {
  if (!data || data.length === 0) return null;

  // Find the last data point that is before or at the timestamp
  let bestPoint: TemperatureStatusCount | null = null;

  for (const point of data) {
    const pointTime = point.timestamp ? Number(point.timestamp.seconds) * 1000 + point.timestamp.nanos / 1000000 : 0;

    if (pointTime <= timestamp) {
      bestPoint = point;
    } else {
      break; // Data is sorted, so we can stop once we pass the timestamp
    }
  }

  return bestPoint;
};

/**
 * Process raw temperature status counts into chart data
 */
export const processChartData = (
  data: TemperatureStatusCount[],
  duration: string,
  _segmentConfig: SegmentConfig,
): SegmentedBarChartData[] => {
  const hourlyIntervals = getHourlyIntervals(duration);
  const processedData: SegmentedBarChartData[] = [];

  // If no data, return empty data points for all intervals
  if (!data || data.length === 0) {
    return hourlyIntervals.map((interval) => ({
      datetime: interval,
      cold: 0,
      ok: 0,
      hot: 0,
      critical: 0,
    }));
  }

  // Sort data by timestamp
  const sortedData = [...data].sort((a, b) => {
    const timeA = a.timestamp ? Number(a.timestamp.seconds) : 0;
    const timeB = b.timestamp ? Number(b.timestamp.seconds) : 0;
    return timeA - timeB;
  });

  // For each hourly interval, find the appropriate data point
  for (const interval of hourlyIntervals) {
    const dataPoint = findDataPointBefore(sortedData, interval);

    // Always create a chart point for every interval
    const chartPoint: SegmentedBarChartData = {
      datetime: interval,
      cold: dataPoint ? dataPoint.coldCount : 0,
      ok: dataPoint ? dataPoint.okCount : 0,
      hot: dataPoint ? dataPoint.hotCount : 0,
      critical: dataPoint ? dataPoint.criticalCount : 0,
    };
    processedData.push(chartPoint);
  }

  return processedData;
};

/**
 * Generate intervals for multi-day charts
 */
export const getMultiDayIntervals = (duration: string): number[][] => {
  const hours = durationToHours(duration);
  const now = new Date();
  const currentTime = Date.now();

  // For durations <= 24h, use single chart (handled by getHourlyIntervals)
  if (hours <= 24) {
    return [getHourlyIntervals(duration)];
  }

  // Determine bars per day based on duration
  const barsPerDay = hours <= 48 ? 12 : 6;
  const hoursPerBar = 24 / barsPerDay;
  const minutesPerBar = hoursPerBar * 60;

  // Calculate start and end times
  const endTime = new Date(now);
  endTime.setMinutes(0, 0, 0);
  const startTime = new Date(endTime.getTime() - hours * 60 * 60 * 1000);

  // Group intervals by day
  const dayIntervals: number[][] = [];
  let currentDay = new Date(startTime);
  currentDay.setHours(0, 0, 0, 0); // Start at beginning of first day

  while (currentDay <= endTime) {
    const dayStart = new Date(currentDay);
    const dayEnd = new Date(currentDay);
    dayEnd.setDate(dayEnd.getDate() + 1);

    const intervals: number[] = [];

    // Generate intervals for this day
    for (let i = 0; i < barsPerDay; i++) {
      const intervalTime = new Date(dayStart.getTime() + i * minutesPerBar * 60 * 1000);

      // Only include intervals that are:
      // 1. After or at the start time
      // 2. Before or at the end time
      // 3. Not in the future
      if (intervalTime >= startTime && intervalTime <= endTime && intervalTime.getTime() <= currentTime) {
        intervals.push(intervalTime.getTime());
      }
    }

    // Only add day if it has intervals
    if (intervals.length > 0) {
      dayIntervals.push(intervals);
    }

    // Move to next day
    currentDay.setDate(currentDay.getDate() + 1);
  }

  return dayIntervals;
};

/**
 * Process data for multi-day charts
 */
export const processMultiDayChartData = (
  data: TemperatureStatusCount[],
  duration: string,
  _segmentConfig: SegmentConfig,
): SegmentedBarChartData[][] => {
  const hours = durationToHours(duration);

  // For durations <= 24h, use single chart
  if (hours <= 24) {
    return [processChartData(data, duration, _segmentConfig)];
  }

  const dayIntervals = getMultiDayIntervals(duration);
  const processedDays: SegmentedBarChartData[][] = [];

  // Sort data by timestamp
  const sortedData = data
    ? [...data].sort((a, b) => {
        const timeA = a.timestamp ? Number(a.timestamp.seconds) : 0;
        const timeB = b.timestamp ? Number(b.timestamp.seconds) : 0;
        return timeA - timeB;
      })
    : [];

  // Process each day's intervals
  for (const intervals of dayIntervals) {
    const dayData: SegmentedBarChartData[] = [];

    for (const interval of intervals) {
      const dataPoint = findDataPointBefore(sortedData, interval);

      const chartPoint: SegmentedBarChartData = {
        datetime: interval,
        cold: dataPoint ? dataPoint.coldCount : 0,
        ok: dataPoint ? dataPoint.okCount : 0,
        hot: dataPoint ? dataPoint.hotCount : 0,
        critical: dataPoint ? dataPoint.criticalCount : 0,
      };
      dayData.push(chartPoint);
    }

    processedDays.push(dayData);
  }

  return processedDays;
};

/**
 * Calculate current breakdown from the last data entry
 */
export const getCurrentBreakdown = (data: TemperatureStatusCount[], segmentConfig: SegmentConfig) => {
  if (!data || data.length === 0) return [];

  // Get the most recent data point
  const latestCount = data[data.length - 1];
  const total = latestCount.coldCount + latestCount.okCount + latestCount.hotCount + latestCount.criticalCount;

  const breakdown = [];

  // Map the counts to segments based on config
  const countMap: Record<string, number> = {
    cold: latestCount.coldCount,
    ok: latestCount.okCount,
    hot: latestCount.hotCount,
    critical: latestCount.criticalCount,
  };

  for (const [key, config] of Object.entries(segmentConfig)) {
    const count = countMap[key] || 0;

    // Include all segments that should be displayed in breakdown, regardless of count
    if (config.displayInBreakdown !== false) {
      const percentageValue = total > 0 ? Math.round((count / total) * 100) : 0;
      const percentageLabel = config.percentageLabel || `${percentageValue}% of miners`;

      breakdown.push({
        key,
        label: config.label,
        count,
        percentage: percentageValue,
        percentageLabel,
        color: config.color.replace("var(", "").replace(")", ""), // Remove var() wrapper for inline style
        icon: config.icon,
        index: config.index ?? 999, // Default to 999 if no index specified
        buttonVariant: config.buttonVariant ?? "secondary", // Default to secondary if not specified
        showButton: config.showButton !== false, // Default to true if not specified
      });
    }
  }

  // Sort by index (lower index appears first)
  breakdown.sort((a, b) => a.index - b.index);

  return breakdown;
};

/**
 * Format miner count with proper singular/plural
 */
export const formatMinerCount = (count: number): string => {
  if (count >= 1000) {
    return `${(count / 1000).toFixed(1)}k`;
  }
  return count.toString();
};
