import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { activeRigRollout, batchedRigRollout, gatedRigRollout, pausedRigRollout } from "./ReleaseChannels.fixtures";
import RolloutLiveView from "./RolloutLiveView";
import {
  type Rollout,
  RolloutCancelReason,
  RolloutStage,
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
  presentation: "inline" as const,
  onViewMiners: vi.fn(),
  onContinue: vi.fn().mockResolvedValue(undefined),
  onPause: vi.fn().mockResolvedValue(undefined),
  onResume: vi.fn().mockResolvedValue(undefined),
  onCancel: vi.fn(),
  onRollback: vi.fn(),
  onRetryFailed: vi.fn(),
  onManage: vi.fn(),
  onViewUpdate: vi.fn(),
});

it.each([activeRigRollout, gatedRigRollout, batchedRigRollout, pausedRigRollout])(
  "starts with the plan and telemetry collapsed in state $state",
  (rollout) => {
    render(<RolloutLiveView {...propsFor(rollout)} />);
    expect(screen.getByRole("heading", { name: "Active firmware update" })).toBeInTheDocument();
    const card = within(screen.getByTestId("inline-rollout-live-view"));
    expect(card.queryByRole("heading", { name: "Active firmware update" })).not.toBeInTheDocument();
    expect(card.queryByRole("button", { name: "Back" })).not.toBeInTheDocument();
    expect(card.getByRole("button", { name: "Manage" })).toBeInTheDocument();
    expect(card.getByTestId("inline-rollout-detail-progress")).toBeInTheDocument();
    const toggle = card.getByRole("button", { name: "View details" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(card.queryByTestId("inline-rollout-detail-stats")).not.toBeInTheDocument();
    expect(card.queryByTestId("inline-rollout-evidence")).not.toBeInTheDocument();

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(toggle).toHaveAttribute("aria-controls", screen.getByTestId("inline-rollout-details").id);
    expect(card.getByTestId("inline-rollout-detail-stats")).toHaveTextContent("Canary channel, 6 miners");
    expect(card.getByTestId("inline-rollout-performance")).toBeInTheDocument();

    fireEvent.click(card.getByRole("button", { name: "Hide details" }));
    expect(card.queryByTestId("inline-rollout-detail-stats")).not.toBeInTheDocument();
    expect(card.queryByTestId("inline-rollout-evidence")).not.toBeInTheDocument();
    expect(card.getByTestId("inline-rollout-detail-progress")).toBeInTheDocument();
  },
);

it("keeps refresh failures, failed miners, and review hold reasons visible when collapsed", () => {
  const props = propsFor(batchedRigRollout);
  render(<RolloutLiveView {...props} refreshWarning={<p>Could not refresh updates</p>} />);
  expect(screen.getByText("Could not refresh updates")).toBeInTheDocument();
  expect(screen.getByTestId("inline-rollout-failed-banner")).toHaveTextContent("1 miner failed to update");
  expect(screen.getByTestId("inline-review-banner")).toHaveTextContent(
    `Holding for review: ${batchedRigRollout.evidence!.holdReason}`,
  );
  expect(screen.getByTestId("inline-view-rollout-continue-action")).not.toBeDisabled();
  expect(screen.queryByTestId("inline-rollout-evidence")).not.toBeInTheDocument();
});

it.each([
  "The assigned firmware file is unavailable. Restore the file or choose another firmware version.",
  "Waiting for space within the channel's offline limit.",
])("keeps a dispatch blocker visible with details collapsed: %s", (holdReason) => {
  const rollout = { ...activeRigRollout, evidence: { ...activeRigRollout.evidence!, holdReason } };
  const props = propsFor(rollout);
  const { rerender } = render(<RolloutLiveView {...props} />);
  expect(screen.getByTestId("inline-rollout-dispatch-hold")).toHaveTextContent(holdReason);
  expect(screen.queryByTestId("inline-rollout-details")).not.toBeInTheDocument();
  expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeEnabled();
  rerender(<RolloutLiveView {...props} rollout={{ ...rollout, evidence: { ...rollout.evidence, holdReason: "" } }} />);
  expect(screen.queryByTestId("inline-rollout-dispatch-hold")).not.toBeInTheDocument();
});

it.each([
  { ...activeRigRollout, state: RolloutState.PAUSED },
  { ...activeRigRollout, stage: RolloutStage.WAITING },
  { ...gatedRigRollout },
  { ...activeRigRollout, state: RolloutState.COMPLETED, status: RolloutStatus.COMPLETED },
])("does not show a dispatch blocker outside dispatch states (%#)", (rollout) => {
  render(
    <RolloutLiveView
      {...propsFor({ ...rollout, evidence: { ...rollout.evidence!, holdReason: "A retained hold reason" } })}
    />,
  );
  expect(screen.queryByTestId("inline-rollout-dispatch-hold")).not.toBeInTheDocument();
});

it.each([RolloutCancelReason.CANCELED_REMAINING, RolloutCancelReason.SUPERSEDED, RolloutCancelReason.ROLLED_BACK])(
  "shows unfinished canceled work neutrally while preserving completed and failed outcomes (%s)",
  (cancelReason) => {
    const rollout = {
      ...activeRigRollout,
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      cancelReason,
      deviceCount: 9,
      deviceCounts: {
        ...activeRigRollout.deviceCounts!,
        done: 2,
        queued: 1,
        inProgress: 2,
        retrying: 1,
        failed: 1,
        excluded: 1,
        skipped: 1,
      },
    };
    render(<RolloutLiveView {...propsFor(rollout)} presentation="fullscreen" />);
    const progress = within(screen.getByTestId("rollout-detail-progress"));
    expect(progress.getByText("Canceled (4)")).toBeInTheDocument();
    expect(progress.getByText("Updated (2)")).toBeInTheDocument();
    expect(progress.getByText("Failed (1)")).toBeInTheDocument();
    expect(progress.queryByText("Remaining (4)")).not.toBeInTheDocument();
    expect(progress.getByText(/Update commands already sent may still finish/)).toBeInTheDocument();
    expect(progress.getByText("1 excluded")).toBeInTheDocument();
    expect(progress.getByText("1 skipped")).toBeInTheDocument();
  },
);

it("shows overall progress for a completed pilot and labels expanded evidence with its actual scope", () => {
  render(<RolloutLiveView {...propsFor(gatedRigRollout)} />);
  const progress = screen.getByTestId("inline-rollout-detail-progress");
  expect(progress).toHaveTextContent("Overall progress: 2 of 6 miners updated (33%)");
  expect(progress).toHaveTextContent("Updated (2)");
  expect(progress).toHaveTextContent("Remaining (4)");
  expect(progress).not.toHaveTextContent("100%");
  expect(screen.getByTestId("inline-review-banner")).toHaveTextContent("2 of 2 miners in this batch updated");
  fireEvent.click(screen.getByRole("button", { name: "View details" }));
  expect(screen.getByTestId("inline-rollout-evidence")).toHaveTextContent("Telemetry evidence: pilot batch (2 miners)");
  expect(screen.getByTestId("inline-rollout-detail-stats")).toHaveTextContent("Least efficient first");
});

it("updates expanded evidence from polling and collapses details for a different update", () => {
  const props = propsFor(activeRigRollout);
  const { rerender } = render(<RolloutLiveView {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "View details" }));
  const refreshed = {
    ...activeRigRollout,
    revision: activeRigRollout.revision + 1n,
    evidence: { ...activeRigRollout.evidence!, newErrors: 7 },
  };
  rerender(<RolloutLiveView {...props} rollout={refreshed} />);
  expect(screen.getByTestId("inline-evidence-errors")).toHaveTextContent("7");
  fireEvent.click(screen.getByRole("button", { name: "Manage" }));
  expect(props.onManage).toHaveBeenCalledExactlyOnceWith(refreshed);

  rerender(<RolloutLiveView {...props} rollout={gatedRigRollout} />);
  expect(screen.getByRole("button", { name: "View details" })).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByTestId("inline-rollout-evidence")).not.toBeInTheDocument();
});

