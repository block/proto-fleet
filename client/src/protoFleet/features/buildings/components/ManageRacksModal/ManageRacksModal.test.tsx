import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ManageRacksModal from "./ManageRacksModal";
import { type RackSelectionDelta } from "./rackSelectionDelta";
import {
  type DeviceSet,
  DeviceSetSchema,
  RackInfoSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";

// Part C (#765): ManageRacksModal fetches server-side — the toggle-off request
// is pinned to the assignable set (this building + no-building racks), the
// toggle-on request broadens to `assignedScope`, and Building/Site facets travel
// on the request. There is NO client-side filtering of a fetched page, so these
// tests drive a realistic mock that filters + paginates exactly like the server.
const mockListRacks = vi.hoisted(() => vi.fn());
const mockListBuildingsBySite = vi.hoisted(() => vi.fn());
const mockListBuildings = vi.hoisted(() => vi.fn());
const mockListSites = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listRacks: mockListRacks }),
}));
vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildingsBySite: mockListBuildingsBySite, listBuildings: mockListBuildings }),
}));
vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({ listSites: mockListSites }),
}));
vi.mock("@/protoFleet/store", () => ({
  useHasPermission: () => true,
}));

// buildingId 7n is "this building"; a rack under building 9n (same site 42n) is
// a reparent candidate ("In another building"). building 0n = no building.
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

interface FetchReq {
  siteIds: bigint[];
  includeUnassigned: boolean;
  buildingIds: bigint[];
  includeNoBuilding: boolean;
  pageSize?: number;
  pageToken?: string;
}

// Server-equivalent match: site scope AND building scope, each null-permissive
// via the include flags. Empty ids + false flag on a dimension = no filter on it.
const matchesReq = (rack: DeviceSet, req: FetchReq): boolean => {
  if (rack.typeDetails.case !== "rackInfo") return false;
  const b = rack.typeDetails.value.buildingId ?? 0n;
  const s = rack.typeDetails.value.siteId ?? 0n;
  const siteOk =
    req.siteIds.length === 0 && !req.includeUnassigned
      ? true
      : req.siteIds.includes(s) || (req.includeUnassigned && s === 0n);
  const buildingOk =
    req.buildingIds.length === 0 && !req.includeNoBuilding
      ? true
      : req.buildingIds.includes(b) || (req.includeNoBuilding && b === 0n);
  return siteOk && buildingOk;
};

// Install a mock that filters `all` by the request and paginates by pageSize/
// pageToken (token = numeric offset as a string). No pageSize → return all
// matches (the fetch-all path used by footer select-all).
const setupListRacks = (all: DeviceSet[]) => {
  mockListRacks.mockImplementation((req: FetchReq & { onSuccess?: Function; onError?: Function }) => {
    const matched = all.filter((r) => matchesReq(r, req));
    if (!req.pageSize) {
      req.onSuccess?.(matched, "", matched.length);
      return;
    }
    const offset = req.pageToken ? Number(req.pageToken) : 0;
    const page = matched.slice(offset, offset + req.pageSize);
    const nextOffset = offset + req.pageSize;
    const nextToken = nextOffset < matched.length ? String(nextOffset) : "";
    req.onSuccess?.(page, nextToken, matched.length);
  });
};

const SCOPE: SiteFilterFields = { siteIds: [42n], includeUnassigned: true };
const ALL_SITES_ASSIGNED_SCOPE: SiteFilterFields = { siteIds: [], includeUnassigned: false };

const renderModal = (overrides?: {
  scope?: SiteFilterFields;
  assignedScope?: SiteFilterFields;
  allSites?: boolean;
  initialSelectedRackIds?: bigint[];
  onConfirm?: (delta: RackSelectionDelta) => void;
}) =>
  render(
    <ManageRacksModal
      open
      siteId={42n}
      currentBuildingId={7n}
      scope={overrides?.scope ?? SCOPE}
      assignedScope={overrides?.assignedScope ?? SCOPE}
      allSites={overrides?.allSites ?? false}
      buildingName="North"
      initialSelectedRackIds={overrides?.initialSelectedRackIds ?? []}
      onDismiss={vi.fn()}
      onConfirm={overrides?.onConfirm ?? vi.fn()}
    />,
  );

const lastRackReq = (): FetchReq => mockListRacks.mock.calls[mockListRacks.mock.calls.length - 1][0];
const reqsMatching = (pred: (r: FetchReq) => boolean): FetchReq[] =>
  mockListRacks.mock.calls.map((c) => c[0]).filter(pred);

