import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

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

function expectProgressHidden(): void {
  expect(screen.queryByTestId("active-curtailment-progress")).not.toBeInTheDocument();
}

function expectPrimaryLockup(value: string): void {
  const lockup = within(screen.getByTestId("active-curtailment-primary-lockup"));

  expect(lockup.getByText("Dispatch status")).toBeVisible();
  expect(lockup.getByText(value)).toBeVisible();
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
  it("renders a curtailing event with stop available and no manage action", async () => {
    const user = userEvent.setup();
    const onRequestStop = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailingCurtailmentEvent} onRequestStop={onRequestStop} />);

    expect(screen.getByText("Active curtailment")).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation (Applies to Rockdale, TX)")).toBeVisible();
    expectPrimaryLockup("Curtailing");
    expect(screen.getByText("Power to shed")).toBeVisible();
    expect(screen.getByText("60.0 kW")).toBeVisible();
    expect(screen.getAllByText("Curtailing")[0]).toBeVisible();
    expect(screen.getByText("10 miners every 120s")).toBeVisible();
    expectProgressHidden();
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Stop" }));

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("renders a pending event with stop available", async () => {
    const user = userEvent.setup();
    const onRequestStop = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          observedReductionKw: 0,
          rollups: [{ state: "pending", count: curtailingCurtailmentEvent.selectedMiners }],
          state: "pending",
        }}
        onRequestStop={onRequestStop}
      />,
    );

    expectPrimaryLockup("Pending");
    expect(screen.getByText("Power to shed")).toBeVisible();
    expect(screen.getByText("60.0 kW")).toBeVisible();
    expect(screen.getAllByText("Pending")[0]).toBeVisible();
    expect(screen.queryByText("Curtailing")).not.toBeInTheDocument();
    expectProgressHidden();
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Stop" }));

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("renders pending events as curtailing once dispatch has started", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          observedReductionKw: 0,
          rollups: [
            { state: "dispatched", count: 1 },
            { state: "pending", count: curtailingCurtailmentEvent.selectedMiners - 1 },
          ],
          state: "pending",
        }}
      />,
    );

    expectPrimaryLockup("Curtailing");
    expect(screen.getAllByText("Curtailing")[0]).toBeVisible();
    expect(screen.queryByText("Pending")).not.toBeInTheDocument();
  });

  it("calls the manage handler when edit is available", async () => {
    const user = userEvent.setup();
    const onRequestEdit = vi.fn();
    const onRequestStop = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={curtailingCurtailmentEvent}
        onRequestEdit={onRequestEdit}
        onRequestStop={onRequestStop}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Manage" }));

    expect(onRequestEdit).toHaveBeenCalledOnce();
    expect(onRequestStop).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Stop" })).toBeVisible();
  });

  it("does not show manage while restoring", () => {
    const onRequestEdit = vi.fn();

    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} onRequestEdit={onRequestEdit} />);

    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
    expect(onRequestEdit).not.toHaveBeenCalled();
  });

  it("renders a curtailed event with restore available", async () => {
    const user = userEvent.setup();
    const onRequestRestore = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailedCurtailmentEvent} onRequestRestore={onRequestRestore} />);

    expectPrimaryLockup("Curtailed");
    expect(screen.getByText("Power to shed")).toBeVisible();
    expect(screen.getByText("60.0 kW")).toBeVisible();
    expect(screen.getAllByText("Curtailed")[0]).toBeVisible();
    expectProgressHidden();
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");

    await user.click(screen.getByRole("button", { name: "Restore" }));

    expect(onRequestRestore).toHaveBeenCalledOnce();
  });

  it("renders automation recovery context without normal restore", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailedCurtailmentEvent,
          isAutomationOwned: true,
          sourceLabel: "Curtailment automation",
        }}
      />,
    );

    expect(screen.getByText("Curtailment automation recovery")).toBeVisible();
    expect(screen.getByText(/Abort cancels this event and disables the owning automation rule/)).toBeVisible();
    expectActionButtonHidden("Restore");
  });

  it("renders incomplete target site coverage warning", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailedCurtailmentEvent,
          targetSiteCoverage: {
            complete: false,
            targetCount: 52,
            mappedTargetCount: 50,
            unknownTargetCount: 2,
          },
        }}
        onRequestForceRelease={vi.fn()}
      />,
    );

    expect(screen.getByText("Target site coverage incomplete")).toBeVisible();
    expect(screen.getByText(/2 targets no longer map to a known site/)).toBeVisible();
    expect(screen.getByText(/Org admins can still stop or abort this event/)).toBeVisible();
  });

  it("renders a restoring event without stop, restore, or manage actions", () => {
    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} />);

    expectPrimaryLockup("Restoring");
    expect(screen.getByText("Power to restore")).toBeVisible();
    expect(screen.getByText("60.0 kW")).toBeVisible();
    expect(screen.getByText("Restoring")).toBeVisible();
    expect(screen.getByText("10 miners every 120s")).toBeVisible();
    expect(screen.getByText("Estimated time to restore")).toBeVisible();
    expect(screen.getByText("Immediate")).toBeVisible();
    expect(screen.queryByText("Estimated completion")).not.toBeInTheDocument();
    expectProgressHidden();
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
  });

  it("renders terminate recovery while restoring when recovery is available", async () => {
    const user = userEvent.setup();
    const onRequestTerminateRecovery = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          isAutomationOwned: true,
          sourceLabel: "Curtailment automation",
        }}
        onRequestTerminateRecovery={onRequestTerminateRecovery}
      />,
    );

    expect(screen.getByText("Curtailment automation recovery")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Terminate recovery" }));

    expect(onRequestTerminateRecovery).toHaveBeenCalledOnce();
  });

  it("labels abort action for restoring events", async () => {
    const user = userEvent.setup();
    const onRequestForceRelease = vi.fn();

    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} onRequestForceRelease={onRequestForceRelease} />);

    await user.click(screen.getByRole("button", { name: "Abort restore" }));

    expect(onRequestForceRelease).toHaveBeenCalledOnce();
  });

  it("labels abort action for automation-owned active events", async () => {
    const user = userEvent.setup();
    const onRequestForceRelease = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          isAutomationOwned: true,
          sourceLabel: "Curtailment automation",
        }}
        onRequestForceRelease={onRequestForceRelease}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Abort curtailment" }));

    expect(onRequestForceRelease).toHaveBeenCalledOnce();
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

    expect(screen.getByText("Power to restore")).toBeVisible();
    expect(screen.getByText("60.0 kW")).toBeVisible();
    expectProgressHidden();
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
      expectProgressHidden();
    } finally {
      vi.useRealTimers();
    }
  });

  it("excludes failed targets from the restoring completion estimate", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:00:00-04:00"));

    try {
      render(
        <ActiveCurtailmentStatus
          event={{
            ...restoringCurtailmentEvent,
            restoreBatchSize: 5,
            rollups: [
              { state: "resolved", count: 10 },
              { state: "restoreFailed", count: 8 },
            ],
            selectedMiners: 18,
          }}
        />,
      );

      expect(screen.getByText("Estimated time to restore")).toBeVisible();
      expect(screen.getByText("Immediate")).toBeVisible();
      expect(screen.queryByText("Estimated completion")).not.toBeInTheDocument();
      expect(screen.queryByText(formatExpectedDateTime("2026-05-01T10:02:00-04:00"))).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders an unavailable restore completion when the estimate is out of range", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          restoreBatchIntervalSec: Number.MAX_SAFE_INTEGER,
          restoreBatchSize: 1,
          rollups: [
            { state: "resolved", count: 0 },
            { state: "confirmed", count: 2 },
          ],
          selectedMiners: 2,
        }}
      />,
    );

    expect(screen.queryByText("Estimated completion")).not.toBeInTheDocument();
    expect(screen.queryByText("Time unavailable")).not.toBeInTheDocument();
  });

  it("renders a restored event with dismiss available", async () => {
    const user = userEvent.setup();
    const onDismissRestored = vi.fn();

    render(<ActiveCurtailmentStatus event={restoredCurtailmentEvent} onDismissRestored={onDismissRestored} />);

    expectPrimaryLockup("Restored");
    expect(screen.getByText("Power restored")).toBeVisible();
    expect(screen.getByText("60.0 kW restored")).toBeVisible();
    expect(screen.getAllByText("Restored")[0]).toBeVisible();
    expect(screen.getByText("Time to restore")).toBeVisible();
    expect(screen.getByText("2 minutes")).toBeVisible();
    expectProgressHidden();
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
  });

  it("shows an unavailable completed time when a restored event has no end time", () => {
    render(<ActiveCurtailmentStatus event={{ ...restoredCurtailmentEvent, endedAt: undefined }} />);

    expect(screen.getByText("Completed")).toBeVisible();
    expect(screen.getByText("Time unavailable")).toBeVisible();
  });

  it("uses rollup totals for terminal restore duration when selected miner count is stale", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoredCurtailmentEvent,
          rollups: [{ state: "resolved", count: 25 }],
          selectedMiners: 0,
        }}
      />,
    );

    expect(screen.getByText("Time to restore")).toBeVisible();
    expect(screen.getByText("4 minutes")).toBeVisible();
  });

  it("renders a completed-with-failures event as an incomplete restore", async () => {
    const user = userEvent.setup();
    const onDismissRestored = vi.fn();

    render(<ActiveCurtailmentStatus event={restoreIncompleteCurtailmentEvent} onDismissRestored={onDismissRestored} />);

    expectPrimaryLockup("Restore incomplete");
    expect(screen.getByText("Power to restore")).toBeVisible();
    expect(screen.getByText("60.0 kW restore requested")).toBeVisible();
    expect(screen.getByText("Restore incomplete")).toBeVisible();
    expect(screen.getByText("Failed to restore")).toBeVisible();
    expect(screen.getByText("1 miner")).toBeVisible();
    expect(screen.queryByText("60.0 kW restored")).not.toBeInTheDocument();
    expectProgressHidden();
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
  });

  it("renders failed and cancelled events without active controls", () => {
    const onDismissRestored = vi.fn();
    const onRequestRestore = vi.fn();
    const onRequestStop = vi.fn();

    const { rerender } = render(
      <ActiveCurtailmentStatus
        event={{ ...curtailingCurtailmentEvent, state: "failed" }}
        onDismissRestored={onDismissRestored}
        onRequestRestore={onRequestRestore}
        onRequestStop={onRequestStop}
      />,
    );

    expect(screen.getByText("Failed")).toBeVisible();
    expectActionButtonHidden("Dismiss");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");

    rerender(
      <ActiveCurtailmentStatus
        event={{ ...curtailedCurtailmentEvent, state: "cancelled" }}
        onDismissRestored={onDismissRestored}
        onRequestRestore={onRequestRestore}
        onRequestStop={onRequestStop}
      />,
    );

    expect(screen.getByText("Cancelled")).toBeVisible();
    expectActionButtonHidden("Dismiss");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
  });
});
