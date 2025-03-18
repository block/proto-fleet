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

export const downsample = (data: TimeSeriesData[], duration: Duration) => {
  // we can continue without aggregation if we have less than 360 data points
  // or if the duration is 12h or 24h as it fits on the larger chart
  if (!data?.length || data.length <= 360 || duration === "12h") {
    return data;
  }

  let compareTimeMinutes = 10;
  if (duration === "48h") {
    compareTimeMinutes = 10;
  } else if (duration === "5d") {
    compareTimeMinutes = 60;
  }

  return aggregateValues(data, compareTimeMinutes);
};
