import { enableMapSet } from "immer";
import type { StateCreator } from "zustand";
import { shallow } from "zustand/shallow";

import {
  asEntityKey,
  type ComponentId,
  type ContainerId,
  type CoolingMode,
  type EntityKey,
  type FanTelemetry,
  type HardwareState,
  type LevelRollup,
  type Measurement,
  type MetricTelemetry,
  type PsuHardware,
  type PsuTelemetry,
  ROOT_MODULE_KEY,
  type TelemetrySnapshot,
  type TelemetryState,
} from "../contract";
import type { MinerStore } from "../useMinerStore";
import { createEmptyTelemetryState } from "../utils/createEmptyTelemetryState";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";
import { projectPsuReadings, psuRollupPower } from "../utils/psuFreshness";
import { boardTemperatures } from "../utils/readingFreshness";
import { containerModuleInventoryKey, containerPlantInventoryKey, replaceHardwareSnapshot } from "./hardwareSlice";

enableMapSet();

type MetricContainer = Record<string, unknown>;
const metricFields = [
  "hashrate",
  "idealHashrate",
  "power",
  "efficiency",
  "temperature",
  "minimumTemperature",
  "maximumTemperature",
  "inletTemperature",
  "outletTemperature",
  "outletATemperature",
  "outletBTemperature",
  "tankALiquidLevel",
  "tankBLiquidLevel",
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
  "srHeatsinkTemperature",
  "pfcHeatsinkTemperature",
  "dryCoolerInletTemperature",
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
  "coolantPumpSpeed",
  "pumpInletPressure",
  "pumpOutletPressure",
  "pumpPressureDelta",
  "heatRejectionInletPressure",
  "heatRejectionOutletPressure",
  "heatRejectionPressureDelta",
  "current",
  "voltageAb",
  "voltageBc",
  "voltageCa",
  "currentA",
  "currentB",
  "currentC",
  "activeEnergy",
] as const;

type MetricReadings = Partial<Record<(typeof metricFields)[number], MetricTelemetry>>;

const clearedLatestReadings = (previous: MetricReadings) => {
  const cleared: MetricReadings = {};
  for (const field of metricFields) {
    const latest = previous[field]?.latest;
    if (latest) cleared[field] = { latest: { ...latest, value: null } };
  }
  return cleared;
};

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

const isStaleWrite = (gate: StalenessGate | undefined, key: string) => {
  if (!gate) return false;
  const entityId = `${gate.prefix}:${key}`;
  if (gate.requestId < (gate.lastApplied.get(entityId) ?? 0)) return true;
  gate.lastApplied.set(entityId, gate.requestId);
  return false;
};

const mergeMap = <K, V extends object>(target: Map<K, V>, incoming: Map<K, V>, gate?: StalenessGate) => {
  incoming.forEach((value, key) => {
    target.set(key, mergeMetrics(target.get(key), value, isStaleWrite(gate, String(key))));
  });
};

const boardTemperatureFields = ["temperature", "minimumTemperature", "maximumTemperature"] as const;

const refreshPsuRollup = (rollup: LevelRollup, readPower: (id: ComponentId) => number | null | undefined) => {
  if (rollup.psuInputIds === undefined) return rollup;
  const { power, efficiency, powerCoverage } = psuRollupPower(
    rollup.psuInputIds,
    readPower,
    rollup.hashrate?.latest?.value ?? undefined,
  );
  if (
    rollup.power?.latest?.value === power.latest?.value &&
    rollup.efficiency?.latest?.value === efficiency.latest?.value &&
    rollup.powerCoverage?.kind === powerCoverage.kind
  )
    return rollup;
  return {
    ...rollup,
    power: { ...rollup.power, ...power },
    efficiency: { ...rollup.efficiency, ...efficiency },
    powerCoverage,
  };
};

const mergeRollups = (
  target: TelemetryState["rollupsByEntity"],
  incoming: TelemetryState["rollupsByEntity"],
  psus: TelemetryState["psus"],
  rollupGate: StalenessGate | undefined,
  boardTemperatureGate: StalenessGate | undefined,
) => {
  for (const [key, rollup] of incoming) {
    const current = target.get(key);
    const merged = mergeMetrics(current, rollup, isStaleWrite(rollupGate, key));
    const hasBoardSnapshot = rollup.boardTemperatureSnapshot !== undefined;
    if (hasBoardSnapshot || current?.boardTemperatureSnapshot !== undefined) {
      // Miner-only and history polls do not set the order of full board snapshots.
      const source = hasBoardSnapshot && !isStaleWrite(boardTemperatureGate, key) ? rollup : current;
      if (source?.boardTemperatureSnapshot === undefined) delete merged.boardTemperatureSnapshot;
      else merged.boardTemperatureSnapshot = source.boardTemperatureSnapshot;
      if (source?.psuInputIds === undefined) delete merged.psuInputIds;
      else merged.psuInputIds = source.psuInputIds;
      for (const field of boardTemperatureFields) {
        const metric = merged[field] ?? {};
        const latest = source?.[field]?.latest;
        if (latest === undefined) delete metric.latest;
        else metric.latest = latest;
        if (metric.latest === undefined && metric.timeSeries === undefined) delete merged[field];
        else merged[field] = metric;
      }
    }
    target.set(
      key,
      refreshPsuRollup(merged, (id) => psus.get(id)?.inputPower?.latest?.value),
    );
  }
};

