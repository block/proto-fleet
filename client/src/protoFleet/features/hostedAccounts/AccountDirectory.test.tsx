import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import AccountDirectory from "./AccountDirectory";
import AccountWorkspace from "./AccountWorkspace";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountRole, AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const rootId = "99999999-9999-4999-8999-999999999999";
const clientId = "11111111-1111-4111-8111-111111111111";
const newClient = create(AccountSchema, {
  accountId: "22222222-2222-4222-8222-222222222222",
  parentAccountId: rootId,
  name: "New client",
  depth: 1,
  role: AccountRole.UNSPECIFIED,
  capabilityCeiling: 31n,
  effectiveCapabilities: 31n,
});
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
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function mount(owner = true, entry = "/accounts", managedClient = client) {
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
          ? { accounts: [toJson(AccountSchema, managedClient), ...(created ? [toJson(AccountSchema, newClient)] : [])] }
          : method === "CreateAccount" || method === "GetAccount"
            ? { account: toJson(AccountSchema, body.accountId === clientId ? managedClient : newClient) }
            : method === "ListMembers"
              ? { members: [] }
              : method === "CreateInvitation"
                ? { token: "a".repeat(43) }
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
        <Routes>
          <Route path="/accounts" element={<AccountDirectory />} />
          <Route path="/accounts/:accountId/:section" element={<AccountWorkspace />} />
        </Routes>
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
    const invitation = await screen.findByRole("dialog", { name: "Create invitation" });
    expect(within(invitation).getByText("Invite someone to New client.")).toBeInTheDocument();
    expect(within(invitation).getByRole("button", { name: "Role" })).toHaveTextContent("Account administrator");
    expect(calls).toContainEqual({
      method: "CreateAccount",
      account: rootId,
      body: { accountId: rootId, name: "New client", capabilities: "31" },
    });
    expect(refresh).toHaveBeenCalled();
    await user.click(within(invitation).getByRole("button", { name: "Create invitation" }));
    expect(await screen.findByLabelText("Invitation code")).toHaveValue("a".repeat(43));
    expect(calls).toContainEqual({
      method: "CreateInvitation",
      account: newClient.accountId,
      body: { accountId: newClient.accountId, role: "ACCOUNT_ROLE_ADMINISTRATOR", expiresInSeconds: 86400 },
    });
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("opens a readable client in its workspace and returns through the breadcrumb", async () => {
    const { calls } = mount(true, "/accounts", create(AccountSchema, { ...client, effectiveCapabilities: 31n }));
    const user = userEvent.setup();
    const link = await screen.findByRole("link", { name: "Existing client" });
    expect(link).toHaveAttribute("href", `/accounts/${clientId}/overview`);
    await user.click(link);
    await screen.findByRole("link", { name: "View mining assets" });
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.getByText("Existing client")).toBeInTheDocument();
    expect(calls.some((call) => call.method === "ListAccountAssets")).toBe(false);
    await user.click(breadcrumb.getByRole("link", { name: "Clients" }));
    await screen.findByRole("button", { name: "Create client" });
  });
  it("retains allocation-only inventory access without private workspace authority", async () => {
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
  it("opens invitations from the client's inventory without recreating the client", async () => {
    const { calls } = mount(
      true,
      `/accounts?assets=${clientId}`,
      create(AccountSchema, {
        ...client,
        role: AccountRole.ADMINISTRATOR,
        effectiveCapabilities: 31n,
      }),
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("link", { name: "Invite team member" }));
    const invitation = await screen.findByRole("dialog", { name: "Create invitation" });
    expect(within(invitation).getByText("Invite someone to Existing client.")).toBeInTheDocument();
    expect(within(invitation).getByRole("button", { name: "Role" })).toHaveTextContent("Viewer");
    await user.click(within(invitation).getByRole("button", { name: "Create invitation" }));
    await screen.findByLabelText("Invitation code");
    expect(calls).toContainEqual({
      method: "CreateInvitation",
      account: clientId,
      body: { accountId: clientId, role: "ACCOUNT_ROLE_VIEWER", expiresInSeconds: 86400 },
    });
    expect(calls.some((call) => call.method === "CreateAccount")).toBe(false);
  });
  it.each([
    { mask: 31n, suspended: false, invite: true },
    { mask: 1n, suspended: false, invite: false },
    { mask: 0n, suspended: false, invite: false },
    { mask: 31n, suspended: true, invite: false },
  ])(
    "gates the client invitation menu on live membership authority: $mask, suspended=$suspended",
    async ({ mask, suspended, invite }) => {
      mount(true, "/accounts", create(AccountSchema, { ...client, effectiveCapabilities: mask, suspended }));
      const user = userEvent.setup();
      const row = (await screen.findByRole("link", { name: "Existing client" })).closest("tr")!;
      await user.click(within(row).getByRole("button", { name: "Row actions" }));
      await screen.findByRole("button", { name: "View mining assets" });
      expect(!!screen.queryByRole("button", { name: "Invite team member" })).toBe(invite);
      if (invite) {
        await user.click(screen.getByRole("button", { name: "Invite team member" }));
        expect(await screen.findByRole("dialog", { name: "Create invitation" })).toBeInTheDocument();
      }
    },
  );
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
