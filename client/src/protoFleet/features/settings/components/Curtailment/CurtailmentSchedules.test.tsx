import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, onTestFinished, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { describeNextScheduleWindow } from "@/protoFleet/api/curtailmentSchedules";
import type { AutomationRule, ResponseProfile } from "@/protoFleet/api/curtailmentTypes";
import { CurtailmentScheduleDayOfWeek } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { CurtailmentSchedulesContent } from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSchedules";

const responseProfiles: ResponseProfile[] = [
  {
    id: "standard-shed",
    revision: "11111111-1111-4111-8111-111111111111",
    name: "Standard shed",
    targetSummary: "50% reduction",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    isAutomationReady: true,
  },
];

const weeknightRule: AutomationRule = {
  id: "9",
  priority: 1,
  name: "Weeknight peak",
  conditionType: "schedule",
  conditionSummary: "Weekly on Mon, Fri, 10:00 PM – 6:00 AM next day",
  schedule: {
    scheduleType: "recurring",
    timezone: "America/Chicago",
    startDate: "2026-06-01",
    startTime: "22:00",
    endTime: "06:00",
    endDate: "",
    frequency: "weekly",
    daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY, CurtailmentScheduleDayOfWeek.FRIDAY],
    dayOfMonth: "",
    untilDate: "",
  },
  windowStartAt: new Date("2099-06-12T03:00:00Z"),
  windowEndAt: new Date("2099-06-12T11:00:00Z"),
  responseProfileId: "standard-shed",
  enabled: true,
};

