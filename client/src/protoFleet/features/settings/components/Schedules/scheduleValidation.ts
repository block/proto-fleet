import { create } from "@bufbuild/protobuf";

import {
  CreateScheduleRequestSchema,
  DayOfWeek,
  PowerTargetConfigSchema,
  PowerTargetMode,
  ScheduleAction as ProtoScheduleAction,
  ScheduleType as ProtoScheduleType,
  RecurrenceFrequency,
  type Schedule,
  ScheduleRecurrenceSchema,
  ScheduleTargetSchema,
  ScheduleTargetType,
  UpdateScheduleRequestSchema,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { hasFutureScheduleRun } from "@/protoFleet/features/settings/components/Schedules/scheduleRunUtils";
import {
  addDaysToDateValue,
  buildDateInTimeZone,
  formatDateParts,
  getTimeZoneDateTimeParts,
  parseDate,
} from "@/protoFleet/features/settings/utils/scheduleDateUtils";
import type { SelectOption } from "@/shared/components/Select";

export type ScheduleFormAction = "setPowerTarget" | "reboot" | "sleep";
export type ScheduleFormType = "oneTime" | "recurring";
export type ScheduleFormFrequency = "daily" | "weekly" | "monthly";
export type ScheduleFormEndBehavior = "indefinite" | "endDate";
export type ScheduleFormPowerTargetMode = "default" | "max";

export type ScheduleFormValues = {
  name: string;
  action: ScheduleFormAction;
  powerTargetMode: ScheduleFormPowerTargetMode;
  scheduleType: ScheduleFormType;
  startDate: string;
  startTime: string;
  endTime: string;
  timezone: string;
  frequency: ScheduleFormFrequency;
  daysOfWeek: DayOfWeek[];
  dayOfMonth: string;
  endBehavior: ScheduleFormEndBehavior;
  endDate: string;
  rackTargetIds: string[];
  groupTargetIds: string[];
  minerTargetIds: string[];
};

export type ScheduleFormErrors = Partial<
  Record<"name" | "startDate" | "startTime" | "endTime" | "daysOfWeek" | "dayOfMonth" | "endDate", string>
>;

const resolvedTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

const roundDateToQuarterHour = (date: Date) => {
  const next = new Date(date);
  next.setSeconds(0, 0);
  const minutes = next.getMinutes();
  const roundedMinutes = minutes % 15 === 0 ? minutes + 15 : Math.ceil(minutes / 15) * 15;

  if (roundedMinutes === 60) {
    next.setHours(next.getHours() + 1, 0, 0, 0);
  } else {
    next.setMinutes(roundedMinutes, 0, 0);
  }

  return next;
};

const formatTimeValue = (date: Date) =>
  `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;

const mapProtoAction = (action: ProtoScheduleAction): ScheduleFormAction => {
  switch (action) {
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

const mapProtoScheduleType = (scheduleType: ProtoScheduleType): ScheduleFormType =>
  scheduleType === ProtoScheduleType.RECURRING ? "recurring" : "oneTime";

const mapProtoFrequency = (frequency: RecurrenceFrequency): ScheduleFormFrequency => {
  switch (frequency) {
    case RecurrenceFrequency.WEEKLY:
      return "weekly";
    case RecurrenceFrequency.MONTHLY:
      return "monthly";
    case RecurrenceFrequency.DAILY:
    case RecurrenceFrequency.UNSPECIFIED:
    default:
      return "daily";
  }
};

const toProtoAction = (action: ScheduleFormAction) => {
  switch (action) {
    case "reboot":
      return ProtoScheduleAction.REBOOT;
    case "sleep":
      return ProtoScheduleAction.SLEEP;
    case "setPowerTarget":
    default:
      return ProtoScheduleAction.SET_POWER_TARGET;
  }
};

const toProtoScheduleType = (scheduleType: ScheduleFormType) =>
  scheduleType === "recurring" ? ProtoScheduleType.RECURRING : ProtoScheduleType.ONE_TIME;

const toProtoFrequency = (frequency: ScheduleFormFrequency) => {
  switch (frequency) {
    case "weekly":
      return RecurrenceFrequency.WEEKLY;
    case "monthly":
      return RecurrenceFrequency.MONTHLY;
    case "daily":
    default:
      return RecurrenceFrequency.DAILY;
  }
};

const toProtoPowerTargetMode = (mode: ScheduleFormPowerTargetMode) =>
  mode === "max" ? PowerTargetMode.MAX : PowerTargetMode.DEFAULT;

const parsePositiveInteger = (value: string) => {
  const parsed = Number(value);

  if (!Number.isInteger(parsed) || parsed < 1) {
    return null;
  }

  return parsed;
};

const getDefaultValues = (timeZone: string): ScheduleFormValues => {
  const now = new Date();
  const nowInTimeZone = getTimeZoneDateTimeParts(now, timeZone);
  const roundedStart = roundDateToQuarterHour(
    nowInTimeZone
      ? new Date(
          nowInTimeZone.year,
          nowInTimeZone.month - 1,
          nowInTimeZone.day,
          nowInTimeZone.hours,
          nowInTimeZone.minutes,
        )
      : now,
  );
  const defaultEnd = new Date(roundedStart);
  defaultEnd.setHours(defaultEnd.getHours() + 1);

  return {
    name: "",
    action: "setPowerTarget",
    powerTargetMode: "default",
    scheduleType: "oneTime",
    startDate: formatDateParts({
      year: roundedStart.getFullYear(),
      month: roundedStart.getMonth() + 1,
      day: roundedStart.getDate(),
    }),
    startTime: formatTimeValue(roundedStart),
    endTime: formatTimeValue(defaultEnd),
    timezone: timeZone,
    frequency: "daily",
    daysOfWeek: [],
    dayOfMonth: String(roundedStart.getDate()),
    endBehavior: "indefinite",
    endDate: "",
    rackTargetIds: [],
    groupTargetIds: [],
    minerTargetIds: [],
  };
};

export const weekdayOptions: Array<{ value: DayOfWeek; label: string }> = [
  { value: DayOfWeek.SUNDAY, label: "Sun" },
  { value: DayOfWeek.MONDAY, label: "Mon" },
  { value: DayOfWeek.TUESDAY, label: "Tue" },
  { value: DayOfWeek.WEDNESDAY, label: "Wed" },
  { value: DayOfWeek.THURSDAY, label: "Thu" },
  { value: DayOfWeek.FRIDAY, label: "Fri" },
  { value: DayOfWeek.SATURDAY, label: "Sat" },
];

export const actionOptions: SelectOption[] = [
  { value: "setPowerTarget", label: "Set power target" },
  { value: "reboot", label: "Reboot" },
  { value: "sleep", label: "Sleep" },
];

export const powerTargetModeOptions: SelectOption[] = [
  {
    value: "default",
    label: "Default",
    description: "Use the manufacturer's default power setting.",
  },
  {
    value: "max",
    label: "Max",
    description: "Run miners at maximum performance and power consumption.",
  },
];

export const scheduleTypeOptions: SelectOption[] = [
  { value: "oneTime", label: "One-time" },
  { value: "recurring", label: "Recurring" },
];

export const frequencyOptions: SelectOption[] = [
  { value: "daily", label: "Daily" },
  { value: "weekly", label: "Weekly" },
  { value: "monthly", label: "Monthly" },
];

export const endBehaviorOptions: SelectOption[] = [
  { value: "indefinite", label: "Run indefinitely" },
  { value: "endDate", label: "End on date" },
];

export const timeOptions: SelectOption[] = Array.from({ length: 24 * 4 }, (_, index) => {
  const hours = Math.floor(index / 4);
  const minutes = (index % 4) * 15;
  const value = `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
  const date = new Date(2026, 0, 1, hours, minutes);

  return {
    value,
    label: new Intl.DateTimeFormat(undefined, {
      hour: "numeric",
      minute: "2-digit",
    }).format(date),
  };
});

