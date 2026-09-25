import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { deferred } from "./__tests__/helpers";
import {
  activeRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import {
  type Rollout,
  RolloutBehaviorSchema,
  RolloutDeviceCountsSchema,
  RolloutEvidenceSchema,
  RolloutMethod,
  RolloutState,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFleetStore } from "@/protoFleet/store";

const initialAuth = useFleetStore.getState().auth;
beforeEach(() =>
  useFleetStore.setState({
    auth: { ...initialAuth, username: "operator", sessionGeneration: 1, isAuthenticated: true },
  }),
);
afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
});

const propsFor = (rollout: Rollout) => ({
  rollout,
  onViewMiners: vi.fn(),
  onClose: vi.fn(),
  onContinue: vi.fn().mockResolvedValue(undefined),
  onPause: vi.fn().mockResolvedValue(undefined),
  onResume: vi.fn().mockResolvedValue(undefined),
  onCancel: vi.fn(),
  onRollback: vi.fn(),
  onRetryFailed: vi.fn(),
});

describe.each(["success", "failure"] as const)("overflow actions during lifecycle mutation (%s)", (outcome) => {
  it.each([
    { action: "continue", callback: "onContinue", rollout: gatedRigRollout },
    { action: "pause", callback: "onPause", rollout: activeRigRollout },
    { action: "resume", callback: "onResume", rollout: pausedRigRollout },
  ] as const)("blocks cancel and rollback until $action settles", async ({ action, callback, rollout }) => {
    const pending = deferred();
    const reportedError = vi.fn();
    const props = { ...propsFor(rollout), currentGeneration: rollout.assignmentGeneration };
    // ActiveUpdatesMonitor reports mutation failures and settles the callback.
    props[callback].mockReturnValue(pending.promise.catch(reportedError));
    const { rerender } = render(<RolloutDetailModal {...props} />);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    expect(screen.getByTestId("view-rollout-cancel-action")).not.toBeDisabled();
    expect(screen.getByTestId("view-rollout-rollback-action")).not.toBeDisabled();

    fireEvent.click(screen.getByTestId(`view-rollout-${action}-action`));
    const refreshed = { ...rollout, revision: rollout.revision + 1n };
    rerender(<RolloutDetailModal {...props} rollout={refreshed} />);
    if (!screen.queryByTestId("view-rollout-more-actions-menu"))
      fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    const cancel = screen.getByTestId("view-rollout-cancel-action");
    const rollback = screen.getByTestId("view-rollout-rollback-action");
    expect(cancel).toBeDisabled();
    expect(rollback).toBeDisabled();
    expect(screen.getByTestId("view-rollout-view-miners-action")).not.toBeDisabled();
    fireEvent.click(cancel);
    fireEvent.click(rollback);
    expect(props.onCancel).not.toHaveBeenCalled();
    expect(props.onRollback).not.toHaveBeenCalled();
    expect(props[callback]).toHaveBeenCalledExactlyOnceWith(rollout);

    const failure = new Error("The update changed; review it again");
    await act(async () => {
      if (outcome === "success") pending.resolve();
      else pending.reject(failure);
    });

    expect(screen.getByTestId("view-rollout-cancel-action")).not.toBeDisabled();
    expect(screen.getByTestId("view-rollout-rollback-action")).not.toBeDisabled();
    fireEvent.click(screen.getByTestId("view-rollout-cancel-action"));
    expect(props.onCancel).toHaveBeenCalledExactlyOnceWith(refreshed);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    fireEvent.click(screen.getByTestId("view-rollout-rollback-action"));
    expect(props.onRollback).toHaveBeenCalledExactlyOnceWith(refreshed);
    if (outcome === "failure") expect(reportedError).toHaveBeenCalledExactlyOnceWith(failure);
    else expect(reportedError).not.toHaveBeenCalled();
  });
});

