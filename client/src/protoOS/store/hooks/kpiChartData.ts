import {
  asEntityKey,
  type ContainerTopology,
  type EntityKey,
  type HashboardId,
  type HashboardTelemetry,
  type LevelRollup,
  type MetricTelemetry,
  type MetricTimeSeries,
  type MetricUnit,
  ROOT_MODULE_KEY,
  type TankNode,
} from "../contract";
import type { HardwareSlice } from "../slices/hardwareSlice";
import { getHashboardId } from "../utils/entityIds";
import { type Duration, getDurationMs } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart";

export type KpiMetricName = "hashrate" | "temperature" | "power" | "efficiency";

export const kpiMetricUnits: Record<KpiMetricName, MetricUnit> = {
  hashrate: "TH/s",
  temperature: "°C",
  power: "W",
  efficiency: "J/TH",
};

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

export interface KpiChartResult {
  chartData: ChartData[];
  seriesTimestamps: Record<string, number[]>;
  chartLines: string[];
  series: KpiChartSeries[];
  segmentsLabel: "hashboards" | "tanks";
  xAxisDomain?: [number, number];
}

type RollupsByEntity = Map<EntityKey, LevelRollup>;
type HashboardsTelemetryById = Map<HashboardId, HashboardTelemetry>;

interface KpiChartSegment {
  key: string;
  label: string;
  name: string;
  colorIndex: number;
  metric?: MetricTelemetry;
}

const emptyHashboardChart = (): KpiChartResult => ({
  chartData: [],
  seriesTimestamps: {},
  chartLines: [],
  series: [],
  segmentsLabel: "hashboards",
});

const compactTankLabel = (label: string, fallback: number) => {
  const withoutPrefix = label.replace(/^tank\s+/i, "").trim();
  return withoutPrefix || String(fallback);
};

const combinePointValues = (metricName: KpiMetricName, values: number[]) => {
  if (metricName === "hashrate" || metricName === "power") {
    return values.reduce((sum, value) => sum + value, 0);
  }
  return values.reduce((sum, value) => sum + value, 0) / values.length;
};

const averagePointValues = (points: { value: number | null; sampleCount?: number }[]) => {
  const values = points.filter((point): point is typeof point & { value: number } => point.value !== null);
  if (!values.length) return undefined;
  const weighted = values.filter(
    (point): point is typeof point & { value: number; sampleCount: number } =>
      point.sampleCount !== undefined && point.sampleCount > 0,
  );
  const sampleCount = weighted.reduce((sum, point) => sum + point.sampleCount, 0);
  return weighted.length === values.length && sampleCount > 0
    ? weighted.reduce((sum, point) => sum + point.value * point.sampleCount, 0) / sampleCount
    : values.reduce((sum, point) => sum + point.value, 0) / values.length;
};

const aggregateMetricTimeSeriesByValue = (
  metricName: KpiMetricName,
  metrics: (MetricTelemetry | undefined)[],
): MetricTimeSeries | undefined => {
  const sourceSeries = metrics.flatMap((metric) => (metric?.timeSeries?.points.length ? [metric.timeSeries] : []));
  if (!sourceSeries.length) return undefined;

  const valuesByTimestamp = new Map<number, number[]>();
  sourceSeries.forEach((series) => {
    series.points.forEach((point) => {
      if (typeof point.value !== "number") return;
      const values = valuesByTimestamp.get(point.timestampMs) ?? [];
      values.push(point.value);
      valuesByTimestamp.set(point.timestampMs, values);
    });
  });

  const points = [...valuesByTimestamp.entries()]
    .sort(([left], [right]) => left - right)
    .map(([timestampMs, values]) => ({
      timestampMs,
      value: values.length ? combinePointValues(metricName, values) : null,
      sampleCount: values.length,
    }));
  const values = points.map((point) => point.value).filter((value): value is number => value !== null);
  const units = sourceSeries[0].units ?? kpiMetricUnits[metricName];

  return {
    points,
    bucketSizeMs: sourceSeries[0].bucketSizeMs,
    units,
    aggregates: values.length
      ? {
          min: { value: Math.min(...values), units },
          avg: { value: values.reduce((sum, value) => sum + value, 0) / values.length, units },
          max: { value: Math.max(...values), units },
        }
      : undefined,
  };
};

