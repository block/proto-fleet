import { Aggregates, TimeSeriesData } from "@/protoOS/api/generatedApi";
import { Duration } from "@/shared/components/DurationSelector";
import { getDateFromEpoch } from "@/shared/utils/datetime";
import { convertMegahashSecToTerahashSec, convertWtoKW } from "@/shared/utils/utility";

export const conversionFns = {
  hashrate: convertMegahashSecToTerahashSec,
  powerUsage: convertWtoKW,
  temperature: (value?: number | null) => (value ? value : 0),
  efficiency: (value?: number | null) => (value ? value : 0),
} as const;

// make generic where you can pass conversion function in
export const convertHashrateValues = (data: TimeSeriesData[]) => {
  return (
    data?.map((hashrate) => ({
      datetime: hashrate.datetime || 0,
      value: convertMegahashSecToTerahashSec(hashrate.value) || 0,
    })) || []
  );
};

export const convertValues = (data: TimeSeriesData[], convertFn: (value?: number | null) => number) => {
  return (
    data?.map((dataItem) => ({
      datetime: dataItem.datetime,
      value: convertFn(dataItem.value) || 0,
    })) || []
  );
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
export const aggregateValues = (dataToAggregate: TimeSeriesData[] = [], compareTimeMinutes: number) => {
  // if data is empty, we have not received any data from the server
  // so no need to aggregate data
  if (dataToAggregate.length === 0) {
    return dataToAggregate;
  }

  let aggregatedData: {
    datetime: number;
    value: number;
    numberOfValues: number;
  }[] = [];
  let currentDate: Date | null = null;

  dataToAggregate.forEach((data) => {
    // Skip entries with invalid datetime
    if (!data.datetime) {
      return;
    }

    const dateToCompareEpoch = getDateFromEpoch(data.datetime).setSeconds(0);
    const dateToCompare = getDateFromEpoch(dateToCompareEpoch);

    // Initialize currentDate with first data point
    if (currentDate === null) {
      currentDate = dateToCompare;
      aggregatedData.push({
        datetime: data.datetime,
        value: +(data.value || 0),
        numberOfValues: 1,
      });
      return;
    }

    const diffMs = dateToCompare.getTime() - currentDate.getTime();
    const diffMins = diffMs / 60000;

    if (diffMins < compareTimeMinutes) {
      // Same bucket - add to existing
      aggregatedData[aggregatedData.length - 1] = {
        datetime: aggregatedData[aggregatedData.length - 1].datetime,
        value: +aggregatedData[aggregatedData.length - 1].value + +(data.value || 0),
        numberOfValues: aggregatedData[aggregatedData.length - 1].numberOfValues + 1,
      };
    } else {
      // New bucket - create new entry
      currentDate = dateToCompare;
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

export const convertAggregateValues = (
  aggregates?: Aggregates,
  convertFn: (value?: number | null) => number = convertMegahashSecToTerahashSec,
) => {
  return Object.keys(aggregates || {}).reduce((acc = {}, key: string) => {
    const aggregateKey = key as keyof Aggregates;
    const value = convertFn(aggregates?.[aggregateKey]);
    if (value !== undefined) acc[aggregateKey] = +value.toFixed(2);
    return acc;
  }, {} as Aggregates);
};

export const downsample = (data: TimeSeriesData[], duration: Duration) => {
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

  // Only use real data - no artificial downtime insertion
  return aggregateValues(data, compareTimeMinutes);
};
