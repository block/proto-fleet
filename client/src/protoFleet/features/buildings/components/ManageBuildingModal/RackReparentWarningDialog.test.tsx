import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import RackReparentWarningDialog from "./RackReparentWarningDialog";

const racks = [{ rackId: 2n, label: "Beta", minerCount: 5 }];

describe("RackReparentWarningDialog", () => {
  it("dismisses on Escape when idle", async () => {
    const onCancel = vi.fn();
    render(<RackReparentWarningDialog racks={racks} buildingName="North" onCancel={onCancel} onConfirm={vi.fn()} />);
    await userEvent.keyboard("{Escape}");
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("cannot be dismissed while the move RPC is in flight", async () => {
    // With `busy`, Escape/outside-click must not fire onCancel — otherwise an
    // apparent cancel would hide the dialog while the in-flight promise still
    // resolves and commits the reparent.
    const onCancel = vi.fn();
    render(
      <RackReparentWarningDialog racks={racks} buildingName="North" busy onCancel={onCancel} onConfirm={vi.fn()} />,
    );
    await userEvent.keyboard("{Escape}");
    expect(onCancel).not.toHaveBeenCalled();
    // The footer actions are disabled too, so there is no path to dismiss.
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Moving…" })).toBeDisabled();
  });
});
