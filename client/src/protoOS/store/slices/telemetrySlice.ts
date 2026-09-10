import { enableMapSet } from "immer";
import type { StateCreator } from "zustand";

import {
  type CoolingMode,
  type FanTelemetry,
  type Measurement,
  type MetricTelemetry,
  type PsuTelemetry,
  ROOT_MODULE_KEY,
  type TelemetryState,
} from "../contract";
import type { MinerStore } from "../useMinerStore";
import { createEmptyTelemetryState } from "../utils/createEmptyTelemetryState";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";

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
  "passRate",
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
  "totalPower",
  "heatRejectionPower",
  "facilityPower",
  "powerFactor",
  "pue",
  "flowRate",
  "current",
  "voltageAb",
  "voltageBc",
  "voltageCa",
  "currentA",
  "currentB",
  "currentC",
  "activeEnergy",
] as const;

// When `preferCurrent` is true the incoming snapshot is stale for this entity
// (an older request landing after a newer one already wrote): current values win
// on conflict, and stale data may only fill fields the current snapshot lacks.
const mergeMetrics = <T extends object>(current: T | undefined, incoming: T, preferCurrent = false): T => {
  const merged = (preferCurrent ? { ...incoming, ...(current ?? {}) } : { ...(current ?? {}), ...incoming }) as T &
    MetricContainer;
  for (const field of metricFields) {
    const before = (current as MetricContainer | undefined)?.[field] as MetricTelemetry | undefined;
    const after = (incoming as MetricContainer)[field] as MetricTelemetry | undefined;
    if (!before && !after) continue;
    // Lower-priority source is applied first, then the winner overrides it.
    const lower = preferCurrent ? after : before;
    const higher = preferCurrent ? before : after;
    const metric: MetricTelemetry = {};
    if (lower?.latest !== undefined) metric.latest = lower.latest;
    if (lower?.timeSeries !== undefined) metric.timeSeries = lower.timeSeries;
    if (higher?.latest !== undefined) metric.latest = higher.latest;
    if (higher?.timeSeries !== undefined) metric.timeSeries = higher.timeSeries;
    if (metric.latest !== undefined || metric.timeSeries !== undefined) (merged as MetricContainer)[field] = metric;
    else delete (merged as MetricContainer)[field];
  }
  return merged;
};

// Per-entity newest-write-wins gate. Returns whether the incoming write is stale
// for a given entity id, recording the requestId of accepted (fresh) writes so a
// slower response keyed on a different resource string can't clobber fresher data.
interface StalenessGate {
  requestId: number;
  lastApplied: Map<string, number>;
  prefix: string;
}

const mergeMap = <K, V extends object>(target: Map<K, V>, incoming: Map<K, V>, gate?: StalenessGate) => {
  incoming.forEach((value, key) => {
    let preferCurrent = false;
    if (gate) {
      const entityId = `${gate.prefix}:${String(key)}`;
      if (gate.requestId < (gate.lastApplied.get(entityId) ?? 0)) preferCurrent = true;
      else gate.lastApplied.set(entityId, gate.requestId);
    }
    target.set(key, mergeMetrics(target.get(key), value, preferCurrent));
  });
};

export interface TelemetrySlice extends TelemetryState {
  // Tracks the last-applied requestId per entity so newest-write-wins is enforced
  // per entity, independent of the caller's resource-key string. Not part of the
  // persisted/reset TelemetryState contract; cleared explicitly in clearAllData.
  lastAppliedRequestByEntity: Map<string, number>;
  mergeTelemetry: (telemetry: TelemetryState, requestId?: number) => void;
  updateHashboardTemperatures: (
    serial: string,
    inlet?: Measurement,
    outlet?: Measurement,
    average?: Measurement,
    maximum?: Measurement,
  ) => void;
  updatePsuTelemetry: (slot: number, telemetry: Partial<PsuTelemetry>) => void;
  updateFanTelemetry: (slot: number, telemetry: Partial<FanTelemetry>) => void;
  replaceFanLiveTelemetry: (fans: FanTelemetry[]) => void;
  updateCoolingMode: (mode: CoolingMode) => void;
  clearOldData: (olderThanTimestamp: number) => void;
  clearTimeSeriesData: () => void;
  clearLatestData: () => void;
  clearAllData: () => void;
}

