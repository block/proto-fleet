import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import PoolConfigurationLibrary from "./PoolConfigurationLibrary";
import {
  type LocalPoolConfiguration,
  type PoolAssignmentService,
  PoolDeletionUnconfirmed,
} from "@/protoOS/api/adapters/poolAssignment";
import type { ContainerPoolConfiguration } from "@/protoOS/api/adapters/poolConfiguration";
import { savePoolAttempt } from "@/protoOS/features/poolAssignment/attemptStorage";

const hosting = vi.hoisted(() => ({ poolAssignment: undefined as PoolAssignmentService | undefined }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: hosting, isFleetHosted: false }),
}));
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: (select: (state: unknown) => unknown) =>
    select({ systemInfo: { data: { identity: { deviceId: "ct1" } } }, connectionGeneration: 1 }),
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => ({ isPhone: false }) }));
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

test("reloads assignment counts when an operator clears a completed recovery record", async () => {
  const service = fixture([{ ...local, assignedCount: 4 }]);
  service.listConfigurationPage.mockResolvedValueOnce({ configurations: [local], total: 1, capacity });
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

  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  expect(await screen.findByRole("cell", { name: "3" })).toBeVisible();
  fireEvent.click(await screen.findByRole("button", { name: "Pool assignment status" }));
  fireEvent.click(await screen.findByRole("button", { name: "Done" }));
  expect(await screen.findByRole("cell", { name: "4" })).toBeVisible();
});

test("uses Fleet's library and pool details without hashboard or group controls", async () => {
  const service = fixture();
  render(<PoolConfigurationLibrary service={service} configuration={configuration} onReload={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Production" }));
  const details = await screen.findByRole("dialog", { name: "Pool configuration" });
  expect(within(details).getByText("Username: full.account.worker")).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Primary pool" })).toBeInTheDocument();
  expect(screen.queryByText("View hashboards")).not.toBeInTheDocument();
  expect(screen.queryByText(/mining groups/i)).not.toBeInTheDocument();
});
test("blocks deleting an assigned configuration and deletes an unused configuration by revision", async () => {
  const service = fixture([{ ...local, assignedCount: 0 }]);
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
    const service = fixture([{ ...local, assignedCount: 0 }]);
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
  expect(await screen.findByRole("button", { name: "Production 0" })).toBeInTheDocument();
  expect(service.listConfigurationPage).toHaveBeenCalledWith(
    0,
    25,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  expect(await screen.findByRole("button", { name: "Production 25" })).toBeInTheDocument();
  expect(service.listConfigurationPage).toHaveBeenLastCalledWith(
    25,
    25,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});