beforeEach(() => {
  mockListRacks.mockReset();
  mockListBuildingsBySite.mockReset();
  mockListBuildings.mockReset();
  mockListSites.mockReset();
  mockListBuildingsBySite.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  mockListBuildings.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  mockListSites.mockImplementation(({ onSuccess }) => onSuccess?.([]));
  setupListRacks([]);
});

describe("ManageRacksModal fetch scoping (server-side)", () => {
  it("pins the toggle-off request to the assignable set (site scope + this building + no-building)", async () => {
    setupListRacks([createRack(1n, "Alpha", 7n, 42n)]);
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    const req = lastRackReq();
    expect(req.siteIds).toEqual([42n]);
    expect(req.includeUnassigned).toBe(true);
    expect(req.buildingIds).toEqual([7n]);
    expect(req.includeNoBuilding).toBe(true);
    expect(req.pageSize).toBe(50);
  });

  it("forwards a site-unassigned scope unchanged (no whole-org fallback)", async () => {
    renderModal({ scope: { siteIds: [], includeUnassigned: true } });
    await waitFor(() => expect(mockListRacks).toHaveBeenCalled());
    const req = lastRackReq();
    expect(req.siteIds).toEqual([]);
    expect(req.includeUnassigned).toBe(true);
    // Still pinned to this building so other-building racks never leak.
    expect(req.buildingIds).toEqual([7n]);
  });
});

describe("ManageRacksModal show-assigned toggle (server-side)", () => {
  beforeEach(() => {
    // Alpha is eligible (this building); Beta is a reparent candidate (another
    // building, same site). The server (mock) decides which are returned.
    setupListRacks([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Beta", 9n, 42n, 5)]);
  });

  it("excludes already-placed racks from the toggle-off fetch and surfaces them when toggled on", async () => {
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    // Default off: Beta was excluded server-side (building 9 ∉ {7, none}).
    expect(screen.queryByText("Beta")).not.toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
    expect(screen.getByText("Alpha")).toBeInTheDocument();
  });

  it("drops the building pin and broadens to assignedScope when toggled on (all-sites → global)", async () => {
    renderModal({ scope: SCOPE, assignedScope: ALL_SITES_ASSIGNED_SCOPE });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    expect(lastRackReq()).toEqual(expect.objectContaining({ siteIds: [42n], buildingIds: [7n] }));

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
    const req = lastRackReq();
    expect(req.siteIds).toEqual([]);
    expect(req.includeUnassigned).toBe(false);
    expect(req.buildingIds).toEqual([]);
    expect(req.includeNoBuilding).toBe(false);
  });

  const rowCheckbox = (index: number) =>
    screen.getByTestId("list-body").querySelectorAll<HTMLInputElement>("input[type='checkbox']")[index];

  it("header select-all selects the whole page including reparent rows and reports them in reassigned", async () => {
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    const selectAll = screen.getByTestId("select-all-checkbox").querySelector("input")!;
    await userEvent.click(selectAll);
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.added.map((a: { rackId: bigint }) => a.rackId)).toEqual(expect.arrayContaining([1n, 2n]));
    expect(delta.reassigned).toEqual([{ rackId: 2n, label: "Beta", minerCount: 5 }]);
  });

  it("hides the footer 'Select all' while assigned racks are shown, and restores it when hidden", async () => {
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Select all" })).toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Select all" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.queryByText("Beta")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Select all" })).toBeInTheDocument();
  });

  it("selecting a reparent row then toggling off drops it from the delta", async () => {
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    await userEvent.click(rowCheckbox(1)); // reparent pick (Beta)
    await userEvent.click(screen.getByLabelText("Show assigned racks")); // toggle off
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.reassigned).toEqual([]);
    expect(delta.added.map((a: { rackId: bigint }) => a.rackId)).not.toContain(2n);
  });

  it("allows an explicit single per-row reparent pick through the delta", async () => {
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    await userEvent.click(rowCheckbox(1));
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.reassigned).toEqual([{ rackId: 2n, label: "Beta", minerCount: 5 }]);
  });

  it("recovers when the broadened (toggle-on) fetch fails", async () => {
    // The scoped fetch succeeds; the broadened all-sites fetch (siteIds empty)
    // fails. The failure must revert the toggle, keep the picker usable, and not
    // strand the operator behind the blocking error state.
    mockListRacks.mockImplementation((req: FetchReq & { onSuccess?: Function; onError?: Function }) => {
      if (req.siteIds.length === 0) {
        req.onError?.("network down");
        return;
      }
      req.onSuccess?.([createRack(1n, "Alpha", 7n, 42n)], "", 1);
    });

    renderModal({ scope: SCOPE, assignedScope: ALL_SITES_ASSIGNED_SCOPE });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    await userEvent.click(screen.getByLabelText("Show assigned racks"));

    await waitFor(() => expect(screen.getByLabelText("Show assigned racks")).not.toBeChecked());
    expect(screen.queryByTestId("manage-racks-modal-error")).not.toBeInTheDocument();
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.queryByText("Beta")).not.toBeInTheDocument();
  });

  it("never reports a seeded rack absent from the fetch as removed", async () => {
    // A seeded rack (id 3) that the eligibility-pinned fetch doesn't return
    // (paging gap / soft-delete window) must be left alone, not unassigned.
    const onConfirm = vi.fn();
    renderModal({ initialSelectedRackIds: [1n, 3n], onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.removed).toEqual([]);
    expect(delta.added).toEqual([]);
  });
});

