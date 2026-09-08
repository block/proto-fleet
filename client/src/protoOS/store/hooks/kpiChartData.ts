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
  type ModuleId,
  ROOT_MODULE_KEY,
  type TankNode,
  type ViewSelection,
} from "../contract";
import { hashboardTelemetryView, minerTelemetryView } from "../selectors/legacyTelemetry";
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

export const moduleHashboardIds = (
  topology: { modules: ContainerTopology["modules"] } | undefined,
  moduleId: ModuleId,
) => topology?.modules.get(moduleId)?.hashboardIds ?? [];

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

const valuesBySegmentKey = (segments: KpiChartSegment[]) =>
  new Map(
    segments.map(({ key, metric }) => [
      key,
      new Map((metric?.timeSeries?.points ?? []).map((point) => [point.timestampMs, point.value])),
    ]),
  );

const chartResultFromSegments = (
  aggregateMetric: MetricTimeSeries | undefined,
  segments: KpiChartSegment[],
  duration: Duration,
  segmentsLabel: KpiChartResult["segmentsLabel"],
): KpiChartResult => {
  const series = seriesFromSegments(segments);
  if (!aggregateMetric?.points.length || aggregateMetric.bucketSizeMs === undefined) {
    return { chartData: [], chartLines: [], series, segmentsLabel };
  }

  const valuesByKey = valuesBySegmentKey(segments);
  const chartData = aggregateMetric.points.map((point) => {
    const dataPoint: ChartData = { datetime: point.timestampMs, miner: point.value };
    series.forEach(({ key }) => {
      const value = valuesByKey.get(key)?.get(point.timestampMs);
      if (value !== undefined) dataPoint[key] = value;
    });
    return dataPoint;
  });
  const startTime = aggregateMetric.points[0].timestampMs;

  return {
    chartData,
    chartLines: ["miner", ...series.map(({ key }) => key)],
    series,
    segmentsLabel,
    xAxisDomain: [startTime, startTime + getDurationMs(duration)],
  };
};

const hashboardSegmentsForModule = (
  topology: ContainerTopology,
  moduleId: ModuleId,
  metricName: KpiMetricName,
  hashboardsTelemetry: HashboardsTelemetryById,
) =>
  moduleHashboardIds(topology, moduleId)
    .map((id, index) => ({
      id,
      index,
      node: topology.hashboards.get(id),
      telemetry: hashboardsTelemetry.get(id),
    }))
    .flatMap<KpiChartSegment>(({ id, index, node, telemetry }) => {
      if (!telemetry) return [];
      const label = String(node?.location?.slot ?? index + 1);
      return [
        {
          key: node?.identity.value ?? String(id),
          label,
          name: `Hashboard ${label}`,
          colorIndex: node?.location?.slot ?? index + 1,
          metric: telemetry[metricName],
        },
      ];
    });

const moduleChartResult = (
  topology: ContainerTopology,
  moduleId: ModuleId,
  metricName: KpiMetricName,
  rollups: RollupsByEntity,
  hashboardsTelemetry: HashboardsTelemetryById,
  duration: Duration,
) => {
  const hashboardIds = moduleHashboardIds(topology, moduleId);
  const hashboardMetrics = hashboardIds.map((hashboardId) => hashboardsTelemetry.get(hashboardId));
  const aggregateMetric =
    rollups.get(asEntityKey(String(moduleId)))?.[metricName]?.timeSeries ??
    aggregateMetricTimeSeries(
      metricName,
      hashboardMetrics.map((metrics) => metrics?.[metricName]),
      hashboardMetrics,
    );

  return chartResultFromSegments(
    aggregateMetric,
    hashboardSegmentsForModule(topology, moduleId, metricName, hashboardsTelemetry),
    duration,
    "hashboards",
  );
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

const containerChartResult = (
  topology: ContainerTopology,
  selection: ViewSelection | undefined,
  metricName: KpiMetricName,
  rollups: RollupsByEntity,
  hashboardsTelemetry: HashboardsTelemetryById,
  duration: Duration,
) =>
  selection?.level === "module"
    ? moduleChartResult(topology, selection.moduleId, metricName, rollups, hashboardsTelemetry, duration)
    : tankChartResult(topology, metricName, rollups, hashboardsTelemetry, duration);

const rigChartResult = (
  hardware: HardwareSlice,
  metricName: KpiMetricName,
  rollups: RollupsByEntity,
  hashboardsTelemetry: HashboardsTelemetryById,
  duration: Duration,
): KpiChartResult => {
  const rollup = rollups.get(ROOT_MODULE_KEY);
  const intervalMs = rollup?.[metricName]?.timeSeries?.bucketSizeMs;
  const miner = minerTelemetryView(
    rollup,
    [...hardware.hashboards.values()].map((board) => board.identity.value),
  );
  if (!miner) return emptyHashboardChart();

  const minerMetric = miner[metricName]?.timeSeries;
  if (!minerMetric?.values.length || intervalMs === undefined) return emptyHashboardChart();

  const sortedMinerHashboards = miner.hashboards
    .map((id) => hashboardsTelemetry.get(getHashboardId(id)))
    .filter((value): value is HashboardTelemetry => value !== undefined)
    .map(hashboardTelemetryView)
    .sort((a, b) => {
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
  const chartData = minerMetric.values.map((minerValue, index) => {
    const dataPoint: ChartData = {
      datetime: minerMetric.startTime + index * intervalMs,
      miner: minerValue,
    };

    sortedMinerHashboards.forEach((hashboard) => {
      const hashboardMetric = hashboard[metricName]?.timeSeries;
      if (hashboardMetric?.values && hashboardMetric.values.length > index) {
        dataPoint[hashboard.serial] = hashboardMetric.values[index];
      }
    });

    return dataPoint;
  });

  return {
    chartData,
    chartLines: ["miner", ...series.map(({ key }) => key)],
    series,
    segmentsLabel: "hashboards",
    xAxisDomain: [minerMetric.startTime, minerMetric.startTime + getDurationMs(duration)],
  };
};

export const buildKpiChartData = ({
  duration,
  hardware,
  hashboardsTelemetry,
  metricName,
  rollups,
  selection,
}: {
  duration: Duration;
  hardware: HardwareSlice;
  hashboardsTelemetry: HashboardsTelemetryById;
  metricName: KpiMetricName;
  rollups: RollupsByEntity;
  selection?: ViewSelection;
}) => {
  if (hardware.topology?.kind === "container") {
    return containerChartResult(hardware.topology, selection, metricName, rollups, hashboardsTelemetry, duration);
  }

  return rigChartResult(hardware, metricName, rollups, hashboardsTelemetry, duration);
};
