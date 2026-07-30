import { type Browser, type Page, type TestInfo } from "@playwright/test";
import { DEFAULT_TIMEOUT, testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { AuthPage } from "../pages/auth";
import { FleetLocationsPage } from "../pages/fleetLocations";
import { MinersPage } from "../pages/miners";
import { RacksPage } from "../pages/racks";
import {
  assignRackToBuilding,
  createRackWithAssignedMiners,
  validateRackAndMinerPlacementAcrossTabs,
  validateSiteAndBuildingCounts,
} from "./buildingsTestSetup";
import { CommonSteps } from "./commonSteps";
import { getSafeProjectName, getTestRunKey, installAllSitesInitScript } from "./fleetLocationsSetup";
import { generateRandomText } from "./testDataHelper";
const AUTOMATION_SITE_BASE_PREFIX = "automation_building_detail_site";
const AUTOMATION_BUILDING_BASE_PREFIX = "automation_building_detail_building";
const AUTOMATION_RACK_BASE_PREFIX = "automation_building_detail_rack";
const SHORT_CLEANUP_TIMEOUT = DEFAULT_TIMEOUT / 6;

type BuildingDetailRunPrefixes = {
  sitePrefix: string;
  buildingPrefix: string;
  rackPrefix: string;
};

const testRunPrefixes = new Map<string, BuildingDetailRunPrefixes>();

type BuildingDetailCleanupFleetLocationsPage = Pick<
  FleetLocationsPage,
  "deleteBuildingByNameIfVisible" | "deleteSiteByNameIfVisible" | "listBuildingNames" | "listSiteNames"
>;

type BuildingDetailCleanupRacksPage = Pick<
  RacksPage,
  | "clickViewList"
  | "deleteRackByLabelIfVisible"
  | "listRackNames"
  | "navigateToRacksPage"
  | "tryAction"
  | "waitForRackListToLoad"
>;

export type BuildingDetailScenarioData = {
  siteName: string;
  buildingName: string;
  renamedBuildingName: string;
  siblingBuildingName: string;
  rackLabel: string;
  powerCapacityMw: string;
};

function createRunPrefixes(testInfo: TestInfo): BuildingDetailRunPrefixes {
  const runPrefix = `${getSafeProjectName(testInfo)}_${testInfo.workerIndex}_${testInfo.retry}`;

  return {
    sitePrefix: generateRandomText(`${AUTOMATION_SITE_BASE_PREFIX}_${runPrefix}`),
    buildingPrefix: generateRandomText(`${AUTOMATION_BUILDING_BASE_PREFIX}_${runPrefix}`),
    rackPrefix: generateRandomText(`${AUTOMATION_RACK_BASE_PREFIX}_${runPrefix}`),
  };
}

function getRunPrefixes(testInfo: TestInfo): BuildingDetailRunPrefixes {
  const prefixes = testRunPrefixes.get(getTestRunKey(testInfo));
  if (!prefixes) {
    throw new Error("Building detail E2E run prefixes were not initialized.");
  }

  return prefixes;
}

export function createBuildingDetailScenarioData(testInfo: TestInfo): BuildingDetailScenarioData {
  const prefixes = getRunPrefixes(testInfo);

  return {
    siteName: generateRandomText(`${prefixes.sitePrefix}_primary`),
    buildingName: generateRandomText(`${prefixes.buildingPrefix}_primary`),
    renamedBuildingName: generateRandomText(`${prefixes.buildingPrefix}_renamed`),
    siblingBuildingName: generateRandomText(`${prefixes.buildingPrefix}_sibling`),
    rackLabel: generateRandomText(`${prefixes.rackPrefix}_primary`),
    powerCapacityMw: "4.2",
  };
}

async function cleanupAutomationFixtures(
  fleetLocationsPage: BuildingDetailCleanupFleetLocationsPage,
  racksPage: BuildingDetailCleanupRacksPage,
  prefixes: BuildingDetailRunPrefixes,
) {
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

  const rackNames = (await racksPage.listRackNames(SHORT_CLEANUP_TIMEOUT)).filter((name) =>
    name.startsWith(prefixes.rackPrefix),
  );
  for (const rackName of rackNames) {
    await racksPage.deleteRackByLabelIfVisible(rackName, SHORT_CLEANUP_TIMEOUT);
  }

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

async function cleanupAutomationBuildingDetail(browser: Browser, testInfo: TestInfo) {
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
    const racksPage = new RacksPage(page, isMobile);
    const fleetLocationsPage = new FleetLocationsPage(page, isMobile);
    const commonSteps = new CommonSteps(authPage, minersPage);

    await commonSteps.loginAsAdmin();
    await cleanupAutomationFixtures(fleetLocationsPage, racksPage, prefixes);
  } finally {
    await context.close();
  }
}

export function useBuildingDetailHooks() {
  test.beforeEach(async ({ page, commonSteps, fleetLocationsPage, racksPage }, testInfo) => {
    testRunPrefixes.set(getTestRunKey(testInfo), createRunPrefixes(testInfo));

    await installAllSitesInitScript(page);
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await cleanupAutomationFixtures(fleetLocationsPage, racksPage, getRunPrefixes(testInfo));
  });

  test.afterEach("CLEANUP: Delete automation building detail fixtures", async ({ browser }, testInfo) => {
    try {
      await cleanupAutomationBuildingDetail(browser, testInfo);
    } finally {
      testRunPrefixes.delete(getTestRunKey(testInfo));
    }
  });
}

export async function setupBuildingDetailScenario(
  page: Page,
  fleetLocationsPage: FleetLocationsPage,
  racksPage: RacksPage,
  scenario: BuildingDetailScenarioData,
) {
  return await test.step("Create a site, two buildings, and an assigned rack for building detail coverage", async () => {
    await fleetLocationsPage.createSite(scenario.siteName);
    const buildingId = await fleetLocationsPage.createBuilding(scenario.siteName, scenario.buildingName);
    await fleetLocationsPage.createBuilding(scenario.siteName, scenario.siblingBuildingName);

    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, scenario.rackLabel);
    await assignRackToBuilding(page, racksPage, scenario.rackLabel, rackId, scenario.buildingName, buildingId);

    return { buildingId, rackId, selectedMinerIps };
  });
}

