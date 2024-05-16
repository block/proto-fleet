import { Aggregates, HashrateResponseHashratedata } from "apiTypes";

import {
  getDisplayValue,
  getTimeFromEpoch,
} from "common/utils/stringUtils";

const convertMhSToThS = (value: number = 0) => value / 1000000;

export const convertHashrateValues = (
  data: HashrateResponseHashratedata["data"]
) => {
  return data?.map((hashrate) => ({
    datetime: getTimeFromEpoch(hashrate.datetime),
    value: +(getDisplayValue(convertMhSToThS(hashrate.value)) || 0),
  }));
};

export const convertAggregateValues = (
  aggregates: HashrateResponseHashratedata["aggregates"]
) => {
  return Object.keys(aggregates || {}).reduce((acc = {}, key: string) => {
    const aggregateKey = key as keyof Aggregates;
    const value = getDisplayValue(convertMhSToThS(aggregates?.[aggregateKey]));
    if (value) acc[aggregateKey] = +value;
    return acc;
  }, {} as HashrateResponseHashratedata["aggregates"]);
};
