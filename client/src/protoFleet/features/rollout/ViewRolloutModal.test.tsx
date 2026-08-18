import type { HTMLAttributes, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { inProgressFirmwareEvent } from "./rollout.fixtures";
import ViewRolloutModal from "./ViewRolloutModal";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

describe("ViewRolloutModal", () => {
  it("keeps Manage and Pause visible and moves drill-in and curtailment actions into More", async () => {
    const user = userEvent.setup();
    render(
      <ViewRolloutModal
        event={inProgressFirmwareEvent}
        onDismiss={vi.fn()}
        onManage={vi.fn()}
        onPause={vi.fn()}
        onCancelRemaining={vi.fn()}
        onViewMiners={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Manage" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    expect(screen.queryByTestId("active-rollout-details-toggle")).not.toBeInTheDocument();
    expect(screen.getByText("Scope")).toBeVisible();
    expect(screen.getByText(inProgressFirmwareEvent.scopeLabel)).toBeVisible();
    expect(screen.queryByRole("button", { name: "View miners" })).not.toBeInTheDocument();
    expect(screen.queryByText("Cancel remaining")).not.toBeInTheDocument();
    expect(screen.queryByTestId("view-rollout-view-miners-action")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: `More actions for ${inProgressFirmwareEvent.title}` }));

    expect(screen.getByTestId("view-rollout-view-miners-action")).toBeInTheDocument();
    expect(screen.getByTestId("view-rollout-cancel-action")).toBeInTheDocument();
  });

  it("wires abort and revert as distinct eligible actions", async () => {
    const user = userEvent.setup();
    const onAbort = vi.fn();
    const onRevert = vi.fn();
    const { rerender } = render(
      <ViewRolloutModal
        event={{ ...inProgressFirmwareEvent, state: "running" }}
        onDismiss={vi.fn()}
        onAbort={onAbort}
        onRevert={onRevert}
      />,
    );

    await user.click(screen.getByRole("button", { name: `More actions for ${inProgressFirmwareEvent.title}` }));
    await user.click(screen.getByTestId("view-rollout-abort-action"));
    expect(onAbort).toHaveBeenCalledTimes(1);
    expect(onRevert).not.toHaveBeenCalled();

    rerender(
      <ViewRolloutModal
        event={{ ...inProgressFirmwareEvent, state: "aborted" }}
        onDismiss={vi.fn()}
        onAbort={onAbort}
        onRevert={onRevert}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Revert" }));
    expect(onRevert).toHaveBeenCalledTimes(1);
  });

  it("hides lifecycle controls when control permission is absent", () => {
    render(
      <ViewRolloutModal
        event={{ ...inProgressFirmwareEvent, state: "running" }}
        onDismiss={vi.fn()}
        canControl={false}
        onAbort={vi.fn()}
        onPause={vi.fn()}
      />,
    );

    expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("view-rollout-abort-action")).not.toBeInTheDocument();
  });

  it("renders membership and convergence progress independently", () => {
    render(
      <ViewRolloutModal
        event={{
          ...inProgressFirmwareEvent,
          membershipProgress: { completed: 3, total: 10 },
          convergenceProgress: { completed: 7, total: 10, attentionRequired: 1 },
        }}
        onDismiss={vi.fn()}
      />,
    );

    expect(screen.getByTestId("active-rollout-membership-progress")).toHaveTextContent("3 of 10");
    expect(screen.getByTestId("active-rollout-convergence-progress")).toHaveTextContent("7 of 10");
    expect(screen.getByTestId("active-rollout-convergence-progress")).toHaveTextContent("1 needs attention");
  });
});
