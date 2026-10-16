import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { HostedAccountsContext } from "../HostedAccountsContext";
import { FleetAccountAssignment } from "./FleetAccountAssignment";
import {
  AccountAssetSchema,
  AccountSchema,
  ListAccountAssetsResponseSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const rootId = "11111111-1111-4111-8111-111111111111";
const childId = "22222222-2222-4222-8222-222222222222";
const references = [
  { kind: MiningAssetKind.MINER, id: 1n },
  { kind: MiningAssetKind.MODULE, id: 1n },
];
const rows = references.map((ref, index) =>
  create(AccountAssetSchema, { ...ref, revision: BigInt(index + 7), ownerAccountId: rootId, available: true }),
);
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function mount({
  assets = rows,
  children = [{ accountId: childId, name: "Customer", suspended: false }],
  exact = true,
  failure = false,
  mask = 16n,
} = {}) {
  const calls: { method: string; body: Record<string, unknown>; account: string | null }[] = [];
  const onAssigned = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      const method = String(input).split("/").slice(-1)[0];
      const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) : {};
      calls.push({ method, body, account: new Headers(init?.headers).get("X-Fleet-Account-ID") });
      return new Response(
        JSON.stringify(
          method === "ListAccountAssets"
            ? toJson(
                ListAccountAssetsResponseSchema,
                create(ListAccountAssetsResponseSchema, {
                  assets,
                  children,
                  canManageAssignments: true,
                  canViewAssignments: true,
                }),
              )
            : failure
              ? { code: "aborted", message: "changed" }
              : {},
        ),
        {
          status: method === "AssignAccountAssets" && failure ? 409 : 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    }),
  );
  const props = {
    assets: references,
    exactSelection: exact,
    onActionStart: vi.fn(),
    onActionComplete: vi.fn(),
    onAssigned,
  };
  const page = (current = props, capability = mask) => (
    <MemoryRouter>
      <HostedAccountsContext.Provider
        value={{
          accounts: [
            create(AccountSchema, { accountId: rootId, depth: 0, name: "Host", effectiveCapabilities: capability }),
          ],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <FleetAccountAssignment {...current} />
      </HostedAccountsContext.Provider>
    </MemoryRouter>
  );
  const view = render(page());
  return {
    calls,
    onAssigned,
    rerender: (assets = references, capability = mask) => view.rerender(page({ ...props, assets }, capability)),
  };
}
const chooseCustomer = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(await screen.findByRole("button", { name: "Assign to client" }));
  await user.click(await screen.findByRole("option", { name: "Customer" }));
};
describe("Fleet account assignment", () => {
  it("looks up exact typed leaves, freezes selection, and submits reviewed revisions under host authority", async () => {
    const { calls, rerender, onAssigned } = mount();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Assign to account" }));
    await chooseCustomer(user);
    rerender([{ kind: MiningAssetKind.MINER, id: 99n }]);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 2 assets" }));
    await waitFor(() => expect(onAssigned).toHaveBeenCalledOnce());
    expect(calls.find((call) => call.method === "ListAccountAssets")).toEqual({
      method: "ListAccountAssets",
      account: rootId,
      body: {
        accountId: rootId,
        selectedAssets: [
          { kind: "MINING_ASSET_KIND_MINER", id: "1" },
          { kind: "MINING_ASSET_KIND_MODULE", id: "1" },
        ],
      },
    });
    expect(calls.find((call) => call.method === "AssignAccountAssets")).toEqual({
      method: "AssignAccountAssets",
      account: rootId,
      body: {
        accountId: rootId,
        childAccountId: childId,
        intent: "ASSET_ASSIGNMENT_INTENT_ASSIGN",
        assets: [
          { kind: "MINING_ASSET_KIND_MINER", id: "1", expectedRevision: "7" },
          { kind: "MINING_ASSET_KIND_MODULE", id: "1", expectedRevision: "8" },
        ],
      },
    });
  });
  it.each(["missing", "assigned", "unavailable", "policy"])(
    "blocks the entire selection when a leaf is %s",
    async (reason) => {
      const assets =
        reason === "missing"
          ? rows.slice(0, 1)
          : [
              rows[0],
              create(AccountAssetSchema, {
                ...rows[1],
                ownerAccountId: reason === "assigned" ? childId : rootId,
                available: reason !== "unavailable",
                outsidePolicy: reason === "policy",
              }),
            ];
      const { calls } = mount({ assets });
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Assign to account" }));
      await screen.findByText(
        reason === "missing" ? /no longer available or accessible/ : /Only available, unassigned assets/,
      );
      expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 2 assets" })).toBeDisabled();
      expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
    },
  );
  it("requires a fresh selection after an assignment conflict", async () => {
    const { calls } = mount({ failure: true });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Assign to account" }));
    await chooseCustomer(user);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 2 assets" }));
    await screen.findByText(/Assignments or equipment changed/);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Assign 2 assets" })).toBeDisabled();
    expect(calls.filter((call) => call.method === "AssignAccountAssets")).toHaveLength(1);
  });
  it("never expands a global selection into a partial assignment", () => {
    const { calls } = mount({ exact: false });
    expect(screen.getByRole("button", { name: "Assign to account" })).toBeDisabled();
    expect(calls).toHaveLength(0);
  });
  it("closes the confirmation when host delegation is revoked", async () => {
    const { rerender } = mount();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Assign to account" }));
    await chooseCustomer(user);
    rerender(references, 1n);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Assign to account" })).not.toBeInTheDocument();
    rerender(references, 16n);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
