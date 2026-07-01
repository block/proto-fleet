import { type Browser, type Page, type TestInfo } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { AuthPage } from "../pages/auth";
import { FleetLocationsPage } from "../pages/fleetLocations";
import { MinersPage } from "../pages/miners";
import { CommonSteps } from "./commonSteps";
import { generateRandomText } from "./testDataHelper";

const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";
const AUTOMATION_SITE_BASE_PREFIX = "automation_site_detail_site";
const AUTOMATION_BUILDING_BASE_PREFIX = "automation_site_detail_building";

type SiteDetailRunPrefixes = {
  sitePrefix: string;
  buildingPrefix: string;
};

const testRunPrefixes = new Map<string, SiteDetailRunPrefixes>();

type SitesDetailCleanupFleetLocationsPage = Pick<
  FleetLocationsPage,
  "deleteBuildingByNameIfVisible" | "deleteSiteByNameIfVisible" | "listBuildingNames" | "listSiteNames"
>;

export type SiteDetailScenarioData = {
  siteName: string;
  renamedSiteName: string;
  siblingSiteName: string;
  buildingName: string;
  city: string;
  powerCapacityMw: string;
};

function getTestRunKey(testInfo: TestInfo): string {
  return [testInfo.project.name, testInfo.testId, testInfo.workerIndex, testInfo.retry, testInfo.repeatEachIndex].join(
    ":",
  );
}

function getSafeProjectName(testInfo: TestInfo): string {
  return (
    testInfo.project.name
      .replace(/[^a-zA-Z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "")
      .toLowerCase() || "project"
  );
}

function createRunPrefixes(testInfo: TestInfo): SiteDetailRunPrefixes {
  const runPrefix = `${getSafeProjectName(testInfo)}_${testInfo.workerIndex}_${testInfo.retry}`;

  return {
    sitePrefix: generateRandomText(`${AUTOMATION_SITE_BASE_PREFIX}_${runPrefix}`),
    buildingPrefix: generateRandomText(`${AUTOMATION_BUILDING_BASE_PREFIX}_${runPrefix}`),
  };
}

function getRunPrefixes(testInfo: TestInfo): SiteDetailRunPrefixes {
  const prefixes = testRunPrefixes.get(getTestRunKey(testInfo));
  if (!prefixes) {
    throw new Error("Site detail E2E run prefixes were not initialized.");
  }

  return prefixes;
}

export function createSiteDetailScenarioData(testInfo: TestInfo): SiteDetailScenarioData {
  const prefixes = getRunPrefixes(testInfo);

  return {
    siteName: generateRandomText(`${prefixes.sitePrefix}_primary`),
    renamedSiteName: generateRandomText(`${prefixes.sitePrefix}_renamed`),
    siblingSiteName: generateRandomText(`${prefixes.sitePrefix}_sibling`),
    buildingName: generateRandomText(`${prefixes.buildingPrefix}_primary`),
    city: "Chicago",
    powerCapacityMw: "12.5",
  };
}

async function installAllSitesInitScript(page: Page) {
  await page.addInitScript(
    ({ storageKey }) => {
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          state: {
            ui: {
              activeSite: { kind: "all" },
            },
          },
          version: 0,
        }),
      );
    },
    { storageKey: ACTIVE_SITE_STORAGE_KEY },
  );
}

async function cleanupAutomationFixtures(
  fleetLocationsPage: SitesDetailCleanupFleetLocationsPage,
  prefixes: SiteDetailRunPrefixes,
) {
  const buildingNames = (await fleetLocationsPage.listBuildingNames()).filter((name) =>
    name.startsWith(prefixes.buildingPrefix),
  );
  for (const buildingName of buildingNames) {
    await fleetLocationsPage.deleteBuildingByNameIfVisible(buildingName);
  }

  const siteNames = (await fleetLocationsPage.listSiteNames()).filter((name) => name.startsWith(prefixes.sitePrefix));
  for (const siteName of siteNames) {
    await fleetLocationsPage.deleteSiteByNameIfVisible(siteName);
  }
}

async function cleanupAutomationSites(browser: Browser, testInfo: TestInfo) {
  const prefixes = getRunPrefixes(testInfo);
  const isMobile = testInfo.project.use?.isMobile ?? false;
  const context = await browser.newContext({
    baseURL: testConfig.baseUrl,
    viewport: testInfo.project.use?.viewport,
  });

  try {
    const page = await context.newPage();
    await installAllSitesInitScript(page);
    await page.goto("/");

    const authPage = new AuthPage(page, isMobile);
    const minersPage = new MinersPage(page, isMobile);
    const fleetLocationsPage = new FleetLocationsPage(page, isMobile);
    const commonSteps = new CommonSteps(authPage, minersPage);

    await commonSteps.loginAsAdmin();
    await cleanupAutomationFixtures(fleetLocationsPage, prefixes);
  } finally {
    await context.close();
  }
}

export function useSiteDetailHooks() {
  test.beforeEach(async ({ page, commonSteps, fleetLocationsPage }, testInfo) => {
    testRunPrefixes.set(getTestRunKey(testInfo), createRunPrefixes(testInfo));

    await installAllSitesInitScript(page);
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await cleanupAutomationFixtures(fleetLocationsPage, getRunPrefixes(testInfo));
  });

  test.afterEach("CLEANUP: Delete automation site detail fixtures", async ({ browser }, testInfo) => {
    try {
      await cleanupAutomationSites(browser, testInfo);
    } finally {
      testRunPrefixes.delete(getTestRunKey(testInfo));
    }
  });
}

export async function createSiteDetailSites(
  fleetLocationsPage: FleetLocationsPage,
  scenario: Pick<SiteDetailScenarioData, "siteName" | "siblingSiteName">,
) {
  await test.step("Create the sites used by the detail scenarios", async () => {
    await fleetLocationsPage.createSite(scenario.siteName);
    await fleetLocationsPage.createSite(scenario.siblingSiteName);
  });
}
