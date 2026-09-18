import type { SelectedMetric } from "../AsicMetricContext";
import { criticalTemp, dangerTemp, warningTemp } from "@/protoOS/features/kpis/constants";
import type { AsicPalette } from "@/protoOS/features/kpis/hooks";
import { map } from "@/shared/utils/math";

export type MetricRange = readonly [number, number];

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
  const finite = values
    .filter((value): value is number => value !== undefined && Number.isFinite(value))
    .sort((a, b) => a - b);
  if (!finite.length) return [0, 0];

  // Scale within this board, trimming 5% at each end so a dead ASIC or a spike
  // cannot wash out its neighbours. Small samples (<20) retain their min/max.
  const trim = Math.floor(finite.length * 0.05);
  const low = finite[trim];
  const high = finite[finite.length - 1 - trim];
  const midpoint = (low + high) / 2;
  // A 5% minimum spread keeps near-uniform boards near the middle blue fill.
  // This is a display scale, not an expected-performance or safety threshold.
  const spread = Math.max(high - low, Math.abs(midpoint) * 0.05);
  return [midpoint - spread / 2, midpoint + spread / 2];
}

export function getMetricColor(
  palette: AsicPalette,
  metric: SelectedMetric,
  value: number | undefined,
  [min, max]: MetricRange,
): string {
  if (value === undefined || !Number.isFinite(value)) return palette.unknown;
  if (metric === "temperature") return getTemperatureColor(palette, value);
  const ratio = value < min ? 0 : value > max ? 1 : max > min ? (value - min) / (max - min) : 0.5;
  // Keep labels legible: saturation communicates magnitude without a solid dark fill.
  return `color-mix(in srgb, ${palette.cool} ${Math.round(8 + ratio * 37)}%, transparent)`;
}
