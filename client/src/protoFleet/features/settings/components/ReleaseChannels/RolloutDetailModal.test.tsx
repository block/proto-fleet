import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { batchedRigRollout, completedWithFailuresRigRollout } from "./ReleaseChannels.fixtures";
import RolloutDetailModal from "./RolloutDetailModal";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

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

describe("failed rollout retry action", () => {
  it("requires confirmed eligibility for a terminal rollout and updates when eligibility changes", async () => {
    const props = propsFor(completedWithFailuresRigRollout);
    const { rerender } = render(<RolloutDetailModal {...props} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();

    rerender(<RolloutDetailModal {...props} canRetryFailed />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(props.rollout));

    rerender(<RolloutDetailModal {...props} canRetryFailed={false} />);
    expect(screen.queryByTestId("view-rollout-retry-action")).not.toBeInTheDocument();
    expect(screen.getByTestId("rollout-failed-banner")).toBeInTheDocument();
  });

  it("keeps retry available for an active rollout without a channel snapshot", async () => {
    const props = propsFor(batchedRigRollout);
    render(<RolloutDetailModal {...props} />);
    fireEvent.click(screen.getByTestId("view-rollout-retry-action"));
    await waitFor(() => expect(props.onRetryFailed).toHaveBeenCalledExactlyOnceWith(props.rollout));
  });
});
