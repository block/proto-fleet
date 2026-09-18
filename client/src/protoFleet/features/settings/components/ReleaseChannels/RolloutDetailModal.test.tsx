import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { activeRigRollout, completedWithFailuresRigRollout, gatedRigRollout } from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import {
  type Rollout,
  RolloutDevicePhase,
  RolloutDeviceSchema,
  RolloutState,
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
