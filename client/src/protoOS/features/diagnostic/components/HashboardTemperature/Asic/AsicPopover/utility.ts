// TODO: [STORE_REFACTOR] we can probably remove some of these utils in favor of the store hooks (chartDataForMetric, getCurrentValue, etc.)
// Deprioritize for now, since this feature is turned off

import { HashrateResponseHashratedata, TemperatureResponseTemperaturedata } from "@/protoOS/api/generatedApi";
import { type MetricTimeSeries } from "@/protoOS/store";
import { type TemperatureUnit } from "@/protoOS/store";
import { ChartData } from "@/shared/components/LineChart/types";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertMegahashSecToTerahashSec } from "@/shared/utils/utility";
import { convertCtoF } from "@/shared/utils/utility";

export const convertTemperatureValues = (data: TemperatureResponseTemperaturedata["data"]) => {
  return data?.map((temperature) => ({
    datetime: temperature.datetime || 0,
    value: temperature.value || 0,
  }));
};

export const convertHashrateValues = (data: HashrateResponseHashratedata["data"]) => {
  return data?.map((hashrate) => ({
    datetime: hashrate.datetime || 0,
    value: convertMegahashSecToTerahashSec(hashrate.value) || 0,
  }));
};

export const convertAndFormatTemperature = (
  tempC: number | null | undefined,
  temperatureUnit: TemperatureUnit,
  showUnits: boolean = true,
) => {
  // Assume 0 means not available
  if (tempC === 0 || tempC === null || tempC === undefined) {
    return "N/A";
  }

  if (temperatureUnit === "F") {
    return `${getDisplayValue(convertCtoF(tempC))} °${showUnits ? temperatureUnit : ""}`;
  }

  return `${getDisplayValue(tempC)} °${showUnits ? temperatureUnit : ""}`;
};

/**
 * Convert MetricTimeSeries temperature data to ChartData format
 */
export const convertTelemetryTemperatureToChartData = (
  metric: MetricTimeSeries,
  intervalMs: number = 60000, // Default 1 minute intervals
): ChartData[] => {
  if (!metric?.values?.length) return [];

  return metric.values.map((value, index) => ({
    datetime: Math.floor((metric.startTime + index * intervalMs) / 1000), // Convert to seconds
    value,
  }));
};

/**
 * Convert MetricTimeSeries hashrate data to ChartData format
 */
export const convertTelemetryHashrateToChartData = (
  metric: MetricTimeSeries,
  intervalMs: number = 60000, // Default 1 minute intervals
): ChartData[] => {
  if (!metric?.values?.length) return [];

  return metric.values.map((value, index) => ({
    datetime: Math.floor((metric.startTime + index * intervalMs) / 1000), // Convert to seconds
    value: value ? convertMegahashSecToTerahashSec(value) : value, // Convert from MH/s to TH/s
  }));
};
