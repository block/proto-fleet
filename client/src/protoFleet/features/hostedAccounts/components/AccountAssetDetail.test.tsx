import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import AccountWorkspace from "../AccountWorkspace";
import { HostedAccountsContext } from "../HostedAccountsContext";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("account mining asset details", () => {
  it("requests a typed current detail in the selected workspace and clears it after recall", async () => {
    let recalled = false;
    let deferDetail = false;
    let finishDetail!: () => void;
    const polling = vi.spyOn(window, "setInterval");
    const calls: { method: string; account: string | null; body: Record<string, unknown> }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
        const method = String(input).split("/").slice(-1)[0];
        const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) : {};
        calls.push({ method, account: new Headers(init?.headers).get("X-Fleet-Account-ID"), body });
        if (method === "GetAccountAssetDetail" && recalled)
          return new Response(JSON.stringify({ code: "not_found", message: "Asset is no longer assigned" }), {
            status: 404,
            headers: { "Content-Type": "application/json" },
          });
        if (method === "GetAccountAssetDetail" && deferDetail) {
          deferDetail = false;
          await new Promise<void>((resolve) => {
            finishDetail = resolve;
          });
        }
        const data =
          method === "GetAccount"
            ? { account: { accountId: "client", name: "Customer", depth: 1, effectiveCapabilities: "1" } }
            : method === "GetAccountAssetDetail"
              ? {
                  asset: {
                    kind: "MINING_ASSET_KIND_MODULE",
                    id: "7",
                    name: "Allocated module",
                    available: true,
                    hashrateHs: 1000000000000,
                  },
                }
              : {};
        return new Response(JSON.stringify(data), { headers: { "Content-Type": "application/json" } });
      }),
    );
    render(
      <MemoryRouter initialEntries={["/accounts/client/assets/module/7"]}>
        <HostedAccountsContext.Provider
          value={{ accounts: [], hostAccess: false, loading: false, error: null, refresh: vi.fn() }}
        >
          <Routes>
            <Route path="/accounts/:accountId/assets/:kind/:assetId" element={<AccountWorkspace />} />
          </Routes>
        </HostedAccountsContext.Provider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Current allocated mining asset")).toBeInTheDocument();
    expect(screen.getAllByText("Allocated module").length).toBeGreaterThan(0);
    expect(calls.find((call) => call.method === "GetAccountAssetDetail")).toMatchObject({
      account: "client",
      body: { accountId: "client", asset: { kind: "MINING_ASSET_KIND_MODULE", id: "7" } },
    });
    for (const link of screen.getAllByRole("link", { name: "Mining assets" }))
      expect(link).toHaveAttribute("href", "/accounts/client/assets");
    deferDetail = true;
    const poll = polling.mock.calls.filter(([, interval]) => interval === POLL_INTERVAL_MS).slice(-1)[0]?.[0];
    expect(poll).toBeTypeOf("function");
    act(() => {
      if (typeof poll === "function") poll();
    });
    await waitFor(() => expect(finishDetail).toBeTypeOf("function"));
    expect(screen.getByText("Current allocated mining asset")).toBeInTheDocument();
    expect(screen.getAllByText("Allocated module").length).toBeGreaterThan(0);
    await act(async () => finishDetail());
    recalled = true;
    act(() => window.dispatchEvent(new Event("focus")));
    await screen.findByText(/Asset is no longer assigned/);
    await waitFor(() => expect(screen.queryByText("Allocated module")).not.toBeInTheDocument());
    expect(calls.filter((call) => call.method !== "enabled" && !call.method.startsWith("Get"))).toEqual([]);
  });
});
