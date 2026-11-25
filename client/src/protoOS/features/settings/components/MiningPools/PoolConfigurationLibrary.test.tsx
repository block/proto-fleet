import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import PoolConfigurationLibrary from "./PoolConfigurationLibrary";
import {
  type LocalPoolConfiguration,
  type PoolAssignmentService,
  PoolDeletionUnconfirmed,
} from "@/protoOS/api/adapters/poolAssignment";
import type { ContainerPoolConfiguration } from "@/protoOS/api/adapters/poolConfiguration";
import { readPoolAttempts, savePoolAttempt } from "@/protoOS/features/poolAssignment/attemptStorage";

const hosting = vi.hoisted(() => ({ poolAssignment: undefined as PoolAssignmentService | undefined }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: hosting, isFleetHosted: false }),
}));
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: (select: (state: unknown) => unknown) =>
    select({ systemInfo: { data: { identity: { deviceId: "ct1" } } }, connectionGeneration: 1 }),
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => ({ isPhone: false }) }));
vi.mock("@/protoOS/features/poolAssignment/poolWriteLock", () => ({
  withPoolWriteLock: (_controller: string, signal: AbortSignal, write: (signal: AbortSignal) => Promise<void>) =>
    write(signal),
}));
const configuration: ContainerPoolConfiguration = {
  mode: "groups",
  suspended: false,
  groups: [],
  unassignedBoards: [],
  pendingOperations: [],
  inventoryIssues: [],
  locationsAvailable: true,
};
const local: LocalPoolConfiguration = {
  id: "local",
  name: "Production",
  revision: "7",
  assignedCount: 3,
  assignedMacs: ["aa:bb:cc:00:00:01", "aa:bb:cc:00:00:02", "aa:bb:cc:00:00:03"],
  deletable: true,
  isDefault: false,
  mining: true,
  pools: [
    {
      id: "1",
      name: "Primary",
      url: "stratum+tcp://pool:3333",
      username: "full.account.worker",
      enabled: true,
      priority: 0,
    },
  ],
};
const capacity = { groupsUsed: 1, maxGroups: 10, maxBoards: 4, maxPools: 3 };
const fixture = (configurations = [local]) =>
  ({
    listConfigurations: vi.fn().mockResolvedValue(configurations),
    listConfigurationUpdates: vi.fn().mockResolvedValue({ updates: [], limitReached: false }),
    listConfigurationPage: vi.fn().mockImplementation(async (offset, limit) => ({
      configurations: configurations.slice(offset, offset + limit),
      total: configurations.length,
      capacity,
    })),
    deleteConfiguration: vi.fn().mockResolvedValue(undefined),
    inspect: vi.fn().mockResolvedValue({
      savedPools: [],
      mixedConfigurations: false,
      capacity,
    }),
    review: vi.fn(),
    apply: vi.fn(),
    inspectWorkerNames: vi.fn(),
    reviewWorkerNames: vi.fn(),
    applyWorkerNames: vi.fn(),
    status: vi.fn(),
    checkMiningRecovery: vi.fn(),
  }) satisfies PoolAssignmentService;
beforeEach(() => {
  hosting.poolAssignment = undefined;
  localStorage.clear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});

test("reloads assignment counts and membership when an operator clears a completed recovery record", async () => {
  const service = fixture([{ ...local, assignedCount: 4 }]);
  service.listConfigurationPage.mockResolvedValueOnce({ configurations: [local], total: 1, capacity });
  const onReload = vi.fn();
  service.status.mockResolvedValue({
    state: "saved",
    message: "Pool assignment saved.",
    confirmed: 1,
    total: 1,
    blockers: [],
  });
  hosting.poolAssignment = service;
  savePoolAttempt("ct1", {
    id: "saved",
    macs: ["aa:bb:cc:00:00:01"],
    destinationId: local.id,
    destinationRef: "proto-os:pools:local",
    pools: local.pools,
    configurationConfirmed: true,
    operationIds: [],
    phase: "Checking assignments",
  });

  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={onReload} />);
  expect(await screen.findByRole("cell", { name: "3" })).toBeVisible();
  fireEvent.click(await screen.findByRole("button", { name: "Pool assignment status" }));
  fireEvent.click(await screen.findByRole("button", { name: "Done" }));
  expect(onReload).toHaveBeenCalledOnce();
  expect(await screen.findByRole("cell", { name: "4" })).toBeVisible();
});

