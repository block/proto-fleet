import { Aggregates, TimeSeriesData } from "@/protoOS/api/types";
import { aggregateValues } from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";
import { convertMhSToThS, convertWtoKW } from "@/shared/utils/utility";

export const convertionFns = {
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

  const filledData: TimeSeriesData[] = [];
  data = addDurationStartValue(data, duration);
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
  return filledData;
};

export const downsample = (
  data: TimeSeriesData[],
  duration: Duration,
  insertDownTime: Boolean = true,
) => {
  const numDataPoints = 180;
  let compareTimeMinutes = 10;
  if (duration === "12h") {
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
