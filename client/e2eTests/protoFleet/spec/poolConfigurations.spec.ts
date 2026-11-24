import { create, toBinary } from "@bufbuild/protobuf";
import type { BrowserContext } from "@playwright/test";
import { expect, test } from "@playwright/test";
import { FleetErrorDetailsSchema } from "../../../src/protoFleet/api/generated/common/v1/common_pb";
import { ConfirmReviewErrorCode } from "../../../src/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { clientA, mockClientWorkspace } from "../helpers/clientWorkspaceMocks";
import { ConfirmationRecoveryPage } from "../pages/confirmationRecovery";
import { EditPoolPage } from "../pages/editPool";
import { NewPoolModalPage } from "../pages/newPoolModal";
import { SettingsPoolsPage } from "../pages/settingsPools";

type Slot = { slotId: string; url: string; username: string; workerTemplate?: string };
type Configuration = { configurationId: string; name: string; version: string; assignedCount: string; pools: Slot[] };

test.use({ storageState: { cookies: [], origins: [] } });

async function mockPoolConfigurations(context: BrowserContext) {
  const workspace = await mockClientWorkspace(context, false);
  const calls: { method: string; body: Record<string, unknown> }[] = [];
  const configurations = new Map<string, Configuration>();
  let pending: Configuration | undefined;
  let maintenance = false;
  let assigned = false;
  let loseNextReply = false;
  let expiredUnaccepted = false;
  const production: Configuration = {
    configurationId: "config-1",
    name: "Production",
    version: "1",
    assignedCount: "0",
    pools: [
      {
        slotId: "slot-1",
        url: "stratum+tcp://primary.example:3333",
        username: "payout",
        workerTemplate: "{miner_name}",
      },
    ],
  };
  configurations.set(production.configurationId, production);
  await context.route(/\/VerifyCredentials$/, (route) => route.fulfill({ json: {} }));
  await context.route(/\/ListAccountAssets$/, (route) =>
    route.fulfill({
      json: {
        assets: ["42", "43"].map((id) => ({
          kind: "MINING_ASSET_KIND_MINER",
          id,
          name: `Miner ${id}`,
          available: true,
          canChangePools: true,
        })),
      },
    }),
  );
  await context.route(/\/GetAccountAssetDetail$/, (route) =>
    route.fulfill({
      json: {
        asset: {
          kind: "MINING_ASSET_KIND_MINER",
          id: "42",
          name: "Reviewed miner",
          available: true,
          canChangePools: true,
        },
      },
    }),
  );
  await context.route(/\/poolconfigurations\.v1\.PoolConfigurationService\//, async (route) => {
    const method = new URL(route.request().url()).pathname.split("/").slice(-1)[0];
    const body = route.request().postDataJSON() as Record<string, unknown>;
    calls.push({ method, body });
    if (method === "ListConfigurations")
      return route.fulfill({ json: { configurations: [...configurations.values()] } });
    if (method === "GetAssignments") return route.fulfill({ json: { selectedCount: "1", assignments: [] } });
    if (method === "TestConnection") return route.fulfill({ json: { connected: true } });
    if (method === "CreateConfiguration") {
      const draft = body.configuration as { name: string; pools: Slot[] };
      const created = {
        ...draft,
        configurationId: "created-config",
        version: "1",
        assignedCount: "0",
        pools: draft.pools.map((slot, index) => ({ ...slot, slotId: `created-slot-${index}` })),
      };
      configurations.set(created.configurationId, created);
      return route.fulfill({ json: { configuration: created } });
    }
    if (method === "ReviewEdit") {
      const previous = configurations.get(String(body.configurationId))!;
      const draft = body.configuration as { name: string; pools: Slot[] };
      pending = { ...previous, ...draft, version: String(Number(previous.version) + 1) };
      return route.fulfill({
        json: {
          reviewId: "edit-review",
          configuration: pending,
          affectedMiners: assigned ? "2" : "0",
          affectedHashboards: "0",
          equipment: assigned
            ? [
                { kind: "device", targetId: "42", label: "Miner 42" },
                { kind: "device", targetId: "43", label: "Miner 43" },
              ]
            : [],
        },
      });
    }
    if (method === "ReviewAssignment") {
      if (maintenance)
        return route.fulfill({
          status: 400,
          json: {
            code: "failed_precondition",
            message: "Pool configuration maintenance is in progress.",
          },
        });
      pending = configurations.get(String(body.configurationId))!;
      return route.fulfill({
        json: {
          reviewId: "assignment-review",
          configuration: pending,
          affectedMiners: String((body.deviceIds as string[]).length),
          affectedHashboards: "0",
          equipment: [{ kind: "device", targetId: "42", deviceIdentifier: "miner-42", label: "Reviewed miner" }],
        },
      });
    }
    if (method === "ConfirmReview") {
      if (expiredUnaccepted) {
        if (loseNextReply) {
          loseNextReply = false;
          return route.abort("connectionreset");
        }
        return route.fulfill({
          status: 400,
          json: {
            code: "failed_precondition",
            message: "This review expired without being accepted.",
            details: [
              {
                type: "common.v1.FleetErrorDetails",
                value: Buffer.from(
                  toBinary(
                    FleetErrorDetailsSchema,
                    create(FleetErrorDetailsSchema, {
                      code: { case: "endpoint", value: ConfirmReviewErrorCode.EXPIRED_UNACCEPTED },
                    }),
                  ),
                ).toString("base64"),
              },
            ],
          },
        });
      }
      const configuration = pending!;
      configurations.set(configuration.configurationId, configuration);
      if (loseNextReply) {
        loseNextReply = false;
        return route.abort("connectionreset");
      }
      return route.fulfill({
        json: {
          configuration,
          ...(body.reviewId === "assignment-review" || assigned ? { operationId: "operation-1" } : {}),
        },
      });
    }
    if (method === "GetOperation")
      return route.fulfill({
        json: {
          operationId: "operation-1",
          configurationId: production.configurationId,
          configurationName: production.name,
          targets: [
            {
              kind: "device",
              targetId: "42",
              deviceIdentifier: "miner-42",
              label: "Reviewed miner",
              state: "verified",
            },
          ],
        },
      });
    if (method === "ListOperations") return route.fulfill({ json: {} });
    if (method === "ArchiveConfiguration") {
      configurations.delete(String(body.configurationId));
      return route.fulfill({ json: {} });
    }
    throw new Error(`Unexpected pool RPC: ${method}`);
  });
  return {
    calls,
    workspace,
    expireUnaccepted: (expired: boolean) => {
      expiredUnaccepted = expired;
    },
    loseNextReply: () => {
      loseNextReply = true;
    },
    assigned: () => {
      assigned = true;
      production.assignedCount = "2";
    },
    maintenance: () => {
      maintenance = true;
    },
  };
}

