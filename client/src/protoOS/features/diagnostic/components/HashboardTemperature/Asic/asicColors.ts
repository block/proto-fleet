import type { SelectedMetric } from "../AsicMetricContext";
import { criticalTemp, dangerTemp, warningTemp } from "@/protoOS/features/kpis/constants";
import type { AsicPalette } from "@/protoOS/features/kpis/hooks";
import { map } from "@/shared/utils/math";

export type MetricRange = readonly [number, number];

// FPO for CT1 and R2: fake-ct1 targets 335 TH/s across 312 ASICs (~1074 GH/s each).
// TODO: Replace these placeholders with API-provided expected/ideal ASIC hashrate
// thresholds when available, after confirming the values with firmware.
const hashrateThresholds = { expectedGhs: 1000, idealGhs: 1075 };

/** Heatmap colour for a single ASIC temperature, in °C. */
const getTemperatureColor = (palette: AsicPalette, temperature: number | null | undefined): string => {
  if (temperature === undefined || temperature === null) {
    return palette.unknown;
  }

  let opacity =
    temperature >= criticalTemp
      ? 1.0
      : temperature >= warningTemp
        ? map(temperature, warningTemp, criticalTemp, 0.4, 1)
        : map(temperature, 30, warningTemp, 0.4, 0.05);

  // round opacity to nearest 0.05
  opacity = Math.round(opacity * 20) / 20;

  const color =
    temperature >= dangerTemp ? palette.danger : temperature >= warningTemp ? palette.warning : palette.cool;

  return color.replace(")", `/ ${opacity})`);
};

export function getMetricRange(metric: SelectedMetric, values: (number | undefined)[]): MetricRange {
  if (metric === "temperature") return [30, criticalTemp];
  const finite = values.filter((value): value is number => value !== undefined && Number.isFinite(value));
  if (!finite.length) return [0, 0];
  return metric === "hashrate" ? [0, Math.max(0, ...finite)] : [Math.min(...finite), Math.max(...finite)];
}

export function getMetricColor(
  palette: AsicPalette,
  metric: SelectedMetric,
  value: number | undefined,
  [min, max]: MetricRange,
): string {
  if (value === undefined || !Number.isFinite(value)) return palette.unknown;
  if (metric === "temperature") return getTemperatureColor(palette, value);
  if (metric === "hashrate") {
    const { expectedGhs, idealGhs } = hashrateThresholds;
    const color = value < expectedGhs ? palette.danger : value < idealGhs ? palette.warning : palette.success;
    return `color-mix(in srgb, ${color} 45%, transparent)`;
  }
  const ratio = max > min ? Math.max(0, Math.min(1, (value - min) / (max - min))) : 0.5;
  // Keep labels legible: saturation communicates magnitude without a solid dark fill.
  return `color-mix(in srgb, ${palette.cool} ${Math.round(8 + ratio * 37)}%, transparent)`;
}
