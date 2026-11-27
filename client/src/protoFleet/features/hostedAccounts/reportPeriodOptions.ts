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
  // The year the period starts in, and its label within that year.
  year: string;
  periodLabel: string;
}

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

// The calendar days an existing period covers. The end is read from the last covered instant, so a
// period ending partway through a day still counts that day as covered.
export const calendarRangeOf = (
  start: Timestamp | undefined,
  end: Timestamp | undefined,
  timezone: string,
): Partial<CalendarRange> => {
  const last = end && calendarDay({ ...end, seconds: end.seconds - (end.nanos > 0 ? 0n : 1n) }, timezone);
  return {
    start: calendarDay(start, timezone),
    end: last ? dayValue(new Date(new Date(`${last}T00:00:00Z`).getTime() + 86400000)) : undefined,
  };
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
// The list reaches back five years, or to `earliest` (a calendar day) when that is further back.
export const scheduledPeriodOptions = (
  kind: string,
  today: string,
  yearStart: ReportingYearStart,
  // Month (1–12) that any scheduled quarter starts in; quarters follow the reporting year without one.
  quarterMonth = yearStart.month,
  earliest?: string,
): ReportPeriodOption[] => {
  const [year, month] = today.split("-").map(Number);
  if (!["monthly", "quarterly", "annual"].includes(kind) || !year || !month) return [];
  const fiveYearsBack = `${String(year - 5).padStart(4, "0")}${today.slice(4)}`;
  const floor = earliest && earliest < fiveYearsBack ? earliest : fiveYearsBack;
  const monthIndex = month - 1;
  const yearMonthIndex = yearStart.month - 1;
  const period = (back: number): [Date, Date] => {
    if (kind === "annual")
      return [
        utcDay(year - back, yearMonthIndex, yearStart.day),
        utcDay(year - back + 1, yearMonthIndex, yearStart.day),
      ];
    const months = kind === "quarterly" ? 3 : 1;
    // A February quarter start gives Feb–Apr, May–Jul, and so on.
    const first = monthIndex - ((((monthIndex - (quarterMonth - 1)) % months) + months) % months) - back * months;
    return [utcDay(year, first, 1), utcDay(year, first + months, 1)];
  };
  const options: ReportPeriodOption[] = [];
  for (let back = 0; back < 1200; back += 1) {
    const [first, next] = period(back);
    const range = { start: dayValue(first), end: dayValue(next) };
    if (range.end <= floor) break;
    if (range.end > today) continue;
    const last = new Date(next.getTime() - 86400000);
    let label = calendarRangeLabel(range);
    let periodLabel = label;
    if (kind === "monthly") {
      label = format(first, { month: "long", year: "numeric" });
      periodLabel = format(first, { month: "long" });
    } else if (kind === "quarterly") {
      const sameYear = first.getUTCFullYear() === last.getUTCFullYear();
      const from = format(first, sameYear ? { month: "short" } : { month: "short", year: "numeric" });
      label = `${from}–${format(last, { month: "short", year: "numeric" })}`;
      // A quarter that crosses into the next year keeps both years so it is not misread.
      periodLabel = sameYear ? `${from}–${format(last, { month: "short" })}` : label;
    } else if (yearStart.month === 1 && yearStart.day === 1) label = periodLabel = format(first, { year: "numeric" });
    options.push({ ...range, value: calendarRangeValue(range), label, year: range.start.slice(0, 4), periodLabel });
  }
  return options;
};
