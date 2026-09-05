import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import CreatePartModal from "./CreatePartModal";
it("submits canonical part fields", async () => {
  const user = userEvent.setup();
  const submit = vi.fn(async () => true);
  render(<CreatePartModal sites={[{ id: "2", name: "Denver" }]} onDismiss={vi.fn()} onSubmit={submit} />);
  await user.type(screen.getByRole("textbox", { name: "Part name" }), "Fan");
  await user.type(screen.getByRole("textbox", { name: "Type" }), "Cooling");
  await user.click(screen.getByRole("button", { name: "Add part" }));
  expect(submit).toHaveBeenCalledWith(expect.objectContaining({ name: "Fan", type: "Cooling", onHand: 0 }));
});
