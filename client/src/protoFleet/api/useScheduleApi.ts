import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { authClient, scheduleClient } from "@/protoFleet/api/clients";
import {
  type CreateScheduleRequest,
  DayOfWeek,
  DeleteScheduleRequestSchema,
  ListSchedulesRequestSchema,
  PauseScheduleRequestSchema,
  ScheduleAction as ProtoScheduleAction,
  ScheduleStatus as ProtoScheduleStatus,
  ScheduleType as ProtoScheduleType,
  RecurrenceFrequency,
  type ReorderSchedulesRequest,
  ReorderSchedulesRequestSchema,
  ResumeScheduleRequestSchema,
  type Schedule,
  ScheduleTargetType,
  type UpdateScheduleRequest,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { useAuthErrors } from "@/protoFleet/store";

export type ScheduleAction = "setPowerTarget" | "reboot" | "sleep";
export type ScheduleStatus = "running" | "active" | "paused" | "completed";

export interface ScheduleListItem {
  id: string;
  priority: number;
  name: string;
  targetSummary: string;
  scheduleSummary: string;
  nextRunSummary: string | null;
  action: ScheduleAction;
  status: ScheduleStatus;
  createdBy: string;
}

const dayFormatter = new Intl.DateTimeFormat(undefined, { weekday: "short" });
const dateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  year: "numeric",
  hour: "numeric",
  minute: "2-digit",
});
const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
});

const normalizeSchedules = (schedules: ScheduleListItem[]): ScheduleListItem[] =>
  [...schedules]
    .sort((left, right) => left.priority - right.priority)
    .map((schedule, index) => ({
      ...schedule,
      priority: index + 1,
    }));

const resequenceSchedules = (schedules: ScheduleListItem[]): ScheduleListItem[] =>
  schedules.map((schedule, index) => ({
    ...schedule,
    priority: index + 1,
  }));

const ensureError = (error: unknown, fallbackMessage: string) =>
  error instanceof Error ? error : new Error(typeof error === "string" ? error : fallbackMessage);

const parseDateParts = (value: string) => {
  const [year, month, day] = value.split("-").map(Number);

  if (!year || !month || !day) {
    return null;
  }

  return { year, month, day };
};

const parseTimeParts = (value: string) => {
  const [hours, minutes] = value.split(":").map(Number);

  if (Number.isNaN(hours) || Number.isNaN(minutes)) {
    return null;
  }

  return { hours, minutes };
};

const parseDate = (value: string) => {
  const parts = parseDateParts(value);

  if (!parts) {
    return null;
  }

  return new Date(parts.year, parts.month - 1, parts.day);
};

const parseTime = (value: string) => {
  const parts = parseTimeParts(value);

  if (!parts) {
    return null;
  }

  const date = new Date();
  date.setHours(parts.hours, parts.minutes, 0, 0);
  return date;
};

const parseDateTime = (dateValue: string, timeValue: string) => {
  const date = parseDate(dateValue);
  const time = parseTime(timeValue);

  if (!date || !time) {
    return null;
  }

  date.setHours(time.getHours(), time.getMinutes(), 0, 0);
  return date;
};

const toDate = (seconds: bigint, nanos = 0) => new Date(Number(seconds) * 1000 + Math.floor(nanos / 1_000_000));

const formatDateParts = (parts: { year: number; month: number; day: number }) =>
  `${parts.year}-${String(parts.month).padStart(2, "0")}-${String(parts.day).padStart(2, "0")}`;

const formatTimeZoneDateParts = (parts: { year: number; month: number; day: number }) =>
  formatDateParts({
    year: parts.year,
    month: parts.month,
    day: parts.day,
  });

const getTimeZoneDateTimeParts = (date: Date, timeZone: string) => {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    }).formatToParts(date);
    const getNumericPart = (type: Intl.DateTimeFormatPartTypes) =>
      Number(parts.find((part) => part.type === type)?.value);

    return {
      year: getNumericPart("year"),
      month: getNumericPart("month"),
      day: getNumericPart("day"),
      hours: getNumericPart("hour"),
      minutes: getNumericPart("minute"),
    };
  } catch {
    return null;
  }
};

