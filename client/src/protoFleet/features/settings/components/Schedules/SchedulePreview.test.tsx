import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import process from "node:process";

import SchedulePreview from "./SchedulePreview";
import { DayOfWeek } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { createDefaultScheduleFormValues } from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";

describe("SchedulePreview", () => {
  const originalTimeZone = process.env.TZ;

  beforeEach(() => {
    process.env.TZ = "UTC";
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-01T12:00:00.000Z"));
  });

  afterEach(() => {
    vi.useRealTimers();

    if (originalTimeZone === undefined) {
      delete process.env.TZ;
      return;
    }

    process.env.TZ = originalTimeZone;
  });

  it("renders weekly recurrence summaries with an explicit 'on' clause", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Midweek reboot",
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "weekly" as const,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.WEDNESDAY],
      startDate: "2026-04-03",
      startTime: "07:00",
    };

    render(<SchedulePreview values={values} />);

    expect(screen.getAllByText(/on Mon, Wed at/i)).not.toHaveLength(0);
    expect(screen.queryByText(/for all miners mon, Wed at/i)).not.toBeInTheDocument();
  });

  it("renders monthly recurrence summaries with an explicit day-of-month clause", () => {
    const values = {
      ...createDefaultScheduleFormValues("UTC"),
      name: "Monthly reboot",
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "monthly" as const,
      dayOfMonth: "5",
      startDate: "2026-04-05",
      startTime: "07:00",
    };

    render(<SchedulePreview values={values} />);

    expect(screen.getAllByText(/on the 5th day of month at/i)).not.toHaveLength(0);
  });

  it("formats one-time previews in the schedule timezone", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/Chicago"),
      name: "Morning reboot",
      action: "reboot" as const,
      scheduleType: "oneTime" as const,
      startDate: "2026-04-10",
      startTime: "07:00",
    };

    render(<SchedulePreview values={values} />);

    expect(screen.getAllByText(/7:00 AM/i)).not.toHaveLength(0);
    expect(screen.queryByText(/12:00 PM/i)).not.toBeInTheDocument();
  });

  it("keeps recurring start and end dates on the schedule's calendar day", () => {
    const values = {
      ...createDefaultScheduleFormValues("America/Chicago"),
      name: "Daily reboot",
      action: "reboot" as const,
      scheduleType: "recurring" as const,
      frequency: "daily" as const,
      startDate: "2026-04-10",
      startTime: "07:00",
      endBehavior: "endDate" as const,
      endDate: "2026-04-12",
    };

    render(<SchedulePreview values={values} />);

    expect(screen.getAllByText(/starting Apr 10, 2026 ending Apr 12, 2026/i)).not.toHaveLength(0);
    expect(screen.queryByText(/starting Apr 9, 2026/i)).not.toBeInTheDocument();
  });
});
