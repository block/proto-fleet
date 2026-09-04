import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import ImportCsvModal from "./ImportCsvModal";
import { CsvPreviewRowSchema } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import type { InventoryCsvPreview } from "@/protoFleet/api/inventory";
it("summarizes valid rows without rendering a wide data preview", async () => {
  const user = userEvent.setup();
  const preview = vi.fn(async (_bytes: Uint8Array) => ({
    rows: [
      create(CsvPreviewRowSchema, {
        rowNumber: 2,
        name: "Fan",
        type: "Cooling",
        siteName: "Denver",
        onHand: 1,
      }),
    ],
    validCount: 1,
    errorCount: 0,
  }));
  render(<ImportCsvModal onDismiss={vi.fn()} onPreview={preview} onConfirm={vi.fn()} onSuccess={vi.fn()} />);
  await user.upload(screen.getByLabelText("Inventory CSV"), new File(["name,type"], "parts.csv"));

  await waitFor(() => expect(screen.getByText("1 part ready to import")).toBeInTheDocument());
  expect(screen.getByText("parts.csv")).toBeInTheDocument();
  expect(screen.queryByText("Fan")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Choose another" })).toBeInTheDocument();
});

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
  expect(screen.getByText("Row 2: unknown site")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Import 0 parts" })).toBeDisabled();
  expect(preview.mock.calls[0][0]).toEqual(new Uint8Array(await file.arrayBuffer()));
});

it("keeps the newest file when overlapping previews resolve out of order", async () => {
  const user = userEvent.setup();
  let resolveFirst!: (value: InventoryCsvPreview) => void;
  let resolveSecond!: (value: InventoryCsvPreview) => void;
  const preview = vi
    .fn<(bytes: Uint8Array) => Promise<InventoryCsvPreview>>()
    .mockImplementationOnce(() => new Promise((resolve) => (resolveFirst = resolve)))
    .mockImplementationOnce(() => new Promise((resolve) => (resolveSecond = resolve)));
  render(<ImportCsvModal onDismiss={vi.fn()} onPreview={preview} onConfirm={vi.fn()} onSuccess={vi.fn()} />);
  const input = screen.getByLabelText("Inventory CSV");

  await user.upload(input, new File(["first"], "first.csv"));
  await waitFor(() => expect(screen.getByText("first.csv")).toBeInTheDocument());
  await user.upload(input, new File(["second"], "second.csv"));
  await waitFor(() => expect(screen.getByText("second.csv")).toBeInTheDocument());

  resolveSecond({ rows: [], validCount: 2, errorCount: 0 });
  await waitFor(() => expect(screen.getByText("2 parts ready to import")).toBeInTheDocument());
  resolveFirst({ rows: [], validCount: 1, errorCount: 0 });

  await waitFor(() => expect(screen.getByText("2 parts ready to import")).toBeInTheDocument());
  expect(screen.queryByText("1 part ready to import")).not.toBeInTheDocument();
});
