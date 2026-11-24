import type { SelectedMetric } from "../AsicMetricContext";
import {
  type AsicData,
  convertAndFormatMeasurement,
  convertValueUnits,
  type Measurement,
  temperatureDisplayUnit,
  type TemperatureUnit,
} from "@/protoOS/store";

export const metricLabels: Record<SelectedMetric, string> = {
  temperature: "Temperature",
  hashrate: "Hashrate",
  frequency: "Frequency",
  voltage: "Voltage",
};
const metricUnits = { temperature: "°C", hashrate: "GH/s", frequency: "MHz", voltage: "mV" } as const;

/** Use the same normalized reading for cell colors, labels, and the live popover. */
export function getMetricValue(asic: AsicData, metric: SelectedMetric): number | undefined {
  const value = convertValueUnits(asic[metric]?.latest, metricUnits[metric])?.value;
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

export function formatMetric(
  metric: SelectedMetric,
  value: number | undefined,
  temperatureUnit: TemperatureUnit,
  withUnit = true,
): string {
  if (value === undefined || !Number.isFinite(value)) return "—";
  const measurement: Measurement = { value, units: metricUnits[metric] };
  return (
    convertAndFormatMeasurement(
      measurement,
      metric === "temperature" ? temperatureDisplayUnit(temperatureUnit) : metricUnits[metric],
      withUnit,
    ) ?? "—"
  );
}