export const getNextEndTimeAfterStart = (startTime: string) =>
  timeOptions.find((option) => option.value > startTime)?.value ?? timeOptions[0]?.value ?? startTime;

export const createDefaultScheduleFormValues = (timeZone = resolvedTimeZone()) => getDefaultValues(timeZone);

export const createScheduleFormValuesFromSchedule = (schedule: Schedule): ScheduleFormValues => {
  const defaults = getDefaultValues(schedule.timezone || resolvedTimeZone());
  const recurrence = schedule.recurrence;
  const scheduleType = mapProtoScheduleType(schedule.scheduleType);
  const frequency = mapProtoFrequency(recurrence?.frequency ?? RecurrenceFrequency.DAILY);
  const usesPowerTargetWindow =
    scheduleType === "recurring" && schedule.action === ProtoScheduleAction.SET_POWER_TARGET;
  const validStartDate = schedule.startDate && parseDate(schedule.startDate) ? schedule.startDate : "";
  const validEndDate = schedule.endDate && parseDate(schedule.endDate) ? schedule.endDate : "";

  return {
    ...defaults,
    name: schedule.name,
    action: mapProtoAction(schedule.action),
    powerTargetMode: schedule.actionConfig?.mode === PowerTargetMode.MAX ? "max" : defaults.powerTargetMode,
    scheduleType,
    startDate: schedule.startDate ? validStartDate : defaults.startDate,
    startTime: schedule.startTime || defaults.startTime,
    endTime: usesPowerTargetWindow ? schedule.endTime || defaults.endTime : "",
    timezone: schedule.timezone || defaults.timezone,
    frequency,
    daysOfWeek: recurrence?.daysOfWeek?.filter((day) => day !== DayOfWeek.UNSPECIFIED) ?? [],
    dayOfMonth: recurrence?.dayOfMonth ? String(recurrence.dayOfMonth) : defaults.dayOfMonth,
    endBehavior: schedule.endDate ? "endDate" : "indefinite",
    endDate: validEndDate,
    rackTargetIds: schedule.targets
      .filter((target) => target.targetType === ScheduleTargetType.RACK)
      .map((target) => target.targetId),
    groupTargetIds: schedule.targets
      .filter((target) => target.targetType === ScheduleTargetType.GROUP)
      .map((target) => target.targetId),
    minerTargetIds: schedule.targets
      .filter((target) => target.targetType === ScheduleTargetType.MINER)
      .map((target) => target.targetId),
  };
};

