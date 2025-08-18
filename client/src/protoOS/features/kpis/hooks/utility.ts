import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import { Duration } from "@/shared/components/DurationSelector";
import { getDateFromEpoch } from "@/shared/utils/datetime";
import { convertMhSToThS, convertWtoKW } from "@/shared/utils/utility";

export const conversionFns = {
  hashrate: convertMhSToThS,
  powerUsage: convertWtoKW,
  temperature: (value?: number) => (value ? value : 0),
  efficiency: (value?: number) => (value ? value : 0),
} as const;

// make generic where you can pass conversion function in
export const convertHashrateValues = (data: TimeSeriesData[]) => {
  return (
    data?.map((hashrate) => ({
      datetime: hashrate.datetime || 0,
      value: convertMhSToThS(hashrate.value) || 0,
    })) || []
  );
};

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
  // if data is empty, we have not received any data from the server
  // so no need to insert down time data
  if (data.length === 0) {
    return data;
  }

  const originalLength = data.length;
  // add dummy point at start of duration if needed
  data = addDurationStartValue(data, duration);

  // Check if a dummy point was added
  const dummyPointAdded = data.length > originalLength;

  if (!dummyPointAdded) {
    // No dummy point needed, return original data without any downtime insertion
    return data;
  }

  const filledData: TimeSeriesData[] = [];

  // Only fill gap between dummy point (index 0) and first real data point (index 1)
  const dummyPoint = data[0];
  const firstRealPoint = data[1];

  filledData.push(dummyPoint);

  if (firstRealPoint?.datetime && dummyPoint?.datetime) {
    const timeDifference = (firstRealPoint.datetime - dummyPoint.datetime) / 60;

    if (timeDifference > compareTimeMinutes) {
      const numberOfPoints = Math.floor(timeDifference / compareTimeMinutes);
      for (let j = 1; j <= numberOfPoints; j++) {
        filledData.push({
          datetime: dummyPoint.datetime + j * compareTimeMinutes * 60,
          value: 0,
        });
      }
    }
  }

  // Add all the real data points (from index 1 onwards)
  filledData.push(...data.slice(1));

  return filledData;
};

export const downsample = (
  data: TimeSeriesData[],
  duration: Duration,
  insertDownTime: Boolean = true,
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

  return aggregateValues(
    insertDownTime
      ? insertDownTimeData(data, duration, compareTimeMinutes)
      : data,
    compareTimeMinutes,
  );
};
