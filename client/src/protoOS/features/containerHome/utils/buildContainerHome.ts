import type { CategorySummary, HomeReading, HomeSeries, LocationReading } from "../types";
import {
  asEntityKey,
  type HardwareState,
  type MetricTelemetry,
  type MetricTimeSeries,
  type TelemetryState,
} from "@/protoOS/store/contract";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

export const latest = (metric?: MetricTelemetry) => {
  const value = metric?.latest?.value;
  return typeof value === "number" && Number.isFinite(value) ? value : null;
};
export const formatReading = (value: number | null, units: string) =>
  value === null || !Number.isFinite(value)
    ? "—"
    : `${value.toLocaleString(undefined, { maximumFractionDigits: 1, minimumFractionDigits: 1 })} ${units}`;
export const temperatureValue = (value: number | null, unit: "C" | "F") =>
  value === null ? null : unit === "F" ? convertCtoF(value) : value;

const summarizeModules = (values: (number | null)[]) => {
  const readings = values.filter((value): value is number => value !== null);
  // These labels describe all modules; incomplete coverage must not imply a
  // container-wide minimum, maximum, or average from only the reporting subset.
  if (!readings.length || readings.length !== values.length) return { high: null, low: null, average: null };
  return {
    high: Math.max(...readings),
    low: Math.min(...readings),
    average: readings.reduce((sum, value) => sum + value, 0) / readings.length,
  };
};

export function buildContainerLive(hardware: HardwareState, telemetry: TelemetryState, unit: "C" | "F") {
  const topology = hardware.topology;
  const rollup =
    topology?.kind === "container" ? telemetry.rollupsByEntity.get(asEntityKey(topology.containerId)) : undefined;
  const plantId = [...hardware.plant.values()].find((component) => component.kind === "plant")?.ref.id;
  const plant = plantId ? telemetry.plant.get(plantId) : undefined;
  const hashrate = latest(rollup?.hashrate);
  // Facility power includes mining and cooling overhead. Neither plant total
  // nor mining power is a valid fallback for the whole-container numerator.
  const powerSource = {
    metric: "facilityPower" as const,
    value: latest(plant?.facilityPower),
    label: "Reported facility power, including pump and dry-cooler fan overhead",
  };
  const power = powerSource.value;
  const efficiency = power !== null && hashrate !== null && hashrate > 0 ? power / hashrate : null;
  // The single Home module value covers all member hashboards: ratio of sums,
  // not a mean of individual efficiencies. Unknown firmware coverage is not
  // evidence of a hashboard-only numerator; don't use stale bootstrap boards.
  const hashboardPower = rollup?.powerCoverage?.kind === "hashboardOnly" ? latest(rollup.power) : null;
  const moduleEfficiency =
    hashboardPower !== null && hashrate !== null && hashrate > 0 ? hashboardPower / hashrate : null;
  const modules =
    topology?.kind === "container"
      ? [...topology.modules.keys()].map((id) => telemetry.rollupsByEntity.get(asEntityKey(id)))
      : [];
  // Match the Modules page: shared PSU input power and average board-sensor
  // temperature, giving each module equal weight in the live summary.
  const modulePower = summarizeModules(modules.map((module) => latest(module?.power)));
  const moduleTemperature = summarizeModules(modules.map((module) => latest(module?.temperature)));
  const pdus = [...hardware.plant.values()].filter((component) => component.kind === "distribution");
  const reading = (id: string, label: string, value: number | null, units: string): HomeReading => ({
    id,
    label,
    value: formatReading(value, units),
  });
  const powerReadings: HomeReading[] = [
    reading("container-power", "Container power", power === null ? null : power / 1000, "kW"),
    reading("container-efficiency", "Container efficiency", efficiency, "J/TH"),
    ...(["high", "low", "average"] as const).map((stat) =>
      reading(`module-${stat}`, `Module ${stat}`, modulePower[stat] === null ? null : modulePower[stat] / 1000, "kW"),
    ),
    reading("module-efficiency", "Module efficiency", moduleEfficiency, "J/TH"),
  ];
  const temp = (id: string, label: string, value: number | null) =>
    reading(id, label, temperatureValue(value, unit), `°${unit}`);
  const temperatureReadings: HomeReading[] = [
    temp("inside", "Ambient temp", latest(plant?.insideAmbientTemperature)),
    temp("outside", "Outdoor temp", latest(plant?.outsideAmbientTemperature)),
    temp("fluid", "Fluid temp", latest(plant?.inletTemperature)),
    ...(["high", "low", "average"] as const).map((stat) =>
      temp(`module-${stat}`, `Module ${stat}`, moduleTemperature[stat]),
    ),
    ...pdus.map((component) => temp(component.ref.id, component.name, null)),
  ];
  const tanks =
    topology?.kind === "container"
      ? [...topology.tanks.values()].sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity))
      : [];
  // Location charts are period-based too. Bootstrap's current tank rollups are
  // not a substitute for a server-provided tank history/period aggregate.
  const powerLocations: LocationReading[] = tanks.map((tank) => ({
    id: tank.id,
    label: tank.label?.trim() || `Tank ${tank.slot ?? ""}`,
    amount: null,
    value: "—",
  }));
  const fluidLocations: LocationReading[] = tanks.map((tank) => ({
    id: tank.id,
    label: tank.label?.trim() || `Tank ${tank.slot ?? ""}`,
    amount: null,
    value: "—",
  }));
  const boardCount =
    rollup?.boardCount !== undefined && rollup.onlineBoardCount !== undefined
      ? `${rollup.onlineBoardCount}/${rollup.boardCount} hashboards hashing`
      : undefined;
  return {
    rollup,
    plant,
    hashrate,
    power,
    powerSource,
    efficiency,
    moduleEfficiency,
    powerReadings,
    temperatureReadings,
    powerLocations,
    fluidLocations,
    boardCount,
  };
}

