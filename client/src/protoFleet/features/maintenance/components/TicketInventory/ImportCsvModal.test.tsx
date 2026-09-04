import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import ImportCsvModal from "./ImportCsvModal";
import { CsvPreviewRowSchema } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
it("retains exact bytes and blocks confirmation when any preview row is invalid", async () => {
  const user = userEvent.setup();
  const preview = vi.fn(async (_bytes: Uint8Array) => ({
    rows: [
      create(CsvPreviewRowSchema, {
        rowNumber: 2,
        name: "Fan",
        type: "Cooling",
        manufacturer: "Acme",
        partNumber: "F1",
        siteName: "Denver",
        onHand: 1,
        reorderPoint: 2,
        binLocation: "A",
        error: "unknown site",
      }),
    ],
    validCount: 0,
    errorCount: 1,
  }));
  render(<ImportCsvModal onDismiss={vi.fn()} onPreview={preview} onConfirm={vi.fn()} onSuccess={vi.fn()} />);
  const file = new File(["name,type"], "parts.csv", { type: "text/csv" });
  await user.upload(screen.getByLabelText("Inventory CSV"), file);
  await waitFor(() => expect(screen.getByText("Fix all errors before importing")).toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Import 0 parts" })).toBeDisabled();
  expect(preview.mock.calls[0][0]).toEqual(new Uint8Array(await file.arrayBuffer()));
});
