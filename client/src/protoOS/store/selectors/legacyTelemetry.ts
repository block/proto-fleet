import type {
  AsicTelemetry,
  FanTelemetry,
  HashboardTelemetry,
  LevelRollup,
  MetricTelemetry as NormalizedMetric,
  PsuTelemetry,
} from "../contract";
import type {
  AsicTelemetryData,
  FanTelemetryData,
  HashboardTelemetryData,
  MetricTelemetry,
  MetricUnit,
  MinerTelemetryData,
  PsuTelemetryData,
} from "../types";

const metric = (value?: NormalizedMetric): MetricTelemetry | undefined => {
  if (!value) return undefined;
  const points = value.timeSeries?.points ?? [];
  const normalizedUnits = value.latest?.units ?? value.timeSeries?.units ?? value.timeSeries?.aggregates?.avg?.units;
  if (!normalizedUnits) return undefined;
  const units = normalizedUnits as MetricUnit;
  const measurement = (item: typeof value.latest) =>
    item ? { value: item.value, units: item.units as MetricUnit } : undefined;
  const aggregates = value.timeSeries?.aggregates;
  return {
    latest: measurement(value.latest),
    timeSeries: value.timeSeries
      ? {
          aggregates: aggregates
            ? { min: measurement(aggregates.min), avg: measurement(aggregates.avg), max: measurement(aggregates.max) }
            : undefined,
          units,
          values: points.map((point) => point.value),
          startTime: points[0]?.timestampMs ?? 0,
          endTime: points[points.length - 1]?.timestampMs ?? 0,
          bucketSizeMs: value.timeSeries.bucketSizeMs,
        }
      : undefined,
  };
};

export const minerTelemetryView = (rollup: LevelRollup | undefined, hashboards: string[]): MinerTelemetryData | null =>
  rollup
    ? {
        hashboards,
        hashrate: metric(rollup.hashrate),
        temperature: metric(rollup.temperature),
        power: metric(rollup.power),
        efficiency: metric(rollup.efficiency),
      }
    : null;

export const hashboardTelemetryView = (value: HashboardTelemetry): HashboardTelemetryData => ({
  serial: String(value.hashboardId).replace(/^serial:/, ""),
  inletTemp: metric(value.inletTemperature),
  outletTemp: metric(value.outletTemperature),
  avgAsicTemp: metric(value.averageAsicTemperature),
  maxAsicTemp: metric(value.maximumAsicTemperature),
  hashrate: metric(value.hashrate),
  temperature: metric(value.temperature),
  power: metric(value.power),
  efficiency: metric(value.efficiency),
});

export const asicTelemetryView = (value: AsicTelemetry): AsicTelemetryData => ({
  id: String(value.asicId),
  hashrate: metric(value.hashrate),
  temperature: metric(value.temperature),
  voltage: metric(value.voltage),
  frequency: metric(value.frequency),
});

const componentSlot = (id: string) => Number(id.match(/:(\d+)$/)?.[1] ?? 0);

export const psuTelemetryView = (value: PsuTelemetry): PsuTelemetryData => ({
  id: componentSlot(String(value.componentId)),
  inputVoltage: metric(value.inputVoltage),
  inputCurrent: metric(value.inputCurrent),
  inputPower: metric(value.inputPower),
  outputVoltage: metric(value.outputVoltage),
  outputCurrent: metric(value.outputCurrent),
  outputPower: metric(value.outputPower),
  temperatureAmbient: metric(value.ambientTemperature),
  temperatureAverage: metric(value.averageTemperature),
  temperatureHotspot: metric(value.hotspotTemperature),
});

export const fanTelemetryView = (value: FanTelemetry): FanTelemetryData => ({
  slot: componentSlot(String(value.componentId)),
  rpm: metric(value.rpm),
  percentage: metric(value.speedPercent),
  minRpm: metric(value.minimumRpm),
  maxRpm: metric(value.maximumRpm),
});
