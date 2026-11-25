// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  curtailmentScheduleToApi,
  describeCurtailmentSchedule,
  formatScheduleClockTime,
  formatScheduleDate,
} from "@/protoFleet/api/curtailmentSchedules";
import type { CurtailmentScheduleDefinition } from "@/protoFleet/api/curtailmentTypes";
import {
  CurtailmentScheduleDayOfWeek,
  CurtailmentScheduleFrequency,
  CurtailmentScheduleType,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

const recurring: CurtailmentScheduleDefinition = {
  scheduleType: "recurring",
  timezone: "UTC",
  startDate: "2026-06-01",
  startTime: "17:00",
  endTime: "21:00",
  endDate: "2026-06-01",
  frequency: "daily",
  daysOfWeek: [CurtailmentScheduleDayOfWeek.FRIDAY],
  dayOfMonth: "15",
  untilDate: "",
};

const window = (start: string, end: string) => `${formatScheduleClockTime(start)} – ${formatScheduleClockTime(end)}`;

describe("describeCurtailmentSchedule", () => {
  it("describes each recurrence", () => {
    expect(describeCurtailmentSchedule(recurring)).toBe(`Daily, ${window("17:00", "21:00")}`);
    expect(
      describeCurtailmentSchedule({
        ...recurring,
        frequency: "weekly",
        daysOfWeek: [CurtailmentScheduleDayOfWeek.FRIDAY, CurtailmentScheduleDayOfWeek.MONDAY],
      }),
    ).toBe(`Weekly on Mon, Fri, ${window("17:00", "21:00")}`);
    expect(describeCurtailmentSchedule({ ...recurring, frequency: "monthly" })).toBe(
      `Monthly on day 15, ${window("17:00", "21:00")}`,
    );
  });

  it("marks overnight windows and the repeat end", () => {
    expect(
      describeCurtailmentSchedule({ ...recurring, startTime: "22:00", endTime: "06:00", untilDate: "2026-12-31" }),
    ).toBe(`Daily, ${window("22:00", "06:00")} next day, until ${formatScheduleDate("2026-12-31")}`);
  });

  it("describes one-time windows", () => {
    const oneTime = { ...recurring, scheduleType: "oneTime" as const };
    expect(describeCurtailmentSchedule(oneTime)).toBe(
      `${formatScheduleDate("2026-06-01")}, ${window("17:00", "21:00")}`,
    );
    expect(describeCurtailmentSchedule({ ...oneTime, endDate: "2026-06-03" })).toBe(
      `${formatScheduleDate("2026-06-01")}, ${formatScheduleClockTime("17:00")} – ${formatScheduleDate("2026-06-03")}, ${formatScheduleClockTime("21:00")}`,
    );
  });
});

describe("curtailmentScheduleToApi", () => {
  it("sends only the fields for the selected shape", () => {
    expect(curtailmentScheduleToApi({ ...recurring, scheduleType: "oneTime", endDate: "2026-06-02" })).toEqual({
      scheduleType: CurtailmentScheduleType.ONE_TIME,
      timezone: "UTC",
      startDate: "2026-06-01",
      startTime: "17:00",
      endTime: "21:00",
      endDate: "2026-06-02",
    });
    expect(curtailmentScheduleToApi(recurring)).toMatchObject({
      scheduleType: CurtailmentScheduleType.RECURRING,
      frequency: CurtailmentScheduleFrequency.DAILY,
      daysOfWeek: [],
      dayOfMonth: 0,
    });
    expect(
      curtailmentScheduleToApi({
        ...recurring,
        frequency: "weekly",
        daysOfWeek: [CurtailmentScheduleDayOfWeek.FRIDAY, CurtailmentScheduleDayOfWeek.MONDAY],
      }),
    ).toMatchObject({
      daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY, CurtailmentScheduleDayOfWeek.FRIDAY],
      dayOfMonth: 0,
    });
    expect(curtailmentScheduleToApi({ ...recurring, frequency: "monthly" })).toMatchObject({
      daysOfWeek: [],
      dayOfMonth: 15,
    });
  });
});
