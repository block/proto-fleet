import type { ModuleHistorySample } from "./hooks/useModuleHistory";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import type { FleetDuration } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart/types";

const fields = ["hashrate", "power", "temperature", "efficiency"] as const;
export const emptyModulePoint = (datetime: number): ChartData => ({
  datetime,
  hashrate: null,
  power: null,
  temperature: null,
  efficiency: null,
});

// ListModuleHistory returns raw samples through 24h, hourly averages for 7d,
// and daily averages for longer windows. Do not upsample retained summaries.
export function moduleHistoryChart(
  samples: ModuleHistorySample[],
  duration: FleetDuration,
  unit: "C" | "F",
): ChartData[] {
  const resolution = duration === "1h" || duration === "24h" ? 0 : duration === "7d" ? 3600 : 86400;
  const width = Math.max(resolution, getGranularityForDuration(duration)) * 1000;
  const latest = new Map<number, ModuleHistorySample>();
  for (const sample of samples) {
    if (!sample.observedAt) continue;
    const at = Number(sample.observedAt.seconds) * 1000 + sample.observedAt.nanos / 1e6;
    const previous = latest.get(at);
    const collected = (s: ModuleHistorySample) =>
      Number(s.collectedAt?.seconds ?? 0n) * 1000 + (s.collectedAt?.nanos ?? 0) / 1e6;
    if (!previous || collected(sample) >= collected(previous)) latest.set(at, sample);
  }
  const buckets = new Map<number, { sum: number[]; count: number[] }>();
  for (const [at, sample] of latest) {
    const time = Math.floor(at / width) * width;
    const bucket = buckets.get(time) ?? { sum: [0, 0, 0, 0], count: [0, 0, 0, 0] };
    const values = [
      sample.hashrateHs === undefined ? undefined : sample.hashrateHs / 1e12,
      sample.powerW === undefined ? undefined : sample.powerW / 1000,
      sample.temperatureC === undefined
        ? undefined
        : unit === "F"
          ? sample.temperatureC * 1.8 + 32
          : sample.temperatureC,
      sample.efficiencyJh === undefined ? undefined : sample.efficiencyJh * 1e12,
    ];
    values.forEach((value, index) => {
      if (value !== undefined && Number.isFinite(value)) {
        bucket.sum[index] += value;
        bucket.count[index]++;
      }
    });
    buckets.set(time, bucket);
  }
  if (!buckets.size) return [];
  const times = [...buckets.keys()].sort((a, b) => a - b);
  const points: ChartData[] = [];
  for (let time = times[0]; time <= times[times.length - 1] && points.length < 1001; time += width) {
    const bucket = buckets.get(time);
    const point = emptyModulePoint(time);
    fields.forEach((field, index) => {
      point[field] = bucket?.count[index] ? bucket.sum[index] / bucket.count[index] : null;
    });
    points.push(point);
  }
  return points;
}
