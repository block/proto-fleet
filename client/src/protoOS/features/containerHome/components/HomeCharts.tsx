import { useId, useMemo } from "react";
import { Area, AreaChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { DotItemDotProps } from "recharts";
import type { HomeHistory, HomeSeries, LocationReading } from "../types";
import HomeHistoryTooltip from "./HomeHistoryTooltip";

interface HistoryChartProps {
  history: HomeHistory;
  compact?: boolean;
}

export function HomeHistoryChart({ history, compact = false }: HistoryChartProps) {
  return (
    <div>
      {history.error ? (
        <p role="alert" className="mb-3 text-300 text-text-critical">
          {history.error}
        </p>
      ) : null}
      {history.notice ? <p className="mb-3 text-200 text-text-primary-70">{history.notice}</p> : null}
      <div className="flex flex-col gap-6 tablet:flex-row">
        <div className={compact ? "h-72 min-w-0 tablet:flex-1" : "h-80 min-w-0 tablet:h-96 tablet:flex-1"}>
          <HistoryPlot history={history} compact={compact} />
        </div>
        {!compact ? <HistoryLegend series={history.series} /> : null}
      </div>
    </div>
  );
}

function HistoryPlaceholder({ loading }: { loading?: boolean }) {
  return (
    <div
      role="status"
      className="flex h-full items-center justify-center rounded-xl border border-core-primary-10 px-6 text-center text-300 text-text-primary-50"
    >
      {loading ? "Loading history…" : "History unavailable for this selection."}
    </div>
  );
}

function HistoryPlot({ history, compact }: HistoryChartProps) {
  const titleId = useId();
  const data = useMemo(() => {
    const rows = new Map<number, Record<string, number | null>>();
    for (const series of history.series) {
      for (const point of series.points) {
        if (point.datetime < history.window[0] || point.datetime > history.window[1]) continue;
        const row = rows.get(point.datetime) ?? { datetime: point.datetime };
        row[series.id] = point.value;
        rows.set(point.datetime, row);
      }
    }
    return [...rows.values()].sort((a, b) => Number(a.datetime) - Number(b.datetime));
  }, [history.series, history.window]);
  const hasValues = data.some((row) => history.series.some((series) => typeof row[series.id] === "number"));
  if (!hasValues) return <HistoryPlaceholder loading={history.loading} />;

  const Chart = history.stacked ? AreaChart : LineChart;
  const formatTimeTick = (value: number) => {
    if (history.window[1] - history.window[0] > 24 * 3_600_000)
      return new Date(value).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit" });
    return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  };
  // Keep the Recharts primitives as direct chart children.
  const seriesElements = history.series.map((series) => {
    const commonProps = {
      dataKey: series.id,
      name: series.label,
      stroke: series.color,
      type: "linear" as const,
      connectNulls: false,
      isAnimationActive: false,
      dot: IsolatedHistoryDot,
    };
    if (history.stacked)
      return <Area key={series.id} {...commonProps} stackId="power" fill={series.color} fillOpacity={1} />;
    return <Line key={series.id} {...commonProps} strokeWidth={2} />;
  });

  return (
    <figure className="h-full" aria-labelledby={titleId}>
      <figcaption id={titleId} className="sr-only">
        History in {history.units} for the selected period. Latest values are listed alongside the chart.
      </figcaption>
      <ResponsiveContainer width="100%" height="100%" minWidth={0}>
        <Chart data={data} margin={{ top: 16, right: 8, bottom: 0, left: 0 }}>
          {!compact ? <CartesianGrid vertical={false} stroke="var(--color-core-primary-5)" /> : null}
          <XAxis
            dataKey="datetime"
            type="number"
            domain={history.window}
            hide={compact}
            tickFormatter={formatTimeTick}
            tick={{ fill: "var(--color-text-primary-50)", fontSize: 12 }}
            minTickGap={36}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            hide={compact}
            width={48}
            domain={history.stacked ? [0, "auto"] : ["auto", "auto"]}
            tick={{ fill: "var(--color-text-primary-50)", fontSize: 12 }}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip
            isAnimationActive={false}
            content={({ active, payload }) => (
              <HomeHistoryTooltip
                point={active ? payload?.[0]?.payload : undefined}
                series={history.series}
                units={history.units}
              />
            )}
          />
          {seriesElements}
        </Chart>
      </ResponsiveContainer>
    </figure>
  );
}

function IsolatedHistoryDot({ cx, cy, points, index, stroke }: DotItemDotProps) {
  if (!Number.isFinite(cx) || !Number.isFinite(cy)) return null;
  // Use the plotted neighbors so nulls, missing timestamps and window edges match the line's gaps.
  const hasNeighbor = [points[index - 1], points[index + 1]].some(
    (point) => Number.isFinite(point?.x) && Number.isFinite(point?.y),
  );
  if (hasNeighbor) return null;
  return <circle cx={cx} cy={cy} r={3} fill={stroke} aria-hidden />;
}

function HistoryLegend({ series }: { series: HomeSeries[] }) {
  if (!series.length) return null;
  return (
    <ul
      aria-label="Latest series values"
      className="flex flex-wrap gap-x-6 gap-y-3 tablet:w-40 tablet:shrink-0 tablet:flex-col tablet:pt-4"
    >
      {series.map((item) => (
        <li key={item.id} className="flex items-center gap-2 text-200">
          <span className="size-2 rounded-full" style={{ backgroundColor: item.color }} aria-hidden />
          <span>{item.label}</span>
          <span className="ml-auto tabular-nums">{item.latest}</span>
        </li>
      ))}
    </ul>
  );
}

export function LocationBarChart({
  readings,
  color = "var(--color-core-accent-fill)",
}: {
  readings: LocationReading[];
  color?: string;
}) {
  if (!readings.length)
    return (
      <p role="status" className="flex min-h-60 items-center justify-center text-300 text-text-primary-50">
        No tanks reported.
      </p>
    );

  const max = Math.max(0, ...readings.map((reading) => reading.amount ?? 0));
  return (
    <ul aria-label="Readings by tank" className="flex min-h-60 gap-3 overflow-x-auto pt-6">
      {readings.map((reading) => (
        <LocationBar key={reading.id} reading={reading} max={max} color={color} />
      ))}
    </ul>
  );
}

function LocationBar({ reading, max, color }: { reading: LocationReading; max: number; color: string }) {
  const height = max > 0 ? (Math.max(0, reading.amount ?? 0) / max) * 100 : 0;
  return (
    <li className="flex min-w-12 flex-1 flex-col items-center gap-2 text-center text-200 text-text-primary-50">
      <div className="flex h-48 w-3 items-end overflow-hidden rounded bg-core-primary-5" aria-hidden>
        {reading.amount !== null ? (
          <div className="w-full" style={{ height: `${height}%`, backgroundColor: color }} />
        ) : null}
      </div>
      <span>{reading.label}</span>
      <span className="text-text-primary tabular-nums">{reading.value}</span>
    </li>
  );
}
