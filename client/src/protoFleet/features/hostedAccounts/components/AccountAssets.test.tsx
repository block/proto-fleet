import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountAssets from "./AccountAssets";
import { SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  AccountAssetSchema,
  AccountSchema,
  AssetAssignmentIntent,
  ListAccountAssetsResponseSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

beforeEach(() => {
  // Popovers require a visible trigger; JSDOM does not calculate layout.
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

const accountId = "11111111-1111-4111-8111-111111111111";
const childId = "22222222-2222-4222-8222-222222222222";
const miner = create(AccountAssetSchema, {
  kind: MiningAssetKind.MINER,
  id: 1n,
  revision: 7n,
  name: "Miner One",
  available: true,
  ownerAccountId: accountId,
  ownerAccountName: "Customer",
  state: "hashing",
  hashrateHs: 200e12,
  model: "S21",
  powerW: 6000,
  temperatureC: 55.5,
  efficiencyJTh: 30,
});
const module = create(AccountAssetSchema, {
  kind: MiningAssetKind.MODULE,
  id: 1n,
  revision: 9n,
  name: "Module One",
  available: true,
  ownerAccountId: accountId,
  ownerAccountName: "Customer",
  containerId: 10n,
  containerName: "Container A",
  state: "hashing",
  hashrateHs: 100e12,
  powerW: 2000,
  temperatureC: 42.5,
  efficiencyJTh: 20,
  coverage: {
    hashrate: { reporting: 1, expected: 2, topologyComplete: true },
    power: { reporting: 1, expected: 2, topologyComplete: true },
  },
  partialCoverage: true,
});
function mount(
  mask = 31n,
  assets = [miner, module],
  assignment = vi.fn().mockResolvedValue({}),
  nextPageToken = "",
  mode: "inventory" | "assign" = "inventory",
  child?: { accountId: string; name: string; suspended: boolean },
) {
  const client = {
    listAccountAssets: vi.fn().mockResolvedValue(
      create(ListAccountAssetsResponseSchema, {
        assets,
        nextPageToken,
        canManageAssignments: (mask & 16n) !== 0n,
        canViewAssignments: (mask & 24n) !== 0n,
        children: (mask & 24n) !== 0n ? [{ accountId: childId, name: "East" }] : [],
      }),
    ),
    assignAccountAssets: assignment,
  };
  const scope: AccountScope = {
    account: create(AccountSchema, { accountId, name: "Customer", depth: 1, effectiveCapabilities: mask }),
    client: client as unknown as AccountScope["client"],
    signal: new AbortController().signal,
    refreshAccount: vi.fn(),
    invalidateAccount: vi.fn(),
  };
  const page = (currentScope: AccountScope) => (
    <MemoryRouter>
      <AccountScopeContext.Provider value={currentScope}>
        <AccountAssets mode={mode} child={child} />
      </AccountScopeContext.Provider>
    </MemoryRouter>
  );
  const view = render(page(scope));
  return {
    client,
    scope,
    unmount: view.unmount,
    rerenderAccount: (account: AccountScope["account"]) => view.rerender(page({ ...scope, account })),
  };
}
const selectTarget = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole("button", { name: /Assign to subclient/ }));
  await user.click(await screen.findByRole("option", { name: "East" }));
};

describe("account mining asset allocation", () => {
  it("uses Fleet's metric units and module cells with the common columns", async () => {
    mount();
    const minerRow = (await screen.findByText("Miner One")).closest("tr")!;
    for (const label of ["Name", "Model", "Status", "Hashrate", "Efficiency", "Power", "Temp"])
      expect(screen.getByRole("columnheader", { name: label })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Equipment power" })).not.toBeInTheDocument();
    for (const text of ["S21", "200.0 TH/s", "30.0 J/TH", "6.0 kW", "55.5 °C"])
      expect(within(minerRow).getByText(text)).toBeInTheDocument();
    const moduleRow = screen.getByText("Module One").closest("tr")!;
    expect(within(moduleRow).getByText("42.5 °C")).toBeInTheDocument();
    expect(within(moduleRow).getAllByText("1/2 boards reporting")).toHaveLength(2);
  });
  it("retains unknown coverage hints for zero module readings", async () => {
    mount(31n, [
      create(AccountAssetSchema, {
        ...module,
        hashrateHs: 0,
        powerW: 0,
        efficiencyJTh: 0,
        temperatureC: 0,
        coverage: undefined,
        unknownCoverage: true,
      }),
    ]);
    const row = (await screen.findByText("Module One")).closest("tr")!;
    expect(within(row).getAllByText("Module coverage unknown")).toHaveLength(4);
  });
  it("sorts through the account API, clears selected rows and persists column choices", async () => {
    const { client, unmount } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    expect(screen.getByText("1 mining asset selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Power" }));
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ pageToken: "", sort: { field: SortField.POWER, direction: SortDirection.DESC } }),
        expect.anything(),
      ),
    );
    expect(screen.getByRole("columnheader", { name: "Power" })).toHaveAttribute("aria-sort", "descending");
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    await user.click(screen.getByRole("checkbox", { name: "Toggle Power column" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: { field: SortField.NAME, direction: SortDirection.ASC } }),
        expect.anything(),
      ),
    );
    expect(screen.queryByRole("columnheader", { name: "Power" })).not.toBeInTheDocument();
    unmount();
    mount();
    await screen.findByText("Miner One");
    expect(screen.queryByRole("columnheader", { name: "Power" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    await user.click(screen.getByRole("button", { name: "Reset to defaults" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("columnheader", { name: "Power" })).toBeInTheDocument();
  });

  it("keeps the account’s own inventory read-only, including its subclient allocations", async () => {
    const { client } = mount(31n, [miner, create(AccountAssetSchema, { ...module, ownerAccountId: childId })]);
    await screen.findByText("Miner One");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Assignment" }));
    await user.click(await screen.findByRole("option", { name: "East" }));
    expect(screen.getByRole("link", { name: "Manage subclient assets" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/children?assets=${childId}`,
    );
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });
  it("scopes a child inventory and its recall to the parent authority", async () => {
    const { client } = mount(
      31n,
      [create(AccountAssetSchema, { ...miner, ownerAccountId: childId })],
      undefined,
      "",
      "inventory",
      { accountId: childId, name: "East", suspended: false },
    );
    const user = userEvent.setup();
    await screen.findByRole("checkbox", { name: "Select Miner One" });
    expect(screen.queryByRole("button", { name: "Refresh" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Assets allocated to/)).not.toBeInTheDocument();
    expect(screen.queryByText("Select allocated assets to recall.")).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Select Miner One" }));
    expect(client.listAccountAssets).toHaveBeenLastCalledWith(
      expect.objectContaining({ accountId, ownerAccountId: childId }),
      expect.anything(),
    );
    expect(screen.queryByRole("button", { name: "Assignment" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Recall 1 asset" }));
    expect(client.assignAccountAssets).toHaveBeenCalledWith(
      expect.objectContaining({ accountId, childAccountId: childId, intent: AssetAssignmentIntent.RECALL }),
    );
  });
  it("keeps the subclient assignment picker limited to directly held assets and its chosen destination", async () => {
    const { client } = mount(31n, [miner], undefined, "", "assign", {
      accountId: childId,
      name: "East",
      suspended: false,
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    expect(client.listAccountAssets).toHaveBeenLastCalledWith(
      expect.objectContaining({ accountId, ownerAccountId: accountId }),
      expect.anything(),
    );
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    expect(screen.queryByRole("button", { name: "Assign to subclient" })).not.toBeInTheDocument();
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 1 asset" }));
    expect(client.assignAccountAssets).toHaveBeenCalledWith(
      expect.objectContaining({ accountId, childAccountId: childId, intent: AssetAssignmentIntent.ASSIGN }),
    );
  });
  it.each([false, true])("explains when no active destination exists (suspended: %s)", async (suspended) => {
    const { client } = mount(31n, undefined, undefined, "", "assign");
    client.listAccountAssets.mockResolvedValue(
      create(ListAccountAssetsResponseSchema, {
        assets: [miner],
        canManageAssignments: true,
        canViewAssignments: true,
        children: suspended ? [{ accountId: childId, name: "East", suspended: true }] : [],
      }),
    );
    const user = userEvent.setup();
    act(() => window.dispatchEvent(new Event("focus")));
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    expect(screen.getByText("1 mining asset selected")).toBeInTheDocument();
    expect(screen.getByText("No active subclients are available to receive these assets.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });
  it("follows asset cursors with miner-list ranges and clears selection between pages", async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      create(AccountAssetSchema, {
        ...miner,
        id: BigInt(index + 1),
        name: `Miner ${index + 1}`,
      }),
    );
    const lastPage = [51, 52].map((id) =>
      create(AccountAssetSchema, { ...miner, id: BigInt(id), name: `Miner ${id}` }),
    );
    const { client } = mount(31n, firstPage, undefined, "page-2", "assign");
    const user = userEvent.setup();
    await screen.findByText("Showing 1–50 mining assets");
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Select Miner 1" }));
    client.listAccountAssets.mockResolvedValueOnce(
      create(ListAccountAssetsResponseSchema, {
        assets: lastPage,
        canManageAssignments: true,
        canViewAssignments: true,
      }),
    );
    await user.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Showing 51–52 of 52 mining assets");
    expect(client.listAccountAssets).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "page-2", pageSize: 50 }),
      expect.anything(),
    );
    expect(screen.queryByText(/selected on this page/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Previous page" }));
    await screen.findByText("Showing 1–50 mining assets");
    expect(client.listAccountAssets).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "", pageSize: 50 }),
      expect.anything(),
    );
    expect(screen.getByRole("checkbox", { name: "Select Miner 1" })).not.toBeChecked();
  });
  it.each([1n, 7n])("keeps assignment details private for non-managing access %s", async (mask) => {
    mount(
      mask,
      [miner, module].map((asset) =>
        create(AccountAssetSchema, { ...asset, ownerAccountId: "", ownerAccountName: "" }),
      ),
    );
    await screen.findByText("Miner One");
    expect(screen.queryByRole("columnheader", { name: "Assigned to" })).not.toBeInTheDocument();
    expect(screen.getByText(/Assets allocated to this account/)).toBeInTheDocument();
  });
  it.each(
    [1n, 7n, 9n].flatMap((mask) => [
      { mask, mode: "assign" as const },
      { mask, mode: "inventory" as const },
    ]),
  )("withholds $mode controls without delegation at access $mask despite server permission", async ({ mask, mode }) => {
    const child = mode === "inventory" ? { accountId: childId, name: "East", suspended: false } : undefined;
    const assets = [miner, module].map((asset) =>
      create(AccountAssetSchema, { ...asset, ownerAccountId: child ? childId : accountId }),
    );
    const { scope, rerenderAccount, client } = mount(31n, assets, undefined, "", mode, child);
    const user = userEvent.setup();
    await screen.findByRole("checkbox", { name: "Select Miner One" });
    rerenderAccount(create(AccountSchema, { ...scope.account, effectiveCapabilities: mask }));
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Container" }));
    await user.click(await screen.findByRole("option", { name: "Container A" }));
    expect(screen.queryByRole("button", { name: "Select modules on this page" })).not.toBeInTheDocument();
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });
  it("shows assignment details to administration without delegation", async () => {
    mount(9n);
    await screen.findByText("Miner One");
    expect(screen.getByRole("columnheader", { name: "Assigned to" })).toBeInTheDocument();
  });
  it("removes an open allocation review when delegation is revoked without reopening it on restoration", async () => {
    const { scope, rerenderAccount, client } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    await selectTarget(user);
    expect(screen.getByRole("dialog", { name: /Assign \d+ assets?\?/ })).toBeInTheDocument();

    rerenderAccount(create(AccountSchema, { ...scope.account, effectiveCapabilities: 9n }));
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    rerenderAccount(scope.account);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });
  it("keeps the reviewed assignment pending across window focus and prevents another submission", async () => {
    let finish!: (value: object) => void;
    const assign = vi.fn(
      () =>
        new Promise<object>((resolve) => {
          finish = resolve;
        }),
    );
    mount(31n, [miner], assign, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    await selectTarget(user);
    const confirm = within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 1 asset" });
    await user.dblClick(confirm);
    expect(assign).toHaveBeenCalledTimes(1);
    expect(confirm).toBeDisabled();
    act(() => window.dispatchEvent(new Event("focus")));
    expect(screen.getByRole("dialog", { name: /Assign \d+ assets?\?/ })).toBeInTheDocument();
    expect(confirm).toBeDisabled();
    expect(assign).toHaveBeenCalledTimes(1);
    finish({});
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
  it("reviews typed selections with exact revisions and a chosen destination", async () => {
    const { client } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    const dialog = screen.getByRole("dialog", { name: /Assign \d+ assets?\?/ });
    expect(within(dialog).getByText("Assign the selected assets from Customer.")).toBeInTheDocument();
    expect(within(dialog).queryByText("Miner One")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Module One")).not.toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Assign 2 assets" })).toBeDisabled();
    await selectTarget(user);
    await user.click(within(dialog).getByRole("button", { name: "Assign 2 assets" }));
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith({
        accountId,
        childAccountId: childId,
        intent: AssetAssignmentIntent.ASSIGN,
        assets: [
          { kind: MiningAssetKind.MINER, id: 1n, expectedRevision: 7n },
          { kind: MiningAssetKind.MODULE, id: 1n, expectedRevision: 9n },
        ],
      }),
    );
    expect(await screen.findByRole("status")).toHaveTextContent("Assigned 2 assets to East");
  });
  it("selects only displayed modules for a container and states the page boundary", async () => {
    mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await screen.findByText("Module One");
    await user.click(screen.getByRole("button", { name: /Container/ }));
    await user.click(await screen.findByRole("option", { name: "Container A" }));
    await user.click(await screen.findByRole("button", { name: "Select modules on this page" }));
    expect(screen.getByRole("checkbox", { name: "Select Module One" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    expect(within(screen.getByRole("dialog")).getByText(/Only the selected modules are included/)).toBeInTheDocument();
  });
  it("clears stale selections and requires another review after an ownership conflict", async () => {
    const assign = vi.fn().mockRejectedValue(new ConnectError("Assignment changed", Code.Aborted));
    const { client } = mount(31n, [miner], assign, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    await selectTarget(user);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 1 asset" }));
    expect(await screen.findByRole("status")).toHaveTextContent(/select them again/);
    await waitFor(() => expect(client.listAccountAssets).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(await screen.findByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    expect(assign).toHaveBeenCalledTimes(1);
  });
  it("contains Tab and Shift+Tab in the recall dialog while its actions are pending", async () => {
    vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue(
      Object.assign([new DOMRect()], { item: () => null }),
    );
    let finish!: (value: object) => void;
    const recall = vi.fn(
      () =>
        new Promise<object>((resolve) => {
          finish = resolve;
        }),
    );
    mount(31n, [create(AccountAssetSchema, { ...miner, ownerAccountId: childId })], recall, "", "inventory", {
      accountId: childId,
      name: "East",
      suspended: false,
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    const dialog = screen.getByRole("dialog", { name: "Recall 1 asset?" });
    await user.click(within(dialog).getByRole("button", { name: "Recall 1 asset" }));
    expect(recall).toHaveBeenCalledOnce();
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "Recall 1 asset" })).toBeDisabled();
    await user.tab();
    expect(dialog).toHaveFocus();
    await user.tab({ shift: true });
    expect(dialog).toHaveFocus();
    expect(recall).toHaveBeenCalledOnce();
    await act(async () => finish({}));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
  it("allows a parent to recall retired child allocations without assigning replacement hardware", async () => {
    const retired = create(AccountAssetSchema, {
      kind: MiningAssetKind.MODULE,
      id: 8n,
      revision: 4n,
      ownerAccountId: childId,
      ownerAccountName: "East",
      available: false,
      name: "",
    });
    const { client } = mount(31n, [retired], undefined, "", "inventory", {
      accountId: childId,
      name: "East",
      suspended: false,
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Unavailable module 8" }));
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    expect(within(screen.getByRole("dialog")).queryByText("Unavailable module 8")).not.toBeInTheDocument();
    expect(within(screen.getByRole("dialog")).getByText(/from East to Customer/)).toBeInTheDocument();
    await user.click(
      within(screen.getByRole("dialog", { name: /Recall \d+ assets?\?/ })).getByRole("button", {
        name: "Recall 1 asset",
      }),
    );
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith({
        accountId,
        childAccountId: childId,
        intent: AssetAssignmentIntent.RECALL,
        assets: [{ kind: MiningAssetKind.MODULE, id: 8n, expectedRevision: 4n }],
      }),
    );
  });
});
