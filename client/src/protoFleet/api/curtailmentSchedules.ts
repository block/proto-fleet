import type { MessageInitShape } from "@bufbuild/protobuf";

import type { AutomationRule, CurtailmentScheduleDefinition } from "@/protoFleet/api/curtailmentTypes";
import {
  type CurtailmentSchedule as ApiCurtailmentSchedule,
  CurtailmentScheduleDayOfWeek,
  CurtailmentScheduleFrequency,
  type CurtailmentScheduleSchema,
  CurtailmentScheduleType,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { parseDate } from "@/protoFleet/features/settings/utils/scheduleDateUtils";

const weekdayShortLabels: Record<CurtailmentScheduleDayOfWeek, string> = {
  [CurtailmentScheduleDayOfWeek.UNSPECIFIED]: "",
  [CurtailmentScheduleDayOfWeek.SUNDAY]: "Sun",
  [CurtailmentScheduleDayOfWeek.MONDAY]: "Mon",
  [CurtailmentScheduleDayOfWeek.TUESDAY]: "Tue",
  [CurtailmentScheduleDayOfWeek.WEDNESDAY]: "Wed",
  [CurtailmentScheduleDayOfWeek.THURSDAY]: "Thu",
  [CurtailmentScheduleDayOfWeek.FRIDAY]: "Fri",
  [CurtailmentScheduleDayOfWeek.SATURDAY]: "Sat",
};

function frequencyFromApi(frequency: CurtailmentScheduleFrequency): CurtailmentScheduleDefinition["frequency"] {
  switch (frequency) {
    case CurtailmentScheduleFrequency.WEEKLY:
      return "weekly";
    case CurtailmentScheduleFrequency.MONTHLY:
      return "monthly";
    case CurtailmentScheduleFrequency.DAILY:
    case CurtailmentScheduleFrequency.UNSPECIFIED:
    default:
      return "daily";
  }
}

function frequencyToApi(frequency: CurtailmentScheduleDefinition["frequency"]): CurtailmentScheduleFrequency {
  switch (frequency) {
    case "weekly":
      return CurtailmentScheduleFrequency.WEEKLY;
    case "monthly":
      return CurtailmentScheduleFrequency.MONTHLY;
    case "daily":
    default:
      return CurtailmentScheduleFrequency.DAILY;
  }
}

export function curtailmentScheduleFromApi(schedule: ApiCurtailmentSchedule): CurtailmentScheduleDefinition {
  const recurring = schedule.scheduleType === CurtailmentScheduleType.RECURRING;
  return {
    scheduleType: recurring ? "recurring" : "oneTime",
    timezone: schedule.timezone,
    startDate: schedule.startDate,
    startTime: schedule.startTime,
    endTime: schedule.endTime,
    endDate: schedule.endDate,
    frequency: frequencyFromApi(schedule.frequency),
    daysOfWeek: schedule.daysOfWeek.filter((day) => day !== CurtailmentScheduleDayOfWeek.UNSPECIFIED),
    dayOfMonth: schedule.dayOfMonth > 0 ? String(schedule.dayOfMonth) : "",
    untilDate: schedule.untilDate,
  };
}

export function curtailmentScheduleToApi(
  schedule: CurtailmentScheduleDefinition,
): MessageInitShape<typeof CurtailmentScheduleSchema> {
  const base = {
    timezone: schedule.timezone,
    startDate: schedule.startDate,
    startTime: schedule.startTime,
    endTime: schedule.endTime,
  };
  if (schedule.scheduleType === "oneTime") {
    return { ...base, scheduleType: CurtailmentScheduleType.ONE_TIME, endDate: schedule.endDate };
  }
  return {
    ...base,
    scheduleType: CurtailmentScheduleType.RECURRING,
    frequency: frequencyToApi(schedule.frequency),
    daysOfWeek: schedule.frequency === "weekly" ? [...schedule.daysOfWeek].sort((left, right) => left - right) : [],
    dayOfMonth: schedule.frequency === "monthly" ? Number(schedule.dayOfMonth) : 0,
    untilDate: schedule.untilDate,
  };
}

export function formatScheduleClockTime(value: string): string {
  const match = /^(\d{2}):(\d{2})$/.exec(value);
  if (!match) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(
    new Date(2026, 0, 1, Number(match[1]), Number(match[2])),
  );
}

export function formatScheduleDate(value: string): string {
  const date = parseDate(value);
  if (!date) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" }).format(date);
}

export function describeCurtailmentSchedule(schedule: CurtailmentScheduleDefinition): string {
  const start = formatScheduleClockTime(schedule.startTime);
  const end = formatScheduleClockTime(schedule.endTime);

  if (schedule.scheduleType === "oneTime") {
    const startDate = formatScheduleDate(schedule.startDate);
    if (schedule.endDate === schedule.startDate) {
      return `${startDate}, ${start} – ${end}`;
    }
    return `${startDate}, ${start} – ${formatScheduleDate(schedule.endDate)}, ${end}`;
  }

  const crossesMidnight = schedule.endTime <= schedule.startTime;
  const window = `${start} – ${end}${crossesMidnight ? " next day" : ""}`;
  let cadence = "Daily";
  if (schedule.frequency === "weekly") {
    const days = [...schedule.daysOfWeek]
      .sort((left, right) => left - right)
      .map((day) => weekdayShortLabels[day])
      .filter(Boolean);
    cadence = days.length === 7 ? "Daily" : `Weekly on ${days.join(", ")}`;
  } else if (schedule.frequency === "monthly") {
    cadence = `Monthly on day ${schedule.dayOfMonth}`;
  }
  const until = schedule.untilDate ? `, until ${formatScheduleDate(schedule.untilDate)}` : "";
  return `${cadence}, ${window}${until}`;
}

function windowFormatter(timeZone: string | undefined): Intl.DateTimeFormat {
  const options: Intl.DateTimeFormatOptions = { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" };
  try {
    return new Intl.DateTimeFormat(undefined, { ...options, timeZone: timeZone || undefined });
  } catch {
    return new Intl.DateTimeFormat(undefined, options);
  }
}

// Windows are shown in the schedule's timezone, matching its clock times.
export function describeNextScheduleWindow(rule: AutomationRule, now = new Date()): string {
  if (!rule.windowStartAt || !rule.windowEndAt) {
    return "No upcoming windows";
  }
  const formatter = windowFormatter(rule.schedule?.timezone);
  if (rule.windowStartAt <= now && now < rule.windowEndAt) {
    return `Active until ${formatter.format(rule.windowEndAt)}`;
  }
  return formatter.format(rule.windowStartAt);
}