export const describeSelectedTargets = (
  values: Pick<ScheduleFormValues, "rackTargetIds" | "groupTargetIds" | "minerTargetIds">,
) => {
  const rackCount = values.rackTargetIds.length;
  const groupCount = values.groupTargetIds.length;
  const minerCount = values.minerTargetIds.length;

  if (rackCount === 0 && groupCount === 0 && minerCount === 0) {
    return "Applies to all miners";
  }

  const parts = [
    rackCount > 0 ? `${rackCount} ${rackCount === 1 ? "rack" : "racks"}` : null,
    groupCount > 0 ? `${groupCount} ${groupCount === 1 ? "group" : "groups"}` : null,
    minerCount > 0 ? `${minerCount} ${minerCount === 1 ? "miner" : "miners"}` : null,
  ].filter(Boolean);

  if (parts.length === 1) {
    return `Applies to ${parts[0]}`;
  }

  const head = parts.slice(0, -1).join(", ");
  const tail = parts[parts.length - 1];
  return `Applies to ${head} and ${tail}`;
};

export const validateSchedule = (values: ScheduleFormValues, now = new Date()): ScheduleFormErrors => {
  const errors: ScheduleFormErrors = {};
  const trimmedName = values.name.trim();
  const scheduledStart =
    values.startDate && values.startTime
      ? buildDateInTimeZone(values.startDate, values.startTime, values.timezone)
      : null;

  if (!trimmedName) {
    errors.name = "Enter a schedule name";
  } else if (trimmedName.length > 100) {
    errors.name = "Schedule names must be 100 characters or fewer";
  }

  if (!values.startDate) {
    errors.startDate = "Select a date";
  }

  if (!values.startTime) {
    errors.startTime = "Select a time";
  }

  if (values.startDate && values.startTime && !scheduledStart) {
    errors.startTime = "Selected time does not exist in the chosen timezone";
  }

  if (values.scheduleType === "oneTime" && values.startDate && values.startTime) {
    if (scheduledStart && scheduledStart.getTime() <= now.getTime()) {
      errors.startTime = "Choose a future run time";
    }
  }

  if (values.scheduleType === "recurring") {
    if (values.frequency === "weekly" && values.daysOfWeek.length === 0) {
      errors.daysOfWeek = "Select at least one day";
    }

    if (values.frequency === "monthly") {
      const dayOfMonth = parsePositiveInteger(values.dayOfMonth);

      if (dayOfMonth === null || dayOfMonth > 31) {
        errors.dayOfMonth = "Enter a day between 1 and 31";
      }
    }

    if (values.action === "setPowerTarget" && !values.endTime) {
      errors.endTime = "Select an end time";
    }

    if (
      values.action === "setPowerTarget" &&
      values.startDate &&
      values.startTime &&
      values.endTime &&
      !buildDateInTimeZone(
        values.endTime < values.startTime ? addDaysToDateValue(values.startDate, 1) : values.startDate,
        values.endTime,
        values.timezone,
      )
    ) {
      errors.endTime = "Selected time does not exist in the chosen timezone";
    }

    if (
      values.action === "setPowerTarget" &&
      values.startTime &&
      values.endTime &&
      values.endTime === values.startTime
    ) {
      errors.endTime = "End time must be different from the start time";
    }

    if (values.endBehavior === "endDate") {
      if (!values.endDate) {
        errors.endDate = "Select an end date";
      } else {
        const startDate = parseDate(values.startDate);
        const endDate = parseDate(values.endDate);

        if (startDate && endDate && endDate.getTime() < startDate.getTime()) {
          errors.endDate = "End date must be on or after the start date";
        }
      }
    }

    if (
      values.endBehavior === "endDate" &&
      values.startDate &&
      values.startTime &&
      values.endDate &&
      !errors.startDate &&
      !errors.startTime &&
      !errors.daysOfWeek &&
      !errors.dayOfMonth &&
      !errors.endDate &&
      !hasFutureScheduleRun(values, now)
    ) {
      errors.endDate = "End date must allow at least one future run";
    }
  }

  return errors;
};

