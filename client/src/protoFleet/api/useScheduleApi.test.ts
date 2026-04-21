import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import { SCHEDULES_CHANGED_EVENT } from "./scheduleEvents";
import useScheduleApi from "./useScheduleApi";
import { scheduleClient } from "@/protoFleet/api/clients";
import {
  DayOfWeek,
  DeleteScheduleResponseSchema,
  ListSchedulesResponseSchema,
  PauseScheduleResponseSchema,
  ScheduleAction as ProtoScheduleAction,
  ScheduleStatus as ProtoScheduleStatus,
  ScheduleType as ProtoScheduleType,
  RecurrenceFrequency,
  ReorderSchedulesResponseSchema,
  ResumeScheduleResponseSchema,
  ScheduleRecurrenceSchema,
  ScheduleSchema,
  ScheduleTargetSchema,
  ScheduleTargetType,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";

vi.mock("@/protoFleet/api/clients", () => ({
  scheduleClient: {
    listSchedules: vi.fn(),
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

const mockListSchedules = vi.mocked(scheduleClient.listSchedules);
const mockPauseSchedule = vi.mocked(scheduleClient.pauseSchedule);
const mockResumeSchedule = vi.mocked(scheduleClient.resumeSchedule);
const mockDeleteSchedule = vi.mocked(scheduleClient.deleteSchedule);
const mockReorderSchedules = vi.mocked(scheduleClient.reorderSchedules);
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

const createTimestamp = (value: string) => {
  const date = new Date(value);

  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(date.getTime() / 1000)),
    nanos: (date.getTime() % 1000) * 1_000_000,
  });
};

const createDeferred = <T>() => {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });

  return { promise, resolve, reject };
};

const formatExpectedNextRunSummary = (value: string) => {
  const nextRun = new Date(value);
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

const createScheduleMessage = ({
  id,
  priority,
  name,
  action,
  status,
  createdBy,
  createdByUsername,
  startDate,
  startTime,
  timezone = "America/Toronto",
  nextRunAt,
  targets = [],
  recurrence,
}: {
  id: bigint;
  priority: number;
  name: string;
  action: ProtoScheduleAction;
  status: ProtoScheduleStatus;
  createdBy: bigint;
  createdByUsername?: string;
  startDate: string;
  startTime: string;
  timezone?: string;
  nextRunAt?: string;
  targets?: Array<{ targetType: ScheduleTargetType; targetId: string }>;
  recurrence?: Partial<{
    frequency: RecurrenceFrequency;
    interval: number;
    daysOfWeek: DayOfWeek[];
    dayOfMonth?: number;
  }>;
}) =>
  create(ScheduleSchema, {
    id,
    priority,
    name,
    action,
    status,
    createdBy,
    createdByUsername,
    scheduleType: ProtoScheduleType.RECURRING,
    recurrence: create(ScheduleRecurrenceSchema, {
      frequency: RecurrenceFrequency.WEEKLY,
      interval: 1,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.TUESDAY, DayOfWeek.WEDNESDAY, DayOfWeek.THURSDAY, DayOfWeek.FRIDAY],
      ...recurrence,
    }),
    startDate,
    startTime,
    endTime: action === ProtoScheduleAction.SET_POWER_TARGET ? "06:00" : "",
    timezone,
    nextRunAt: nextRunAt ? createTimestamp(nextRunAt) : undefined,
    targets: targets.map((target) => create(ScheduleTargetSchema, target)),
  });

