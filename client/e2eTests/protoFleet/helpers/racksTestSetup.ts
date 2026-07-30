import { type Page } from "@playwright/test";
import { DEFAULT_TIMEOUT, testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { RacksPage } from "../pages/racks";
import { SettingsPage } from "../pages/settings";
import { SettingsPoolsPage } from "../pages/settingsPools";
import { CommonSteps } from "./commonSteps";

export const VALID_POOL_URL = "stratum+tcp://mine.ocean.xyz:3334";
export const AUTOMATION_ZONE = "AutomationZone";
// Racks no longer auto-generate a label from the zone, so tests type one
// explicitly. The afterEach deletes all racks, so a shared constant is safe
// for single-rack tests (no cross-test label collisions).
export const RACK_LABEL = "AutomationRack";
export const RACK_COLUMNS = 2;
export const RACK_ROWS = 2;
export const VALIDATION_RACK_COLUMNS = 1;
export const VALIDATION_RACK_ROWS = 1;
export const NETWORK_RACK_COLUMNS = 9;
export const NETWORK_RACK_ROWS = 9;
export const LARGE_RACK_COLUMNS = 3;
export const LARGE_RACK_ROWS = 3;
export const OVERVIEW_RACK_COLUMNS = 8;
export const OVERVIEW_RACK_ROWS = 2;
const SHORT_CLEANUP_TIMEOUT = DEFAULT_TIMEOUT / 6;
export const ORDER_INDEX_SCENARIOS = [
  { label: "Bottom left", expectedNumbers: [3, 4, 1, 2] },
  { label: "Top left", expectedNumbers: [1, 2, 3, 4] },
  { label: "Bottom right", expectedNumbers: [4, 3, 2, 1] },
  { label: "Top right", expectedNumbers: [2, 1, 4, 3] },
] as const;

export async function cleanupPoolIfPageOpen(
  page: Page,
  settingsPage: SettingsPage,
  settingsPoolsPage: SettingsPoolsPage,
  poolName: string,
) {
  if (page.isClosed()) {
    return;
  }

  const closeAssignPoolsButton = page.getByLabel("Close assign pools");
  if (await closeAssignPoolsButton.isVisible().catch(() => false)) {
    await closeAssignPoolsButton.click();
  }

  await settingsPage.navigateToMiningPoolsSettings();
  await settingsPoolsPage.deletePoolByNameIfVisible(poolName);
}

async function cleanupAllRacks(racksPage: RacksPage) {
  await racksPage.navigateToRacksPage();
  await racksPage.tryAction(() => racksPage.clickViewList(SHORT_CLEANUP_TIMEOUT), SHORT_CLEANUP_TIMEOUT);
  if (
    !(await racksPage.tryAction(
      () => racksPage.waitForRackListToLoad({ timeout: SHORT_CLEANUP_TIMEOUT }),
      SHORT_CLEANUP_TIMEOUT,
    ))
  ) {
    return;
  }

  let rackNames = await racksPage.listRackNames(SHORT_CLEANUP_TIMEOUT);

  while (rackNames.length > 0) {
    await racksPage.deleteRackByLabelIfVisible(rackNames[0], SHORT_CLEANUP_TIMEOUT);
    rackNames = await racksPage.listRackNames(SHORT_CLEANUP_TIMEOUT);
  }
}

// Registers the shared login/navigate beforeEach and the rack-cleanup afterEach
// for a Racks describe block. The rack suites are split across several spec
// files so Playwright can place them on separate shards; this keeps a single
// source of truth for the hooks. Cleanup runs after every test, so with serial
// execution (workers: 1) the suites never observe each other's racks.
export function useRacksHooks() {
  test.beforeEach(async ({ page, commonSteps, racksPage }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await racksPage.navigateToRacksPage();
  });

  test.afterEach("CLEANUP: Delete all racks", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({
      baseURL: testConfig.baseUrl,
      viewport: testInfo.project.use?.viewport,
    });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const racksPage = new RacksPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await cleanupAllRacks(racksPage);
    } finally {
      await context.close();
    }
  });
}
