import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import AggregateRolloutStatus from "./AggregateRolloutStatus";
import {
  antminerS21AbortedChild,
  antminerS21CreatedChild,
  protoAlphaReviewChild,
  protoAlphaRunningChild,
  stableProductionLane,
} from "./betweenChannel.fixtures";
import type { RolloutGroup, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

function parent(children: RolloutRecord[]): RolloutGroup {
  return {
    id: "parent-1",
    laneId: stableProductionLane.id,
    name: "August model updates",
    reason: "test",
    resultRevision: 0n,
    terminalOutcome: "pending",
    resultReady: false,
    lifecycle: "active",
    activity: "review",
    needsAction: false,
    evidenceReadiness: "pending",
    models: [],
    children,
  };
}

function renderAggregate({
  children,
  focusedChildId = children[0]?.id ?? null,
  childMutationState,
  onFocusChange = vi.fn(),
  onAdmit = vi.fn(),
  onPause = vi.fn(),
}: {
  children: RolloutRecord[];
  focusedChildId?: string | null;
  childMutationState?: Record<string, { loading: boolean; error?: string }>;
  onFocusChange?: (childId: string | null) => void;
  onAdmit?: (child: RolloutRecord) => void;
  onPause?: (child: RolloutRecord) => void;
}) {
  return render(
    <AggregateRolloutStatus
      parent={parent(children)}
      children={children}
      focusedChildId={focusedChildId}
      laneLabel={stableProductionLane.label}
      canControl
      childMutationState={childMutationState}
      onFocusChange={onFocusChange}
      onAdmit={onAdmit}
      onPause={onPause}
      onResume={vi.fn()}
      onContinue={vi.fn()}
      onAbort={vi.fn()}
      onRevert={vi.fn()}
      onCompleteWithFailures={vi.fn()}
    />,
  );
}

describe("AggregateRolloutStatus", () => {
  afterEach(() => {
    act(() => {
      document.body.style.removeProperty("--phone-max-width");
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
      window.dispatchEvent(new Event("resize"));
    });
  });

  it("binds child expansion to its panel and owns one aggregate live region", async () => {
    const onFocusChange = vi.fn();
    renderAggregate({
      children: [protoAlphaRunningChild, antminerS21CreatedChild],
      onFocusChange,
    });

    const trigger = screen.getByRole("button", { name: /Proto Alpha.*running/ });
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(trigger).toHaveAttribute("aria-controls", "rollout-child-proto-alpha-child");
    expect(document.getElementById("rollout-child-proto-alpha-child")).toBeInTheDocument();
    expect(screen.getAllByTestId("aggregate-rollout-live-region")).toHaveLength(1);
    expect(screen.getAllByRole("status")).toHaveLength(1);

    await userEvent.click(screen.getByRole("button", { name: /Antminer S21.*created/ }));
    expect(onFocusChange).toHaveBeenCalledWith(antminerS21CreatedChild.id);
  });

  it("routes a child-only retry without removing its sibling", async () => {
    const onAdmit = vi.fn();
    renderAggregate({
      children: [protoAlphaRunningChild, antminerS21CreatedChild],
      focusedChildId: antminerS21CreatedChild.id,
      childMutationState: {
        [antminerS21CreatedChild.id]: {
          loading: false,
          error: "The model start response could not be confirmed.",
        },
      },
      onAdmit,
    });

    await userEvent.click(screen.getByRole("button", { name: "Retry model start" }));
    expect(onAdmit).toHaveBeenCalledWith(antminerS21CreatedChild);
    expect(screen.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
  });

  it("keeps the primary child action direct and destructive actions in More at phone width", async () => {
    act(() => {
      document.body.style.setProperty("--phone-max-width", "639");
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
      window.dispatchEvent(new Event("resize"));
    });
    const user = userEvent.setup();
    const view = renderAggregate({
      children: [protoAlphaReviewChild, antminerS21AbortedChild],
    });
    act(() => window.dispatchEvent(new Event("resize")));

    expect(screen.getByRole("button", { name: "Continue" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Abort rollout" })).not.toBeInTheDocument();
    const protoMore = screen.getByRole("button", { name: /More actions for Proto Alpha rollout/ });
    await waitFor(() => expect(protoMore).toHaveAttribute("aria-expanded", "false"));
    await user.click(protoMore);
    await user.click(await screen.findByText("Abort rollout"));
    expect(screen.getByText("Abort Proto Alpha rollout for 3 miners?")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    view.rerender(
      <AggregateRolloutStatus
        parent={parent([protoAlphaReviewChild, antminerS21AbortedChild])}
        children={[protoAlphaReviewChild, antminerS21AbortedChild]}
        focusedChildId={antminerS21AbortedChild.id}
        laneLabel={stableProductionLane.label}
        canControl
        onFocusChange={vi.fn()}
        onAdmit={vi.fn()}
        onPause={vi.fn()}
        onResume={vi.fn()}
        onContinue={vi.fn()}
        onAbort={vi.fn()}
        onRevert={vi.fn()}
        onCompleteWithFailures={vi.fn()}
      />,
    );
    act(() => window.dispatchEvent(new Event("resize")));

    await waitFor(() => expect(screen.queryByRole("button", { name: "Revert" })).not.toBeInTheDocument());
    const moreActions = screen.getByRole("button", { name: /More actions for Antminer S21 rollout/ });
    await user.click(moreActions);
    await user.click(within(await screen.findByTestId("active-rollout-more-actions-menu")).getByText("Revert"));
    expect(screen.getByText("Revert Antminer S21 for 1 confirmed miner?")).toBeInTheDocument();
    await user.click(screen.getByTestId("confirm-revert-rollout"));
    expect(moreActions).toHaveFocus();
  });
});
