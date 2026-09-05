import { render, screen } from "@testing-library/react";
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
};
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
