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

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listBuildingsBySite: listBuildingsBySiteMock,
    listAllBuildings: vi.fn(),
    getBuilding: vi.fn(),
  }),
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

const noop = () => undefined;

describe("ManageSiteModal", () => {
  beforeEach(() => listBuildingsBySiteMock.mockReset());

  it("invokes onSave and closes when the save reports closeOnSuccess", async () => {
    const onSave = vi.fn().mockResolvedValue({ closeOnSuccess: true });
    const onDismiss = vi.fn();

    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft()}
        onSave={onSave}
        onEditDetails={noop}
        onDeleteRequested={noop}
        onDismiss={onDismiss}
      />,
    );

    // FullScreenTwoPaneModal renders the button twice (laptop + mobile);
    // click the first instance.
    fireEvent.click(screen.getAllByTestId("manage-site-modal-save")[0]);

    await waitFor(() => expect(onSave).toHaveBeenCalled());
    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
  });

  it("disables Save in edit mode until the building list has loaded", () => {
    // No seed → listBuildingsBySite never calls onSuccess, so the working
    // set stays in the loading (undefined) state.
    const site = create(SiteSchema, { id: 7n, name: "East DC" });
    render(
      <ManageSiteModal
        open
        mode="edit"
        site={site}
        draft={draft({ name: "East DC" })}
        onSave={vi.fn()}
        onEditDetails={noop}
        onDeleteRequested={noop}
        onDismiss={noop}
      />,
    );

    expect(screen.getAllByTestId("manage-site-modal-save")[0]).toBeDisabled();
  });

  it("Site settings fires the parent callback", () => {
    const onEditDetails = vi.fn();
    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft()}
        onSave={() => Promise.resolve(null)}
        onEditDetails={onEditDetails}
        onDeleteRequested={noop}
        onDismiss={noop}
      />,
    );

    fireEvent.click(screen.getAllByTestId("manage-site-modal-edit-details")[0]);
    expect(onEditDetails).toHaveBeenCalled();
  });

  it("Delete site fires onDeleteRequested", () => {
    const onDeleteRequested = vi.fn();
    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft()}
        onSave={() => Promise.resolve(null)}
        onEditDetails={noop}
        onDeleteRequested={onDeleteRequested}
        onDismiss={noop}
      />,
    );

    fireEvent.click(screen.getAllByTestId("manage-site-modal-delete")[0]);
    expect(onDeleteRequested).toHaveBeenCalled();
  });

  it("create mode prompts to save the site before adding buildings", () => {
    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft()}
        onSave={() => Promise.resolve(null)}
        onEditDetails={noop}
        onDeleteRequested={noop}
        onDismiss={noop}
      />,
    );

    expect(screen.getByText("Save the site first to add buildings.")).toBeInTheDocument();
    // Manage buildings is disabled until the site exists.
    expect(screen.getAllByTestId("manage-site-modal-manage-buildings")[0]).toBeDisabled();
  });

  it("shows comma-separated meta on each corner of the preview", () => {
    const site = create(SiteSchema, {
      id: 7n,
      name: "East DC",
      locationCity: "Boston",
      locationState: "MA",
      powerCapacityMw: 5,
    });
    render(
      <ManageSiteModal
        open
        mode="edit"
        site={site}
        draft={draft({
          name: "East DC",
          locationCity: "Boston",
          locationState: "MA",
          powerCapacityMw: 5,
        })}
        onSave={() => Promise.resolve(null)}
        onEditDetails={noop}
        onDeleteRequested={noop}
        onDismiss={noop}
      />,
    );
    expect(screen.getByText("East DC, Boston, MA")).toBeInTheDocument();
    expect(screen.getByText("5 MW, 0 buildings")).toBeInTheDocument();
  });

  it("renders rack count as a subtitle and kebab-removes a building from the working set", () => {
    seedBuildings([{ id: 1n, name: "Building A", siteId: 7n, rackCount: 3n }]);
    const site = create(SiteSchema, { id: 7n, name: "East DC" });
    render(
      <ManageSiteModal
        open
        mode="edit"
        site={site}
        draft={draft({ name: "East DC" })}
        onSave={() => Promise.resolve(null)}
        onEditDetails={noop}
        onDeleteRequested={noop}
        onDismiss={noop}
      />,
    );

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