export async function setupBuildingDetailDeletionScenario(
  page: Page,
  fleetLocationsPage: FleetLocationsPage,
  racksPage: RacksPage,
  scenario: BuildingDetailScenarioData,
) {
  return await test.step("Create a site, one building, and an assigned rack for the delete scenario", async () => {
    await fleetLocationsPage.createSite(scenario.siteName);
    const buildingId = await fleetLocationsPage.createBuilding(scenario.siteName, scenario.buildingName);

    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, scenario.rackLabel);
    await assignRackToBuilding(page, racksPage, scenario.rackLabel, rackId, scenario.buildingName, buildingId);

    return { buildingId, rackId, selectedMinerIps };
  });
}

export async function validateBuildingDetailScenarioAcrossTabs({
  page,
  fleetLocationsPage,
  minersPage,
  racksPage,
  scenario,
  selectedMinerIps,
}: {
  page: Page;
  fleetLocationsPage: FleetLocationsPage;
  minersPage: MinersPage;
  racksPage: RacksPage;
  scenario: {
    siteName: string;
    buildingName: string;
    siblingBuildingName: string;
    rackLabel: string;
  };
  selectedMinerIps: string[];
}) {
  await test.step("Validate the renamed building and sibling counts across the Fleet tabs", async () => {
    await validateSiteAndBuildingCounts(fleetLocationsPage, {
      siteName: scenario.siteName,
      siteCounts: {
        buildings: 2,
        racks: 1,
        miners: 2,
      },
      buildings: [
        { buildingName: scenario.buildingName, racks: 1, miners: 2 },
        { buildingName: scenario.siblingBuildingName, racks: 0, miners: 0 },
      ],
    });
  });

  await test.step("Validate the building detail View racks button opens the scoped racks list", async () => {
    await fleetLocationsPage.openBuildingDetail(scenario.buildingName);
    await fleetLocationsPage.openRacksFromBuildingDetail();
    await racksPage.clickViewList();
    await racksPage.waitForRackListToLoad({ allowEmpty: false });
    await racksPage.validateRackPlacementRow(scenario.rackLabel, scenario.siteName, scenario.buildingName);
  });

  await test.step("Validate the building detail View miners button opens the scoped miners list", async () => {
    await fleetLocationsPage.openBuildingDetail(scenario.buildingName);
    await fleetLocationsPage.openMinersFromBuildingDetail();
    await minersPage.waitForMinersTitle();
    await minersPage.waitForMinersListToLoad();

    for (const ipAddress of selectedMinerIps) {
      await test.expect.poll(() => minersPage.getMinerColumnText(ipAddress, "site")).toBe(scenario.siteName);
      await test.expect.poll(() => minersPage.getMinerColumnText(ipAddress, "building")).toBe(scenario.buildingName);
      await test.expect.poll(() => minersPage.getMinerColumnText(ipAddress, "rack")).toBe(scenario.rackLabel);
    }
  });

  await test.step("Validate the racks and miners tabs still show the renamed building placement", async () => {
    await validateRackAndMinerPlacementAcrossTabs({
      page,
      minersPage,
      racksPage,
      siteName: scenario.siteName,
      buildingName: scenario.buildingName,
      rackLabel: scenario.rackLabel,
      selectedMinerIps,
    });
  });
}
