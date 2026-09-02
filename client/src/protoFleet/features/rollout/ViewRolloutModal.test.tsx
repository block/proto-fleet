import type { HTMLAttributes, ReactNode } from "react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { inProgressFirmwareEvent, inProgressWithErrorsFirmwareEvent } from "./rollout.fixtures";
import ViewRolloutModal from "./ViewRolloutModal";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

describe("ViewRolloutModal", () => {
  it("keeps Manage and Pause visible and moves miner and curtailment actions into More", async () => {
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
    const detailsGrid = screen.getByText("Scope").parentElement?.parentElement;
    const details = screen.getByTestId("active-rollout-details");
    expect(detailsGrid).toHaveClass("grid", "tablet:grid-cols-4");
    expect(detailsGrid).not.toHaveClass("flex-col");
    expect(details.compareDocumentPosition(screen.getByTestId("active-rollout-progress"))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(screen.getByText(inProgressFirmwareEvent.scopeLabel)).toBeVisible();
    expect(screen.queryByRole("button", { name: "View miners" })).not.toBeInTheDocument();
    expect(screen.queryByText("Cancel remaining")).not.toBeInTheDocument();
    expect(screen.queryByTestId("view-rollout-view-miners-action")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: `More actions for ${inProgressFirmwareEvent.title}` }));

    expect(screen.getByTestId("view-rollout-view-miners-action")).toBeInTheDocument();
    expect(screen.getByTestId("view-rollout-cancel-action")).toBeInTheDocument();
  });

  it("uses a full-screen rollout modal and opens both miner filters in a standalone modal", async () => {
    const user = userEvent.setup();
    render(
      <ViewRolloutModal
        event={inProgressWithErrorsFirmwareEvent}
        onDismiss={vi.fn()}
        onPause={vi.fn()}
        onViewMiners={vi.fn()}
        onViewErrors={vi.fn()}
      />,
    );

    const modalSurface = screen.getByTestId("view-rollout-modal").parentElement;
    expect(modalSurface).toHaveClass("h-full", "w-full", "rounded-none");
    expect(screen.getByTestId("view-rollout-header")).toHaveClass("bg-surface-base", "px-6", "pt-6", "pb-4");
    expect(screen.getByRole("button", { name: "Close rollout details" })).toBeVisible();
    expect(screen.getByTestId("view-rollout-content")).toHaveClass("mx-auto", "max-w-[800px]");
    const errorBanner = screen.getByTestId("active-rollout-error-banner");
    expect(errorBanner).toHaveClass("mt-0");
    expect(errorBanner.compareDocumentPosition(screen.getByTestId("active-rollout-primary-lockup"))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );

    await user.click(screen.getByRole("button", { name: "Review errors" }));
    const minersModal = screen.getByTestId("rollout-miners-modal");
    expect(minersModal).toBeVisible();
    expect(within(minersModal).getByText("3 errors affecting 4 miners")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Back to update details" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByTestId("view-rollout-modal")).toBeVisible();
    expect(screen.getByText("Batch 5 of 12")).toBeVisible();

    await user.click(
      screen.getByRole("button", { name: `More actions for ${inProgressWithErrorsFirmwareEvent.title}` }),
    );
    await user.click(screen.getByTestId("view-rollout-view-miners-action"));
    const allMinersModal = screen.getByTestId("rollout-miners-modal");
    expect(allMinersModal).toBeVisible();
    expect(within(allMinersModal).getByText("12 of 222 included miners shown")).toBeVisible();
  });
});
