import { Aggregates, HashrateResponseHashratedata } from "apiTypes";

import {
  getDisplayValue,
  getTimeFromEpoch,
} from "common/utils/stringUtils";
import { convertMhSToThS } from "common/utils/utility";

export const convertHashrateValues = (
  data: HashrateResponseHashratedata["data"]
) => {
  return data?.map((hashrate) => ({
    datetime: getTimeFromEpoch(hashrate.datetime),
    value: convertMhSToThS(hashrate.value) || 0,
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
