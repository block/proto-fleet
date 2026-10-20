import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import AccountDirectory from "./AccountDirectory";
import AccountWorkspace from "./AccountWorkspace";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountRole, AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";

const rootId = "99999999-9999-4999-8999-999999999999";
const accountId = "11111111-1111-4111-8111-111111111111";
const initialAuth = useFleetStore.getState().auth;
const initialUI = useFleetStore.getState().ui;
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth, ui: initialUI });
});
function mount(
  account = create(AccountSchema, {
    accountId: rootId,
    name: "Host",
    depth: 0,
    effectiveCapabilities: 16n,
    capabilityCeiling: 31n,
  }),
  section = "children",
  hostAccess = account.depth === 0,
  accounts = [account],
) {
  const refresh = vi.fn();
  render(
    <MemoryRouter initialEntries={[`/accounts/${account.accountId}/${section}`]}>
      <HostedAccountsContext.Provider value={{ accounts, hostAccess, loading: false, error: null, refresh }}>
        <Routes>
          <Route path="/accounts/:accountId/:section?" element={<AccountWorkspace />} />
          <Route path="/accounts/:accountId/manage/:section?" element={<AccountWorkspace management />} />
          <Route path="/accounts" element={<AccountDirectory />} />
        </Routes>
      </HostedAccountsContext.Provider>
    </MemoryRouter>,
  );
  return refresh;
}
const fetchRpc = (reply: (method: string, body: Record<string, unknown>) => unknown) => {
  const calls: { method: string; body: Record<string, unknown>; account: string | null }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
      const method = String(input).split("/").slice(-1)[0];
      const body = init?.body ? JSON.parse(new TextDecoder().decode(init.body as Uint8Array)) : {};
      calls.push({ method, body, account: new Headers(init?.headers).get("X-Fleet-Account-ID") });
      const response = reply(method, body);
      return new Response(JSON.stringify(response), { headers: { "Content-Type": "application/json" } });
    }),
  );
  return calls;
};
describe("real account workspace routing", () => {
  it.each([
    { role: AccountRole.VIEWER, mask: 1n, depth: 1, management: false, children: false },
    { role: AccountRole.POOL_OPERATOR, mask: 7n, depth: 1, management: false, children: false },
    { role: AccountRole.ADMINISTRATOR, mask: 31n, depth: 1, management: true, children: true },
    { role: AccountRole.ADMINISTRATOR, mask: 31n, depth: 2, management: true, children: false },
  ])(
    "opens the mining Overview for role $role at depth $depth with only permitted tools",
    async ({ role, mask, depth, management, children }) => {
      const account = create(AccountSchema, { accountId, name: "Customer", depth, role, effectiveCapabilities: mask });
      fetchRpc((method) => (method === "GetAccount" ? { account: toJson(AccountSchema, account) } : { totals: {} }));
      mount(account, "");
      const main = within(screen.getByTestId("navigation-menu"));
      await waitFor(() => expect(main.getByRole("link", { name: "Home" })).toHaveAttribute("aria-current", "page"));
      expect(main.getByRole("link", { name: "Fleet" })).toHaveAttribute("href", `/accounts/${accountId}/assets`);
      expect(main.getByRole("link", { name: "Settings" })).toBeInTheDocument();
      expect(main.queryByRole("link", { name: "Pools" })).not.toBeInTheDocument();
      expect(main.queryByRole("link", { name: "Team" })).not.toBeInTheDocument();
      expect(main.queryByRole("link", { name: "Subclients" })).not.toBeInTheDocument();
      await screen.findByText("No mining assets assigned");
      await userEvent.setup().click(main.getByRole("link", { name: "Settings" }));
      const settings = within(await screen.findByRole("navigation", { name: "Settings" }));
      expect(settings.getByRole("link", { name: "Pools" })).toBeInTheDocument();
      expect(settings.getByRole("link", { name: "Access" })).toBeInTheDocument();
      expect(settings.getByRole("link", { name: "Preferences" })).toHaveAttribute(
        "href",
        `/accounts/${accountId}/preferences`,
      );
      expect(!!settings.queryByRole("link", { name: "Team" })).toBe(management);
      expect(!!settings.queryByRole("link", { name: "Subclients" })).toBe(children);
      expect(main.getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
      const user = userEvent.setup();
      await user.click(settings.getByRole("link", { name: "Preferences" }));
      await user.click(await screen.findByRole("button", { name: "Celsius" }));
      await user.click(screen.getByTestId("fahrenheit-option"));
      await user.click(screen.getByRole("button", { name: "Done" }));
      expect(screen.getByRole("button", { name: "Fahrenheit" })).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "System" }));
      await user.click(screen.getByTestId("theme-dark-option"));
      await user.click(screen.getByRole("button", { name: "Done" }));
      expect(screen.getByRole("button", { name: "Dark" })).toBeInTheDocument();
    },
  );
  it("opens host root administration by default without resolving a private customer", async () => {
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") throw new Error("Root is not a private customer account");
      return method === "ListChildren" ? { accounts: [] } : {};
    });
    mount(undefined, "");
    await screen.findByRole("button", { name: "Create client" });
    expect(screen.queryByRole("button", { name: "Switch account" })).not.toBeInTheDocument();
    expect(calls.some((call) => call.method === "GetAccount" || call.method === "GetAccountMiningSummary")).toBe(false);
    const tabs = within(screen.getByRole("navigation", { name: "Account sections" }));
    expect(tabs.getByRole("button", { name: "Clients" })).toHaveAttribute("aria-current", "page");
    expect(tabs.queryByRole("button", { name: "Mining assets" })).not.toBeInTheDocument();
    expect(tabs.queryByRole("button", { name: "Overview" })).not.toBeInTheDocument();
    expect(tabs.getByRole("button", { name: "Settings" })).toBeInTheDocument();
  });
  it("opens customer allocation inventory through host authority without private workspace access", async () => {
    const childId = "22222222-2222-4222-8222-222222222222";
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") throw new Error("Private customer reads are not part of host allocation management");
      if (method === "ListChildren") return { accounts: [{ accountId: childId, name: "Customer", depth: 1 }] };
      return {
        assets: [],
        canManageAssignments: true,
        canViewAssignments: true,
        children: [{ accountId: childId, name: "Customer" }],
      };
    });
    mount(undefined, `children?assets=${childId}`);
    await screen.findByRole("link", { name: "Assign from Fleet" });
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.getByText("Customer")).toBeInTheDocument();
    expect(breadcrumb.getByRole("link", { name: "Clients" })).toHaveAttribute("href", `/accounts/${rootId}/children`);
    expect(calls.find((call) => call.method === "ListAccountAssets")).toEqual(
      expect.objectContaining({
        account: rootId,
        body: expect.objectContaining({ accountId: rootId, ownerAccountId: childId }),
      }),
    );
    expect(calls.some((call) => call.method === "GetAccount")).toBe(false);
    expect(screen.getByRole("link", { name: "Assign from Fleet" })).toHaveAttribute("href", "/fleet/miners");
    expect(screen.queryByRole("button", { name: "Assign selected" })).not.toBeInTheDocument();
  });
  it("clears private assignment filters when live account capabilities are reduced", async () => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
    const childId = "22222222-2222-4222-8222-222222222222";
    let reduced = false;
    const account = create(AccountSchema, {
      accountId,
      name: "Customer",
      depth: 1,
      effectiveCapabilities: 31n,
      capabilityCeiling: 31n,
    });
    const calls = fetchRpc((method) => {
      if (method === "GetAccount")
        return {
          account: toJson(
            AccountSchema,
            create(AccountSchema, { ...account, effectiveCapabilities: reduced ? 1n : 31n }),
          ),
        };
      if (method === "GetAccountMiningSummary")
        return reduced ? { totals: {} } : { totals: {}, direct: {}, children: [{ accountId: childId, name: "East" }] };
      return {
        assets: [],
        canManageAssignments: !reduced,
        canViewAssignments: !reduced,
        children: reduced ? [] : [{ accountId: childId, name: "East" }],
      };
    });
    const user = userEvent.setup();
    mount(account, "assets");
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
    await user.click(await screen.findByRole("button", { name: "Add Filter" }));
    await user.click(screen.getByTestId("nested-dropdown-filter-row-assignment"));
    await user.click(screen.getByTestId(`filter-option-${childId}`));
    fireEvent.mouseDown(screen.getByRole("heading", { name: "Fleet" }));
    await waitFor(() =>
      expect(calls.filter((call) => call.method === "ListAccountAssets").slice(-1)[0]?.body.ownerAccountId).toBe(
        childId,
      ),
    );
    reduced = true;
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(screen.queryByRole("button", { name: /Assigned to/ })).not.toBeInTheDocument());
    await waitFor(() =>
      expect(
        calls.filter((call) => call.method === "ListAccountAssets").slice(-1)[0]?.body.ownerAccountId,
      ).toBeUndefined(),
    );
    expect(screen.queryByText("East")).not.toBeInTheDocument();
  });
  it("contains read-only client navigation in a full-screen preview with a close action", async () => {
    const account = create(AccountSchema, {
      accountId,
      name: "Customer",
      depth: 1,
      role: AccountRole.ADMINISTRATOR,
      effectiveCapabilities: 31n,
      capabilityCeiling: 31n,
    });
    useFleetStore.setState({ auth: { ...initialAuth, permissions: ["miner:read", "fleet:read"] } });
    fetchRpc((method) => (method === "GetAccount" ? { account: toJson(AccountSchema, account) } : {}));
    const user = userEvent.setup();
    mount(account, "access", true);
    await screen.findByRole("button", { name: "Rename account" });
    expect(screen.queryByRole("button", { name: "Switch account" })).not.toBeInTheDocument();
    const preview = screen.getByRole("dialog", { name: "Viewing as Customer" });
    expect(within(preview).getByText("Read-only", { exact: true })).toBeInTheDocument();
    expect(within(preview).getByRole("button", { name: "Close client preview" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Exit to Clients" })).not.toBeInTheDocument();
    expect(within(preview).queryByRole("navigation", { name: "Breadcrumb" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Rename account" })).toBeDisabled();
    expect(screen.queryByText("Actions are unavailable in this read-only preview.")).not.toBeInTheDocument();
    const main = screen.getByTestId("navigation-menu");
    expect(within(main).getByRole("link", { name: "Fleet" })).toHaveAttribute("href", `/accounts/${accountId}/assets`);
    expect(within(main).queryByRole("link", { name: "Clients" })).not.toBeInTheDocument();
    expect(within(main).getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByText("Host administration")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Account sections" })).not.toBeInTheDocument();
    const settings = within(screen.getByRole("navigation", { name: "Settings" }));
    await user.click(settings.getByRole("link", { name: "Pools" }));
    await screen.findByRole("button", { name: "Add pool" });
    expect(within(screen.getByTestId("navigation-menu")).getByRole("link", { name: "Settings" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "Pools" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "Add pool" })).toBeDisabled();
    await user.keyboard("{Escape}");
    await screen.findByRole("link", { name: "Customer" });
    expect(screen.queryByRole("dialog", { name: "Viewing as Customer" })).not.toBeInTheDocument();
  });
  it.each([false, true])(
    "keeps account navigation available with the appropriate switcher (multiple accounts=%s)",
    async (multipleAccounts) => {
      const account = create(AccountSchema, {
        accountId,
        parentAccountId: rootId,
        depth: 1,
        name: "Customer",
        role: AccountRole.ADMINISTRATOR,
        effectiveCapabilities: 31n,
        capabilityCeiling: 31n,
      });
      fetchRpc(() => ({ account: toJson(AccountSchema, account) }));
      const user = userEvent.setup();
      const otherAccount = create(AccountSchema, {
        accountId: "22222222-2222-4222-8222-222222222222",
        depth: 1,
        name: "Other client",
        effectiveCapabilities: 1n,
      });
      mount(account, "access", false, multipleAccounts ? [account, otherAccount] : [account]);
      await screen.findByRole("button", { name: "Rename account" });
      const menu = within(screen.getByRole("navigation", { name: "Main" })).getByRole("list");
      expect(within(menu).getByRole("link", { name: "Settings" })).toHaveAttribute(
        "href",
        `/accounts/${accountId}/settings`,
      );
      expect(within(menu).getByRole("link", { name: "Home" })).toHaveAttribute(
        "href",
        `/accounts/${accountId}/overview`,
      );
      expect(within(menu).getByRole("link", { name: "Fleet" })).toHaveAttribute(
        "href",
        `/accounts/${accountId}/assets`,
      );
      expect(
        within(screen.getByRole("navigation", { name: "Settings" })).getByRole("link", { name: "Access" }),
      ).toHaveAttribute("href", `/accounts/${accountId}/access`);
      if (multipleAccounts) {
        await user.click(screen.getByRole("button", { name: "Switch account" }));
        const dialog = within(screen.getByRole("dialog", { name: "Switch account" }));
        expect(dialog.getByRole("radio", { name: /^Other client/ })).toBeInTheDocument();
        await user.click(dialog.getByRole("link", { name: "All accounts" }));
      } else {
        expect(screen.queryByRole("button", { name: "Switch account" })).not.toBeInTheDocument();
        expect(
          within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByText("Customer"),
        ).toBeInTheDocument();
        expect(within(screen.getByTestId("app-header")).queryByText("Customer")).not.toBeInTheDocument();
        expect(screen.getByTestId("navigation-home-link")).toHaveAttribute("href", `/accounts/${accountId}/overview`);
        await user.click(screen.getByRole("link", { name: "All accounts" }));
      }
      await screen.findByRole("link", { name: "Join an account" });
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    },
  );
  it("keeps host team provisioning writable in Clients management", async () => {
    const account = create(AccountSchema, { accountId, name: "Customer", depth: 1, effectiveCapabilities: 31n });
    const calls = fetchRpc((method) => (method === "GetAccount" ? { account: toJson(AccountSchema, account) } : {}));
    mount(account, "manage/team?invite=administrator", true);
    const user = userEvent.setup();
    const dialog = await screen.findByRole("dialog", { name: "Create invitation" });
    expect(screen.queryByText("Viewing as Customer")).not.toBeInTheDocument();
    const tabs = within(screen.getByRole("navigation", { name: "Account sections" }));
    expect(tabs.getByRole("button", { name: "Client settings" })).toBeInTheDocument();
    expect(tabs.queryByRole("button", { name: "Settings" })).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    await waitFor(() => expect(calls.some((call) => call.method === "CreateInvitation")).toBe(true));
  });
  it("does not open a mutation dialog from an invitation deep link in preview", async () => {
    const account = create(AccountSchema, { accountId, name: "Customer", depth: 1, effectiveCapabilities: 31n });
    fetchRpc((method) => (method === "GetAccount" ? { account: toJson(AccountSchema, account) } : {}));
    mount(account, "team?invite=administrator", true);
    expect(await screen.findByRole("button", { name: "Invite team member" })).toBeDisabled();
    expect(screen.queryByRole("dialog", { name: "Create invitation" })).not.toBeInTheDocument();
  });
  it("keeps readable parents in subclient breadcrumbs and returns to their workspace", async () => {
    const parent = create(AccountSchema, {
      accountId: rootId,
      name: "Parent client",
      depth: 1,
      effectiveCapabilities: 31n,
    });
    const child = create(AccountSchema, {
      accountId,
      parentAccountId: rootId,
      name: "Subclient",
      depth: 2,
      effectiveCapabilities: 1n,
    });
    fetchRpc((method, body) =>
      method === "GetAccount"
        ? { account: toJson(AccountSchema, body.accountId === rootId ? parent : child) }
        : { totals: {} },
    );
    mount(child, "overview", false, [parent, child]);
    const user = userEvent.setup();
    await screen.findByText("No mining assets assigned");
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.queryByRole("link", { name: "Clients" })).not.toBeInTheDocument();
    expect(breadcrumb.getByRole("link", { name: "Parent client" })).toHaveAttribute(
      "href",
      `/accounts/${rootId}/overview`,
    );
    expect(breadcrumb.getByText("Subclient")).toBeInTheDocument();
    await user.click(breadcrumb.getByRole("link", { name: "Parent client" }));
    await waitFor(() =>
      expect(
        within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByText("Parent client"),
      ).toBeInTheDocument(),
    );
    expect(
      within(screen.getByRole("navigation", { name: "Breadcrumb" })).queryByText("Subclient"),
    ).not.toBeInTheDocument();
  });
  it("opens host administration and creates a customer using the root ceiling", async () => {
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") throw new Error("Root is not a private customer account");
      return method === "ListChildren" ? { accounts: [] } : {};
    });
    const user = userEvent.setup();
    mount();
    await user.click(await screen.findByRole("button", { name: "Create client" }));
    const dialog = screen.getByRole("dialog", { name: "Create client" });
    expect(within(dialog).getByRole("checkbox", { name: "View account data" })).toBeDisabled();
    expect(within(dialog).getByRole("checkbox", { name: "Manage memberships" })).toBeDisabled();
    expect(within(dialog).getByRole("checkbox", { name: "Manage memberships" })).toBeChecked();
    await user.type(within(dialog).getByLabelText("Client name"), "Customer A");
    await user.click(within(dialog).getByRole("button", { name: "Create client" }));
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "CreateAccount",
        account: rootId,
        body: { accountId: rootId, name: "Customer A", capabilities: "31" },
      }),
    );
    expect(calls.some((c) => c.method === "GetAccount")).toBe(false);
  });
  it("preserves a one-time key on a transient focus refresh, then clears on denied access", async () => {
    const account = create(AccountSchema, {
      accountId,
      parentAccountId: rootId,
      depth: 1,
      name: "Customer",
      role: AccountRole.ADMINISTRATOR,
      effectiveCapabilities: 31n,
      capabilityCeiling: 31n,
    });
    let failure = "";
    fetchRpc((method) => {
      if (method === "GetAccount") return { account: toJson(AccountSchema, account) };
      return method === "CreateAccountKey" ? { secret: "one-time-key" } : {};
    });
    const originalFetch = globalThis.fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
        if (String(input).endsWith("/GetAccount") && failure)
          return Promise.resolve(
            new Response(JSON.stringify({ code: failure, message: "Refresh failed" }), {
              status: failure === "unavailable" ? 503 : 403,
              headers: { "Content-Type": "application/json" },
            }),
          );
        return originalFetch(input, init);
      }),
    );
    const user = userEvent.setup();
    mount(account, "access");
    await user.click(await screen.findByRole("button", { name: "Create API key" }));
    await user.type(screen.getByLabelText("Key name"), "Reporting");
    await user.click(screen.getByRole("button", { name: "Create key" }));
    expect(await screen.findByLabelText("Secret")).toHaveValue("one-time-key");
    failure = "unavailable";
    act(() => window.dispatchEvent(new Event("focus")));
    await screen.findByText(/Refresh failed/);
    expect(screen.getByLabelText("Secret")).toHaveValue("one-time-key");
    failure = "";
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(screen.queryByText(/Refresh failed/)).not.toBeInTheDocument());
    expect(screen.getByLabelText("Secret")).toHaveValue("one-time-key");
    failure = "permission_denied";
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument());
  });
  it("revalidates effective site permission and clears private state in an active tab", async () => {
    const polling = vi.spyOn(window, "setInterval");
    const account = create(AccountSchema, { accountId, name: "Customer", depth: 1, effectiveCapabilities: 31n });
    let restricted = false;
    fetchRpc((method) => {
      if (method === "GetAccount") return { account: toJson(AccountSchema, account) };
      if (method === "GetSitePolicy")
        return { effectiveRestricted: restricted, effectiveSiteIds: restricted ? ["7"] : [] };
      return method === "CreateAccountKey" ? { secret: "private-key" } : {};
    });
    mount(account, "access");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Create API key" }));
    await user.type(screen.getByLabelText("Key name"), "Reporting");
    await user.click(screen.getByRole("button", { name: "Create key" }));
    expect(await screen.findByLabelText("Secret")).toHaveValue("private-key");
    restricted = true;
    const poll = polling.mock.calls.find(([, interval]) => interval === POLL_INTERVAL_MS)?.[0];
    expect(poll).toBeTypeOf("function");
    await act(async () => {
      if (typeof poll === "function") poll();
    });
    await waitFor(() => expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument());
  });
  it("keeps a failed pool password confirmation open while rechecking live access", async () => {
    const account = create(AccountSchema, {
      accountId,
      parentAccountId: rootId,
      depth: 1,
      name: "Customer",
      role: AccountRole.ADMINISTRATOR,
      effectiveCapabilities: 31n,
      capabilityCeiling: 31n,
    });
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") return { account: toJson(AccountSchema, account) };
      if (method === "ListPoolConfigurations") return { configurations: [{ poolId: "test-pool", name: "Test pool" }] };
      return {};
    });
    const originalFetch = globalThis.fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) =>
        String(input).endsWith("/SetIncomingPoolProfile")
          ? Promise.resolve(
              new Response(JSON.stringify({ code: "permission_denied", message: "Incorrect confirmation password" }), {
                status: 403,
                headers: { "Content-Type": "application/json" },
              }),
            )
          : originalFetch(input, init),
      ),
    );
    const user = userEvent.setup();
    mount(account, "pools");
    await user.click(await screen.findByRole("button", { name: "Change selection" }));
    const dialog = screen.getByRole("dialog", { name: "Stored pool profile" });
    await user.click(within(dialog).getByRole("checkbox", { name: /Clear/ }));
    await user.type(within(dialog).getByLabelText("Your Fleet password"), "incorrect-test-password");
    await user.click(within(dialog).getByRole("button", { name: "Confirm selection" }));
    expect(await screen.findByText(/Incorrect confirmation password/)).toBeInTheDocument();
    await waitFor(() => expect(calls.filter((c) => c.method === "GetAccount").length).toBeGreaterThanOrEqual(2));
    expect(screen.getByRole("dialog", { name: "Stored pool profile" })).toBeInTheDocument();
    expect(screen.getByLabelText("Your Fleet password")).toHaveValue("incorrect-test-password");
  });
  it("discards forms and secrets when the login changes on the same account route", async () => {
    fetchRpc((method) =>
      method === "GetAccount"
        ? {
            account: {
              accountId,
              parentAccountId: rootId,
              depth: 1,
              name: "Customer",
              effectiveCapabilities: "1",
              capabilityCeiling: "31",
              role: 1,
            },
          }
        : method === "CreateAccountKey"
          ? { secret: "old-login-secret" }
          : {},
    );
    const user = userEvent.setup();
    mount(create(AccountSchema, { accountId, depth: 1 }), "access");
    await user.click(await screen.findByRole("button", { name: "Create API key" }));
    await user.type(screen.getByLabelText("Key name"), "Reporting");
    await user.click(screen.getByRole("button", { name: "Create key" }));
    await screen.findByLabelText("Secret");
    act(() =>
      useFleetStore.setState({ auth: { ...initialAuth, sessionGeneration: initialAuth.sessionGeneration + 1 } }),
    );
    expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument();
    await screen.findByRole("button", { name: "Create API key" });
  });
});
