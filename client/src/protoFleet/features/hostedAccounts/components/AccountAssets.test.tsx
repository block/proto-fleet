import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountAssets from "./AccountAssets";
import { FleetErrorDetailsSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import { SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  AccountAssetSchema,
  AccountSchema,
  AssetAssignmentErrorCode,
  AssetAssignmentIntent,
  ListAccountAssetsResponseSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const returnDefault = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/features/containers/ReturnHashboardsToDefaultDialog", () => ({
  default: (props: { hashboardIds: bigint[]; recalled: boolean; onClose: () => void }) => {
    returnDefault(props);
    return (
      <div role="dialog" aria-label="Return hashboards to Default">
        <button onClick={props.onClose}>Later</button>
      </div>
    );
  },
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success" },
}));

vi.mock("@/protoFleet/features/poolConfigurations/ConfigurationSelector", () => ({
  ConfigurationSelector: ({
    deviceIds = [],
    boards,
  }: {
    deviceIds?: bigint[];
    boards?: { moduleIds?: bigint[]; hashboardIds?: bigint[] };
  }) => (
    <div role="dialog" aria-label="Pool selection">
      <span aria-label="Selected miners">{deviceIds.map(String).join(",")}</span>
      <span aria-label="Selected modules">{boards?.moduleIds?.map(String).join(",")}</span>
      <span aria-label="Selected hashboards">{boards?.hashboardIds?.map(String).join(",")}</span>
    </div>
  ),
}));

