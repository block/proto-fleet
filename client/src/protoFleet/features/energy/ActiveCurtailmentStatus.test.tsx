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

function expectPrimaryLockup(label: string, value: string): void {
  const lockup = within(screen.getByTestId("active-curtailment-primary-lockup"));

  expect(lockup.getByText(label)).toBeVisible();
  expect(lockup.getByText(value)).toBeVisible();
}

function expectStat(label: string, value: string): void {
  const labelElement = screen.getByText(label);
  const stat = labelElement.parentElement;

  expect(stat).not.toBeNull();
  expect(within(stat as HTMLElement).getByText(value)).toBeVisible();
}

function expectInfrastructureStatus(value: string): void {
  expectStat("Infrastructure", value);
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
  it("renders a curtailing event with restore-now available and no manage action", async () => {
    const user = userEvent.setup();
    const onRequestStop = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailingCurtailmentEvent} onRequestStop={onRequestStop} />);

    expect(screen.getByText("Active curtailment")).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation (Applies to Rockdale, TX)")).toBeVisible();
    expectPrimaryLockup("Power shed", "59.4 of 60.0 kW");
    expectStat("Dispatch status", "Curtailing");
    expect(screen.getByText("18 miners")).toBeVisible();
    expect(screen.getAllByText("Curtailing")[0]).toBeVisible();
    expect(screen.getByText("10 miners every 120s")).toBeVisible();
    // 16 confirmed of 18 dispatchable; dispatched and drifted miners are still
    // in the curtailing bucket until telemetry confirms curtailment.
    expectProgressSummary("16 of 18 miners curtailed (88%)");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore");

    const restoreNowButton = screen.getByRole("button", { name: "Restore now" });
    expect(restoreNowButton).toHaveClass("bg-core-primary-5");
    expect(restoreNowButton).not.toHaveClass("bg-intent-critical-fill");

    await user.click(restoreNowButton);

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("includes infrastructure devices in the applies to summary and infrastructure status when present", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          facilityFanDeviceCount: 2,
        }}
      />,
    );

    expect(screen.getByText("18 miners, 2 devices")).toBeVisible();
    expectInfrastructureStatus("Curtailing");
  });

  it("renders infrastructure as curtailed after fan off is sent", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          facilityFanDeviceCount: 2,
          fanOffSentAt: "2026-05-01T12:05:00.000Z",
        }}
      />,
    );

    expectInfrastructureStatus("Curtailed");
  });

  it("renders reopened active infrastructure as curtailing until fans turn off again", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          facilityFanDeviceCount: 2,
          fanOffSentAt: "2026-05-01T12:05:00.000Z",
          fanAirflowReopenedAt: "2026-05-01T12:10:00.000Z",
        }}
      />,
    );

    expectInfrastructureStatus("Curtailing");
  });

  it("renders restoring infrastructure status during restore", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          facilityFanDeviceCount: 2,
        }}
      />,
    );

    expectInfrastructureStatus("Restoring");
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

  it("renders a pending event with restore-now available", async () => {
    const user = userEvent.setup();
    const onRequestStop = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          facilityFanDeviceCount: 2,
          observedReductionKw: 0,
          rollups: [{ state: "pending", count: curtailingCurtailmentEvent.selectedMiners }],
          state: "pending",
        }}
        onRequestStop={onRequestStop}
      />,
    );

    expectPrimaryLockup("Power shed", "0.0 of 60.0 kW");
    expectStat("Dispatch status", "Pending");
    expectInfrastructureStatus("Pending");
    expect(screen.getAllByText("Pending")[0]).toBeVisible();
    expectProgressSummary("0 of 18 miners curtailed (0%)");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Restore now" }));

    expect(onRequestStop).toHaveBeenCalledOnce();
  });

  it("renders pending infrastructure failures when fan command evidence exists", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          facilityFanDeviceCount: 2,
          fanOffSentAt: "2026-05-01T12:05:00.000Z",
          fanLastError: "fan command failed",
          observedReductionKw: 0,
          rollups: [{ state: "pending", count: curtailingCurtailmentEvent.selectedMiners }],
          state: "pending",
        }}
      />,
    );

    expectStat("Dispatch status", "Pending");
    expectInfrastructureStatus("Curtail failed");
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

    expectPrimaryLockup("Power shed", "0.0 of 60.0 kW");
    expectStat("Dispatch status", "Curtailing");
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
    expect(screen.getByRole("button", { name: "Restore now" })).toBeVisible();
  });

  it("does not show manage while restoring", () => {
    const onRequestEdit = vi.fn();

    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} onRequestEdit={onRequestEdit} />);

    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore now");
    expectActionButtonHidden("Restore");
    expect(onRequestEdit).not.toHaveBeenCalled();
  });

  it("renders a curtailed event with restore available", async () => {
    const user = userEvent.setup();
    const onRequestRestore = vi.fn();

    render(<ActiveCurtailmentStatus event={curtailedCurtailmentEvent} onRequestRestore={onRequestRestore} />);

    expectPrimaryLockup("Power shed", "60.0 of 60.0 kW");
    expectStat("Dispatch status", "Curtailed");
    expect(screen.getAllByText("Curtailed")[0]).toBeVisible();
    expectProgressSummary("18 miners curtailed (100%)");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore now");

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
    expect(screen.getByText(/Org admins can still restore or abort this event/)).toBeVisible();
  });

  it("renders a restoring event without stop, restore, or manage actions", () => {
    render(<ActiveCurtailmentStatus event={restoringCurtailmentEvent} />);

    expectPrimaryLockup("Power restored", "0.6 of 60.0 kW");
    expectStat("Dispatch status", "Restoring");
    expect(screen.getByText("Restoring")).toBeVisible();
    expect(screen.getByText("10 miners every 120s")).toBeVisible();
    expect(screen.getByText("Estimated time to restore")).toBeVisible();
    expect(screen.getByText("Immediate")).toBeVisible();
    expect(screen.queryByText("Estimated completion")).not.toBeInTheDocument();
    // 8 resolved of 18 restorable (9 confirmed awaiting + 1 restore-failed).
    expectProgressSummary("8 of 18 miners restored (44%)");
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Restore now");
    expectActionButtonHidden("Restore");
  });

  it("renders stop restore while restoring when recovery is available", async () => {
    const user = userEvent.setup();
    const onRequestTerminateRecovery = vi.fn();
    const onRequestForceRelease = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          isAutomationOwned: true,
          sourceLabel: "Curtailment automation",
        }}
        onRequestForceRelease={onRequestForceRelease}
        onRequestTerminateRecovery={onRequestTerminateRecovery}
      />,
    );

    expect(screen.getByText("Curtailment automation recovery")).toBeVisible();
    expectActionButtonHidden("Abort restore");
    await user.click(screen.getByRole("button", { name: "Stop restore" }));

    expect(onRequestTerminateRecovery).toHaveBeenCalledOnce();
    expect(onRequestForceRelease).not.toHaveBeenCalled();
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

    expect(screen.getByText("Power restored")).toBeVisible();
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
    const onRequestForceRelease = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoredCurtailmentEvent,
          startedAt: "2026-04-30T14:07:37-04:00",
          endedAt: "2026-04-30T14:08:00-04:00",
        }}
        onDismissRestored={onDismissRestored}
        onRequestForceRelease={onRequestForceRelease}
      />,
    );

    expectPrimaryLockup("Power restored", "60.0 of 60.0 kW");
    expectStat("Dispatch status", "Restored");
    expect(screen.getAllByText("Restored")[0]).toBeVisible();
    expect(screen.getByText("Time to restore")).toBeVisible();
    expect(screen.getByText("2 minutes")).toBeVisible();
    expectProgressSummary("18 miners restored (100%)");
    expect(within(screen.getByTestId("active-curtailment-progress")).getByText("23s elapsed")).toBeVisible();
    expectActionButtonHidden("Manage");
    expectActionButtonHidden("Abort curtailment");
    expectActionButtonHidden("Restore now");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
    expect(onRequestForceRelease).not.toHaveBeenCalled();
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
    const onRequestForceRelease = vi.fn();

    render(
      <ActiveCurtailmentStatus
        event={restoreIncompleteCurtailmentEvent}
        onDismissRestored={onDismissRestored}
        onRequestForceRelease={onRequestForceRelease}
      />,
    );

    expectPrimaryLockup("Power restored", "56.7 of 60.0 kW");
    expectStat("Dispatch status", "Restore incomplete");
    expect(screen.getByText("Restore incomplete")).toBeVisible();
    expect(screen.getByText("Failed to restore")).toBeVisible();
    expect(screen.getByText("1 miner")).toBeVisible();
    expect(screen.queryByText("60.0 kW restored")).not.toBeInTheDocument();
    expectProgressSummary("17 of 18 miners restored (94%)");
    expectActionButtonHidden("Abort curtailment");
    expectActionButtonHidden("Restore now");
    expectActionButtonHidden("Restore");

    await user.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(onDismissRestored).toHaveBeenCalledOnce();
    expect(onRequestForceRelease).not.toHaveBeenCalled();
  });

  it("does not estimate restored power from all-pending incomplete restore rollups", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoreIncompleteCurtailmentEvent,
          rollups: [{ state: "pending", count: restoreIncompleteCurtailmentEvent.selectedMiners }],
        }}
      />,
    );

    expectPrimaryLockup("Power restored", "Unavailable");
    expectProgressSummary("0 of 18 miners restored (0%)");
  });

  it("does not estimate restored power when incomplete restore rollups are empty", () => {
    render(<ActiveCurtailmentStatus event={{ ...restoreIncompleteCurtailmentEvent, rollups: [] }} />);

    expectPrimaryLockup("Power restored", "Unavailable");
    expectProgressHidden();
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
          unavailableReasonCounts: [
            { label: "offline", count: 3 },
            { label: "needs authentication", count: 1 },
            { label: "in maintenance", count: 1 },
          ],
        }}
      />,
    );

    expectProgressSummary("300 of 500 miners curtailed (60%)");
    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.queryByText("Curtail progress")).not.toBeInTheDocument();
    expect(progress.getByText("Curtailed (300)")).toBeVisible();
    expect(progress.getByText("Curtailing (200)")).toBeVisible();
    expect(progress.queryByText("80 pending, 20 retrying, 100 awaiting confirmation")).not.toBeInTheDocument();
    expect(progress.queryByText("Needs redispatch (20)")).not.toBeInTheDocument();
    expect(progress.queryByText("Retrying (20)")).not.toBeInTheDocument();
    expect(progress.queryByText("In progress (180)")).not.toBeInTheDocument();
    expect(progress.queryByText("Awaiting confirmation (100)")).not.toBeInTheDocument();
    expect(progress.queryByText("Pending (80)")).not.toBeInTheDocument();
    const unavailableAnnotation = progress.getByText(
      "5 unavailable (3 offline, 1 needs authentication, 1 in maintenance)",
    );
    expect(unavailableAnnotation).toBeVisible();
    expect(unavailableAnnotation).toHaveClass("ml-auto");
    expect(unavailableAnnotation).toHaveClass("text-right");
  });

  it("falls back when unavailable reason totals do not match the live rollup", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          rollups: [
            { state: "confirmed", count: 9 },
            { state: "unavailable", count: 5 },
          ],
          unavailableReasonCounts: [{ label: "offline", count: 3 }],
        }}
      />,
    );

    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.getByText("5 unavailable (details unavailable)")).toBeVisible();
    expect(progress.queryByText("5 unavailable (3 offline)")).not.toBeInTheDocument();
  });

  it("reaches full curtailment progress when only unavailable targets remain uncurtailed", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          rollups: [
            { state: "confirmed", count: 10 },
            { state: "unavailable", count: 40 },
          ],
        }}
      />,
    );

    expectProgressSummary("10 miners curtailed (100%)");
  });

  it("renders unavailable reasons when no targets are dispatchable", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          selectedMiners: 18,
          rollups: [{ state: "unavailable", count: 18 }],
          unavailableReasonCounts: [
            { label: "offline", count: 10 },
            { label: "needs authentication", count: 8 },
          ],
        }}
      />,
    );

    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.getByText("No dispatchable miners")).toBeVisible();
    expect(progress.getByText("18 unavailable (10 offline, 8 needs authentication)")).toBeVisible();
    expect(progress.queryByText(/Curtailed \(/)).not.toBeInTheDocument();
    expect(progress.queryByText(/Curtailing \(/)).not.toBeInTheDocument();
    expect(progress.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("hides curtail progress when no live rollup data exists", () => {
    // startedAt is set so the absent elapsed readout proves the live-data gate,
    // not just a missing timestamp.
    render(
      <ActiveCurtailmentStatus
        event={{ ...curtailingCurtailmentEvent, rollups: [], startedAt: "2026-05-01T10:00:00-04:00" }}
      />,
    );

    expectProgressHidden();
    expect(screen.queryByText(/elapsed/)).not.toBeInTheDocument();
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

    expectProgressSummary("1 miner curtailed (100%)");
  });

  it("floors the curtailed percentage so completion is never overstated", () => {
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
    expectProgressSummary("997 of 1,000 miners curtailed (99%)");
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

      const progress = within(screen.getByTestId("active-curtailment-progress"));
      expect(progress.getByText("3m 12s elapsed")).toBeVisible();
      expect(screen.queryByText("Elapsed")).not.toBeInTheDocument();

      act(() => {
        vi.advanceTimersByTime(3_000);
      });

      expect(progress.getByText("3m 15s elapsed")).toBeVisible();
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

      const progress = within(screen.getByTestId("active-curtailment-progress"));
      expect(progress.getByText("1m 30s elapsed")).toBeVisible();
      expect(screen.queryByText("Elapsed")).not.toBeInTheDocument();
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
    expect(screen.queryByText(/elapsed/)).not.toBeInTheDocument();
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
    expect(screen.getByText("Estimated time to curtail")).toBeVisible();
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
    expect(screen.getByText("Estimated time to curtail")).toBeVisible();
    expect(screen.getByText("2 minutes")).toBeVisible();
  });

  it("charges every pending wave when drifted targets prove a wave already dispatched", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...curtailingCurtailmentEvent,
          curtailBatchSize: 10,
          curtailBatchIntervalSec: 60,
          rollups: [
            { state: "drifted", count: 2 },
            { state: "pending", count: 25 },
          ],
        }}
      />,
    );

    // Drifted targets carry a prior dispatch, so the reconciler's interval
    // gate makes even the next pending wave wait: all 3 waves cost 60s.
    expect(screen.getByText("Estimated time to curtail")).toBeVisible();
    expect(screen.getByText("3 minutes")).toBeVisible();
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

    expect(screen.queryByText("Estimated time to curtail")).not.toBeInTheDocument();
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

    expect(screen.queryByText("Estimated time to curtail")).not.toBeInTheDocument();
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
    const restoredLegend = progress.getByText("Restored (400)");
    const curtailedLegend = progress.getByText("Curtailed (80)");
    const failedLegend = progress.getByText("Failed to restore (20)");
    expect(restoredLegend).toBeVisible();
    expect(restoredLegend.firstElementChild).toHaveClass("bg-intent-success-fill");
    expect(curtailedLegend).toBeVisible();
    expect(curtailedLegend.firstElementChild).toHaveClass("bg-core-primary-fill");
    expect(failedLegend).toBeVisible();
    expect(restoredLegend.compareDocumentPosition(curtailedLegend) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(curtailedLegend.compareDocumentPosition(failedLegend) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(progress.queryByText("Restoring (80)")).not.toBeInTheDocument();
    expect(progress.queryByText("Awaiting restore (80)")).not.toBeInTheDocument();
    expect(progress.getByText("5 unavailable (details unavailable)")).toBeVisible();
    expect(progress.queryByText("Restore progress")).not.toBeInTheDocument();
  });

  it("hides the failed restore legend entry when no restore failures exist", () => {
    render(
      <ActiveCurtailmentStatus
        event={{
          ...restoringCurtailmentEvent,
          rollups: [
            { state: "resolved", count: 300 },
            { state: "released", count: 100 },
            { state: "dispatched", count: 30 },
            { state: "confirmed", count: 50 },
          ],
        }}
      />,
    );

    const progress = within(screen.getByTestId("active-curtailment-progress"));
    expect(progress.getByText("Restored (400)")).toBeVisible();
    expect(progress.getByText("Curtailed (80)")).toBeVisible();
    expect(progress.queryByText(/Failed to restore/)).not.toBeInTheDocument();
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
    expectActionButtonHidden("Restore now");
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
    expectActionButtonHidden("Restore now");
    expectActionButtonHidden("Restore");
  });
});