test.each([
  { selection: "the same board", selectedMac: local.assignedMacs[0], unassignedOnly: true },
  { selection: "another board", selectedMac: "aa:bb:cc:00:00:09", unassignedOnly: true },
  { selection: "another board after a move started in Modules", selectedMac: "aa:bb:cc:00:00:09" },
])("shows saved recovery when opening Edit pool for $selection", async ({ selectedMac, unassignedOnly }) => {
  const service = fixture();
  const onReload = vi.fn();
  const mac = local.assignedMacs[0];
  hosting.poolAssignment = service;
  savePoolAttempt("ct1", {
    id: "interrupted",
    macs: [mac],
    unassignedOnly,
    destinationId: local.id,
    destinationRef: "proto-os:pools:local",
    pools: local.pools,
    configurationConfirmed: true,
    operationIds: ["transfer"],
    phase: "Moving selected hashboards",
  });
  service.status.mockResolvedValue({
    state: "saved",
    message: "Pool assignment saved.",
    confirmed: 1,
    total: 1,
    blockers: [],
  });
  render(
    <PoolConfigurationLibrary
      service={service}
      configuration={{ ...configuration, unassignedBoards: [{ mac }, { mac: "aa:bb:cc:00:00:09" }] }}
      onReload={onReload}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "View Hashboards with no reported group assignment" }));
  const boards = within(screen.getByRole("dialog", { name: "Hashboards with no reported group assignment" }));
  fireEvent.click(boards.getByRole("checkbox", { name: `Select ${selectedMac}` }));
  fireEvent.click(boards.getByRole("button", { name: "Edit pool" }));

  const recovery = within(await screen.findByRole("dialog", { name: "Pool assignment status" }));
  await waitFor(() => expect(recovery.getByText("Pool assignment saved.")).toBeVisible());
  expect(readPoolAttempts("ct1")).toHaveLength(1);
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(recovery.getByRole("button", { name: "Done" }));
  expect(onReload).toHaveBeenCalledOnce();
  expect(readPoolAttempts("ct1")).toHaveLength(0);
});

test("opens pool details, recent updates and read-only equipment from the primary pool cell", async () => {
  const service = fixture();
  service.listConfigurationUpdates.mockResolvedValue({
    updates: [
      {
        id: "3",
        action: "Transfer into this group",
        result: "ok",
        macs: local.assignedMacs.slice(0, 2),
        time: "2026-11-19T21:50:00Z",
      },
    ],
    limitReached: false,
  });
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("cell", { name: local.pools[0].url }));
  const details = await screen.findByRole("dialog", { name: "Pool configuration" });
  expect(within(details).getByText("Username: full.account.worker")).toBeInTheDocument();
  expect(await within(details).findByText("Transfer into this group · 2 equipment")).toBeInTheDocument();
  expect(service.listConfigurationUpdates).toHaveBeenCalledWith(local.id, expect.anything());
  fireEvent.click(within(details).getByRole("button", { name: "View Production hashboards" }));
  const equipment = screen.getByRole("dialog", { name: "Production hashboards" });
  expect(within(equipment).getByText(local.assignedMacs[0])).toBeInTheDocument();
  expect(within(equipment).queryByRole("checkbox")).not.toBeInTheDocument();
  expect(within(equipment).queryByRole("button", { name: "Edit pool" })).not.toBeInTheDocument();
  fireEvent.click(within(equipment).getByRole("button", { name: "Close dialog" }));

  fireEvent.click(within(details).getByRole("button", { name: "View results" }));
  const results = screen.getByRole("dialog", { name: "Pool configuration" });
  expect(within(results).queryByRole("region", { name: "Update history" })).not.toBeInTheDocument();
  expect(within(results).getByText("Transfer into this group")).toBeInTheDocument();
  for (const mac of local.assignedMacs.slice(0, 2)) {
    const board = within(results).getByRole("region", { name: `Hashboard ${mac}` });
    expect(within(board).getByRole("heading", { name: "Request accepted" })).toBeInTheDocument();
    expect(within(board).getByText("Update recorded")).toBeInTheDocument();
  }
  expect(within(results).queryByRole("button", { name: /retry|reapply|check configuration/i })).not.toBeInTheDocument();
  fireEvent.click(within(results).getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog", { name: "Pool configuration" })).not.toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Primary pool" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "No reported group assignment" })).not.toBeInTheDocument();
  expect(screen.queryByText(/mining groups/i)).not.toBeInTheDocument();
});

