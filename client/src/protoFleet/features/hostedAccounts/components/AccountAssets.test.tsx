import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountAssets from "./AccountAssets";
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
afterEach(() => vi.restoreAllMocks());

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
  partialCoverage: true,
});
function mount(mask = 31n, assets = [miner, module], assignment = vi.fn().mockResolvedValue({})) {
  const client = {
    listAccountAssets: vi.fn().mockResolvedValue(
      create(ListAccountAssetsResponseSchema, {
        assets,
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
  render(
    <MemoryRouter>
      <AccountScopeContext.Provider value={scope}>
        <AccountAssets />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
  return { client, scope };
}
const selectTarget = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole("button", { name: /Assign to subclient/ }));
  await user.click(await screen.findByRole("option", { name: "East" }));
};

describe("account mining asset allocation", () => {
  it("keeps viewers read-only and names reporting-only assignment semantics", async () => {
    mount(1n, [create(AccountAssetSchema, { ...miner, ownerAccountId: "", ownerAccountName: "" })]);
    await screen.findByText("Miner One");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Assigned to" })).not.toBeInTheDocument();
    expect(screen.getByText(/Assignments control account reporting/)).toBeInTheDocument();
  });
  it("withholds allocation controls when administration lacks delegation", async () => {
    mount(9n);
    await screen.findByText("Miner One");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Assigned to" })).toBeInTheDocument();
  });
  it("keeps the reviewed assignment pending across window focus and prevents another submission", async () => {
    let finish!: (value: object) => void;
    const assign = vi.fn(
      () =>
        new Promise<object>((resolve) => {
          finish = resolve;
        }),
    );
    mount(31n, [miner], assign);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    await selectTarget(user);
    const confirm = within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 1 asset" });
    await user.dblClick(confirm);
    expect(assign).toHaveBeenCalledTimes(1);
    expect(confirm).toBeDisabled();
    act(() => window.dispatchEvent(new Event("focus")));
    expect(screen.getByRole("dialog", { name: "Assign assets" })).toBeInTheDocument();
    expect(confirm).toBeDisabled();
    expect(assign).toHaveBeenCalledTimes(1);
    finish({});
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
  it("reviews typed selections with exact revisions and a chosen destination", async () => {
    const { client } = mount();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Module One" }));
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    const dialog = screen.getByRole("dialog", { name: "Assign assets" });
    expect(within(dialog).getByText("1 miner · 1 module")).toBeInTheDocument();
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
    mount();
    const user = userEvent.setup();
    await screen.findByText("Module One");
    await user.click(screen.getByRole("button", { name: /Container/ }));
    await user.click(await screen.findByRole("option", { name: "Container A" }));
    await user.click(await screen.findByRole("button", { name: "Select modules on this page" }));
    expect(screen.getByRole("checkbox", { name: "Select Module One" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select Miner One" })).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "Assign selected" }));
    expect(within(screen.getByRole("dialog")).getByText("0 miners · 1 module")).toBeInTheDocument();
    expect(within(screen.getByRole("dialog")).getByText(/only the modules selected on this page/)).toBeInTheDocument();
  });
  it("clears stale selections and requires another review after an ownership conflict", async () => {
    const assign = vi.fn().mockRejectedValue(new ConnectError("Assignment changed", Code.Aborted));
    const { client } = mount(31n, [miner], assign);
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
    const { client } = mount(31n, [retired]);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select Unavailable module 8" }));
    expect(screen.getByRole("button", { name: "Assign selected" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Recall selected" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "Recall assets" })).getByRole("button", { name: "Recall 1 asset" }),
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
