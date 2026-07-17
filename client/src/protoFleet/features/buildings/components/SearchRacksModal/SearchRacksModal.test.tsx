import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import SearchRacksModal from "./SearchRacksModal";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";

// SearchRacksModal owns its own listRacks effect (separate from
// ManageRacksModal), so it needs independent coverage that the scope reaches the
// fetch (#758) and the "Show assigned racks" toggle surfaces reparent
// candidates and reports the reassignment on confirm (#766).
// vi.hoisted so the handles exist when the hoisted vi.mock factories below run.
const mockListRacks = vi.hoisted(() => vi.fn());
const mockListBuildingsBySite = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: mockListRacks }),
}));
vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildingsBySite: mockListBuildingsBySite }),
}));

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

const renderModal = (onConfirm = vi.fn(), overrides?: { assignedScope?: SiteFilterFields }) =>
  render(
    <SearchRacksModal
      open
      siteId={42n}
      currentBuildingId={7n}
      scope={SCOPE}
      assignedScope={overrides?.assignedScope ?? SCOPE}
      buildingName="North"
      onDismiss={vi.fn()}
      onConfirm={onConfirm}
    />,
  );

describe("SearchRacksModal fetch scoping", () => {
  beforeEach(() => {
    mockListRacks.mockReset();
    mockListBuildingsBySite.mockReset();
    mockListBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockListRacks.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  });

  it("passes the scope's siteIds/includeUnassigned into listRacks", async () => {
    renderModal();
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    expect(mockListRacks).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [42n], includeUnassigned: true }));
  });

  it("forwards a site-unassigned scope unchanged (no whole-org fallback)", async () => {
    render(
      <SearchRacksModal
        open
        siteId={42n}
        currentBuildingId={7n}
        scope={{ siteIds: [], includeUnassigned: true }}
        assignedScope={{ siteIds: [], includeUnassigned: true }}
        buildingName="North"
        onDismiss={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    const arg = mockListRacks.mock.calls[0][0];
    expect(arg.siteIds).toEqual([]);
    expect(arg.includeUnassigned).toBe(true);
  });
});

describe("SearchRacksModal show-assigned toggle + reparent reporting", () => {
  beforeEach(() => {
    mockListRacks.mockReset();
    mockListBuildingsBySite.mockReset();
    mockListBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
    mockListRacks.mockImplementation(({ onSuccess }) =>
      onSuccess?.([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Beta", 9n, 42n, 5)]),
    );
  });

  it("hides already-placed racks by default and surfaces them when toggled on", async () => {
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    expect(screen.queryByText("Beta")).not.toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
  });

  // Rows sort alphabetically, so body checkbox 0 = Alpha, 1 = Beta.
  const rowCheckbox = (index: number) =>
    screen.getByTestId("list-body").querySelectorAll<HTMLInputElement>("input[type='checkbox']")[index];

  it("reports the reassignment (with miner count) when a placed rack is chosen", async () => {
    const onConfirm = vi.fn();
    renderModal(onConfirm);
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    // Select the reparent candidate (Beta), then Assign.
    await userEvent.click(rowCheckbox(1));
    await userEvent.click(screen.getByTestId("search-racks-modal-confirm"));

    expect(onConfirm).toHaveBeenCalledWith(2n, "Beta", { rackId: 2n, label: "Beta", minerCount: 5 });
  });

  it("omits the reparent descriptor for an eligible rack", async () => {
    const onConfirm = vi.fn();
    renderModal(onConfirm);
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    await userEvent.click(rowCheckbox(0));
    await userEvent.click(screen.getByTestId("search-racks-modal-confirm"));

    expect(onConfirm).toHaveBeenCalledWith(1n, "Alpha", undefined);
  });
});
