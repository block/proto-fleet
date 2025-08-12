import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";

import { Duration } from "@/shared/components/DurationSelector";
import { getDateFromEpoch } from "@/shared/utils/datetime";
import { convertMhSToThS, convertWtoKW } from "@/shared/utils/utility";

interface AxisTickOffsetProps {
  chartType?: "line" | "bar";
  firstTick: boolean;
  hasDate?: boolean;
  midTick: boolean;
  lastTick: boolean;
  payloadOffset: number;
  x: number;
}

const offsets = {
  line: {
    first: 25,
    firstDate: 16,
    mid: 16,
    midDate: 25,
    last: 0,
    lastDate: 42,
  },
  bar: {
    first: 17,
    firstDate: 26,
    mid: 15,
    midDate: 24,
    last: 15,
    lastDate: 24,
  },
};

export const getAxisTickOffset = ({
  chartType = "line",
  firstTick,
  hasDate,
  midTick,
  lastTick,
  payloadOffset,
  x,
}: AxisTickOffsetProps) => {
  let xOffset = 0;
  const isLineChart = chartType === "line";
  if (firstTick) {
    // the offset needed to add margin left to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.firstDate : 0;
      xOffset = offsets.line.first + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.firstDate : 0;
      xOffset = x - (offsets.bar.first + payloadOffset) + dateOffset;
    }
  } else if (midTick) {
    // the offset needed to center the mid ticks
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.midDate : 0;
      xOffset = offsets.line.mid + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.midDate : 0;
      xOffset = offsets.bar.mid + dateOffset;
    }
  } else if (lastTick) {
    // the offset needed to add margin right to the first tick
    if (isLineChart) {
      const dateOffset = hasDate ? offsets.line.lastDate : 0;
      xOffset = offsets.line.last + payloadOffset + dateOffset;
    } else {
      const dateOffset = hasDate ? offsets.bar.lastDate : 0;
      xOffset = offsets.bar.last + dateOffset;
    }
  }
  return xOffset;
};

/**
 * Aggregates time series data points into time buckets and calculates the average value for each bucket.
 *
 * @param {TimeSeriesData[]} dataToAggregate - Array of time series data points to aggregate. Each point should have datetime and value properties.
 * @param {number} compareTimeMinutes - Time interval in minutes that defines the size of each time bucket.
 * @returns {TimeSeriesData[]} - Array of aggregated time series data with averaged values.
 *
 * @description
 * The function works by:
 * 1. Creating time buckets based on the specified interval (compareTimeMinutes)
 * 2. Grouping data points that fall within the same time bucket
 * 3. Calculating the average value for each bucket by summing values and dividing by count
 * 4. Returns a new array with the same datetime as the first point in each bucket and the average value
 */
export const aggregateValues = (
  dataToAggregate: TimeSeriesData[] = [],
  compareTimeMinutes: number,
) => {
  // if data is empty, we have not received any data from the server
  // so no need to aggregate data
  if (dataToAggregate.length === 0) {
    return dataToAggregate;
  }

  let aggregatedData = [
    { datetime: dataToAggregate[0].datetime, value: 0, numberOfValues: 0 },
  ];
  const currentDateEpoch = getDateFromEpoch(
    dataToAggregate[0].datetime,
  ).setSeconds(0);
  let currentDate = getDateFromEpoch(currentDateEpoch);
  dataToAggregate.forEach((data) => {
    const dateToCompareEpoch = getDateFromEpoch(data.datetime).setSeconds(0);
    const dateToCompare = getDateFromEpoch(dateToCompareEpoch);
    const diffMs = dateToCompare.getTime() - currentDate.getTime();
    const diffMins = diffMs / 60000;
    if (diffMins < compareTimeMinutes) {
      aggregatedData[aggregatedData.length - 1] = {
        datetime: aggregatedData[aggregatedData.length - 1].datetime,
        value:
          +aggregatedData[aggregatedData.length - 1].value + +(data.value || 0),
        numberOfValues:
          aggregatedData[aggregatedData.length - 1].numberOfValues + 1,
      };
    } else {
      currentDate = getDateFromEpoch(dateToCompareEpoch);
      aggregatedData.push({
        datetime: data.datetime,
        value: +(data.value || 0),
        numberOfValues: 1,
      });
    }
  });
  return aggregatedData.map((data) => ({
    datetime: data.datetime,
    value: +data.value / data.numberOfValues,
  }));
};

export const conversionFns = {
  hashrate: convertMhSToThS,
  powerUsage: convertWtoKW,
  temperature: (value?: number) => (value ? value : 0),
  efficiency: (value?: number) => (value ? value : 0),
} as const;

export const convertValues = (
  data: TimeSeriesData[],
  convertFn: (value?: number) => number,
) => {
  return (
    data?.map((dataItem) => ({
      datetime: dataItem.datetime,
      value: convertFn(dataItem.value) || 0,
    })) || []
  );
};

