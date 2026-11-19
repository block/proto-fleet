import { useMemo } from "react";
import type { HomeHistory } from "../types";
import LineChart from "@/shared/components/LineChart";
import SkeletonBar from "@/shared/components/SkeletonBar";

// The same compact presentation as the Fleet dashboard panels: three labelled
// grid lines, four time labels, and no axis line.
const VISIBLE_TICKS = [0, 2, 4];
// The shared chart resolves colours from CSS variable names.
const SERIES_COLORS = ["--color-core-primary-fill", "--color-core-accent-fill"];

/** One compact history plot for the Overview; several series share its axis. */
export default function ProductionChart({ history }: { history: HomeHistory }) {
  const { series } = history;
  const chartData = useMemo(() => {
    const rows = new Map<number, { datetime: number; [key: string]: number | null }>();
    let plotted = false;
    for (const item of series)
      for (const point of item.points) {
        const row = rows.get(point.datetime) ?? { datetime: point.datetime };
        row[item.id] = point.value;
        rows.set(point.datetime, row);
        plotted ||= point.value !== null;
      }
    return plotted ? [...rows.values()].sort((a, b) => a.datetime - b.datetime) : null;
  }, [series]);
  if (!chartData)
    return history.loading ? (
      <SkeletonBar className="h-60 w-full" />
    ) : (
      <p role="status" className="flex h-60 items-center justify-center text-300 text-text-primary-50">
        No history for this period.
      </p>
    );
  const keys = series.map((item) => item.id);
  const colorMap = Object.fromEntries(keys.map((key, index) => [key, SERIES_COLORS[index % SERIES_COLORS.length]]));
  return (
    <>
      <div className="flex h-60 w-full *:min-h-0! [&_.recharts-cartesian-axis-line]:hidden">
        <LineChart
          chartData={chartData}
          aggregateKey={keys[0]}
          activeKeys={keys}
          colorMap={colorMap}
          units={history.units}
          xAxisDomainOverride={history.window}
          tickCount={5}
          yAxisTickYOffset={-8}
          visibleTickIndices={VISIBLE_TICKS}
          chartMarginTop={20}
          xAxisLabelCount={4}
          hideAggregateContextWhenSingleSeries
        />
      </div>
      {series.length > 1 ? (
        <ul aria-label="Latest series values" className="flex flex-wrap gap-x-6 gap-y-2 text-200">
          {series.map((item) => (
            <li key={item.id} className="flex items-center gap-2">
              <span
                className="size-2 rounded-full"
                style={{ backgroundColor: `var(${colorMap[item.id]})` }}
                aria-hidden
              />
              <span>{item.label}</span>
              <span className="text-text-primary-50 tabular-nums">{item.latest}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </>
  );
}
