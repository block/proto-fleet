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

describe("ManageBuildingsModal — create hand-offs", () => {
  beforeEach(() => {
    listAllBuildingsMock.mockReset();
    listSitesMock.mockReset();
  });

  it("shows each create button only when its launch handler is supplied", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    const { rerender } = render(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} />);

    await waitFor(() => expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeInTheDocument());
    expect(screen.queryByTestId("manage-buildings-modal-create-new")).not.toBeInTheDocument();
    expect(screen.queryByTestId("manage-buildings-modal-create-multiple")).not.toBeInTheDocument();

    // Separate props rather than one callback with a variant: a host without the
    // batch RPC wired can offer the first and not the second.
    rerender(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} onCreateNewLaunch={vi.fn()} />);
    expect(screen.getByTestId("manage-buildings-modal-create-new")).toHaveTextContent("Create building");
    expect(screen.queryByTestId("manage-buildings-modal-create-multiple")).not.toBeInTheDocument();

    rerender(
      <ManageBuildingsModal
        {...baseProps}
        onConfirm={vi.fn()}
        onCreateNewLaunch={vi.fn()}
        onCreateMultipleLaunch={vi.fn()}
      />,
    );
    expect(screen.getByTestId("manage-buildings-modal-create-multiple")).toHaveTextContent("Create multiple buildings");
  });

  it("hands off to the batch create flow without committing the selection", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    const onConfirm = vi.fn();
    const onCreateMultipleLaunch = vi.fn();
    render(
      <ManageBuildingsModal
        {...baseProps}
        onConfirm={onConfirm}
        onCreateNewLaunch={vi.fn()}
        onCreateMultipleLaunch={onCreateMultipleLaunch}
      />,
    );

    await waitFor(() => expect(screen.getByTestId("manage-buildings-modal-create-multiple")).toBeEnabled());
    fireEvent.click(screen.getAllByRole("checkbox")[0]);
    fireEvent.click(screen.getByTestId("manage-buildings-modal-create-multiple"));

    expect(onCreateMultipleLaunch).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
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

describe("ManageBuildingsModal — Save", () => {
  beforeEach(() => {
    listAllBuildingsMock.mockReset();
    listSitesMock.mockReset();
  });

  it("closes without an AssignBuildingsToSite no-op when the selection matches the site", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    const onConfirm = vi.fn();
    const onDismiss = vi.fn();
    render(<ManageBuildingsModal {...baseProps} onConfirm={onConfirm} onDismiss={onDismiss} />);

    const save = await screen.findByTestId("manage-buildings-modal-confirm");
    // Loaded with nothing checked and nothing seeded. Reviewing the list and
    // keeping it as-is is a legitimate outcome, so Save closes — but it must not
    // report a membership change it didn't make.
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getAllByRole("checkbox")[0]);
    fireEvent.click(save);
    expect(onConfirm).toHaveBeenCalledWith({
      added: [{ buildingId: 1n, label: "Building A" }],
      removed: [],
    });
  });

  it("keeps Save disabled while the building list is still loading", () => {
    // The one gate that stays: without the list there's no delta to compute, so
    // a click could only misread the selection.
    // No onSuccess → items stays undefined, so the delta can't be computed.
    listAllBuildingsMock.mockReturnValue(Promise.resolve(undefined));
    listSitesMock.mockReturnValue(Promise.resolve(undefined));
    render(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} />);

    expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeDisabled();
  });

  it("re-checking a seeded building is no change again", async () => {
    // Uncheck then re-check: the delta returns to empty, so Save is a no-op
    // close again rather than latching on "the operator touched something".
    seed([{ id: 1n, name: "Building A", siteId: 7n }]);
    const onConfirm = vi.fn();
    const onDismiss = vi.fn();
    render(
      <ManageBuildingsModal
        {...baseProps}
        initialSelectedBuildingIds={[1n]}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    const save = await screen.findByTestId("manage-buildings-modal-confirm");
    await waitFor(() => expect(save).toBeEnabled());

    const checkbox = screen.getAllByRole("checkbox")[0];
    fireEvent.click(checkbox);
    fireEvent.click(checkbox);
    fireEvent.click(save);

    expect(onConfirm).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
