import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ManageRacksModal from "./ManageRacksModal";
import { type RackSelectionDelta } from "./rackSelectionDelta";
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

const renderModal = (overrides?: {
  scope?: SiteFilterFields;
  assignedScope?: SiteFilterFields;
  onConfirm?: (delta: RackSelectionDelta) => void;
  initialSelectedRackIds?: bigint[];
}) =>
  render(
    <ManageRacksModal
      open
      siteId={42n}
      currentBuildingId={7n}
      scope={overrides?.scope ?? SCOPE}
      assignedScope={overrides?.assignedScope ?? SCOPE}
      buildingName="North"
      initialSelectedRackIds={overrides?.initialSelectedRackIds ?? []}
      onDismiss={vi.fn()}
      onConfirm={overrides?.onConfirm ?? vi.fn()}
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

  // Rows sort alphabetically, so body checkbox 0 = Alpha (eligible), 1 = Beta
  // (reparent candidate).
  const rowCheckbox = (index: number) =>
    screen.getByTestId("list-body").querySelectorAll<HTMLInputElement>("input[type='checkbox']")[index];

  it("header select-all does not bulk-select reparent candidates (security guard)", async () => {
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    // The table header "select all" would otherwise batch in the reparent row.
    const selectAll = screen.getByTestId("select-all-checkbox").querySelector("input")!;
    await userEvent.click(selectAll);
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.reassigned).toEqual([]); // no accidental reparent
    expect(delta.added.map((a: { rackId: bigint }) => a.rackId)).toContain(1n); // eligible still selected
    expect(delta.added.map((a: { rackId: bigint }) => a.rackId)).not.toContain(2n);
  });

  it("header select-all excludes reparent rows even with an eligible row pre-picked", async () => {
    // Codex edge: with an eligible row already selected, the header checkbox
    // fires a setter that "adds" exactly the reparent id. A count-based guard
    // (>1) would let that lone id through; the structural isRowBulkSelectable
    // guard keeps it out regardless of prior selection.
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    // Pre-select the eligible row, then hit header select-all.
    await userEvent.click(rowCheckbox(0));
    const selectAll = screen.getByTestId("select-all-checkbox").querySelector("input")!;
    await userEvent.click(selectAll);
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.reassigned).toEqual([]);
    expect(delta.added.map((a: { rackId: bigint }) => a.rackId)).not.toContain(2n);
  });

  it("header deselect clears an explicit reparent pick (bulk guard gates add, not clear)", async () => {
    // Codex edge: pick the reparent row explicitly, then select the eligible
    // row so the header checkbox reads "checked", then click it to clear the
    // page. The bulk guard excludes the reparent row from *adding*, but clearing
    // must still remove it — otherwise the explicit pick survives a "clear the
    // page" gesture and Continue proceeds with an unintended reparent.
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    await userEvent.click(rowCheckbox(1)); // explicit reparent pick (Beta)
    await userEvent.click(rowCheckbox(0)); // eligible pick (Alpha) → header now checked
    const selectAll = screen.getByTestId("select-all-checkbox").querySelector("input")!;
    await userEvent.click(selectAll); // header now checked → this clears the page
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.reassigned).toEqual([]); // reparent pick cleared, not stranded
    expect(delta.added).toEqual([]);
  });

  it("keeps a seeded reparent rack when toggling off (does not silently remove it)", async () => {
    // Codex edge: Beta was reparented earlier in this unsaved session, so it is
    // seeded via initialSelectedRackIds — yet listRacks still reports it in its
    // old building (reassignment row) until Save. Toggling off must NOT strip a
    // seeded reparent, or Continue would report it in `removed` and undo the
    // accepted reparent.
    const onConfirm = vi.fn();
    renderModal({ onConfirm, initialSelectedRackIds: [2n] });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    // Toggle on, then off — the strip runs on toggle-off.
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await userEvent.click(screen.getByTestId("manage-racks-modal-confirm"));

    const delta = onConfirm.mock.calls[0][0];
    expect(delta.removed).not.toContain(2n); // seeded reparent preserved
    expect(delta.removed).toEqual([]);
  });

  it("selecting a reparent row then toggling off drops it from the delta", async () => {
    const onConfirm = vi.fn();
    renderModal({ onConfirm });
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
    await waitFor(() => expect(screen.getByText("Beta")).toBeInTheDocument());

    // Explicit per-row pick of the reparent candidate is allowed...
    await userEvent.click(rowCheckbox(1));
    // ...but toggling off hides it and must not leave it silently selected.
    await userEvent.click(screen.getByLabelText("Show assigned racks"));
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
});
