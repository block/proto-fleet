import type { ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ScheduleModal from "./ScheduleModal";
import {
  ScheduleAction,
  ScheduleSchema,
  ScheduleStatus,
  ScheduleType,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import { getNextEndTimeAfterStart } from "@/protoFleet/features/settings/components/Schedules/scheduleValidation";

const { listRacksMock, listGroupsMock, pushToastMock } = vi.hoisted(() => ({
  listRacksMock: vi.fn(),
  listGroupsMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listRacks: listRacksMock,
    listGroups: listGroupsMock,
  }),
}));

vi.mock("@/protoFleet/api/useFleet", () => ({
  __esModule: true,
  default: () => ({
    totalMiners: 1,
    hasInitialLoadCompleted: true,
  }),
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/SchedulePreview", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/RackSelectionModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/GroupSelectionModal", () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock("@/shared/components/PageOverlay", () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/shared/components/Header", () => ({
  __esModule: true,
  default: ({
    title,
    buttons,
  }: {
    title: string;
    buttons?: Array<{ text: string; onClick?: () => void; disabled?: boolean }>;
  }) => (
    <div>
      <div>{title}</div>
      {buttons?.map((button) => (
        <button key={button.text} type="button" onClick={button.onClick} disabled={button.disabled}>
          {button.text}
        </button>
      ))}
    </div>
  ),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: (...args: unknown[]) => pushToastMock(...args),
  STATUSES: {
    success: "success",
    error: "error",
  },
}));

const createScheduleListItem = (
  status: ScheduleListItem["status"],
  overrides: Partial<{ timezone: string }> = {},
): ScheduleListItem => {
  const rawSchedule = create(ScheduleSchema, {
    id: 7n,
    name: "Night reboot",
    action: ScheduleAction.REBOOT,
    scheduleType: ScheduleType.ONE_TIME,
    startDate: "2026-04-10",
    startTime: "09:00",
    timezone: overrides.timezone ?? "UTC",
    status: status === "paused" ? ScheduleStatus.PAUSED : ScheduleStatus.RUNNING,
    createdBy: 1n,
  });

  return {
    id: "7",
    priority: 1,
    name: "Night reboot",
    targetSummary: "Applies to all miners",
    scheduleSummary: "Apr 10, 2026, 9:00 AM",
    nextRunSummary: "Runs on Apr 10, 2026, 9:00 AM",
    action: "reboot",
    status,
    createdBy: "Negar",
    rawSchedule,
  };
};

const renderScheduleModal = (schedule: ScheduleListItem) =>
  render(
    <ScheduleModal
      open
      schedule={schedule}
      onDismiss={vi.fn()}
      onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
      onUpdateSchedule={vi.fn().mockResolvedValue(undefined)}
      onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
      onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
      onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
    />,
  );

describe("ScheduleModal", () => {
  beforeEach(() => {
    listRacksMock.mockReset();
    listRacksMock.mockImplementation(() => undefined);
    listGroupsMock.mockReset();
    listGroupsMock.mockImplementation(() => undefined);
    pushToastMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("preserves draft edits when the same schedule rerenders with an updated status", async () => {
    const user = userEvent.setup();
    const runningSchedule = createScheduleListItem("running");
    const { rerender } = renderScheduleModal(runningSchedule);

    const nameInput = screen.getByLabelText("Schedule name");
    await user.clear(nameInput);
    await user.type(nameInput, "Draft reboot");

    expect(screen.getByLabelText("Schedule name")).toHaveValue("Draft reboot");

    rerender(
      <ScheduleModal
        open
        schedule={createScheduleListItem("paused")}
        onDismiss={vi.fn()}
        onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
        onUpdateSchedule={vi.fn().mockResolvedValue(undefined)}
        onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
        onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
        onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByLabelText("Schedule name")).toHaveValue("Draft reboot");
  });

  it("shows the schedule timezone when editing an existing schedule", async () => {
    renderScheduleModal(createScheduleListItem("running", { timezone: "America/Chicago" }));

    await waitFor(() => {
      expect(screen.getByText(/All times America\/Chicago/i)).toBeVisible();
    });
  });

  it("loads the full rack list when the modal opens", () => {
    renderScheduleModal(createScheduleListItem("running"));

    expect(listRacksMock).toHaveBeenCalledWith(expect.not.objectContaining({ pageSize: expect.anything() }));
  });

  it("wraps the default recurring end time to midnight after the last time option", () => {
    expect(getNextEndTimeAfterStart("23:45")).toBe("00:00");
  });

  it("disables Save when editing a running schedule", () => {
    renderScheduleModal(createScheduleListItem("running"));

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("saves the start date selected from the date picker", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-04-08T12:00:00"));
    const user = userEvent.setup();
    const onUpdateSchedule = vi.fn().mockResolvedValue(undefined);

    render(
      <ScheduleModal
        open
        schedule={createScheduleListItem("paused")}
        onDismiss={vi.fn()}
        onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
        onUpdateSchedule={onUpdateSchedule}
        onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
        onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
        onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    fireEvent.click(screen.getByTestId("schedule-start-date-trigger"));
    fireEvent.click(screen.getByTestId("schedule-start-date-calendar-day-12"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(onUpdateSchedule).toHaveBeenCalledWith(
        expect.objectContaining({
          startDate: "2026-04-12",
        }),
      );
    });
  });

  it("shows the end date validation error after the date picker closes without a selection", async () => {
    const user = userEvent.setup();

    render(
      <ScheduleModal
        open
        onDismiss={vi.fn()}
        onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
        onUpdateSchedule={vi.fn().mockResolvedValue(undefined)}
        onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
        onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
        onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await user.click(screen.getByLabelText("Type"));
    await user.click(screen.getByText("Recurring"));
    await user.click(screen.getByLabelText("End behavior"));
    await user.click(screen.getByText("End on date"));

    fireEvent.click(screen.getByTestId("schedule-end-date-trigger"));
    await user.click(screen.getByLabelText("Schedule name"));

    await waitFor(() => {
      expect(screen.getByText("Select an end date")).toBeVisible();
    });
  });

  it("shows the end date validation error after keyboard focus leaves the open date picker", async () => {
    render(
      <ScheduleModal
        open
        onDismiss={vi.fn()}
        onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
        onUpdateSchedule={vi.fn().mockResolvedValue(undefined)}
        onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
        onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
        onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await userEvent.click(screen.getByLabelText("Type"));
    await userEvent.click(screen.getByText("Recurring"));
    await userEvent.click(screen.getByLabelText("End behavior"));
    await userEvent.click(screen.getByText("End on date"));

    fireEvent.click(screen.getByTestId("schedule-end-date-trigger"));
    fireEvent.focusIn(screen.getByLabelText("Previous month"));
    fireEvent.focusIn(screen.getByLabelText("Schedule name"));

    await waitFor(() => {
      expect(screen.getByText("Select an end date")).toBeVisible();
    });
  });

  it("shows the end date validation error when the field blurs without opening the picker", async () => {
    render(
      <ScheduleModal
        open
        onDismiss={vi.fn()}
        onCreateSchedule={vi.fn().mockResolvedValue(undefined)}
        onUpdateSchedule={vi.fn().mockResolvedValue(undefined)}
        onDeleteSchedule={vi.fn().mockResolvedValue(undefined)}
        onPauseSchedule={vi.fn().mockResolvedValue(undefined)}
        onResumeSchedule={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    await userEvent.click(screen.getByLabelText("Type"));
    await userEvent.click(screen.getByText("Recurring"));
    await userEvent.click(screen.getByLabelText("End behavior"));
    await userEvent.click(screen.getByText("End on date"));

    const endDateTrigger = screen.getByTestId("schedule-end-date-trigger");
    fireEvent.focus(endDateTrigger);
    fireEvent.blur(endDateTrigger);

    await waitFor(() => {
      expect(screen.getByText("Select an end date")).toBeVisible();
    });
  });
});
