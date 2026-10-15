import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import AccountDirectory from "./AccountDirectory";
import AccountWorkspace from "./AccountWorkspace";
import { HostedAccountsContext } from "./HostedAccountsContext";
import { AccountRole, AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";

const rootId = "99999999-9999-4999-8999-999999999999";
const accountId = "11111111-1111-4111-8111-111111111111";
const initialAuth = useFleetStore.getState().auth;
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  useFleetStore.setState({ auth: initialAuth });
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
) {
  const refresh = vi.fn();
  render(
    <MemoryRouter initialEntries={[`/accounts/${account.accountId}/${section}`]}>
      <HostedAccountsContext.Provider value={{ accounts: [account], hostAccess, loading: false, error: null, refresh }}>
        <Routes>
          <Route path="/accounts/:accountId/:section?" element={<AccountWorkspace />} />
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
      const main = within(screen.getByRole("navigation", { name: "Main" }));
      expect(await main.findByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
      expect(main.getByRole("link", { name: "Mining assets" })).toBeInTheDocument();
      expect(main.getByRole("link", { name: "Pools" })).toBeInTheDocument();
      expect(main.getByRole("link", { name: "Access" })).toBeInTheDocument();
      expect(main.getByRole("link", { name: "Settings" })).toHaveAttribute("href", `/accounts/${accountId}/settings`);
      expect(!!main.queryByRole("link", { name: "Team" })).toBe(management);
      expect(!!main.queryByRole("link", { name: "Subclients" })).toBe(children);
      expect(main.queryByRole("link", { name: "Fleet" })).not.toBeInTheDocument();
      await screen.findByText("No mining assets assigned");
    },
  );
  it("opens host root administration by default without resolving a private customer", async () => {
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") throw new Error("Root is not a private customer account");
      return method === "ListChildren" ? { accounts: [] } : {};
    });
    mount(undefined, "");
    await screen.findByRole("button", { name: "Create customer" });
    expect(calls.some((call) => call.method === "GetAccount" || call.method === "GetAccountMiningSummary")).toBe(false);
    const tabs = within(screen.getByRole("navigation", { name: "Account sections" }));
    expect(tabs.getByRole("button", { name: "Customers" })).toHaveAttribute("aria-current", "page");
    expect(tabs.getByRole("button", { name: "Mining assets" })).toBeInTheDocument();
    expect(tabs.queryByRole("button", { name: "Overview" })).not.toBeInTheDocument();
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
    await user.click(await screen.findByRole("button", { name: /Assignment/ }));
    await user.click(await screen.findByRole("option", { name: "East" }));
    await waitFor(() =>
      expect(calls.filter((call) => call.method === "ListAccountAssets").slice(-1)[0]?.body.ownerAccountId).toBe(
        childId,
      ),
    );
    reduced = true;
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(screen.queryByRole("button", { name: /Assignment/ })).not.toBeInTheDocument());
    await waitFor(() =>
      expect(
        calls.filter((call) => call.method === "ListAccountAssets").slice(-1)[0]?.body.ownerAccountId,
      ).toBeUndefined(),
    );
    expect(screen.queryByText("East")).not.toBeInTheDocument();
  });
  it("retains the host shell and opens account sections as tabs under Settings Admin", async () => {
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
    mount(account, "settings", true);
    await screen.findByRole("button", { name: "Rename account" });
    const main = screen.getByRole("navigation", { name: "Main" });
    expect(within(main).getByRole("link", { name: "Fleet" })).toBeInTheDocument();
    expect(within(main).queryByRole("link", { name: "Accounts" })).not.toBeInTheDocument();
    expect(within(main).getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
    const admin = screen.getByRole("navigation", { name: "Settings" });
    expect(within(admin).getByText("Admin")).toBeInTheDocument();
    expect(within(admin).getByRole("link", { name: "Accounts" })).toHaveAttribute("aria-current", "page");
    const tabs = screen.getByRole("navigation", { name: "Account sections" });
    await user.click(within(tabs).getByRole("button", { name: "Pools" }));
    await screen.findByRole("button", { name: "Change selection" });
    expect(
      within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Fleet" }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("navigation", { name: "Account sections" })).getByRole("button", { name: "Pools" }),
    ).toHaveAttribute("aria-current", "page");
  });
  it("keeps Overview and Mining assets available beside account settings without duplicate Home navigation", async () => {
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
    mount(account, "settings");
    await screen.findByRole("button", { name: "Rename account" });
    const menu = within(screen.getByRole("navigation", { name: "Main" })).getByRole("list");
    expect(within(menu).getByRole("link", { name: "Settings" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/settings`,
    );
    expect(within(menu).queryByRole("link", { name: "Home" })).not.toBeInTheDocument();
    expect(within(menu).getByRole("link", { name: "Overview" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/overview`,
    );
    expect(within(menu).getByRole("link", { name: "Mining assets" })).toHaveAttribute(
      "href",
      `/accounts/${accountId}/assets`,
    );
    await user.click(screen.getByRole("button", { name: "Switch account" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "Switch account" })).getByRole("link", { name: "All accounts" }),
    );
    await screen.findByRole("link", { name: "Join an account" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("opens host administration and creates a customer using the root ceiling", async () => {
    const calls = fetchRpc((method) => {
      if (method === "GetAccount") throw new Error("Root is not a private customer account");
      return method === "ListChildren" ? { accounts: [] } : {};
    });
    const user = userEvent.setup();
    mount();
    await user.click(await screen.findByRole("button", { name: "Create customer" }));
    const dialog = screen.getByRole("dialog", { name: "Create account" });
    expect(within(dialog).getByRole("checkbox", { name: "View account data" })).toBeDisabled();
    expect(within(dialog).getByRole("checkbox", { name: "Manage memberships" })).toBeDisabled();
    expect(within(dialog).getByRole("checkbox", { name: "Manage memberships" })).toBeChecked();
    await user.type(within(dialog).getByLabelText("Account name"), "Customer A");
    await user.click(within(dialog).getByRole("button", { name: "Create account" }));
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
    const calls = fetchRpc((method) => (method === "GetAccount" ? { account: toJson(AccountSchema, account) } : {}));
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
    const dialog = screen.getByRole("dialog", { name: "Incoming pool configuration" });
    await user.click(within(dialog).getByRole("checkbox", { name: /Clear/ }));
    await user.type(within(dialog).getByLabelText("Your Fleet password"), "incorrect-test-password");
    await user.click(within(dialog).getByRole("button", { name: "Confirm selection" }));
    expect(await screen.findByText(/Incorrect confirmation password/)).toBeInTheDocument();
    await waitFor(() => expect(calls.filter((c) => c.method === "GetAccount").length).toBeGreaterThanOrEqual(2));
    expect(screen.getByRole("dialog", { name: "Incoming pool configuration" })).toBeInTheDocument();
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
