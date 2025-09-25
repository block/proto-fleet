import {
  HashrateResponseHashratedata,
  TemperatureResponseTemperaturedata,
} from "@/protoOS/api/types";
import {
  TEMP_UNITS,
  type TemperatureUnits,
} from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertMegahashSecToTerahashSec } from "@/shared/utils/utility";
import { convertCtoF } from "@/shared/utils/utility";

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
    value: convertMegahashSecToTerahashSec(hashrate.value) || 0,
  }));
};

export const convertAndFormatTemperature = (
  tempC: number | null | undefined,
  temperatureUnits: TemperatureUnits,
  showUnits: boolean = true,
) => {
  // Assume 0 means not available
  if (tempC === 0 || tempC === null || tempC === undefined) {
    return "N/A";
  }

  if (temperatureUnits === TEMP_UNITS.fahrenheit) {
    return `${getDisplayValue(convertCtoF(tempC))}º${showUnits ? "F" : ""}`;
  }

  return `${getDisplayValue(tempC)}º${showUnits ? "C" : ""}`;
};
