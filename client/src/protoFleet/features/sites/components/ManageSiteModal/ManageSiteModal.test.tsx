import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ManageSiteModal from "./ManageSiteModal";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { SiteSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues } from "@/protoFleet/api/sites";

// Per-test seeding for the building fetch. listBuildingsBySite invokes the
// caller's onSuccess synchronously with whatever rows the test queued.
const { listBuildingsBySiteMock } = vi.hoisted(() => ({ listBuildingsBySiteMock: vi.fn() }));

// Keep the module's real helpers (emptyBuildingFormValues is used by both this
// modal and the BuildingSettingsModal it renders) and override only the hook.
vi.mock("@/protoFleet/api/buildings", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/api/buildings")>()),
  useBuildings: () => ({
    listBuildingsBySite: listBuildingsBySiteMock,
    listAllBuildings: vi.fn(),
    getBuilding: vi.fn(),
  }),
}));

// Stub the building picker: the real one self-fetches the org-wide building
// list and site catalog, which this suite doesn't wire up. All we need from it
// here are the two create hand-offs that reach the inline create flow. Each
// renders only when its callback is wired, mirroring the real picker.
vi.mock("../ManageBuildingsModal", () => ({
  default: ({
    onCreateNewLaunch,
    onCreateMultipleLaunch,
  }: {
    onCreateNewLaunch?: () => void;
    onCreateMultipleLaunch?: () => void;
  }) => (
    <>
      {onCreateNewLaunch ? (
        <button type="button" data-testid="stub-picker-create-new" onClick={onCreateNewLaunch}>
          Create building
        </button>
      ) : null}
      {onCreateMultipleLaunch ? (
        <button type="button" data-testid="stub-picker-create-multiple" onClick={onCreateMultipleLaunch}>
          Create multiple buildings
        </button>
      ) : null}
    </>
  ),
}));

const seedBuildings = (rows: { id: bigint; name: string; siteId: bigint; rackCount: bigint }[]) => {
  listBuildingsBySiteMock.mockImplementation((args?: { onSuccess?: (rows: unknown[]) => void }) => {
    args?.onSuccess?.(
      rows.map((r) =>
        create(BuildingWithCountsSchema, {
          building: create(BuildingSchema, { id: r.id, name: r.name, siteId: r.siteId }),
          rackCount: r.rackCount,
        }),
      ),
    );
    return Promise.resolve(undefined);
  });
};

const draft = (overrides: Partial<SiteFormValues> = {}): SiteFormValues => ({
  ...emptySiteFormValues(),
  name: "North DC",
  ...overrides,
});

const site7 = create(SiteSchema, { id: 7n, name: "East DC" });

const noop = () => undefined;

// Common props — the site is always persisted by the time the modal opens.
const baseProps = {
  open: true as const,
  site: site7,
  draft: draft({ name: "East DC" }),
  onAssignBuildings: vi.fn().mockResolvedValue(true),
  onRemoveBuilding: vi.fn().mockResolvedValue(true),
  onCreateBuilding: vi.fn().mockResolvedValue(null),
  onEditDetails: noop,
  onDeleteRequested: noop,
  onDismiss: noop,
};

