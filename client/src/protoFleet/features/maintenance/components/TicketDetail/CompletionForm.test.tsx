import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import CompletionForm from "./CompletionForm";

const listPartsBySite = vi.fn(async ({ onSuccess }) => {
  onSuccess([
    {
      id: 7n,
      name: "Fan",
      onHand: 10,
      allocated: 2,
    },
  ]);
});

vi.mock("@/protoFleet/api/inventory", () => ({ useInventoryApi: () => ({ listPartsBySite }) }));

it("preserves existing reservations when completing without editing parts", async () => {
  const onSubmit = vi.fn(async () => true);
  render(
    <CompletionForm
      siteId="11"
      initialParts={[{ inventoryPartId: "7", partName: "Fan", quantity: 2 }]}
      onSubmit={onSubmit}
      onCancel={vi.fn()}
    />,
  );

  const quantity = await screen.findByRole("spinbutton", { name: "Fan quantity" });
  expect(quantity).toHaveValue(2);
  expect(screen.getByText("Fan (10 available)")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Complete repair" }));
  await waitFor(() =>
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        partsSelection: [{ inventoryPartId: 7n, partName: "Fan", quantity: 2 }],
      }),
    ),
  );
});
