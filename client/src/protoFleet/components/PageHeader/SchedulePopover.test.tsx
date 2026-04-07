import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SchedulePopover from "./SchedulePopover";
import { ScheduleSchema } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";

const createSchedule = (id: string, name: string, status: ScheduleListItem["status"]): ScheduleListItem => ({
  id,
  priority: Number(id),
  name,
  targetSummary: "Applies to 1 miner",
  scheduleSummary: "Weekdays · 10:00 PM",
  nextRunSummary: "Runs tomorrow at 10:00 PM",
  action: "reboot",
  status,
  createdBy: "Review",
  rawSchedule: create(ScheduleSchema, {
    id: BigInt(id),
    name,
    startDate: "2026-04-07",
    startTime: "22:00",
    timezone: "UTC",
  }),
});

describe("SchedulePopover", () => {
  it("disables every toggle button while a schedule update is in flight", () => {
    render(
      <MemoryRouter>
        <SchedulePopover
          sections={[
            {
              id: "running",
              title: "Active now",
              schedules: [
                createSchedule("1", "Night reboot", "running"),
                createSchedule("2", "Weekend sleep", "paused"),
              ],
            },
          ]}
          pendingScheduleId="1"
          onToggleScheduleStatus={vi.fn()}
          onNavigateToSchedules={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "Pause" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Resume" })).toBeDisabled();
  });

  it("uses the standard hover background treatment for the schedules link", () => {
    render(
      <MemoryRouter>
        <SchedulePopover
          sections={[]}
          pendingScheduleId={null}
          onToggleScheduleStatus={vi.fn()}
          onNavigateToSchedules={vi.fn()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("link", { name: "View all schedules" })).toHaveClass(
      "hover:bg-core-primary-5",
      "px-3",
      "py-2.5",
    );
  });
});
