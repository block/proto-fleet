import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import DeleteRolloutLaneDialog from "./DeleteRolloutLaneDialog";

describe("DeleteRolloutLaneDialog", () => {
  it("explains membership cleanup and retained history before confirming", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    const onDismiss = vi.fn();
    render(
      <DeleteRolloutLaneDialog
        open
        laneLabel="Stable production"
        isSubmitting={false}
        error={null}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByText("Delete Stable production?")).toBeInTheDocument();
    expect(screen.getByText(/miners in this lane will become unmanaged/i)).toBeInTheDocument();
    expect(screen.getByText(/rollout and release history will be retained/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Delete lane" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("shows mutation errors and prevents duplicate submission", () => {
    render(
      <DeleteRolloutLaneDialog
        open
        laneLabel="Stable production"
        isSubmitting
        error="Lane rollout work is still active."
        onConfirm={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );

    expect(screen.getByText("Lane rollout work is still active.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete lane" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });
});
