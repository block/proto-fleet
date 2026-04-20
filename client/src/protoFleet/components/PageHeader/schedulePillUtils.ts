import { PowerTargetMode, ScheduleType } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import { scheduleActionLabels } from "@/protoFleet/features/settings/components/Schedules/constants";
import {
  addDaysToDateValue,
  buildDateInTimeZone,
  formatTimeZoneDateParts,
  getTimeZoneDateTimeParts,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";

const MINUTE_IN_MS = 60_000;
const HOUR_IN_MINUTES = 60;
const DAY_IN_MINUTES = 24 * HOUR_IN_MINUTES;
const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
});
const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
  hour: "numeric",
  minute: "2-digit",
});
const nextRunDateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

const schedulePopoverSectionConfigs = [
  {
    id: "running",
    title: "Active now",
    status: "running",
  },
  {
    id: "active",
    title: "Up next",
    status: "active",
  },
  {
    id: "paused",
    title: "Paused",
    status: "paused",
  },
] as const satisfies readonly {
  id: string;
  title: string;
  status: ScheduleListItem["status"];
}[];

export type SchedulePopoverSectionId = (typeof schedulePopoverSectionConfigs)[number]["id"];

export interface SchedulePopoverSection {
  id: SchedulePopoverSectionId;
  title: string;
  schedules: ScheduleListItem[];
}

const MAX_POPOVER_SCHEDULES = 3;

const sortByPriority = (schedules: ScheduleListItem[]) =>
  [...schedules].sort((left, right) => left.priority - right.priority);

const toDate = (seconds: bigint, nanos = 0) => new Date(Number(seconds) * 1000 + Math.floor(nanos / 1_000_000));

const shouldUseNextRunDate = (sectionId: SchedulePopoverSectionId) => sectionId === "active" || sectionId === "paused";

const formatActionTimeWindow = (schedule: ScheduleListItem, dateValue: string, startSummary: string) => {
  if (schedule.action !== "setPowerTarget" || !schedule.rawSchedule.endTime) {
    return null;
  }

  const endDateValue =
    schedule.rawSchedule.endTime < schedule.rawSchedule.startTime ? addDaysToDateValue(dateValue, 1) : dateValue;
  const end = buildDateInTimeZone(endDateValue, schedule.rawSchedule.endTime, schedule.rawSchedule.timezone);

  if (!end) {
    return null;
  }

  return `${scheduleActionLabels[schedule.action]} · ${startSummary} – ${timeFormatter.format(end)}`;
};

export const formatSchedulePopoverRelativeStart = (schedule: ScheduleListItem) => {
  const nextRunAt = schedule.rawSchedule.nextRunAt;

  if (!nextRunAt) {
    return schedule.nextRunSummary ?? "Starting soon";
  }

  const nextRun = toDate(nextRunAt.seconds, nextRunAt.nanos);
  const diffMinutes = Math.max(0, Math.floor((nextRun.getTime() - Date.now()) / MINUTE_IN_MS));

  if (diffMinutes <= 0) {
    return "Starting soon";
  }

  const days = Math.floor(diffMinutes / DAY_IN_MINUTES);
  const hours = Math.floor((diffMinutes % DAY_IN_MINUTES) / HOUR_IN_MINUTES);
  const minutes = diffMinutes % HOUR_IN_MINUTES;
  const parts: string[] = [];

  if (days > 0) {
    parts.push(`${days}d`);
  }

  if (hours > 0 && parts.length < 2) {
    parts.push(`${hours}h`);
  }

  if (minutes > 0 && parts.length < 2) {
    parts.push(`${minutes}m`);
  }

  return `Starting in ${parts.join(" ")}`;
};

export const getSchedulePopoverActionSummary = (sectionId: SchedulePopoverSectionId, schedule: ScheduleListItem) => {
  const { rawSchedule } = schedule;
  const referenceDateValue = rawSchedule.startDate;
  const start = buildDateInTimeZone(referenceDateValue, rawSchedule.startTime, rawSchedule.timezone);

  if (!start) {
    return scheduleActionLabels[schedule.action];
  }

  if (shouldUseNextRunDate(sectionId) && rawSchedule.nextRunAt) {
    const nextRun = toDate(rawSchedule.nextRunAt.seconds, rawSchedule.nextRunAt.nanos);
    const nextRunParts = getTimeZoneDateTimeParts(nextRun, rawSchedule.timezone);

    if (nextRunParts) {
      const nextRunDateValue = formatTimeZoneDateParts(nextRunParts);
      const timeWindowSummary = formatActionTimeWindow(
        schedule,
        nextRunDateValue,
        nextRunDateTimeFormatter.format(nextRun),
      );

      if (timeWindowSummary) {
        return timeWindowSummary;
      }
    }

    return `${scheduleActionLabels[schedule.action]} · ${nextRunDateTimeFormatter.format(nextRun)}`;
  }

  if (rawSchedule.scheduleType === ScheduleType.ONE_TIME) {
    return `${scheduleActionLabels[schedule.action]} · ${dateTimeFormatter.format(start)}`;
  }

  return (
    formatActionTimeWindow(schedule, referenceDateValue, timeFormatter.format(start)) ??
    `${scheduleActionLabels[schedule.action]} · ${timeFormatter.format(start)}`
  );
};

export const getSchedulePopoverPowerTargetDetail = (schedule: ScheduleListItem) => {
  if (schedule.action !== "setPowerTarget") {
    return null;
  }

  switch (schedule.rawSchedule.actionConfig?.mode) {
    case PowerTargetMode.MAX:
      return "Max";
    case PowerTargetMode.DEFAULT:
      return "Default";
    default:
      return null;
  }
};

export const getSchedulePopoverTargetSummary = (schedule: ScheduleListItem) => schedule.targetSummary;

export const buildSchedulePopoverSections = (schedules: ScheduleListItem[]): SchedulePopoverSection[] => {
  let remainingSlots = MAX_POPOVER_SCHEDULES;

  return schedulePopoverSectionConfigs.reduce<SchedulePopoverSection[]>((result, sectionConfig) => {
    if (remainingSlots <= 0) {
      return result;
    }

    const sectionSchedules = sortByPriority(schedules.filter((schedule) => schedule.status === sectionConfig.status));

    if (sectionSchedules.length === 0) {
      return result;
    }

    const visibleSchedules = sectionSchedules.slice(0, remainingSlots);
    remainingSlots -= visibleSchedules.length;

    result.push({
      id: sectionConfig.id,
      title: sectionConfig.title,
      schedules: visibleSchedules,
    });

    return result;
  }, []);
};

export const selectPillSchedule = (sections: SchedulePopoverSection[]) => sections[0]?.schedules[0] ?? null;