test("create and edit a canonical configuration, including backup ordering", async ({ context, page, isMobile }) => {
  const mock = await mockPoolConfigurations(context);
  const library = new SettingsPoolsPage(page, isMobile);
  const editor = new NewPoolModalPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/pools`);
  await library.clickAddPool();
  await editor.validateConnectionDisabled();
  await editor.inputPoolName("Browser configuration");
  await editor.inputPoolUrl("stratum+tcp://new.example:3333");
  await editor.inputPoolUsername("wallet");
  await editor.clickTestConnection();
  await editor.validateConnectionSuccessful();
  await editor.clickSaveNewPool();
  await library.validatePoolEntryByUniqueName("Browser configuration", "stratum+tcp://new.example:3333", "wallet");
  await library.editConfiguration("Browser configuration");
  await editor.addBackup("stratum+tcp://backup.example:3333", "backup-wallet");
  await editor.moveBackupToPrimary();
  await editor.clickSaveNewPool();
  await library.validatePoolEntryByUniqueName(
    "Browser configuration",
    "stratum+tcp://backup.example:3333",
    "backup-wallet",
  );
  expect(mock.calls.find(({ method }) => method === "ReviewEdit")?.body).toMatchObject({
    configurationId: "created-config",
    expectedVersion: "1",
    configuration: {
      pools: [
        { url: "stratum+tcp://backup.example:3333", username: "backup-wallet" },
        { url: "stratum+tcp://new.example:3333", username: "wallet" },
      ],
    },
  });
  await library.deletePoolByNameIfVisible("Browser configuration");
  expect(mock.calls.some(({ method }) => method === "ArchiveConfiguration")).toBe(true);
  expect(mock.workspace.unexpected).toEqual([]);
});

test("assign a single miner directly, then inspect verified results", async ({ context, page, isMobile }) => {
  const mock = await mockPoolConfigurations(context);
  const picker = new EditPoolPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/assets/miner/42`);
  await page.getByRole("button", { name: "Edit pool", exact: true }).click();
  await page.getByLabel("Username", { exact: true }).fill("customer-admin");
  await page.getByLabel("Password", { exact: true }).fill("actor-password");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await picker.selectConfiguration("Production");
  await picker.assignSingle();
  expect(mock.calls.find(({ method }) => method === "ReviewAssignment")?.body).toMatchObject({
    accountId: clientA,
    configurationId: "config-1",
    expectedVersion: "1",
    deviceIds: ["42"],
  });
  await page.getByRole("button", { name: "View details", exact: true }).click();
  const results = page.getByRole("dialog", { name: "Pool configuration", exact: true });
  await expect(results).toContainText("Reviewed miner");
  await expect(results).toContainText("Verified");
  expect(mock.calls.find(({ method }) => method === "ConfirmReview")?.body).toEqual({
    accountId: clientA,
    reviewId: "assignment-review",
    password: "actor-password",
  });
  expect(mock.workspace.unexpected).toEqual([]);
});

