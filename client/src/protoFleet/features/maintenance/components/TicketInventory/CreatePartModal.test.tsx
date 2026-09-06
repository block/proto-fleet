import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import CreatePartModal from "./CreatePartModal";
const DismissibleCreation = ({ onSubmit }: { onSubmit: () => Promise<boolean> }) => {
  const [open, setOpen] = useState(true);
  return open ? <CreatePartModal sites={[]} onDismiss={() => setOpen(false)} onSubmit={onSubmit} /> : null;
};

it.each([
  { dismiss: "Escape", success: true },
  { dismiss: "header close", success: true },
  { dismiss: "Escape", success: false },
  { dismiss: "header close", success: false },
])("blocks $dismiss during creation and restores dismissal after success=$success", async ({ dismiss, success }) => {
  const user = userEvent.setup();
  let finish!: (value: boolean) => void;
  const pending = new Promise<boolean>((resolve) => {
    finish = resolve;
  });
  render(<DismissibleCreation onSubmit={() => pending} />);
  await user.type(screen.getByRole("textbox", { name: "Part name" }), "Fan");
  await user.type(screen.getByRole("textbox", { name: "Type" }), "Cooling");
  await user.click(screen.getByRole("button", { name: "Add part" }));
  expect(screen.getByRole("button", { name: "Add part" })).toBeDisabled();
  const attemptDismiss = () =>
    dismiss === "Escape" ? user.keyboard("{Escape}") : user.click(screen.getByRole("button", { name: "Close dialog" }));
  await attemptDismiss();
  expect(screen.getByRole("textbox", { name: "Part name" })).toHaveValue("Fan");
  await act(async () => finish(success));
  if (!success) {
    expect(screen.getByRole("alert")).toHaveTextContent("Unable to add part");
    expect(screen.getByRole("textbox", { name: "Part name" })).toHaveValue("Fan");
    expect(screen.getByRole("button", { name: "Add part" })).toBeEnabled();
    await attemptDismiss();
  }
  expect(screen.queryByRole("textbox", { name: "Part name" })).not.toBeInTheDocument();
});

it.each(["On hand", "Reorder point"])("keeps Add part disabled when %s is blank", async (label) => {
  const user = userEvent.setup();
  render(<CreatePartModal sites={[]} onDismiss={vi.fn()} onSubmit={vi.fn()} />);
  await user.type(screen.getByRole("textbox", { name: "Part name" }), "Fan");
  await user.type(screen.getByRole("textbox", { name: "Type" }), "Cooling");
  await user.clear(screen.getByLabelText(label));

  expect(screen.getByRole("button", { name: "Add part" })).toBeDisabled();
});

it("submits canonical part fields", async () => {
  const user = userEvent.setup();
  const submit = vi.fn(async () => true);
  render(<CreatePartModal sites={[{ id: "2", name: "Denver" }]} onDismiss={vi.fn()} onSubmit={submit} />);
  await user.type(screen.getByRole("textbox", { name: "Part name" }), "Fan");
  await user.type(screen.getByRole("textbox", { name: "Type" }), "Cooling");
  await user.click(screen.getByRole("button", { name: "Add part" }));
  expect(submit).toHaveBeenCalledWith(expect.objectContaining({ name: "Fan", type: "Cooling", onHand: 0 }));
});