const addDaysToDateValue = (dateValue: string, days: number) => {
  const parsed = parseDate(dateValue);

  if (!parsed) {
    return dateValue;
  }

  parsed.setDate(parsed.getDate() + days);

  return formatDateParts({
    year: parsed.getFullYear(),
    month: parsed.getMonth() + 1,
    day: parsed.getDate(),
  });
};

const buildDateInTimeZone = (dateValue: string, timeValue: string, timeZone: string) => {
  const dateParts = parseDateParts(dateValue);
  const timeParts = parseTimeParts(timeValue);

  if (!dateParts || !timeParts) {
    return null;
  }

  const desiredUtcTime = Date.UTC(
    dateParts.year,
    dateParts.month - 1,
    dateParts.day,
    timeParts.hours,
    timeParts.minutes,
  );
  let candidate = new Date(desiredUtcTime);

  for (let attempt = 0; attempt < 2; attempt += 1) {
    const candidateParts = getTimeZoneDateTimeParts(candidate, timeZone);

    if (!candidateParts) {
      return null;
    }

    const candidateUtcTime = Date.UTC(
      candidateParts.year,
      candidateParts.month - 1,
      candidateParts.day,
      candidateParts.hours,
      candidateParts.minutes,
    );
    const delta = candidateUtcTime - desiredUtcTime;

    if (delta === 0) {
      return candidate;
    }

    candidate = new Date(candidate.getTime() - delta);
  }

  return candidate;
};

const formatTimeValue = (value: string, timeZone: string, dateValue: string) => {
  const parsed = buildDateInTimeZone(dateValue, value, timeZone) ?? parseTime(value);
  return parsed ? timeFormatter.format(parsed) : value;
};

const formatDateTimeValue = (dateValue: string, timeValue: string, timeZone: string) => {
  const parsed = buildDateInTimeZone(dateValue, timeValue, timeZone) ?? parseDateTime(dateValue, timeValue);
  return parsed ? dateTimeFormatter.format(parsed) : `${dateValue} at ${timeValue}`;
};

const formatOrdinal = (value: number) => {
  const suffix =
    value % 10 === 1 && value % 100 !== 11
      ? "st"
      : value % 10 === 2 && value % 100 !== 12
        ? "nd"
        : value % 10 === 3 && value % 100 !== 13
          ? "rd"
          : "th";
  return `${value}${suffix}`;
};

const weekdayNames: Record<DayOfWeek, string> = {
  [DayOfWeek.UNSPECIFIED]: "",
  [DayOfWeek.SUNDAY]: "Sun",
  [DayOfWeek.MONDAY]: "Mon",
  [DayOfWeek.TUESDAY]: "Tue",
  [DayOfWeek.WEDNESDAY]: "Wed",
  [DayOfWeek.THURSDAY]: "Thu",
  [DayOfWeek.FRIDAY]: "Fri",
  [DayOfWeek.SATURDAY]: "Sat",
};

const mapStatus = (status: ProtoScheduleStatus): ScheduleStatus => {
  switch (status) {
    case ProtoScheduleStatus.RUNNING:
      return "running";
    case ProtoScheduleStatus.PAUSED:
      return "paused";
    case ProtoScheduleStatus.COMPLETED:
      return "completed";
    case ProtoScheduleStatus.ACTIVE:
    case ProtoScheduleStatus.UNSPECIFIED:
    default:
      return "active";
  }
};

const mapAction = (schedule: Schedule): ScheduleAction => {
  switch (schedule.action) {
    case ProtoScheduleAction.REBOOT:
      return "reboot";
    case ProtoScheduleAction.SLEEP:
      return "sleep";
    case ProtoScheduleAction.SET_POWER_TARGET:
    case ProtoScheduleAction.UNSPECIFIED:
    default:
      return "setPowerTarget";
  }
};