const aggregateEfficiencyTimeSeries = (
  metrics: (MetricTelemetry | undefined)[],
  relatedMetrics: ({ hashrate?: MetricTelemetry; power?: MetricTelemetry } | undefined)[],
): MetricTimeSeries | undefined => {
  const totalsByTimestamp = new Map<number, { power: number; hashrate: number; sampleCount: number }>();
  relatedMetrics.forEach((metric) => {
    const { hashrate, power } = metric ?? {};
    const powerPoints = power?.timeSeries?.points;
    const hashratePoints = hashrate?.timeSeries?.points;
    if (!powerPoints?.length || !hashratePoints?.length) return;

    const hashrateByTime = new Map(hashratePoints.map((point) => [point.timestampMs, point]));
    powerPoints.forEach((powerPoint) => {
      const hashratePoint = hashrateByTime.get(powerPoint.timestampMs);
      if (powerPoint.value === null || !hashratePoint?.value) return;
      const totals = totalsByTimestamp.get(powerPoint.timestampMs) ?? { power: 0, hashrate: 0, sampleCount: 0 };
      totals.power += powerPoint.value;
      totals.hashrate += hashratePoint.value;
      totals.sampleCount += Math.min(powerPoint.sampleCount ?? 1, hashratePoint.sampleCount ?? 1);
      totalsByTimestamp.set(powerPoint.timestampMs, totals);
    });
  });

  if (!totalsByTimestamp.size) {
    return aggregateMetricTimeSeriesByValue("efficiency", metrics);
  }

  const points = [...totalsByTimestamp.entries()]
    .sort(([left], [right]) => left - right)
    .map(([timestampMs, totals]) => ({
      timestampMs,
      value: totals.hashrate > 0 ? totals.power / totals.hashrate : null,
      sampleCount: totals.sampleCount || undefined,
    }));
  const values = points.map((point) => point.value).filter((value): value is number => value !== null);
  const units = kpiMetricUnits.efficiency;

  return {
    points,
    bucketSizeMs:
      relatedMetrics.find((metric) => metric?.power?.timeSeries?.bucketSizeMs !== undefined)?.power?.timeSeries
        ?.bucketSizeMs ??
      relatedMetrics.find((metric) => metric?.hashrate?.timeSeries?.bucketSizeMs !== undefined)?.hashrate?.timeSeries
        ?.bucketSizeMs,
    units,
    aggregates: values.length
      ? {
          min: { value: Math.min(...values), units },
          avg: { value: averagePointValues(points)!, units },
          max: { value: Math.max(...values), units },
        }
      : undefined,
  };
};

export const aggregateMetricTimeSeries = (
  metricName: KpiMetricName,
  metrics: (MetricTelemetry | undefined)[],
  relatedMetrics: ({ hashrate?: MetricTelemetry; power?: MetricTelemetry } | undefined)[] = [],
): MetricTimeSeries | undefined => {
  if (metricName === "efficiency" && relatedMetrics.length) {
    return aggregateEfficiencyTimeSeries(metrics, relatedMetrics);
  }

  return aggregateMetricTimeSeriesByValue(metricName, metrics);
};

const metricPointValues = (metric: MetricTimeSeries | undefined) =>
  metric?.points.map((point) => point.value).filter((value): value is number => typeof value === "number") ?? [];

const averageMetricValue = (metric: MetricTimeSeries | undefined) => {
  const aggregateValue = metric?.aggregates?.avg?.value;
  if (aggregateValue !== undefined) return aggregateValue;

  const values = metricPointValues(metric);
  return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : undefined;
};

const maximumMetricValue = (metric: MetricTimeSeries | undefined) => {
  const aggregateValue = metric?.aggregates?.max?.value;
  if (aggregateValue !== undefined) return aggregateValue;

  const values = metricPointValues(metric);
  return values.length ? Math.max(...values) : undefined;
};

