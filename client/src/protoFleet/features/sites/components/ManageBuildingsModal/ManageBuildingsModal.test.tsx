import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import ManageBuildingsModal from "./ManageBuildingsModal";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
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

const containersMock = vi.hoisted(() => ({ list: vi.fn(), assign: vi.fn(), bump: vi.fn(), auth: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: { listContainers: containersMock.list },
  sitesClient: { assignContainersToSite: containersMock.assign },
}));
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors: containersMock.auth }) }));
vi.mock("@/protoFleet/store/useFleetStore", () => ({
  useFleetStore: (select: (state: unknown) => unknown) =>
    select({ ui: { sitesRevision: 0, bumpSitesRevision: containersMock.bump } }),
}));
beforeEach(() => {
  containersMock.list.mockReset().mockResolvedValue({ containers: [] });
  containersMock.assign.mockReset().mockResolvedValue({});
});

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

  it("recovers from a failed container load without closing or losing existing assignments", async () => {
    seed([{ id: 1n, name: "Building A", siteId: 7n }]);
    containersMock.list.mockRejectedValueOnce(new Error("Containers unavailable")).mockResolvedValueOnce({
      containers: [create(ContainerSchema, { deviceIdentifier: "ct1", name: "CT1", site: { id: 7n } })],
    });
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

    expect(await screen.findByRole("alert")).toHaveTextContent("Containers unavailable");
    expect(screen.queryByTestId("manage-buildings-modal-loading")).not.toBeInTheDocument();
    expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.getByTestId("manage-buildings-modal-confirm")).toBeEnabled());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByText("Building A")).toBeInTheDocument();
    expect(screen.getByText("CT1")).toBeInTheDocument();
    expect(containersMock.list).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByTestId("manage-buildings-modal-confirm"));
    await waitFor(() => expect(onDismiss).toHaveBeenCalledOnce());
    expect(onConfirm).not.toHaveBeenCalled();
    expect(containersMock.assign).not.toHaveBeenCalled();
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

describe("ManageBuildingsModal — containers in the shared list", () => {
  const seedMixed = () => {
    seed([{ id: 1n, name: "Building A", siteId: 0n }]);
    containersMock.list.mockResolvedValue({
      containers: [
        create(ContainerSchema, { id: 1n, deviceIdentifier: "loose", name: "CT1 loose" }),
        create(ContainerSchema, { deviceIdentifier: "member", name: "CT1 member", site: { id: 7n } }),
        create(ContainerSchema, { deviceIdentifier: "other", name: "CT1 other", site: { id: 8n, label: "West" } }),
      ],
    });
  };
  const checkbox = (name: string) => within(screen.getByText(name).closest("tr")!).getByRole("checkbox");

  it("saves a mixed selection through the respective APIs, with cross-site rows disabled", async () => {
    seedMixed();
    const onConfirm = vi.fn();
    render(<ManageBuildingsModal {...baseProps} onConfirm={onConfirm} />);
    await screen.findByText("CT1 loose");
    expect(checkbox("CT1 member")).toBeChecked();
    expect(checkbox("CT1 other")).toBeDisabled();
    fireEvent.click(checkbox("Building A"));
    fireEvent.click(checkbox("CT1 loose"));
    fireEvent.click(checkbox("CT1 member"));
    fireEvent.click(screen.getByTestId("manage-buildings-modal-confirm"));
    await waitFor(() =>
      expect(onConfirm).toHaveBeenCalledWith({ added: [{ buildingId: 1n, label: "Building A" }], removed: [] }),
    );
    expect(containersMock.assign.mock.calls).toEqual([
      [{ containerIdentifiers: ["loose"], targetSiteId: 7n }],
      [{ containerIdentifiers: ["member"], targetSiteId: undefined }],
    ]);
  });

  it("keeps failed assignments open and does not replay a successful batch on retry", async () => {
    seedMixed();
    containersMock.assign
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(
        new ConnectError('{"required":"site:manage","scope":{"site_id":7}}', Code.PermissionDenied),
      );
    const dismiss = vi.fn();
    render(<ManageBuildingsModal {...baseProps} onConfirm={vi.fn()} onDismiss={dismiss} />);
    await screen.findByText("CT1 loose");
    fireEvent.click(checkbox("CT1 loose"));
    fireEvent.click(checkbox("CT1 member"));
    fireEvent.click(screen.getByTestId("manage-buildings-modal-confirm"));
    expect(await screen.findByRole("alert")).toHaveTextContent("You don't have permission to change site assignments.");
    expect(dismiss).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("manage-buildings-modal-confirm"));
    await waitFor(() => expect(dismiss).toHaveBeenCalledOnce());
    expect(containersMock.assign.mock.calls).toEqual([
      [{ containerIdentifiers: ["loose"], targetSiteId: 7n }],
      [{ containerIdentifiers: ["member"], targetSiteId: undefined }],
      [{ containerIdentifiers: ["member"], targetSiteId: undefined }],
    ]);
  });
});
