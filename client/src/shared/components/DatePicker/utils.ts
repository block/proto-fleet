import { CALENDAR_ROWS, DAYS_IN_WEEK, MONTH_NAMES_SHORT, PresetId } from "./constants";
import { DayData, Timeframe, WeekDay } from "./types";

// ── Date comparison ──

export function isSameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

export function isBeforeDay(a: Date, b: Date): boolean {
  return toDateOnly(a).getTime() < toDateOnly(b).getTime();
}

export function isAfterDay(a: Date, b: Date): boolean {
  return toDateOnly(a).getTime() > toDateOnly(b).getTime();
}

export function isBetweenDays(date: Date, start: Date, end: Date): boolean {
  return isAfterDay(date, start) && isBeforeDay(date, end);
}

function toDateOnly(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

// ── Week/month boundaries ──

export function getStartOfWeek(date: Date, weekStartsOn: WeekDay): Date {
  const d = new Date(date);
  const day = d.getDay();
  const diff = (day - weekStartsOn + 7) % 7;
  d.setDate(d.getDate() - diff);
  return toDateOnly(d);
}

export function getEndOfWeek(date: Date, weekStartsOn: WeekDay): Date {
  const start = getStartOfWeek(date, weekStartsOn);
  start.setDate(start.getDate() + 6);
  return start;
}

export function getStartOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1);
}

export function getEndOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth() + 1, 0);
}

// ── Calendar grid ──

export function buildCalendarGrid(year: number, month: number, weekStartsOn: WeekDay): DayData[][] {
  const today = new Date();
  const firstOfMonth = new Date(year, month, 1);
  const firstDayOfWeek = firstOfMonth.getDay();
  const offset = (firstDayOfWeek - weekStartsOn + 7) % 7;
  const startDate = new Date(year, month, 1 - offset);

  const grid: DayData[][] = [];
  const current = new Date(startDate);

  for (let row = 0; row < CALENDAR_ROWS; row++) {
    const week: DayData[] = [];
    for (let col = 0; col < DAYS_IN_WEEK; col++) {
      week.push({
        date: current.getDate(),
        month: current.getMonth(),
        year: current.getFullYear(),
        isCurrentMonth: current.getMonth() === month && current.getFullYear() === year,
        today: isSameDay(current, today),
        disabled: false,
        dateObj: new Date(current),
      });
      current.setDate(current.getDate() + 1);
    }
    grid.push(week);
  }

  return grid;
}

export function getOrderedDayNames(weekStartsOn: WeekDay): string[] {
  const days = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];
  return [...days.slice(weekStartsOn), ...days.slice(0, weekStartsOn)];
}

// ── Timeframe checks ──

export function isDateInTimeframe(date: Date, timeframe?: Timeframe): boolean {
  if (!timeframe) return true;
  const today = new Date();
  if (timeframe === "present") return isSameDay(date, today);
  if (isSameDay(date, today)) return true;
  if (timeframe === "past" && isAfterDay(date, today)) return false;
  if (timeframe === "future" && isBeforeDay(date, today)) return false;
  return true;
}

// ── Formatting ──

export function formatDate(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

export function formatTime(date: Date): string {
  const h = String(date.getHours()).padStart(2, "0");
  const min = String(date.getMinutes()).padStart(2, "0");
  return `${h}:${min}`;
}

export function formatDisplayDate(date: Date): string {
  return `${MONTH_NAMES_SHORT[date.getMonth()]} ${date.getDate()}, ${date.getFullYear()}`;
}

export function formatDisplayDateTime(date: Date): string {
  return `${formatDisplayDate(date)} ${formatTime(date)}`;
}

// ── Parsing ──

export function parseDate(str: string): Date | null {
  const match = str.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(year, month - 1, day);
  if (isNaN(date.getTime())) return null;
  if (date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day) return null;
  return date;
}

export function parseDateTime(str: string): Date | null {
  const match = str.match(/^(\d{4})-(\d{2})-(\d{2})\s+(\d{2}):(\d{2})$/);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const date = new Date(year, month - 1, day, hour, minute);
  if (isNaN(date.getTime())) return null;
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day ||
    date.getHours() !== hour ||
    date.getMinutes() !== minute
  ) {
    return null;
  }
  return date;
}

export function isDateRangeSelectable(start: Date, end: Date, isSelectableDate: (date: Date) => boolean): boolean {
  const current = toDateOnly(start);
  const last = toDateOnly(end);

  while (current.getTime() <= last.getTime()) {
    if (!isSelectableDate(new Date(current))) return false;
    current.setDate(current.getDate() + 1);
  }

  return true;
}

// ── Preset date computation ──

export function computePresetDates(
  presetId: PresetId,
  weekStartsOn: WeekDay,
): { startDate: Date; endDate: Date } | null {
  const today = new Date();
  const todayOnly = toDateOnly(today);

  switch (presetId) {
    case "today":
      return { startDate: todayOnly, endDate: todayOnly };
    case "yesterday": {
      const yesterday = new Date(todayOnly);
      yesterday.setDate(yesterday.getDate() - 1);
      return { startDate: yesterday, endDate: yesterday };
    }
    case "this-week":
      return { startDate: getStartOfWeek(todayOnly, weekStartsOn), endDate: getEndOfWeek(todayOnly, weekStartsOn) };
    case "last-week": {
      const lastWeek = new Date(todayOnly);
      lastWeek.setDate(lastWeek.getDate() - 7);
      return { startDate: getStartOfWeek(lastWeek, weekStartsOn), endDate: getEndOfWeek(lastWeek, weekStartsOn) };
    }
    case "this-month":
      return { startDate: getStartOfMonth(todayOnly), endDate: getEndOfMonth(todayOnly) };
    case "last-month": {
      const lastMonth = new Date(todayOnly.getFullYear(), todayOnly.getMonth() - 1, 1);
      return { startDate: getStartOfMonth(lastMonth), endDate: getEndOfMonth(lastMonth) };
    }
    case "custom":
      return null;
  }
}