it("keeps the miner drill-down available while the monitor holds a lifecycle mutation", () => {
  const props = { ...propsFor(activeRigRollout), currentGeneration: activeRigRollout.assignmentGeneration };
  render(<RolloutDetailModal {...props} actionsDisabled isRetrying />);
  expect(screen.getByRole("status")).toHaveTextContent("Requesting retry");
  fireEvent.click(screen.getByTestId("view-rollout-view-miners-action"));
  expect(props.onViewMiners).toHaveBeenCalledExactlyOnceWith(activeRigRollout, "all");
  expect(props.onCancel).not.toHaveBeenCalled();
  expect(props.onRollback).not.toHaveBeenCalled();
});

describe("rollout scope and neutral targets", () => {
  it.each([
    { phases: { skipped: 6 }, updated: 0, eligible: 0 },
    { phases: { excluded: 6 }, updated: 0, eligible: 0 },
    { phases: { done: 3, failed: 1, skipped: 1, excluded: 1 }, updated: 3, eligible: 4 },
  ])(
    "keeps every target in the scope while excluding neutral phases from progress (%#)",
    ({ phases, updated, eligible }) => {
      const deviceCounts = create(RolloutDeviceCountsSchema, phases);
      const rollout = {
        ...completedRigRollout,
        deviceCount: 6,
        deviceCounts,
        status: deviceCounts.failed ? RolloutStatus.COMPLETED_WITH_FAILURES : RolloutStatus.COMPLETED,
        state: deviceCounts.failed ? RolloutState.COMPLETED_WITH_FAILURES : RolloutState.COMPLETED,
      };
      render(<RolloutDetailModal {...propsFor(rollout)} />);
      expect(
        within(screen.getByTestId("rollout-detail-stats")).getByText("Canary channel, 6 miners"),
      ).toBeInTheDocument();
      const progress = screen.getByTestId("rollout-detail-progress");
      expect(progress).toHaveTextContent(`Overall progress: ${updated} of ${eligible} miners updated`);
      if (deviceCounts.skipped)
        expect(within(progress).getByText(`${deviceCounts.skipped} skipped`)).toBeInTheDocument();
      if (deviceCounts.excluded)
        expect(within(progress).getByText(`${deviceCounts.excluded} excluded`)).toBeInTheDocument();
    },
  );

  it.each([
    { batch: { done: 1, skipped: 1, excluded: 1 }, all: { done: 1, skipped: 2, excluded: 2, queued: 3 } },
    { batch: { skipped: 3 }, all: { skipped: 4, excluded: 1, queued: 3 } },
  ])(
    "counts neutral targets in review and outside-batch labels without mixing batch and rollout totals (%#)",
    ({ batch, all }) => {
      const currentBatchCounts = create(RolloutDeviceCountsSchema, batch);
      const rollout = {
        ...gatedRigRollout,
        deviceCount: 8,
        deviceCounts: create(RolloutDeviceCountsSchema, all),
        currentBatchCounts,
        behavior: { ...gatedRigRollout.behavior!, pilotSize: 3 },
        evidence: create(RolloutEvidenceSchema, {
          devicesTotal: 3,
          verified: currentBatchCounts.done,
          skipped: currentBatchCounts.skipped,
          excluded: currentBatchCounts.excluded,
          online: 3,
          hashing: 3,
          baselineHashing: 3,
          holdReason: "Manual review",
        }),
      };
      render(<RolloutDetailModal {...propsFor(rollout)} />);
      expect(
        within(screen.getByTestId("rollout-detail-stats")).getByText("Canary channel, 8 miners"),
      ).toBeInTheDocument();
      expect(screen.getByTestId("review-banner")).toHaveTextContent(
        `${currentBatchCounts.done} of 3 miners in this batch updated to ${rollout.firmwareVersion}.`,
      );
      const progress = within(screen.getByTestId("rollout-detail-progress"));
      expect(progress.getByText("5 outside this batch")).toBeInTheDocument();
      expect(progress.getByText(`${currentBatchCounts.skipped} skipped`)).toBeInTheDocument();
      if (currentBatchCounts.excluded) expect(progress.getByText("1 excluded")).toBeInTheDocument();
      else expect(progress.queryByText(/excluded/)).not.toBeInTheDocument();
    },
  );
});