/** Fill omitted buckets with null so the chart cannot bridge a telemetry outage. */
export function historyPoints(
  series: MetricTimeSeries | undefined,
  window: [number, number],
  convert: (value: number) => number = (value) => value,
) {
  if (!series) return [];
  const points = [...series.points]
    .filter((point) => point.timestampMs >= window[0] && point.timestampMs < window[1])
    .sort((a, b) => a.timestampMs - b.timestampMs);
  const result: HomeSeries["points"] = [];
  const step = series.bucketSizeMs;
  for (const point of points) {
    const previous = result[result.length - 1];
    if (previous && step && point.timestampMs - previous.datetime > step * 1.5)
      result.push({ datetime: previous.datetime + step, value: null });
    result.push({
      datetime: point.timestampMs,
      value: point.value === null || !Number.isFinite(point.value) ? null : convert(point.value),
    });
  }
  // Buckets are start-stamped and the requested end is exclusive.
  // A trailing outage must not leave an older sample as the legend's latest value.
  if (step && step > 0) {
    const lastBucket = window[1] - step;
    const lastPoint = result[result.length - 1];
    if (lastBucket >= window[0] && (!lastPoint || lastPoint.datetime < lastBucket))
      result.push({ datetime: lastBucket, value: null });
  }
  return result;
}

export function toHomeSeries(
  id: string,
  label: string,
  color: string,
  series: MetricTimeSeries | undefined,
  window: [number, number],
  units: string,
  convert: (value: number) => number = (value) => value,
): HomeSeries {
  const points = historyPoints(series, window, convert);
  // The latest bucket may be missing; do not silently substitute an older value.
  const value = points[points.length - 1]?.value ?? null;
  const stat = (key: "min" | "max" | "avg") => {
    const number = series?.aggregates?.[key]?.value;
    return formatReading(typeof number === "number" && Number.isFinite(number) ? convert(number) : null, units);
  };
  return {
    id,
    label,
    color,
    points,
    latest: formatReading(value, units),
    high: stat("max"),
    low: stat("min"),
    average: stat("avg"),
  };
}

/** Ratios use matched buckets. Missing/zero hashrate never yields an efficiency. */
export function efficiencyHistory(power?: MetricTimeSeries, hashrate?: MetricTimeSeries): MetricTimeSeries | undefined {
  if (!power || !hashrate) return undefined;
  const rates = new Map(hashrate.points.map((point) => [point.timestampMs, point.value]));
  return {
    units: "J/TH",
    bucketSizeMs: power.bucketSizeMs,
    points: power.points.map((point) => {
      const rate = rates.get(point.timestampMs);
      return {
        timestampMs: point.timestampMs,
        value: point.value !== null && typeof rate === "number" && rate > 0 ? point.value / rate : null,
      };
    }),
  };
}

/** Summed bucket averages require every component at that timestamp. These
 * extrema describe bucket averages, not a sum of unrelated component peaks. */
export function sumHistory(series: (MetricTimeSeries | undefined)[]): MetricTimeSeries | undefined {
  if (!series.length || series.some((item) => !item)) return undefined;
  const sources = series as MetricTimeSeries[];
  const maps = sources.map((item) => new Map(item.points.map((point) => [point.timestampMs, point.value])));
  const times = [...new Set(sources.flatMap((item) => item.points.map((point) => point.timestampMs)))].sort(
    (a, b) => a - b,
  );
  const points = times.map((timestampMs) => {
    const values = maps.map((map) => map.get(timestampMs));
    return {
      timestampMs,
      value: values.every((value) => typeof value === "number" && Number.isFinite(value))
        ? (values as number[]).reduce((a, b) => a + b, 0)
        : null,
    };
  });
  const values = points.flatMap((point) => (point.value === null ? [] : [point.value]));
  return {
    units: "W",
    bucketSizeMs: sources[0].bucketSizeMs,
    points,
    aggregates: {
      min: { value: values.length ? Math.min(...values) : null, units: "W" },
      max: { value: values.length ? Math.max(...values) : null, units: "W" },
      avg: { value: values.length ? values.reduce((a, b) => a + b, 0) / values.length : null, units: "W" },
    },
  };
}

export const categorySummaries = (fans?: HomeSeries): CategorySummary[] =>
  ["Pumps", "Fans", "AC", "Aux"].map((label) => ({
    id: label.toLowerCase(),
    label,
    high: label === "Fans" ? (fans?.high ?? "—") : "—",
    low: label === "Fans" ? (fans?.low ?? "—") : "—",
    average: label === "Fans" ? (fans?.average ?? "—") : "—",
  }));
