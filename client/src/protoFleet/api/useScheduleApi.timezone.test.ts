import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import process from "node:process";

import {
  DayOfWeek,
  ListSchedulesResponseSchema,
  ScheduleAction as ProtoScheduleAction,
  ScheduleStatus as ProtoScheduleStatus,
  ScheduleType as ProtoScheduleType,
  RecurrenceFrequency,
  ScheduleRecurrenceSchema,
  ScheduleSchema,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";

const { mockListSchedules } = vi.hoisted(() => ({
  mockListSchedules: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  scheduleClient: {
    listSchedules: mockListSchedules,
    createSchedule: vi.fn(),
    updateSchedule: vi.fn(),
    deleteSchedule: vi.fn(),
    pauseSchedule: vi.fn(),
    resumeSchedule: vi.fn(),
    reorderSchedules: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: ({ onError, error }: { onError?: (error: unknown) => void; error: unknown }) => {
      onError?.(error);
    },
  }),
}));

const createScheduleMessage = ({
  id,
  priority,
  name,
  createdBy,
  startDate,
  startTime,
  timezone,
}: {
  id: bigint;
  priority: number;
  name: string;
  createdBy: bigint;
  startDate: string;
  startTime: string;
  timezone: string;
}) =>
  create(ScheduleSchema, {
    id,
    priority,
    name,
    action: ProtoScheduleAction.REBOOT,
    status: ProtoScheduleStatus.PAUSED,
    createdBy,
    scheduleType: ProtoScheduleType.RECURRING,
    recurrence: create(ScheduleRecurrenceSchema, {
      frequency: RecurrenceFrequency.WEEKLY,
      interval: 1,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.TUESDAY, DayOfWeek.WEDNESDAY, DayOfWeek.THURSDAY, DayOfWeek.FRIDAY],
    }),
    startDate,
    startTime,
    timezone,
  });

describe("useScheduleApi DST schedule summaries", () => {
  const originalTimeZone = process.env.TZ;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();
    process.env.TZ = "UTC";
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-10T09:00:00.000Z"));
  });

  afterEach(() => {
    vi.useRealTimers();

    if (originalTimeZone === undefined) {
      delete process.env.TZ;
      return;
    }

    process.env.TZ = originalTimeZone;
  });

  it("uses the current schedule date for recurring summaries when nextRunAt is missing", async () => {
    const timeFormatter = new Intl.DateTimeFormat(undefined, {
      hour: "numeric",
      minute: "2-digit",
    });

    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Weekday reboot",
            createdBy: 1n,
            startDate: "2026-01-15",
            startTime: "07:00",
            timezone: "America/New_York",
          }),
        ],
      }),
    );

    const { default: useScheduleApi } = await import("./useScheduleApi");
    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
    });

    expect(result.current.schedules[0]).toMatchObject({
      name: "Weekday reboot",
      scheduleSummary: `Weekdays · ${timeFormatter.format(new Date("2026-07-10T11:00:00.000Z"))}`,
    });
  });

  it("does not shift nonexistent DST-gap wall-clock times to a different local hour", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          create(ScheduleSchema, {
            id: 2n,
            priority: 1,
            name: "Spring-forward reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.PAUSED,
            createdBy: 1n,
            scheduleType: ProtoScheduleType.ONE_TIME,
            startDate: "2026-03-08",
            startTime: "02:30",
            timezone: "America/New_York",
          }),
        ],
      }),
    );

    const { default: useScheduleApi } = await import("./useScheduleApi");
    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
    });

    expect(result.current.schedules[0]).toMatchObject({
      name: "Spring-forward reboot",
      scheduleSummary: "2026-03-08 at 02:30",
    });
  });
});
