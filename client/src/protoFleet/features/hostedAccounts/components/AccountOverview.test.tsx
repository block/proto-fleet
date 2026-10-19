import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";

import { AccountScopeContext } from "../AccountScope";
import { createAccountConnection } from "../api";
import { HostedAccountsContext } from "../HostedAccountsContext";
import AccountOverview from "./AccountOverview";
import {
  AccountSchema,
  GetAccountMiningSummaryResponseSchema,
  MiningSummarySchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const accountId = "11111111-1111-4111-8111-111111111111";
const childId = "22222222-2222-4222-8222-222222222222";
const total = create(MiningSummarySchema, {
  assetCount: 4n,
  minerCount: 2n,
  moduleCount: 2n,
  hashrateHs: 200e12,
  powerW: 6000,
  efficiencyJTh: 30,
  hashrateReportingCount: 3n,
  powerReportingCount: 2n,
  pairedReportingCount: 2n,
  partialModuleCount: 1n,
  unknownModuleCount: 1n,
  hashingCount: 2n,
  offlineCount: 1n,
  unknownCount: 1n,
  outsidePolicyCount: 1n,
  unavailableCount: 1n,
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount({
  mask = 31n,
  childAccess = false,
  reply = create(GetAccountMiningSummaryResponseSchema, {
    totals: total,
    direct: { assetCount: 1n, minerCount: 1n, hashrateHs: 0, hashrateReportingCount: 1n },
    children: [
      { accountId: childId, name: "Subclient A", suspended: true, summary: { assetCount: 3n, moduleCount: 2n } },
    ],
    generatedAt: { seconds: 1792065600n },
  }),
} = {}) {
  const calls: { method: string; account: string | null }[] = [];
  const fetchMock = vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    calls.push({
      method: String(input).split("/").slice(-1)[0],
      account: new Headers(init?.headers).get("X-Fleet-Account-ID"),
    });
    return new Response(JSON.stringify(toJson(GetAccountMiningSummaryResponseSchema, reply)), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  const connection = createAccountConnection(accountId);
  const account = create(AccountSchema, { accountId, name: "Customer A", depth: 1, effectiveCapabilities: mask });
  const invalidate = vi.fn();
  const view = render(
    <MemoryRouter>
      <HostedAccountsContext.Provider
        value={{
          accounts: childAccess
            ? [create(AccountSchema, { accountId: childId, depth: 2, effectiveCapabilities: 1n })]
            : [],
          loading: false,
          error: null,
          hostAccess: false,
          refresh: vi.fn(),
        }}
      >
        <AccountScopeContext.Provider
          value={{
            account,
            client: connection.client,
            signal: connection.signal,
            refreshAccount: vi.fn(),
            invalidateAccount: invalidate,
          }}
        >
          <AccountOverview />
        </AccountScopeContext.Provider>
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  return { ...view, calls, fetchMock, invalidate };
}

describe("account mining overview", () => {
  it("shows server totals, per-metric coverage and current state without inventing complete module measurements", async () => {
    const { calls } = mount();
    await screen.findByText("200.0 TH/s");
    expect(screen.getByText("3 of 4 assets reporting")).toBeInTheDocument();
    expect(screen.getAllByText("2 of 4 assets reporting")).toHaveLength(2);
    expect(screen.getByText(/1 module.*partial.*1 module.*unknown/i)).toBeInTheDocument();
    expect(screen.getByText(/1 unavailable.*1 outside.*policy/i)).toBeInTheDocument();
    expect(calls).toEqual([{ method: "GetAccountMiningSummary", account: accountId }]);
    expect(screen.getByRole("link", { name: "View mining assets" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/assets`,
    );
  });
  it("keeps estate totals above the direct and subclient comparison without another tab row", async () => {
    mount();
    await screen.findByText("200.0 TH/s");
    expect(screen.queryByRole("navigation", { name: "Mining summary scope" })).not.toBeInTheDocument();
    expect(within(screen.getByTestId("stats-container")).getByText("200.0 TH/s")).toBeInTheDocument();
    const direct = screen.getByText("Directly assigned").closest("tr")!;
    expect(within(direct).getByText("0 TH/s")).toBeInTheDocument();
    expect(within(direct).getByText("1 of 1 asset reporting")).toBeInTheDocument();
    expect(screen.getByText("Subclient A")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Subclient A" })).not.toBeInTheDocument();
  });
  it("retains suspended children in reporting without a private workspace link", async () => {
    mount({ childAccess: true });
    // Suspended children remain in reporting but cannot open a workspace.
    await screen.findByText("Subclient A");
    expect(screen.queryByRole("link", { name: "Subclient A" })).not.toBeInTheDocument();
  });
  it.each([false, true])("links an active named child only with directory View access: %s", async (childAccess) => {
    mount({
      childAccess,
      reply: create(GetAccountMiningSummaryResponseSchema, {
        totals: total,
        direct: {},
        children: [{ accountId: childId, name: "Subclient A", summary: {} }],
      }),
    });
    await screen.findByText("Subclient A");
    if (childAccess)
      expect(screen.getByRole("link", { name: "Subclient A" })).toHaveAttribute(
        "href",
        `/accounts/${childId}/overview`,
      );
    else expect(screen.queryByRole("link", { name: "Subclient A" })).not.toBeInTheDocument();
  });
  it("omits the comparison when the server supplies only estate totals", async () => {
    mount({ reply: create(GetAccountMiningSummaryResponseSchema, { totals: total }) });
    await screen.findByText("200.0 TH/s");
    expect(screen.queryByRole("region", { name: "Subclient comparison" })).not.toBeInTheDocument();
  });
  it("keeps viewer totals private from child partitions even if a malformed response includes them", async () => {
    mount({ mask: 1n });
    await screen.findByText("200.0 TH/s");
    expect(screen.queryByText("Subclient A")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Subclient comparison" })).not.toBeInTheDocument();
  });
  it.each([9n, 17n])("shows the server partition for either management capability: %s", async (mask) => {
    mount({ mask });
    expect(await screen.findByRole("region", { name: "Subclient comparison" })).toBeInTheDocument();
    expect(screen.getByText("Directly assigned")).toBeInTheDocument();
    expect(screen.getByText("Subclient A")).toBeInTheDocument();
  });
  it("distinguishes an empty estate from unavailable measurements", async () => {
    mount({ reply: create(GetAccountMiningSummaryResponseSchema, { totals: {} }) });
    await screen.findByText("No mining assets assigned");
    expect(screen.queryByText("0 TH/s")).not.toBeInTheDocument();
    expect(within(screen.getByTestId("stats-container")).getAllByText("—")).toHaveLength(3);
  });
  it("retains assigned counts when no assets report measurements", async () => {
    mount({
      reply: create(GetAccountMiningSummaryResponseSchema, {
        totals: { assetCount: 2n, minerCount: 2n, unknownCount: 2n },
      }),
    });
    await screen.findByText("2 miners · 0 modules");
    expect(screen.queryByText("No mining assets assigned")).not.toBeInTheDocument();
    expect(within(screen.getByTestId("stats-container")).getAllByText("—")).toHaveLength(3);
    expect(within(screen.getByTestId("stats-container")).getAllByText("0 of 2 assets reporting")).toHaveLength(3);
  });
  it("clears readings on refresh and invalidates the account when access is denied", async () => {
    const { fetchMock, invalidate } = mount();
    await screen.findByText("200.0 TH/s");
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ code: "permission_denied", message: "Access revoked" }), {
          status: 403,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    act(() => window.dispatchEvent(new Event("focus")));
    expect(screen.queryByText("200.0 TH/s")).not.toBeInTheDocument();
    await screen.findByText(/Access revoked/);
    await waitFor(() => expect(invalidate).toHaveBeenCalled());
  });
});
