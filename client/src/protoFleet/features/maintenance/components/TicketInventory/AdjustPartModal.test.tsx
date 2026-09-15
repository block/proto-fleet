import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import AdjustPartModal from "./AdjustPartModal";
import { AdjustmentReason } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
const part = {
  id: "1",
  name: "Fan",
  type: "Cooling",
  manufacturer: "",
  partNumber: "",
  siteId: "2",
  siteName: "Denver",
  onHand: 5,
  allocated: 0,
  available: 5,
  reorderPoint: 2,
  binLocation: "A1",
  lowStock: false,
  createdAt: null,
  updatedAt: null,
  updatedAtSnapshot: null,
};
const DismissibleAdjustment = ({ onSubmit }: { onSubmit: () => Promise<boolean> }) => {
  const [open, setOpen] = useState(true);
  return open ? <AdjustPartModal part={part} sites={[]} onDismiss={() => setOpen(false)} onSubmit={onSubmit} /> : null;
};

it.each([
  { dismiss: "Escape", success: true },
  { dismiss: "header close", success: true },
  { dismiss: "Escape", success: false },
  { dismiss: "header close", success: false },
])("blocks $dismiss during Save and restores dismissal after success=$success", async ({ dismiss, success }) => {
  const user = userEvent.setup();
  let finish!: (value: boolean) => void;
  const pending = new Promise<boolean>((resolve) => {
    finish = resolve;
  });
  render(<DismissibleAdjustment onSubmit={() => pending} />);

  await user.clear(screen.getByLabelText("On hand"));
  await user.type(screen.getByLabelText("On hand"), "8");
  await user.click(screen.getByRole("button", { name: "Reason" }));
  await user.click(screen.getByText("Received shipment"));
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

  const attemptDismiss = () =>
    dismiss === "Escape" ? user.keyboard("{Escape}") : user.click(screen.getByRole("button", { name: "Close dialog" }));
  await attemptDismiss();
  expect(screen.getByLabelText("On hand")).toHaveValue(8);

  await act(async () => finish(success));
  if (!success) {
    expect(screen.getByRole("alert")).toHaveTextContent("Unable to adjust part");
    expect(screen.getByLabelText("On hand")).toHaveValue(8);
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    await attemptDismiss();
  }
  expect(screen.queryByLabelText("On hand")).not.toBeInTheDocument();
});

it("submits the expected stock snapshot with an on-hand edit", async () => {
  const user = userEvent.setup();
  const submit = vi.fn(async () => true);
  render(<AdjustPartModal part={part} sites={[]} onDismiss={vi.fn()} onSubmit={submit} />);

  const onHand = screen.getByLabelText("On hand");
  await user.clear(onHand);
  await user.type(onHand, "8");
  await user.click(screen.getByRole("button", { name: "Reason" }));
  await user.click(screen.getByText("Received shipment"));
  await user.click(screen.getByRole("button", { name: "Save" }));

  expect(submit).toHaveBeenCalledWith({
    id: 1n,
    onHand: 8,
    expectedOnHand: 5,
    reason: AdjustmentReason.RECEIVED_SHIPMENT,
  });
});

it.each(["On hand", "Reorder point"])("keeps Save disabled when %s is blank", async (label) => {
  const user = userEvent.setup();
  render(<AdjustPartModal part={part} sites={[]} onDismiss={vi.fn()} onSubmit={vi.fn()} />);

  await user.clear(screen.getByLabelText(label));
  await user.click(screen.getByRole("button", { name: "Reason" }));
  await user.click(screen.getByText("Cycle count"));

  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
});

it("requires a reason and submits integer quantities", async () => {
  const user = userEvent.setup();
  const submit = vi.fn(async () => true);
  render(
    <AdjustPartModal
      part={part}
      sites={[
        { id: "2", name: "Denver" },
        { id: "3", name: "Repair Depot" },
      ]}
      onDismiss={vi.fn()}
      onSubmit={submit}
    />,
  );
  expect(screen.queryByLabelText("Notes")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Site" }));
  await user.click(screen.getByText("Repair Depot"));
  await user.click(screen.getByRole("button", { name: "Reason" }));
  await user.click(screen.getByText("Cycle count"));
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(submit).toHaveBeenCalledWith({
    id: 1n,
    siteId: 3n,
    reason: AdjustmentReason.CYCLE_COUNT,
  });
});