describe("remaining rollout retry action", () => {
  it("requires confirmed eligibility for a terminal rollout and updates when eligibility changes", async () => {
    const props = { ...propsFor(completedWithFailuresRigRollout), onManage: vi.fn() };
    const { rerender } = render(<RolloutDetailModal {...props} />);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();

    rerender(<RolloutDetailModal {...props} canRetryRemaining />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(props.rollout);

    rerender(<RolloutDetailModal {...props} canRetryRemaining={false} />);
    fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();
  });
});

it("keeps lifecycle controls in the live view and miner navigation beside progress", async () => {
  const props = { ...propsFor(activeRigRollout), onManage: vi.fn() };
  render(<RolloutDetailModal {...props} />);
  expect(screen.queryByTestId("rollout-detail-header")).not.toBeInTheDocument();
  const liveView = within(screen.getByTestId("rollout-live-view"));
  expect(liveView.getByTestId("rollout-detail-title")).toHaveTextContent("Canary, Proto Rig firmware update");
  const back = liveView.getByRole("button", { name: "Back" });
  expect(back).toHaveAttribute("data-testid", "view-rollout-back-action");
  expect(
    back.compareDocumentPosition(liveView.getByTestId("rollout-detail-title")) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  const liveActions = within(screen.getByTestId("rollout-detail-actions"));
  expect(liveActions.getByTestId("view-rollout-pause-action")).toBeInTheDocument();
  expect(liveActions.getByTestId("view-rollout-more-actions-trigger")).toBeInTheDocument();
  expect(liveActions.queryByRole("button", { name: "Retry remaining" })).not.toBeInTheDocument();
  expect(liveActions.queryByRole("button", { name: "Manage channel" })).not.toBeInTheDocument();

  const progress = screen.getByTestId("rollout-detail-progress");
  const stats = screen.getByTestId("rollout-detail-stats");
  expect(progress.compareDocumentPosition(stats) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  fireEvent.click(within(progress).getByRole("button", { name: "View miners" }));
  expect(props.onViewMiners).toHaveBeenCalledExactlyOnceWith(activeRigRollout, "all");
});

it("goes back from the live view without changing the update", () => {
  const props = { ...propsFor(activeRigRollout), onManage: vi.fn() };
  render(<RolloutDetailModal {...props} />);
  const liveView = within(screen.getByTestId("rollout-live-view"));
  fireEvent.click(liveView.getByRole("button", { name: "Back" }));
  expect(props.onClose).toHaveBeenCalledOnce();
  expect(props.onContinue).not.toHaveBeenCalled();
  expect(props.onPause).not.toHaveBeenCalled();
  expect(props.onResume).not.toHaveBeenCalled();
  expect(props.onCancel).not.toHaveBeenCalled();
  expect(props.onRollback).not.toHaveBeenCalled();
  expect(props.onRetryFailed).not.toHaveBeenCalled();
  expect(props.onManage).not.toHaveBeenCalled();
});

it("opens management for the current update from the overflow menu", () => {
  const props = { ...propsFor(activeRigRollout), onManage: vi.fn() };
  const { rerender } = render(<RolloutDetailModal {...props} />);
  const refreshed = { ...activeRigRollout, revision: activeRigRollout.revision + 1n };
  rerender(<RolloutDetailModal {...props} rollout={refreshed} />);
  fireEvent.click(screen.getByTestId("view-rollout-more-actions-trigger"));
  const manage = screen.getByTestId("view-rollout-manage-action");
  expect(manage).toHaveTextContent("Manage channel");
  fireEvent.click(manage);
  expect(props.onManage).toHaveBeenCalledExactlyOnceWith(refreshed);
  expect(props.onRetryFailed).not.toHaveBeenCalled();
  expect(props.onClose).not.toHaveBeenCalled();
});

describe("review gate controls", () => {
  it.each([
    { waitBetweenBatchesSeconds: 0, methodLabel: "Batches of 2, back to back" },
    { waitBetweenBatchesSeconds: 120, methodLabel: "Batches of 2, 2m between batches" },
  ])("does not claim review gates for unreviewed batches with a $waitBetweenBatchesSeconds-second wait", (testCase) => {
    const rollout = {
      ...activeRigRollout,
      behavior: create(RolloutBehaviorSchema, {
        method: RolloutMethod.BATCHED,
        batchSize: 2,
        waitBetweenBatchesSeconds: testCase.waitBetweenBatchesSeconds,
      }),
    };
    render(<RolloutDetailModal {...propsFor(rollout)} />);
    const stats = within(screen.getByTestId("rollout-detail-stats"));
    expect(stats.getByText(testCase.methodLabel)).toBeInTheDocument();
    expect(stats.queryByText("Review gates")).not.toBeInTheDocument();
    expect(stats.queryByText("Manual")).not.toBeInTheDocument();
  });

  it.each([
    { method: RolloutMethod.PILOT_THEN_CONTINUE, automatic: false },
    { method: RolloutMethod.PILOT_THEN_CONTINUE, automatic: true },
    { method: RolloutMethod.BATCHED, automatic: false },
    { method: RolloutMethod.BATCHED, automatic: true },
  ])("shows the actual gate policy for method $method (automatic=$automatic)", ({ method, automatic }) => {
    const rollout = {
      ...activeRigRollout,
      behavior: create(RolloutBehaviorSchema, {
        method,
        batchSize: method === RolloutMethod.BATCHED ? 2 : 0,
        pilotSize: method === RolloutMethod.PILOT_THEN_CONTINUE ? 2 : 0,
        // The pilot always gates, even when this optional flag is false.
        reviewAfterEachBatch: method === RolloutMethod.BATCHED,
        autoContinueOnHealthyTelemetry: automatic,
        stabilizationSeconds: automatic ? 120 : 0,
        thresholds: automatic ? { maxNewErrors: 0 } : undefined,
      }),
    };
    render(<RolloutDetailModal {...propsFor(rollout)} />);
    const stats = within(screen.getByTestId("rollout-detail-stats"));
    expect(stats.getByText("Review gates")).toBeInTheDocument();
    expect(stats.getByText(automatic ? "Automatic (≤ 0 new errors, 2m to settle)" : "Manual")).toBeInTheDocument();
  });

  it.each([false, true])("requires resume before continuing a paused review gate (automatic=%s)", async (automatic) => {
    const resumed = {
      ...gatedRigRollout,
      behavior: { ...gatedRigRollout.behavior!, autoContinueOnHealthyTelemetry: automatic },
      evidence: { ...gatedRigRollout.evidence!, readyToAdvance: automatic },
    };
    const paused = { ...resumed, state: RolloutState.PAUSED };
    const props = propsFor(paused);
    const { rerender } = render(<RolloutDetailModal {...props} />);
    expect(screen.queryByTestId("view-rollout-continue-action")).not.toBeInTheDocument();
    expect(screen.queryByTestId("review-banner")).not.toBeInTheDocument();
    expect(screen.getByTestId("paused-banner")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("view-rollout-resume-action"));
    await waitFor(() => expect(props.onResume).toHaveBeenCalledExactlyOnceWith(paused));
    expect(props.onContinue).not.toHaveBeenCalled();

    rerender(<RolloutDetailModal {...props} rollout={resumed} />);
    expect(screen.getByTestId("review-banner")).toBeInTheDocument();
    expect(screen.queryByTestId("paused-banner")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("view-rollout-continue-action"));
    await waitFor(() => expect(props.onContinue).toHaveBeenCalledExactlyOnceWith(resumed));
  });
});

it("leaves the failure diagnosis to miner details instead of assuming exhausted attempts", async () => {
  const props = propsFor(completedWithFailuresRigRollout);
  render(<RolloutDetailModal {...props} canRetryRemaining={false} />);
  expect(screen.getByTestId("rollout-failed-banner")).toHaveTextContent(
    "Review miner details for the cause of each failed update.",
  );
  expect(screen.getByTestId("rollout-failed-banner")).not.toHaveTextContent(/three|attempts|retry/i);
  fireEvent.click(screen.getByRole("button", { name: "Review miners" }));
  expect(props.onViewMiners).toHaveBeenCalledExactlyOnceWith(completedWithFailuresRigRollout, "failed");
});
