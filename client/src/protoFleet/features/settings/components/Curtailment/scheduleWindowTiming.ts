import type { CurtailmentScheduleDefinition } from "@/protoFleet/api/curtailmentTypes";
import type { CurtailmentScheduleDayOfWeek } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { parseDate } from "@/protoFleet/features/settings/utils/scheduleDateUtils";

const minuteMs = 60 * 1000;
const dayMs = 24 * 60 * minuteMs;
// A year ahead covers both of a zone's annual clock changes.
const recurringLookaheadDays = 366;

export type ScheduleWindowTiming = {
  // How long a window lasts by its start and end clock times.
  clockDurationMs: number;
  // The first upcoming window whose actual length differs from its clock times
  // because the zone's UTC offset changes, such as for daylight saving time.
  clockChange: { startDate: string; durationMs: number } | null;
};

const wallClockFormatters = new Map<string, Intl.DateTimeFormat>();

function wallClockMs(instantMs: number, timeZone: string): number | null {
  let formatter = wallClockFormatters.get(timeZone);
  if (!formatter) {
    try {
      formatter = new Intl.DateTimeFormat("en-US", {
        timeZone,
        hourCycle: "h23",
        year: "numeric",
        month: "numeric",
        day: "numeric",
        hour: "numeric",
        minute: "numeric",
      });
    } catch {
      return null;
    }
    wallClockFormatters.set(timeZone, formatter);
  }
  const parts = formatter.formatToParts(instantMs);
  const part = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((p) => p.type === type)?.value);
  return Date.UTC(part("year"), part("month") - 1, part("day"), part("hour"), part("minute"));
}

function wallMs(dateValue: string, timeValue: string): number | null {
  const date = parseDate(dateValue);
  const time = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(timeValue);
  if (!date || !time) {
    return null;
  }
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate(), Number(time[1]), Number(time[2]));
}

/**
 * Resolves a local date and clock time in timeZone the way the server's
 * schedule windows do: a time skipped by a spring-forward gap runs at the same
 * instant as under the offset before the gap, and a time repeated by a
 * fall-back transition resolves to its first occurrence.
 */
export function resolveScheduleTime(dateValue: string, timeValue: string, timeZone: string): number | null {
  const wall = wallMs(dateValue, timeValue);
  if (wall === null) {
    return null;
  }
  const wallBefore = wallClockMs(wall - dayMs, timeZone);
  const wallAfter = wallClockMs(wall + dayMs, timeZone);
  if (wallBefore === null || wallAfter === null) {
    return null;
  }
  const offsetBefore = wallBefore - (wall - dayMs);
  const offsetAfter = wallAfter - (wall + dayMs);
  const candidates = [wall - offsetBefore, wall - offsetAfter].sort((left, right) => left - right);
  const match = candidates.find((candidate) => wallClockMs(candidate, timeZone) === wall);
  return match ?? wall - offsetBefore;
}

function formatDayValue(dayUtcMs: number): string {
  return new Date(dayUtcMs).toISOString().slice(0, 10);
}

function occursOn(schedule: CurtailmentScheduleDefinition, dayUtcMs: number): boolean {
  const day = new Date(dayUtcMs);
  switch (schedule.frequency) {
    case "weekly":
      // CurtailmentScheduleDayOfWeek numbers SUNDAY = 1 through SATURDAY = 7.
      return schedule.daysOfWeek.includes((day.getUTCDay() + 1) as CurtailmentScheduleDayOfWeek);
    case "monthly":
      return day.getUTCDate() === Number(schedule.dayOfMonth);
    default:
      return true;
  }
}

function oneTimeWindowTiming(schedule: CurtailmentScheduleDefinition): ScheduleWindowTiming | null {
  const startWall = wallMs(schedule.startDate, schedule.startTime);
  const endWall = wallMs(schedule.endDate, schedule.endTime);
  const start = resolveScheduleTime(schedule.startDate, schedule.startTime, schedule.timezone);
  const end = resolveScheduleTime(schedule.endDate, schedule.endTime, schedule.timezone);
  if (
    startWall === null ||
    endWall === null ||
    start === null ||
    end === null ||
    endWall <= startWall ||
    end <= start
  ) {
    return null;
  }
  const clockDurationMs = endWall - startWall;
  const durationMs = end - start;
  return {
    clockDurationMs,
    clockChange: durationMs === clockDurationMs ? null : { startDate: schedule.startDate, durationMs },
  };
}

function recurringWindowTiming(schedule: CurtailmentScheduleDefinition, now: Date): ScheduleWindowTiming | null {
  const startWall = wallMs(schedule.startDate, schedule.startTime);
  const sameDayEndWall = wallMs(schedule.startDate, schedule.endTime);
  const today = wallClockMs(now.getTime(), schedule.timezone);
  if (startWall === null || sameDayEndWall === null || today === null || sameDayEndWall === startWall) {
    return null;
  }
  const endsNextDay = sameDayEndWall < startWall;
  const clockDurationMs = sameDayEndWall - startWall + (endsNextDay ? dayMs : 0);
  const timing: ScheduleWindowTiming = { clockDurationMs, clockChange: null };

  const startDay = startWall - (startWall % dayMs);
  const firstDay = Math.max(startDay, today - (today % dayMs) - dayMs);
  const untilWall = schedule.untilDate ? wallMs(schedule.untilDate, "00:00") : null;
  const lastDay = Math.min(untilWall ?? Number.POSITIVE_INFINITY, firstDay + recurringLookaheadDays * dayMs);
  for (let day = firstDay; day <= lastDay; day += dayMs) {
    if (!occursOn(schedule, day)) {
      continue;
    }
    const startDate = formatDayValue(day);
    const start = resolveScheduleTime(startDate, schedule.startTime, schedule.timezone);
    const end = resolveScheduleTime(
      formatDayValue(endsNextDay ? day + dayMs : day),
      schedule.endTime,
      schedule.timezone,
    );
    if (start === null || end === null || end <= start || end <= now.getTime()) {
      continue;
    }
    if (end - start !== clockDurationMs) {
      timing.clockChange = { startDate, durationMs: end - start };
      break;
    }
  }
  return timing;
}

/**
 * Describes how long a schedule's windows last. A one-time window's actual
 * length is its clock duration adjusted by any clockChange; a recurring
 * schedule reports the next window, within a year, that a clock change affects.
 */
export function getScheduleWindowTiming(
  schedule: CurtailmentScheduleDefinition,
  now = new Date(),
): ScheduleWindowTiming | null {
  return schedule.scheduleType === "oneTime" ? oneTimeWindowTiming(schedule) : recurringWindowTiming(schedule, now);
}
