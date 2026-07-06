import { act, render, screen, within } from "@testing-library/react";
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

function expectProgressSummary(summary: string): void {
  const progress = within(screen.getByTestId("active-curtailment-progress"));
  expect(progress.getByText(summary)).toBeVisible();
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
    // 16 confirmed + 1 dispatched of 18 dispatchable; the drifted miner is
    // observed uncurtailed and must not count as reached.
    expectProgressSummary("17 of 18 miners reached (94%)");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Stop" }));

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("renders immediate restore profile for zero restore batch size", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          restoreBatchSize: 0,
          restoreBatchIntervalSec: 0,
        }}
      />,
    );

    expect(screen.getByText("Up to safety limit immediately")).toBeVisible();
  });

  it("renders paced safety-limited restore profile for zero restore batch size with interval", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          restoreBatchSize: 0,
          restoreBatchIntervalSec: 3600,
        }}
      />,
    );

    expect(screen.getByText("Up to safety limit every 3,600s")).toBeVisible();
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
    expectProgressSummary("0 of 18 miners reached (0%)");
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
    expectProgressSummary("18 of 18 miners reached (100%)");
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
    // 8 resolved of 18 restorable (9 confirmed awaiting + 1 restore-failed).
    expectProgressSummary("8 of 18 miners restored (44%)");
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
    // 8 resolved + 2 released of 18 restorable.
    expectProgressSummary("10 of 18 miners restored (55%)");
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
      expectProgressSummary("10 of 25 miners restored (40%)");
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
    expectProgressSummary("18 of 18 miners restored (100%)");
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
    expectProgressSummary("17 of 18 miners restored (94%)");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
  });

  it("renders curtail progress with segment legend and unavailable annotation", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          selectedMiners: 505,
          rollups: [
            { state: "confirmed", count: 300 },
            { state: "dispatched", count: 100 },
            { state: "drifted", count: 20 },
            { state: "pending", count: 80 },
            { state: "unavailable", count: 5 },
          ],
        }}
      />,
    );

    expectProgressSummary("400 of 500 miners reached (80%)");
    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.getByText("Confirmed quiet (300)")).toBeVisible();
    expect(progress.getByText("Command sent (100)")).toBeVisible();
    expect(progress.getByText("Drifted (20)")).toBeVisible();
    expect(progress.getByText("Pending (80)")).toBeVisible();
    expect(progress.getByText("5 unavailable (excluded)")).toBeVisible();
  });

  it("reaches full progress when only unavailable targets remain unreached", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          rollups: [
            { state: "confirmed", count: 9 },
            { state: "dispatched", count: 1 },
            { state: "unavailable", count: 40 },
          ],
        }}
      />,
    );

    expectProgressSummary("10 of 10 miners reached (100%)");
  });

  it("hides curtail progress when no live rollup data exists", () => {
    // startedAt is set so the absent Elapsed stat proves the live-data gate,
    // not just a missing timestamp.
    render(
      <ActiveCurtailmentStatus
        event={{ ...curtailingCurtailmentEvent, rollups: [], startedAt: "2026-05-01T10:00:00-04:00" }}
      />,
    );

    expectProgressHidden();
    expect(screen.queryByText("Elapsed")).not.toBeInTheDocument();
  });

  it("uses the singular miner label when one target is dispatchable", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          rollups: [{ state: "confirmed", count: 1 }],
        }}
      />,
    );

    expectProgressSummary("1 of 1 miner reached (100%)");
  });

  it("floors the reached percentage so completion is never overstated", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          rollups: [
            { state: "confirmed", count: 997 },
            { state: "pending", count: 3 },
          ],
        }}
      />,
    );

    // 997/1000 = 99.7% must not round up to 100% while targets are pending.
    expectProgressSummary("997 of 1,000 miners reached (99%)");
  });

  it("shows elapsed time from the event start and ticks it forward", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:03:12-04:00"));

    try {
      render(
        <ActiveCurtailmentStatus
          event={{
            ...curtailingCurtailmentEvent,
            startedAt: "2026-05-01T10:00:00-04:00",
          }}
        />,
      );

      expect(screen.getByText("Elapsed")).toBeVisible();
      expect(screen.getByText("3m 12s")).toBeVisible();

      act(() => {
        vi.advanceTimersByTime(3_000);
      });

      expect(screen.getByText("3m 15s")).toBeVisible();
    } finally {
      vi.useRealTimers();
    }
  });

  it("anchors elapsed time on creation when started_at is not yet stamped", () => {
    // Open-loop events only stamp started_at at the pending -> active
    // transition, after targets confirm — too late for a dispatch-window
    // timer. The clock falls back to when the operator started the event.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-05-01T10:01:30-04:00"));

    try {
      render(
        <ActiveCurtailmentStatus
          event={{
            ...curtailingCurtailmentEvent,
            startedAt: undefined,
            createdAt: "2026-05-01T10:00:00-04:00",
            rollups: [
              { state: "dispatched", count: 6 },
              { state: "pending", count: 12 },
            ],
            state: "pending",
          }}
        />,
      );

      expect(screen.getByText("Elapsed")).toBeVisible();
      expect(screen.getByText("1m 30s")).toBeVisible();
    } finally {
      vi.useRealTimers();
    }
  });

  it("omits elapsed time when no clock anchor exists", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          startedAt: undefined,
          rollups: [{ state: "pending", count: 18 }],
          state: "pending",
        }}
      />,
    );

    expect(screen.getByTestId("active-curtailment-progress")).toBeInTheDocument();
    expect(screen.queryByText("Elapsed")).not.toBeInTheDocument();
  });

  it("estimates remaining time to curtail from pending targets and batch pacing", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          curtailBatchSize: 10,
          curtailBatchIntervalSec: 60,
          rollups: [
            { state: "confirmed", count: 10 },
            { state: "pending", count: 25 },
          ],
        }}
      />,
    );

    // 10 confirmed already reached, so a wave is in flight: all 3 pending
    // waves (ceil(25 / 10)) wait on the 60s interval clock.
    expect(screen.getByText("Est. time to curtail")).toBeVisible();
    expect(screen.getByText("3 minutes")).toBeVisible();
  });

  it("does not charge an interval for the immediately dispatchable first wave", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          curtailBatchSize: 10,
          curtailBatchIntervalSec: 60,
          state: "pending",
          rollups: [{ state: "pending", count: 25 }],
        }}
      />,
    );

    // Nothing dispatched yet: the reconciler sends the first wave without
    // waiting on the interval clock, so only 2 of the 3 waves cost 60s.
    expect(screen.getByText("Est. time to curtail")).toBeVisible();
    expect(screen.getByText("2 minutes")).toBeVisible();
  });

  it.each([
    ["absent", undefined, undefined],
    ["zero-valued (proto3 unset)", 0, 0],
    ["partial (no interval)", 10, 0],
  ])("hides the curtail estimate when batch pacing is %s", (_label, curtailBatchSize, curtailBatchIntervalSec) => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          curtailBatchSize,
          curtailBatchIntervalSec,
          rollups: [
            { state: "confirmed", count: 10 },
            { state: "pending", count: 25 },
          ],
        }}
      />,
    );

    expect(screen.queryByText("Est. time to curtail")).not.toBeInTheDocument();
  });

  it("hides the curtail estimate once every target has been reached", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          curtailBatchSize: 10,
          curtailBatchIntervalSec: 60,
        }}
      />,
    );

    expect(screen.queryByText("Est. time to curtail")).not.toBeInTheDocument();
  });

  it("renders restore progress with segment legend and unavailable annotation", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          rollups: [
            { state: "resolved", count: 300 },
            { state: "released", count: 100 },
            { state: "restoreFailed", count: 20 },
            { state: "dispatched", count: 30 },
            { state: "confirmed", count: 50 },
            { state: "unavailable", count: 5 },
          ],
        }}
      />,
    );

    expectProgressSummary("400 of 500 miners restored (80%)");
    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.getByText("Restored (400)")).toBeVisible();
    expect(progress.getByText("Failed to restore (20)")).toBeVisible();
    expect(progress.getByText("Awaiting restore (80)")).toBeVisible();
    expect(progress.getByText("5 unavailable (excluded)")).toBeVisible();
  });

  it("hides restore progress when no live rollup data exists", () => {
    render(<ActiveCurtailmentStatus event={{ ...restoringCurtailmentEvent, rollups: [] }} />);

    expectProgressHidden();
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
    expectProgressHidden();
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
    expectProgressHidden();
    expectActionButtonHidden("Dismiss");
    expectActionButtonHidden("Stop");
    expectActionButtonHidden("Restore");
  });
});
