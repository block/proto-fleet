import { Aggregates, PowerResponsePowerdata } from "apiTypes";

import { getTimeFromEpoch } from "common/utils/stringUtils";
import { convertWtoKW } from "common/utils/utility";

export const convertPowerValues = (data: PowerResponsePowerdata["data"]) => {
  return data?.map((data) => ({
    time: getTimeFromEpoch(data.datetime),
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
