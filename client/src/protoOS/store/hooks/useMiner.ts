import { useMemo } from "react";
import type {
  AsicData,
  HashboardData,
  HashboardTelemetryData,
  MinerData,
} from "../types";
import useMinerStore from "../useMinerStore";
import type { ChartData } from "@/shared/components/LineChart";

// =============================================================================
// Miner Convenience Hooks (combining hardware + telemetry slices)
// =============================================================================

/**
 * Get combined miner data combining hardware info + telemetry
 */
export const useMiner = (): MinerData | null => {
  const hardware = useMinerStore((state) => state.hardware.miner);
  const telemetry = useMinerStore((state) => state.telemetry.miner);

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
export const useMinerHashboard = (serial: string): HashboardData | null => {
  const hardware = useMinerStore((state) =>
    state.hardware.getHashboard(serial),
  );
  const telemetry = useMinerStore((state) =>
    state.telemetry.hashboards.get(serial),
  );

  return useMemo(() => {
    if (!hardware) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardware, telemetry]);
};

/**
 * Get all combined hashboards for the miner
 */
export const useMinerHashboards = (): HashboardData[] => {
  const hardwareHashboards = useMinerStore(
    (state) => state.hardware.hashboards,
  );
  const telemetryHashboards = useMinerStore(
    (state) => state.telemetry.hashboards,
  );

  return useMemo(() => {
    const hashboards = Array.from(hardwareHashboards.values());

    return hashboards.map((hardware) => {
      const telemetry = telemetryHashboards.get(hardware.serial);

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
  const telemetry = useMinerStore((state) => state.telemetry.asics.get(asicId));

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
  const hashboard = useMinerStore((state) =>
    state.hardware.getHashboard(hashboardSerial),
  );
  const allAsics = useMinerStore((state) => state.hardware.asics);
  const telemetryData = useMinerStore((state) => state.telemetry.asics);

  return useMemo(() => {
    if (!hashboard || !hashboard.asicIds) return [];

    return hashboard.asicIds.reduce<AsicData[]>((acc, asicId) => {
      const hardware = allAsics.get(asicId);
      if (!hardware) return acc;

      const telemetry = telemetryData.get(asicId);

      acc.push({
        ...hardware,
        ...telemetry,
      });

      return acc;
    }, []);
  }, [hashboard, allAsics, telemetryData]);
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
): { chartData: ChartData[]; chartLines: string[] } => {
  // Select primitive values and lastUpdated to trigger re-render when data clears
  const miner = useMinerStore((state) => state.telemetry.miner);
  const hashboardsTelemetry = useMinerStore(
    (state) => state.telemetry.hashboards,
  );
  const hashboardsHardware = useMinerStore(
    (state) => state.hardware.hashboards,
  );
  const intervalMs = useMinerStore((state) => state.telemetry.intervalMs);

  return useMemo(() => {
    if (!miner) return { chartData: [], chartLines: [] };

    const minerMetric = miner[metricName]?.timeSeries;
    if (!minerMetric?.values.length) return { chartData: [], chartLines: [] };

    // Get hashboards associated with this miner
    const minerHashboards = miner.hashboards
      .map((id) => hashboardsTelemetry.get(id))
      .filter(Boolean) as HashboardTelemetryData[];

    // Sort hashboards by slot using hardware slice
    const sortedMinerHashboards = minerHashboards.sort((a, b) => {
      const slotA = hashboardsHardware.get(a.serial)?.slot;
      const slotB = hashboardsHardware.get(b.serial)?.slot;
      return (slotA ?? 0) - (slotB ?? 0);
    });

    // Generate chart lines (keys that will be in the data)
    const chartLines = [
      "miner",
      ...sortedMinerHashboards.map((hb) => hb.serial),
    ];

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

    return { chartData, chartLines };
  }, [miner, hashboardsTelemetry, hashboardsHardware, intervalMs, metricName]);
};
