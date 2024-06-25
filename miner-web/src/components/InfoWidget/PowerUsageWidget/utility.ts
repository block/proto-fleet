import { Aggregates, PowerResponsePowerdata } from "apiTypes";

import { convertWtoKW } from "common/utils/utility";

import { aggregateValues } from "components/Chart/utility";
import { Duration } from "components/DurationSelector";

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
  // we can continue without aggregation if we have less than 360 data points
  if (!data?.length || data.length < 360) {
    return data;
  }
  let compareTimeMinutes = 0;
  if (duration === "12h") {
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
