import { Aggregates, PowerResponsePowerdata } from "@/protoOS/api/types";

import { aggregateValues } from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";
import { convertWtoKW } from "@/shared/utils/utility";

export const convertPowerValues = (data: PowerResponsePowerdata["data"]) => {
  return data?.map((data) => ({
    datetime: data.datetime || 0,
    value: convertWtoKW(data.value),
  }));
};

export const convertAggregatePowerValues = (
  aggregates: PowerResponsePowerdata["aggregates"]
) => {
  return Object.keys(aggregates || {}).reduce(
    (acc = {}, key: string) => {
      const aggregateKey = key as keyof Aggregates;
      const value = convertWtoKW(aggregates?.[aggregateKey]).toFixed(2);
      if (value) acc[aggregateKey] = +value;
      return acc;
    },
    {} as PowerResponsePowerdata["aggregates"]
  );
};

export const aggregatePowerValues = (
  data: PowerResponsePowerdata["data"],
  duration: Duration
) => {
  // we can continue without aggregation if we have less than 100 data points
  if (!data?.length || data.length < 100) {
    return data;
  }
  let compareTimeMinutes = 0;
  if (data.length < 250) {
    compareTimeMinutes = 10;
  } else if (data.length < 360) {
    compareTimeMinutes = 20;
  } else if (duration === "12h") {
    compareTimeMinutes = 30;
  } else if (duration === "24h") {
    compareTimeMinutes = 60;
  } else if (duration === "48h") {
    compareTimeMinutes = 120;
  } else if (duration === "5d") {
    compareTimeMinutes = 300;
  }
  return aggregateValues(data, compareTimeMinutes);
};
