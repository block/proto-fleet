import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

const { listRacksMock, pushToastMock } = vi.hoisted(() => ({
  listRacksMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listRacks: listRacksMock,
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
    pushToastMock.mockReset();
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

  it("shows the schedule timezone when editing an existing schedule", () => {
    renderScheduleModal(createScheduleListItem("running", { timezone: "America/Chicago" }));

    expect(screen.getByText(/All times America\/Chicago/i)).toBeVisible();
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
});
