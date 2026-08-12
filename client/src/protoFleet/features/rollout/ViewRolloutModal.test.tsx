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
});
