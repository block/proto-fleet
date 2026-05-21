import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SiteModals from "./SiteModals";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues } from "@/protoFleet/api/sites";
import { type SiteModalsApi } from "@/protoFleet/features/sites/hooks/useSiteModals";

// Builds a SiteModalsApi stub with vi.fn() handlers so each test can assert
// which callback fired. The `state` field is the only thing the component
// reads beyond the handlers, so it's all that needs realistic values.
const makeModals = (overrides: Partial<SiteModalsApi> = {}): SiteModalsApi => ({
  state: { kind: "none" },
  saving: false,
  deleting: false,
  openCreate: vi.fn(),
  openManageEdit: vi.fn(),
  openDeleteConfirm: vi.fn(),
  dismiss: vi.fn(),
  cancelAll: vi.fn(),
  detailsContinueCreate: vi.fn(),
  detailsSaveEdit: vi.fn(),
  manageEditDetails: vi.fn(),
  manageNetworkConfigChange: vi.fn(),
  manageSave: vi.fn().mockResolvedValue(null),
  deleteConfirm: vi.fn().mockResolvedValue(undefined),
  ...overrides,
});

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listBuildingsBySite: vi.fn().mockResolvedValue(undefined),
    listAllBuildings: vi.fn(),
    getBuilding: vi.fn(),
  }),
}));

describe("SiteModals", () => {
  it("clicking Delete in manageEditEditingDetails dispatches to onDeleteFromDetailsEdit", () => {
    const onDeleteFromDetailsEdit = vi.fn();
    const site = create(SiteSchema, { id: 42n, name: "North DC" });
    const modals = makeModals({
      state: { kind: "manageEditEditingDetails", site, draft: emptySiteFormValues() },
    });

    render(<SiteModals modals={modals} onDeleteFromDetailsEdit={onDeleteFromDetailsEdit} />);

    fireEvent.click(screen.getByTestId("site-details-modal-delete"));

    expect(onDeleteFromDetailsEdit).toHaveBeenCalled();
    // Delete must NOT call deleteConfirm directly — that's the dialog's job.
    expect(modals.deleteConfirm).not.toHaveBeenCalled();
  });

  it("renders the cascade dialog when state is deleteConfirm", () => {
    const siteWithCounts = create(SiteWithCountsSchema, {
      site: create(SiteSchema, { id: 42n, name: "North DC" }),
      deviceCount: 3n,
      rackCount: 1n,
      buildingCount: 0n,
    });
    const modals = makeModals({
      state: { kind: "deleteConfirm", site: siteWithCounts },
    });

    render(<SiteModals modals={modals} onDeleteFromDetailsEdit={() => undefined} />);

    expect(screen.getByTestId("site-delete-dialog")).toBeInTheDocument();
    expect(screen.getByText(/Delete site "North DC"\?/)).toBeInTheDocument();
  });

  it("Delete in manageCreateEditingDetails cancels all (no cascade dialog)", () => {
    const onDeleteFromDetailsEdit = vi.fn();
    const modals = makeModals({
      state: { kind: "manageCreateEditingDetails", draft: { ...emptySiteFormValues(), name: "Pending" } },
    });

    render(<SiteModals modals={modals} onDeleteFromDetailsEdit={onDeleteFromDetailsEdit} />);

    fireEvent.click(screen.getByTestId("site-details-modal-delete"));

    expect(modals.cancelAll).toHaveBeenCalled();
    expect(onDeleteFromDetailsEdit).not.toHaveBeenCalled();
  });
});