export const convertAggregateValues = (
  aggregates?: Aggregates,
  convertFn: (value?: number) => number = convertMhSToThS,
) => {
  return Object.keys(aggregates || {}).reduce((acc = {}, key: string) => {
    const aggregateKey = key as keyof Aggregates;
    const value = convertFn(aggregates?.[aggregateKey]);
    if (value !== undefined) acc[aggregateKey] = +value.toFixed(2);
    return acc;
  }, {} as Aggregates);
};

const detectGranularityMinutes = (data: TimeSeriesData[]): number => {
  if (data.length < 2) {
    return 1.5; // Default fallback to API granularity
  }

  // Calculate intervals between consecutive data points
  const intervals: number[] = [];
  for (let i = 1; i < Math.min(data.length, 10); i++) {
    // Sample first 10 points
    const current = data[i].datetime;
    const previous = data[i - 1].datetime;
    if (current && previous) {
      intervals.push((current - previous) / 60); // Convert to minutes
    }
  }

  if (intervals.length === 0) {
    return 1.5; // Default fallback
  }

  // Use max interval to be conservative about what constitutes a gap
  const maxInterval = Math.max(...intervals);

  return Math.max(maxInterval, 0.1); // Minimum 0.1 minutes (6 seconds)
};

const durationStringToSeconds = (duration: Duration): number => {
  const unit = duration.slice(-1);
  const value = parseInt(duration.slice(0, -1), 10);

  if (isNaN(value)) return 0;

  switch (unit) {
    case "h":
      return value * 60 * 60;
    case "d":
      return value * 24 * 60 * 60;
    default:
      return 0;
  }
};

const addDurationStartValue = (ts: TimeSeriesData[], duration: Duration) => {
  if (ts.length === 0) {
    return ts;
  }

  const mostRecent = ts[ts.length - 1].datetime;
  const modified = ts;

  if (
    mostRecent &&
    mostRecent > Date.now() / 1000 - durationStringToSeconds(duration)
  ) {
    modified.unshift({
      datetime: mostRecent - durationStringToSeconds(duration),
      value: 0,
    });
  }

  return modified;
};

const insertDownTimeData = (
  data: TimeSeriesData[],
  duration: Duration,
  compareTimeMinutes: number,
) => {
  // if data is empty, create a full timeline of downtime points
  if (data.length === 0) {
    const now = Math.floor(Date.now() / 1000);
    const durationSeconds = durationStringToSeconds(duration);
    const startTime = now - durationSeconds;
    const points: TimeSeriesData[] = [];

    // Create points at regular intervals across the full duration
    const intervalSeconds = compareTimeMinutes * 60;
    for (let time = startTime; time <= now; time += intervalSeconds) {
      points.push({
        datetime: time,
        value: 0,
      });
    }

    return points;
  }

  const filledData: TimeSeriesData[] = [];
  data = addDurationStartValue(data, duration);

  // Fill gaps between existing data points
  for (let i = 0; i < data.length - 1; i++) {
    const current = data[i];
    const next = data[i + 1];
    filledData.push(current);

    if (!next.datetime || !current.datetime) {
      continue;
    }

    const timeDifference = (next.datetime - current.datetime) / 60;
    if (timeDifference > compareTimeMinutes) {
      const numberOfPoints = Math.floor(timeDifference / compareTimeMinutes);
      for (let j = 1; j <= numberOfPoints; j++) {
        filledData.push({
          datetime: current.datetime + j * compareTimeMinutes * 60,
          value: 0,
        });
      }
    }
  }
  filledData.push(data[data.length - 1]);

  // Fill gap from last data point to current time
  const now = Math.floor(Date.now() / 1000);
  const lastPoint = filledData[filledData.length - 1];
  if (lastPoint?.datetime) {
    const timeSinceLastPoint = (now - lastPoint.datetime) / 60;
    if (timeSinceLastPoint > compareTimeMinutes) {
      const numberOfPoints = Math.floor(
        timeSinceLastPoint / compareTimeMinutes,
      );
      for (let j = 1; j <= numberOfPoints; j++) {
        filledData.push({
          datetime: lastPoint.datetime + j * compareTimeMinutes * 60,
          value: 0,
        });
      }
    }
  }

  return filledData;
};

export const downsample = (
  data: TimeSeriesData[],
  duration: Duration,
  insertDownTime: boolean = true,
) => {
  const numDataPoints = 180;

  let compareTimeMinutes = 10;
  if (duration === "1h") {
    compareTimeMinutes = (1 * 60) / numDataPoints;
  } else if (duration === "12h") {
    compareTimeMinutes = (12 * 60) / numDataPoints;
  } else if (duration === "24h") {
    compareTimeMinutes = (24 * 60) / numDataPoints;
  } else if (duration === "48h") {
    compareTimeMinutes = (48 * 60) / numDataPoints;
  } else if (duration === "5d") {
    compareTimeMinutes = (5 * 24 * 60) / numDataPoints;
  }

  // Ensure compareTimeMinutes is not less than detected granularity
  const detectedGranularity = detectGranularityMinutes(data);
  compareTimeMinutes = Math.max(compareTimeMinutes, detectedGranularity);

  return aggregateValues(
    insertDownTime
      ? insertDownTimeData(data, duration, compareTimeMinutes)
      : data,
    compareTimeMinutes,
  );
};
