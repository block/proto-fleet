import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { deferred } from "./__tests__/helpers";
import { activeRigRollout, batchedRigRollout, gatedRigRollout, pausedRigRollout } from "./ReleaseChannels.fixtures";
import RolloutLiveView from "./RolloutLiveView";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
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
  minerNames: {},
  listRolloutDevices: vi.fn().mockResolvedValue([]),
  onContinue: vi.fn().mockResolvedValue(undefined),
  onPause: vi.fn().mockResolvedValue(undefined),
  onResume: vi.fn().mockResolvedValue(undefined),
  onCancel: vi.fn(),
  onRollback: vi.fn(),
  onRetryFailed: vi.fn().mockResolvedValue(undefined),
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

it("keeps inline recovery confirmations and navigation available with the current update snapshot", async () => {
  const props = propsFor(activeRigRollout);
  const pending = deferred();
  props.onRetryFailed.mockReturnValue(pending.promise);
  render(<RolloutLiveView {...props} currentGeneration={activeRigRollout.assignmentGeneration} />);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
  expect(props.onRetryFailed).not.toHaveBeenCalled();
  const dialog = screen.getByTestId("inline-retry-rollout-dialog");
  expect(dialog).toHaveTextContent("including earlier updates");
  fireEvent.click(within(dialog).getByTestId("inline-confirm-rollout-retry"));
  expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(activeRigRollout);
  expect(screen.getByRole("button", { name: "Manage" })).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-pause-action")).toBeDisabled();
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  expect(screen.getByTestId("inline-view-rollout-cancel-action")).toBeDisabled();
  expect(screen.getByTestId("inline-view-rollout-rollback-action")).toBeDisabled();
  fireEvent.click(screen.getByTestId("inline-view-rollout-open-action"));
  expect(props.onViewUpdate).toHaveBeenCalledExactlyOnceWith(activeRigRollout);
  await act(async () => pending.resolve());
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
  expect(await screen.findByTestId("rollout-miners-modal")).toBeInTheDocument();
  expect(props.listRolloutDevices).toHaveBeenCalledExactlyOnceWith(gatedRigRollout.id, expect.any(AbortSignal));
});
