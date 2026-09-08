import { useMemo } from "react";
import {
  asAsicId,
  asEntityKey,
  type HashboardId,
  type LevelRollup,
  type MetricTelemetry,
  ROOT_MODULE_KEY,
  type Topology,
  type ViewSelection,
} from "../contract";
import {
  asicTelemetryView,
  fanTelemetryView,
  hashboardTelemetryView,
  minerTelemetryView,
  psuTelemetryView,
} from "../selectors/legacyTelemetry";
import type { HardwareSlice } from "../slices/hardwareSlice";
import type { AsicData, FanData, HashboardData, MinerData, PsuData } from "../types";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";
import {
  aggregateMetricTimeSeries,
  buildKpiChartData,
  type KpiChartResult,
  type KpiMetricName,
  kpiMetricUnits,
  moduleHashboardIds,
} from "./kpiChartData";
import type { AsicData as AsicTableData } from "@/shared/components/AsicTablePreview";

const homeRollupKey = (topology: Topology | undefined) =>
  topology?.kind === "container" ? asEntityKey(String(topology.containerId)) : ROOT_MODULE_KEY;

const rollupKeyForSelection = (topology: Topology | undefined, selection: ViewSelection | undefined) => {
  if (selection?.level === "container") return asEntityKey(String(selection.containerId));
  if (selection?.level === "tank") return asEntityKey(String(selection.tankId));
  if (selection?.level === "module" || selection?.level === "rig") return asEntityKey(String(selection.moduleId));
  return homeRollupKey(topology);
};

const hashboardIdentities = (topology: Topology | undefined, hashboardIds: HashboardId[]) =>
  hashboardIds.map((id) => topology?.hashboards.get(id)?.identity.value ?? String(id).replace(/^serial:/, ""));

const withDerivedModuleHistory = (
  rollup: LevelRollup | undefined,
  hashboardIds: HashboardId[],
  hashboardsTelemetry: Map<HashboardId, { [key in KpiMetricName]?: MetricTelemetry }>,
): LevelRollup | undefined => {
  if (!rollup && hashboardIds.length === 0) return undefined;

  const derived = { ...(rollup ?? {}) };
  (Object.keys(kpiMetricUnits) as KpiMetricName[]).forEach((metricName) => {
    const existing = rollup?.[metricName];
    if (existing?.timeSeries) return;
    const selectedHashboardMetrics = hashboardIds.map((hashboardId) => hashboardsTelemetry.get(hashboardId));
    const timeSeries = aggregateMetricTimeSeries(
      metricName,
      selectedHashboardMetrics.map((metrics) => metrics?.[metricName]),
      selectedHashboardMetrics,
    );
    if (timeSeries) derived[metricName] = { ...existing, timeSeries };
  });

  return derived;
};

const selectedMinerHardware = (
  hardwareState: HardwareSlice,
  selection: ViewSelection | undefined,
  selectedModuleHashboardIds: HashboardId[],
) => {
  if (selection?.level === "module") {
    return {
      hashboardSerials: hashboardIdentities(hardwareState.topology, selectedModuleHashboardIds),
      controlBoardSerial: [...hardwareState.controlBoards.values()][0]?.serialNumber,
    };
  }

  const rigHardware = hardwareState.getMiner();
  if (rigHardware) return rigHardware;

  if (hardwareState.topology?.kind !== "container") return null;

  return {
    hashboardSerials: [...hardwareState.topology.hashboards.values()].map((board) => board.identity.value),
    controlBoardSerial: [...hardwareState.controlBoards.values()][0]?.serialNumber,
  };
};

// =============================================================================
// Miner Convenience Hooks (combining hardware + telemetry slices)
// =============================================================================

/**
 * Get combined miner data combining hardware info + telemetry
 */
