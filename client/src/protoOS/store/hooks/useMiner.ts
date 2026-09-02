import { useMemo } from "react";
import { asAsicId, asEntityKey, ROOT_MODULE_KEY, type Topology } from "../contract";
import {
  asicTelemetryView,
  fanTelemetryView,
  hashboardTelemetryView,
  minerTelemetryView,
  psuTelemetryView,
} from "../selectors/legacyTelemetry";
import type { AsicData, FanData, HashboardData, HashboardTelemetryData, MinerData, PsuData } from "../types";
import useMinerStore from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";
import type { AsicData as AsicTableData } from "@/shared/components/AsicTablePreview";
import { getDurationMs } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart";

type KpiMetricName = "hashrate" | "temperature" | "power" | "efficiency";

export interface KpiChartSeries {
  key: string;
  /** Compact selector label, for example `1`. */
  label: string;
  /** Full entity label used in summaries and tooltips. */
  name: string;
  colorIndex: number;
  averageValue?: number | null;
  maximumValue?: number | null;
}

const homeRollupKey = (topology: Topology | undefined) =>
  topology?.kind === "container" ? asEntityKey(String(topology.containerId)) : ROOT_MODULE_KEY;

const compactTankLabel = (label: string, fallback: number) => {
  const withoutPrefix = label.replace(/^tank\s+/i, "").trim();
  return withoutPrefix || String(fallback);
};

// =============================================================================
// Miner Convenience Hooks (combining hardware + telemetry slices)
// =============================================================================

/**
 * Get combined miner data combining hardware info + telemetry
 */
export const useMiner = (): MinerData | null => {
  const hardwareState = useMinerStore((state) => state.hardware);
  const rollups = useMinerStore((state) => state.telemetry.rollupsByEntity);

  return useMemo(() => {
    const hardware =
      hardwareState.getMiner() ??
      (hardwareState.topology?.kind === "container"
        ? {
            hashboardSerials: [...hardwareState.topology.hashboards.values()].map((board) => board.identity.value),
            controlBoardSerial: [...hardwareState.controlBoards.values()][0]?.serialNumber,
          }
        : null);
    const rollup = rollups.get(homeRollupKey(hardwareState.topology));
    const telemetry = minerTelemetryView(
      rollup,
      [...hardwareState.hashboards.values()].map((board) => board.identity.value),
    );

    if (!hardware || !telemetry) return null;

    return {
      ...hardware,
      ...telemetry,
    };
  }, [hardwareState, rollups]);
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
export const useChartDataForMetric = (
  metricName: KpiMetricName,
): {
  chartData: ChartData[];
  chartLines: string[];
  series: KpiChartSeries[];
  segmentsLabel: "hashboards" | "tanks";
  xAxisDomain?: [number, number];
} => {
  // Select primitive values and lastUpdated to trigger re-render when data clears
  const rollups = useMinerStore((state) => state.telemetry.rollupsByEntity);
  const hashboardsTelemetry = useMinerStore((state) => state.telemetry.hashboards);
  const hardware = useMinerStore((state) => state.hardware);
  const duration = useMinerStore((state) => state.ui.duration);

  return useMemo(() => {
    if (hardware.topology?.kind === "container") {
      const tanks = [...hardware.topology.tanks.values()].sort(
        (a, b) => (a.slot ?? Number.MAX_SAFE_INTEGER) - (b.slot ?? Number.MAX_SAFE_INTEGER),
      );
      const series = tanks.map<KpiChartSeries>((tank, index) => {
        const position = tank.slot ?? index + 1;
        const name = tank.label?.trim() || `Tank ${position}`;
        const metric = rollups.get(asEntityKey(String(tank.id)))?.[metricName];
        return {
          key: String(tank.id),
          label: compactTankLabel(name, position),
          name,
          colorIndex: position,
          averageValue: metric?.timeSeries?.aggregates?.avg?.value,
          maximumValue: metric?.timeSeries?.aggregates?.max?.value,
        };
      });
      const aggregateMetric = rollups.get(asEntityKey(String(hardware.topology.containerId)))?.[metricName]?.timeSeries;
      const intervalMs = aggregateMetric?.bucketSizeMs;
      if (!aggregateMetric?.points.length || intervalMs === undefined) {
        return { chartData: [], chartLines: [], series, segmentsLabel: "tanks" as const };
      }

      const tankValues = new Map(
        tanks.map((tank) => [
          String(tank.id),
          new Map(
            (rollups.get(asEntityKey(String(tank.id)))?.[metricName]?.timeSeries?.points ?? []).map((point) => [
              point.timestampMs,
              point.value,
            ]),
          ),
        ]),
      );
      const chartData = aggregateMetric.points.map((point) => {
        const dataPoint: ChartData = { datetime: point.timestampMs, miner: point.value };
        series.forEach(({ key }) => {
          const value = tankValues.get(key)?.get(point.timestampMs);
          if (value !== undefined) dataPoint[key] = value;
        });
        return dataPoint;
      });
      const durationMs = getDurationMs(duration);
      const startTime = aggregateMetric.points[0].timestampMs;

      return {
        chartData,
        chartLines: ["miner", ...series.map(({ key }) => key)],
        series,
        segmentsLabel: "tanks" as const,
        xAxisDomain: [startTime, startTime + durationMs] as [number, number],
      };
    }

    const rollup = rollups.get(ROOT_MODULE_KEY);
    const intervalMs = rollup?.[metricName]?.timeSeries?.bucketSizeMs;
    const miner = minerTelemetryView(
      rollup,
      [...hardware.hashboards.values()].map((board) => board.identity.value),
    );
    if (!miner) return { chartData: [], chartLines: [], series: [], segmentsLabel: "hashboards" as const };

    const minerMetric = miner[metricName]?.timeSeries;
    if (!minerMetric?.values.length || intervalMs === undefined)
      return { chartData: [], chartLines: [], series: [], segmentsLabel: "hashboards" as const };

    // Get hashboards associated with this miner
    const minerHashboards = miner.hashboards
      .map((id) => hashboardsTelemetry.get(getHashboardId(id)))
      .filter((value) => value !== undefined)
      .map(hashboardTelemetryView) as HashboardTelemetryData[];

    // Sort hashboards by slot using hardware slice
    const sortedMinerHashboards = minerHashboards.sort((a, b) => {
      const slotA = hardware.getHashboard(a.serial)?.slot;
      const slotB = hardware.getHashboard(b.serial)?.slot;
      return (slotA ?? 0) - (slotB ?? 0);
    });

    const series = sortedMinerHashboards.map<KpiChartSeries>((hashboard, index) => {
      const slot = hardware.getHashboard(hashboard.serial)?.slot ?? index + 1;
      const metric = hashboard[metricName]?.timeSeries;
      return {
        key: hashboard.serial,
        label: String(slot),
        name: `Hashboard ${slot}`,
        colorIndex: slot,
        averageValue: metric?.aggregates?.avg?.value,
        maximumValue: metric?.aggregates?.max?.value,
      };
    });

    // Generate chart lines (keys that will be in the data)
    const chartLines = ["miner", ...series.map(({ key }) => key)];

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

    return { chartData, chartLines, series, segmentsLabel: "hashboards" as const, xAxisDomain };
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
