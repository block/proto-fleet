import {
  HashrateResponseHashratedata,
  TemperatureResponseTemperaturedata,
} from "@/protoOS/api/types";
import { convertMhSToThS } from "@/shared/utils/utility";

export const convertTemperatureValues = (
  data: TemperatureResponseTemperaturedata["data"],
) => {
  return data?.map((temperature) => ({
    datetime: temperature.datetime || 0,
    value: temperature.value || 0,
  }));
};

export const convertHashrateValues = (
  data: HashrateResponseHashratedata["data"],
) => {
  return data?.map((hashrate) => ({
    datetime: hashrate.datetime || 0,
    value: convertMhSToThS(hashrate.value) || 0,
  }));
};
