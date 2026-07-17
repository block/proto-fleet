import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ManageRacksModal from "./ManageRacksModal";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";

// Assert the picker forwards its scope into the listRacks fetch (site scoping,
// #758) and drives the "Show assigned racks" toggle (#766): default-off hides
// already-placed racks, toggling on surfaces them and broadens the fetch to
// `assignedScope`.
// vi.hoisted so the handles exist when the hoisted vi.mock factories below run.
const mockListRacks = vi.hoisted(() => vi.fn());
const mockListBuildingsBySite = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: mockListRacks }),
}));
vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildingsBySite: mockListBuildingsBySite }),
}));

// buildingId 7n is "this building"; a rack under building 9n (same site 42n) is
// a reparent candidate ("In another building").
const createRack = (id: bigint, label: string, buildingId: bigint, siteId?: bigint, deviceCount = 0) =>
  create(DeviceSetSchema, {
    id,
    label,
    deviceCount,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, { rows: 1, columns: 1, buildingId, siteId }),
    },
  });

const SCOPE: SiteFilterFields = { siteIds: [42n], includeUnassigned: true };
const ALL_SITES_ASSIGNED_SCOPE: SiteFilterFields = { siteIds: [], includeUnassigned: false };

const renderModal = (overrides?: { scope?: SiteFilterFields; assignedScope?: SiteFilterFields }) =>
  render(
    <ManageRacksModal
      open
      siteId={42n}
      currentBuildingId={7n}
      scope={overrides?.scope ?? SCOPE}
      assignedScope={overrides?.assignedScope ?? SCOPE}
      buildingName="North"
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
    mockListRacks.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  });

  it("passes the scope's siteIds/includeUnassigned into listRacks", async () => {
    renderModal();
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    expect(mockListRacks).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [42n], includeUnassigned: true }));
  });

  it("forwards a site-unassigned scope unchanged (no whole-org fallback)", async () => {
    renderModal({ scope: { siteIds: [], includeUnassigned: true } });
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    const arg = mockListRacks.mock.calls[0][0];
    expect(arg.siteIds).toEqual([]);
    expect(arg.includeUnassigned).toBe(true);
  });
});

describe("ManageRacksModal show-assigned toggle", () => {
  beforeEach(() => {
    mockListRacks.mockReset();
    mockListBuildingsBySite.mockReset();
    mockListBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    // Both an eligible rack (this building) and a reparent candidate (another
    // building, same site) come back on every fetch; the toggle governs which
    // are shown, not the fetch.
    mockListRacks.mockImplementation(({ onSuccess }) =>
      onSuccess?.([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Beta", 9n, 42n, 5)]),
    );
  });

  it("hides already-placed racks by default and surfaces them when toggled on", async () => {
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    // Default off: the reparent candidate is hidden.
    expect(screen.queryByText("Beta")).not.toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    // Toggled on: it surfaces.
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
    expect(screen.getByText("Alpha")).toBeInTheDocument();
  });

  it("broadens the fetch to assignedScope when toggled on (all-sites → global)", async () => {
    renderModal({ scope: SCOPE, assignedScope: ALL_SITES_ASSIGNED_SCOPE });
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    // Default fetch uses the site scope.
    expect(mockListRacks.mock.calls[0][0]).toEqual(
      expect.objectContaining({ siteIds: [42n], includeUnassigned: true }),
    );

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    // Toggle-on fetch broadens to the global assignedScope.
    await waitFor(() =>
      expect(mockListRacks).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [], includeUnassigned: false })),
    );
  });
});