export const buildScheduleRequest = (values: ScheduleFormValues, scheduleId?: string) => {
  const action = toProtoAction(values.action);
  const scheduleType = toProtoScheduleType(values.scheduleType);

  const targets = [
    ...values.rackTargetIds.map((targetId) =>
      create(ScheduleTargetSchema, {
        targetType: ScheduleTargetType.RACK,
        targetId,
      }),
    ),
    ...values.groupTargetIds.map((targetId) =>
      create(ScheduleTargetSchema, {
        targetType: ScheduleTargetType.GROUP,
        targetId,
      }),
    ),
    ...values.minerTargetIds.map((targetId) =>
      create(ScheduleTargetSchema, {
        targetType: ScheduleTargetType.MINER,
        targetId,
      }),
    ),
  ];

  const recurrence =
    values.scheduleType === "recurring"
      ? create(ScheduleRecurrenceSchema, {
          frequency: toProtoFrequency(values.frequency),
          interval: 1,
          daysOfWeek: values.frequency === "weekly" ? values.daysOfWeek : [],
          dayOfMonth:
            values.frequency === "monthly" ? (parsePositiveInteger(values.dayOfMonth) ?? undefined) : undefined,
        })
      : undefined;

  const actionConfig =
    action === ProtoScheduleAction.SET_POWER_TARGET
      ? create(PowerTargetConfigSchema, {
          mode: toProtoPowerTargetMode(values.powerTargetMode),
        })
      : undefined;

  const requestBody = {
    name: values.name.trim(),
    action,
    actionConfig,
    scheduleType,
    recurrence,
    startDate: values.startDate,
    startTime: values.startTime,
    endTime: values.scheduleType === "recurring" && values.action === "setPowerTarget" ? values.endTime : "",
    endDate: values.scheduleType === "recurring" && values.endBehavior === "endDate" ? values.endDate : "",
    timezone: values.timezone,
    targets,
  };

  if (scheduleId) {
    return create(UpdateScheduleRequestSchema, {
      scheduleId: BigInt(scheduleId),
      ...requestBody,
    });
  }

  return create(CreateScheduleRequestSchema, requestBody);
};
