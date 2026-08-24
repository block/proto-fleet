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
  it("labels a model child while leaving its controls on the child card", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          parentId: "708ba540-2748-43d5-a2d1-a4b062bc1df0",
          laneModelId: "ba3febcc-1144-45b9-99bb-b9c1bb8efa43",
          modelIdentityKey: "v1:5:proto:5:alpha",
          manufacturer: "Proto",
          model: "Alpha",
        }}
        laneLabel="Stable production"
        canControl
        onPause={vi.fn()}
      />,
    );

    expect(screen.getByText("Model rollout")).toBeInTheDocument();
    expect(screen.getByText("Proto Alpha")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
  });

  it("exposes admission retry only on the created child", async () => {
    const user = userEvent.setup();
    const onAdmit = vi.fn();
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "created",
          parentId: "708ba540-2748-43d5-a2d1-a4b062bc1df0",
          laneModelId: "ba3febcc-1144-45b9-99bb-b9c1bb8efa43",
          manufacturer: "Proto",
          model: "Alpha",
        }}
        laneLabel="Stable production"
        canControl
        onAdmit={onAdmit}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Retry model start" }));
    expect(onAdmit).toHaveBeenCalledOnce();
  });

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
    expect(screen.queryByRole("button", { name: "Continue anyway" })).not.toBeInTheDocument();
  });

  it("requires explicit confirmation with evidence context before overriding a held verdict", async () => {
    const user = userEvent.setup();
    const onContinue = vi.fn();
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          hashratePolicy: {
            maxDropBasisPoints: 10,
            healthyDurationSeconds: 30,
          },
          availableActions: { ...baseRollout.availableActions, continue: true },
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "held",
                totalCount: 3n,
                pairedCount: 3n,
                latestPolicyBucketDeltaBasisPoints: -400,
                postWindowFinalized: false,
              },
            },
            baseRollout.batches[1],
          ],
        }}
        laneLabel="Stable production"
        canControl
        onContinue={onContinue}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(onContinue).not.toHaveBeenCalled();
    expect(screen.getByText("Configured maximum drop: 0.10%")).toBeInTheDocument();
    expect(screen.getByText("Latest policy bucket: −4.00%")).toBeInTheDocument();
    expect(screen.getByText("Paired coverage: 3 of 3")).toBeInTheDocument();
    expect(screen.getByText(/admits the next batch despite this held evidence/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onContinue).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Continue anyway" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Hashrate held");

    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.click(screen.getByRole("button", { name: "Continue anyway" }));
    expect(onContinue).toHaveBeenCalledOnce();
    expect(onContinue).toHaveBeenCalledWith("Override held hashrate evidence");
  });

  it("keeps completed batch evidence and performance visible while paused", async () => {
    const user = userEvent.setup();
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "paused",
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              completedAt: "2026-08-18T01:00:00.000Z",
              evidenceSummary: {
                status: "observing",
                totalCount: 2n,
                pairedCount: 2n,
                cumulativeBaselineHashrateHs: 250_000_000_000_000,
                cumulativeCurrentHashrateHs: 237_500_000_000_000,
                evaluatedAt: new Date().toISOString(),
                postWindowFinalized: false,
              },
            },
            baseRollout.batches[1],
          ],
          availableActions: {
            ...baseRollout.availableActions,
            pause: false,
            resume: true,
          },
        }}
        laneLabel="Stable production"
        canControl
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Observing hashrate");
    await user.click(screen.getByRole("button", { name: "View details" }));
    expect(screen.getByTestId("active-rollout-performance")).toHaveTextContent("237.5 TH/s");
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

    const moreActions = screen.getByRole("button", { name: /more actions/i });
    await user.click(moreActions);
    await user.click(screen.getByText("Abort rollout"));
    expect(screen.getByText(/Undispatched miners remain on the current release/)).toBeInTheDocument();
    expect(screen.getByText(/In-flight work may still settle/)).toBeInTheDocument();
    await user.click(screen.getByTestId("confirm-abort-rollout"));
    expect(onAbort).toHaveBeenCalledTimes(1);
    expect(moreActions).toHaveFocus();

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
    expect(screen.getByText("Revert this model for 1 confirmed miner?")).toBeInTheDocument();
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

  it.each([
    ["pending", "Evidence pending"],
    ["collecting", "Collecting evidence"],
    ["unavailable", "Evidence unavailable"],
    ["observing", "Observing hashrate"],
    ["healthy", "Hashrate healthy"],
    ["held", "Hashrate held"],
    ["stale", "Evidence stale"],
    ["automationError", "Automation error"],
    ["finalized", "Evidence finalized"],
    ["cancelled", "Evidence cancelled"],
  ] as const)("renders %s evidence with text status and paired coverage", (status, expectedLabel) => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              completedAt: "2026-08-18T01:00:00.000Z",
              evidenceSummary: {
                status,
                totalCount: 3n,
                pairedCount: status === "pending" ? 0n : 2n,
                evaluatedAt: new Date().toISOString(),
                postWindowFinalized: status === "finalized" || status === "cancelled",
              },
            },
            baseRollout.batches[1],
          ],
        }}
        laneLabel="Stable production"
        canControl
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent(expectedLabel);
    expect(screen.getByText(`Paired coverage ${status === "pending" ? "0" : "2"} of 3`)).toBeInTheDocument();
    expect(screen.getByText(/Last evaluated/)).toBeInTheDocument();
  });

  it("does not fabricate metrics when evidence is unavailable", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "unavailable",
                totalCount: 3n,
                pairedCount: 0n,
                postWindowFinalized: true,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
      />,
    );

    expect(screen.queryByTestId("active-rollout-performance")).not.toBeInTheDocument();
    expect(screen.queryByText(/0 TH\/s/)).not.toBeInTheDocument();
  });

  it.each(["unavailable", "automationError"] as const)("preserves manual controls when evidence is %s", (status) => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          availableActions: {
            ...baseRollout.availableActions,
            continue: true,
            pause: true,
            abort: true,
          },
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status,
                totalCount: 3n,
                pairedCount: 0n,
                postWindowFinalized: status === "unavailable",
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
        onContinue={vi.fn()}
        onPause={vi.fn()}
        onAbort={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Continue" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /more actions/i })).toBeInTheDocument();
  });

  it("displays the persisted automation error while preserving manual controls", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          availableActions: {
            ...baseRollout.availableActions,
            continue: true,
            pause: true,
          },
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "automationError",
                totalCount: 3n,
                pairedCount: 3n,
                errorMessage: "Continue control failed after the rollout revision changed",
                postWindowFinalized: false,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
        onContinue={vi.fn()}
        onPause={vi.fn()}
      />,
    );

    expect(screen.getByText("Continue control failed after the rollout revision changed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
  });

  it("distinguishes the latest policy health check from cumulative performance", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          hashratePolicy: {
            maxDropBasisPoints: 10,
            healthyDurationSeconds: 30,
          },
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "held",
                totalCount: 3n,
                pairedCount: 3n,
                cumulativeBaselineHashrateHs: 250_000_000_000_000,
                cumulativeCurrentHashrateHs: 237_500_000_000_000,
                cumulativeDeltaBasisPoints: -500,
                latestPolicyBucketHashrateHs: 240_000_000_000_000,
                latestPolicyBucketDeltaBasisPoints: -400,
                evaluatedAt: new Date().toISOString(),
                postWindowFinalized: false,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
      />,
    );

    expect(screen.getByText("Latest policy health check")).toBeInTheDocument();
    expect(screen.getByText("−4.00%")).toBeInTheDocument();
    expect(screen.getByText("Cumulative performance appears in the comparison below.")).toBeInTheDocument();
  });

  it("derives stale display without hiding manual controls", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-18T01:01:00.000Z"));

    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          availableActions: {
            ...baseRollout.availableActions,
            continue: true,
            pause: true,
            abort: true,
          },
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "observing",
                totalCount: 3n,
                pairedCount: 3n,
                evaluatedAt: "2026-08-18T01:00:00.000Z",
                postWindowFinalized: false,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
        onContinue={vi.fn()}
        onPause={vi.fn()}
        onAbort={vi.fn()}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Evidence stale");
    expect(
      screen.getByText(
        "Telemetry samples or evaluator updates are older than 20 seconds. Manual controls remain available.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /more actions/i })).toBeInTheDocument();

    vi.useRealTimers();
  });

  it("announces verdict changes but keeps routine metric updates outside the live region", () => {
    const rollout = {
      ...baseRollout,
      state: "review" as const,
      batches: [
        {
          ...baseRollout.batches[0],
          state: "completed" as const,
          evidenceSummary: {
            status: "observing" as const,
            totalCount: 3n,
            pairedCount: 3n,
            cumulativeBaselineHashrateHs: 250_000_000_000_000,
            cumulativeCurrentHashrateHs: 249_000_000_000_000,
            evaluatedAt: new Date().toISOString(),
            postWindowFinalized: false,
          },
        },
      ],
    };
    const view = render(<BetweenChannelRolloutStatus rollout={rollout} laneLabel="Stable production" canControl />);

    const liveRegion = screen.getByRole("status");
    expect(liveRegion).toHaveAttribute("aria-live", "polite");
    expect(liveRegion).toHaveTextContent("Observing hashrate");
    expect(liveRegion).not.toHaveTextContent("249");
    expect(liveRegion).not.toHaveTextContent("Paired coverage");

    view.rerender(
      <BetweenChannelRolloutStatus
        rollout={{
          ...rollout,
          batches: [
            {
              ...rollout.batches[0],
              evidenceSummary: {
                ...rollout.batches[0].evidenceSummary,
                status: "held",
                cumulativeCurrentHashrateHs: 237_500_000_000_000,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Hashrate held");
  });

  it("lets an aggregate own the single polite live region", () => {
    render(
      <BetweenChannelRolloutStatus
        rollout={{
          ...baseRollout,
          state: "review",
          batches: [
            {
              ...baseRollout.batches[0],
              state: "completed",
              evidenceSummary: {
                status: "held",
                totalCount: 3n,
                pairedCount: 3n,
                evaluatedAt: new Date().toISOString(),
                postWindowFinalized: false,
              },
            },
          ],
        }}
        laneLabel="Stable production"
        canControl
        announceEvidenceStatus={false}
      />,
    );

    expect(screen.getByText("Hashrate held")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