test("maintenance rejection keeps the picker open and never confirms", async ({ context, page, isMobile }) => {
  const mock = await mockPoolConfigurations(context);
  mock.maintenance();
  const picker = new EditPoolPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/assets/miner/42`);
  await page.getByRole("button", { name: "Edit pool", exact: true }).click();
  await page.getByLabel("Username", { exact: true }).fill("customer-admin");
  await page.getByLabel("Password", { exact: true }).fill("actor-password");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await picker.selectConfiguration("Production");
  await page
    .getByRole("dialog", { name: "Edit pool", exact: true })
    .getByRole("button", { name: "Assign", exact: true })
    .click();
  await expect(page.getByText("Pool configuration maintenance is in progress.", { exact: true })).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Edit pool", exact: true })).toBeVisible();
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toEqual([]);
  expect(mock.workspace.unexpected).toEqual([]);
});

test("bulk assignment uses a count confirmation and can be canceled", async ({ context, page, isMobile }, testInfo) => {
  const mock = await mockPoolConfigurations(context);
  const picker = new EditPoolPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/assets`);
  await page.getByRole("checkbox", { name: "Select Miner 42", exact: true }).check();
  await page.getByRole("checkbox", { name: "Select Miner 43", exact: true }).check();
  await page.getByRole("button", { name: "Edit pool", exact: true }).click();
  await page.getByLabel("Username", { exact: true }).fill("customer-admin");
  await page.getByLabel("Password", { exact: true }).fill("actor-password");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await picker.selectConfiguration("Production");
  await picker.reviewAssignment(2);
  const confirmation = page.getByRole("dialog", { name: "Assign pool configuration to 2 miners?", exact: true });
  await expect(confirmation).toContainText("Assign Production? Pool changes may briefly interrupt mining.");
  await expect(confirmation.getByLabel("Your password")).toHaveCount(0);
  await expect(confirmation).toHaveCSS("opacity", "1");
  await expect(confirmation.locator("..")).toHaveCSS("opacity", "1");
  await page.screenshot({ path: testInfo.outputPath("bulk-assignment.png") });
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toEqual([]);
  await confirmation.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Edit pool", exact: true })).toBeVisible();
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toEqual([]);
  await picker.reviewAssignment(2);
  await picker.confirmAssignment();
  expect(mock.calls.find(({ method }) => method === "ReviewAssignment")?.body.deviceIds).toEqual(["42", "43"]);
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toHaveLength(1);
  expect(mock.workspace.unexpected).toEqual([]);
});