describe("ManageRacksModal facets → request (server-side)", () => {
  it("translates a Building facet into buildingIds on the request", async () => {
    setupListRacks([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Beta", 9n, 42n, 5)]);
    mockListBuildings.mockImplementation(({ onSuccess }) =>
      onSuccess?.([{ building: { id: 7n, name: "North" } }, { building: { id: 9n, name: "South" } }]),
    );
    renderModal();
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    // Show assigned so the Building facet narrows a cross-building set.
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    await userEvent.click(screen.getByTestId("nested-dropdown-filter-row-building"));
    await userEvent.click(screen.getByTestId("filter-option-9")); // building 9 = "South"

    await waitFor(() => {
      const req = lastRackReq();
      expect(req.buildingIds).toEqual([9n]);
    });
  });

  it("offers the Site facet only when the header is all-sites", async () => {
    setupListRacks([createRack(1n, "Alpha", 7n, 42n)]);
    const { unmount } = renderModal({ allSites: false });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    expect(screen.getByTestId("nested-dropdown-filter-row-building")).toBeInTheDocument();
    expect(screen.queryByTestId("nested-dropdown-filter-row-site")).not.toBeInTheDocument();
    unmount();

    setupListRacks([createRack(1n, "Alpha", 7n, 42n)]);
    renderModal({ allSites: true });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    expect(screen.getByTestId("nested-dropdown-filter-row-site")).toBeInTheDocument();
  });

  it("translates a Site facet into siteIds on the request", async () => {
    setupListRacks([createRack(1n, "Alpha", 7n, 42n), createRack(3n, "Gamma", 5n, 99n)]);
    mockListSites.mockImplementation(({ onSuccess }) =>
      onSuccess?.([{ site: { id: 42n, name: "HQ" } }, { site: { id: 99n, name: "Remote" } }]),
    );
    renderModal({ allSites: true, assignedScope: ALL_SITES_ASSIGNED_SCOPE });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));

    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    await userEvent.click(screen.getByTestId("nested-dropdown-filter-row-site"));
    await userEvent.click(screen.getByTestId("filter-option-99")); // site 99 = "Remote"

    await waitFor(() => expect(lastRackReq().siteIds).toEqual([99n]));
  });
});

