import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
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
  insights: { totalOnHand: 5, totalAllocated: 1, lowStockCount: 0, sitesCount: 1 },
  loading: false,
  error: null,
  nextPageToken: "",
  setFilter: vi.fn(),
  loadMore: vi.fn(),
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
it("renders live insights and mutation controls", () => {
  render(<InventoryTab />);
  expect(screen.getByText("Fan")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add part" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Import CSV" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Export/ })).not.toBeInTheDocument();
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
