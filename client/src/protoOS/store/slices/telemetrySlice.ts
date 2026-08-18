import { enableMapSet } from "immer";
import type { StateCreator } from "zustand";

import {
  asComponentId,
  asHashboardId,
  type CoolingMode,
  type Measurement,
  type MetricTelemetry,
  ROOT_MODULE_ID,
  ROOT_MODULE_KEY,
  type TelemetryState,
} from "../contract";
import type { FanTelemetryData, PsuTelemetryData } from "../types";
import type { MinerStore } from "../useMinerStore";

enableMapSet();

type MetricContainer = Record<string, unknown>;
const metricFields = [
  "hashrate",
  "power",
  "efficiency",
  "temperature",
  "inletTemperature",
  "outletTemperature",
  "averageAsicTemperature",
  "maximumAsicTemperature",
  "voltage",
  "frequency",
  "inputVoltage",
  "inputCurrent",
  "inputPower",
  "outputVoltage",
  "outputCurrent",
  "outputPower",
  "ambientTemperature",
  "averageTemperature",
  "hotspotTemperature",
  "rpm",
  "speedPercent",
  "minimumRpm",
  "maximumRpm",
] as const;

const mergeMetrics = <T extends object>(current: T | undefined, incoming: T): T => {
  const merged = { ...(current ?? {}), ...incoming } as T & MetricContainer;
  for (const field of metricFields) {
    const before = (current as MetricContainer | undefined)?.[field] as MetricTelemetry | undefined;
    const after = (incoming as MetricContainer)[field] as MetricTelemetry | undefined;
    if (before || after) (merged as MetricContainer)[field] = { ...(before ?? {}), ...(after ?? {}) };
  }
  return merged;
};

const mergeMap = <K, V extends object>(target: Map<K, V>, incoming: Map<K, V>) => {
  incoming.forEach((value, key) => target.set(key, mergeMetrics(target.get(key), value)));
};

const mapMeasurement = (
  value: { latest?: { value: number | null; units?: string } } | undefined,
  fallback: Measurement["units"],
): MetricTelemetry | undefined =>
  value?.latest
    ? {
        latest: {
          value: value.latest.value,
          units: (value.latest.units as Measurement["units"] | undefined) ?? fallback,
        },
      }
    : undefined;

export interface TelemetrySlice extends TelemetryState {
  mergeTelemetry: (telemetry: TelemetryState) => void;
  updateHashboardTemperatures: (
    serial: string,
    inlet?: Measurement,
    outlet?: Measurement,
    average?: Measurement,
    maximum?: Measurement,
  ) => void;
  updatePsuTelemetry: (slot: number, telemetry: Partial<PsuTelemetryData>) => void;
  updateFanTelemetry: (slot: number, telemetry: Partial<FanTelemetryData>) => void;
  updateCoolingMode: (mode: CoolingMode) => void;
  clearOldData: (olderThanTimestamp: number) => void;
  clearTimeSeriesData: () => void;
  clearLatestData: () => void;
  clearAllData: () => void;
}

const empty = (): TelemetryState => ({
  resourceByScope: new Map(),
  rollupsByEntity: new Map(),
  hashboards: new Map(),
  asics: new Map(),
  psus: new Map(),
  fans: new Map(),
  coolingModeByOwner: new Map(),
  asicLoadByHashboard: new Map(),
});

const clearMetricPart = (state: TelemetryState, part: "latest" | "timeSeries") => {
  const values: object[] = [
    ...state.rollupsByEntity.values(),
    ...state.hashboards.values(),
    ...state.asics.values(),
    ...state.psus.values(),
    ...state.fans.values(),
  ];
  values.forEach((value) =>
    metricFields.forEach((field) => {
      const metric = (value as MetricContainer)[field] as MetricTelemetry | undefined;
      if (metric) delete metric[part];
    }),
  );
};

