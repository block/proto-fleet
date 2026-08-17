import { useMemo } from "react";
import { asAsicId, asComponentId, asHashboardId, ROOT_MODULE_KEY } from "../contract";
import {
  asicTelemetryView,
  fanTelemetryView,
  hashboardTelemetryView,
  minerTelemetryView,
  psuTelemetryView,
} from "../selectors/legacyTelemetry";
import type { AsicData, FanData, HashboardData, HashboardTelemetryData, MinerData, PsuData } from "../types";
import useMinerStore from "../useMinerStore";
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
  const hardware = useMinerStore((state) => state.hardware.getMiner());
  const telemetry = useMinerStore((state) =>
    minerTelemetryView(
      state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY),
      [...state.hardware.hashboards.values()].map((board) => board.identity.value),
    ),
  );

  return useMemo(() => {
    if (!hardware || !telemetry) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardware, telemetry]);
};

/**
 * Get combined hashboard data combining hardware info + telemetry
 */
export const useMinerHashboard = (serial: string | null): HashboardData | null => {
  const hardware = useMinerStore((state) => (serial ? state.hardware.getHashboard(serial) : null));
  const telemetry = useMinerStore((state) => {
    const value = serial ? state.telemetry.hashboards.get(asHashboardId(`serial:${serial}`)) : undefined;
    return value ? hashboardTelemetryView(value) : undefined;
  });

  return useMemo(() => {
    if (!serial || !hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [serial, hardware, telemetry]);
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
      const normalized = telemetryHashboards.get(asHashboardId(`serial:${hardware.serial}`));
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
  const hardware = useMinerStore((state) => state.hardware.getAsic(asicId));
  const telemetry = useMinerStore((state) => {
    const value = state.telemetry.asics.get(asAsicId(asicId));
    return value ? asicTelemetryView(value) : undefined;
  });

  return useMemo(() => {
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardware, telemetry]);
};

/**
 * Get all combined ASICs for a specific hashboard
 */
export const useMinerHashboardAsics = (hashboardSerial: string): AsicData[] => {
  const hashboard = useMinerStore((state) => state.hardware.getHashboard(hashboardSerial));
  const hardware = useMinerStore((state) => state.hardware);
  const telemetryData = useMinerStore((state) => state.telemetry.asics);

  return useMemo(() => {
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
  }, [hashboard, hardware, telemetryData]);
};

/**
 * Get combined PSU data combining hardware info + telemetry
 */
export const useMinerPsu = (id: number): PsuData | null => {
  const hardware = useMinerStore((state) => state.hardware.getPsu(id));
  const telemetry = useMinerStore((state) => {
    const value = state.telemetry.psus.get(asComponentId(`module:root:psu:${id}`));
    return value ? psuTelemetryView(value) : undefined;
  });

  return useMemo(() => {
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardware, telemetry]);
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
      const value = telemetryPsus.get(asComponentId(`module:root:psu:${hardware.id}`));
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
  const hardware = useMinerStore((state) => state.hardware.getFan(id));
  const telemetry = useMinerStore((state) => {
    const value = state.telemetry.fans.get(asComponentId(`module:root:fan:${id}`));
    return value ? fanTelemetryView(value) : undefined;
  });

  return useMemo(() => {
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardware, telemetry]);
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
      const value = telemetryFans.get(asComponentId(`module:root:fan:${hardware.slot}`));
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
export const useChartDataForMetric = (
  metricName: "hashrate" | "temperature" | "power" | "efficiency",
): { chartData: ChartData[]; chartLines: string[]; xAxisDomain?: [number, number] } => {
  // Select primitive values and lastUpdated to trigger re-render when data clears
  const miner = useMinerStore((state) =>
    minerTelemetryView(
      state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY),
      [...state.hardware.hashboards.values()].map((board) => board.identity.value),
    ),
  );
  const hashboardsTelemetry = useMinerStore((state) => state.telemetry.hashboards);
  const hardware = useMinerStore((state) => state.hardware);
  const intervalMs = useMinerStore(
    (state) => state.telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.[metricName]?.timeSeries?.bucketSizeMs ?? 900_000,
  );
  const duration = useMinerStore((state) => state.ui.duration);

  return useMemo(() => {
    if (!miner) return { chartData: [], chartLines: [] };

    const minerMetric = miner[metricName]?.timeSeries;
    if (!minerMetric?.values.length) return { chartData: [], chartLines: [] };

    // Get hashboards associated with this miner
    const minerHashboards = miner.hashboards
      .map((id) => hashboardsTelemetry.get(asHashboardId(`serial:${id}`)))
      .filter((value) => value !== undefined)
      .map(hashboardTelemetryView) as HashboardTelemetryData[];

    // Sort hashboards by slot using hardware slice
    const sortedMinerHashboards = minerHashboards.sort((a, b) => {
      const slotA = hardware.getHashboard(a.serial)?.slot;
      const slotB = hardware.getHashboard(b.serial)?.slot;
      return (slotA ?? 0) - (slotB ?? 0);
    });

    // Generate chart lines (keys that will be in the data)
    const chartLines = ["miner", ...sortedMinerHashboards.map((hb) => hb.serial)];

    // Create chart data points
    const chartData = minerMetric.values.map((minerValue, index) => {
      const datetime = minerMetric.startTime + index * intervalMs;

      const dataPoint: ChartData = {
        datetime,
        miner: minerValue,
      };

      // Add hashboard values for the same metric and timestamp
      sortedMinerHashboards.forEach((hashboard) => {
        const hashboardMetric = hashboard[metricName]?.timeSeries;
        if (hashboardMetric?.values && hashboardMetric?.values.length > index) {
          dataPoint[hashboard.serial] = hashboardMetric?.values[index];
        }
      });

      return dataPoint;
    });

    // Anchor the X-axis to the data's startTime and extend by the exact
    // selected duration so the chart always spans the full user-selected range
    const durationMs = getDurationMs(duration);
    const xAxisDomain: [number, number] = [minerMetric.startTime, minerMetric.startTime + durationMs];

    return { chartData, chartLines, xAxisDomain };
  }, [miner, hashboardsTelemetry, hardware, intervalMs, metricName, duration]);
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