test("editing an assigned configuration retains the detailed equipment review", async ({
  context,
  page,
  isMobile,
}, testInfo) => {
  const mock = await mockPoolConfigurations(context);
  mock.assigned();
  const library = new SettingsPoolsPage(page, isMobile);
  const editor = new NewPoolModalPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/pools`);
  await library.editConfiguration("Production");
  await editor.inputPoolName("Updated production");
  await page
    .getByRole("dialog", { name: "Edit pool configuration", exact: true })
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  const review = page.getByRole("dialog", { name: "Review pool configuration changes", exact: true });
  await expect(review.getByRole("region", { name: "Affected equipment" })).toContainText("Miner 42");
  await expect(review.getByRole("region", { name: "Affected equipment" })).toContainText("Miner 43");
  await expect(review).toHaveCSS("opacity", "1");
  await expect(review.locator("..")).toHaveCSS("opacity", "1");
  await page.screenshot({ path: testInfo.outputPath("configuration-edit-review.png") });
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toEqual([]);
  await review.getByLabel("Your password", { exact: true }).fill("actor-password");
  await review.getByRole("button", { name: "Save and update equipment", exact: true }).click();
  await expect(review).toBeHidden();
  expect(mock.calls.find(({ method }) => method === "ConfirmReview")?.body.reviewId).toBe("edit-review");
  expect(mock.workspace.unexpected).toEqual([]);
});

test("recover a lost confirmation after closing and reloading without submitting a new review", async ({
  context,
  page,
  isMobile,
}, testInfo) => {
  const mock = await mockPoolConfigurations(context);
  mock.assigned();
  const library = new SettingsPoolsPage(page, isMobile);
  const editor = new NewPoolModalPage(page, isMobile);
  await page.goto(`/accounts/${clientA}/pools`);
  await library.editConfiguration("Production");
  await editor.inputPoolName("Recovered production");
  await page
    .getByRole("dialog", { name: "Edit pool configuration", exact: true })
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  const review = page.getByRole("dialog", { name: "Review pool configuration changes", exact: true });
  mock.loseNextReply();
  await review.getByLabel("Your password", { exact: true }).fill("first-password");
  await review.getByRole("button", { name: "Save and update equipment", exact: true }).click();
  await expect(review).toContainText("It may still be running.");
  await review.getByRole("button", { name: "Close", exact: true }).click();
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toHaveLength(1);
  await page.reload();
  await page.getByRole("button", { name: "Review update", exact: true }).click();
  const recovery = page.getByRole("dialog", { name: "Resolve update confirmation", exact: true });
  await expect(recovery).toContainText("Recovered production");
  await expect(recovery.getByLabel("Your password", { exact: true })).toBeEmpty();
  expect(mock.calls.filter(({ method }) => method === "ConfirmReview")).toHaveLength(1);
  await expect(recovery).toHaveCSS("opacity", "1");
  await expect(recovery.locator("..")).toHaveCSS("opacity", "1");
  await page.screenshot({ path: testInfo.outputPath("confirmation-recovery.png") });
  await recovery.getByLabel("Your password", { exact: true }).fill("fresh-password");
  await recovery.getByRole("button", { name: "Retry confirmation", exact: true }).click();
  await expect(recovery).toBeHidden();
  await page.getByRole("button", { name: "View details", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Pool configuration", exact: true })).toContainText("Verified");
  expect(
    mock.calls
      .filter(({ method }) => method === "ConfirmReview")
      .map(({ body }) => ({ reviewId: body.reviewId, password: body.password })),
  ).toEqual([
    { reviewId: "edit-review", password: "first-password" },
    { reviewId: "edit-review", password: "fresh-password" },
  ]);
  expect(mock.calls.filter(({ method }) => method === "ReviewEdit")).toHaveLength(1);
  expect(await page.evaluate(() => sessionStorage.getItem("proto-pool-confirmation-recovery"))).not.toContain(
    "password",
  );
  expect(mock.workspace.unexpected).toEqual([]);
});

test("device readings continue after a failed controller batch and open reported settings", async ({
  context,
  page,
}) => {
  const mock = await mockPoolConfigurations(context);
  await context.route(/\/ListAccountAssets$/, (route) =>
    route.fulfill({
      json: {
        assets: ["42", "43", "44", "45", "46", "47"].map((id) => ({
          kind: "MINING_ASSET_KIND_MINER",
          id,
          name: `Miner ${id}`,
          available: true,
          canChangePools: true,
        })),
      },
    }),
  );
  const batches: { targets: { kind: string; targetId: string }[]; accountId: string }[] = [];
  await context.route(/\/GetAssignments$/, (route) => {
    const request = route.request().postDataJSON() as { deviceIds?: string[] };
    return route.fulfill({
      json: {
        assignments: (request.deviceIds ?? []).map((id) => ({
          deviceId: id,
          deviceIdentifier: `miner-${id}`,
          deviceName: `Miner ${id}`,
          state: "unavailable",
        })),
      },
    });
  });
  await context.route(/\/ReadDevicePools$/, (route) => {
    const request = route.request().postDataJSON() as (typeof batches)[number];
    batches.push(request);
    if (request.targets.some((target) => target.targetId === "42")) {
      return route.fulfill({ status: 504, json: { code: "deadline_exceeded", message: "controller batch timed out" } });
    }
    return route.fulfill({
      json: {
        readings: request.targets.map((target) => ({
          target,
          reading:
            target.targetId === "46"
              ? {
                  state: "device_configuration",
                  readbackAvailable: true,
                  reported: [{ url: "stratum+tcp://device.example:3333", username: "device.worker" }],
                }
              : { state: "unavailable", resultCode: "check_unavailable" },
        })),
      },
    });
  });
  await page.goto(`/accounts/${clientA}/assets`);
  await expect(page.getByRole("button", { name: "Device configuration", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Unavailable", exact: true })).toHaveCount(5);
  expect(batches).toEqual([
    {
      accountId: clientA,
      targets: [
        { kind: "device", targetId: "42" },
        { kind: "device", targetId: "43" },
        { kind: "device", targetId: "44" },
        { kind: "device", targetId: "45" },
      ],
    },
    {
      accountId: clientA,
      targets: [
        { kind: "device", targetId: "46" },
        { kind: "device", targetId: "47" },
      ],
    },
  ]);
  await page.getByRole("button", { name: "Device configuration", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Pool configuration", exact: true });
  await expect(dialog).toContainText("device.worker");
  await expect(dialog).toContainText("stratum+tcp://device.example:3333");
  expect(mock.calls.filter(({ method }) => method === "CheckConfiguration")).toEqual([]);
  expect(mock.workspace.unexpected).toEqual([]);
});

for (const count of [1, 2]) {
  for (const expired of [false, true]) {
    test(`recover initial assignment for ${count} miners after reload (${expired ? "expired unaccepted" : "accepted"})`, async ({
      context,
      page,
      isMobile,
    }, testInfo) => {
      const mock = await mockPoolConfigurations(context);
      const picker = new EditPoolPage(page, isMobile);
      const recovery = new ConfirmationRecoveryPage(page, isMobile);
      await picker.openAccountSelection(clientA, count);
      await picker.selectConfiguration("Production");
      mock.loseNextReply();
      mock.expireUnaccepted(expired);
      await picker.assignExpectingUnknown(count);
      await page.reload();
      await recovery.retry("fresh-password");
      await recovery.validateOutcome(expired, testInfo.outputPath("expired-confirmation.png"));
      expect(mock.calls.filter(({ method }) => method === "ConfirmReview").map(({ body }) => body.reviewId)).toEqual([
        "assignment-review",
        "assignment-review",
      ]);
      expect(mock.calls.filter(({ method }) => method === "ReviewAssignment")).toHaveLength(1);
      await page.goto(`/accounts/${clientA}/assets/miner/42`);
      await page.reload();
      await expect(page.getByRole("button", { name: "Edit pool", exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "Review update", exact: true })).toHaveCount(0);
      const persisted = await page.evaluate(() => sessionStorage.getItem("proto-pool-confirmation-recovery"));
      expect(JSON.parse(persisted!).state.attempts).toEqual([]);
      expect(mock.workspace.unexpected).toEqual([]);
    });
  }
}
