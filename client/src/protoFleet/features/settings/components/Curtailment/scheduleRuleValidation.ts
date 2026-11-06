import type { ResponseProfile, ScheduleRuleFormValues } from "@/protoFleet/api/curtailmentTypes";
import { resolveScheduleTime } from "@/protoFleet/features/settings/components/Curtailment/scheduleWindowTiming";
import {
  formatDateParts,
  getTimeZoneDateTimeParts,
  parseDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

const maxOneTimeDurationMs = 7 * 24 * 60 * 60 * 1000;

export type ScheduleRuleFormErrors = Partial<
  Record<
    | "name"
    | "responseProfileId"
    | "startDate"
    | "startTime"
    | "endDate"
    | "endTime"
    | "daysOfWeek"
    | "dayOfMonth"
    | "untilDate",
    string
  >
>;

const resolvedTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

export function getDefaultScheduleRuleFormValues(
  responseProfiles: ResponseProfile[],
  now = new Date(),
  timeZone = resolvedTimeZone(),
): ScheduleRuleFormValues {
  const responseProfile = responseProfiles[0];
  const parts = getTimeZoneDateTimeParts(now, timeZone);
  const today = parts ? formatDateParts(parts) : "";
  return {
    name: "",
    responseProfileId: responseProfile?.id ?? "",
    responseProfileRevision: responseProfile?.revision ?? "",
    schedule: {
      scheduleType: "recurring",
      timezone: timeZone,
      startDate: today,
      startTime: "17:00",
      endTime: "21:00",
      endDate: today,
      frequency: "daily",
      daysOfWeek: [],
      dayOfMonth: parts ? String(parts.day) : "1",
      untilDate: "",
    },
  };
}

export function validateScheduleRuleFormValues(
  values: ScheduleRuleFormValues,
  now = new Date(),
): ScheduleRuleFormErrors {
  const errors: ScheduleRuleFormErrors = {};
  const { schedule } = values;

  if (values.name.trim() === "") {
    errors.name = "Enter a schedule name.";
  }
  if (values.responseProfileId === "") {
    errors.responseProfileId = "Select a response profile.";
  } else if (values.responseProfileRevision === "") {
    errors.responseProfileId = "Reload response profiles before saving.";
  }
  if (!parseDate(schedule.startDate)) {
    errors.startDate = "Select a start date.";
  }
  if (schedule.startTime === "") {
    errors.startTime = "Enter a start time.";
  }
  if (schedule.endTime === "") {
    errors.endTime = "Enter an end time.";
  }

  if (schedule.scheduleType === "oneTime") {
    if (!parseDate(schedule.endDate)) {
      errors.endDate = "Select an end date.";
      return errors;
    }
    const start = resolveScheduleTime(schedule.startDate, schedule.startTime, schedule.timezone);
    const end = resolveScheduleTime(schedule.endDate, schedule.endTime, schedule.timezone);
    if (start === null || end === null) {
      return errors;
    }
    if (end <= start) {
      errors.endTime = "End must be after start.";
    } else if (end - start > maxOneTimeDurationMs) {
      errors.endDate = "One-time schedules can last up to 7 days.";
    } else if (end <= now.getTime()) {
      errors.endTime = "End must be in the future.";
    }
    return errors;
  }

  if (schedule.startTime !== "" && schedule.startTime === schedule.endTime) {
    errors.endTime = "End time must differ from start time.";
  }
  if (schedule.frequency === "weekly" && schedule.daysOfWeek.length === 0) {
    errors.daysOfWeek = "Select at least one day.";
  }
  if (schedule.frequency === "monthly") {
    const dayOfMonth = Number(schedule.dayOfMonth);
    if (!Number.isInteger(dayOfMonth) || dayOfMonth < 1 || dayOfMonth > 31) {
      errors.dayOfMonth = "Enter a day from 1 to 31.";
    }
  }
  if (schedule.untilDate !== "") {
    if (!parseDate(schedule.untilDate)) {
      errors.untilDate = "Select a valid date.";
    } else if (schedule.untilDate < schedule.startDate) {
      errors.untilDate = "Repeat until must be on or after the start date.";
    } else if (schedule.untilDate < formatTodayIn(schedule.timezone, now)) {
      errors.untilDate = "Repeat until must be today or later.";
    }
  }
  return errors;
}

function formatTodayIn(timeZone: string, now: Date): string {
  const parts = getTimeZoneDateTimeParts(now, timeZone);
  return parts ? formatDateParts(parts) : "";
}