it("requests retry confirmation and navigation with the current update snapshot", () => {
  const props = propsFor(activeRigRollout);
  render(<RolloutLiveView {...props} currentGeneration={activeRigRollout.assignmentGeneration} />);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
  expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(activeRigRollout);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  fireEvent.click(screen.getByTestId("inline-view-rollout-open-action"));
  expect(props.onViewUpdate).toHaveBeenCalledExactlyOnceWith(activeRigRollout);
});

it("honors the monitor action lock while keeping miner navigation and details available", async () => {
  const props = propsFor(gatedRigRollout);
  render(<RolloutLiveView {...props} actionsDisabled currentGeneration={gatedRigRollout.assignmentGeneration} />);
  expect(screen.getByRole("button", { name: "Manage" })).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-continue-action")).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "View details" }));
  expect(screen.getByTestId("inline-rollout-evidence")).toBeInTheDocument();
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  expect(screen.getByTestId("inline-view-rollout-retry-action")).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-cancel-action")).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-rollback-action")).toBeDisabled();
  fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));
  expect(props.onViewMiners).toHaveBeenCalledExactlyOnceWith(gatedRigRollout, "all");
});

it.each([
  { state: RolloutState.IN_PROGRESS, status: RolloutStatus.ACTIVE, label: "Updating", animates: true },
  {
    state: RolloutState.STABILIZING_TELEMETRY,
    status: RolloutStatus.ACTIVE,
    label: "Waiting for telemetry",
    animates: true,
  },
  {
    state: RolloutState.WAITING_FOR_CONTROLLER,
    status: RolloutStatus.ACTIVE,
    label: "Waiting for controller",
    animates: false,
  },
  { state: RolloutState.UNSPECIFIED, status: RolloutStatus.ACTIVE, label: "Updating", animates: false },
  {
    state: RolloutState.PAUSED_AT_PILOT_GATE,
    status: RolloutStatus.ACTIVE,
    label: "Pilot batch review",
    animates: false,
  },
  {
    state: RolloutState.PAUSED_AT_BATCH_REVIEW,
    status: RolloutStatus.ACTIVE,
    label: "Batch review",
    animates: false,
  },
  { state: RolloutState.PAUSED, status: RolloutStatus.ACTIVE, label: "Paused", animates: false },
  { state: RolloutState.COMPLETED, status: RolloutStatus.COMPLETED, label: "Completed", animates: false },
  {
    state: RolloutState.COMPLETED_WITH_FAILURES,
    status: RolloutStatus.COMPLETED_WITH_FAILURES,
    label: "Completed with failures",
    animates: false,
  },
  { state: RolloutState.CANCELED, status: RolloutStatus.CANCELED, label: "Canceled", animates: false },
])("indicates activity accurately for $label (state $state)", ({ state, status, label, animates }) => {
  const rollout = {
    ...activeRigRollout,
    state,
    status,
    stage:
      state === RolloutState.STABILIZING_TELEMETRY ||
      state === RolloutState.PAUSED_AT_PILOT_GATE ||
      state === RolloutState.PAUSED_AT_BATCH_REVIEW
        ? RolloutStage.AWAITING_REVIEW
        : activeRigRollout.stage,
  };
  render(<RolloutLiveView {...propsFor(rollout)} />);
  expect(screen.getByTestId("inline-rollout-status-headline")).toHaveTextContent(label);
  const activity = screen.queryByTestId("inline-rollout-status-activity");
  if (animates) expect(activity).toHaveClass("animate-spin", "motion-reduce:animate-none");
  else expect(activity).not.toBeInTheDocument();
});