beforeEach(() => {
  returnDefault.mockClear();
  useFleetStore.getState().auth.setPermissions([]);
  vi.mocked(pushToast).mockClear();
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
  boards: [
    { id: 11n, expectedRevision: 7n },
    { id: 12n, expectedRevision: 9n },
    { id: 13n, expectedRevision: 2n },
  ],
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
const boards = module.boards.map((board, index) =>
  create(AccountAssetSchema, {
    kind: MiningAssetKind.HASHBOARD,
    id: board.id,
    revision: board.expectedRevision,
    name: `Hashboard ${index + 1}`,
    available: true,
    ownerAccountId: accountId,
  }),
);
function mount(
  mask = 31n,
  assets = [miner, module],
  assignment = vi.fn().mockResolvedValue({}),
  nextPageToken = "",
  mode: "inventory" | "assign" = "inventory",
  child?: { accountId: string; name: string; suspended: boolean },
  props: { onAssigned?: () => void } = {},
) {
  const client = {
    listAccountAssets: vi.fn().mockResolvedValue(
      create(ListAccountAssetsResponseSchema, {
        assets,
        nextPageToken,
        filterOptions: {
          models: ["S21", "S19"],
          sites: [
            { id: 7n, name: "North" },
            { id: 8n, name: "South" },
          ],
          containers: [
            { id: 10n, name: "Container A" },
            { id: 20n, name: "Container B" },
          ],
        },
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
        <AccountAssets mode={mode} child={child} {...props} />
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
  await user.click(screen.getByRole("button", { name: /Allocate to subclient/ }));
  await user.click(await screen.findByRole("option", { name: "East" }));
};

describe("account mining asset allocation", () => {
  it("offers the Default confirmation for only the recalled module's reviewed boards", async () => {
    useFleetStore.getState().auth.setPermissions(["miner:update_pools", "miner:start_mining", "miner:stop_mining"]);
    const { scope, rerenderAccount, client } = mount(
      31n,
      [create(AccountAssetSchema, { ...module, ownerAccountId: childId })],
      undefined,
      "",
      "inventory",
      { accountId: childId, name: "East", suspended: false },
    );
    rerenderAccount(create(AccountSchema, { ...scope.account, depth: 0 }));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    expect(returnDefault).not.toHaveBeenCalled();
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Recall 1 asset" }));
    await screen.findByRole("dialog", { name: "Return hashboards to Default" });
    expect(returnDefault).toHaveBeenLastCalledWith(expect.objectContaining({ hashboardIds: [11n, 12n, 13n] }));
    expect(client.assignAccountAssets).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Later" }));
    expect(screen.queryByRole("dialog", { name: "Return hashboards to Default" })).not.toBeInTheDocument();
    expect(client.assignAccountAssets).toHaveBeenCalledOnce();
  });
  it("closes the optional Default dialog when pool permissions are removed", async () => {
    const permissions = ["miner:update_pools", "miner:start_mining", "miner:stop_mining"];
    useFleetStore.getState().auth.setPermissions(permissions);
    const { scope, rerenderAccount, client } = mount(
      31n,
      [create(AccountAssetSchema, { ...module, ownerAccountId: childId })],
      undefined,
      "",
      "inventory",
      { accountId: childId, name: "East", suspended: false },
    );
    rerenderAccount(create(AccountSchema, { ...scope.account, depth: 0 }));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Recall 1 asset" }));
    await screen.findByRole("dialog", { name: "Return hashboards to Default" });
    act(() => useFleetStore.getState().auth.setPermissions(["miner:update_pools"]));
    expect(screen.queryByRole("dialog", { name: "Return hashboards to Default" })).not.toBeInTheDocument();
    act(() => useFleetStore.getState().auth.setPermissions(permissions));
    expect(screen.queryByRole("dialog", { name: "Return hashboards to Default" })).not.toBeInTheDocument();
    expect(client.assignAccountAssets).toHaveBeenCalledOnce();
  });

  it.each([
    { name: "a host without pool permissions", depth: 0, permitted: false, asset: module },
    { name: "a client with pool permissions", depth: 1, permitted: true, asset: module },
    { name: "a host recalling only miners", depth: 0, permitted: true, asset: miner },
  ])("keeps recall available without the optional Default dialog for $name", async ({ depth, permitted, asset }) => {
    if (permitted)
      useFleetStore.getState().auth.setPermissions(["miner:update_pools", "miner:start_mining", "miner:stop_mining"]);
    const { scope, rerenderAccount, client } = mount(
      31n,
      [create(AccountAssetSchema, { ...asset, ownerAccountId: childId })],
      undefined,
      "",
      "inventory",
      { accountId: childId, name: "East", suspended: false },
    );
    rerenderAccount(create(AccountSchema, { ...scope.account, depth }));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: `Select ${asset.name}` }));
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Recall 1 asset" }));
    await waitFor(() => expect(client.assignAccountAssets).toHaveBeenCalledOnce());
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(returnDefault).not.toHaveBeenCalled();
  });

  it("leaves the organization name out of root allocation copy", async () => {
    const { scope, rerenderAccount } = mount(31n, undefined, undefined, "", "assign");
    rerenderAccount(create(AccountSchema, { ...scope.account, depth: 0 }));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    const dialog = screen.getByRole("dialog", { name: "Allocate 1 asset?" });
    expect(within(dialog).getByRole("button", { name: /Allocate to client/ })).toBeInTheDocument();
    expect(within(dialog).queryByText(/Customer/)).not.toBeInTheDocument();
  });

  it("leaves the organization name out of root recall copy", async () => {
    const { scope, rerenderAccount } = mount(
      31n,
      [create(AccountAssetSchema, { ...miner, ownerAccountId: childId })],
      undefined,
      "",
      "inventory",
      { accountId: childId, name: "East", suspended: false },
    );
    rerenderAccount(create(AccountSchema, { ...scope.account, depth: 0 }));
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/^Recall the selected assets from East\./)).toBeInTheDocument();
    expect(within(dialog).queryByText(/Customer/)).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Recall 1 asset" }));
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Recalled 1 asset from East.", status: STATUSES.success }),
    );
  });

  it("keeps pool status visible to account readers without Change pools permission", async () => {
    mount(1n);
    await screen.findByText("Miner One");
    expect(screen.getByRole("columnheader", { name: "Pool configuration" })).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    expect(screen.getByRole("checkbox", { name: "Toggle Pool configuration column" })).toBeChecked();
  });

  it("clears an individual board choice when its allocation changes during refresh", async () => {
    const { client } = mount(31n, [module], undefined, "", "assign");
    await screen.findByText("Module One");
    client.listAccountAssets.mockImplementation((request: { selectedAssets?: unknown[] }) =>
      Promise.resolve(
        create(
          ListAccountAssetsResponseSchema,
          request.selectedAssets
            ? { assets: boards }
            : {
                assets: [module],
                canManageAssignments: true,
                canViewAssignments: true,
                children: [{ accountId: childId, name: "East" }],
              },
        ),
      ),
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    await user.click(await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" }));
    expect(screen.getByRole("button", { name: "Allocate selected" })).toBeInTheDocument();
    client.listAccountAssets.mockImplementation((request: { selectedAssets?: unknown[] }) =>
      Promise.resolve(
        create(
          ListAccountAssetsResponseSchema,
          request.selectedAssets
            ? {
                assets: boards.map((board) =>
                  create(AccountAssetSchema, { ...board, ownerAccountId: childId, revision: board.revision + 1n }),
                ),
              }
            : {
                assets: [module],
                canManageAssignments: true,
                canViewAssignments: true,
                children: [{ accountId: childId, name: "East" }],
              },
        ),
      ),
    );
    await act(async () => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument());
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });

  it("expands module boards and switches between full-module and individual allocation", async () => {
    const { client } = mount(31n, undefined, undefined, "", "assign");
    await screen.findByText("Module One");
    expect(client.listAccountAssets).toHaveBeenCalledTimes(1);
    client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: boards }));
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    const firstBoard = await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" });
    expect(screen.getByText("2 assets on this page")).toBeInTheDocument();
    expect(client.listAccountAssets).toHaveBeenLastCalledWith(
      {
        accountId,
        selectedAssets: boards.map((board) => ({ kind: MiningAssetKind.HASHBOARD, id: board.id })),
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    await user.click(firstBoard);
    const wholeModule = screen.getByRole("checkbox", { name: "Select Module One" });
    expect(wholeModule).toHaveAttribute("aria-checked", "mixed");
    await user.click(wholeModule);
    expect(wholeModule).toBeChecked();
    for (const board of boards)
      expect(screen.getByRole("checkbox", { name: `Select ${board.name} in Module One` })).toBeChecked();
    await user.click(firstBoard);
    expect(firstBoard).not.toBeChecked();
    expect(wholeModule).toHaveAttribute("aria-checked", "mixed");
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    const dialog = screen.getByRole("dialog", { name: "Allocate 2 assets?" });
    await selectTarget(user);
    await user.click(within(dialog).getByRole("button", { name: "Allocate 2 assets" }));
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith({
        accountId,
        childAccountId: childId,
        intent: AssetAssignmentIntent.ASSIGN,
        assets: boards
          .slice(1)
          .map((board) => ({ kind: MiningAssetKind.HASHBOARD, id: board.id, expectedRevision: board.revision })),
      }),
    );
  });

  it("keeps selected boards when modules open or close and clears selection on page changes", async () => {
    const second = create(AccountAssetSchema, {
      ...module,
      id: 2n,
      name: "Module Two",
      boards: [{ ...module.boards[0], id: 21n, expectedRevision: 1n }],
    });
    const { client } = mount(31n, [module, second], undefined, "next", "assign");
    await screen.findByText("Module One");
    client.listAccountAssets.mockResolvedValueOnce(
      create(ListAccountAssetsResponseSchema, {
        assets: [...boards, create(AccountAssetSchema, { ...boards[0], id: 21n, revision: 1n, name: "Other board" })],
      }),
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    await user.click(await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" }));
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module Two" }));
    expect(await screen.findByRole("checkbox", { name: "Select Other board in Module Two" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select Hashboard 1 in Module One" })).toBeChecked();
    expect(client.listAccountAssets).toHaveBeenCalledTimes(2);
    await user.click(screen.getByRole("button", { name: "Collapse hashboards for Module One" }));
    expect(screen.queryByRole("checkbox", { name: "Select Hashboard 1 in Module One" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Allocate selected" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Collapse hashboards for Module Two" }));
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    expect(screen.getByRole("checkbox", { name: "Select Hashboard 1 in Module One" })).toBeChecked();
    expect(client.listAccountAssets).toHaveBeenCalledTimes(2);
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
  });

  it.each(["row", "header"])(
    "clears a top-level board through the %s checkbox after its module loads",
    async (control) => {
      const { client } = mount(31n, [module, boards[0]], undefined, "", "assign");
      await screen.findByText("Module One");
      client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: boards }));
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
      const child = await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" });
      const topLevel = screen.getByRole("checkbox", { name: "Select Hashboard 1" });
      if (control === "row") {
        await user.click(child);
        expect(topLevel).toBeChecked();
        await user.click(topLevel);
      } else {
        const header = screen.getByRole("checkbox", { name: "Select all assets on this page" });
        await user.click(header);
        expect(header).toBeChecked();
        expect(child).toBeChecked();
        await user.click(header);
      }
      expect(topLevel).not.toBeChecked();
      expect(child).not.toBeChecked();
      expect(screen.getByRole("checkbox", { name: "Select Module One" })).not.toBeChecked();
      expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
      expect(client.assignAccountAssets).not.toHaveBeenCalled();
    },
  );

  it.each(["parent", "child"])(
    "removes a covered board through its %s row without restoring its duplicate",
    async (row) => {
      const { client } = mount(31n, [module, boards[0]], undefined, "", "assign");
      await screen.findByText("Module One");
      client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: boards }));
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
      const child = await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" });
      const parent = screen.getByRole("checkbox", { name: "Select Hashboard 1" });
      await user.click(screen.getByRole("checkbox", { name: "Select all assets on this page" }));
      await user.click(row === "parent" ? parent : child);
      expect(parent).not.toBeChecked();
      expect(child).not.toBeChecked();
      expect(screen.getByRole("checkbox", { name: "Select Module One" })).toHaveAttribute("aria-checked", "mixed");
      await user.click(screen.getByRole("button", { name: "Allocate selected" }));
      const dialog = screen.getByRole("dialog", { name: "Allocate 2 assets?" });
      await selectTarget(user);
      await user.click(within(dialog).getByRole("button", { name: "Allocate 2 assets" }));
      await waitFor(() =>
        expect(client.assignAccountAssets).toHaveBeenCalledWith(
          expect.objectContaining({
            assets: boards
              .slice(1)
              .map((board) => ({ kind: MiningAssetKind.HASHBOARD, id: board.id, expectedRevision: board.revision })),
          }),
        ),
      );
    },
  );

  it.each(["board", "module"])(
    "does not repeat a board shown in parent and child rows when selecting a %s",
    async (choice) => {
      const { client } = mount(31n, [miner, module, boards[0]], undefined, "", "assign");
      await screen.findByText("Module One");
      client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: boards }));
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
      const boardCheckbox = await screen.findByRole("checkbox", { name: "Select Hashboard 1 in Module One" });
      if (choice === "board") {
        await user.click(boardCheckbox);
        await user.click(screen.getByRole("checkbox", { name: "Select Miner One" }));
      } else {
        await user.click(screen.getByRole("checkbox", { name: "Select all assets on this page" }));
      }
      await user.click(screen.getByRole("button", { name: "Allocate selected" }));
      const dialog = screen.getByRole("dialog", { name: "Allocate 2 assets?" });
      await selectTarget(user);
      await user.click(within(dialog).getByRole("button", { name: "Allocate 2 assets" }));
      const expected =
        choice === "board"
          ? { kind: MiningAssetKind.HASHBOARD, id: boards[0].id, expectedRevision: boards[0].revision }
          : { kind: MiningAssetKind.MODULE, id: module.id, expectedRevision: module.revision, boards: module.boards };
      await waitFor(() =>
        expect(client.assignAccountAssets).toHaveBeenCalledWith(
          expect.objectContaining({
            assets: [{ kind: MiningAssetKind.MINER, id: miner.id, expectedRevision: miner.revision }, expected],
          }),
        ),
      );
    },
  );

  it("disables missing, changed, and ineligible boards without broadening the reviewed module", async () => {
    const { client } = mount(31n, [module], undefined, "", "assign");
    await screen.findByText("Module One");
    client.listAccountAssets.mockResolvedValueOnce(
      create(ListAccountAssetsResponseSchema, {
        assets: [
          create(AccountAssetSchema, { ...boards[0], revision: 100n }),
          create(AccountAssetSchema, { ...boards[1], outsidePolicy: true }),
        ],
      }),
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    await screen.findByText("Hashboard 2");
    expect(screen.getAllByRole("checkbox", { name: "Select unavailable hashboard in Module One" })).toHaveLength(2);
    for (const checkbox of screen
      .getAllByRole("checkbox")
      .filter((element) => element.getAttribute("aria-label")?.includes("in Module One")))
      expect(checkbox).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" }));
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith(
        expect.objectContaining({
          assets: [
            { kind: MiningAssetKind.MODULE, id: module.id, expectedRevision: module.revision, boards: module.boards },
          ],
        }),
      ),
    );
  });

  it("shows module boards without selection controls to a viewer", async () => {
    const { client } = mount(1n, [module]);
    await screen.findByText("Module One");
    client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: boards }));
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Expand hashboards for Module One" }));
    await screen.findByText("Hashboard 1");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("keeps the toolbar and filtered empty state when a miner-only account shows hashboards", async () => {
    const { client } = mount(31n, [miner]);
    const user = userEvent.setup();
    await screen.findByText("Miner One");
    client.listAccountAssets.mockImplementation((request: { hashboardsOnly?: boolean }) =>
      Promise.resolve(create(ListAccountAssetsResponseSchema, { assets: request.hashboardsOnly ? [] : [miner] })),
    );

    await user.click(screen.getByRole("button", { name: "Show" }));
    await user.click(screen.getByRole("option", { name: "Individual hashboards" }));
    expect(await screen.findByText("No matching assets")).toBeInTheDocument();
    expect(screen.queryByText("No mining assets")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search mining assets" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Manage columns" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show" }));
    await user.click(screen.getByRole("option", { name: "All assets" }));
    expect(await screen.findByText("Miner One")).toBeInTheDocument();
    expect(screen.queryByText("No matching assets")).not.toBeInTheDocument();
  });
  it("removes assets after a background authorization change without requiring window focus", async () => {
    const polling = vi.spyOn(window, "setInterval");
    const { client } = mount();
    await screen.findByText("Miner One");
    client.listAccountAssets.mockResolvedValue(create(ListAccountAssetsResponseSchema, { assets: [] }));
    const poll = polling.mock.calls.find(([, interval]) => interval === POLL_INTERVAL_MS)?.[0];
    expect(poll).toBeTypeOf("function");
    await act(async () => {
      if (typeof poll === "function") poll();
    });
    await waitFor(() => expect(screen.queryByText("Miner One")).not.toBeInTheDocument());
    expect(screen.queryByText("Module One")).not.toBeInTheDocument();
  });
  it("uses Fleet's heading, navigation, tools, metric units and common columns", async () => {
    mount();
    const minerRow = (await screen.findByText("Miner One")).closest("tr")!;
    expect(screen.getByRole("heading", { name: "Fleet" })).toBeInTheDocument();
    expect(
      within(screen.getByRole("navigation", { name: "Fleet sections" })).getByRole("button", { name: "Miners" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "Add Filter" })).toBeInTheDocument();
    expect(screen.getByText("2 mining assets")).toBeInTheDocument();
    expect(screen.queryByText(/Assets allocated to this account/)).not.toBeInTheDocument();
    for (const label of ["Name", "Model", "Status", "Hashrate", "Efficiency", "Power", "Temp"])
      expect(screen.getByRole("columnheader", { name: label })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Equipment power" })).not.toBeInTheDocument();
    for (const text of ["S21", "200.0 TH/s", "30.0 J/TH", "6.0 kW", "55.5 °C"])
      expect(within(minerRow).getByText(text)).toBeInTheDocument();
    const moduleRow = screen.getByText("Module One").closest("tr")!;
    expect(within(moduleRow).getByText("42.5 °C")).toBeInTheDocument();
    expect(within(moduleRow).getAllByText("1/2 boards reporting")).toHaveLength(2);
  });
  it("filters client Fleet through shared chips and resets pagination when filters change", async () => {
    const { client } = mount(31n, undefined, undefined, "next");
    await screen.findByText("Miner One");
    expect(screen.getByText("2 mining assets on this page")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ pageToken: "next" }),
        expect.anything(),
      ),
    );
    const user = userEvent.setup();
    const selectFilter = async (key: string, id: string) => {
      await user.click(screen.getByRole("button", { name: "Add Filter" }));
      await user.click(screen.getByTestId(`nested-dropdown-filter-row-${key}`));
      await user.click(await screen.findByTestId(`filter-option-${id}`));
      fireEvent.mouseDown(screen.getByRole("heading", { name: "Fleet" }));
    };
    await selectFilter("assignment", childId);
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ ownerAccountId: childId, pageToken: "" }),
        expect.anything(),
      ),
    );
    expect(screen.getByTestId("active-filter-assignment")).toHaveTextContent("East");
    await selectFilter("model", "S19");
    await selectFilter("site", "8");
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ model: "S19", siteId: 8n, pageToken: "" }),
        expect.anything(),
      ),
    );
    expect(screen.getByTestId("active-filter-model")).toHaveTextContent("S19");
    expect(screen.getByTestId("active-filter-site")).toHaveTextContent("South");
    await selectFilter("container", "10");
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ ownerAccountId: childId, containerId: 10n, pageToken: "" }),
        expect.anything(),
      ),
    );
    expect(screen.getByTestId("active-filter-container")).toHaveTextContent("Container A");
    fireEvent.click(screen.getByRole("button", { name: "Clear Allocated to filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear Container filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear Model filter" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear Site filter" }));
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({
          ownerAccountId: undefined,
          containerId: undefined,
          model: undefined,
          siteId: undefined,
          pageToken: "",
        }),
        expect.anything(),
      ),
    );
  });
  it("keeps the toolbar mounted while clearing a search that matched nothing", async () => {
    const user = userEvent.setup();
    const { client } = mount();
    await screen.findByText("Miner One");
    client.listAccountAssets.mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: [] }));
    await user.click(screen.getByRole("button", { name: "Search mining assets" }));
    await user.type(screen.getByLabelText("Search mining assets"), "nothing");
    await waitFor(() => expect(screen.queryByText("Miner One")).not.toBeInTheDocument());
    const search = screen.getByLabelText("Search mining assets");
    client.listAccountAssets.mockReturnValueOnce(new Promise(() => {}));
    await user.clear(search);
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ search: "" }),
        expect.anything(),
      ),
    );
    expect(screen.getByLabelText("Search mining assets")).toBe(search);
    expect(search).toHaveFocus();
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

  it("keeps inventory read-only without Change pools, including subclient allocations", async () => {
    const { client } = mount(27n, [miner, create(AccountAssetSchema, { ...module, ownerAccountId: childId })]);
    await screen.findByText("Miner One");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Add Filter" }));
    await user.click(screen.getByTestId("nested-dropdown-filter-row-assignment"));
    await user.click(screen.getByTestId(`filter-option-${childId}`));
    fireEvent.mouseDown(screen.getByRole("heading", { name: "Fleet" }));
    expect(screen.getByRole("link", { name: "Manage subclient assets" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/children?assets=${childId}`,
    );
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });

  it("offers pool changes only for eligible miners and clears the action when user permission is revoked", async () => {
    const user = userEvent.setup();
    const { scope, rerenderAccount, client } = mount(5n, [
      create(AccountAssetSchema, { ...miner, canChangePools: true, ownerAccountId: "" }),
      module,
    ]);
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    expect(screen.getByRole("checkbox", { name: "Select Module One" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Edit pool" }));
    expect(screen.getByRole("dialog", { name: "Pool selection" })).toHaveTextContent("1");
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
    rerenderAccount(create(AccountSchema, { ...scope.account, effectiveCapabilities: 1n }));
    expect(screen.queryByRole("dialog", { name: "Pool selection" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    rerenderAccount(scope.account);
    expect(screen.queryByRole("dialog", { name: "Pool selection" })).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
  });
  it("passes an individually selected hosted hashboard to the shared pool picker", async () => {
    const user = userEvent.setup();
    mount(5n, [create(AccountAssetSchema, { ...boards[0], canChangePools: true })]);
    await user.click(await screen.findByRole("checkbox", { name: "Select Hashboard 1" }));
    await user.click(screen.getByRole("button", { name: "Edit pool" }));
    const selection = within(screen.getByRole("dialog", { name: "Pool selection" }));
    expect(selection.getByLabelText("Selected hashboards")).toHaveTextContent("11");
    expect(selection.getByLabelText("Selected miners")).toBeEmptyDOMElement();
    expect(selection.getByLabelText("Selected modules")).toBeEmptyDOMElement();
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
    expect(screen.queryByRole("button", { name: "Allocation" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Recall 1 asset" }));
    expect(client.assignAccountAssets).toHaveBeenCalledWith(
      expect.objectContaining({ accountId, childAccountId: childId, intent: AssetAssignmentIntent.RECALL }),
    );
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Recalled 1 asset to Customer.", status: STATUSES.success }),
    );
    expect(screen.queryByText("Recalled 1 asset to Customer.")).not.toBeInTheDocument();
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
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    expect(screen.queryByRole("button", { name: "Allocate to subclient" })).not.toBeInTheDocument();
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" }));
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
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
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
    expect(screen.queryByRole("columnheader", { name: "Allocated to" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add Filter" }));
    expect(screen.queryByTestId("nested-dropdown-filter-row-assignment")).not.toBeInTheDocument();
    expect(screen.getByTestId("nested-dropdown-filter-row-container")).toBeInTheDocument();
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
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Recall selected" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Container" }));
    await user.click(await screen.findByRole("option", { name: "Container A" }));
    expect(screen.queryByRole("button", { name: "Select modules on this page" })).not.toBeInTheDocument();
    expect(client.assignAccountAssets).not.toHaveBeenCalled();
  });
  it("shows assignment details to administration without delegation", async () => {
    mount(9n);
    await screen.findByText("Miner One");
    expect(screen.getByRole("columnheader", { name: "Allocated to" })).toBeInTheDocument();
  });
  it("removes an open allocation review when delegation is revoked without reopening it on restoration", async () => {
    const { scope, rerenderAccount, client } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    expect(screen.getByRole("dialog", { name: /Allocate \d+ assets?\?/ })).toBeInTheDocument();

    rerenderAccount(create(AccountSchema, { ...scope.account, effectiveCapabilities: 9n }));
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
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
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    const confirm = within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" });
    await user.dblClick(confirm);
    expect(assign).toHaveBeenCalledTimes(1);
    expect(confirm).toBeDisabled();
    act(() => window.dispatchEvent(new Event("focus")));
    expect(screen.getByRole("dialog", { name: /Allocate \d+ assets?\?/ })).toBeInTheDocument();
    expect(confirm).toBeDisabled();
    expect(assign).toHaveBeenCalledTimes(1);
    finish({});
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
  it("keeps an open review across window focus revalidation", async () => {
    const { client } = mount(31n, [miner], undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(client.listAccountAssets).toHaveBeenCalledTimes(2));
    const dialog = screen.getByRole("dialog", { name: "Allocate 1 asset?" });
    expect(screen.getByText("Miner One")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Allocate 1 asset" }));
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith(
        expect.objectContaining({
          childAccountId: childId,
          assets: [{ kind: MiningAssetKind.MINER, id: 1n, expectedRevision: 7n }],
        }),
      ),
    );
  });
  it("drops selections that a focus revalidation no longer offers", async () => {
    const { client } = mount(31n, [miner, module], undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Module One" }));
    expect(screen.getByText("2 mining assets selected")).toBeInTheDocument();
    client.listAccountAssets.mockResolvedValue(
      create(ListAccountAssetsResponseSchema, {
        assets: [miner, create(AccountAssetSchema, { ...module, ownerAccountId: childId, revision: 10n })],
        canManageAssignments: true,
        canViewAssignments: true,
        children: [{ accountId: childId, name: "East" }],
      }),
    );
    act(() => window.dispatchEvent(new Event("focus")));
    expect(await screen.findByText("1 mining asset selected")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).toBeChecked();
  });
  it("shows the assignment confirmation once in a toast when returning to inventory", async () => {
    const onAssigned = vi.fn();
    const child = { accountId: childId, name: "East", suspended: false };
    const { unmount } = mount(31n, [miner], undefined, "", "assign", child, { onAssigned });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" }));
    await waitFor(() => expect(onAssigned).toHaveBeenCalledOnce());
    expect(pushToast).toHaveBeenCalledExactlyOnceWith({
      message: "Allocated 1 asset to East.",
      status: STATUSES.success,
    });
    expect(screen.queryByText("Allocated 1 asset to East.")).not.toBeInTheDocument();
    unmount();
    mount(31n, [miner], undefined, "", "inventory", child);
    await screen.findByText("Miner One");
    expect(screen.queryByText("Allocated 1 asset to East.")).not.toBeInTheDocument();
    expect(pushToast).toHaveBeenCalledOnce();
  });
  it("keeps the hidden assignment column when a subclient view saves its columns", async () => {
    const user = userEvent.setup();
    const inventory = mount();
    await screen.findByRole("columnheader", { name: "Allocated to" });
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    await user.click(screen.getByRole("checkbox", { name: "Toggle Allocated to column" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.queryByRole("columnheader", { name: "Allocated to" })).not.toBeInTheDocument();
    inventory.unmount();
    const child = mount(31n, undefined, undefined, "", "inventory", {
      accountId: childId,
      name: "East",
      suspended: false,
    });
    await screen.findByText("Miner One");
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    expect(screen.queryByRole("checkbox", { name: "Toggle Allocated to column" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Toggle Power column" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    child.unmount();
    mount();
    await screen.findByText("Miner One");
    expect(screen.queryByRole("columnheader", { name: "Allocated to" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Power" })).not.toBeInTheDocument();
  });
  it("reviews typed selections with exact revisions and a chosen destination", async () => {
    const { client } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    const dialog = screen.getByRole("dialog", { name: /Allocate \d+ assets?\?/ });
    expect(within(dialog).getByText("Allocate the selected assets from Customer.")).toBeInTheDocument();
    expect(within(dialog).queryByText("Miner One")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Module One")).not.toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Allocate 2 assets" })).toBeDisabled();
    await selectTarget(user);
    await user.click(within(dialog).getByRole("button", { name: "Allocate 2 assets" }));
    await waitFor(() =>
      expect(client.assignAccountAssets).toHaveBeenCalledWith({
        accountId,
        childAccountId: childId,
        intent: AssetAssignmentIntent.ASSIGN,
        assets: [
          { kind: MiningAssetKind.MINER, id: 1n, expectedRevision: 7n },
          { kind: MiningAssetKind.MODULE, id: 1n, expectedRevision: 9n, boards: module.boards },
        ],
      }),
    );
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Allocated 2 assets to East.", status: STATUSES.success }),
    );
  });
  it("selects only displayed modules for a container and states the page boundary", async () => {
    const { client } = mount(31n, undefined, undefined, "", "assign");
    const user = userEvent.setup();
    await screen.findByText("Module One");
    await user.click(screen.getByRole("button", { name: /Container/ }));
    await user.click(await screen.findByRole("option", { name: "Container A" }));
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ containerId: 10n, pageToken: "" }),
        expect.anything(),
      ),
    );
    await user.click(await screen.findByRole("button", { name: "Select modules on this page" }));
    expect(screen.getByRole("checkbox", { name: "Select Module One" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    expect(
      within(screen.getByRole("dialog")).getByText(/Only the boards in the reviewed modules are included/),
    ).toBeInTheDocument();
  });
  it("keeps the reviewed selection available to retry after a destination policy error", async () => {
    const policyError = "selected assets are outside the destination account's site policy";
    const assign = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError(policyError, Code.InvalidArgument))
      .mockResolvedValue({});
    const { client } = mount(31n, [miner], assign, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    const dialog = screen.getByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Allocate 1 asset" });
    await user.click(confirm);
    expect(await within(dialog).findByText(policyError)).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).toBeChecked();
    expect(client.listAccountAssets).toHaveBeenCalledOnce();
    await user.click(confirm);
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Allocated 1 asset to East.", status: STATUSES.success }),
    );
    expect(assign).toHaveBeenCalledTimes(2);
  });
  it("keeps the mining-group prerequisite visible in the allocation dialog", async () => {
    const message = "Enable mining groups and refresh the container before allocating its hashboards.";
    const error = new ConnectError(message, Code.FailedPrecondition, undefined, [
      {
        desc: FleetErrorDetailsSchema,
        value: { code: { case: "endpoint", value: AssetAssignmentErrorCode.MINING_GROUPS_NOT_READY } },
      },
    ]);
    const assign = vi.fn().mockRejectedValue(error);
    const { client } = mount(31n, [miner], assign, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Allocate 1 asset" }));
    expect(await within(dialog).findByText(message)).toBeVisible();
    expect(client.listAccountAssets).toHaveBeenCalledOnce();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).toBeChecked();
  });
  it("clears stale selections and requires another review after an ownership conflict", async () => {
    const assign = vi.fn().mockRejectedValue(new ConnectError("Assignment changed", Code.Aborted));
    const { client } = mount(31n, [miner], assign, "", "assign");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await selectTarget(user);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" }));
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
  it("allows a parent to recall unavailable board allocations without assigning replacement hardware", async () => {
    const retired = create(AccountAssetSchema, {
      kind: MiningAssetKind.HASHBOARD,
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
    await user.click(await screen.findByRole("checkbox", { name: "Select Unavailable hashboard 8" }));
    expect(screen.queryByRole("button", { name: "Allocate selected" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    expect(within(screen.getByRole("dialog")).queryByText("Unavailable hashboard 8")).not.toBeInTheDocument();
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
        assets: [{ kind: MiningAssetKind.HASHBOARD, id: 8n, expectedRevision: 4n }],
      }),
    );
  });
});
