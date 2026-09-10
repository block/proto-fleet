import { useMemo } from "react";
import { asAsicId, ROOT_MODULE_KEY } from "../contract";
import type { AsicData, FanData, HashboardData, MinerData, PsuData } from "../types";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";
import type { AsicData as AsicTableData } from "@/shared/components/AsicTablePreview";
import { getDurationMs } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart";

// =============================================================================
// Miner Convenience Hooks (combining hardware + telemetry slices)
// =============================================================================

/**
 * Get combined miner data combining hardware info + telemetry
 */
export const useMiner = (): MinerData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY));

  return useMemo(() => {
    const hardware = hardwareState.getMiner();

    if (!hardware || !rollup) return null;

    return {
      ...hardware,
      hashrate: rollup.hashrate,
      power: rollup.power,
      efficiency: rollup.efficiency,
      temperature: rollup.temperature,
    };
  }, [hardwareState, rollup]);
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
export const useMinerPsu = (id: number): PsuData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry.psus.get(getPsuId(id)));

  return useMemo(() => {
    const hardware = hardwareState.getPsu(id);
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, id, telemetry]);
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
export const useChartDataForMetric = (
  metricName: "hashrate" | "temperature" | "power" | "efficiency",
): { chartData: ChartData[]; chartLines: string[]; xAxisDomain?: [number, number] } => {
  // Select primitive values and lastUpdated to trigger re-render when data clears
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY));
  const hashboardsTelemetry = useMinerStore((state) => state.telemetry.hashboards);
  const hardware = useMinerStore((state) => state.hardware);
  const intervalMs = useMinerStore(
    (state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.[metricName]?.timeSeries?.bucketSizeMs,
  );
  const duration = useMinerStore((state) => state.ui.duration);

  return useMemo(() => {
    const minerPoints = rollup?.[metricName]?.timeSeries?.points ?? [];
    if (!minerPoints.length || intervalMs === undefined) return { chartData: [], chartLines: [] };

    // Anchor the series to the first bucket timestamp, then advance by the fixed
    // bucket interval per index so miner + hashboard series stay index-aligned.
    const startTime = minerPoints[0]?.timestampMs ?? 0;

    // Get hashboards for this miner (from hardware), keep their telemetry + slot for sorting
    const sortedHashboards = [...hardware.hashboards.values()]
      .map((board) => {
        const serial = board.identity.value;
        return {
          serial,
          slot: hardware.getHashboard(serial)?.slot ?? 0,
          telemetry: hashboardsTelemetry.get(getHashboardId(serial)),
        };
      })
      .filter((board) => board.telemetry !== undefined)
      .sort((a, b) => a.slot - b.slot);

    // Generate chart lines (keys that will be in the data)
    const chartLines = ["miner", ...sortedHashboards.map((hb) => hb.serial)];

    // Create chart data points
    const chartData = minerPoints.map((point, index) => {
      const datetime = startTime + index * intervalMs;

      const dataPoint: ChartData = {
        datetime,
        miner: point.value,
      };

      // Add hashboard values for the same metric and timestamp
      sortedHashboards.forEach(({ serial, telemetry }) => {
        const hashboardPoints = telemetry?.[metricName]?.timeSeries?.points;
        if (hashboardPoints && hashboardPoints.length > index) {
          dataPoint[serial] = hashboardPoints[index]?.value ?? null;
        }
      });

      return dataPoint;
    });

    // Anchor the X-axis to the data's startTime and extend by the exact
    // selected duration so the chart always spans the full user-selected range
    const durationMs = getDurationMs(duration);
    const xAxisDomain: [number, number] = [startTime, startTime + durationMs];

    return { chartData, chartLines, xAxisDomain };
  }, [rollup, hashboardsTelemetry, hardware, intervalMs, metricName, duration]);
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
