import { type Timestamp, timestampFromDate } from "@bufbuild/protobuf/wkt";

import {
  buildDateInTimeZone,
  formatDateParts,
  getTimeZoneDateTimeParts,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

// Calendar days (YYYY-MM-DD) in the reporting timezone. `end` is the day after the last covered day.
export interface CalendarRange {
  start: string;
  end: string;
}
export interface ReportingYearStart {
  month: number;
  day: number;
}
export interface ReportPeriodOption extends CalendarRange {
  value: string;
  label: string;
}

const optionCounts: Record<string, number> = { monthly: 24, quarterly: 12, annual: 5 };

// Calendar days are carried as UTC midnights so month arithmetic never crosses a clock change.
const utcDay = (year: number, monthIndex: number, day: number) => new Date(Date.UTC(year, monthIndex, day));
const dayValue = (date: Date) => date.toISOString().slice(0, 10);
const format = (date: Date, options: Intl.DateTimeFormatOptions) =>
  new Intl.DateTimeFormat(undefined, { ...options, timeZone: "UTC" }).format(date);

export const calendarDayStart = (day: string | undefined, timezone: string): Timestamp | undefined => {
  const instant = day ? buildDateInTimeZone(day, "00:00", timezone) : null;
  return instant ? timestampFromDate(instant) : undefined;
};

export const calendarDay = (time: Timestamp | undefined, timezone: string) => {
  if (!time) return undefined;
  const parts = getTimeZoneDateTimeParts(new Date(Number(time.seconds) * 1000), timezone);
  return parts ? formatDateParts(parts) : undefined;
};

export const calendarToday = (timezone: string, now = new Date()) =>
  calendarDay(timestampFromDate(now), timezone) ?? dayValue(now);

export const calendarRangeValue = (range: Partial<CalendarRange>) => `${range.start ?? ""}/${range.end ?? ""}`;

export const calendarRangeLabel = ({ start, end }: CalendarRange) => {
  const first = new Date(`${start}T00:00:00Z`);
  const last = new Date(new Date(`${end}T00:00:00Z`).getTime() - 86400000);
  if (!Number.isFinite(first.getTime()) || !Number.isFinite(last.getTime())) return `${start} – ${end}`;
  return `${format(first, { dateStyle: "medium" })} – ${format(last, { dateStyle: "medium" })}`;
};

// Completed periods only, newest first: a report's evidence cannot predate the end of its period.
export const scheduledPeriodOptions = (
  kind: string,
  today: string,
  yearStart: ReportingYearStart,
): ReportPeriodOption[] => {
  const count = optionCounts[kind];
  const [year, month] = today.split("-").map(Number);
  if (!count || !year || !month) return [];
  const monthIndex = month - 1;
  const yearMonthIndex = yearStart.month - 1;
  const period = (back: number): [Date, Date] => {
    if (kind === "annual")
      return [
        utcDay(year - back, yearMonthIndex, yearStart.day),
        utcDay(year - back + 1, yearMonthIndex, yearStart.day),
      ];
    const months = kind === "quarterly" ? 3 : 1;
    // Quarters follow the reporting year, so a July year start gives Jul–Sep, Oct–Dec, and so on.
    const first = monthIndex - ((((monthIndex - yearMonthIndex) % months) + months) % months) - back * months;
    return [utcDay(year, first, 1), utcDay(year, first + months, 1)];
  };
  const options: ReportPeriodOption[] = [];
  for (let back = 0; options.length < count && back < count + 2; back += 1) {
    const [first, next] = period(back);
    const range = { start: dayValue(first), end: dayValue(next) };
    if (range.end > today) continue;
    const last = new Date(next.getTime() - 86400000);
    let label = calendarRangeLabel(range);
    if (kind === "monthly") label = format(first, { month: "long", year: "numeric" });
    else if (kind === "quarterly")
      label = `${format(first, first.getUTCFullYear() === last.getUTCFullYear() ? { month: "short" } : { month: "short", year: "numeric" })}–${format(last, { month: "short", year: "numeric" })}`;
    else if (yearStart.month === 1 && yearStart.day === 1) label = format(first, { year: "numeric" });
    options.push({ ...range, value: calendarRangeValue(range), label });
  }
  return options;
};
