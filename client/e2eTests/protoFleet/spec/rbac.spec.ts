import type { Page } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import {
  ALERTS_E2E_ENABLED,
  createAlertChannelAsAdmin,
  createCurtailment,
  createInfrastructureFixturesAsAdmin,
  createPool,
  createRack,
  createSchedule,
  invokeIngestCurtailmentSignal,
  markRbacCleanupTargets,
  MEMBER_PASSWORD,
  provisionRoleAndLogin,
  provisionRoleViaStoredAdminContext,
  RBAC_ALERT_CHANNEL_PREFIX,
  RBAC_BUILDING_PREFIX,
  RBAC_CURTAILMENT_REASON_PREFIX,
  RBAC_POOL_PREFIX,
  RBAC_RACK_PREFIX,
  RBAC_RACK_ZONE,
  RBAC_SCHEDULE_PREFIX,
  RBAC_SITE_PREFIX,
  REACHABLE_WEBHOOK_URL,
  useRbacHooks,
} from "../helpers/rbacTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";

async function validateManageOnlySettingsRouteHidden(
  page: Page,
  {
    route,
    validateSubmenuHidden,
  }: {
    route: string;
    validateSubmenuHidden: () => Promise<void>;
  },
) {
  await page.goto("/settings/preferences");
  await expect(page).toHaveURL(/.*\/settings\/preferences/);
  await validateSubmenuHidden();

  await page.goto(route);
  await expect(page).toHaveURL(/.*\/settings\/preferences/);
  await validateSubmenuHidden();
}

function expectConnectError(result: { body: string; status: number }, code: "permission_denied" | "unimplemented") {
  const normalizedBody = result.body.toLowerCase();
  const summary = JSON.stringify(result);

  if (code === "permission_denied") {
    expect(result.status === 403 || normalizedBody.includes(code), summary).toBeTruthy();
    return;
  }

  expect(result.status === 501 || normalizedBody.includes(code), summary).toBeTruthy();
}

function expectConnectSuccessfulOrUnimplemented(result: { body: string; ok: boolean; status: number }) {
  const normalizedBody = result.body.toLowerCase();
  const summary = JSON.stringify(result);

  expect(normalizedBody.includes("permission_denied"), summary).toBeFalsy();
  expect(normalizedBody.includes("unauthenticated"), summary).toBeFalsy();
  expect(result.ok || result.status === 501 || normalizedBody.includes("unimplemented"), summary).toBeTruthy();
}

