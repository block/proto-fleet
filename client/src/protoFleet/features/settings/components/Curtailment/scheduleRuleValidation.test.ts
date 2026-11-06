import { describe, expect, it } from "vitest";

import type { ScheduleRuleFormValues } from "@/protoFleet/api/curtailmentTypes";
import { CurtailmentScheduleDayOfWeek } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { validateScheduleRuleFormValues } from "@/protoFleet/features/settings/components/Curtailment/scheduleRuleValidation";

const now = new Date("2026-06-11T12:00:00Z");

const values: ScheduleRuleFormValues = {
  name: "Evening peak",
  responseProfileId: "21",
  responseProfileRevision: "11111111-1111-4111-8111-111111111111",
  schedule: {
    scheduleType: "recurring",
    timezone: "UTC",
    startDate: "2026-06-11",
    startTime: "17:00",
    endTime: "21:00",
    endDate: "2026-06-11",
    frequency: "daily",
    daysOfWeek: [],
    dayOfMonth: "11",
    untilDate: "",
  },
};

const withSchedule = (patch: Partial<ScheduleRuleFormValues["schedule"]>): ScheduleRuleFormValues => ({
  ...values,
  schedule: { ...values.schedule, ...patch },
});

describe("validateScheduleRuleFormValues", () => {
  it("accepts a valid recurring schedule", () => {
    expect(validateScheduleRuleFormValues(values, now)).toEqual({});
    expect(validateScheduleRuleFormValues(withSchedule({ startTime: "22:00", endTime: "06:00" }), now)).toEqual({});
  });

  it("requires a name and an automation-ready response profile", () => {
    expect(validateScheduleRuleFormValues({ ...values, name: " " }, now).name).toBe("Enter a schedule name.");
    expect(validateScheduleRuleFormValues({ ...values, responseProfileRevision: "" }, now).responseProfileId).toBe(
      "Reload response profiles before saving.",
    );
  });

  it("validates recurring fields", () => {
    expect(validateScheduleRuleFormValues(withSchedule({ endTime: "17:00" }), now).endTime).toBe(
      "End time must differ from start time.",
    );
    expect(validateScheduleRuleFormValues(withSchedule({ frequency: "weekly" }), now).daysOfWeek).toBe(
      "Select at least one day.",
    );
    expect(
      validateScheduleRuleFormValues(
        withSchedule({ frequency: "weekly", daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY] }),
        now,
      ),
    ).toEqual({});
    expect(
      validateScheduleRuleFormValues(withSchedule({ frequency: "monthly", dayOfMonth: "32" }), now).dayOfMonth,
    ).toBe("Enter a day from 1 to 31.");
    expect(validateScheduleRuleFormValues(withSchedule({ untilDate: "2026-06-10" }), now).untilDate).toBe(
      "Repeat until must be on or after the start date.",
    );
    expect(
      validateScheduleRuleFormValues(withSchedule({ startDate: "2026-06-01", untilDate: "2026-06-10" }), now).untilDate,
    ).toBe("Repeat until must be today or later.");
  });

  it("validates one-time windows", () => {
    const oneTime = (patch: Partial<ScheduleRuleFormValues["schedule"]>) =>
      withSchedule({ scheduleType: "oneTime", ...patch });

    expect(validateScheduleRuleFormValues(oneTime({}), now)).toEqual({});
    expect(validateScheduleRuleFormValues(oneTime({ endTime: "16:00" }), now).endTime).toBe("End must be after start.");
    expect(validateScheduleRuleFormValues(oneTime({ endDate: "2026-06-18" }), now).endDate).toBe(
      "One-time schedules can last up to 7 days.",
    );
    expect(validateScheduleRuleFormValues(oneTime({ endDate: "2026-06-18", endTime: "17:00" }), now)).toEqual({});
    expect(
      validateScheduleRuleFormValues(oneTime({ startDate: "2026-06-10", endDate: "2026-06-10" }), now).endTime,
    ).toBe("End must be in the future.");
    expect(validateScheduleRuleFormValues(oneTime({ endDate: "" }), now).endDate).toBe("Select an end date.");
  });

  it("checks one-time windows across daylight saving time changes like the server", () => {
    const oneTime = (patch: Partial<ScheduleRuleFormValues["schedule"]>) =>
      withSchedule({ scheduleType: "oneTime", ...patch });

    // 02:30 and 02:15 on 2027-03-14 are skipped in New York and run at 03:30 and 03:15.
    expect(
      validateScheduleRuleFormValues(
        oneTime({
          timezone: "America/New_York",
          startDate: "2027-03-14",
          startTime: "02:30",
          endDate: "2027-03-14",
          endTime: "02:15",
        }),
        now,
      ).endTime,
    ).toBe("End must be after start.");
    // 02:30 on 2027-10-31 repeats in Berlin; its first occurrence keeps this window at exactly 7 days.
    expect(
      validateScheduleRuleFormValues(
        oneTime({
          timezone: "Europe/Berlin",
          startDate: "2027-10-24",
          startTime: "02:30",
          endDate: "2027-10-31",
          endTime: "02:30",
        }),
        now,
      ),
    ).toEqual({});
  });
});