const summarizeTargets = (schedule: Schedule) => {
  if (schedule.targets.length === 0) {
    return "Applies to all miners";
  }

  const rackCount = schedule.targets.filter((target) => target.targetType === ScheduleTargetType.RACK).length;
  const minerCount = schedule.targets.filter((target) => target.targetType === ScheduleTargetType.MINER).length;
  const parts = [
    rackCount > 0 ? `${rackCount} ${rackCount === 1 ? "rack" : "racks"}` : null,
    minerCount > 0 ? `${minerCount} ${minerCount === 1 ? "miner" : "miners"}` : null,
  ].filter(Boolean);

  return `Applies to ${parts.join(" and ")}`;
};

const summarizeWeeklyRecurrence = (daysOfWeek: DayOfWeek[]) => {
  const uniqueDays = Array.from(new Set(daysOfWeek)).sort((left, right) => left - right);

  if (uniqueDays.length === 7) {
    return "Every day";
  }

  const weekdaySet = new Set([
    DayOfWeek.MONDAY,
    DayOfWeek.TUESDAY,
    DayOfWeek.WEDNESDAY,
    DayOfWeek.THURSDAY,
    DayOfWeek.FRIDAY,
  ]);
  const weekendSet = new Set([DayOfWeek.SATURDAY, DayOfWeek.SUNDAY]);

  if (uniqueDays.length === weekdaySet.size && uniqueDays.every((day) => weekdaySet.has(day))) {
    return "Weekdays";
  }

  if (uniqueDays.length === weekendSet.size && uniqueDays.every((day) => weekendSet.has(day))) {
    return "Weekends";
  }

  return uniqueDays
    .map((day) => weekdayNames[day])
    .filter(Boolean)
    .join(", ");
};

const summarizeWeeklyPattern = (recurrence: NonNullable<Schedule["recurrence"]>) => {
  const weeklyPattern = summarizeWeeklyRecurrence(recurrence.daysOfWeek);
  return recurrence.interval <= 1 ? weeklyPattern : `Every ${recurrence.interval} weeks · ${weeklyPattern}`;
};

const summarizeMonthlyPattern = (recurrence: NonNullable<Schedule["recurrence"]>) => {
  const monthlyPattern = recurrence.dayOfMonth ? `${formatOrdinal(recurrence.dayOfMonth)} day of month` : "Every month";

  if (recurrence.interval <= 1) {
    return monthlyPattern;
  }

  if (monthlyPattern === "Every month") {
    return `Every ${recurrence.interval} months`;
  }

  return `Every ${recurrence.interval} months on ${monthlyPattern}`;
};

const summarizeRecurringPattern = (schedule: Schedule) => {
  const recurrence = schedule.recurrence;

  if (!recurrence) {
    return "Recurring";
  }

  switch (recurrence.frequency) {
    case RecurrenceFrequency.DAILY:
      return recurrence.interval <= 1 ? "Every day" : `Every ${recurrence.interval} days`;
    case RecurrenceFrequency.WEEKLY:
      return summarizeWeeklyPattern(recurrence);
    case RecurrenceFrequency.MONTHLY:
      return summarizeMonthlyPattern(recurrence);
    case RecurrenceFrequency.UNSPECIFIED:
    default:
      return "Recurring";
  }
};

const getReferenceDateValue = (schedule: Schedule) => {
  if (!schedule.nextRunAt) {
    if (schedule.scheduleType === ProtoScheduleType.RECURRING) {
      const currentDateParts = getTimeZoneDateTimeParts(new Date(), schedule.timezone);

      if (currentDateParts) {
        return formatTimeZoneDateParts(currentDateParts);
      }
    }

    return schedule.startDate;
  }

  const nextRunParts = getTimeZoneDateTimeParts(
    toDate(schedule.nextRunAt.seconds, schedule.nextRunAt.nanos),
    schedule.timezone,
  );

  return nextRunParts ? formatTimeZoneDateParts(nextRunParts) : schedule.startDate;
};

const summarizeTimeWindow = (schedule: Schedule) => {
  const referenceDateValue = getReferenceDateValue(schedule);
  const startTime = formatTimeValue(schedule.startTime, schedule.timezone, referenceDateValue);

  if (schedule.action !== ProtoScheduleAction.SET_POWER_TARGET || !schedule.endTime) {
    return startTime;
  }

  const endDateValue =
    schedule.endTime < schedule.startTime ? addDaysToDateValue(referenceDateValue, 1) : referenceDateValue;

  return `${startTime} – ${formatTimeValue(schedule.endTime, schedule.timezone, endDateValue)}`;
};

