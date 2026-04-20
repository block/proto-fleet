import type { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SchedulePill from "./SchedulePill";
import {
  buildSchedulePopoverSections,
  getSchedulePopoverActionSummary,
  getSchedulePopoverTargetSummary,
  selectPillSchedule,
} from "./schedulePillUtils";
import {
  PowerTargetMode,
  ScheduleAction as ProtoScheduleAction,
  ScheduleSchema,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { Schedule as ProtoSchedule } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleAction, ScheduleListItem, ScheduleStatus } from "@/protoFleet/api/useScheduleApi";

vi.mock("./SchedulePopover", () => ({
  __esModule: true,
  default: () => <div>Popover content</div>,
}));

vi.mock("@/shared/components/Popover", () => ({
  __esModule: true,
  default: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  popoverSizes: {
    small: "small",
    medium: "medium",
    normal: "normal",
  },
  useResponsivePopover: () => ({
    triggerRef: { current: null },
  }),
}));

const createScheduleListItem = ({
  id,
  name,
  priority,
  status,
  action = "reboot",
  powerTargetMode = PowerTargetMode.DEFAULT,
  nextRunAt,
}: {
  id: string;
  name: string;
  priority: number;
  status: ScheduleStatus;
  action?: ScheduleAction;
  powerTargetMode?: PowerTargetMode;
  nextRunAt?: Date;
}): ScheduleListItem => {
  const protoAction =
    action === "setPowerTarget"
      ? ProtoScheduleAction.SET_POWER_TARGET
      : action === "sleep"
        ? ProtoScheduleAction.SLEEP
        : ProtoScheduleAction.REBOOT;

  return {
    id,
    priority,
    name,
    targetSummary: "Applies to 1 rack",
    scheduleSummary: "Weekdays · 10:00 PM",
    nextRunSummary: "Runs tomorrow at 10:00 PM",
    action,
    status,
    createdBy: "Negar",
    rawSchedule: create(ScheduleSchema, {
      id: BigInt(id),
      name,
      action: protoAction,
      actionConfig: {
        mode: powerTargetMode,
      },
      nextRunAt: nextRunAt
        ? {
            seconds: BigInt(Math.floor(nextRunAt.getTime() / 1000)),
            nanos: 0,
          }
        : undefined,
      startDate: "2026-04-07",
      startTime: "22:00",
      timezone: "UTC",
    }) as ProtoSchedule,
  };
};

describe("SchedulePill helpers", () => {
  it("groups schedules by header priority and limits the popover to three entries", () => {
    const sections = buildSchedulePopoverSections([
      createScheduleListItem({ id: "1", name: "Paused low", priority: 5, status: "paused" }),
      createScheduleListItem({ id: "2", name: "Running second", priority: 2, status: "running" }),
      createScheduleListItem({ id: "3", name: "Active first", priority: 1, status: "active" }),
      createScheduleListItem({ id: "4", name: "Running first", priority: 1, status: "running" }),
      createScheduleListItem({ id: "5", name: "Completed", priority: 3, status: "completed" }),
      createScheduleListItem({ id: "6", name: "Active second", priority: 4, status: "active" }),
    ]);

    expect(sections).toHaveLength(2);
    expect(sections[0]?.title).toBe("Active now");
    expect(sections[0]?.schedules.map((schedule) => schedule.name)).toEqual(["Running first", "Running second"]);
    expect(sections[1]?.title).toBe("Up next");
    expect(sections[1]?.schedules.map((schedule) => schedule.name)).toEqual(["Active first"]);
  });

  it("selects the pill schedule with running schedules first, then active, then paused", () => {
    const runningSections = buildSchedulePopoverSections([
      createScheduleListItem({ id: "1", name: "Paused", priority: 2, status: "paused" }),
      createScheduleListItem({ id: "2", name: "Running", priority: 3, status: "running" }),
      createScheduleListItem({ id: "3", name: "Active", priority: 1, status: "active" }),
    ]);

    const activeSections = buildSchedulePopoverSections([
      createScheduleListItem({ id: "4", name: "Paused", priority: 2, status: "paused" }),
      createScheduleListItem({ id: "5", name: "Active", priority: 1, status: "active" }),
    ]);

    const pausedSections = buildSchedulePopoverSections([
      createScheduleListItem({ id: "6", name: "Paused only", priority: 1, status: "paused" }),
    ]);

    expect(selectPillSchedule(runningSections)?.name).toBe("Running");
    expect(selectPillSchedule(activeSections)?.name).toBe("Active");
    expect(selectPillSchedule(pausedSections)?.name).toBe("Paused only");
  });

  it("keeps the pill label live while the popover is open", () => {
    const runningSchedule = createScheduleListItem({ id: "1", name: "Running first", priority: 1, status: "running" });
    const activeSchedule = createScheduleListItem({ id: "2", name: "Active next", priority: 1, status: "active" });

    const { rerender } = render(
      <SchedulePill
        pillSchedule={runningSchedule}
        sections={[{ id: "running", title: "Active now", schedules: [runningSchedule] }]}
        pendingScheduleId={null}
        onToggleScheduleStatus={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View schedule details for Running first" }));

    expect(screen.getByText("Popover content")).toBeInTheDocument();
    expect(screen.getByText("Running first")).toBeInTheDocument();

    rerender(
      <SchedulePill
        pillSchedule={activeSchedule}
        sections={[{ id: "active", title: "Up next", schedules: [activeSchedule] }]}
        pendingScheduleId={null}
        onToggleScheduleStatus={vi.fn()}
      />,
    );

    expect(screen.getByText("Popover content")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View schedule details for Active next" })).toBeInTheDocument();
    expect(screen.getByText("Active next")).toBeInTheDocument();
    expect(screen.queryByText("Running first")).not.toBeInTheDocument();
  });

  it("uses the next scheduled occurrence in paused action summaries", () => {
    const nextRunAt = new Date("2026-04-11T22:00:00.000Z");
    const schedule = createScheduleListItem({
      id: "7",
      name: "Weekend sleep",
      priority: 1,
      status: "paused",
      action: "sleep",
      nextRunAt,
    });
    const expectedDateTime = new Intl.DateTimeFormat(undefined, {
      weekday: "short",
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }).format(nextRunAt);

    expect(getSchedulePopoverActionSummary("paused", schedule)).toBe(`Sleep · ${expectedDateTime}`);
  });

  it("keeps the fleet-wide target summary for schedules that apply to all miners", () => {
    const schedule = {
      ...createScheduleListItem({
        id: "8",
        name: "Fleet wide sleep",
        priority: 1,
        status: "active",
        action: "sleep",
      }),
      targetSummary: "Applies to all miners",
      rawSchedule: create(ScheduleSchema, {
        id: 8n,
        name: "Fleet wide sleep",
        action: ProtoScheduleAction.SLEEP,
        targets: [],
        startDate: "2026-04-07",
        startTime: "22:00",
        timezone: "UTC",
      }) as ProtoSchedule,
    };

    expect(getSchedulePopoverTargetSummary(schedule)).toBe("Applies to all miners");
  });
});