describe("useScheduleApi", () => {
  let dispatchEventSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-30T09:00:00-04:00"));
    dispatchEventSpy = vi.spyOn(window, "dispatchEvent");
  });

  afterEach(() => {
    vi.useRealTimers();
    dispatchEventSpy.mockRestore();
  });

  it("lists schedules from the schedule service and maps them into list rows", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 2n,
            priority: 2,
            name: "Night sleep",
            action: ProtoScheduleAction.SLEEP,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 2n,
            createdByUsername: "Rongxin Liu",
            startDate: "2026-03-30",
            startTime: "22:00",
            timezone: "America/Chicago",
            nextRunAt: "2026-04-01T02:00:00.000Z",
          }),
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Morning reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.PAUSED,
            createdBy: 1n,
            createdByUsername: "Negar Naghshbandi",
            startDate: "2026-03-30",
            startTime: "07:00",
            nextRunAt: "2026-03-31T11:00:00.000Z",
            targets: [{ targetType: ScheduleTargetType.MINER, targetId: "miner-1" }],
          }),
        ],
      }),
    );

    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
    });

    expect(result.current.schedules.map((schedule) => schedule.id)).toEqual(["1", "2"]);
    expect(result.current.schedules[0]).toMatchObject({
      name: "Morning reboot",
      targetSummary: "Applies to 1 miner",
      action: "reboot",
      status: "paused",
      createdBy: "Negar Naghshbandi",
    });
    expect(result.current.schedules[1]).toMatchObject({
      name: "Night sleep",
      targetSummary: "Applies to all miners",
      scheduleSummary: `Weekdays · ${timeFormatter.format(new Date("2026-04-01T03:00:00.000Z"))}`,
      action: "sleep",
      status: "active",
      createdBy: "Rongxin Liu",
    });
    expect(result.current.schedules[1].nextRunSummary).toBe(formatExpectedNextRunSummary("2026-04-01T02:00:00.000Z"));
  });

  it("prefers the server-provided creator username when schedules include it", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Morning reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 1n,
            createdByUsername: "admin@example.com",
            startDate: "2026-03-30",
            startTime: "07:00",
          }),
        ],
      }),
    );

    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
    });

    expect(result.current.schedules[0]?.createdBy).toBe("admin@example.com");
  });

  it("keeps the loading flag idle during background refreshes", async () => {
    const deferred = createDeferred<Awaited<ReturnType<typeof scheduleClient.listSchedules>>>();
    mockListSchedules.mockReturnValue(deferred.promise);

    const { result } = renderHook(() => useScheduleApi());

    let refreshPromise: Promise<unknown> | undefined;

    await act(async () => {
      refreshPromise = result.current.refreshSchedules({ background: true });
    });

    expect(result.current.isLoading).toBe(false);

    deferred.resolve(
      create(ListSchedulesResponseSchema, {
        schedules: [],
      }),
    );

    await act(async () => {
      await refreshPromise;
    });

    expect(result.current.isLoading).toBe(false);
  });

  it("reuses the same in-flight refresh across background and foreground callers", async () => {
    const deferred = createDeferred<Awaited<ReturnType<typeof scheduleClient.listSchedules>>>();
    mockListSchedules.mockReturnValue(deferred.promise);

    const { result } = renderHook(() => useScheduleApi());

    let backgroundRefreshPromise: Promise<unknown> | undefined;
    let foregroundRefreshPromise: Promise<unknown> | undefined;

    await act(async () => {
      backgroundRefreshPromise = result.current.refreshSchedules({ background: true });
      foregroundRefreshPromise = result.current.refreshSchedules();
    });

    expect(mockListSchedules).toHaveBeenCalledTimes(1);
    expect(result.current.isLoading).toBe(true);

    deferred.resolve(
      create(ListSchedulesResponseSchema, {
        schedules: [],
      }),
    );

    await act(async () => {
      await Promise.all([backgroundRefreshPromise, foregroundRefreshPromise]);
    });

    expect(result.current.isLoading).toBe(false);
  });

  it("pauses and resumes schedules via the schedule service", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Morning reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 1n,
            startDate: "2026-03-30",
            startTime: "07:00",
          }),
        ],
      }),
    );
    mockPauseSchedule.mockResolvedValue(
      create(PauseScheduleResponseSchema, {
        schedule: createScheduleMessage({
          id: 1n,
          priority: 1,
          name: "Morning reboot",
          action: ProtoScheduleAction.REBOOT,
          status: ProtoScheduleStatus.PAUSED,
          createdBy: 1n,
          startDate: "2026-03-30",
          startTime: "07:00",
        }),
      }),
    );
    mockResumeSchedule.mockResolvedValue(
      create(ResumeScheduleResponseSchema, {
        schedule: createScheduleMessage({
          id: 1n,
          priority: 1,
          name: "Morning reboot",
          action: ProtoScheduleAction.REBOOT,
          status: ProtoScheduleStatus.ACTIVE,
          createdBy: 1n,
          startDate: "2026-03-30",
          startTime: "07:00",
        }),
      }),
    );

    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.refreshSchedules();
      await result.current.pauseSchedule("1");
      await result.current.resumeSchedule("1");
    });

    expect(mockPauseSchedule).toHaveBeenCalledWith(expect.objectContaining({ scheduleId: 1n }));
    expect(mockResumeSchedule).toHaveBeenCalledWith(expect.objectContaining({ scheduleId: 1n }));
    expect(result.current.schedules[0]?.status).toBe("active");
    expect(dispatchEventSpy.mock.calls.map(([event]: [Event]) => event.type)).toContain(SCHEDULES_CHANGED_EVENT);
  });

  it("reorders schedules through the service and removes deleted schedules locally", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Morning reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 1n,
            startDate: "2026-03-30",
            startTime: "07:00",
          }),
          createScheduleMessage({
            id: 2n,
            priority: 2,
            name: "Night curtailment",
            action: ProtoScheduleAction.SET_POWER_TARGET,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 2n,
            startDate: "2026-03-30",
            startTime: "22:00",
          }),
        ],
      }),
    );
    mockReorderSchedules.mockResolvedValue(create(ReorderSchedulesResponseSchema, {}));
    mockDeleteSchedule.mockResolvedValue(create(DeleteScheduleResponseSchema, {}));

    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
      await result.current.reorderSchedules(["2", "1"]);
      await result.current.deleteSchedule("1");
    });

    expect(mockReorderSchedules).toHaveBeenCalledWith(expect.objectContaining({ scheduleIds: [2n, 1n] }));
    expect(mockDeleteSchedule).toHaveBeenCalledWith(expect.objectContaining({ scheduleId: 1n }));
    expect(result.current.schedules).toEqual([
      expect.objectContaining({
        id: "2",
        priority: 1,
      }),
    ]);
    expect(dispatchEventSpy.mock.calls.map(([event]: [Event]) => event.type)).toContain(SCHEDULES_CHANGED_EVENT);
  });

  it("includes weekly and monthly recurrence patterns in schedule summaries", async () => {
    mockListSchedules.mockResolvedValue(
      create(ListSchedulesResponseSchema, {
        schedules: [
          createScheduleMessage({
            id: 1n,
            priority: 1,
            name: "Midweek reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 1n,
            startDate: "2026-03-30",
            startTime: "07:00",
            nextRunAt: "2026-04-01T11:00:00.000Z",
            recurrence: {
              frequency: RecurrenceFrequency.WEEKLY,
              interval: 1,
              daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.WEDNESDAY],
            },
          }),
          createScheduleMessage({
            id: 2n,
            priority: 2,
            name: "Monthly reboot",
            action: ProtoScheduleAction.REBOOT,
            status: ProtoScheduleStatus.ACTIVE,
            createdBy: 2n,
            startDate: "2026-03-30",
            startTime: "02:00",
            nextRunAt: "2026-04-01T06:00:00.000Z",
            recurrence: {
              frequency: RecurrenceFrequency.MONTHLY,
              interval: 1,
              dayOfMonth: 1,
              daysOfWeek: [],
            },
          }),
        ],
      }),
    );

    const { result } = renderHook(() => useScheduleApi());

    await act(async () => {
      await result.current.listSchedules();
    });

    expect(result.current.schedules[0]).toMatchObject({
      name: "Midweek reboot",
      scheduleSummary: `Mon, Wed · ${timeFormatter.format(new Date("2026-04-01T11:00:00.000Z"))}`,
    });
    expect(result.current.schedules[1]).toMatchObject({
      name: "Monthly reboot",
      scheduleSummary: `1st day of month · ${timeFormatter.format(new Date("2026-04-01T06:00:00.000Z"))}`,
    });
  });
});