describe("CurtailmentSchedulesContent", () => {
  it("lists schedules with their window and response profile", () => {
    render(<CurtailmentSchedulesContent scheduleRules={[weeknightRule]} responseProfiles={responseProfiles} />);

    expect(screen.getByText("Schedules")).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "Next window" })).toBeVisible();
    const row = screen.getByText("Weeknight peak").closest("tr") as HTMLTableRowElement;
    expect(within(row).getByText(weeknightRule.conditionSummary)).toBeVisible();
    expect(within(row).getByText(describeNextScheduleWindow(weeknightRule))).toBeVisible();
    expect(within(row).getByText("Standard shed")).toBeVisible();
  });

  it("shows an empty state", () => {
    render(<CurtailmentSchedulesContent scheduleRules={[]} responseProfiles={responseProfiles} />);

    expect(screen.getByText("No schedules configured")).toBeVisible();
  });

  it("creates a one-time schedule after validating the form", async () => {
    // The form defaults to 17:00-21:00 today, which must still be in the future.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-06-11T12:00:00"));
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const user = userEvent.setup();
    const onCreateSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[]}
        responseProfiles={responseProfiles}
        onCreateSchedule={onCreateSchedule}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Create schedule" }));
    expect(screen.getByTestId("curtailment-schedule-modal")).toHaveTextContent("Create schedule");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByText("Enter a schedule name.")).toBeVisible());
    expect(onCreateSchedule).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Schedule name"), { target: { value: "Grid event" } });
    await user.click(screen.getByLabelText("Type"));
    await user.click(screen.getByText("One-time"));
    expect(screen.getByTestId("schedule-rule-end-date-trigger")).toBeInTheDocument();
    expect(screen.queryByTestId("schedule-rule-until-date-trigger")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onCreateSchedule).toHaveBeenCalledTimes(1));
    expect(onCreateSchedule).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Grid event",
        responseProfileId: "standard-shed",
        responseProfileRevision: responseProfiles[0].revision,
        schedule: expect.objectContaining({ scheduleType: "oneTime", startTime: "17:00", endTime: "21:00" }),
      }),
    );
    await waitFor(() => expect(screen.queryByTestId("curtailment-schedule-modal")).not.toBeInTheDocument());
  });

  it("edits an existing schedule with its saved values", async () => {
    const onUpdateSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[weeknightRule]}
        responseProfiles={responseProfiles}
        onUpdateSchedule={onUpdateSchedule}
      />,
    );

    fireEvent.click(screen.getByText("Weeknight peak"));
    expect(screen.getByTestId("curtailment-schedule-modal")).toHaveTextContent("Edit schedule");
    expect(screen.getByLabelText("Schedule name")).toHaveValue("Weeknight peak");
    expect(screen.getByText("Ends the next day.")).toBeInTheDocument();
    expect(screen.getByText(/All times America\/Chicago/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onUpdateSchedule).toHaveBeenCalledWith(
        expect.objectContaining({ id: weeknightRule.id }),
        expect.objectContaining({
          name: "Weeknight peak",
          schedule: expect.objectContaining({
            scheduleType: "recurring",
            timezone: "America/Chicago",
            startTime: "22:00",
            endTime: "06:00",
            frequency: "weekly",
            daysOfWeek: weeknightRule.schedule?.daysOfWeek,
          }),
        }),
      ),
    );
  });

  it("accepts any minute and flags an end time on the next day", async () => {
    const onUpdateSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[
          { ...weeknightRule, schedule: weeknightRule.schedule && { ...weeknightRule.schedule, startTime: "22:07" } },
        ]}
        responseProfiles={responseProfiles}
        onUpdateSchedule={onUpdateSchedule}
      />,
    );

    fireEvent.click(screen.getByText("Weeknight peak"));
    expect(screen.getByLabelText("Start time")).toHaveValue("22:07");
    expect(screen.getByLabelText("End time")).toHaveValue("06:00");
    expect(screen.getByText("Ends the next day.")).toBeInTheDocument();
    expect(screen.getByTestId("schedule-rule-duration")).toHaveTextContent(
      "Curtails for 7 hours 53 minutes each time.",
    );

    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: "05:13" } });
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: "09:41" } });
    expect(screen.queryByText("Ends the next day.")).not.toBeInTheDocument();
    expect(screen.getByTestId("schedule-rule-duration")).toHaveTextContent(
      "Curtails for 4 hours 28 minutes each time.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onUpdateSchedule).toHaveBeenCalledWith(
        expect.objectContaining({ id: weeknightRule.id }),
        expect.objectContaining({
          schedule: expect.objectContaining({ startTime: "05:13", endTime: "09:41" }),
        }),
      ),
    );
  });

  it("notes the next recurring window that a daylight saving time change lengthens", () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-20T12:00:00Z"));
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const dailyRule: AutomationRule = {
      ...weeknightRule,
      schedule: weeknightRule.schedule && { ...weeknightRule.schedule, frequency: "daily", daysOfWeek: [] },
    };
    render(<CurtailmentSchedulesContent scheduleRules={[dailyRule]} responseProfiles={responseProfiles} />);

    fireEvent.click(screen.getByText("Weeknight peak"));

    expect(screen.getByTestId("schedule-rule-duration")).toHaveTextContent("Curtails for 8 hours each time.");
    expect(
      screen.getByText("A daylight saving time change makes the window starting Oct 31, 2026 run 1 hour longer."),
    ).toBeInTheDocument();
  });

  it("shows a one-time window's actual duration across a daylight saving time change", () => {
    const oneTimeRule: AutomationRule = {
      ...weeknightRule,
      schedule: weeknightRule.schedule && {
        ...weeknightRule.schedule,
        scheduleType: "oneTime",
        startDate: "2026-10-31",
        endDate: "2026-11-01",
        daysOfWeek: [],
      },
    };
    render(<CurtailmentSchedulesContent scheduleRules={[oneTimeRule]} responseProfiles={responseProfiles} />);

    fireEvent.click(screen.getByText("Weeknight peak"));

    expect(screen.getByTestId("schedule-rule-duration")).toHaveTextContent("Curtails for 9 hours.");
    expect(
      screen.getByText(
        "A daylight saving time change makes this window run 1 hour longer than its start and end times suggest.",
      ),
    ).toBeInTheDocument();
  });

  it("applies a typed date on Enter without saving the previous date", async () => {
    const user = userEvent.setup();
    const onUpdateSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[weeknightRule]}
        responseProfiles={responseProfiles}
        onUpdateSchedule={onUpdateSchedule}
      />,
    );

    fireEvent.click(screen.getByText("Weeknight peak"));
    await user.click(screen.getByTestId("schedule-rule-start-date-trigger"));
    const dateInput = screen.getByTestId("schedule-rule-start-date-inputs-date-input");
    await user.clear(dateInput);
    await user.type(dateInput, "2026-07-04{Enter}");

    expect(onUpdateSchedule).not.toHaveBeenCalled();
    expect(screen.getByTestId("schedule-rule-start-date-trigger")).toHaveTextContent("Jul 4, 2026");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onUpdateSchedule).toHaveBeenCalledWith(
        expect.objectContaining({ id: weeknightRule.id }),
        expect.objectContaining({ schedule: expect.objectContaining({ startDate: "2026-07-04" }) }),
      ),
    );
  });

  it("disables enabling a schedule whose response profile is not automation-ready", () => {
    const onToggleSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[{ ...weeknightRule, enabled: false }]}
        responseProfiles={[{ ...responseProfiles[0], isAutomationReady: false }]}
        onToggleSchedule={onToggleSchedule}
      />,
    );

    expect(screen.getByRole("checkbox")).toBeDisabled();
  });

  it("disables an enabled schedule without a profile revision", () => {
    const onToggleSchedule = vi.fn().mockResolvedValue(undefined);
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[weeknightRule]}
        responseProfiles={responseProfiles}
        onToggleSchedule={onToggleSchedule}
      />,
    );

    fireEvent.click(screen.getByRole("checkbox"));

    expect(onToggleSchedule).toHaveBeenCalledWith(weeknightRule, false, undefined);
  });

  it("sends the response profile revision when enabling a schedule", () => {
    const onToggleSchedule = vi.fn().mockResolvedValue(undefined);
    const disabledRule = { ...weeknightRule, enabled: false };
    render(
      <CurtailmentSchedulesContent
        scheduleRules={[disabledRule]}
        responseProfiles={responseProfiles}
        onToggleSchedule={onToggleSchedule}
      />,
    );

    fireEvent.click(screen.getByRole("checkbox"));

    expect(onToggleSchedule).toHaveBeenCalledWith(disabledRule, true, responseProfiles[0].revision);
  });
});

describe("describeNextScheduleWindow", () => {
  const rule: AutomationRule = {
    ...weeknightRule,
    windowStartAt: new Date("2026-06-12T03:00:00Z"),
    windowEndAt: new Date("2026-06-12T11:00:00Z"),
  };

  it("describes an upcoming, active, or missing window in the schedule's timezone", () => {
    const chicagoWindowStart = new Intl.DateTimeFormat(undefined, {
      timeZone: "America/Chicago",
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }).format(rule.windowStartAt);
    expect(describeNextScheduleWindow(rule, new Date("2026-06-12T01:00:00Z"))).toBe(chicagoWindowStart);
    expect(describeNextScheduleWindow(rule, new Date("2026-06-12T04:00:00Z"))).toMatch(/^Active until /);
    expect(describeNextScheduleWindow({ ...rule, windowStartAt: undefined, windowEndAt: undefined })).toBe(
      "No upcoming windows",
    );
  });
});
