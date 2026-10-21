import type { BrowserContext, Route } from "@playwright/test";

export async function mockRemoteNetworkSetup(context: BrowserContext) {
  await context.addInitScript(() => {
    localStorage.setItem(
      "proto-fleet-auth",
      JSON.stringify({
        state: {
          auth: {
            sessionExpiry: new Date(Date.now() + 3600000).toISOString(),
            sessionGeneration: 1,
            isAuthenticated: true,
            username: "setup-operator",
            role: "Admin",
            permissions: ["miner:pair", "fleetnode:read", "fleetnode:manage"],
            permissionsScope: "org",
          },
        },
        version: 0,
      }),
    );
  });

  const unexpected: string[] = [];
  const fulfill = (route: Route, data: unknown) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(data) });
  await context.route("**/api-proxy/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api-proxy/api/v1/alerts/enabled") return fulfill(route, { enabled: false });
    const method = path.split("/").pop();
    switch (method) {
      case "GetFleetInitStatus":
        return fulfill(route, { status: { adminCreated: true } });
      case "GetFleetOnboardingStatus":
        return fulfill(route, { status: {} });
      case "GetNetworkInfo":
        return fulfill(route, { networkInfo: { subnet: "192.168.1.0/24" } });
      case "ListFleetNodes":
        return fulfill(route, { fleetNodes: [] });
      case "ListAccounts":
        return fulfill(route, { hostAccess: true, accounts: [] });
      case "CreateEnrollmentCode":
        return fulfill(route, { code: "setup-code", pendingEnrollmentId: "11" });
      default:
        unexpected.push(path);
        return route.fulfill({ status: 501, contentType: "application/json", body: "{}" });
    }
  });
  return { unexpected };
}
