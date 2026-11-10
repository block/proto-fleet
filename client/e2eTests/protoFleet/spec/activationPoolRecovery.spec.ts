import { expect, test } from "@playwright/test";
import { mockClientWorkspace } from "../helpers/clientWorkspaceMocks";
import { ActivationPoolRecoveryPage } from "../pages/activationPoolRecovery";

test.use({ storageState: { cookies: [], origins: [] } });

test("recover an enabled container assignment without clearing device pools", async ({ context, page }) => {
  await mockClientWorkspace(context, true, { fleet: true });
  const calls: { method: string; body: Record<string, unknown> }[] = [];
  let removed = false;
  let assigned = false;
  const configuration = {
    configurationId: "config-1",
    name: "Production",
    version: "1",
    assignedCount: "1",
    pools: [{ slotId: "slot-1", url: "stratum+tcp://pool.example:3333", username: "worker" }],
  };
  const container = { id: "7", name: "Recovery CT1", deviceIdentifier: "ct1", tanks: [] };
  const parent = { kind: "ENTITY_KIND_CONTAINER", id: "7" };
  const tank = { ref: { kind: "ENTITY_KIND_TANK", id: "81" }, ancestors: [parent], name: "Tank 1", position: 1 };
  const module = {
    ref: { kind: "ENTITY_KIND_MODULE", id: "91" },
    ancestors: [parent, tank.ref],
    deviceIdentifier: "ct1",
    name: "Module 1",
    position: 1,
    state: "active",
  };
  // This scenario intercepts all RPCs; no backend or equipment is modified.
  await context.route(/\/GetContainer$/, (route) => route.fulfill({ json: { container } }));
  await context.route(/\/GetState$/, (route) =>
    route.fulfill({
      json: {
        supported: true,
        enabled: true,
        status: "complete",
        observedAt: new Date().toISOString(),
      },
    }),
  );
  await context.route(/\/ListInventory$/, (route) => {
    const body = route.request().postDataJSON();
    const items = body.kinds?.includes("ENTITY_KIND_TANK")
      ? [tank]
      : [{ ...module, observedAt: new Date().toISOString() }];
    return route.fulfill({ json: { items, totalCount: "1" } });
  });
  await context.route(/\/GetModuleHashboards$/, (route) =>
    route.fulfill({ json: { modules: [{ moduleId: "91", slots: [] }] } }),
  );
  await context.route(/\/VerifyCredentials$/, (route) => route.fulfill({ json: {} }));
  await context.route(/\/poolconfigurations\.v1\.PoolConfigurationService\//, async (route) => {
    const method = new URL(route.request().url()).pathname.split("/").pop()!;
    const body = route.request().postDataJSON() as Record<string, unknown>;
    calls.push({ method, body });
    if (method === "GetAssignments") {
      const boards = !!body.boards;
      return route.fulfill({
        json: {
          selectedCount: "1",
          assignments: boards
            ? [
                {
                  moduleId: "91",
                  hashboardId: "101",
                  installationId: "201",
                  deviceId: "7",
                  deviceIdentifier: "ct1",
                  configurationId: assigned ? "config-1" : "",
                  name: assigned ? "Production" : "",
                  state: assigned ? "verified" : "device_configuration",
                },
              ]
            : removed
              ? []
              : [{ deviceId: "7", deviceIdentifier: "ct1", deviceName: "Recovery CT1", configurationId: "config-1" }],
        },
      });
    }
    if (method === "ListConfigurations") return route.fulfill({ json: { configurations: [configuration] } });
    if (method === "AuthorizeReviews") return route.fulfill({ json: {} });
    if (method === "ReviewAssignment")
      return route.fulfill({
        json: body.removalTarget
          ? { reviewId: "removal-review", removal: { verificationState: "applied_unverified", clearSupported: false } }
          : {
              reviewId: "board-review",
              configuration,
              affectedHashboards: "1",
              equipment: [{ kind: "hashboard_installation", targetId: "201", label: "Hashboard 101" }],
            },
      });
    if (method === "ConfirmReview") {
      if (body.reviewId === "removal-review") removed = true;
      else assigned = true;
      return route.fulfill({
        json: { operationId: body.reviewId === "removal-review" ? "removal-op" : "board-op", configuration },
      });
    }
    if (method === "GetOperation")
      return route.fulfill({
        json:
          body.operationId === "removal-op"
            ? {
                operationId: "removal-op",
                kind: "unassign",
                targets: [{ kind: "device", targetId: "7", state: "completed" }],
              }
            : {
                operationId: "board-op",
                configurationId: "config-1",
                configurationName: "Production",
                targets: [{ kind: "hashboard_installation", targetId: "201", state: "verified" }],
              },
      });
    return route.fulfill({ json: {} });
  });
  await page.goto("/");
  await page.evaluate(() => {
    const auth = JSON.parse(localStorage.getItem("proto-fleet-auth")!);
    auth.state.auth.permissions = [
      "account:manage",
      "fleet:read",
      "site:read",
      "miner:read",
      "pool:read",
      "miner:update_pools",
      "miner:start_mining",
      "miner:stop_mining",
    ];
    localStorage.setItem("proto-fleet-auth", JSON.stringify(auth));
  });
  const recovery = new ActivationPoolRecoveryPage(page);
  await recovery.removeContainerAssignment();
  await expect
    .poll(() => calls.find((call) => call.method === "ConfirmReview")?.body)
    .toMatchObject({ reviewId: "removal-review", keepDeviceSettings: true });
  await expect.poll(() => calls.some((call) => call.method === "AuthorizeReviews")).toBe(true);
  await expect.poll(() => calls.some((call) => call.method === "GetOperation")).toBe(true);
  expect(calls.some((call) => call.method === "ListConfigurations")).toBe(false);
  await recovery.assignModule();
  await expect(page.getByRole("button", { name: "Production", exact: true })).toBeVisible();
  await expect
    .poll(
      () =>
        calls.find(
          (call) =>
            call.method === "ReviewAssignment" && call.body.reviewId !== "removal-review" && !call.body.removalTarget,
        )?.body,
    )
    .toMatchObject({ boards: { moduleIds: ["91"] }, configurationId: "config-1" });
  await expect
    .poll(() => calls.some((call) => call.method === "ConfirmReview" && call.body.reviewId === "board-review"))
    .toBe(true);
  expect(calls.findIndex((call) => call.method === "ReviewAssignment" && !call.body.removalTarget)).toBeLessThan(
    calls.findIndex((call) => call.method === "ConfirmReview" && call.body.reviewId === "board-review"),
  );
});
