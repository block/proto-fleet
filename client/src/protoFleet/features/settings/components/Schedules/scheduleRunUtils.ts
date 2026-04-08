import { DayOfWeek } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleFormValues } from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import {
  addDaysToDateValue,
  buildDateInTimeZone,
  formatDateParts,
  parseDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

export type ScheduleRun = {
  start: Date;
  end?: Date;
};

const toScheduleWeekday = (date: Date) => {
  const value = date.getDay() + 1;
  return value as DayOfWeek;
};

const matchesRecurringDate = (date: Date, startDate: Date, values: ScheduleFormValues) => {
  if (date.getTime() < startDate.getTime()) {
    return false;
  }

  if (values.frequency === "daily") {
    return true;
  }

  if (values.frequency === "weekly") {
    return values.daysOfWeek.includes(toScheduleWeekday(date));
  }

  return date.getDate() === (Number(values.dayOfMonth) || 1);
};

const buildScheduleRun = (values: ScheduleFormValues, dateValue: string): ScheduleRun | null => {
  const start = buildDateInTimeZone(dateValue, values.startTime, values.timezone);

  if (!start) {
    return null;
  }

  if (values.scheduleType !== "recurring" || values.action !== "setPowerTarget") {
    return { start };
  }

  const endDateValue = values.endTime < values.startTime ? addDaysToDateValue(dateValue, 1) : dateValue;
  const end = buildDateInTimeZone(endDateValue, values.endTime, values.timezone);

  if (!end) {
    return null;
  }

  return { start, end };
};

export const getFutureScheduleRuns = (values: ScheduleFormValues, now = new Date(), count = 5) => {
  if (values.scheduleType === "oneTime") {
    const run = buildScheduleRun(values, values.startDate);

    if (!run || run.start.getTime() <= now.getTime()) {
      return [];
    }

    return [run];
  }

  const startDate = parseDate(values.startDate);
  if (!startDate) {
    return [];
  }

  // Recurrence matching walks YYYY-MM-DD calendar dates; the actual runtime
  // instant is built separately in the schedule timezone below.
  if (!buildScheduleRun(values, values.startDate)) {
    return [];
  }

  const endDate = values.endBehavior === "endDate" ? parseDate(values.endDate) : null;
  const cursor = new Date(startDate);
  const runs: ScheduleRun[] = [];
  let iterations = 0;

  while (runs.length < count && iterations < 3660) {
    if (endDate && cursor.getTime() > endDate.getTime()) {
      break;
    }

    if (matchesRecurringDate(cursor, startDate, values)) {
      const dateValue = formatDateParts({
        year: cursor.getFullYear(),
        month: cursor.getMonth() + 1,
        day: cursor.getDate(),
      });
      const run = buildScheduleRun(values, dateValue);

      if (run && run.start.getTime() > now.getTime()) {
        runs.push(run);
      }
    }

    cursor.setDate(cursor.getDate() + 1);
    iterations += 1;
  }

  return runs;
};

export const hasFutureScheduleRun = (values: ScheduleFormValues, now = new Date()) =>
  getFutureScheduleRuns(values, now, 1).length > 0;
