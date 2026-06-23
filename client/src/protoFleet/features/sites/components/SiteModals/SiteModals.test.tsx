import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SiteModals from "./SiteModals";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues } from "@/protoFleet/api/sites";
import { type SiteModalsApi } from "@/protoFleet/features/sites/hooks/useSiteModals";

// Builds a SiteModalsApi stub with vi.fn() handlers so each test can assert
// which callback fired. The `state` and `deleteTarget` fields are the only
// things the component reads beyond the handlers, so they're all that need
// realistic values per case.
const makeModals = (overrides: Partial<SiteModalsApi> = {}): SiteModalsApi => ({
  state: { kind: "none" },
  deleteTarget: null,
  saving: false,
  deleting: false,
  openCreate: vi.fn(),
  openManageEdit: vi.fn(),
  requestDeleteCurrent: vi.fn(),
  dismiss: vi.fn(),
  cancelAll: vi.fn(),
  dismissDeleteConfirm: vi.fn(),
  detailsContinueCreate: vi.fn(),
  detailsSaveEdit: vi.fn(),
  manageEditDetails: vi.fn(),
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
  it("Delete in manageEditEditingDetails resolves the cascade target via requestDeleteCurrent(sites)", () => {
    const site = create(SiteSchema, { id: 42n, name: "North DC" });
    const requestDeleteCurrent = vi.fn();
    const modals = makeModals({
      state: { kind: "manageEditEditingDetails", site, draft: emptySiteFormValues() },
      requestDeleteCurrent,
    });
    const sites = [
      create(SiteWithCountsSchema, {
        site: create(SiteSchema, { id: 42n, name: "North DC" }),
        deviceCount: 0n,
        rackCount: 0n,
        buildingCount: 0n,
      }),
    ];

    render(<SiteModals modals={modals} sites={sites} />);

    fireEvent.click(screen.getByTestId("site-settings-modal-delete"));

    expect(requestDeleteCurrent).toHaveBeenCalledWith(sites);
    expect(modals.deleteConfirm).not.toHaveBeenCalled();
  });

  it("renders the cascade dialog over the underlying modals when deleteTarget is set", () => {
    const site = create(SiteSchema, { id: 42n, name: "North DC" });
    const siteWithCounts = create(SiteWithCountsSchema, {
      site,
      deviceCount: 3n,
      rackCount: 1n,
      buildingCount: 0n,
    });
    const modals = makeModals({
      state: { kind: "manageEditEditingDetails", site, draft: emptySiteFormValues() },
      deleteTarget: siteWithCounts,
    });

    render(<SiteModals modals={modals} sites={undefined} />);

    // Both the underlying details modal and the cascade dialog render.
    expect(screen.getByTestId("site-delete-dialog")).toBeInTheDocument();
    expect(screen.getByText(/Delete site "North DC"\?/)).toBeInTheDocument();
  });

  it("Delete in manageCreateEditingDetails calls cancelAll (no cascade dialog)", () => {
    const modals = makeModals({
      state: { kind: "manageCreateEditingDetails", draft: { ...emptySiteFormValues(), name: "Pending" } },
    });

    render(<SiteModals modals={modals} sites={undefined} />);

    fireEvent.click(screen.getByTestId("site-settings-modal-delete"));

    expect(modals.cancelAll).toHaveBeenCalled();
    expect(modals.requestDeleteCurrent).not.toHaveBeenCalled();
  });

  it("Cancelling the cascade dialog dismisses only the dialog, leaving the underlying modals", () => {
    const site = create(SiteSchema, { id: 42n, name: "North DC" });
    const siteWithCounts = create(SiteWithCountsSchema, {
      site,
      deviceCount: 0n,
      rackCount: 0n,
      buildingCount: 0n,
    });
    const modals = makeModals({
      state: { kind: "manageEditEditingDetails", site, draft: emptySiteFormValues() },
      deleteTarget: siteWithCounts,
    });

    render(<SiteModals modals={modals} sites={undefined} />);

    fireEvent.click(screen.getByTestId("site-delete-dialog-cancel"));

    expect(modals.dismissDeleteConfirm).toHaveBeenCalled();
    expect(modals.dismiss).not.toHaveBeenCalled();
  });
});