export const useMiner = (selection?: ViewSelection): MinerData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const rollups = useMinerStore((state) => state.telemetry.rollupsByEntity);
  const hashboardsTelemetry = useMinerStore((state) => state.telemetry.hashboards);

  return useMemo(() => {
    const selectedModuleHashboardIds =
      selection?.level === "module" ? moduleHashboardIds(hardwareState.topology, selection.moduleId) : [];
    const hardware = selectedMinerHardware(hardwareState, selection, selectedModuleHashboardIds);
    const rollup = withDerivedModuleHistory(
      rollups.get(rollupKeyForSelection(hardwareState.topology, selection)),
      selectedModuleHashboardIds,
      hashboardsTelemetry,
    );
    const telemetry = minerTelemetryView(rollup, hardware?.hashboardSerials ?? []);

    if (!hardware || !telemetry) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, hashboardsTelemetry, rollups, selection]);
};

/**
 * Get combined hashboard data combining hardware info + telemetry
 */
export const useMinerHashboard = (serial: string | null): HashboardData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetryValue = useMinerStore((state) =>
    serial ? state.telemetry.hashboards.get(getHashboardId(serial)) : undefined,
  );

  return useMemo(() => {
    if (!serial) return null;
    const hardware = hardwareState.getHashboard(serial);
    if (!hardware) return null;
    const telemetry = telemetryValue ? hashboardTelemetryView(telemetryValue) : undefined;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [serial, hardwareState, telemetryValue]);
};

/**
 * Get all combined hashboards for the miner
 */
export const useMinerHashboards = (): HashboardData[] => {
  const hardwareHashboards = useMinerStore((state) => state.hardware.hashboards);
  const telemetryHashboards = useMinerStore((state) => state.telemetry.hashboards);

  return useMemo(() => {
    const hashboards = Array.from(hardwareHashboards.values())
      .map((board) => useMinerStore.getState().hardware.getHashboard(board.identity.value))
      .filter((board) => board !== undefined);

    return hashboards.map((hardware) => {
      const normalized = telemetryHashboards.get(getHashboardId(hardware.serial));
      const telemetry = normalized ? hashboardTelemetryView(normalized) : undefined;

      return {
        ...hardware,
        ...telemetry,
      };
    });
  }, [hardwareHashboards, telemetryHashboards]);
};

/**
 * Get combined ASIC data combining hardware info + telemetry
 */
export const useMinerAsic = (asicId: string): AsicData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetryValue = useMinerStore((state) => state.telemetry.asics.get(asAsicId(asicId)));

  return useMemo(() => {
    const hardware = hardwareState.getAsic(asicId);
    if (!hardware) return null;
    const telemetry = telemetryValue ? asicTelemetryView(telemetryValue) : undefined;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [asicId, hardwareState, telemetryValue]);
};

/**
 * Get all combined ASICs for a specific hashboard
 */
export const useMinerHashboardAsics = (hashboardSerial: string): AsicData[] => {
  const hardware = useMinerStore((state) => state.hardware);
  const telemetryData = useMinerStore((state) => state.telemetry.asics);

  return useMemo(() => {
    const hashboard = hardware.getHashboard(hashboardSerial);
    if (!hashboard || !hashboard.asicIds) return [];

    return hashboard.asicIds.reduce<AsicData[]>((acc, asicId) => {
      const asicHardware = hardware.getAsic(asicId);
      if (!asicHardware) return acc;

      const telemetryValue = telemetryData.get(asAsicId(asicId));
      const telemetry = telemetryValue ? asicTelemetryView(telemetryValue) : undefined;

      acc.push({
        ...asicHardware,
        ...telemetry,
      });

      return acc;
    }, []);
  }, [hashboardSerial, hardware, telemetryData]);
};

/**
 * Get combined PSU data combining hardware info + telemetry
 */
export const useMinerPsu = (id: number): PsuData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetryValue = useMinerStore((state) => state.telemetry.psus.get(getPsuId(id)));

  return useMemo(() => {
    const hardware = hardwareState.getPsu(id);
    if (!hardware) return null;
    const telemetry = telemetryValue ? psuTelemetryView(telemetryValue) : undefined;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, id, telemetryValue]);
};

