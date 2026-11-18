import { MemoryRouter, Route, Routes } from "react-router-dom";
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
  failureCode = "aborted",
  failureMessage = "changed",
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
              ? { code: failureCode, message: failureMessage }
              : {},
        ),
        {
          status: method === "AssignAccountAssets" && failure ? (failureCode === "invalid_argument" ? 400 : 409) : 200,
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
    <MemoryRouter initialEntries={["/fleet/miners"]}>
      <HostedAccountsContext.Provider
        value={{
          reportingAccess: false,
          accounts: [
            create(AccountSchema, { accountId: rootId, depth: 0, name: "Host", effectiveCapabilities: capability }),
          ],
          hostAccess: true,
          loading: false,
          error: null,
          refresh: vi.fn(),
        }}
      >
        <Routes>
          <Route path="/fleet/miners" element={<FleetAccountAssignment {...current} />} />
        </Routes>
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
const openAllocation = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole("button", { name: "More asset actions" }));
  await user.click(screen.getByRole("button", { name: "Allocate to client" }));
};
const chooseCustomer = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(await screen.findByRole("button", { name: "Allocate to client" }));
  await user.click(await screen.findByRole("option", { name: "Customer" }));
};
describe("Fleet account assignment", () => {
  it("looks up exact typed leaves, freezes selection, and submits reviewed revisions under host authority", async () => {
    const { calls, rerender, onAssigned } = mount();
    const user = userEvent.setup();
    await openAllocation(user);
    await chooseCustomer(user);
    rerender([{ kind: MiningAssetKind.MINER, id: 99n }]);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" }));
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
      await openAllocation(user);
      await screen.findByText(
        reason === "missing" ? /no longer available or accessible/ : /Only available, unallocated assets/,
      );
      expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" })).toBeDisabled();
      expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
    },
  );
  it("requires a fresh selection after an assignment conflict", async () => {
    const { calls } = mount({ failure: true });
    const user = userEvent.setup();
    await openAllocation(user);
    await chooseCustomer(user);
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" }));
    await screen.findByText(/Allocations or equipment changed/);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" })).toBeDisabled();
    expect(calls.filter((call) => call.method === "AssignAccountAssets")).toHaveLength(1);
  });
  it("keeps the reviewed assets available to retry after a destination policy error", async () => {
    const policyError = "selected assets are outside the destination account's site policy";
    const { calls, onAssigned } = mount({
      failure: true,
      failureCode: "invalid_argument",
      failureMessage: policyError,
    });
    const user = userEvent.setup();
    await openAllocation(user);
    await chooseCustomer(user);
    const dialog = screen.getByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Allocate 2 assets" });
    await user.click(confirm);
    expect(await within(dialog).findByText(policyError)).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    expect(screen.queryByText(/Allocations or equipment changed/)).not.toBeInTheDocument();
    expect(calls.filter((call) => call.method === "ListAccountAssets")).toHaveLength(1);
    await user.click(confirm);
    await waitFor(() => expect(calls.filter((call) => call.method === "AssignAccountAssets")).toHaveLength(2));
    expect(await within(dialog).findByText(policyError)).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    expect(calls.filter((call) => call.method === "ListAccountAssets")).toHaveLength(1);
    expect(onAssigned).not.toHaveBeenCalled();
  });
  it("never expands a global selection into a partial assignment", async () => {
    const { calls, rerender } = mount({ exact: false });
    rerender([references[1]]);
    await userEvent.setup().click(screen.getByRole("button", { name: "More asset actions" }));
    expect(screen.getByRole("button", { name: /^Allocate to client/ })).toBeDisabled();
    expect(screen.getByText("Select up to 500 specific assets on this page to allocate them.")).toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });
  it("explains the allocation limit for a module-only selection", async () => {
    const { calls, rerender } = mount();
    rerender(Array.from({ length: 501 }, (_, index) => ({ kind: MiningAssetKind.MODULE, id: BigInt(index + 1) })));
    await userEvent.setup().click(screen.getByRole("button", { name: "More asset actions" }));
    expect(screen.getByRole("button", { name: /^Allocate to client/ })).toBeDisabled();
    expect(screen.getByText("Select up to 500 specific assets on this page to allocate them.")).toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });
  it("closes the confirmation when host delegation is revoked", async () => {
    const { rerender } = mount();
    const user = userEvent.setup();
    await openAllocation(user);
    await chooseCustomer(user);
    rerender(references, 1n);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Allocate to client" })).not.toBeInTheDocument();
    rerender(references, 16n);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("keeps standalone allocation in Fleet after success", async () => {
    const { calls, onAssigned } = mount();
    const user = userEvent.setup();
    await openAllocation(user);
    await chooseCustomer(user);
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Allocate 2 assets" }));
    await waitFor(() => expect(onAssigned).toHaveBeenCalledOnce());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "More asset actions" })).toBeInTheDocument();
    expect(calls.find((call) => call.method === "AssignAccountAssets")?.body.childAccountId).toBe(childId);
  });
  it("does not allocate to a suspended client", async () => {
    const { calls } = mount({
      children: [{ accountId: childId, name: "Customer", suspended: true }],
    });
    const user = userEvent.setup();
    await openAllocation(user);
    await screen.findByText(/No active clients/);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 2 assets" })).toBeDisabled();
    expect(calls.some((call) => call.method === "AssignAccountAssets")).toBe(false);
  });
});
