import type { HTMLAttributes, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import BetweenChannelRolloutStatus from "./BetweenChannelRolloutStatus";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

const baseRollout: RolloutRecord = {
  id: "2f214a71-f94e-4e5f-8daf-d36c71b72f6c",
  name: "Production 2.0.0",
  strategyKey: "between-channel",
  state: "running",
  revision: 4n,
  reason: "Validated release",
  batches: [
    {
      id: 1n,
      position: 0,
      label: "Pilot",
      state: "admitted",
      revision: 1n,
      members: [],
    },
    {
      id: 2n,
      position: 1,
      label: "Remaining",
      state: "pending",
      revision: 1n,
      members: [],
    },
  ],
  members: [
    {
      id: 1n,
      batchId: 1n,
      deviceIdentifier: "confirmed",
      position: 0,
      state: "succeeded",
      revision: 1n,
      evidence: [],
    },
    {
      id: 2n,
      batchId: 1n,
      deviceIdentifier: "attention",
      position: 1,
      state: "attentionRequired",
      revision: 1n,
      lastError: "Firmware result is ambiguous",
      evidence: [],
    },
    {
      id: 3n,
      batchId: 2n,
      deviceIdentifier: "queued",
      position: 2,
      state: "pending",
      revision: 1n,
      evidence: [],
    },
  ],
  causes: [],
  availableActions: {
    admit: true,
    continue: false,
    pause: true,
    resume: false,
    abort: true,
    revert: false,
    complete: true,
  },
};

describe("BetweenChannelRolloutStatus", () => {
  it("shows confirmed membership separately from convergence and never offers retry for attention", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={baseRollout}
        laneLabel="Stable production"
        canControl
        onPause={vi.fn()}
        onAbort={vi.fn()}
        onRevert={vi.fn()}
      />,
    );

    expect(screen.getByTestId("active-rollout-membership-progress")).toHaveTextContent("1 of 3");
    expect(screen.getByTestId("active-rollout-convergence-progress")).toHaveTextContent("2 of 3");
    expect(screen.getByText("2 miners remain on the current release")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
  });

  it("hides control actions for read-only operators", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={baseRollout}
        laneLabel="Stable production"
        canControl={false}
        onPause={vi.fn()}
        onAbort={vi.fn()}
        onRevert={vi.fn()}
      />,
    );

    expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /more actions/i })).not.toBeInTheDocument();
  });

  it("wires pause, resume, and the manual review gate", async () => {
    const user = userEvent.setup();
    const onPause = vi.fn();
    const onResume = vi.fn();
    const onContinue = vi.fn();
    const { rerender } = render(
      <BetweenChannelRolloutStatus
        rollout={baseRollout}
        laneLabel="Stable production"
        canControl
        onPause={onPause}
        onResume={onResume}
        onContinue={onContinue}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Pause" }));
    expect(onPause).toHaveBeenCalledTimes(1);

    rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "paused",
          availableActions: { ...baseRollout.availableActions, pause: false, resume: true },
        }}
        laneLabel="Stable production"
        canControl
        onPause={onPause}
        onResume={onResume}
        onContinue={onContinue}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Resume" }));
    expect(onResume).toHaveBeenCalledTimes(1);

    rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          availableActions: { ...baseRollout.availableActions, continue: true, pause: true, resume: false },
        }}
        laneLabel="Stable production"
        canControl
        onPause={onPause}
        onResume={onResume}
        onContinue={onContinue}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it("uses distinct abort aftermath and revert confirmations", async () => {
    const user = userEvent.setup();
    const onAbort = vi.fn();
    const onRevert = vi.fn();
    const { rerender } = render(
      <BetweenChannelRolloutStatus
        rollout={baseRollout}
        laneLabel="Stable production"
        canControl
        onAbort={onAbort}
        onRevert={onRevert}
      />,
    );

    await user.click(screen.getByRole("button", { name: /more actions/i }));
    await user.click(screen.getByText("Abort rollout"));
    expect(screen.getByText(/Undispatched miners remain on the current release/)).toBeInTheDocument();
    expect(screen.getByText(/In-flight work may still settle/)).toBeInTheDocument();
    await user.click(screen.getByTestId("confirm-abort-rollout"));
    expect(onAbort).toHaveBeenCalledTimes(1);

    rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "aborted",
          availableActions: { ...baseRollout.availableActions, pause: false, abort: false, revert: true },
        }}
        laneLabel="Stable production"
        canControl
        onAbort={onAbort}
        onRevert={onRevert}
      />,
    );

    expect(screen.queryByRole("button", { name: "Revert" })).not.toBeInTheDocument();

    rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "aborted",
          batches: baseRollout.batches.map((batch) => ({ ...batch, state: "cancelled" })),
          members: baseRollout.members.map((member, index) => ({
            ...member,
            state: index === 0 ? "succeeded" : "cancelled",
          })),
          availableActions: { ...baseRollout.availableActions, pause: false, abort: false, revert: true },
        }}
        laneLabel="Stable production"
        canControl
        onAbort={onAbort}
        onRevert={onRevert}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Revert" }));
    expect(screen.getByText("Revert 1 confirmed miner?")).toBeInTheDocument();
    await user.click(screen.getByTestId("confirm-revert-rollout"));
    expect(onRevert).toHaveBeenCalledTimes(1);
  });

  it("offers permission-gated explicit completion for a settled final batch with failures", async () => {
    const user = userEvent.setup();
    const onCompleteWithFailures = vi.fn();
    const failedReview: RolloutRecord = {
      ...baseRollout,
      state: "review",
      batches: baseRollout.batches.map((batch) => ({ ...batch, state: "completed" })),
      members: baseRollout.members.map((member, index) => ({
        ...member,
        state: index === 0 ? "succeeded" : index === 1 ? "failed" : "cancelled",
      })),
      availableActions: { ...baseRollout.availableActions, continue: false, pause: false, complete: true },
    };
    const { rerender } = render(
      <BetweenChannelRolloutStatus
        rollout={failedReview}
        laneLabel="Stable production"
        canControl
        onCompleteWithFailures={onCompleteWithFailures}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Complete with failures" }));
    expect(onCompleteWithFailures).toHaveBeenCalledTimes(1);

    rerender(
      <BetweenChannelRolloutStatus
        rollout={failedReview}
        laneLabel="Stable production"
        canControl={false}
        onCompleteWithFailures={onCompleteWithFailures}
      />,
    );
    expect(screen.queryByRole("button", { name: "Complete with failures" })).not.toBeInTheDocument();

    rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...failedReview,
          batches: [{ ...failedReview.batches[0], state: "pending" }],
        }}
        laneLabel="Stable production"
        canControl
        onCompleteWithFailures={onCompleteWithFailures}
      />,
    );
    expect(screen.queryByRole("button", { name: "Complete with failures" })).not.toBeInTheDocument();
  });
});