describe("ManageRacksModal server-side pagination", () => {
  it("pages through results, carrying the scope across pages, with a server total", async () => {
    // 60 eligible racks (this building) → two pages of 50 + 10.
    const many = Array.from({ length: 60 }, (_, i) =>
      createRack(BigInt(i + 1), `Rack ${String(i).padStart(2, "0")}`, 7n, 42n),
    );
    setupListRacks(many);
    renderModal();
    await waitFor(() => expect(screen.getByText("Showing 1–50 of 60 racks")).toBeInTheDocument());
    expect(lastRackReq().pageToken ?? "").toBe("");

    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(screen.getByText("Showing 51–60 of 60 racks")).toBeInTheDocument());
    // Page 2 carried the same scope (facet correctness across pages).
    const page2 = lastRackReq();
    expect(page2.pageToken).toBe("50");
    expect(page2.siteIds).toEqual([42n]);
    expect(page2.buildingIds).toEqual([7n]);

    await userEvent.click(screen.getByRole("button", { name: "Previous page" }));
    await waitFor(() => expect(screen.getByText("Showing 1–50 of 60 racks")).toBeInTheDocument());
  });

  it("footer 'Select all' fetches every eligible rack across pages, not just the current page", async () => {
    const many = Array.from({ length: 60 }, (_, i) =>
      createRack(BigInt(i + 1), `Rack ${String(i).padStart(2, "0")}`, 7n, 42n),
    );
    setupListRacks(many);
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Showing 1–50 of 60 racks")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    await waitFor(() => expect(screen.getByText("60 racks selected")).toBeInTheDocument());

    // The select-all fetch is unpaginated (no pageSize) over the eligible filter.
    const fetchAll = reqsMatching((r) => r.pageSize === undefined);
    expect(fetchAll.length).toBeGreaterThan(0);
    expect(fetchAll[fetchAll.length - 1].buildingIds).toEqual([7n]);

    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));
    expect(onConfirm.mock.calls[0][0].added).toHaveLength(60);
  });
});

// Regression tests for the automated PR-review findings on #789.
describe("ManageRacksModal review fixes (#789)", () => {
  it("removes an explicitly deselected seed even when it is absent from the fetch", async () => {
    // Seed 3 is off-page / pinned out of the assignable fetch (only Alpha=1 is
    // returned). Clicking "Select none" is an explicit clear, so both seeds must
    // be reported removed — the accumulator is placeholder-seeded so the absent
    // seed is still actionable. (Contrast the untouched-seed test above, which
    // preserves it.)
    const onConfirm = vi.fn();
    setupListRacks([createRack(1n, "Alpha", 7n, 42n)]);
    renderModal({ initialSelectedRackIds: [1n, 3n], onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Select none" }));
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.removed).toEqual(expect.arrayContaining([1n, 3n]));
    expect(delta.added).toEqual([]);
  });

  it("footer 'Select all' honors an active Building facet (excludes no-building racks)", async () => {
    // Building 7 has Alpha; there is also a no-building rack (Floating). With a
    // Building facet pinned to this building the visible set drops Floating, and
    // footer Select all must fetch the same effective request — not the raw
    // scope that would sweep Floating back in.
    setupListRacks([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Floating", 0n, 42n)]);
    mockListBuildings.mockImplementation(({ onSuccess }) => onSuccess?.([{ building: { id: 7n, name: "North" } }]));
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    // No facet: building 7 + no-building → both visible.
    expect(screen.getByText("Floating")).toBeInTheDocument();

    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    await userEvent.click(screen.getByTestId("nested-dropdown-filter-row-building"));
    await userEvent.click(screen.getByTestId("filter-option-7"));
    await waitFor(() => expect(screen.queryByText("Floating")).not.toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    await waitFor(() => expect(screen.getByText("1 rack selected")).toBeInTheDocument());

    const fetchAll = reqsMatching((r) => r.pageSize === undefined);
    expect(fetchAll[fetchAll.length - 1]).toEqual(
      expect.objectContaining({ buildingIds: [7n], includeNoBuilding: false }),
    );

    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));
    expect(onConfirm.mock.calls[0][0].added.map((a: { rackId: bigint }) => a.rackId)).toEqual([1n]);
  });

  it("treats a foreign Site facet as an empty assignable view and hides Select all (toggle off)", async () => {
    // all-sites; a cross-site no-building rack (Faraway, site 99) exists. Picking
    // a Site facet for a different site is unsatisfiable for the assignable set,
    // so the view goes empty rather than surfacing Faraway as a disabled
    // reassignment row — and footer Select all is hidden.
    setupListRacks([createRack(1n, "Alpha", 7n, 42n), createRack(2n, "Faraway", 0n, 99n)]);
    mockListSites.mockImplementation(({ onSuccess }) =>
      onSuccess?.([{ site: { id: 42n, name: "HQ" } }, { site: { id: 99n, name: "Remote" } }]),
    );
    renderModal({ allSites: true });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    await userEvent.click(screen.getByTestId("filter-nested-filters-meta"));
    await userEvent.click(screen.getByTestId("nested-dropdown-filter-row-site"));
    await userEvent.click(screen.getByTestId("filter-option-99"));

    await waitFor(() => expect(screen.queryByText("Alpha")).not.toBeInTheDocument());
    expect(screen.queryByText("Faraway")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Select all" })).not.toBeInTheDocument();
  });
});
