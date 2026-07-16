import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ManageRacksModal from "./ManageRacksModal";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";

// Assert the picker forwards its `scope` prop into the listRacks fetch rather
// than reverting to an unscoped whole-org call — the regression the site
// scoping is meant to prevent (see #758 / buildingRackScope).
const mockListRacks = vi.fn();
const mockListBuildingsBySite = vi.fn();

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: mockListRacks }),
}));
vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildingsBySite: mockListBuildingsBySite }),
}));

const renderModal = (scope: SiteFilterFields) =>
  render(
    <ManageRacksModal
      open
      siteId={42n}
      currentBuildingId={7n}
      scope={scope}
      initialSelectedRackIds={[]}
      onDismiss={vi.fn()}
      onConfirm={vi.fn()}
    />,
  );

describe("ManageRacksModal fetch scoping", () => {
  beforeEach(() => {
    mockListRacks.mockReset();
    mockListBuildingsBySite.mockReset();
    // Resolve the building-label lookup with no rows so the effect settles.
    mockListBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  });

  it("passes the scope's siteIds/includeUnassigned into listRacks", async () => {
    renderModal({ siteIds: [42n], includeUnassigned: true });
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    expect(mockListRacks).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [42n], includeUnassigned: true }));
  });

  it("forwards a site-unassigned scope unchanged (no whole-org fallback)", async () => {
    renderModal({ siteIds: [], includeUnassigned: true });
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    const arg = mockListRacks.mock.calls[0][0];
    expect(arg.siteIds).toEqual([]);
    expect(arg.includeUnassigned).toBe(true);
  });
});
