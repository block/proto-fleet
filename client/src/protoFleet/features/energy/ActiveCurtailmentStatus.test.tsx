import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ActiveCurtailmentStatus from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  curtailedCurtailmentEvent,
  curtailingCurtailmentEvent,
  restoredCurtailmentEvent,
  restoreIncompleteCurtailmentEvent,
  restoringCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus.fixtures";

function expectActionButtonHidden(name: string): void {
  expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
}

function expectProgressValue(value: string): void {
  expect(screen.getByTestId("active-curtailment-progress")).toHaveAttribute("aria-valuenow", value);
}

function formatExpectedDateTime(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}

describe("ActiveCurtailmentStatus", () => {
  it("renders a curtailing event with stop available and no manage action", () => {
    const onRequestStop = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailingCurtailmentEvent} onRequestStop={onRequestStop} />);

    expect(screen.getByText("Active curtailment")).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation (Applies to Rockdale, TX)")).toBeVisible();
    expect(screen.getByText("Power shed")).toBeVisible();
    expect(screen.getByText("59.4 kW of 60.0 kW")).toBeVisible();
    expect(screen.getAllByText("Curtailing")[0]).toBeVisible();
    expect(screen.getByText("89% curtailed")).toBeVisible();
    expectProgressValue("89");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore");

    fireEvent.click(screen.getByRole("button", { name: "Stop" }));

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("renders a curtailed event with restore available", () => {
    const onRequestRestore = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailedCurtailmentEvent} onRequestRestore={onRequestRestore} />);

    expect(screen.getByText("60.0 kW of 60.0 kW")).toBeVisible();
    expect(screen.getAllByText("Curtailed")[0]).toBeVisible();
    expectProgressValue("100");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");

    fireEvent.click(screen.getByRole("button", { name: "Restore" }));

    expect(onRequestRestore).toHaveBeenCalledOnce();
  });

  it("renders a restoring event without stop, restore, or manage actions", () => {
    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} />);

    expect(screen.getByText("Power restore")).toBeVisible();
    expect(screen.getByText("26.7 kW of 60.0 kW restored")).toBeVisible();
    expect(screen.getByText("Restoring")).toBeVisible();
    expect(screen.getByText("10 miners every 120s")).toBeVisible();
    expect(screen.getByText("Estimated time to restore")).toBeVisible();
    expect(screen.getByText("Immediate")).toBeVisible();
    expectProgressValue("44");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
  });

  it("counts released targets as restored during restoration", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          rollups: [
            { state: "resolved", count: 8 },
            { state: "released", count: 2 },
            { state: "confirmed", count: 8 },
          ],
        }}
      />,
    );

    expect(screen.getByText("33.3 kW of 60.0 kW restored")).toBeVisible();
    expectProgressValue("56");
  });

  it("estimates restoring completion from the current time", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:00:00-04:00"));

    try {
      render(
        <ActiveCurtailmentStatus
          event={{
            ...restoringCurtailmentEvent,
            selectedMiners: 25,
            rollups: [
              { state: "resolved", count: 10 },
              { state: "confirmed", count: 15 },
            ],
          }}
        />,
      );

      expect(screen.getByText(formatExpectedDateTime("2026-05-01T10:02:00-04:00"))).toBeVisible();
    } finally {
      vi.useRealTimers();
    }
  });

  it("estimates restoring completion from rollup totals when selected miner count is stale", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:00:00-04:00"));

    try {
      render(
        <ActiveCurtailmentStatus
          event={{
            ...restoringCurtailmentEvent,
            selectedMiners: 0,
            rollups: [
              { state: "resolved", count: 10 },
              { state: "confirmed", count: 15 },
            ],
          }}
        />,
      );

      expect(screen.getByText(formatExpectedDateTime("2026-05-01T10:02:00-04:00"))).toBeVisible();
      expectProgressValue("40");
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders a restored event with dismiss available", () => {
    const onDismissRestored = vi.fn();

    render(<ActiveCurtailmentStatus event={restoredCurtailmentEvent} onDismissRestored={onDismissRestored} />);

    expect(screen.getByText("Power restore")).toBeVisible();
    expect(screen.getByText("60.0 kW restored")).toBeVisible();
    expect(screen.getAllByText("Restored")[0]).toBeVisible();
    expect(screen.getByText("Time to restore")).toBeVisible();
    expect(screen.getByText("2 minutes")).toBeVisible();
    expectProgressValue("100");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");

    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
  });

  it("renders a completed-with-failures event as an incomplete restore", () => {
    render(<ActiveCurtailmentStatus event={restoreIncompleteCurtailmentEvent} onDismissRestored={vi.fn()} />);

    expect(screen.getByText("Power restore")).toBeVisible();
    expect(screen.getByText("56.7 kW of 60.0 kW restored")).toBeVisible();
    expect(screen.getByText("Restore incomplete")).toBeVisible();
    expect(screen.getByText("Failed to restore")).toBeVisible();
    expect(screen.getByText("1 miner")).toBeVisible();
    expect(screen.getByText("Not restored")).toBeVisible();
    expect(screen.queryByText("60.0 kW restored")).not.toBeInTheDocument();
    expectProgressValue("94");
    expectActionButtonHidden("Dismiss");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
  });
});
