import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import process from "node:process";

import { DayOfWeek } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import {
  getFutureScheduleRuns,
  hasFutureScheduleRun,
} from "@/protoFleet/features/settings/components/Schedules/scheduleRunUtils";
import { createDefaultScheduleFormValues } from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";
import { getTimeZoneDateTimeParts } from "@/protoFleet/features/settings/utils/scheduleDateUtils";

describe("scheduleRunUtils", () => {
  const originalTimeZone = process.env.TZ;

  beforeEach(() => {
    process.env.TZ = "Pacific/Auckland";
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();

    if (originalTimeZone === undefined) {
      delete process.env.TZ;
      return;
    }

    process.env.TZ = originalTimeZone;
  });

  it("matches weekly recurrences using the schedule's calendar date", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/New_York"),
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "weekly" as const,
      startDate: "2026-03-01",
      startTime: "07:00",
      daysOfWeek: [DayOfWeek.MONDAY],
    };

    const runs = getFutureScheduleRuns(values, new Date("2026-03-01T00:00:00.000Z"), 2);

    expect(runs).toHaveLength(2);
    expect(getTimeZoneDateTimeParts(runs[0]?.start, values.timezone)).toMatchObject({
      year: 2026,
      month: 3,
      day: 2,
      hours: 7,
      minutes: 0,
    });
    expect(getTimeZoneDateTimeParts(runs[1]?.start, values.timezone)).toMatchObject({
      year: 2026,
      month: 3,
      day: 9,
      hours: 7,
      minutes: 0,
    });
  });

  it("rolls overnight power-target windows into the next schedule day", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/Chicago"),
      scheduleType: "recurring" as const,
      action: "setPowerTarget" as const,
      frequency: "daily" as const,
      startDate: "2026-04-10",
      startTime: "23:15",
      endTime: "01:00",
    };

    const [run] = getFutureScheduleRuns(values, new Date("2026-04-10T00:00:00.000Z"), 1);

    expect(run).toBeDefined();

    expect(getTimeZoneDateTimeParts(run!.start, values.timezone)).toMatchObject({
      year: 2026,
      month: 4,
      day: 10,
      hours: 23,
      minutes: 15,
    });
    expect(getTimeZoneDateTimeParts(run!.end!, values.timezone)).toMatchObject({
      year: 2026,
      month: 4,
      day: 11,
      hours: 1,
      minutes: 0,
    });
  });

  it("does not synthesize future runs from a DST-gap start time", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/New_York"),
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "daily" as const,
      startDate: "2026-03-08",
      startTime: "02:30",
    };

    expect(getFutureScheduleRuns(values, new Date("2026-03-08T00:00:00.000Z"), 1)).toEqual([]);
    expect(hasFutureScheduleRun(values, new Date("2026-03-08T00:00:00.000Z"))).toBe(false);
  });
});
