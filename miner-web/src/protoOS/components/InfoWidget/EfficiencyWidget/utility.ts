import { EfficiencyResponseEfficiencydata } from "@/protoOS/api/types";

import { aggregateValues } from "@/shared/components/Chart/utility";

import { Duration } from "@/shared/components/DurationSelector";

export const convertEfficiencyValues = (
  data: EfficiencyResponseEfficiencydata["data"]
) => {
  return data?.map((data) => ({
    datetime: data.datetime || 0,
    value: data.value || 0,
  }));
};

export const aggregateEfficiencyValues = (
  data: EfficiencyResponseEfficiencydata["data"],
  duration: Duration
) => {
  // we can continue without aggregation if we have less than 360 data points
  if (!data?.length || data.length < 360) {
    return data;
  }
  let compareTimeMinutes = 0;
  if (duration === "12h") {
    compareTimeMinutes = 5;
  } else if (duration === "24h") {
    compareTimeMinutes = 10;
  } else if (duration === "48h") {
    compareTimeMinutes = 20;
  } else if (duration === "5d") {
    compareTimeMinutes = 180;
  }
  return aggregateValues(data, compareTimeMinutes);
};
