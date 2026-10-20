import type { BrowserContext, Page, Route } from "@playwright/test";

export const clientA = "11111111-1111-4111-8111-111111111111";
export const clientB = "22222222-2222-4222-8222-222222222222";
export const subclient = "33333333-3333-4333-8333-333333333333";
export const hostAccount = "99999999-9999-4999-8999-999999999999";

export type WorkspaceCall = {
  method: string;
  account: string | undefined;
  body: Record<string, unknown>;
  page: Page;
};

const account = (id: string) => ({
  accountId: id,
  parentAccountId: id === subclient ? clientA : hostAccount,
  name: id === clientA ? "Customer A" : id === clientB ? "Customer B" : "East subclient",
  depth: id === subclient ? 2 : 1,
  role: "ACCOUNT_ROLE_ADMINISTRATOR",
  capabilityCeiling: "31",
  effectiveCapabilities: "31",
});

const summary = (id: string) => ({
  assetCount: id === clientA ? "4" : "2",
  minerCount: "1",
  moduleCount: id === clientA ? "3" : "1",
  hashrateHs: id === clientA ? 400e12 : 200e12,
  powerW: id === clientA ? 9000 : 6000,
  hashrateReportingCount: id === clientA ? "4" : "2",
  powerReportingCount: id === clientA ? "3" : "2",
  pairedReportingCount: id === clientA ? "3" : "2",
  ...(id === clientB ? { efficiencyJTh: 30 } : {}),
  hashingCount: id === clientA ? "3" : "2",
  attentionCount: id === clientA ? "1" : "0",
  temperatureC: 52,
  temperatureReportingCount: id === clientA ? "4" : "2",
  uptimePercent: 97,
  uptimeReportingCount: id === clientA ? "4" : "2",
});

const fulfill = (route: Route, data: unknown) =>
  route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(data) });

/** Every RPC is intercepted: these scenarios never contact a backend or miner. */
export async function mockClientWorkspace(context: BrowserContext, preview: boolean) {
  const calls: WorkspaceCall[] = [];
  const unexpected: string[] = [];
  let denied = false;
  let recalled = false;
  let noHistory = false;
  let pause: { accountId: string; entered: () => void; release: Promise<void>; completed: () => void } | undefined;
  const username = preview ? "host-operator" : "customer-admin";
  await context.addInitScript(
    ({ username, preview }) => {
      if (!localStorage.getItem("proto-fleet-auth")) {
        localStorage.setItem(
          "proto-fleet-auth",
          JSON.stringify({
            state: {
              auth: {
                sessionExpiry: new Date(Date.now() + 3600000).toISOString(),
                sessionGeneration: 1,
                isAuthenticated: true,
                username,
                role: "Admin",
                permissions: preview ? ["account:manage"] : [],
                permissionsScope: "org",
              },
            },
            version: 0,
          }),
        );
      }
    },
    { username, preview },
  );
  await context.route("**/api-proxy/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/api-proxy/api/v1/alerts/enabled") return fulfill(route, { enabled: false });
    const method = url.pathname.split("/").slice(-1)[0];
    const body = (request.postDataJSON() ?? JSON.parse(url.searchParams.get("message") ?? "{}")) as Record<
      string,
      unknown
    >;
    const id = String(body.accountId ?? clientA);
    calls.push({ method, body, account: request.headers()["x-fleet-account-id"], page: request.frame().page() });
    if (method === "GetFleetInitStatus") return fulfill(route, { status: { adminCreated: true } });
    if (method === "ListAccounts")
      return fulfill(route, {
        hostAccess: preview,
        accounts: preview
          ? [{ accountId: hostAccount, name: "Host", depth: 0, capabilityCeiling: "31", effectiveCapabilities: "31" }]
          : [account(clientA)],
      });
    if (method === "GetAccount") {
      if (denied)
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({ code: "permission_denied", message: "Account access removed" }),
        });
      return fulfill(route, { account: account(id) });
    }
    if (method === "GetSitePolicy") return fulfill(route, {});
    if (method === "GetAccountDashboard") {
      const pending = pause?.accountId === id ? pause : undefined;
      if (pending) {
        pause = undefined;
        pending.entered();
        await pending.release;
      }
      try {
        return await fulfill(route, {
          totals: summary(id),
          readings: noHistory
            ? []
            : [
                { time: new Date(Date.now() - 120000).toISOString(), summary: summary(id) },
                { time: new Date(Date.now() - 60000).toISOString(), summary: summary(id) },
              ],
          sites: [
            { siteId: "7", name: id === clientA ? "A allocated site" : "B allocated site", summary: summary(id) },
          ],
          children: id === clientA ? [{ accountId: subclient, name: "East subclient", summary: summary(clientB) }] : [],
          generatedAt: new Date().toISOString(),
          historyAvailableFrom: new Date(Date.now() - 86400000).toISOString(),
        });
      } finally {
        pending?.completed();
      }
    }
    if (method === "ListChildren")
      return fulfill(route, {
        accounts: id === hostAccount ? [account(clientA), account(clientB)] : [account(subclient)],
      });
    if (method === "ListAccountAssets")
      return fulfill(route, {
        filterOptions: {
          models: ["S21", "S19"],
          sites: [
            { id: "7", name: "A allocated site" },
            { id: "8", name: "South" },
          ],
          containers: [],
        },
        assets:
          recalled || body.model === "S19" || body.siteId === "8"
            ? []
            : [
                {
                  kind: "MINING_ASSET_KIND_MINER",
                  id: "42",
                  revision: "1",
                  name: `${account(id).name} miner`,
                  model: "S21",
                  siteId: "7",
                  siteName: "A allocated site",
                  available: true,
                  state: "hashing",
                  hashrateHs: 100e12,
                  powerW: 3000,
                },
              ],
        canViewAssignments: true,
        canManageAssignments: true,
        children: [{ accountId: subclient, name: "East subclient" }],
      });
    if (method === "GetAccountAssetDetail") {
      if (recalled)
        return route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ code: "not_found", message: "Asset is no longer allocated to this account" }),
        });
      return fulfill(route, {
        asset: {
          kind: "MINING_ASSET_KIND_MINER",
          id: "42",
          name: "Customer A miner",
          available: true,
          state: "hashing",
          hashrateHs: 100e12,
          powerW: 3000,
        },
      });
    }
    if (
      [
        "ListPoolConfigurations",
        "GetIncomingPoolProfile",
        "ListMembers",
        "ListInvitations",
        "ListAccountKeys",
        "ListAncestorGrants",
      ].includes(method)
    )
      return fulfill(route, {});
    unexpected.push(method);
    return route.fulfill({
      status: 501,
      contentType: "application/json",
      body: JSON.stringify({ code: "unimplemented", message: `Unexpected workspace RPC: ${method}` }),
    });
  });
  return {
    calls,
    unexpected,
    username,
    withoutHistoricalReadings: () => {
      noHistory = true;
    },
    recallAsset: () => {
      recalled = true;
    },
    denyAccount: () => {
      denied = true;
    },
    pauseDashboard(accountId: string) {
      let release!: () => void;
      let entered!: () => void;
      let completed!: () => void;
      const finished = new Promise<void>((resolve) => {
        completed = resolve;
      });
      const pending = new Promise<void>((resolve) => {
        entered = resolve;
      });
      pause = {
        accountId,
        entered,
        completed,
        release: new Promise<void>((resolve) => {
          release = resolve;
        }),
      };
      return { pending, release, finished };
    },
  };
}
