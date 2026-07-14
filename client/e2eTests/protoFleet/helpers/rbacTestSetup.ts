import { type Browser, type Page, type TestInfo } from "@playwright/test";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { DEFAULT_TIMEOUT, testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { AlertsPage } from "../pages/alerts";
import { AuthPage } from "../pages/auth";
import { EnergyPage } from "../pages/energy";
import { FleetLocationsPage } from "../pages/fleetLocations";
import { MinersPage } from "../pages/miners";
import { NewPoolModalPage } from "../pages/newPoolModal";
import { RacksPage } from "../pages/racks";
import { SettingsPage } from "../pages/settings";
import { SettingsCurtailmentPage } from "../pages/settingsCurtailment";
import { SettingsPoolsPage } from "../pages/settingsPools";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";
import { SettingsTeamPage } from "../pages/settingsTeam";
import { CommonSteps } from "./commonSteps";
import { installAllSitesInitScript } from "./fleetLocationsSetup";
import { generateRandomText } from "./testDataHelper";

export const ALERTS_E2E_ENABLED = process.env.E2E_ALERTS_ENABLED === "true";
export const MEMBER_PASSWORD = "Password123!";
export const VALID_POOL_URL = "stratum+tcp://mine.ocean.xyz:3334";
export const REACHABLE_WEBHOOK_URL = "http://otel-collector:13133/healthz";
export const RBAC_ROLE_PREFIX = "rbac_role";
export const RBAC_USER_PREFIX = "rbac_user";
export const RBAC_POOL_PREFIX = "rbac_pool";
export const RBAC_ALERT_CHANNEL_PREFIX = "rbac_alert_channel";
export const RBAC_SCHEDULE_PREFIX = "rbac_schedule";
export const RBAC_SITE_PREFIX = "rbac_site";
export const RBAC_BUILDING_PREFIX = "rbac_building";
export const RBAC_RACK_PREFIX = "rbac_rack";
export const RBAC_CURTAILMENT_PROFILE_PREFIX = "rbac_curtailment_profile";
export const RBAC_CURTAILMENT_SOURCE_PREFIX = "rbac_curtailment_source";
export const RBAC_CURTAILMENT_REASON_PREFIX = "rbac_curtailment_reason";
export const RBAC_RACK_ZONE = "RbacZone";
const SHORT_HOOK_TIMEOUT = DEFAULT_TIMEOUT / 6;

type RbacCleanupTarget = "alerts" | "curtailment" | "infrastructure" | "pools" | "schedules" | "team";

const DEFAULT_RBAC_CLEANUP_TARGETS: RbacCleanupTarget[] = [
  "curtailment",
  "schedules",
  "alerts",
  "pools",
  "infrastructure",
  "team",
];

type PersistedAdminStorageState = Exclude<
  NonNullable<Parameters<Browser["newContext"]>[0]>["storageState"],
  string | undefined
>;

type AdminStorageState = {
  cookies: PersistedAdminStorageState["cookies"];
  origins: PersistedAdminStorageState["origins"];
};

const helpersDir = path.dirname(fileURLToPath(import.meta.url));
const adminStorageStatePath = path.join(helpersDir, "..", "playwright", ".auth", "admin.json");

function loadAdminStorageState(): {
  localStorageEntries: Array<{ name: string; value: string }>;
  storageState: AdminStorageState;
} {
  try {
    const storageState = JSON.parse(fs.readFileSync(adminStorageStatePath, "utf8")) as AdminStorageState;
    return {
      localStorageEntries: storageState.origins?.flatMap((origin) => origin.localStorage ?? []) ?? [],
      storageState,
    };
  } catch (error) {
    if ((error as { code?: string }).code === "ENOENT") {
      throw Object.assign(
        new Error(
          `Missing admin auth state at ${adminStorageStatePath}. Run 02-saveAuthState.spec.ts or the auth setup dependency before RBAC tests that need stored admin context.`,
        ),
        { cause: error },
      );
    }

    throw error;
  }
}

type ProvisionedMember = {
  roleName: string;
  username: string;
};

type ProvisionedMemberWithTemporaryPassword = ProvisionedMember & {
  temporaryPassword: string;
};

type CleanupPages = {
  alertsPage: AlertsPage;
  commonSteps: CommonSteps;
  energyPage: EnergyPage;
  fleetLocationsPage: FleetLocationsPage;
  page: Page;
  racksPage: RacksPage;
  settingsPage: SettingsPage;
  settingsCurtailmentPage: SettingsCurtailmentPage;
  settingsPoolsPage: SettingsPoolsPage;
  settingsSchedulesPage: SettingsSchedulesPage;
  settingsTeamPage: SettingsTeamPage;
};

export function useRbacHooks(cleanupTargets: RbacCleanupTarget[] = DEFAULT_RBAC_CLEANUP_TARGETS) {
  test.beforeEach(async ({ page }) => {
    await installAllSitesInitScript(page);
    await page.goto("/");
  });

  test.afterEach("CLEANUP: delete RBAC fixtures", async ({ browser }, testInfo) => {
    await cleanupRbacArtifacts(browser, testInfo, cleanupTargets);
  });
}

export async function provisionRoleAndLogin(
  commonSteps: CommonSteps,
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
): Promise<ProvisionedMember> {
  const roleName = generateRandomText(RBAC_ROLE_PREFIX);
  const username = generateRandomText(RBAC_USER_PREFIX);

  const temporaryPassword = await commonSteps.createRoleAndTeamMember({
    roleName,
    roleDescription,
    permissionKeys,
    username,
  });

  await commonSteps.completeFirstLoginAsTeamMember({
    username,
    temporaryPassword,
    newPassword: MEMBER_PASSWORD,
  });

  return { roleName, username };
}

export async function provisionRoleViaStoredAdminContext(
  browser: Browser,
  testInfo: TestInfo,
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
): Promise<ProvisionedMemberWithTemporaryPassword> {
  return await withStoredAdminContext(browser, testInfo, async ({ commonSteps }) => {
    const roleName = generateRandomText(RBAC_ROLE_PREFIX);
    const username = generateRandomText(RBAC_USER_PREFIX);

    const temporaryPassword = await commonSteps.createRoleAndTeamMember({
      roleName,
      roleDescription,
      permissionKeys,
      username,
    });

    return { roleName, temporaryPassword, username };
  });
}

export async function provisionRoleAndLoginViaStoredAdminContext(
  browser: Browser,
  testInfo: TestInfo,
  commonSteps: CommonSteps,
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
): Promise<ProvisionedMember> {
  const provisionedMember = await provisionRoleViaStoredAdminContext(browser, testInfo, {
    permissionKeys,
    roleDescription,
  });

  await commonSteps.completeFirstLoginAsTeamMember({
    username: provisionedMember.username,
    temporaryPassword: provisionedMember.temporaryPassword,
    newPassword: MEMBER_PASSWORD,
  });

  return {
    roleName: provisionedMember.roleName,
    username: provisionedMember.username,
  };
}

export async function createPool(
  settingsPage: SettingsPage,
  settingsPoolsPage: SettingsPoolsPage,
  newPoolModal: NewPoolModalPage,
  {
    poolName,
    poolUsername,
  }: {
    poolName: string;
    poolUsername: string;
  },
) {
  await test.step(`Create pool "${poolName}"`, async () => {
    await settingsPage.navigateToMiningPoolsSettings();
    await settingsPoolsPage.validateMiningPoolsPageOpened();
    await settingsPoolsPage.clickAddPool();
    await newPoolModal.validatePoolModalOpened();
    await newPoolModal.inputPoolName(poolName);
    await newPoolModal.inputPoolUrl(VALID_POOL_URL);
    await newPoolModal.inputPoolUsername(poolUsername);
    await newPoolModal.clickSaveNewPool();
    await settingsPoolsPage.validateTextInToast("Pool added");
    await settingsPoolsPage.validatePoolEntryByUniqueName(poolName, VALID_POOL_URL, poolUsername);
  });
}

export async function createAlertChannelAsAdmin(alertsPage: AlertsPage, channelName: string) {
  await test.step(`Create alerts channel "${channelName}" as admin`, async () => {
    await alertsPage.navigateToAlertsSettings();
    await alertsPage.validateAlertsPageOpened();
    await alertsPage.openAddChannelModal();
    await alertsPage.fillWebhookChannel(channelName, REACHABLE_WEBHOOK_URL);
    await alertsPage.saveChannel();
    await alertsPage.validateChannelListed(channelName);
  });
}

export async function createSchedule(settingsSchedulesPage: SettingsSchedulesPage, scheduleName: string) {
  await test.step(`Create schedule "${scheduleName}"`, async () => {
    await settingsSchedulesPage.navigateToSchedulesSettings();
    await settingsSchedulesPage.validateSchedulesPageOpened();
    await settingsSchedulesPage.clickAddSchedule();
    await settingsSchedulesPage.inputScheduleName(scheduleName);
    await settingsSchedulesPage.selectStartDate(1);
    await settingsSchedulesPage.openMinersTargetSelector();
    await settingsSchedulesPage.waitForMinerSelectionModalToLoad();
    await settingsSchedulesPage.selectFirstMiners(1);
    await settingsSchedulesPage.confirmMinerSelection();
    await settingsSchedulesPage.clickSaveSchedule();
    await settingsSchedulesPage.validateScheduleVisible(scheduleName);
  });
}

export async function createInfrastructureFixturesAsAdmin(
  fleetLocationsPage: FleetLocationsPage,
  racksPage: RacksPage,
  {
    siteName,
    buildingName,
    rackLabel,
  }: {
    siteName: string;
    buildingName: string;
    rackLabel: string;
  },
) {
  await test.step("Create site, building, and rack as admin", async () => {
    await fleetLocationsPage.createSite(siteName);
    await fleetLocationsPage.createBuilding(siteName, buildingName);
    await createRack(racksPage, rackLabel);
  });
}

export async function createRack(racksPage: RacksPage, rackLabel: string) {
  await test.step(`Create rack "${rackLabel}"`, async () => {
    await racksPage.navigateToRacksPage();
    await racksPage.clickAddRackButton();
    await racksPage.inputZone(RBAC_RACK_ZONE);
    await racksPage.inputRackLabel(rackLabel);
    await racksPage.enableCustomRackLayout();
    await racksPage.inputColumns(2);
    await racksPage.inputRows(2);
    await racksPage.clickContinueFromRackSettings();
    await racksPage.clickSaveRack();
    await racksPage.validateRackToast(rackLabel);
    await racksPage.clickViewList();
    await racksPage.waitForRackListToLoad({ allowEmpty: false });
    await racksPage.validateRackRow(rackLabel, RBAC_RACK_ZONE, 0);
  });
}

export async function wakeRigMinerIfSleeping(minersPage: MinersPage, minerIp: string) {
  if ((await minersPage.getMinerStatus(minerIp).catch(() => "")).trim() !== "Sleeping") {
    return;
  }

  await minersPage.clickMinerThreeDotsButton(minerIp);
  await minersPage.clickWakeUpButton();
  await minersPage.clickWakeUpConfirm();
  await minersPage.validateMinerStatusSettled(minerIp, "Hashing");
}

export async function ensureVisibleRigMinersAwake(minersPage: MinersPage) {
  await minersPage.filterRigMiners();

  if (await minersPage.hasAnyMinerWithStatus("Waking")) {
    await minersPage.validateNoMinerWithStatus("Waking", SHORT_HOOK_TIMEOUT);
  }

  while (await minersPage.hasAnyMinerWithStatus("Sleeping")) {
    const sleepingMinerIp = await minersPage.getMinerIpAddressByStatus("Sleeping");
    await minersPage.clickMinerThreeDotsButton(sleepingMinerIp);
    await minersPage.clickWakeUpButton();
    await minersPage.clickWakeUpConfirm();
    await minersPage.validateMinerStatusSettled(sleepingMinerIp, "Hashing");
  }

  await minersPage.validateNoMinerWithStatus("Sleeping", SHORT_HOOK_TIMEOUT);
  await minersPage.validateNoMinerWithStatus("Waking", SHORT_HOOK_TIMEOUT);
}

export async function createCurtailment(energyPage: EnergyPage, reason: string) {
  if (testConfig.target === "real") {
    throw new Error("RBAC curtailment setup is only supported against fake targets.");
  }

  await test.step(`Create curtailment "${reason}"`, async () => {
    await energyPage.navigateToEnergyPage();
    await energyPage.validateEnergyPageOpened();
    await energyPage.openCurtailmentPlanner();
    await energyPage.fillCurtailmentPlan({
      reason,
      targetKw: "1",
      restoreBatchIntervalSec: "60",
    });
    await energyPage.waitForPreview("1");
    await energyPage.startCurtailment();
    await energyPage.validateActiveCurtailment(reason);
  });
}

export async function invokeIngestCurtailmentSignal(page: Page, externalReference: string) {
  return await test.step("Call IngestCurtailmentSignal with the current logged-in user", async () => {
    return await page.evaluate(
      async ({ externalReference }) => {
        const response = await fetch("/api-proxy/curtailment.v1.CurtailmentService/IngestCurtailmentSignal", {
          method: "POST",
          headers: {
            "Connect-Protocol-Version": "1",
            "Content-Type": "application/json",
          },
          credentials: "include",
          body: JSON.stringify({
            externalSource: "rbac-e2e",
            externalReference,
            signalPayload: btoa(JSON.stringify({ dispatch_id: externalReference })),
            reason: "rbac ingest test",
          }),
        });

        return {
          body: await response.text(),
          ok: response.ok,
          status: response.status,
        };
      },
      { externalReference },
    );
  });
}

export async function cleanupRbacTeamArtifacts(browser: Browser, testInfo: TestInfo) {
  await cleanupRbacArtifacts(browser, testInfo, ["team"]);
}

async function cleanupRbacArtifacts(browser: Browser, testInfo: TestInfo, cleanupTargets: RbacCleanupTarget[]) {
  const cleanupTargetSet = new Set(cleanupTargets);

  await withStoredAdminContext(
    browser,
    testInfo,
    async ({
      alertsPage,
      energyPage,
      fleetLocationsPage,
      page,
      racksPage,
      settingsCurtailmentPage,
      settingsPoolsPage,
      settingsSchedulesPage,
      settingsTeamPage,
    }) => {
      if (cleanupTargetSet.has("curtailment")) {
        await energyPage
          .cleanupStartedCurtailmentsByReasonPrefix(RBAC_CURTAILMENT_REASON_PREFIX)
          .catch(() => undefined);

        await page.goto("/settings/curtailment");
        if (
          await page
            .getByRole("button", { name: "Add source", exact: true })
            .isVisible()
            .catch(() => false)
        ) {
          await settingsCurtailmentPage.deleteSourcesByPrefix(RBAC_CURTAILMENT_SOURCE_PREFIX).catch(() => undefined);
          await settingsCurtailmentPage
            .deleteResponseProfilesByPrefix(RBAC_CURTAILMENT_PROFILE_PREFIX)
            .catch(() => undefined);
        }
      }

      if (cleanupTargetSet.has("schedules")) {
        await page.goto("/settings/schedules");
        if (
          await page
            .getByRole("button", { name: "Add a schedule", exact: true })
            .isVisible()
            .catch(() => false)
        ) {
          await settingsSchedulesPage.deleteSchedulesByPrefix(RBAC_SCHEDULE_PREFIX).catch(() => undefined);
        }
      }

      if (cleanupTargetSet.has("alerts") && ALERTS_E2E_ENABLED) {
        await page.goto("/settings/alerts");
        if (
          await page
            .getByRole("button", { name: "Add channel", exact: true })
            .isVisible()
            .catch(() => false)
        ) {
          await alertsPage.deleteChannelsByPrefix(RBAC_ALERT_CHANNEL_PREFIX).catch(() => undefined);
        }
      }

      if (cleanupTargetSet.has("pools")) {
        await page.goto("/settings/mining-pools");
        if (
          await page
            .getByRole("button", { name: "Add pool", exact: true })
            .isVisible()
            .catch(() => false)
        ) {
          await settingsPoolsPage.deletePoolsByPrefix(RBAC_POOL_PREFIX).catch(() => undefined);
        }
      }

      if (cleanupTargetSet.has("infrastructure")) {
        await cleanupInfrastructureFixtures(fleetLocationsPage, racksPage).catch(() => undefined);
      }

      if (cleanupTargetSet.has("team")) {
        await page.goto("/settings/team");
        if (
          await settingsTeamPage
            .openMembersTab(SHORT_HOOK_TIMEOUT)
            .then(() => true)
            .catch(() => false)
        ) {
          await settingsTeamPage.deactivateMembersByPrefix(RBAC_USER_PREFIX).catch(() => undefined);
        }
        if (
          await settingsTeamPage
            .openRolesTab(SHORT_HOOK_TIMEOUT)
            .then(() => true)
            .catch(() => false)
        ) {
          await settingsTeamPage.deleteRolesByPrefix(RBAC_ROLE_PREFIX).catch(() => undefined);
        }
      }
    },
  );
}

async function cleanupInfrastructureFixtures(fleetLocationsPage: FleetLocationsPage, racksPage: RacksPage) {
  await racksPage.navigateToRacksPage();
  await racksPage.tryAction(() => racksPage.clickViewList());
  await racksPage.waitForRackListToLoad();

  const rackNames = (await racksPage.listRackNames()).filter((name) => name.startsWith(RBAC_RACK_PREFIX));
  for (const rackName of rackNames) {
    await racksPage.deleteRackByLabelIfVisible(rackName);
  }

  const buildingNames = (await fleetLocationsPage.listBuildingNames()).filter((name) =>
    name.startsWith(RBAC_BUILDING_PREFIX),
  );
  for (const buildingName of buildingNames) {
    await fleetLocationsPage.deleteBuildingByNameIfVisible(buildingName);
  }

  const siteNames = (await fleetLocationsPage.listSiteNames()).filter((name) => name.startsWith(RBAC_SITE_PREFIX));
  for (const siteName of siteNames) {
    await fleetLocationsPage.deleteSiteByNameIfVisible(siteName);
  }
}

async function withStoredAdminContext<T>(
  browser: Browser,
  testInfo: TestInfo,
  callback: (pages: CleanupPages) => Promise<T>,
): Promise<T> {
  const isMobile = testInfo.project.use?.isMobile ?? false;
  const { localStorageEntries, storageState } = loadAdminStorageState();
  const context = await browser.newContext({
    baseURL: testConfig.baseUrl,
    storageState,
    viewport: testInfo.project.use?.viewport,
  });

  try {
    const page = await context.newPage();
    await page.addInitScript((entries: Array<{ name: string; value: string }>) => {
      for (const entry of entries) {
        localStorage.setItem(entry.name, entry.value);
      }
    }, localStorageEntries);
    await installAllSitesInitScript(page);
    await page.goto("/");

    const authPage = new AuthPage(page, isMobile);
    const minersPage = new MinersPage(page, isMobile);
    const settingsPage = new SettingsPage(page, isMobile);
    const settingsTeamPage = new SettingsTeamPage(page, isMobile);
    const settingsPoolsPage = new SettingsPoolsPage(page, isMobile);
    const settingsSchedulesPage = new SettingsSchedulesPage(page, isMobile);
    const settingsCurtailmentPage = new SettingsCurtailmentPage(page, isMobile);
    const alertsPage = new AlertsPage(page, isMobile);
    const fleetLocationsPage = new FleetLocationsPage(page, isMobile);
    const racksPage = new RacksPage(page, isMobile);
    const energyPage = new EnergyPage(page, isMobile);
    const commonSteps = new CommonSteps(authPage, minersPage, settingsPage, settingsTeamPage);

    await commonSteps.loginAsAdmin();

    return await callback({
      alertsPage,
      commonSteps,
      energyPage,
      fleetLocationsPage,
      page,
      racksPage,
      settingsCurtailmentPage,
      settingsPage,
      settingsPoolsPage,
      settingsSchedulesPage,
      settingsTeamPage,
    });
  } finally {
    await context.close();
  }
}
