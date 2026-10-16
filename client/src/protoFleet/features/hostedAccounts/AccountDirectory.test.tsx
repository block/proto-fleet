import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import AccountDirectory from "./AccountDirectory";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const rootId = "99999999-9999-4999-8999-999999999999";
const clientId = "11111111-1111-4111-8111-111111111111";
const root = create(AccountSchema, {
  accountId: rootId,
  name: "Host organization",
  depth: 0,
  effectiveCapabilities: 16n,
  capabilityCeiling: 31n,
});
const client = create(AccountSchema, {
  accountId: clientId,
  parentAccountId: rootId,
  name: "Existing client",
  depth: 1,
  capabilityCeiling: 31n,
  managementSummary: { minerCount: 3n, moduleCount: 12n, containerCount: 2n, loginCount: 4n },
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function mount(owner = true, entry = "/accounts") {
  const calls: { method: string; body: Record<string, unknown>; account: string | null }[] = [];
  const refresh = vi.fn();
  let created = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      const method = String(input).split("/").slice(-1)[0];
      const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) : {};
      calls.push({ method, body, account: new Headers(init?.headers).get("X-Fleet-Account-ID") });
      if (method === "CreateAccount") created = true;
      const reply =
        method === "ListChildren"
          ? {
              accounts: [
                toJson(AccountSchema, client),
                ...(created
                  ? [
                      toJson(
                        AccountSchema,
                        create(AccountSchema, {
                          depth: 1,
                          parentAccountId: rootId,
                          capabilityCeiling: 31n,
                          accountId: "22222222-2222-4222-8222-222222222222",
                          name: "New client",
                          managementSummary: { minerCount: 0n, moduleCount: 0n, containerCount: 0n, loginCount: 1n },
                        }),
                      ),
                    ]
                  : []),
              ],
            }
          : method === "ListAccountAssets"
            ? {
                assets: [],
                canManageAssignments: true,
                canViewAssignments: true,
                children: [{ accountId: clientId, name: client.name }],
              }
            : {};
      return new Response(JSON.stringify(reply), { headers: { "Content-Type": "application/json" } });
    }),
  );
  const page = (managed = owner) => (
    <MemoryRouter initialEntries={[entry]}>
      <HostedAccountsContext.Provider
        value={{ accounts: managed ? [root] : [client], hostAccess: managed, loading: false, error: null, refresh }}
      >
        <AccountDirectory />
      </HostedAccountsContext.Provider>
    </MemoryRouter>
  );
  const view = render(page());
  return { calls, refresh, rerenderOwner: (managed: boolean) => view.rerender(page(managed)) };
}
describe("owner Clients directory", () => {
  it("lists managed clients directly and creates a client using host authority", async () => {
    const { calls, refresh } = mount();
    const user = userEvent.setup();
    await screen.findByRole("link", { name: "Existing client" });
    expect(screen.getByRole("columnheader", { name: "Client" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Assigned assets" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Logins" })).toBeInTheDocument();
    expect(screen.getByText("3 miners · 12 modules")).toBeInTheDocument();
    expect(screen.getByText("Modules in 2 containers")).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "4" })).toBeInTheDocument();
    expect(screen.queryByText("Host organization")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Join an account" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Switch account" })).not.toBeInTheDocument();
    expect(
      within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "Clients" }),
    ).toHaveAttribute("aria-current", "page");
    await user.click(screen.getByRole("button", { name: "Create client" }));
    const dialog = screen.getByRole("dialog", { name: "Create client" });
    expect(within(dialog).getByRole("button", { name: "Create client" })).toBeDisabled();
    await user.type(within(dialog).getByLabelText("Client name"), "New client");
    await user.click(within(dialog).getByRole("button", { name: "Create client" }));
    const newClient = await screen.findByRole("link", { name: "New client" });
    const row = newClient.closest("tr")!;
    expect(within(row).getByText("No assets assigned")).toBeInTheDocument();
    expect(within(row).getByRole("cell", { name: "1" })).toBeInTheDocument();
    expect(calls).toContainEqual({
      method: "CreateAccount",
      account: rootId,
      body: { accountId: rootId, name: "New client", capabilities: "31" },
    });
    expect(calls.some((call) => call.method === "GetAccount")).toBe(false);
    expect(refresh).toHaveBeenCalled();
  });
  it("opens client inventory in the same directory and returns to client creation", async () => {
    const { calls } = mount();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("link", { name: "Existing client" }));
    await screen.findByRole("link", { name: "Assign from Fleet" });
    expect(calls).toContainEqual(
      expect.objectContaining({
        method: "ListAccountAssets",
        account: rootId,
        body: expect.objectContaining({ accountId: rootId, ownerAccountId: clientId }),
      }),
    );
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.getByText("Existing client")).toBeInTheDocument();
    expect(breadcrumb.getByRole("link", { name: "Clients" })).toHaveAttribute("href", "/accounts");
    let finishRefresh!: () => void;
    const originalFetch = vi.mocked(fetch).getMockImplementation()!;
    vi.mocked(fetch).mockImplementationOnce(
      (input, init) =>
        new Promise((resolve) => {
          finishRefresh = () => resolve(originalFetch(input, init));
        }),
    );
    await user.click(breadcrumb.getByRole("link", { name: "Clients" }));
    expect(screen.getByRole("link", { name: "Existing client" })).toBeInTheDocument();
    await act(async () => finishRefresh());
    await screen.findByRole("link", { name: "Existing client" });
    expect(calls.filter((call) => call.method === "ListChildren")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Create client" })).toBeInTheDocument();
  });
  it("preserves the membership directory for users without host creation authority", async () => {
    const { calls } = mount(false);
    expect(screen.getByRole("link", { name: "Join an account" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create client" })).not.toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });
  it("discards an open creation form when host authority is removed", async () => {
    const { rerenderOwner } = mount();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Create client" }));
    expect(screen.getByRole("dialog", { name: "Create client" })).toBeInTheDocument();
    rerenderOwner(false);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    rerenderOwner(true);
    await waitFor(() => expect(screen.getByRole("button", { name: "Create client" })).toBeInTheDocument());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
