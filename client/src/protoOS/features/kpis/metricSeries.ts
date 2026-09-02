import type { KpiChartSeries } from "@/protoOS/store/hooks/useMiner";

const performerBy = (
  series: KpiChartSeries[],
  value: (item: KpiChartSeries) => number | null | undefined,
  compare: (candidate: number, current: number) => boolean,
  initial: number,
) => {
  let performer: string | undefined;
  let current = initial;
  series.forEach((item) => {
    const candidate = value(item);
    if (typeof candidate === "number" && compare(candidate, current)) {
      current = candidate;
      performer = item.name;
    }
  });
  return performer;
};

export const getLowestAveragePerformer = (series: KpiChartSeries[]) =>
  performerBy(
    series,
    (item) => item.averageValue,
    (candidate, current) => candidate < current,
    Infinity,
  );

export const getHighestAveragePerformer = (series: KpiChartSeries[]) =>
  performerBy(
    series,
    (item) => item.averageValue,
    (candidate, current) => candidate > current,
    -Infinity,
  );

export const getHighestMaximumPerformer = (series: KpiChartSeries[]) =>
  performerBy(
    series,
    (item) => item.maximumValue,
    (candidate, current) => candidate > current,
    -Infinity,
  );
