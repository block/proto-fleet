import { timestampMs } from "@bufbuild/protobuf/wkt";
import type { SegmentConfig, SegmentedBarChartData, StatusCount } from "./types";

/**
 * Convert segment key to field name (e.g., "cold" -> "coldCount", "notHashing" -> "notHashingCount")
 */
const segmentKeyToFieldName = (key: string): string => {
  return `${key}Count`;
};

/**
 * Get count value from data point for a given segment key
 */
const getCountForSegment = (dataPoint: StatusCount | null, segmentKey: string): number => {
  if (!dataPoint) return 0;
  const fieldName = segmentKeyToFieldName(segmentKey);
  const value = dataPoint[fieldName];
  return typeof value === "number" ? value : 0;
};

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
export const findDataPointBefore = (data: StatusCount[], timestamp: number): StatusCount | null => {
  if (!data || data.length === 0) return null;

  // Find the last data point that is before or at the timestamp
  let bestPoint: StatusCount | null = null;

  for (const point of data) {
    const pointTime = point.timestamp ? timestampMs(point.timestamp) : 0;

    if (pointTime <= timestamp) {
      bestPoint = point;
    } else {
      break; // Data is sorted, so we can stop once we pass the timestamp
    }
  }

  return bestPoint;
};

/**
 * Process raw status counts into chart data
 */
export const processChartData = (
  data: StatusCount[],
  duration: string,
  segmentConfig: SegmentConfig,
): SegmentedBarChartData[] => {
  const hourlyIntervals = getHourlyIntervals(duration);
  const processedData: SegmentedBarChartData[] = [];
  const segmentKeys = Object.keys(segmentConfig);

  // If no data, return empty data points for all intervals
  if (!data || data.length === 0) {
    return hourlyIntervals.map((interval) => {
      const chartPoint: SegmentedBarChartData = { datetime: interval };
      segmentKeys.forEach((key) => {
        chartPoint[key] = 0;
      });
      return chartPoint;
    });
  }

  // Sort data by timestamp
  const sortedData = [...data].sort((a, b) => {
    const timeA = a.timestamp ? timestampMs(a.timestamp) : 0;
    const timeB = b.timestamp ? timestampMs(b.timestamp) : 0;
    return timeA - timeB;
  });

  // For each hourly interval, find the appropriate data point
  for (let i = 0; i < hourlyIntervals.length; i++) {
    const interval = hourlyIntervals[i];
    const isLastInterval = i === hourlyIntervals.length - 1;

    // For last interval, use absolute latest data; for others, use data at interval
    const dataPoint = isLastInterval
      ? sortedData[sortedData.length - 1] // Latest data
      : findDataPointBefore(sortedData, interval); // Data at interval boundary

    // Always create a chart point for every interval
    const chartPoint: SegmentedBarChartData = { datetime: interval };
    segmentKeys.forEach((key) => {
      chartPoint[key] = getCountForSegment(dataPoint, key);
    });
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
  data: StatusCount[],
  duration: string,
  segmentConfig: SegmentConfig,
): SegmentedBarChartData[][] => {
  const hours = durationToHours(duration);

  // For durations <= 24h, use single chart
  if (hours <= 24) {
    return [processChartData(data, duration, segmentConfig)];
  }

  const dayIntervals = getMultiDayIntervals(duration);
  const processedDays: SegmentedBarChartData[][] = [];
  const segmentKeys = Object.keys(segmentConfig);

  // If no data, return empty data points for all intervals
  if (!data || data.length === 0) {
    return dayIntervals.map((intervals) => {
      return intervals.map((interval) => {
        const chartPoint: SegmentedBarChartData = { datetime: interval };
        segmentKeys.forEach((key) => {
          chartPoint[key] = 0;
        });
        return chartPoint;
      });
    });
  }

  // Sort data by timestamp
  const sortedData = data
    ? [...data].sort((a, b) => {
        const timeA = a.timestamp ? timestampMs(a.timestamp) : 0;
        const timeB = b.timestamp ? timestampMs(b.timestamp) : 0;
        return timeA - timeB;
      })
    : [];

  // Process each day's intervals
  for (let dayIndex = 0; dayIndex < dayIntervals.length; dayIndex++) {
    const intervals = dayIntervals[dayIndex];
    const dayData: SegmentedBarChartData[] = [];

    for (let i = 0; i < intervals.length; i++) {
      const interval = intervals[i];
      const isLastIntervalOfLastDay =
        dayIndex === dayIntervals.length - 1 && // Last day
        i === intervals.length - 1; // Last interval of that day

      // For last interval of last day, use absolute latest data
      const dataPoint = isLastIntervalOfLastDay
        ? sortedData[sortedData.length - 1]
        : findDataPointBefore(sortedData, interval);

      const chartPoint: SegmentedBarChartData = { datetime: interval };
      segmentKeys.forEach((key) => {
        chartPoint[key] = getCountForSegment(dataPoint, key);
      });
      dayData.push(chartPoint);
    }

    processedDays.push(dayData);
  }

  return processedDays;
};

/**
 * Calculate current breakdown from processed chart data
 */
export const getCurrentBreakdown = (processedChartData: SegmentedBarChartData[][], segmentConfig: SegmentConfig) => {
  // Get the last chart (for multi-day view, this is the most recent day)
  if (!processedChartData || processedChartData.length === 0) return [];
  const lastChart = processedChartData[processedChartData.length - 1];

  // Get the last data point from the last chart (most recent bar)
  if (!lastChart || lastChart.length === 0) return [];
  const latestDataPoint = lastChart[lastChart.length - 1];

  const segmentKeys = Object.keys(segmentConfig);

  // Calculate total from all segment counts
  const total = segmentKeys.reduce((sum, key) => sum + ((latestDataPoint[key] as number) || 0), 0);

  const breakdown = [];

  for (const [key, config] of Object.entries(segmentConfig)) {
    const count = (latestDataPoint[key] as number) || 0;

    // Include all segments that should be displayed in breakdown, regardless of count
    if (config.displayInBreakdown !== false) {
      const percentageValue = total > 0 ? Math.round((count / total) * 100) : 0;
      const percentageLabel = config.percentageLabel || `${percentageValue}% of fleet`;

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
        onClick: config.onClick, // Pass through the onClick handler
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
