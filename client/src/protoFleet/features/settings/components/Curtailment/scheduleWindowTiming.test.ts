import { describe, expect, it } from "vitest";

import type { CurtailmentScheduleDefinition } from "@/protoFleet/api/curtailmentTypes";
import { CurtailmentScheduleDayOfWeek } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  getScheduleWindowTiming,
  resolveScheduleTime,
} from "@/protoFleet/features/settings/components/Curtailment/scheduleWindowTiming";

const hour = 60 * 60 * 1000;

const dailyOvernight: CurtailmentScheduleDefinition = {
  scheduleType: "recurring",
  timezone: "America/New_York",
  startDate: "2027-01-01",
  startTime: "22:00",
  endTime: "06:00",
  endDate: "",
  frequency: "daily",
  daysOfWeek: [],
  dayOfMonth: "",
  untilDate: "",
};

describe("resolveScheduleTime", () => {
  // The same cases as the server's schedulewindow tests.
  it.each([
    ["America/New_York", "2027-03-14", "02:30", "2027-03-14T07:30:00Z"],
    ["Europe/London", "2027-03-28", "01:30", "2027-03-28T01:30:00Z"],
    ["America/New_York", "2027-11-07", "01:30", "2027-11-07T05:30:00Z"],
    ["Europe/London", "2027-10-31", "01:30", "2027-10-31T00:30:00Z"],
    ["Europe/Berlin", "2027-10-31", "02:30", "2027-10-31T00:30:00Z"],
    ["Australia/Sydney", "2027-04-04", "02:30", "2027-04-03T15:30:00Z"],
    ["America/Chicago", "2027-07-01", "17:00", "2027-07-01T22:00:00Z"],
  ])("resolves %s %s %s like the server", (timeZone, date, time, expected) => {
    expect(resolveScheduleTime(date, time, timeZone)).toBe(Date.parse(expected));
  });

  it("rejects incomplete values and unknown zones", () => {
    expect(resolveScheduleTime("2027-07-01", "", "UTC")).toBeNull();
    expect(resolveScheduleTime("", "17:00", "UTC")).toBeNull();
    expect(resolveScheduleTime("2027-07-01", "17:00", "Mars/Olympus")).toBeNull();
  });
});

describe("getScheduleWindowTiming", () => {
  it("measures a one-time window and flags a clock change inside it", () => {
    const oneTime: CurtailmentScheduleDefinition = {
      ...dailyOvernight,
      scheduleType: "oneTime",
      frequency: "daily",
      startDate: "2027-11-06",
      endDate: "2027-11-07",
    };
    expect(getScheduleWindowTiming(oneTime)).toEqual({
      clockDurationMs: 8 * hour,
      clockChange: { startDate: "2027-11-06", durationMs: 9 * hour },
    });
    expect(getScheduleWindowTiming({ ...oneTime, startDate: "2027-07-01", endDate: "2027-07-02" })).toEqual({
      clockDurationMs: 8 * hour,
      clockChange: null,
    });
  });

  it("finds the next recurring window that a clock change lengthens or shortens", () => {
    expect(getScheduleWindowTiming(dailyOvernight, new Date("2027-10-01T12:00:00Z"))).toEqual({
      clockDurationMs: 8 * hour,
      clockChange: { startDate: "2027-11-06", durationMs: 9 * hour },
    });
    expect(getScheduleWindowTiming(dailyOvernight, new Date("2027-12-01T12:00:00Z"))?.clockChange).toEqual({
      startDate: "2028-03-11",
      durationMs: 7 * hour,
    });
  });

  it("reports no clock change for windows that never span one", () => {
    const now = new Date("2027-10-01T12:00:00Z");
    expect(getScheduleWindowTiming({ ...dailyOvernight, startTime: "17:00", endTime: "21:00" }, now)).toEqual({
      clockDurationMs: 4 * hour,
      clockChange: null,
    });
    // New York changes clocks early on Sundays, so Monday-night windows are unaffected.
    expect(
      getScheduleWindowTiming(
        { ...dailyOvernight, frequency: "weekly", daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY] },
        now,
      )?.clockChange,
    ).toBeNull();
    expect(getScheduleWindowTiming({ ...dailyOvernight, untilDate: "2027-11-05" }, now)?.clockChange).toBeNull();
    expect(getScheduleWindowTiming({ ...dailyOvernight, timezone: "UTC" }, now)?.clockChange).toBeNull();
  });

  it("returns nothing for a window without a length", () => {
    expect(getScheduleWindowTiming({ ...dailyOvernight, endTime: "22:00" })).toBeNull();
    expect(getScheduleWindowTiming({ ...dailyOvernight, endTime: "" })).toBeNull();
  });
});