test.describe("Proto Fleet - RBAC", () => {
  useRbacHooks();

  test("Pools read-only role cannot access the Pools settings surface", async ({
    page,
    commonSteps,
    settingsPoolsPage,
  }) => {
    await test.step("Provision a read-only pools role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Read-only mining pool access for RBAC coverage.",
        permissionKeys: ["pool:read"],
      });
    });

    await test.step("Validate the Pools settings surface stays inaccessible", async () => {
      await validateManageOnlySettingsRouteHidden(page, {
        route: "/settings/mining-pools",
        validateSubmenuHidden: () => settingsPoolsPage.validateMiningPoolsSubmenuHidden(),
      });
    });
  });

  test("Pools manage role can create and delete mining pools", async ({
    commonSteps,
    newPoolModal,
    settingsPage,
    settingsPoolsPage,
  }) => {
    const poolName = generateRandomText(RBAC_POOL_PREFIX);
    const poolUsername = generateRandomText("rbac_pool_user");
    markRbacCleanupTargets(test.info(), ["pools"]);

    await test.step("Provision a manage-capable pools role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage mining pools for RBAC coverage.",
        permissionKeys: ["pool:read", "pool:manage"],
      });
    });

    await test.step("Open mining pool settings", async () => {
      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
    });

    await test.step("Create a pool with the RBAC manage role", async () => {
      await createPool(settingsPage, settingsPoolsPage, newPoolModal, {
        poolName,
        poolUsername,
      });
    });

    await test.step("Delete the pool again", async () => {
      await settingsPoolsPage.deletePoolByNameIfVisible(poolName);
      await settingsPoolsPage.validateTextInToast("Pool deleted");
    });
  });

  test("Alerts read-only role can view alerts without channel management", async ({ alertsPage, commonSteps }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      !ALERTS_E2E_ENABLED,
      "Requires the alerts sidecar + VITE_ALERTS_ENABLED; set E2E_ALERTS_ENABLED=true to run.",
    );

    const channelName = generateRandomText(RBAC_ALERT_CHANNEL_PREFIX);
    markRbacCleanupTargets(test.info(), ["alerts"]);

    await test.step("Create an alert channel as admin", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await createAlertChannelAsAdmin(alertsPage, channelName);
    });

    await test.step("Provision a read-only alerts role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Read-only alerts access for RBAC coverage.",
        permissionKeys: ["alert:read"],
      });
    });

    await test.step("Validate the channel is visible but channel management stays hidden", async () => {
      await alertsPage.navigateToAlertsSettings();
      await alertsPage.validateAlertsPageOpened();
      await alertsPage.validateChannelListed(channelName);
      await alertsPage.validateAddChannelHidden();
    });
  });

  test("Alerts manage role can create and delete channels", async ({ alertsPage, commonSteps }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      !ALERTS_E2E_ENABLED,
      "Requires the alerts sidecar + VITE_ALERTS_ENABLED; set E2E_ALERTS_ENABLED=true to run.",
    );

    const channelName = generateRandomText(RBAC_ALERT_CHANNEL_PREFIX);
    markRbacCleanupTargets(test.info(), ["alerts"]);

    await test.step("Provision a manage-capable alerts role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage alerts for RBAC coverage.",
        permissionKeys: ["alert:read", "alert:manage", "miner:read"],
      });
    });

    await test.step("Create a channel from Alerts settings", async () => {
      await alertsPage.navigateToAlertsSettings();
      await alertsPage.validateAlertsPageOpened();
      await alertsPage.openAddChannelModal();
      await alertsPage.fillWebhookChannel(channelName, REACHABLE_WEBHOOK_URL);
      await alertsPage.saveChannel();
      await alertsPage.validateChannelListed(channelName);
    });

    await test.step("Delete the created channel", async () => {
      await alertsPage.deleteChannel(channelName);
    });
  });

  test("Schedules read-only role cannot access the Schedules settings surface", async ({
    page,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    await test.step("Provision a read-only schedules role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Read-only schedules access for RBAC coverage.",
        permissionKeys: ["schedule:read"],
      });
    });

    await test.step("Validate the Schedules settings surface stays inaccessible", async () => {
      await validateManageOnlySettingsRouteHidden(page, {
        route: "/settings/schedules",
        validateSubmenuHidden: () => settingsSchedulesPage.validateSchedulesSubmenuHidden(),
      });
    });
  });

  test("Schedules manage role can create and delete schedules", async ({ commonSteps, settingsSchedulesPage }) => {
    const scheduleName = generateRandomText(RBAC_SCHEDULE_PREFIX);
    markRbacCleanupTargets(test.info(), ["schedules"]);

    await test.step("Provision a manage-capable schedules role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage schedules for RBAC coverage.",
        permissionKeys: ["schedule:manage", "miner:set_power_target"],
      });
    });

    await test.step("Create a schedule", async () => {
      await createSchedule(settingsSchedulesPage, scheduleName);
    });

    await test.step("Delete the created schedule", async () => {
      await settingsSchedulesPage.deleteSchedule(scheduleName);
    });
  });

  test("Curtailment read-only role can view the Energy page without manage controls", async ({
    commonSteps,
    energyPage,
  }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "Curtailment RBAC E2E creates whole-fleet curtailments and is only supported against fake targets.",
    );

    const reason = generateRandomText(RBAC_CURTAILMENT_REASON_PREFIX);
    markRbacCleanupTargets(test.info(), ["curtailment"]);

    await test.step("Create an active curtailment as admin", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await createCurtailment(energyPage, reason);
    });

    await test.step("Provision a read-only curtailment role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Read-only curtailment access for RBAC coverage.",
        permissionKeys: ["curtailment:read"],
      });
    });

    await test.step("Validate the active curtailment is visible without manage controls", async () => {
      await energyPage.navigateToEnergyPage();
      await energyPage.validateEnergyPageOpened();
      await energyPage.validateRunCurtailmentButtonHidden();
      await energyPage.validateActiveCurtailment(reason);
      await energyPage.validateActiveCurtailmentManageActionsHidden(reason);
    });
  });

  test("Curtailment manage role can preview, start, and stop a curtailment", async ({ commonSteps, energyPage }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "Curtailment RBAC E2E creates whole-fleet curtailments and is only supported against fake targets.",
    );

    const reason = generateRandomText(RBAC_CURTAILMENT_REASON_PREFIX);
    markRbacCleanupTargets(test.info(), ["curtailment"]);

    await test.step("Provision a manage-capable curtailment role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage curtailment for RBAC coverage.",
        permissionKeys: ["curtailment:manage"],
      });
    });

    await test.step("Create a curtailment", async () => {
      await createCurtailment(energyPage, reason);
    });

    await test.step("Stop the curtailment again", async () => {
      await energyPage.stopCurtailment({ reason });
      await energyPage.waitForCurtailmentToRestore({ reason });
    });
  });

  test("Curtailment ingest permission reaches the ingest RPC while manage-only is denied", async ({
    authPage,
    browser,
    commonSteps,
    page,
  }, testInfo) => {
    const deniedReference = generateRandomText("rbac_ingest_denied");
    const allowedReference = generateRandomText("rbac_ingest_allowed");

    await test.step("Provision a manage-only curtailment role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage-only curtailment role for ingest denial coverage.",
        permissionKeys: ["curtailment:manage"],
      });
    });

    await test.step("Validate the ingest RPC is denied without curtailment:ingest", async () => {
      const deniedResult = await invokeIngestCurtailmentSignal(page, deniedReference);
      expectConnectError(deniedResult, "permission_denied");
    });

    const ingestMember = await test.step("Provision an ingest-only curtailment role", async () => {
      return await provisionRoleViaStoredAdminContext(browser, testInfo, {
        roleDescription: "Ingest-only curtailment role for RPC coverage.",
        permissionKeys: ["curtailment:ingest"],
      });
    });

    await test.step("Log in as the ingest-only member", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
      await commonSteps.completeFirstLoginAsTeamMember({
        username: ingestMember.username,
        temporaryPassword: ingestMember.temporaryPassword,
        newPassword: MEMBER_PASSWORD,
      });
    });

    await test.step("Validate the ingest RPC no longer returns a permission error", async () => {
      const allowedResult = await invokeIngestCurtailmentSignal(page, allowedReference);
      expectConnectSuccessfulOrUnimplemented(allowedResult);
    });
  });

  test("Sites, buildings, and racks read-only role can view infrastructure without create actions", async ({
    commonSteps,
    fleetLocationsPage,
    racksPage,
  }) => {
    const siteName = generateRandomText(RBAC_SITE_PREFIX);
    const buildingName = generateRandomText(RBAC_BUILDING_PREFIX);
    const rackLabel = generateRandomText(RBAC_RACK_PREFIX);
    markRbacCleanupTargets(test.info(), ["infrastructure"]);

    await test.step("Create infrastructure fixtures as admin", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await createInfrastructureFixturesAsAdmin(fleetLocationsPage, racksPage, {
        siteName,
        buildingName,
        rackLabel,
      });
    });

    await test.step("Provision a read-only infrastructure role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Read-only infrastructure access for RBAC coverage.",
        permissionKeys: ["site:read", "rack:read"],
      });
    });

    await test.step("Validate the infrastructure is visible without create controls", async () => {
      await fleetLocationsPage.validateSiteRowCounts(siteName, {
        buildings: 1,
        racks: 0,
        miners: 0,
      });
      await fleetLocationsPage.validateBuildingRowCounts(buildingName, {
        siteName,
        racks: 0,
        miners: 0,
      });
      await racksPage.navigateToRacksPage();
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false, requireManageAccess: false });
      await racksPage.validateRackRow(rackLabel, RBAC_RACK_ZONE, 0);
      await fleetLocationsPage.validateAddSiteButtonHidden();
      await fleetLocationsPage.validateAddBuildingButtonHidden();
      await racksPage.navigateToRacksPage();
      await racksPage.validateAddRackButtonHidden();
    });
  });

  test("Sites, buildings, and racks manage role can create infrastructure", async ({
    commonSteps,
    fleetLocationsPage,
    racksPage,
  }) => {
    const siteName = generateRandomText(RBAC_SITE_PREFIX);
    const buildingName = generateRandomText(RBAC_BUILDING_PREFIX);
    const rackLabel = generateRandomText(RBAC_RACK_PREFIX);
    markRbacCleanupTargets(test.info(), ["infrastructure"]);

    await test.step("Provision a manage-capable infrastructure role", async () => {
      await provisionRoleAndLogin(commonSteps, {
        roleDescription: "Manage infrastructure for RBAC coverage.",
        permissionKeys: ["site:read", "site:manage", "rack:read", "rack:manage"],
      });
    });

    await test.step("Create a site, building, and rack", async () => {
      await fleetLocationsPage.createSite(siteName);
      await fleetLocationsPage.createBuilding(siteName, buildingName);
      await createRack(racksPage, rackLabel);
    });

    await test.step("Validate the infrastructure rows were created", async () => {
      await fleetLocationsPage.validateSiteRowCounts(siteName, {
        buildings: 1,
        racks: 0,
        miners: 0,
      });
      await fleetLocationsPage.validateBuildingRowCounts(buildingName, {
        siteName,
        racks: 0,
        miners: 0,
      });
    });
  });
});
