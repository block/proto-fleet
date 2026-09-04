import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import DeletePartModal from "./DeletePartModal";
it("keeps the modal open and reports a rejected delete", async () => {
  const user = userEvent.setup();
  render(<DeletePartModal partName="Fan" onDismiss={vi.fn()} onDelete={vi.fn(async () => false)} />);
  await user.click(screen.getByRole("button", { name: "Delete part" }));
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Release active allocations"));
});