test("uses the history fallback for a malformed timestamp", async () => {
  const service = fixture();
  service.listConfigurationUpdates.mockResolvedValue({
    updates: [{ id: "4", action: "Pool settings update", result: "ok", macs: [], time: "malformed" }],
    limitReached: false,
  });
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("row", { name: /Production/ }));
  const history = within(await screen.findByRole("region", { name: "Update history" }));
  expect(await history.findByText("Pool update")).toBeInTheDocument();
  fireEvent.click(history.getByRole("button", { name: "View results" }));
  const results = within(screen.getByRole("dialog", { name: "Pool configuration" }));
  expect(results.queryByText("Invalid Date")).not.toBeInTheDocument();
  expect(results.queryByText("Update recorded")).not.toBeInTheDocument();
});

test("lists hashboards with no reported group assignment in the library", async () => {
  const service = fixture();
  const unassignedBoards = [
    { mac: "aa:bb:cc:00:00:01", location: "Tank 1 · Module 1 · Board 1" },
    { mac: "aa:bb:cc:00:00:02", location: "Tank 1 · Module 1 · Board 2" },
    { mac: "aa:bb:cc:00:00:03" },
  ];
  render(
    <PoolConfigurationLibrary
      service={service}
      configuration={{ ...configuration, unassignedBoards }}
      onReload={vi.fn()}
    />,
  );
  expect(await screen.findByRole("row", { name: /Production/ })).toBeInTheDocument();
  const section = screen.getByRole("region", { name: "No reported group assignment" });
  expect(within(section).getByText("Hashboards: 3")).toBeInTheDocument();
  fireEvent.click(within(section).getByRole("button", { name: "View Hashboards with no reported group assignment" }));
  const dialog = screen.getByRole("dialog", { name: "Hashboards with no reported group assignment" });
  expect(within(dialog).getAllByRole("row")).toHaveLength(4);
  for (const board of unassignedBoards) {
    expect(within(dialog).getByRole("row", { name: new RegExp(board.mac) })).toHaveTextContent(
      board.location ?? "Location unavailable",
    );
  }
});

test("shows incomplete history when the controller reached its record limit", async () => {
  const service = fixture();
  service.listConfigurationUpdates.mockResolvedValue({ updates: [], limitReached: true });
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("cell", { name: local.pools[0].url }));
  const history = within(screen.getByRole("region", { name: "Update history" }));
  expect(
    await history.findByText("Some updates may be missing. The controller reached its record limit."),
  ).toBeVisible();
  expect(history.queryByText("No updates reported in the last 7 days.")).not.toBeInTheDocument();
});

