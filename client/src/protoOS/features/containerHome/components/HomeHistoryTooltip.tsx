import type { HomeSeries } from "../types";
import { getDayFromEpoch, getMonthFromEpoch, getShortYearFromEpoch, getTimeFromEpoch } from "@/shared/utils/datetime";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface HomeHistoryTooltipProps {
  point?: Record<string, number | null>;
  series: HomeSeries[];
  units: string;
}

export default function HomeHistoryTooltip({ point, series, units }: HomeHistoryTooltipProps) {
  const datetime = point?.datetime;
  if (typeof datetime !== "number" || !Number.isFinite(datetime)) return null;

  const readings = series.flatMap((item) => {
    const value = point?.[item.id];
    if (typeof value !== "number" || !Number.isFinite(value)) return [];
    return [{ ...item, value }];
  });
  if (!readings.length) return null;

  const date = `${getMonthFromEpoch(datetime)}/${getDayFromEpoch(datetime)}/${getShortYearFromEpoch(datetime)}`;
  const time = getTimeFromEpoch(datetime).slice(0, -3);

  return (
    <div className="pointer-events-none w-64 rounded-xl bg-surface-elevated-base/70 px-6 py-4 shadow-200 backdrop-blur-[7px]">
      <time
        dateTime={new Date(datetime).toISOString()}
        className="mb-3 block border-b border-core-primary-10 pb-3 text-200 text-text-primary-70"
      >
        {date} • {time}
      </time>
      <ul className="space-y-2">
        {readings.map((item) => (
          <li key={item.id} className="flex items-center justify-between gap-3 text-300 text-text-primary">
            <span className="inline-flex items-center gap-2 whitespace-nowrap tabular-nums">
              <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: item.color }} aria-hidden />
              {getDisplayValue(item.value)} {units}
            </span>
            <span className="text-right">{item.label}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