export interface TelemetrySlice extends TelemetryState {
  // Tracks the last-applied requestId per entity so newest-write-wins is enforced
  // per entity, independent of the caller's resource-key string. Not part of the
  // persisted/reset TelemetryState contract; cleared explicitly in clearAllData.
  lastAppliedRequestByEntity: Map<string, number>;
  /** Tank and module readings and PSU identities belong to the latest complete map. */
  mapInventoryByContainer: Map<ContainerId, { rollupKeys: EntityKey[]; psus: Map<ComponentId, PsuHardware> }>;
  /** A hardware fetch includes topology and telemetry from one snapshot. */
  mergeTelemetry: (telemetry: TelemetrySnapshot, requestId?: number, hardware?: HardwareState) => void;
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
  refreshBoardTemperatures: (now: number) => void;
  refreshPsuReadings: (now: number) => void;
  clearAllData: () => void;
}

const clearMetricPart = (state: TelemetryState, part: "latest" | "timeSeries") => {
  if (part === "latest") {
    for (const rollup of state.rollupsByEntity.values()) {
      delete rollup.boardTemperatureSnapshot;
      delete rollup.psuInputIds;
    }
    for (const psu of state.psus.values()) delete psu.rawObservations;
  }
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
  get,
) => ({
  ...createEmptyTelemetryState(),
  lastAppliedRequestByEntity: new Map<string, number>(),
  mapInventoryByContainer: new Map(),
  mergeTelemetry: (incoming, requestId, hardware) =>
    set((state) => {
      if (hardware) replaceHardwareSnapshot(state, hardware, requestId);
      const lastApplied = state.telemetry.lastAppliedRequestByEntity;
      const gate = (prefix: string): StalenessGate | undefined =>
        requestId === undefined ? undefined : { requestId, lastApplied, prefix };
      const rollups = new Map(incoming.rollupsByEntity);
      const topology = state.hardware.topology;
      const plant = new Map(incoming.plant);
      if (incoming.containerPlant) {
        const { containerId, hardware } = incoming.containerPlant;
        const inventoryKey = containerPlantInventoryKey(containerId);
        if (requestId !== undefined && requestId < (lastApplied.get(inventoryKey) ?? 0)) {
          // An older complete snapshot must not restore components or readings.
          plant.clear();
        } else {
          if (requestId !== undefined) lastApplied.set(inventoryKey, requestId);
          for (const [id, component] of hardware) state.hardware.plant.set(id, component);
          for (const [id, component] of state.hardware.plant) {
            if (component.ref.owner.kind !== "container" || component.ref.owner.id !== containerId || plant.has(id))
              continue;
            const previous = state.telemetry.plant.get(id);
            if (!previous) continue;
            plant.set(id, { ...clearedLatestReadings(previous), componentId: id, health: "unknown" });
          }
        }
      }
      const psus = new Map(incoming.psus);
      // PSU inventory and readings come from the same successful module snapshot.
      // Keep known hardware when data disappears, but clear its current readings.
      if (incoming.containerModules) {
        const { containerId, moduleIds, psus: hardware } = incoming.containerModules;
        const inventoryKey = containerModuleInventoryKey(containerId);
        if (requestId !== undefined && requestId < (lastApplied.get(inventoryKey) ?? 0)) {
          psus.clear();
          rollups.clear();
        } else {
          if (requestId !== undefined) lastApplied.set(inventoryKey, requestId);
          const previousInventory = state.telemetry.mapInventoryByContainer.get(containerId);
          const reportedModules = new Set(moduleIds);
          const installedKeys =
            topology?.kind === "container" && topology.containerId === containerId
              ? [...topology.tanks.keys(), ...topology.modules.keys()].map(asEntityKey)
              : [];
          for (const key of new Set([...(previousInventory?.rollupKeys ?? []), ...installedKeys])) {
            if (rollups.has(key)) continue;
            const previous = state.telemetry.rollupsByEntity.get(key);
            if (!previous) continue;
            rollups.set(key, {
              ...clearedLatestReadings(previous),
              boardTemperatureSnapshot: { samples: [], evaluatedAt: NaN, nextChangeAt: Infinity },
              psuInputIds: null,
              health: "unknown",
              miningStatus: "unknown",
              boardCount: undefined,
              onlineBoardCount: undefined,
              powerCoverage: undefined,
            });
          }
          const reportedPsus = new Map<ComponentId, PsuHardware>();
          // A module can omit its PSU readings while its first topology request is in flight.
          for (const [id, psu] of new Map([...(previousInventory?.psus ?? []), ...hardware])) {
            if (psu.ref.owner.kind !== "module" || !reportedModules.has(psu.ref.owner.id)) continue;
            const stale = requestId !== undefined && requestId < (lastApplied.get(`psu:${id}`) ?? 0);
            const reported = stale ? (previousInventory?.psus.get(id) ?? state.hardware.psus.get(id)) : psu;
            if (!reported) continue;
            reportedPsus.set(id, reported);
            // A poll can report an identity before a topology response names its module.
            const module = topology?.modules.get(psu.ref.owner.id);
            if (!module || stale) continue;
            state.hardware.psus.set(id, psu);
            if (!module.psuIds.includes(id)) module.psuIds.push(id);
          }
          state.telemetry.mapInventoryByContainer.set(containerId, {
            rollupKeys: [...incoming.rollupsByEntity.keys()],
            psus: reportedPsus,
          });
          const knownPsus = [...state.hardware.psus.values(), ...(previousInventory?.psus.values() ?? [])];
          for (const { ref } of knownPsus) {
            const id = ref.id;
            if (ref.owner.kind !== "module" || psus.has(id)) continue;
            if (!reportedModules.has(ref.owner.id) && !previousInventory?.psus.has(id)) continue;
            const previous = state.telemetry.psus.get(id);
            if (previous)
              psus.set(id, {
                ...clearedLatestReadings(previous),
                componentId: id,
                health: "unknown",
                observedAt: {},
                rawObservations: undefined,
              });
          }
        }
      }
      incoming.resourceByScope.forEach((value, key) => state.telemetry.resourceByScope.set(key, value));
      mergeMap(state.telemetry.psus, psus, gate("psu"));
      mergeRollups(
        state.telemetry.rollupsByEntity,
        rollups,
        state.telemetry.psus,
        gate("rollup"),
        gate("board-temperature"),
      );
      mergeMap(state.telemetry.hashboards, incoming.hashboards, gate("hashboard"));
      mergeMap(state.telemetry.asics, incoming.asics, gate("asic"));
      mergeMap(state.telemetry.fans, incoming.fans, gate("fan"));
      mergeMap(state.telemetry.plant, plant, gate("plant"));
      incoming.coolingModeByOwner.forEach((value, key) => state.telemetry.coolingModeByOwner.set(key, value));
      incoming.asicLoadByHashboard.forEach((value, key) => state.telemetry.asicLoadByHashboard.set(key, value));
    }),
  refreshBoardTemperatures: (now) => {
    const changes: TelemetryState["rollupsByEntity"] = new Map();
    for (const [key, rollup] of get().telemetry.rollupsByEntity) {
      const snapshot = rollup.boardTemperatureSnapshot;
      if (!snapshot?.samples.length) continue;
      // Recompute on clock loss, recovery, a backwards correction or a sample crossing its age limit.
      const sameWindow = Number.isFinite(now)
        ? now >= snapshot.evaluatedAt && now < snapshot.nextChangeAt
        : !Number.isFinite(snapshot.evaluatedAt);
      if (sameWindow) continue;
      changes.set(key, mergeMetrics(rollup, boardTemperatures(snapshot.samples, now)));
    }
    if (changes.size)
      set((state) => {
        changes.forEach((rollup, key) => state.telemetry.rollupsByEntity.set(key, rollup));
      });
  },
  refreshPsuReadings: (now) => {
    const { psus, rollupsByEntity } = get().telemetry;
    const changes: TelemetryState["psus"] = new Map();
    const changedPower = new Set<ComponentId>();
    for (const [id, psu] of psus) {
      if (!psu.rawObservations) continue;
      const projected = projectPsuReadings(psu, now);
      const before: MetricReadings = psu;
      const after: MetricReadings = projected;
      if (
        projected.health === psu.health &&
        shallow(projected.observedAt, psu.observedAt) &&
        metricFields.every((field) => before[field]?.latest?.value === after[field]?.latest?.value)
      )
        continue;
      changes.set(id, mergeMetrics(psu, projected));
      if (projected.inputPower?.latest?.value !== psu.inputPower?.latest?.value) changedPower.add(id);
    }
    const rollupChanges: TelemetryState["rollupsByEntity"] = new Map();
    if (changedPower.size) {
      const readPower = (id: ComponentId) => (changes.get(id) ?? psus.get(id))?.inputPower?.latest?.value;
      for (const [key, rollup] of rollupsByEntity) {
        if (!rollup.psuInputIds?.some((id) => changedPower.has(id))) continue;
        const refreshed = refreshPsuRollup(rollup, readPower);
        if (refreshed !== rollup) rollupChanges.set(key, refreshed);
      }
    }
    if (changes.size)
      set((state) => {
        changes.forEach((psu, id) => state.telemetry.psus.set(id, psu));
        rollupChanges.forEach((rollup, key) => state.telemetry.rollupsByEntity.set(key, rollup));
      });
  },
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
        fan.power = fan.power?.timeSeries ? { timeSeries: fan.power.timeSeries } : undefined;
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
      state.telemetry.mapInventoryByContainer.clear();
    }),
});