const seriesFromSegments = (segments: KpiChartSegment[]): KpiChartSeries[] =>
  segments.map(({ key, label, name, colorIndex, metric }) => ({
    key,
    label,
    name,
    colorIndex,
    averageValue: averageMetricValue(metric?.timeSeries),
    maximumValue: maximumMetricValue(metric?.timeSeries),
  }));

const valuesByTimestamp = (metric: MetricTimeSeries | undefined) => {
  const values = new Map((metric?.points ?? []).map((point) => [point.timestampMs, point.value]));
  const bucketSizeMs = metric?.bucketSizeMs;
  if (bucketSizeMs !== undefined && bucketSizeMs > 0) {
    const timestamps = [...values.keys()].sort((left, right) => left - right);
    timestamps.slice(1).forEach((timestamp, index) => {
      const previousTime = timestamps[index];
      if (timestamp - previousTime > bucketSizeMs * 1.5) {
        // Allow small sampling jitter. One null marker breaks the path even
        // across a long outage, without allocating every missing bucket.
        values.set(previousTime + bucketSizeMs, null);
      }
    });
  }
  return values;
};

const joinChartSeries = (
  aggregateMetric: MetricTimeSeries | undefined,
  segments: Pick<KpiChartSegment, "key" | "metric">[],
): Pick<KpiChartResult, "chartData" | "seriesTimestamps"> => {
  const metrics = [{ key: "miner", metric: { timeSeries: aggregateMetric } }, ...segments];
  const valuesByKey = new Map(metrics.map(({ key, metric }) => [key, valuesByTimestamp(metric?.timeSeries)]));
  // Each series may have its own start time, gaps, or ordering. Keep every
  // recorded timestamp, including those absent from the aggregate series.
  const timestamps = [...new Set([...valuesByKey.values()].flatMap((values) => [...values.keys()]))].sort(
    (left, right) => left - right,
  );

  const chartData = timestamps.map((datetime) => {
    const dataPoint: ChartData = { datetime };
    valuesByKey.forEach((values, key) => {
      dataPoint[key] = values.get(datetime) ?? null;
    });
    return dataPoint;
  });

  // Record which timestamps belong to each line, including its gap markers.
  // Other nulls in the joined data only align the shared tooltip timeline.
  const seriesTimestamps = Object.fromEntries([...valuesByKey].map(([key, values]) => [key, [...values.keys()]]));
  return { chartData, seriesTimestamps };
};

const chartResultFromSegments = (
  aggregateMetric: MetricTimeSeries | undefined,
  segments: KpiChartSegment[],
  duration: Duration,
  segmentsLabel: KpiChartResult["segmentsLabel"],
): KpiChartResult => {
  const series = seriesFromSegments(segments);
  const { chartData, seriesTimestamps } = joinChartSeries(aggregateMetric, segments);
  if (!chartData.length) {
    return { chartData: [], seriesTimestamps, chartLines: [], series, segmentsLabel };
  }
  const startTime = chartData[0].datetime;

  return {
    chartData,
    seriesTimestamps,
    chartLines: ["miner", ...series.map(({ key }) => key)],
    series,
    segmentsLabel,
    xAxisDomain: [startTime, startTime + getDurationMs(duration)],
  };
};

const tankHashboardIds = (topology: ContainerTopology, tank: TankNode) =>
  tank.moduleIds.flatMap((moduleId) => topology.modules.get(moduleId)?.hashboardIds ?? []);