test("assigns only eligible matches through the pool picker and reloads membership", async () => {
  const service = fixture();
  const onReload = vi.fn();
  hosting.poolAssignment = service;
  const unassignedBoards = [
    { mac: "aa:bb:cc:00:00:01", location: "Tank 1 · Module 1 · Board 1" },
    { mac: "aa:bb:cc:00:00:02", location: "Tank 1 · Module 1 · Board 2" },
    { mac: "aa:bb:cc:00:00:03" },
    { mac: "aa:bb:cc:00:00:04", location: "Tank 1 · Module 1 · Board 3" },
    { mac: "aa:bb:cc:00:00:05", location: "Tank 1 · Module 1 · Board 4", isolated: true },
  ];
  const blockedMac = unassignedBoards[3].mac;
  const target = { macs: unassignedBoards.slice(0, 2).map(({ mac }) => mac), unassignedOnly: true };
  service.inspect.mockResolvedValue({
    savedPools: [{ ...local, projectedBoards: 4 }],
    mixedConfigurations: false,
    capacity,
  });
  service.review.mockResolvedValue({
    id: "new-assignment",
    name: local.name,
    target,
    boards: unassignedBoards.slice(0, 2),
    stamp: "revision",
    pools: [],
    savedPoolSet: local,
    capacity: { ...capacity, destinationBoards: 4 },
    mining: false,
    requiresActivation: false,
    destinationId: local.id,
    destinationRef: "proto-os:pools:local",
    transfers: [{ source: "", macs: target.macs }],
  });
  service.apply.mockResolvedValue({
    state: "saved",
    message: "Pool assignment saved.",
    confirmed: 2,
    total: 2,
    blockers: [],
  });
  render(
    <PoolConfigurationLibrary
      service={service}
      configuration={{
        ...configuration,
        unassignedBoards,
        inventoryIssues: [{ mac: blockedMac.toUpperCase(), reason: "missing_verified_feed" }],
      }}
      onReload={onReload}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "View Hashboards with no reported group assignment" }));
  const boardsDialog = within(screen.getByRole("dialog", { name: "Hashboards with no reported group assignment" }));
  expect(boardsDialog.getByRole("button", { name: "Edit pool" })).toBeDisabled();
  expect(boardsDialog.getByRole("checkbox", { name: `Select ${blockedMac}` })).toBeDisabled();
  expect(boardsDialog.getByText("Verify this hashboard's power feed before assigning it.")).toBeInTheDocument();
  expect(boardsDialog.getByRole("checkbox", { name: `Select ${unassignedBoards[4].mac}` })).toBeDisabled();
  expect(boardsDialog.getByText("Remove isolation before assigning this hashboard.")).toBeInTheDocument();
  fireEvent.change(boardsDialog.getByLabelText("Search MAC or location"), { target: { value: "Tank 1" } });
  fireEvent.click(boardsDialog.getByRole("button", { name: "Select all" }));
  fireEvent.click(boardsDialog.getByRole("button", { name: "Edit pool" }));

  const picker = within(await screen.findByRole("dialog", { name: "Edit pool" }));
  fireEvent.click(await picker.findByText(local.name));
  expect(service.inspect).toHaveBeenCalledWith(target, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  fireEvent.click(picker.getByRole("button", { name: "Assign" }));
  const review = within(await screen.findByRole("dialog", { name: "Assign pool configuration to 2 hashboards?" }));
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(review.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(onReload).toHaveBeenCalledOnce());
  expect(service.apply).toHaveBeenCalledWith(
    expect.objectContaining({ target }),
    expect.any(Function),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

test("shows when unassigned inventory is unavailable and leaves pools accessible", async () => {
  const service = fixture();
  const onReload = vi.fn();
  render(
    <PoolConfigurationLibrary
      service={service}
      configuration={{ ...configuration, locationsAvailable: false }}
      onReload={onReload}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("Could not check for unassigned hashboards.");
  expect(await screen.findByRole("row", { name: /Production/ })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Reload pools" }));
  expect(onReload).toHaveBeenCalledOnce();
});

test("shows incomplete inventory and keeps the unassigned list readable without assignment controls", async () => {
  const service = fixture();
  const onReload = vi.fn();
  const mac = "aa:bb:cc:00:00:09";
  const assignmentInventoryError = "Board identity is incomplete or duplicated. Refresh the board inventory.";
  hosting.poolAssignment = service;
  render(
    <PoolConfigurationLibrary
      service={service}
      configuration={{ ...configuration, assignmentInventoryError, unassignedBoards: [{ mac }] }}
      onReload={onReload}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent(assignmentInventoryError);
  expect(await screen.findByRole("row", { name: /Production/ })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "View Hashboards with no reported group assignment" }));
  const boards = within(screen.getByRole("dialog", { name: "Hashboards with no reported group assignment" }));
  expect(boards.getByText(mac)).toBeInTheDocument();
  expect(boards.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(boards.queryByRole("button", { name: "Edit pool" })).not.toBeInTheDocument();
  fireEvent.click(boards.getByRole("button", { name: "Close dialog" }));
  fireEvent.click(screen.getByRole("button", { name: "Reload pools" }));
  expect(onReload).toHaveBeenCalledOnce();
  expect(service.inspect).not.toHaveBeenCalled();
});

test("blocks deleting an assigned configuration and deletes an unused configuration by revision", async () => {
  const service = fixture([{ ...local, assignedCount: 0, assignedMacs: [] }]);
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Actions for Production" }));
  fireEvent.click(screen.getByText("Delete configuration"));
  const dialog = await screen.findByRole("dialog", { name: "Delete configuration" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete configuration" }));
  await waitFor(() =>
    expect(service.deleteConfiguration).toHaveBeenCalledWith(
      "local",
      "7",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    ),
  );
});
test("assigned configurations require reassignment before deletion", async () => {
  const service = fixture();
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Actions for Production" }));
  fireEvent.click(screen.getByText("Delete configuration"));
  const dialog = await screen.findByRole("dialog", { name: "Delete configuration" });
  expect(within(dialog).getByRole("button", { name: "Delete configuration" })).toBeDisabled();
  expect(service.deleteConfiguration).not.toHaveBeenCalled();
});
test("creates a configuration without requiring board selection or an account password", async () => {
  const service = fixture([]);
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Create configuration" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Create configuration" }));
  const dialog = await screen.findByRole("dialog", { name: "Create pool configuration" });
  expect(within(dialog).getByRole("group", { name: "Primary pool" })).toBeInTheDocument();
  expect(screen.queryByLabelText("Your password")).not.toBeInTheDocument();
  expect(screen.queryByText(/hashboards selected/)).not.toBeInTheDocument();
});
test("keeps the editor open without inspecting unrelated configurations on rerender", async () => {
  const service = fixture();
  const view = render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "Create configuration" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Create configuration" }));
  view.rerender(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  expect(await screen.findByRole("dialog", { name: "Create pool configuration" })).toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
});
test("Default allows editing, keeps deletion disabled, and has no Set as default action", async () => {
  const service = fixture([{ ...local, id: "default", name: "Default pools", isDefault: true, deletable: false }]);
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Actions for Default pools" }));
  expect(screen.queryByText("Set as default", { exact: true })).not.toBeInTheDocument();
  expect(screen.getByText("This is the controller's fixed default configuration.")).toBeInTheDocument();
  const edit = screen.getByText("Edit configuration", { exact: true }).closest("button")!;
  expect(edit).toBeEnabled();
  expect(screen.getByText("Delete configuration", { exact: true }).closest("button")).toBeDisabled();
  expect(service.deleteConfiguration).not.toHaveBeenCalled();
  fireEvent.click(edit);
  const editor = await screen.findByRole("dialog", { name: "Edit pool configuration" });
  expect(within(editor).getByLabelText("Name")).toHaveAttribute("readonly");
  fireEvent.click(within(editor).getByRole("checkbox", { name: "Keep saved password" }));
  expect(within(editor).getByLabelText("Pool password")).toBeInTheDocument();
  expect(within(editor).getByRole("button", { name: "Review changes" })).toBeDisabled();
});

test("non-default configurations allow editing and deletion", async () => {
  const service = fixture();
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Actions for Production" }));
  const edit = screen.getByText("Edit configuration", { exact: true }).closest("button")!;
  expect(edit).toBeEnabled();
  expect(screen.getByText("Delete configuration", { exact: true }).closest("button")).toBeEnabled();
  fireEvent.click(edit);
  expect(await screen.findByRole("dialog", { name: "Edit pool configuration" })).toBeInTheDocument();
});

test("a Fleet-owned configuration allows editing but protects deletion", async () => {
  const service = fixture([{ ...local, deletable: false }]);
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Actions for Production" }));
  expect(screen.getByText("Edit configuration", { exact: true }).closest("button")).toBeEnabled();
  expect(screen.getByText("Delete configuration", { exact: true }).closest("button")).toBeDisabled();
  expect(screen.getByText("Delete this configuration in Proto Fleet.")).toBeInTheDocument();
});

test.each([new Error("Deletion refused"), new PoolDeletionUnconfirmed()])(
  "keeps the delete error in the dialog and refreshes only when the outcome is uncertain",
  async (failure) => {
    const service = fixture([{ ...local, assignedCount: 0, assignedMacs: [] }]);
    service.deleteConfiguration.mockRejectedValueOnce(failure);
    render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Actions for Production" }));
    fireEvent.click(screen.getByText("Delete configuration"));
    const dialog = await screen.findByRole("dialog", { name: "Delete configuration" });
    const confirm = within(dialog).getByRole("button", { name: "Delete configuration" });
    fireEvent.click(confirm);
    expect(await within(dialog).findByText(failure.message)).toBeInTheDocument();
    const uncertain = failure instanceof PoolDeletionUnconfirmed;
    if (uncertain) expect(confirm).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: uncertain ? "Close" : "Cancel" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Create configuration" })).toBeEnabled());
    expect(service.listConfigurationPage).toHaveBeenCalledTimes(uncertain ? 2 : 1);
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  },
);

test("requests only the visible configuration page", async () => {
  const configurations = Array.from({ length: 26 }, (_, index) => ({
    ...local,
    id: `local-${index}`,
    name: `Production ${index}`,
  }));
  const service = fixture(configurations);
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  expect(await screen.findByRole("row", { name: /Production 0\b/ })).toBeInTheDocument();
  expect(service.listConfigurationPage).toHaveBeenCalledWith(
    0,
    25,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  expect(await screen.findByRole("row", { name: /Production 25\b/ })).toBeInTheDocument();
  expect(service.listConfigurationPage).toHaveBeenLastCalledWith(
    25,
    25,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});
