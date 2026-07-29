import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ManageBuildingsModal from "./ManageBuildingsModal";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const { listAllBuildingsMock, listSitesMock } = vi.hoisted(() => ({
  listAllBuildingsMock: vi.fn(),
  listSitesMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listAllBuildings: listAllBuildingsMock }),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({ listSites: listSitesMock }),
}));

// The picker self-fetches the org-wide building list plus the site catalog for
// its Site column; both mocks resolve synchronously through onSuccess.
const seed = (rows: { id: bigint; name: string; siteId: bigint }[]) => {
  listAllBuildingsMock.mockImplementation((args?: { onSuccess?: (rows: unknown[]) => void }) => {
    args?.onSuccess?.(
      rows.map((r) =>
        create(BuildingWithCountsSchema, {
          building: create(BuildingSchema, { id: r.id, name: r.name, siteId: r.siteId }),
          rackCount: 0n,
        }),
      ),
    );
    return Promise.resolve(undefined);
  });
  listSitesMock.mockImplementation((args?: { onSuccess?: (rows: unknown[]) => void }) => {
    args?.onSuccess?.([create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "East DC" }) })]);
    return Promise.resolve(undefined);
  });
};

const baseProps = {
  open: true as const,
  siteId: 7n,
  initialSelectedBuildingIds: [] as bigint[],
  onDismiss: () => undefined,
};

describe("ManageBuildingsModal — New building hand-off", () => {
  beforeEach(() => {
    listAllBuildingsMock.mockReset();
    listSitesMock.mockReset();
  });

  it("omits the New building button when no launch handler is supplied", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    render(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} />);

    await waitFor(() => expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeInTheDocument());
    expect(screen.queryByTestId("manage-buildings-modal-create-new")).not.toBeInTheDocument();
  });

  it("abandons the pending selection when launching create — Save is what commits", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    const onConfirm = vi.fn();
    const onCreateNewLaunch = vi.fn();
    render(<ManageBuildingsModal {...baseProps} onConfirm={onConfirm} onCreateNewLaunch={onCreateNewLaunch} />);

    await waitFor(() => expect(screen.getByTestId("manage-buildings-modal-create-new")).toBeEnabled());

    // Check the unassigned building, then hand off to the create flow without
    // pressing Save.
    fireEvent.click(screen.getAllByRole("checkbox")[0]);
    fireEvent.click(screen.getByTestId("manage-buildings-modal-create-new"));

    // Nothing is written — leaving without Save means the selection never
    // landed, which is what makes the abandon safe to reason about.
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCreateNewLaunch).toHaveBeenCalled();
  });
});

describe("ManageBuildingsModal — Save gate", () => {
  beforeEach(() => {
    listAllBuildingsMock.mockReset();
    listSitesMock.mockReset();
  });

  it("disables Save until the selection differs from the site's current membership", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    const onConfirm = vi.fn();
    render(<ManageBuildingsModal {...baseProps} onConfirm={onConfirm} />);

    const save = await screen.findByTestId("manage-buildings-modal-confirm");
    // Loaded with nothing checked and nothing seeded — no membership change to
    // write, so Save must not fire an AssignBuildingsToSite no-op.
    await waitFor(() => expect(save).toBeDisabled());

    fireEvent.click(screen.getAllByRole("checkbox")[0]);
    await waitFor(() => expect(save).toBeEnabled());

    fireEvent.click(save);
    expect(onConfirm).toHaveBeenCalledWith({
      added: [{ buildingId: 1n, label: "Building A" }],
      removed: [],
    });
  });

  it("keeps Save disabled while the building list is still loading", () => {
    // No onSuccess → items stays undefined, so the delta can't be computed.
    listAllBuildingsMock.mockReturnValue(Promise.resolve(undefined));
    listSitesMock.mockReturnValue(Promise.resolve(undefined));
    render(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} />);

    expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeDisabled();
  });

  it("re-checking a seeded building leaves Save disabled", async () => {
    // Uncheck then re-check: the delta returns to empty, so the gate closes
    // again rather than latching open on "the operator touched something".
    seed([{ id: 1n, name: "Building A", siteId: 7n }]);
    render(<ManageBuildingsModal {...baseProps} initialSelectedBuildingIds={[1n]} onConfirm={vi.fn()} />);

    const save = await screen.findByTestId("manage-buildings-modal-confirm");
    await waitFor(() => expect(save).toBeDisabled());

    const checkbox = screen.getAllByRole("checkbox")[0];
    fireEvent.click(checkbox);
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(checkbox);
    await waitFor(() => expect(save).toBeDisabled());
  });
});