const clearMetricPart = (state: TelemetryState, part: "latest" | "timeSeries") => {
  const values: object[] = [
    ...state.rollupsByEntity.values(),
    ...state.hashboards.values(),
    ...state.asics.values(),
    ...state.psus.values(),
    ...state.fans.values(),
    ...state.plant.values(),
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
  ...createEmptyTelemetryState(),
  lastAppliedRequestByEntity: new Map<string, number>(),
  mergeTelemetry: (incoming, requestId) =>
    set((state) => {
      const lastApplied = state.telemetry.lastAppliedRequestByEntity;
      const gate = (prefix: string): StalenessGate | undefined =>
        requestId === undefined ? undefined : { requestId, lastApplied, prefix };
      incoming.resourceByScope.forEach((value, key) => state.telemetry.resourceByScope.set(key, value));
      mergeMap(state.telemetry.rollupsByEntity, incoming.rollupsByEntity, gate("rollup"));
      mergeMap(state.telemetry.hashboards, incoming.hashboards, gate("hashboard"));
      mergeMap(state.telemetry.asics, incoming.asics, gate("asic"));
      mergeMap(state.telemetry.psus, incoming.psus, gate("psu"));
      mergeMap(state.telemetry.fans, incoming.fans, gate("fan"));
      mergeMap(state.telemetry.plant, incoming.plant, gate("plant"));
      incoming.coolingModeByOwner.forEach((value, key) => state.telemetry.coolingModeByOwner.set(key, value));
      incoming.asicLoadByHashboard.forEach((value, key) => state.telemetry.asicLoadByHashboard.set(key, value));
    }),
  updateHashboardTemperatures: (serial, inlet, outlet, average, maximum) =>
    set((state) => {
      const id = getHashboardId(serial);
      const current = state.telemetry.hashboards.get(id) ?? { hashboardId: id };
      if (inlet) current.inletTemperature = { latest: inlet };
      if (outlet) current.outletTemperature = { latest: outlet };
      if (average) current.averageAsicTemperature = { latest: average };
      if (maximum) current.maximumAsicTemperature = { latest: maximum };
      state.telemetry.hashboards.set(id, current);
    }),
  updatePsuTelemetry: (slot, input) =>
    set((state) => {
      const id = getPsuId(slot);
      // Input is already the contract shape (Partial<PsuTelemetry>), so merge it
      // straight in — mergeMetrics carries both `latest` and `timeSeries` per field.
      state.telemetry.psus.set(id, mergeMetrics(state.telemetry.psus.get(id), { componentId: id, ...input }));
    }),
  updateFanTelemetry: (slot, input) =>
    set((state) => {
      const id = getFanId(slot);
      state.telemetry.fans.set(id, mergeMetrics(state.telemetry.fans.get(id), { componentId: id, ...input }));
    }),
  replaceFanLiveTelemetry: (fans) =>
    set((state) => {
      state.telemetry.fans.forEach((fan) => {
        fan.rpm = fan.rpm?.timeSeries ? { timeSeries: fan.rpm.timeSeries } : undefined;
        fan.speedPercent = fan.speedPercent?.timeSeries ? { timeSeries: fan.speedPercent.timeSeries } : undefined;
      });
      fans.forEach((fan) => {
        state.telemetry.fans.set(fan.componentId, mergeMetrics(state.telemetry.fans.get(fan.componentId), fan));
      });
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
      Object.assign(state.telemetry, createEmptyTelemetryState());
      state.telemetry.lastAppliedRequestByEntity.clear();
    }),
});