const summarizeSchedule = (schedule: Schedule) => {
  if (schedule.scheduleType === ProtoScheduleType.ONE_TIME) {
    if (schedule.nextRunAt) {
      return dateTimeFormatter.format(toDate(schedule.nextRunAt.seconds, schedule.nextRunAt.nanos));
    }

    return formatDateTimeValue(schedule.startDate, schedule.startTime, schedule.timezone);
  }

  return `${summarizeRecurringPattern(schedule)} · ${summarizeTimeWindow(schedule)}`;
};

const summarizeNextRun = (schedule: Schedule) => {
  if (!schedule.nextRunAt) {
    return null;
  }

  const nextRun = toDate(schedule.nextRunAt.seconds, schedule.nextRunAt.nanos);
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const nextRunDay = new Date(nextRun.getFullYear(), nextRun.getMonth(), nextRun.getDate());
  const dayDifference = Math.round((nextRunDay.getTime() - today.getTime()) / (24 * 60 * 60 * 1000));

  if (dayDifference === 0) {
    return `Runs today at ${timeFormatter.format(nextRun)}`;
  }

  if (dayDifference === 1) {
    return `Runs tomorrow at ${timeFormatter.format(nextRun)}`;
  }

  if (dayDifference > 1 && dayDifference < 7) {
    return `Runs ${dayFormatter.format(nextRun)} at ${timeFormatter.format(nextRun)}`;
  }

  return `Runs on ${dateTimeFormatter.format(nextRun)}`;
};

const summarizeCreatedBy = (createdBy: bigint, usernamesById: Record<string, string>) =>
  usernamesById[createdBy.toString()] ?? createdBy.toString();

const mapSchedule = (schedule: Schedule, usernamesById: Record<string, string>): ScheduleListItem => ({
  id: schedule.id.toString(),
  priority: schedule.priority,
  name: schedule.name,
  targetSummary: summarizeTargets(schedule),
  scheduleSummary: summarizeSchedule(schedule),
  nextRunSummary: summarizeNextRun(schedule),
  action: mapAction(schedule),
  status: mapStatus(schedule.status),
  createdBy: summarizeCreatedBy(schedule.createdBy, usernamesById),
});

const updateMappedSchedule = (
  schedules: ScheduleListItem[],
  schedule: Schedule,
  usernamesById: Record<string, string>,
) =>
  normalizeSchedules(
    schedules.map((current) =>
      current.id === schedule.id.toString() ? mapSchedule(schedule, usernamesById) : current,
    ),
  );

