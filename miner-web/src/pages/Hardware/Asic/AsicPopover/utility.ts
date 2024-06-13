import { HashrateResponseHashratedata, TemperatureResponseTemperaturedata } from "apiTypes";
import { getTimeFromEpoch } from "common/utils/stringUtils";
import { convertMhSToThS } from "common/utils/utility";

export const convertTemperatureValues = (
  data: TemperatureResponseTemperaturedata["data"]
) => {
  return data?.map((temperature) => ({
    time: getTimeFromEpoch(temperature.datetime),
    value: temperature.value || 0,
  }));
};

export const convertHashrateValues = (
  data: HashrateResponseHashratedata["data"]
) => {
  return data?.map((hashrate) => ({
    time: getTimeFromEpoch(hashrate.datetime),
    value: convertMhSToThS(hashrate.value) || 0,
  }));
};
