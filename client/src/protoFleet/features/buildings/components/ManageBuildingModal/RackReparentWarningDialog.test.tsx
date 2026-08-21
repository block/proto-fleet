import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import RackReparentWarningDialog from "./RackReparentWarningDialog";

const racks = [{ rackId: 2n, label: "Beta", minerCount: 5 }];

describe("RackReparentWarningDialog", () => {
  it("dismisses on Escape", async () => {
    const onCancel = vi.fn();
    render(<RackReparentWarningDialog racks={racks} buildingName="North" onCancel={onCancel} onConfirm={vi.fn()} />);
    await userEvent.keyboard("{Escape}");
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("confirms the move (staging) via the Move button", async () => {
    // Confirming stages the reparent into the working set — no in-flight RPC, so
    // the action is a plain synchronous confirm.
    const onConfirm = vi.fn();
    render(<RackReparentWarningDialog racks={racks} buildingName="North" onCancel={vi.fn()} onConfirm={onConfirm} />);
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
