import { useMemo } from "react";
import {
  asAsicId,
  asEntityKey,
  type ComponentId,
  ROOT_MODULE_KEY,
  type Topology,
  type ViewSelection,
} from "../contract";
import type { HardwareSlice } from "../slices/hardwareSlice";
import type { AsicData, FanData, HashboardData, MinerData, PsuData } from "../types";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";
import { buildKpiChartData, type KpiChartResult, type KpiMetricName } from "./kpiChartData";
import type { AsicData as AsicTableData } from "@/shared/components/AsicTablePreview";

const homeRollupKey = (topology: Topology | undefined) =>
  topology?.kind === "container" ? asEntityKey(String(topology.containerId)) : ROOT_MODULE_KEY;

const rollupKeyForSelection = (topology: Topology | undefined, selection: ViewSelection | undefined) => {
  if (selection?.level === "container") return asEntityKey(String(selection.containerId));
  if (selection?.level === "rig") return asEntityKey(String(selection.moduleId));
  return homeRollupKey(topology);
};

const selectedMinerHardware = (hardwareState: HardwareSlice) => {
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

  return useMemo(() => {
    const hardware = selectedMinerHardware(hardwareState);
    const rollup = rollups.get(rollupKeyForSelection(hardwareState.topology, selection));

    if (!hardware || !rollup) return null;

    return {
      ...hardware,
      hashrate: rollup.hashrate,
      power: rollup.power,
      efficiency: rollup.efficiency,
      temperature: rollup.temperature,
    };
  }, [hardwareState, rollups, selection]);
};

/**
 * Get combined hashboard data combining hardware info + telemetry
 */
export const useMinerHashboard = (serial: string | null): HashboardData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) =>
    serial ? state.telemetry.hashboards.get(getHashboardId(serial)) : undefined,
  );

  return useMemo(() => {
    if (!serial) return null;
    const hardware = hardwareState.getHashboard(serial);
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [serial, hardwareState, telemetry]);
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
      const telemetry = telemetryHashboards.get(getHashboardId(hardware.serial));

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
  const telemetry = useMinerStore((state) => state.telemetry.asics.get(asAsicId(asicId)));

  return useMemo(() => {
    const hardware = hardwareState.getAsic(asicId);
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [asicId, hardwareState, telemetry]);
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

      const telemetry = telemetryData.get(asAsicId(asicId));

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
export const useMinerPsu = (slot: number): PsuData | null => useMinerPsuById(getPsuId(slot));

export const useMinerPsuById = (id: ComponentId): PsuData | null => {
  const hardware = useMinerStore((state) => state.hardware.psus.get(id));
  const telemetry = useMinerStore((state) => state.telemetry.psus.get(id));
  return useMemo(
    () =>
      hardware
        ? {
            id: hardware.slot ?? 1,
            slot: hardware.slot,
            serial: hardware.serialNumber,
            manufacturer: hardware.manufacturer,
            model: hardware.model,
            hwRevision: hardware.hardwareRevision,
            firmware: hardware.firmware
              ? {
                  appVersion: hardware.firmware.application?.version,
                  bootloaderVersion: hardware.firmware.bootloader?.version,
                }
              : undefined,
            ...telemetry,
          }
        : null,
    [hardware, telemetry],
  );
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
      const telemetry = telemetryPsus.get(getPsuId(hardware.id));

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
  const telemetry = useMinerStore((state) => state.telemetry.fans.get(getFanId(id)));

  return useMemo(() => {
    const hardware = hardwareState.getFan(id);
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, id, telemetry]);
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
      const telemetry = telemetryFans.get(getFanId(hardware.slot));

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
export const useChartDataForMetric = (metricName: KpiMetricName): KpiChartResult => {
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
    });
  }, [rollups, hashboardsTelemetry, hardware, metricName, duration]);
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