const tankChartResult = (
  topology: ContainerTopology,
  metricName: KpiMetricName,
  rollups: RollupsByEntity,
  hashboardsTelemetry: HashboardsTelemetryById,
  duration: Duration,
) => {
  const tanks = [...topology.tanks.values()].sort(
    (a, b) => (a.slot ?? Number.MAX_SAFE_INTEGER) - (b.slot ?? Number.MAX_SAFE_INTEGER),
  );
  const tankMetrics = new Map(
    tanks.map((tank) => {
      const tankRollupMetric = rollups.get(asEntityKey(String(tank.id)))?.[metricName];
      const hashboardMetrics = tankHashboardIds(topology, tank).map((hashboardId) =>
        hashboardsTelemetry.get(hashboardId),
      );
      const timeSeries =
        tankRollupMetric?.timeSeries ??
        aggregateMetricTimeSeries(
          metricName,
          hashboardMetrics.map((metrics) => metrics?.[metricName]),
          hashboardMetrics,
        );
      return [String(tank.id), { latest: tankRollupMetric?.latest, timeSeries }] as const;
    }),
  );
  const tankSegments = tanks.map<KpiChartSegment>((tank, index) => {
    const position = tank.slot ?? index + 1;
    const name = tank.label?.trim() || `Tank ${position}`;
    return {
      key: String(tank.id),
      label: compactTankLabel(name, position),
      name,
      colorIndex: position,
      metric: tankMetrics.get(String(tank.id)),
    };
  });
  const hashboardMetrics = [...hashboardsTelemetry.values()];
  const aggregateMetric =
    rollups.get(asEntityKey(String(topology.containerId)))?.[metricName]?.timeSeries ??
    aggregateMetricTimeSeries(
      metricName,
      hashboardMetrics.map((metrics) => metrics?.[metricName]),
      hashboardMetrics,
    );

  return chartResultFromSegments(aggregateMetric, tankSegments, duration, "tanks");
};

const rigChartResult = (
  hardware: HardwareSlice,
  metricName: KpiMetricName,
  rollups: RollupsByEntity,
  hashboardsTelemetry: HashboardsTelemetryById,
  duration: Duration,
): KpiChartResult => {
  const rollup = rollups.get(ROOT_MODULE_KEY);
  const aggregateMetric = rollup?.[metricName]?.timeSeries;

  const sortedMinerHashboards = [...hardware.hashboards.values()]
    .flatMap((board) => {
      const serial = board.identity.value;
      const telemetry = hashboardsTelemetry.get(getHashboardId(serial));
      if (!telemetry) return [];

      return [
        {
          serial,
          slot: hardware.getHashboard(serial)?.slot ?? 0,
          telemetry,
        },
      ];
    })
    .sort((a, b) => a.slot - b.slot);

  const series = sortedMinerHashboards.map<KpiChartSeries>((hashboard, index) => {
    const slot = hashboard.slot || index + 1;
    const metric = hashboard.telemetry?.[metricName]?.timeSeries;
    return {
      key: hashboard.serial,
      label: String(slot),
      name: `Hashboard ${slot}`,
      colorIndex: slot,
      averageValue: metric?.aggregates?.avg?.value,
      maximumValue: metric?.aggregates?.max?.value,
    };
  });
  const { chartData, seriesTimestamps } = joinChartSeries(
    aggregateMetric,
    sortedMinerHashboards.map(({ serial, telemetry }) => ({ key: serial, metric: telemetry[metricName] })),
  );
  if (!chartData.length) return emptyHashboardChart();
  const startTime = chartData[0].datetime;

  return {
    chartData,
    seriesTimestamps,
    chartLines: ["miner", ...series.map(({ key }) => key)],
    series,
    segmentsLabel: "hashboards",
    xAxisDomain: [startTime, startTime + getDurationMs(duration)],
  };
};

export const buildKpiChartData = ({
  duration,
  hardware,
  hashboardsTelemetry,
  metricName,
  rollups,
}: {
  duration: Duration;
  hardware: HardwareSlice;
  hashboardsTelemetry: HashboardsTelemetryById;
  metricName: KpiMetricName;
  rollups: RollupsByEntity;
}) => {
  if (hardware.topology?.kind === "container") {
    return tankChartResult(hardware.topology, metricName, rollups, hashboardsTelemetry, duration);
  }

  return rigChartResult(hardware, metricName, rollups, hashboardsTelemetry, duration);
};
