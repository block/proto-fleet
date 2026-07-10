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
    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Read-only mining pool access for RBAC coverage.",
      permissionKeys: ["pool:read"],
    });

    await validateManageOnlySettingsRouteHidden(page, {
      route: "/settings/mining-pools",
      validateSubmenuHidden: () => settingsPoolsPage.validateMiningPoolsSubmenuHidden(),
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

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage mining pools for RBAC coverage.",
      permissionKeys: ["pool:read", "pool:manage"],
    });

    await settingsPage.navigateToMiningPoolsSettings();
    await settingsPoolsPage.validateMiningPoolsPageOpened();

    await createPool(settingsPage, settingsPoolsPage, newPoolModal, {
      poolName,
      poolUsername,
    });

    await settingsPoolsPage.deletePoolByNameIfVisible(poolName);
    await settingsPoolsPage.validateTextInToast("Pool deleted");
  });

  test("Alerts read-only role can view alerts without channel management", async ({ alertsPage, commonSteps }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      !ALERTS_E2E_ENABLED,
      "Requires the alerts sidecar + VITE_ALERTS_ENABLED; set E2E_ALERTS_ENABLED=true to run.",
    );

    const channelName = generateRandomText(RBAC_ALERT_CHANNEL_PREFIX);

    await commonSteps.loginAsAdmin({ forceReauth: true });
    await createAlertChannelAsAdmin(alertsPage, channelName);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Read-only alerts access for RBAC coverage.",
      permissionKeys: ["alert:read"],
    });

    await alertsPage.navigateToAlertsSettings();
    await alertsPage.validateAlertsPageOpened();
    await alertsPage.validateChannelListed(channelName);
    await alertsPage.validateAddChannelHidden();
  });

  test("Alerts manage role can create and delete channels", async ({ alertsPage, commonSteps }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      !ALERTS_E2E_ENABLED,
      "Requires the alerts sidecar + VITE_ALERTS_ENABLED; set E2E_ALERTS_ENABLED=true to run.",
    );

    const channelName = generateRandomText(RBAC_ALERT_CHANNEL_PREFIX);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage alerts for RBAC coverage.",
      permissionKeys: ["alert:read", "alert:manage", "miner:read"],
    });

    await alertsPage.navigateToAlertsSettings();
    await alertsPage.validateAlertsPageOpened();
    await alertsPage.openAddChannelModal();
    await alertsPage.fillWebhookChannel(channelName, REACHABLE_WEBHOOK_URL);
    await alertsPage.saveChannel();
    await alertsPage.validateChannelListed(channelName);
    await alertsPage.deleteChannel(channelName);
  });

  test("Schedules read-only role cannot access the Schedules settings surface", async ({
    page,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Read-only schedules access for RBAC coverage.",
      permissionKeys: ["schedule:read"],
    });

    await validateManageOnlySettingsRouteHidden(page, {
      route: "/settings/schedules",
      validateSubmenuHidden: () => settingsSchedulesPage.validateSchedulesSubmenuHidden(),
    });
  });

  test("Schedules manage role can create and delete schedules", async ({ commonSteps, settingsSchedulesPage }) => {
    const scheduleName = generateRandomText(RBAC_SCHEDULE_PREFIX);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage schedules for RBAC coverage.",
      permissionKeys: ["schedule:manage", "miner:set_power_target"],
    });

    await createSchedule(settingsSchedulesPage, scheduleName);
    await settingsSchedulesPage.deleteSchedule(scheduleName);
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

    await commonSteps.loginAsAdmin({ forceReauth: true });
    await createCurtailment(energyPage, reason);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Read-only curtailment access for RBAC coverage.",
      permissionKeys: ["curtailment:read"],
    });

    await energyPage.navigateToEnergyPage();
    await energyPage.validateEnergyPageOpened();
    await energyPage.validateRunCurtailmentButtonHidden();
    await energyPage.validateActiveCurtailment(reason);
    await energyPage.validateActiveCurtailmentManageActionsHidden(reason);
  });

  test("Curtailment manage role can preview, start, and stop a curtailment", async ({ commonSteps, energyPage }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "Curtailment RBAC E2E creates whole-fleet curtailments and is only supported against fake targets.",
    );

    const reason = generateRandomText(RBAC_CURTAILMENT_REASON_PREFIX);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage curtailment for RBAC coverage.",
      permissionKeys: ["curtailment:manage"],
    });

    await createCurtailment(energyPage, reason);
    await energyPage.stopCurtailment({ reason });
    await energyPage.waitForCurtailmentToRestore({ reason });
  });

  test("Curtailment ingest permission reaches the ingest RPC while manage-only is denied", async ({
    authPage,
    browser,
    commonSteps,
    page,
  }, testInfo) => {
    const deniedReference = generateRandomText("rbac_ingest_denied");
    const allowedReference = generateRandomText("rbac_ingest_allowed");

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage-only curtailment role for ingest denial coverage.",
      permissionKeys: ["curtailment:manage"],
    });

    const deniedResult = await invokeIngestCurtailmentSignal(page, deniedReference);
    expectConnectError(deniedResult, "permission_denied");

    const ingestMember = await provisionRoleViaStoredAdminContext(browser, testInfo, {
      roleDescription: "Ingest-only curtailment role for RPC coverage.",
      permissionKeys: ["curtailment:ingest"],
    });

    await authPage.logout();
    await authPage.validateRedirectedToAuth();
    await commonSteps.completeFirstLoginAsTeamMember({
      username: ingestMember.username,
      temporaryPassword: ingestMember.temporaryPassword,
      newPassword: MEMBER_PASSWORD,
    });

    const allowedResult = await invokeIngestCurtailmentSignal(page, allowedReference);
    expectConnectSuccessfulOrUnimplemented(allowedResult);
  });

  test("Sites, buildings, and racks read-only role can view infrastructure without create actions", async ({
    commonSteps,
    fleetLocationsPage,
    racksPage,
  }) => {
    const siteName = generateRandomText(RBAC_SITE_PREFIX);
    const buildingName = generateRandomText(RBAC_BUILDING_PREFIX);
    const rackLabel = generateRandomText(RBAC_RACK_PREFIX);

    await commonSteps.loginAsAdmin({ forceReauth: true });
    await createInfrastructureFixturesAsAdmin(fleetLocationsPage, racksPage, {
      siteName,
      buildingName,
      rackLabel,
    });

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Read-only infrastructure access for RBAC coverage.",
      permissionKeys: ["site:read", "rack:read"],
    });

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

  test("Sites, buildings, and racks manage role can create infrastructure", async ({
    commonSteps,
    fleetLocationsPage,
    racksPage,
  }) => {
    const siteName = generateRandomText(RBAC_SITE_PREFIX);
    const buildingName = generateRandomText(RBAC_BUILDING_PREFIX);
    const rackLabel = generateRandomText(RBAC_RACK_PREFIX);

    await provisionRoleAndLogin(commonSteps, {
      roleDescription: "Manage infrastructure for RBAC coverage.",
      permissionKeys: ["site:read", "site:manage", "rack:read", "rack:manage"],
    });

    await fleetLocationsPage.createSite(siteName);
    await fleetLocationsPage.createBuilding(siteName, buildingName);
    await createRack(racksPage, rackLabel);

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