describe("ManageSiteModal", () => {
  beforeEach(() => listBuildingsBySiteMock.mockReset());

  it("Save writes nothing and just closes (placement has no backend yet)", async () => {
    seedBuildings([]);
    const onAssignBuildings = vi.fn();
    const onDismiss = vi.fn();

    render(<ManageSiteModal {...baseProps} onAssignBuildings={onAssignBuildings} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByTestId("manage-site-modal-save"));

    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
    // Membership commits in the picker, so this CTA has nothing to persist.
    expect(onAssignBuildings).not.toHaveBeenCalled();
  });

  it("disables Save until the building list has loaded", () => {
    // No seed → listBuildingsBySite never calls onSuccess, so the list stays
    // in the loading (undefined) state.
    render(<ManageSiteModal {...baseProps} />);

    expect(screen.getByTestId("manage-site-modal-save")).toBeDisabled();
  });

  it("Site settings fires the parent callback", () => {
    seedBuildings([]);
    const onEditDetails = vi.fn();
    render(<ManageSiteModal {...baseProps} onEditDetails={onEditDetails} />);

    fireEvent.click(screen.getAllByTestId("manage-site-modal-edit-details")[0]);
    expect(onEditDetails).toHaveBeenCalled();
  });

  it("Delete site fires onDeleteRequested", () => {
    seedBuildings([]);
    const onDeleteRequested = vi.fn();
    render(<ManageSiteModal {...baseProps} onDeleteRequested={onDeleteRequested} />);

    fireEvent.click(screen.getAllByTestId("manage-site-modal-delete")[0]);
    expect(onDeleteRequested).toHaveBeenCalled();
  });

  it("creates a building inline via the picker hand-off and injects it into the working set", async () => {
    seedBuildings([]);
    const created = create(BuildingSchema, { id: 5n, name: "New Bldg", siteId: 7n });
    const onCreateBuilding = vi.fn().mockResolvedValue(created);
    render(<ManageSiteModal {...baseProps} onCreateBuilding={onCreateBuilding} />);

    // Create is reached through the picker's "New building" hand-off, not a
    // dedicated button on this modal.
    expect(screen.getByText("No buildings added to this site")).toBeInTheDocument();
    expect(screen.queryByTestId("manage-site-modal-create-building")).not.toBeInTheDocument();
    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    fireEvent.click(screen.getByTestId("stub-picker-create-new"));
    expect(screen.getByTestId("building-settings-modal")).toBeInTheDocument();

    // Name the building and save.
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "New Bldg" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));

    await waitFor(() => expect(onCreateBuilding).toHaveBeenCalled());
    // The created building is injected and the create modal closes.
    await waitFor(() => expect(screen.getByTestId("manage-site-modal-building-row-5")).toBeInTheDocument());
    expect(screen.queryByTestId("building-settings-modal")).not.toBeInTheDocument();
  });

  it("creates a batch of buildings inline and injects every returned row", async () => {
    seedBuildings([{ id: 1n, name: "Existing", siteId: 7n, rackCount: 0n }]);
    const onCreateBuildings = vi.fn().mockResolvedValue({
      created: [
        create(BuildingSchema, { id: 11n, name: "B-1", siteId: 7n }),
        create(BuildingSchema, { id: 12n, name: "B-2", siteId: 7n }),
      ],
      errors: [],
    });
    render(<ManageSiteModal {...baseProps} onCreateBuildings={onCreateBuildings} />);

    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    // "Create multiple buildings" opens the create modal already on its
    // Multiple side, so there's no toggle to press.
    fireEvent.click(screen.getByTestId("stub-picker-create-multiple"));
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B-" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));

    await waitFor(() =>
      expect(onCreateBuildings).toHaveBeenCalledWith([
        { name: "B-1", aisles: 0, racksPerAisle: 0 },
        { name: "B-2", aisles: 0, racksPerAisle: 0 },
      ]),
    );
    // Both rows join the existing member without a refetch, so any staged
    // picker state would survive.
    await waitFor(() => expect(screen.getByTestId("manage-site-modal-building-row-11")).toBeInTheDocument());
    expect(screen.getByTestId("manage-site-modal-building-row-12")).toBeInTheDocument();
    expect(screen.getByTestId("manage-site-modal-building-row-1")).toBeInTheDocument();
    expect(screen.queryByTestId("building-settings-modal")).not.toBeInTheDocument();
  });

  it("offers the batch create hand-off only when the host wired CreateBuildings", () => {
    seedBuildings([]);
    const { unmount } = render(<ManageSiteModal {...baseProps} onCreateBuilding={vi.fn()} />);
    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    // Without the batch RPC the create modal has no Multiple side to open on,
    // so the second button must not be offered.
    expect(screen.getByTestId("stub-picker-create-new")).toBeInTheDocument();
    expect(screen.queryByTestId("stub-picker-create-multiple")).not.toBeInTheDocument();
    unmount();

    render(<ManageSiteModal {...baseProps} onCreateBuildings={vi.fn()} />);
    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    expect(screen.getByTestId("stub-picker-create-multiple")).toBeInTheDocument();
  });

  it("blocks a batch whose generated name is already at the site", async () => {
    seedBuildings([{ id: 1n, name: "B-2", siteId: 7n, rackCount: 0n }]);
    const onCreateBuildings = vi.fn();
    render(<ManageSiteModal {...baseProps} onCreateBuildings={onCreateBuildings} />);

    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    fireEvent.click(screen.getByTestId("stub-picker-create-new"));
    fireEvent.mouseDown(screen.getByText("Multiple"));
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B-" } });

    // The site's own member list feeds the collision check, so the round trip
    // never happens.
    expect(screen.getByTestId("building-settings-bulk-preview-row-1")).toHaveTextContent("Already used at this site");
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onCreateBuildings).not.toHaveBeenCalled();
  });

  it("keeps the create modal open and injects nothing when create fails", async () => {
    seedBuildings([]);
    const onCreateBuilding = vi.fn().mockResolvedValue(null);
    render(<ManageSiteModal {...baseProps} onCreateBuilding={onCreateBuilding} />);

    fireEvent.click(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]);
    fireEvent.click(screen.getByTestId("stub-picker-create-new"));
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Nope" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));

    await waitFor(() => expect(onCreateBuilding).toHaveBeenCalled());
    // Modal stays open; no row was added.
    expect(screen.getByTestId("building-settings-modal")).toBeInTheDocument();
    expect(screen.getByText("No buildings added to this site")).toBeInTheDocument();
  });

  it("shows comma-separated meta on each corner of the preview", () => {
    seedBuildings([]);
    const site = create(SiteSchema, {
      id: 7n,
      name: "East DC",
      locationCity: "Boston",
      locationState: "MA",
      powerCapacityMw: 5,
    });
    render(
      <ManageSiteModal
        {...baseProps}
        site={site}
        draft={draft({
          name: "East DC",
          locationCity: "Boston",
          locationState: "MA",
          powerCapacityMw: 5,
        })}
      />,
    );
    expect(screen.getByText("East DC, Boston, MA")).toBeInTheDocument();
    expect(screen.getByText("5 MW, 0 buildings")).toBeInTheDocument();
  });

  it("renders rack count as a subtitle and kebab-remove unassigns immediately", async () => {
    seedBuildings([{ id: 1n, name: "Building A", siteId: 7n, rackCount: 3n }]);
    const onRemoveBuilding = vi.fn().mockResolvedValue(true);
    render(<ManageSiteModal {...baseProps} onRemoveBuilding={onRemoveBuilding} />);

    // Rack count renders as the row subtitle (not a trailing column).
    expect(screen.getByTestId("manage-site-modal-building-row-1")).toBeInTheDocument();
    expect(screen.getByText("3 racks")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("manage-site-modal-building-menu-1"));
    fireEvent.click(screen.getByTestId("manage-site-modal-remove-building-1"));

    await waitFor(() => expect(onRemoveBuilding).toHaveBeenCalledWith(1n, "Building A"));
    await waitFor(() => expect(screen.queryByTestId("manage-site-modal-building-row-1")).not.toBeInTheDocument());
    // Empty state takes over once the last building is removed.
    expect(screen.getByText("No buildings added to this site")).toBeInTheDocument();
  });

  it("keeps the row when the unassign fails", async () => {
    seedBuildings([{ id: 1n, name: "Building A", siteId: 7n, rackCount: 3n }]);
    const onRemoveBuilding = vi.fn().mockResolvedValue(false);
    render(<ManageSiteModal {...baseProps} onRemoveBuilding={onRemoveBuilding} />);

    fireEvent.click(screen.getByTestId("manage-site-modal-building-menu-1"));
    fireEvent.click(screen.getByTestId("manage-site-modal-remove-building-1"));

    await waitFor(() => expect(onRemoveBuilding).toHaveBeenCalled());
    expect(screen.getByTestId("manage-site-modal-building-row-1")).toBeInTheDocument();
  });
});