export const useScheduleApi = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [usernamesById, setUsernamesById] = useState<Record<string, string>>({});
  const [schedules, setSchedules] = useState<ScheduleListItem[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const fetchUsernames = useCallback(async () => {
    try {
      const response = await authClient.listUsers({});

      return response.users.reduce<Record<string, string>>((result, user) => {
        result[user.userId] = user.username;
        return result;
      }, {});
    } catch {
      return {};
    }
  }, []);

  const listSchedules = useCallback(async () => {
    setIsLoading(true);

    try {
      const [scheduleResponse, nextUsernamesById] = await Promise.all([
        scheduleClient.listSchedules(create(ListSchedulesRequestSchema, {})),
        fetchUsernames(),
      ]);
      const mappedSchedules = normalizeSchedules(
        scheduleResponse.schedules.map((schedule) => mapSchedule(schedule, nextUsernamesById)),
      );

      setUsernamesById(nextUsernamesById);
      setSchedules(mappedSchedules);
      return mappedSchedules;
    } catch (error) {
      const resolvedError = ensureError(error, "Failed to load schedules.");

      handleAuthErrors({
        error,
        onError: () => {
          throw resolvedError;
        },
      });

      throw resolvedError;
    } finally {
      setIsLoading(false);
    }
  }, [fetchUsernames, handleAuthErrors]);

  const refreshSchedules = useCallback(async () => listSchedules(), [listSchedules]);

  const pauseSchedule = useCallback(
    async (scheduleId: string) => {
      try {
        const response = await scheduleClient.pauseSchedule(
          create(PauseScheduleRequestSchema, { scheduleId: BigInt(scheduleId) }),
        );
        const nextSchedule = response.schedule;

        if (!nextSchedule) {
          throw new Error("Paused schedule response was missing a schedule.");
        }

        setSchedules((current) => updateMappedSchedule(current, nextSchedule, usernamesById));
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to pause schedule.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors, usernamesById],
  );

  const resumeSchedule = useCallback(
    async (scheduleId: string) => {
      try {
        const response = await scheduleClient.resumeSchedule(
          create(ResumeScheduleRequestSchema, { scheduleId: BigInt(scheduleId) }),
        );
        const nextSchedule = response.schedule;

        if (!nextSchedule) {
          throw new Error("Resumed schedule response was missing a schedule.");
        }

        setSchedules((current) => updateMappedSchedule(current, nextSchedule, usernamesById));
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to resume schedule.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors, usernamesById],
  );

  const deleteSchedule = useCallback(
    async (scheduleId: string) => {
      try {
        await scheduleClient.deleteSchedule(create(DeleteScheduleRequestSchema, { scheduleId: BigInt(scheduleId) }));
        setSchedules((current) => normalizeSchedules(current.filter((schedule) => schedule.id !== scheduleId)));
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to delete schedule.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors],
  );

  const reorderSchedules = useCallback(
    async (scheduleIds: string[]) => {
      try {
        const request: ReorderSchedulesRequest = create(ReorderSchedulesRequestSchema, {
          scheduleIds: scheduleIds.map((id) => BigInt(id)),
        });

        await scheduleClient.reorderSchedules(request);

        setSchedules((current) => {
          const rank = new Map(scheduleIds.map((id, index) => [id, index]));
          const fallbackRank = scheduleIds.length;

          return resequenceSchedules(
            [...current].sort((left, right) => {
              const leftRank = rank.get(left.id) ?? fallbackRank + left.priority;
              const rightRank = rank.get(right.id) ?? fallbackRank + right.priority;

              return leftRank - rightRank;
            }),
          );
        });
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to reorder schedules.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors],
  );

  const createSchedule = useCallback(
    async (request: CreateScheduleRequest) => {
      try {
        const response = await scheduleClient.createSchedule(request);
        const nextSchedule = response.schedule;

        if (!nextSchedule) {
          throw new Error("Created schedule response was missing a schedule.");
        }

        const mappedSchedule = mapSchedule(nextSchedule, usernamesById);
        setSchedules((current) => normalizeSchedules([...current, mappedSchedule]));
        return mappedSchedule;
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to create schedule.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors, usernamesById],
  );

  const updateSchedule = useCallback(
    async (request: UpdateScheduleRequest) => {
      try {
        const response = await scheduleClient.updateSchedule(request);
        const nextSchedule = response.schedule;

        if (!nextSchedule) {
          throw new Error("Updated schedule response was missing a schedule.");
        }

        setSchedules((current) => updateMappedSchedule(current, nextSchedule, usernamesById));
        return mapSchedule(nextSchedule, usernamesById);
      } catch (error) {
        const resolvedError = ensureError(error, "Failed to update schedule.");

        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });

        throw resolvedError;
      }
    },
    [handleAuthErrors, usernamesById],
  );

  return useMemo(
    () => ({
      schedules,
      isLoading,
      listSchedules,
      refreshSchedules,
      createSchedule,
      updateSchedule,
      pauseSchedule,
      resumeSchedule,
      deleteSchedule,
      reorderSchedules,
    }),
    [
      schedules,
      isLoading,
      listSchedules,
      refreshSchedules,
      createSchedule,
      updateSchedule,
      pauseSchedule,
      resumeSchedule,
      deleteSchedule,
      reorderSchedules,
    ],
  );
};

export default useScheduleApi;
