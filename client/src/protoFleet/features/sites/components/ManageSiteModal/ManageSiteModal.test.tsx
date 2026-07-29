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
// here is the "New building" hand-off that reaches the inline create flow.
vi.mock("../ManageBuildingsModal", () => ({
  default: ({ onCreateNewLaunch }: { onCreateNewLaunch?: () => void }) => (
    <button type="button" data-testid="stub-picker-create-new" onClick={onCreateNewLaunch}>
      New building
    </button>
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
  onSave: () => Promise.resolve(null),
  onCreateBuilding: vi.fn().mockResolvedValue(null),
  onEditDetails: noop,
  onDeleteRequested: noop,
  onDismiss: noop,
};

describe("ManageSiteModal", () => {
  beforeEach(() => listBuildingsBySiteMock.mockReset());

  it("invokes onSave and closes when the save reports closeOnSuccess", async () => {
    seedBuildings([]);
    const onSave = vi.fn().mockResolvedValue({ closeOnSuccess: true });
    const onDismiss = vi.fn();

    render(<ManageSiteModal {...baseProps} onSave={onSave} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByTestId("manage-site-modal-save"));

    await waitFor(() => expect(onSave).toHaveBeenCalled());
    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
  });

  it("disables Save until the building list has loaded", () => {
    // No seed → listBuildingsBySite never calls onSuccess, so the working
    // set stays in the loading (undefined) state.
    render(<ManageSiteModal {...baseProps} onSave={vi.fn()} />);

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

  it("renders rack count as a subtitle and kebab-removes a building from the working set", () => {
    seedBuildings([{ id: 1n, name: "Building A", siteId: 7n, rackCount: 3n }]);
    render(<ManageSiteModal {...baseProps} />);

    // Rack count renders as the row subtitle (not a trailing column).
    expect(screen.getByTestId("manage-site-modal-building-row-1")).toBeInTheDocument();
    expect(screen.getByText("3 racks")).toBeInTheDocument();

    // Open the kebab and remove — the row drops from the list locally.
    fireEvent.click(screen.getByTestId("manage-site-modal-building-menu-1"));
    fireEvent.click(screen.getByTestId("manage-site-modal-remove-building-1"));
    expect(screen.queryByTestId("manage-site-modal-building-row-1")).not.toBeInTheDocument();
    // Empty state takes over once the last building is removed.
    expect(screen.getByText("No buildings added to this site")).toBeInTheDocument();
  });
});
