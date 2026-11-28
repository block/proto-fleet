import { describe, expect, it } from "vitest";

import {
  calendarDay,
  calendarDayStart,
  calendarRangeLabel,
  calendarRangeOf,
  scheduledPeriodOptions,
} from "./reportPeriodOptions";

const january = { month: 1, day: 1 };

describe("scheduledPeriodOptions", () => {
  it("offers only completed months, newest first", () => {
    const options = scheduledPeriodOptions("monthly", "2026-11-27", january);
    expect(options).toHaveLength(60);
    expect(options[0]).toMatchObject({
      start: "2026-10-01",
      end: "2026-11-01",
      label: "October 2026",
      year: "2026",
      periodLabel: "October",
    });
    expect(options[59]).toMatchObject({ start: "2021-11-01", end: "2021-12-01", year: "2021" });
  });

  it("offers a month on the day after it ends", () => {
    expect(scheduledPeriodOptions("monthly", "2026-11-01", january)[0].label).toBe("October 2026");
    expect(scheduledPeriodOptions("monthly", "2026-10-31", january)[0].label).toBe("September 2026");
  });

  it("reaches back to an earlier schedule start, but never less than five years", () => {
    const from = (earliest: string) =>
      scheduledPeriodOptions("monthly", "2026-11-27", january, 1, earliest).slice(-1)[0];
    expect(from("2019-03-01")).toMatchObject({ start: "2019-03-01" });
    expect(from("2026-06-01")).toMatchObject({ start: "2021-11-01" });
  });

  it("labels a quarter that crosses a year boundary with both years", () => {
    const [crossing, , , previous] = scheduledPeriodOptions("quarterly", "2026-03-10", { month: 2, day: 1 });
    expect(crossing).toMatchObject({ year: "2025", periodLabel: "Nov 2025–Jan 2026" });
    expect(previous).toMatchObject({ start: "2025-02-01", year: "2025", periodLabel: "Feb–Apr" });
  });

  it("aligns quarters to the reporting year", () => {
    expect(scheduledPeriodOptions("quarterly", "2026-11-27", january)[0]).toMatchObject({
      start: "2026-07-01",
      end: "2026-10-01",
      label: "Jul–Sep 2026",
    });
    expect(scheduledPeriodOptions("quarterly", "2026-03-10", { month: 2, day: 1 })[0]).toMatchObject({
      start: "2025-11-01",
      end: "2026-02-01",
      label: "Nov 2025–Jan 2026",
    });
  });

  it("aligns quarters to a scheduled quarterly start that differs from the reporting year", () => {
    expect(scheduledPeriodOptions("quarterly", "2026-11-27", january, 2)[0]).toMatchObject({
      start: "2026-08-01",
      end: "2026-11-01",
      label: "Aug–Oct 2026",
    });
  });

  it("starts annual periods on the configured reporting year start", () => {
    expect(scheduledPeriodOptions("annual", "2026-11-27", january)[0]).toMatchObject({
      start: "2025-01-01",
      end: "2026-01-01",
      label: "2025",
    });
    expect(scheduledPeriodOptions("annual", "2026-11-27", { month: 7, day: 1 })[0]).toMatchObject({
      start: "2025-07-01",
      end: "2026-07-01",
      label: "Jul 1, 2025 – Jun 30, 2026",
    });
  });

  it("offers nothing for ad hoc reports", () => {
    expect(scheduledPeriodOptions("ad_hoc", "2026-11-27", january)).toEqual([]);
  });
});

describe("calendar days", () => {
  it("places a day boundary at midnight in the reporting timezone", () => {
    expect(calendarDayStart("2026-10-01", "Africa/Nairobi")?.seconds).toBe(1790802000n);
    expect(calendarDayStart("2026-10-01", "UTC")?.seconds).toBe(1790812800n);
    expect(calendarDayStart("2026-10-01", "Not/AZone")).toBeUndefined();
  });

  it("reads the calendar day of an instant in the reporting timezone", () => {
    expect(
      calendarDay({ $typeName: "google.protobuf.Timestamp", seconds: 1790802000n, nanos: 0 }, "Africa/Nairobi"),
    ).toBe("2026-10-01");
    expect(calendarDay({ $typeName: "google.protobuf.Timestamp", seconds: 1790802000n, nanos: 0 }, "UTC")).toBe(
      "2026-09-30",
    );
  });

  it("reads the days an existing period covers from its last covered instant", () => {
    const time = (seconds: bigint) => ({ $typeName: "google.protobuf.Timestamp" as const, seconds, nanos: 0 });
    // 2026-10-01 10:00–18:00 UTC stays within October 1.
    expect(calendarRangeOf(time(1790848800n), time(1790877600n), "UTC")).toEqual({
      start: "2026-10-01",
      end: "2026-10-02",
    });
    // A whole October ends at the November 1 midnight.
    expect(calendarRangeOf(time(1790812800n), time(1793491200n), "UTC")).toEqual({
      start: "2026-10-01",
      end: "2026-11-01",
    });
    expect(calendarRangeOf(undefined, undefined, "UTC")).toEqual({ start: undefined, end: undefined });
  });

  it("labels a range by its first and last covered day", () => {
    expect(calendarRangeLabel({ start: "2026-08-01", end: "2026-09-01" })).toBe("Aug 1, 2026 – Aug 31, 2026");
  });
});
