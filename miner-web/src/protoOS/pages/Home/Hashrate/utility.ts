import { Aggregates, HashrateResponseHashratedata } from "@/protoOS/api/types";

import { aggregateValues } from "@/shared/components/Chart/utility";
import { Duration } from "@/shared/components/DurationSelector";
import { convertMhSToThS } from "@/shared/utils/utility";

export const convertHashrateValues = (
  data: HashrateResponseHashratedata["data"]
) => {
  return (
    data?.map((hashrate) => ({
      datetime: hashrate.datetime || 0,
      value: convertMhSToThS(hashrate.value) || 0,
    })) || []
  );
};

export const convertAggregateValues = (
  aggregates: HashrateResponseHashratedata["aggregates"]
) => {
  return Object.keys(aggregates || {}).reduce(
    (acc = {}, key: string) => {
      const aggregateKey = key as keyof Aggregates;
      const value = convertMhSToThS(aggregates?.[aggregateKey]).toFixed(2);
      if (value) acc[aggregateKey] = +value;
      return acc;
    },
    {} as HashrateResponseHashratedata["aggregates"]
  );
};

export const shouldAggregateHashrateValues = (
  data: HashrateResponseHashratedata["data"],
  duration: Duration
) => {
  // we can continue without aggregation if we have less than 360 data points
  // or if the duration is 12h or 24h as it fits on the larger chart
  return (
    !!data?.length &&
    data.length >= 360 &&
    duration !== "12h" &&
    duration !== "24h"
  );
};

export const aggregateHashrateValues = (
  data: HashrateResponseHashratedata["data"],
  duration: Duration
) => {
  if (!shouldAggregateHashrateValues(data, duration)) {
    return data;
  }
  let compareTimeMinutes = 0;
  if (duration === "48h") {
    compareTimeMinutes = 10;
  } else if (duration === "5d") {
    compareTimeMinutes = 60;
  }
  return aggregateValues(data, compareTimeMinutes);
};
