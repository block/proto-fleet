import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import userEvent from "@testing-library/user-event";
import DeletePartModal from "./TicketInventory/DeletePartModal";

const DismissibleDelete = ({ onDelete }: { onDelete: () => Promise<boolean> }) => {
  const [open, setOpen] = useState(true);
  if (!open) return null;
  const onDismiss = () => setOpen(false);
  return <DeletePartModal partName="Fan" onDismiss={onDismiss} onDelete={onDelete} />;
};

describe("part delete confirmation", () => {
  it.each([
    { dismiss: "Escape", success: true },
    { dismiss: "header close", success: true },
    { dismiss: "Escape", success: false },
    { dismiss: "header close", success: false },
  ])("blocks $dismiss while pending and restores dismissal after success=$success", async ({ dismiss, success }) => {
    const user = userEvent.setup();
    let finish!: (value: boolean) => void;
    const pending = new Promise<boolean>((resolve) => {
      finish = resolve;
    });
    render(<DismissibleDelete onDelete={() => pending} />);
    const deleteLabel = "Delete part";
    await user.click(screen.getByRole("button", { name: deleteLabel }));
    expect(screen.getByRole("button", { name: deleteLabel })).toBeDisabled();
    const attemptDismiss = () =>
      dismiss === "Escape"
        ? user.keyboard("{Escape}")
        : user.click(screen.getByRole("button", { name: "Close dialog" }));
    await attemptDismiss();
    expect(screen.getByRole("button", { name: deleteLabel })).toBeInTheDocument();
    await act(async () => finish(success));
    if (!success) {
      expect(screen.getByRole("alert")).toHaveTextContent("Unable to delete");
      expect(screen.getByRole("button", { name: deleteLabel })).toBeEnabled();
      await attemptDismiss();
    }
    expect(screen.queryByRole("button", { name: deleteLabel })).not.toBeInTheDocument();
  });
});