export const createTelemetrySlice: StateCreator<MinerStore, [["zustand/immer", never]], [], TelemetrySlice> = (
  set,
) => ({
  ...empty(),
  mergeTelemetry: (incoming) =>
    set((state) => {
      incoming.resourceByScope.forEach((value, key) => state.telemetry.resourceByScope.set(key, value));
      mergeMap(state.telemetry.rollupsByEntity, incoming.rollupsByEntity);
      mergeMap(state.telemetry.hashboards, incoming.hashboards);
      mergeMap(state.telemetry.asics, incoming.asics);
      mergeMap(state.telemetry.psus, incoming.psus);
      mergeMap(state.telemetry.fans, incoming.fans);
      incoming.coolingModeByOwner.forEach((value, key) => state.telemetry.coolingModeByOwner.set(key, value));
      incoming.asicLoadByHashboard.forEach((value, key) => state.telemetry.asicLoadByHashboard.set(key, value));
    }),
  updateHashboardTemperatures: (serial, inlet, outlet, average, maximum) =>
    set((state) => {
      const id = asHashboardId(`serial:${serial}`);
      const current = state.telemetry.hashboards.get(id) ?? { hashboardId: id };
      if (inlet) current.inletTemperature = { latest: inlet };
      if (outlet) current.outletTemperature = { latest: outlet };
      if (average) current.averageAsicTemperature = { latest: average };
      if (maximum) current.maximumAsicTemperature = { latest: maximum };
      state.telemetry.hashboards.set(id, current);
    }),
  updatePsuTelemetry: (slot, input) =>
    set((state) => {
      const id = asComponentId(`${ROOT_MODULE_ID}:psu:${slot}`);
      const current = state.telemetry.psus.get(id) ?? { componentId: id };
      if (input.inputVoltage !== undefined) current.inputVoltage = mapMeasurement(input.inputVoltage, "V");
      if (input.inputCurrent !== undefined) current.inputCurrent = mapMeasurement(input.inputCurrent, "A");
      if (input.inputPower !== undefined) current.inputPower = mapMeasurement(input.inputPower, "W");
      if (input.outputVoltage !== undefined) current.outputVoltage = mapMeasurement(input.outputVoltage, "V");
      if (input.outputCurrent !== undefined) current.outputCurrent = mapMeasurement(input.outputCurrent, "A");
      if (input.outputPower !== undefined) current.outputPower = mapMeasurement(input.outputPower, "W");
      if (input.temperatureAmbient !== undefined)
        current.ambientTemperature = mapMeasurement(input.temperatureAmbient, "°C");
      if (input.temperatureAverage !== undefined)
        current.averageTemperature = mapMeasurement(input.temperatureAverage, "°C");
      if (input.temperatureHotspot !== undefined)
        current.hotspotTemperature = mapMeasurement(input.temperatureHotspot, "°C");
      state.telemetry.psus.set(id, current);
    }),
  updateFanTelemetry: (slot, input) =>
    set((state) => {
      const id = asComponentId(`${ROOT_MODULE_ID}:fan:${slot}`);
      const current = state.telemetry.fans.get(id) ?? { componentId: id };
      if (input.rpm !== undefined) current.rpm = mapMeasurement(input.rpm, "RPM");
      if (input.percentage !== undefined) current.speedPercent = mapMeasurement(input.percentage, "%");
      if (input.minRpm !== undefined) current.minimumRpm = mapMeasurement(input.minRpm, "RPM");
      if (input.maxRpm !== undefined) current.maximumRpm = mapMeasurement(input.maxRpm, "RPM");
      state.telemetry.fans.set(id, current);
    }),
  updateCoolingMode: (mode) =>
    set((state) => {
      state.telemetry.coolingModeByOwner.set(ROOT_MODULE_KEY, mode);
    }),
  clearOldData: (cutoff) =>
    set((state) =>
      state.telemetry.resourceByScope.forEach((resource, key) => {
        if (resource.updatedAtMs !== undefined && resource.updatedAtMs < cutoff)
          state.telemetry.resourceByScope.set(key, { status: "stale", updatedAtMs: resource.updatedAtMs });
      }),
    ),
  clearTimeSeriesData: () => set((state) => clearMetricPart(state.telemetry, "timeSeries")),
  clearLatestData: () => set((state) => clearMetricPart(state.telemetry, "latest")),
  clearAllData: () =>
    set((state) => {
      Object.assign(state.telemetry, empty());
    }),
});
