// TODO: [STORE_REFACTOR] we can probably remove some of these utils in favor of the store hooks (chartDataForMetric, getCurrentValue, etc.)
// Deprioritize for now, since this feature is turned off

import { type MetricTimeSeries } from "@/protoOS/store";
import { temperatureDisplayUnit, type TemperatureUnit } from "@/protoOS/store";
import { ChartData } from "@/shared/components/LineChart/types";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertMegahashSecToTerahashSec } from "@/shared/utils/telemetryFormat";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

type LegacyPoint = { datetime?: number; value?: number | null };

export const convertTemperatureValues = (data?: LegacyPoint[]) => {
  return data?.map((temperature) => ({
    datetime: temperature.datetime || 0,
    value: temperature.value || 0,
  }));
};

export const convertHashrateValues = (data?: LegacyPoint[]) => {
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

  const unit = temperatureDisplayUnit(temperatureUnit);
  if (temperatureUnit === "F") {
    return `${getDisplayValue(convertCtoF(tempC))}${showUnits ? ` ${unit}` : ""}`;
  }

  return `${getDisplayValue(tempC)}${showUnits ? ` ${unit}` : ""}`;
};

/**
 * Convert MetricTimeSeries temperature data to ChartData format
 */
export const convertTelemetryTemperatureToChartData = (
  metric: MetricTimeSeries,
  intervalMs: number | undefined,
): ChartData[] => {
  const points = metric?.points ?? [];
  if (!points.length || intervalMs === undefined) return [];

  // Anchor to the first bucket timestamp, then advance by the fixed bucket interval per index.
  const startTime = points[0]?.timestampMs ?? 0;
  return points.map((point, index) => ({
    datetime: Math.floor((startTime + index * intervalMs) / 1000), // Convert to seconds
    value: point.value,
  }));
};

/**
 * Convert MetricTimeSeries hashrate data to ChartData format
 */
export const convertTelemetryHashrateToChartData = (
  metric: MetricTimeSeries,
  intervalMs: number | undefined,
): ChartData[] => {
  const points = metric?.points ?? [];
  if (!points.length || intervalMs === undefined) return [];

  const startTime = points[0]?.timestampMs ?? 0;
  return points.map((point, index) => ({
    datetime: Math.floor((startTime + index * intervalMs) / 1000), // Convert to seconds
    value: point.value ? convertMegahashSecToTerahashSec(point.value) : point.value, // Convert from MH/s to TH/s
  }));
};
