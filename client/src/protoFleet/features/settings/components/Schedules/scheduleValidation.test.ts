import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  DayOfWeek,
  PowerTargetConfigSchema,
  PowerTargetMode,
  RecurrenceFrequency,
  ScheduleAction,
  ScheduleRecurrenceSchema,
  ScheduleSchema,
  ScheduleTargetSchema,
  ScheduleTargetType,
  ScheduleType,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import {
  buildScheduleRequest,
  createDefaultScheduleFormValues,
  createScheduleFormValuesFromSchedule,
  validateSchedule,
} from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";

describe("scheduleValidation", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-01T12:00:00.000Z"));
  });

  it("flags one-time schedules in the past", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Morning reboot",
      action: "reboot" as const,
      scheduleType: "oneTime" as const,
      startDate: "2026-04-01",
      startTime: "11:45",
    };

    expect(validateSchedule(values)).toMatchObject({
      startTime: "Choose a future run time",
    });
  });

  it("rejects one-time schedules whose local wall-clock time does not exist", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/New_York"),
      name: "Spring-forward reboot",
      action: "reboot" as const,
      scheduleType: "oneTime" as const,
      startDate: "2026-03-08",
      startTime: "02:30",
    };

    expect(validateSchedule(values)).toMatchObject({
      startTime: "Selected time does not exist in the chosen timezone",
    });
  });

  it("flags recurring schedules whose end date allows no future runs", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Monday reboot",
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "weekly" as const,
      daysOfWeek: [DayOfWeek.MONDAY],
      startDate: "2026-04-01",
      startTime: "07:00",
      endBehavior: "endDate" as const,
      endDate: "2026-04-03",
    };

    expect(validateSchedule(values)).toMatchObject({
      endDate: "End date must allow at least one future run",
    });
  });

  it("flags recurring power-target schedules whose end time matches the start time", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Night cap",
      action: "setPowerTarget" as const,
      scheduleType: "recurring" as const,
      frequency: "daily" as const,
      startDate: "2026-04-02",
      startTime: "23:45",
      endTime: "23:45",
    };

    expect(validateSchedule(values)).toMatchObject({
      endTime: "End time must be different from the start time",
    });
  });

  it("builds default values in the requested timezone and rolls the date at midnight", () => {
    vi.setSystemTime(new Date("2026-04-01T23:53:00.000Z"));

    expect(createDefaultScheduleFormValues("UTC")).toMatchObject({
      startDate: "2026-04-02",
      startTime: "00:00",
      endTime: "01:00",
      timezone: "UTC",
    });
  });

  it("rounds exact quarter-hour times forward so the default run is always in the future", () => {
    vi.setSystemTime(new Date("2026-04-01T09:45:00.000Z"));

    expect(createDefaultScheduleFormValues("UTC")).toMatchObject({
      startDate: "2026-04-01",
      startTime: "10:00",
      endTime: "11:00",
      timezone: "UTC",
    });
  });

  it("uses the requested timezone instead of the machine timezone for default values", () => {
    vi.setSystemTime(new Date("2026-04-01T00:07:00.000Z"));

    expect(createDefaultScheduleFormValues("Pacific/Honolulu")).toMatchObject({
      startDate: "2026-03-31",
      startTime: "14:15",
      endTime: "15:15",
      timezone: "Pacific/Honolulu",
    });
  });

  it("builds recurring update requests with targets and recurrence details", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Weekday cap",
      action: "setPowerTarget" as const,
      powerTargetMode: "max" as const,
      scheduleType: "recurring" as const,
      frequency: "weekly" as const,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.FRIDAY],
      startDate: "2026-04-03",
      startTime: "06:00",
      endTime: "22:00",
      endBehavior: "endDate" as const,
      endDate: "2026-05-01",
      rackTargetIds: ["rack-1"],
      minerTargetIds: ["miner-9"],
    };

    const request = buildScheduleRequest(values, "12");

    expect(request).toMatchObject({
      scheduleId: 12n,
      name: "Weekday cap",
      action: ScheduleAction.SET_POWER_TARGET,
      scheduleType: ScheduleType.RECURRING,
      startDate: "2026-04-03",
      startTime: "06:00",
      endTime: "22:00",
      endDate: "2026-05-01",
    });
    expect(request.actionConfig?.mode).toBe(PowerTargetMode.MAX);
    expect(request.recurrence).toMatchObject({
      frequency: RecurrenceFrequency.WEEKLY,
      interval: 1,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.FRIDAY],
    });
    expect(request.targets).toEqual([
      expect.objectContaining({
        targetType: ScheduleTargetType.RACK,
        targetId: "rack-1",
      }),
      expect.objectContaining({
        targetType: ScheduleTargetType.MINER,
        targetId: "miner-9",
      }),
    ]);
  });

  it("maps saved schedules into editable form values", () => {
    const schedule = create(ScheduleSchema, {
      id: 9n,
      name: "Night sleep",
      action: ScheduleAction.SLEEP,
      actionConfig: create(PowerTargetConfigSchema, {
        mode: PowerTargetMode.DEFAULT,
      }),
      scheduleType: ScheduleType.RECURRING,
      recurrence: create(ScheduleRecurrenceSchema, {
        frequency: RecurrenceFrequency.MONTHLY,
        interval: 1,
        dayOfMonth: 5,
      }),
      startDate: "2026-04-05",
      startTime: "23:00",
      endDate: "2026-09-05",
      timezone: "UTC",
      targets: [
        create(ScheduleTargetSchema, {
          targetType: ScheduleTargetType.RACK,
          targetId: "rack-3",
        }),
      ],
    });

    expect(createScheduleFormValuesFromSchedule(schedule)).toMatchObject({
      name: "Night sleep",
      action: "sleep",
      scheduleType: "recurring",
      frequency: "monthly",
      dayOfMonth: "5",
      startDate: "2026-04-05",
      startTime: "23:00",
      endBehavior: "endDate",
      endDate: "2026-09-05",
      rackTargetIds: ["rack-3"],
    });
  });

  it("keeps endTime empty for saved schedules that do not use a power-target window", () => {
    const schedule = create(ScheduleSchema, {
      id: 10n,
      name: "Night sleep",
      action: ScheduleAction.SLEEP,
      scheduleType: ScheduleType.RECURRING,
      recurrence: create(ScheduleRecurrenceSchema, {
        frequency: RecurrenceFrequency.WEEKLY,
        interval: 1,
        daysOfWeek: [DayOfWeek.MONDAY],
      }),
      startDate: "2026-04-07",
      startTime: "23:00",
      timezone: "UTC",
    });

    expect(createScheduleFormValuesFromSchedule(schedule)).toMatchObject({
      action: "sleep",
      scheduleType: "recurring",
      startTime: "23:00",
      endTime: "",
    });
  });

  it("blanks malformed saved dates instead of normalizing them to another day", () => {
    const schedule = create(ScheduleSchema, {
      id: 11n,
      name: "Malformed schedule",
      action: ScheduleAction.REBOOT,
      scheduleType: ScheduleType.RECURRING,
      recurrence: create(ScheduleRecurrenceSchema, {
        frequency: RecurrenceFrequency.DAILY,
        interval: 1,
      }),
      startDate: "2026-02-31",
      startTime: "08:00",
      endDate: "2026-13-01",
      timezone: "UTC",
    });

    expect(createScheduleFormValuesFromSchedule(schedule)).toMatchObject({
      startDate: "",
      startTime: "08:00",
      endBehavior: "endDate",
      endDate: "",
    });
  });
});
