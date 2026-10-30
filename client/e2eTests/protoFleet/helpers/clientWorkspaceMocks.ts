import type { BrowserContext, Page, Route } from "@playwright/test";

export const clientA = "11111111-1111-4111-8111-111111111111";
export const clientB = "22222222-2222-4222-8222-222222222222";
export const subclient = "33333333-3333-4333-8333-333333333333";
export const hostAccount = "99999999-9999-4999-8999-999999999999";
export const newClient = "44444444-4444-4444-8444-444444444444";

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
export async function mockClientWorkspace(
  context: BrowserContext,
  preview: boolean,
  options: { fleet?: boolean; missingModuleUptime?: boolean } = {},
) {
  const configured = new Map<string, { capabilities: string; suspended: boolean }>();
  let createdName = "";
  let allocatedTo = hostAccount;
  const accountFor = (id: string) => {
    const saved = configured.get(id);
    return {
      ...account(id),
      ...(id === newClient ? { name: createdName, depth: 1, parentAccountId: hostAccount } : {}),
      ...(saved
        ? {
            capabilityCeiling: saved.capabilities,
            effectiveCapabilities: saved.capabilities,
            suspended: saved.suspended,
          }
        : {}),
    };
  };
  const calls: WorkspaceCall[] = [];
  const unexpected: string[] = [];
  let denied = false;
  let recalled = false;
  let noHistory = false;
  let activityMiner = false;
  let narrowActivity = false;
  let pause:
    | { method: string; accountId: string; entered: () => void; release: Promise<void>; completed: () => void }
    | undefined;
  const username = preview ? "host-operator" : "customer-admin";
  await context.addInitScript(
    ({ username, preview, fleet }) => {
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
                permissions: preview
                  ? ["account:manage", ...(fleet ? ["fleet:read", "site:read", "miner:read"] : [])]
                  : [],
                permissionsScope: "org",
              },
            },
            version: 0,
          }),
        );
      }
    },
    { username, preview, fleet: options.fleet },
  );
  await context.route("**/api-proxy/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/api-proxy/api/v1/alerts/enabled") return fulfill(route, { enabled: false });
    const method = url.pathname.split("/").slice(-1)[0];
    const envelope = method === "ExportAccountActivities" ? request.postDataBuffer()! : null;
    const body = (
      envelope
        ? JSON.parse(envelope.subarray(5, 5 + envelope.readUInt32BE(1)).toString("utf8"))
        : (request.postDataJSON() ?? JSON.parse(url.searchParams.get("message") ?? "{}"))
    ) as Record<string, unknown>;
    const id = String(body.accountId ?? clientA);
    calls.push({ method, body, account: request.headers()["x-fleet-account-id"], page: request.frame().page() });
    if (method === "GetFleetInitStatus") return fulfill(route, { status: { adminCreated: true } });
    if (method === "ListAccounts")
      return fulfill(route, {
        hostAccess: preview,
        accounts: preview
          ? [{ accountId: hostAccount, name: "Host", depth: 0, capabilityCeiling: "31", effectiveCapabilities: "31" }]
          : [accountFor(clientA)],
      });
    if (method === "ConfigureAccount") {
      configured.set(String(body.childAccountId), {
        capabilities: String(body.capabilities),
        suspended: !!body.suspended,
      });
      return fulfill(route, {});
    }
    if (method === "CreateAccount") {
      createdName = String(body.name);
      return fulfill(route, { account: accountFor(newClient) });
    }
    if (method === "CreateInvitation") return fulfill(route, { token: "K7MP-4X9R" });
    if (method === "AssignAccountAssets") {
      allocatedTo = String(body.childAccountId);
      return fulfill(route, {});
    }
    if (options.fleet && method === "ListInventory") {
      const items = [
        { id: "42", name: "Allocation miner", deviceIdentifier: "allocation-miner" },
        { id: "43", name: "Other miner", deviceIdentifier: "other-miner" },
      ].map(({ id, name, deviceIdentifier }) => ({
        ref: { kind: "ENTITY_KIND_MINER", id },
        name,
        deviceIdentifier,
        miner: {
          deviceIdentifier,
          name,
          model: "S21",
          pairingStatus: "PAIRING_STATUS_PAIRED",
          deviceStatus: "DEVICE_STATUS_ONLINE",
        },
      }));
      return fulfill(route, { items, totalCount: "2", minerCount: "2", models: ["S21"] });
    }
    if (options.fleet && method === "ListMinerStateSnapshots") return fulfill(route, {});
    if (
      options.fleet &&
      ["ListSites", "ListBuildings", "ListContainers", "ListTankOptions", "ListSavedViews", "Query"].includes(method)
    )
      return fulfill(route, {});
    if (method === "GetAccount") {
      if (denied)
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({ code: "permission_denied", message: "Account access removed" }),
        });
      return fulfill(route, { account: accountFor(id) });
    }
    if (method === "GetSitePolicy") return fulfill(route, {});
    if (["ListAccountActivities", "ExportAccountActivities"].includes(method)) {
      if (denied)
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({ code: "permission_denied", message: "Account access removed" }),
        });
      const pending = pause?.accountId === id && pause.method === method ? pause : undefined;
      if (pending) {
        pause = undefined;
        pending.entered();
        await pending.release;
      }
      try {
        const filter = (body.filter ?? {}) as { searchText?: string; eventTypes?: string[]; scopeTypes?: string[] };
        const entries = [
          {
            eventId: `${id}-pool`,
            eventCategory: "pool",
            eventType: "create_hosted_account_pool",
            description: "Created pool configuration",
            scopeType: "account",
            scopeLabel: "Account",
            scopeCount: 0,
            result: "success",
            createdAt: "2026-10-20T09:00:00Z",
          },
          {
            eventId: `${id}-recall`,
            eventCategory: "fleet_management",
            eventType: "account_assets_recalled",
            description: "Recalled mining assets",
            scopeType: "asset",
            scopeLabel: "",
            scopeCount: 2,
            result: "success",
            createdAt: "2026-10-20T08:00:00Z",
          },
        ];
        if (activityMiner)
          entries[0] = {
            ...entries[0],
            eventCategory: "device_command",
            eventType: "reboot.completed",
            description: "Rebooted miners",
            scopeType: "device",
            scopeLabel: "",
            scopeCount: 1,
          };
        if (narrowActivity) entries.splice(0, 1);
        if (id === clientB)
          entries.splice(0, entries.length, {
            eventId: `${id}-rename`,
            eventCategory: "auth",
            eventType: "rename_hosted_account",
            description: "Renamed account",
            scopeType: "account",
            scopeLabel: "Account",
            scopeCount: 0,
            result: "success",
            createdAt: "2026-10-20T09:00:00Z",
          });
        const filtered = entries.filter(
          (entry) =>
            (!filter.searchText || entry.description.toLowerCase().includes(filter.searchText.toLowerCase())) &&
            (!filter.eventTypes?.length || filter.eventTypes.includes(entry.eventType)) &&
            (!filter.scopeTypes?.length || filter.scopeTypes.includes(entry.scopeType)),
        );
        if (method === "ExportAccountActivities") {
          const envelope = (flags: number, message: unknown) => {
            const payload = Buffer.from(JSON.stringify(message));
            const header = Buffer.alloc(5);
            header.writeUInt8(flags, 0);
            header.writeUInt32BE(payload.length, 1);
            return Buffer.concat([header, payload]);
          };
          const csv =
            "Timestamp,Type,Description\n" +
            filtered.map((entry) => `${entry.createdAt},${entry.eventType},${entry.description}`).join("\n");
          return route.fulfill({
            status: 200,
            contentType: "application/connect+json",
            body: Buffer.concat([envelope(0, { chunk: Buffer.from(csv).toString("base64") }), envelope(2, {})]),
          });
        }
        return fulfill(route, {
          activities: body.pageToken ? filtered.slice(1) : filtered.slice(0, 1),
          totalCount: filtered.length,
          authorityFingerprint: `${id}:${narrowActivity ? "narrow" : "full"}`,
          nextPageToken: !body.pageToken && filtered.length > 1 ? "next" : "",
          filterOptions: {
            eventTypes: entries.map(({ eventType, eventCategory }) => ({ eventType, eventCategory })),
            scopeTypes: ["account", "asset"],
          },
        });
      } finally {
        pending?.completed();
      }
    }
    if (method === "GetAccountDashboard") {
      const pending = pause?.accountId === id && pause.method === method ? pause : undefined;
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
                ...[120000, 60000].map((age) => {
                  const time = new Date(Date.now() - age).toISOString();
                  const reading = summary(id);
                  return {
                    time,
                    summary: reading,
                    temperatureStatus: { timestamp: time, okCount: Number(reading.assetCount) },
                    uptimeStatus: {
                      timestamp: time,
                      hashingCount: options.missingModuleUptime ? 1 : Number(reading.hashingCount),
                      brokenCount: options.missingModuleUptime ? 0 : Number(reading.attentionCount),
                    },
                  };
                }),
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
        accounts:
          id === hostAccount
            ? [accountFor(clientA), accountFor(clientB), ...(createdName ? [accountFor(newClient)] : [])]
            : [accountFor(subclient)],
      });
    if (method === "ListAccountAssets") {
      if (options.fleet && id === hostAccount) {
        const selected = body.selectedAssets as { id: string }[] | undefined;
        const assets = ["42", "43"].map((assetId) => ({
          kind: "MINING_ASSET_KIND_MINER",
          id: assetId,
          revision: assetId === "42" ? "7" : "3",
          name: assetId === "42" ? "Allocation miner" : "Other miner",
          model: "S21",
          siteId: "7",
          siteName: "A allocated site",
          available: true,
          ownerAccountId: assetId === "42" ? allocatedTo : hostAccount,
          ownerAccountName: assetId === "42" && allocatedTo !== hostAccount ? createdName : "Host",
        }));
        return fulfill(route, {
          assets: assets
            .filter((asset) =>
              selected ? selected.some(({ id }) => id === asset.id) : asset.ownerAccountId === body.ownerAccountId,
            )
            .filter(
              (asset) =>
                (!body.model || body.model === asset.model) &&
                (!body.search || asset.name.toLowerCase().includes(String(body.search).toLowerCase())),
            ),
          filterOptions: { models: ["S21", "S19"] },
          canViewAssignments: true,
          canManageAssignments: true,
          children: [accountFor(clientA), accountFor(clientB), ...(createdName ? [accountFor(newClient)] : [])],
        });
      }
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
          recalled ||
          body.model === "S19" ||
          body.siteId === "8" ||
          (body.ownerAccountId === newClient && allocatedTo !== newClient)
            ? []
            : [
                {
                  kind: "MINING_ASSET_KIND_MINER",
                  id: "42",
                  revision: "1",
                  name: body.ownerAccountId === newClient ? "Allocation miner" : `${accountFor(id).name} miner`,
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
    }
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
  function pauseRead(method: string, accountId: string) {
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
      method,
      entered,
      completed,
      release: new Promise<void>((resolve) => {
        release = resolve;
      }),
    };
    return { pending, release, finished };
  }
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
    showMinerActivity: () => {
      activityMiner = true;
    },
    narrowActivityAuthority: () => {
      narrowActivity = true;
    },
    denyAccount: () => {
      denied = true;
    },
    pauseDashboard: (accountId: string) => pauseRead("GetAccountDashboard", accountId),
    pauseActivity: (accountId: string) => pauseRead("ListAccountActivities", accountId),
    pauseActivityExport: (accountId: string) => pauseRead("ExportAccountActivities", accountId),
  };
}