/**
 * Get all combined PSUs for the miner
 */
export const useMinerPsus = (): PsuData[] => {
  const hardware = useMinerStore((state) => state.hardware);
  const telemetryPsus = useMinerStore((state) => state.telemetry.psus);

  return useMemo(() => {
    const psus = hardware.getAllPsus();

    return psus.map((hardware) => {
      const value = telemetryPsus.get(getPsuId(hardware.id));
      const telemetry = value ? psuTelemetryView(value) : undefined;

      return {
        ...hardware,
        ...telemetry,
      };
    });
  }, [hardware, telemetryPsus]);
};

/**
 * Get combined Fan data combining hardware info + telemetry
 */
export const useMinerFan = (id: number): FanData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetryValue = useMinerStore((state) => state.telemetry.fans.get(getFanId(id)));

  return useMemo(() => {
    const hardware = hardwareState.getFan(id);
    if (!hardware) return null;
    const telemetry = telemetryValue ? fanTelemetryView(telemetryValue) : undefined;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, id, telemetryValue]);
};

/**
 * Get all combined Fans for the miner
 */
export const useMinerFans = (): FanData[] => {
  const hardware = useMinerStore((state) => state.hardware);
  const telemetryFans = useMinerStore((state) => state.telemetry.fans);

  return useMemo(() => {
    const fans = hardware.getAllFans();

    return fans.map((hardware) => {
      const value = telemetryFans.get(getFanId(hardware.slot));
      const telemetry = value ? fanTelemetryView(value) : undefined;

      return {
        ...hardware,
        ...telemetry,
      };
    });
  }, [hardware, telemetryFans]);
};

// =============================================================================
// Chart Data Hooks
// =============================================================================

/**
 * Main chart data hook that combines miner and hashboard data for a specific metric
 * Transforms telemetry store data into chart-ready format
 */
export const useChartDataForMetric = (metricName: KpiMetricName, selection?: ViewSelection): KpiChartResult => {
  // Select primitive values and lastUpdated to trigger re-render when data clears
  const rollups = useMinerStore((state) => state.telemetry.rollupsByEntity);
  const hashboardsTelemetry = useMinerStore((state) => state.telemetry.hashboards);
  const hardware = useMinerStore((state) => state.hardware);
  const duration = useMinerStore((state) => state.ui.duration);

  return useMemo(() => {
    return buildKpiChartData({
      duration,
      hardware,
      hashboardsTelemetry,
      metricName,
      rollups,
      selection,
    });
  }, [rollups, hashboardsTelemetry, hardware, metricName, duration, selection]);
};

// =============================================================================
// Data Transformation Hooks
// =============================================================================

/**
 * Transforms ProtoOS ASIC data to the shared AsicData format used by AsicTablePreview component.
 *
 * This hook provides a consistent way to transform the store's ASIC data structure
 * (which includes hardware and telemetry information) into the simplified format
 * expected by the shared AsicTablePreview component.
 *
 * @param asics - Array of ProtoOS ASIC data from the store
 * @returns Array of AsicData formatted for AsicTablePreview component
 *
 * @example
 * ```typescript
 * const asics = useMinerHashboardAsics(serialNumber);
 * const asicData = useAsicDataTransform(asics);
 *
 * return <AsicTablePreview asics={asicData} />;
 * ```
 */
export const useAsicDataTransform = (asics: AsicData[] | undefined | null): AsicTableData[] => {
  return useMemo((): AsicTableData[] => {
    if (!asics || asics.length === 0) return [];

    return asics
      .filter((asic) => asic.row !== undefined && asic.column !== undefined)
      .map((asic) => ({
        row: asic.row!,
        col: asic.column!,
        value: asic.temperature?.latest?.value ?? null,
      }));
  }, [asics]);
};
