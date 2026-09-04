import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import InventoryTab from "./InventoryTab";
const state = { canManage: true };
const inventory = {
  data: [
    {
      id: "1",
      name: "Fan",
      type: "Cooling",
      manufacturer: "Acme",
      partNumber: "F1",
      siteId: "2",
      siteName: "Denver",
      onHand: 5,
      allocated: 1,
      available: 4,
      reorderPoint: 2,
      binLocation: "A1",
      lowStock: false,
      createdAt: null,
      updatedAt: null,
    },
  ],
  insights: { totalOnHand: 5, totalAllocated: 1, lowStockCount: 0, sitesCount: 1, partTypes: ["Cooling"] },
  loading: false,
  error: null,
  nextPageToken: "",
  total: 1,
  currentPage: 0,
  hasPreviousPage: false,
  setFilter: vi.fn(),
  nextPage: vi.fn(),
  previousPage: vi.fn(),
  adjust: vi.fn(),
  remove: vi.fn(),
  create: vi.fn(),
  previewCsv: vi.fn(),
  applyCsv: vi.fn(),
};
vi.mock("@/protoFleet/features/maintenance/hooks/useInventory", () => ({ useInventory: () => inventory }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ sites: [{ id: "2", name: "Denver" }] }),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => state.canManage }));
beforeEach(() => {
  state.canManage = true;
  vi.clearAllMocks();
});
it("renders the complete top-line summary and mutation controls", () => {
  render(<InventoryTab />);
  expect(screen.getByText("Fan")).toBeInTheDocument();
  expect(screen.getByText("Total on hand").parentElement).toHaveTextContent("5");
  expect(screen.getByText("Allocated to repairs").parentElement).toHaveTextContent("1");
  expect(screen.getByText("Low stock items").parentElement).toHaveTextContent("0");
  expect(screen.getByText("Sites").parentElement).toHaveTextContent("1");
  expect(screen.getByRole("button", { name: "Add part" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Import CSV" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Export/ })).not.toBeInTheDocument();
});
it("uses compact site, type, and low-stock controls", async () => {
  const user = userEvent.setup();
  render(<InventoryTab />);

  await user.click(screen.getByRole("button", { name: "Site" }));
  await user.click(screen.getByTestId("filter-option-2"));
  expect(inventory.setFilter).toHaveBeenLastCalledWith({ siteIds: [2n], types: [], lowStockOnly: false });

  await user.click(screen.getByRole("button", { name: "Type" }));
  await user.click(screen.getByTestId("filter-option-Cooling"));
  expect(inventory.setFilter).toHaveBeenLastCalledWith({
    siteIds: [2n],
    types: ["Cooling"],
    lowStockOnly: false,
  });

  await user.click(screen.getByRole("button", { name: "Low stock" }));
  expect(inventory.setFilter).toHaveBeenLastCalledWith({
    siteIds: [2n],
    types: ["Cooling"],
    lowStockOnly: true,
  });
});

it("uses the low-stock metric as a filter shortcut", async () => {
  const user = userEvent.setup();
  render(<InventoryTab />);
  await user.click(screen.getByRole("button", { name: "Show low stock items" }));
  expect(inventory.setFilter).toHaveBeenLastCalledWith({ siteIds: [], types: [], lowStockOnly: true });
});

it("renders standard page controls instead of Load more", () => {
  inventory.total = 51;
  inventory.nextPageToken = "next";
  render(<InventoryTab />);
  expect(screen.getByRole("button", { name: "Next page" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  inventory.total = 1;
  inventory.nextPageToken = "";
});

it("renders mutation controls when inventory is empty", () => {
  const parts = inventory.data;
  inventory.data = [];
  render(<InventoryTab />);
  expect(screen.getByText("No inventory parts")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add part" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Import CSV" })).toBeInTheDocument();
  inventory.data = parts;
});

it("hides mutation controls from read-only users", () => {
  state.canManage = false;
  render(<InventoryTab />);
  expect(screen.queryByRole("button", { name: "Add part" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Import CSV" })).not.toBeInTheDocument();
});
