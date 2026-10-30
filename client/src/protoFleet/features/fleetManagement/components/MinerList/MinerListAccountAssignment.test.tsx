import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import type { BulkAction } from "../BulkActions/types";
import MinerList from "./MinerList";
import {
  EntityKind,
  type InventoryItem,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  AccountAssetSchema,
  AccountSchema,
  ListAccountAssetsResponseSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { HostedAccountsContext } from "@/protoFleet/features/hostedAccounts/HostedAccountsContext";
import { useFleetStore } from "@/protoFleet/store";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu", () => ({
  default: ({ additionalActions = [] }: { additionalActions?: BulkAction<string>[] }) => (
    <RowActionsMenu
      ariaLabel="More asset actions"
      triggerLabel="More"
      actions={additionalActions.map((action) => ({
        label: action.title,
        disabled: action.disabled,
        onClick: action.actionHandler,
      }))}
    />
  ),
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => null,
}));
vi.mock("@/protoFleet/features/containers/MiningActionsMenu", () => ({ default: () => null }));

const rootId = "11111111-1111-4111-8111-111111111111";
const clientId = "22222222-2222-4222-8222-222222222222";
const miner = create(MinerStateSnapshotSchema, {
  deviceIdentifier: "module:1",
  name: "Conventional miner",
  pairingStatus: PairingStatus.PAIRED,
  deviceStatus: DeviceStatus.ONLINE,
});
const minerItem = create(InventoryItemSchema, {
  ref: { kind: EntityKind.MINER, id: 1n },
  deviceIdentifier: miner.deviceIdentifier,
  details: { case: "miner", value: miner },
});
const moduleItem = create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 1n }, name: "Immersion module" });

beforeEach(() => {
  localStorage.clear();
  useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "host-assignment-test", permissions: [] } }));
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount(items: InventoryItem[], totalMiners = 1, initialEntry = "/fleet/miners") {
  const assignments: { account: string | null; body: unknown }[] = [];
  let assigned = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      const method = String(input).split("/").slice(-1)[0];
      const body = JSON.parse(new TextDecoder().decode(init?.body as Uint8Array));
      if (method === "AssignAccountAssets") {
        assignments.push({ account: new Headers(init?.headers).get("X-Fleet-Account-ID"), body });
        assigned = true;
        return Response.json({});
      }
      if (method !== "ListAccountAssets") throw new Error(`Unexpected RPC: ${method}`);
      return Response.json(
        toJson(
          ListAccountAssetsResponseSchema,
          create(ListAccountAssetsResponseSchema, {
            assets: [MiningAssetKind.MINER, MiningAssetKind.MODULE].map((kind, index) =>
              create(AccountAssetSchema, {
                kind,
                id: 1n,
                revision: BigInt(index + 7),
                ownerAccountId: assigned ? clientId : rootId,
                ownerAccountName: assigned ? "Northstar" : "Host",
                available: true,
              }),
            ),
            children: [{ accountId: clientId, name: "Northstar" }],
            canManageAssignments: true,
            canViewAssignments: true,
          }),
        ),
      );
    }),
  );
  const refetch = vi.fn();
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <HostedAccountsContext.Provider
        value={{
          accounts: [create(AccountSchema, { accountId: rootId, depth: 0, name: "Host", effectiveCapabilities: 31n })],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <MinerList
          minerIds={[miner.deviceIdentifier]}
          miners={{ [miner.deviceIdentifier]: miner }}
          inventoryItems={items}
          totalMiners={totalMiners}
          inventoryMinerCount={totalMiners}
          errorsByDevice={{}}
          errorsLoaded
          getActiveBatches={() => []}
          onAddMiners={vi.fn()}
          onRefetchMiners={refetch}
        />
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  return { assignments, refetch };
}

describe("Fleet list account assignment integration", () => {
  it("assigns distinct typed miner/module IDs and refreshes owner cells while clearing selection", async () => {
    const { assignments, refetch } = mount([moduleItem, minerItem], 1, `/fleet/miners?allocateTo=${clientId}`);
    const user = userEvent.setup();
    const moduleCheckbox = await screen.findByRole("checkbox", { name: "Select module:1" });
    expect(screen.queryByRole("link", { name: "Return to client" })).not.toBeInTheDocument();
    const minerCheckbox = screen.getByRole("checkbox", { name: "Select miner:module:1" });
    await user.click(moduleCheckbox);
    await user.click(minerCheckbox);
    await user.click(screen.getByRole("button", { name: "More asset actions" }));
    await user.click(screen.getByRole("button", { name: "Allocate to client" }));
    await user.click(await screen.findByRole("button", { name: "Allocate to client" }));
    await user.click(await screen.findByRole("option", { name: "Northstar" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" }));
    await waitFor(() => expect(refetch).toHaveBeenCalledOnce());
    expect(assignments).toEqual([
      {
        account: rootId,
        body: {
          accountId: rootId,
          childAccountId: clientId,
          intent: "ASSET_ASSIGNMENT_INTENT_ASSIGN",
          assets: [
            { kind: "MINING_ASSET_KIND_MODULE", id: "1", expectedRevision: "8" },
            { kind: "MINING_ASSET_KIND_MINER", id: "1", expectedRevision: "7" },
          ],
        },
      },
    ]);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(moduleCheckbox).not.toBeChecked();
    expect(minerCheckbox).not.toBeChecked();
    expect(screen.queryByRole("button", { name: "Allocate to client" })).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByRole("link", { name: "Northstar" })).toHaveLength(2));
    for (const checkbox of [moduleCheckbox, minerCheckbox])
      expect(within(checkbox.closest("tr")!).getByRole("link", { name: "Northstar" })).toHaveAttribute(
        "href",
        `/accounts/${clientId}/manage/assets`,
      );
  });

  it("offers the Allocated to column toggle to hosted roots", async () => {
    mount([minerItem]);
    const user = userEvent.setup();
    await screen.findByRole("checkbox", { name: "Select miner:module:1" });
    await user.click(screen.getByRole("button", { name: "Manage columns" }));
    expect(screen.getByRole("checkbox", { name: "Toggle Allocated to column" })).toBeInTheDocument();
  });
  it("keeps assignment disabled for global selection even when visible miners have typed references", async () => {
    const { assignments } = mount([minerItem], 200);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select miner:module:1" }));
    await user.click(screen.getByRole("button", { name: "More asset actions" }));
    expect(screen.getByRole("button", { name: "Allocate to client" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Select all" }));
    expect(screen.getByText("All 200 miners selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "More asset actions" }));
    expect(screen.getByRole("button", { name: "Allocate to client" })).toBeDisabled();
    expect(assignments).toHaveLength(0);
  });

  it("does not assign a visible miner that has no typed inventory reference", async () => {
    const { assignments } = mount([create(InventoryItemSchema, { ...minerItem, ref: undefined })]);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("checkbox", { name: "Select miner:module:1" }));
    await user.click(screen.getByRole("button", { name: "More asset actions" }));
    const assign = screen.getByRole("button", { name: "Allocate to client" });
    expect(assign).toBeDisabled();
    await user.click(assign);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(assignments).toHaveLength(0);
  });
});
