import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  activeRigRollout,
  completedRigRollout,
  completedWithFailuresRigRollout,
  gatedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import {
  type Rollout,
  RolloutBehaviorSchema,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutDeviceSchema,
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
  minerNames: {},
  listRolloutDevices: vi.fn().mockResolvedValue([]),
  onClose: vi.fn(),
  onContinue: vi.fn().mockResolvedValue(undefined),
  onPause: vi.fn().mockResolvedValue(undefined),
  onResume: vi.fn().mockResolvedValue(undefined),
  onCancel: vi.fn(),
  onRollback: vi.fn(),
  onRetryFailed: vi.fn().mockResolvedValue(undefined),
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
    const props = propsFor(completedWithFailuresRigRollout);
    const { rerender } = render(<RolloutDetailModal {...props} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();

    rerender(<RolloutDetailModal {...props} canRetryRemaining />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(props.rollout));

    rerender(<RolloutDetailModal {...props} canRetryRemaining={false} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();
  });

  it("keeps retry available for an active rollout even when it has no failures of its own", async () => {
    const props = propsFor(activeRigRollout);
    render(<RolloutDetailModal {...props} />);
    expect(screen.getByText(/including earlier updates\. It does not advance review gates\./)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry remaining" }));
    await waitFor(() => expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(props.rollout));
  });
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
  const error = "Reported manufacturer/model does not match the firmware target";
  props.listRolloutDevices.mockResolvedValue([
    create(RolloutDeviceSchema, {
      deviceId: 1n,
      deviceIdentifier: "incompatible-rig",
      phase: RolloutDevicePhase.FAILED,
      attempts: 0,
      lastError: error,
      online: true,
    }),
  ]);
  render(<RolloutDetailModal {...props} canRetryRemaining={false} />);
  expect(screen.getByTestId("rollout-failed-banner")).toHaveTextContent(
    "Review miner details for the cause of each failed update.",
  );
  expect(screen.getByTestId("rollout-failed-banner")).not.toHaveTextContent(/three|attempts|retry/i);
  fireEvent.click(screen.getByRole("button", { name: "Review miners" }));
  expect(await screen.findByText(error)).toBeInTheDocument();
});
