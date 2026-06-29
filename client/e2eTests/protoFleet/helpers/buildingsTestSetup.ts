import { type Browser, type Page, type TestInfo } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { AuthPage } from "../pages/auth";
import { FleetLocationsPage } from "../pages/fleetLocations";
import { MinersPage } from "../pages/miners";
import { RacksPage } from "../pages/racks";
import { CommonSteps } from "./commonSteps";
import { addSelectableMinersToSlots } from "./racksHelpers";
import { generateRandomText } from "./testDataHelper";

const ASSIGN_RACKS_TO_BUILDING = "AssignRacksToBuilding";
const AUTOMATION_SITE_PREFIX = "automation_buildings_site";
const AUTOMATION_BUILDING_PREFIX = "automation_buildings_building";
const AUTOMATION_RACK_PREFIX = "automation_buildings_rack";
const TEMP_ZONE = "AutomationBuildingsZone";
const RACK_COLUMNS = 2;
const RACK_ROWS = 2;
const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";

type BuildingsCleanupFleetLocationsPage = Pick<
  FleetLocationsPage,
  "deleteBuildingByNameIfVisible" | "deleteSiteByNameIfVisible" | "listBuildingNames" | "listSiteNames"
>;

type BuildingsCleanupRacksPage = Pick<
  RacksPage,
  | "clickViewList"
  | "deleteRackByLabelIfVisible"
  | "listRackNames"
  | "navigateToRacksPage"
  | "tryAction"
  | "waitForRackListToLoad"
>;

export type BuildingsScenarioData = {
  siteName: string;
  buildingName: string;
  rackLabel: string;
};

export function createBuildingsScenarioData(): BuildingsScenarioData {
  return {
    siteName: generateRandomText(AUTOMATION_SITE_PREFIX),
    buildingName: generateRandomText(AUTOMATION_BUILDING_PREFIX),
    rackLabel: generateRandomText(AUTOMATION_RACK_PREFIX),
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
  fleetLocationsPage: BuildingsCleanupFleetLocationsPage,
  racksPage: BuildingsCleanupRacksPage,
) {
  await racksPage.navigateToRacksPage();
  await racksPage.tryAction(() => racksPage.clickViewList());
  await racksPage.waitForRackListToLoad();

  const rackNames = (await racksPage.listRackNames()).filter((name) => name.startsWith(AUTOMATION_RACK_PREFIX));
  for (const rackName of rackNames) {
    await racksPage.deleteRackByLabelIfVisible(rackName);
  }

  const buildingNames = (await fleetLocationsPage.listBuildingNames()).filter((name) =>
    name.startsWith(AUTOMATION_BUILDING_PREFIX),
  );
  for (const buildingName of buildingNames) {
    await fleetLocationsPage.deleteBuildingByNameIfVisible(buildingName);
  }

  const siteNames = (await fleetLocationsPage.listSiteNames()).filter((name) =>
    name.startsWith(AUTOMATION_SITE_PREFIX),
  );
  for (const siteName of siteNames) {
    await fleetLocationsPage.deleteSiteByNameIfVisible(siteName);
  }
}

async function cleanupAutomationBuildings(browser: Browser, testInfo: TestInfo) {
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
    await cleanupAutomationFixtures(fleetLocationsPage, racksPage);
  } finally {
    await context.close();
  }
}

export function useBuildingsHooks() {
  test.beforeEach(async ({ page, commonSteps, fleetLocationsPage, racksPage }) => {
    await installAllSitesInitScript(page);
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await cleanupAutomationFixtures(fleetLocationsPage, racksPage);
  });

  test.afterEach("CLEANUP: Delete automation buildings fixtures", async ({ browser }, testInfo) => {
    await cleanupAutomationBuildings(browser, testInfo);
  });
}

export async function createSiteAndBuilding(
  fleetLocationsPage: FleetLocationsPage,
  scenario: BuildingsScenarioData,
): Promise<bigint> {
  return await test.step("Create a site and building", async () => {
    await fleetLocationsPage.createSite(scenario.siteName);
    return await fleetLocationsPage.createBuilding(scenario.siteName, scenario.buildingName);
  });
}

export async function createRackWithAssignedMiners(
  racksPage: RacksPage,
  rackLabel: string,
): Promise<{ rackId: bigint; selectedMinerIps: string[] }> {
  return await test.step("Create a rack with two miners assigned", async () => {
    await racksPage.navigateToRacksPage();
    await racksPage.clickAddRackButton();
    await racksPage.inputZone(TEMP_ZONE);
    await racksPage.inputRackLabel(rackLabel);
    await racksPage.enableCustomRackLayout();
    await racksPage.inputColumns(RACK_COLUMNS);
    await racksPage.inputRows(RACK_ROWS);
    await racksPage.clickContinueFromRackSettings();

    const selectedMinerIps = (await addSelectableMinersToSlots(racksPage, 2, [1, 2])).map((miner) => miner.ipAddress);
    test.expect(selectedMinerIps).toHaveLength(2);

    await racksPage.clickSaveRack();
    await racksPage.validateRackToast(rackLabel);
    await racksPage.clickViewList();
    await racksPage.waitForRackListToLoad({ allowEmpty: false });

    return {
      rackId: await racksPage.getRackIdByLabel(rackLabel),
      selectedMinerIps,
    };
  });
}

export async function assignRackToBuilding(
  page: Page,
  racksPage: RacksPage,
  rackLabel: string,
  rackId: bigint,
  buildingName: string,
  buildingId: bigint,
) {
  await test.step("Move the rack into the building from the racks tab", async () => {
    const requestPromise = page.waitForRequest(new RegExp(ASSIGN_RACKS_TO_BUILDING));
    const responsePromise = page.waitForResponse(new RegExp(ASSIGN_RACKS_TO_BUILDING));

    await racksPage.assignRackToBuildingFromList(rackLabel, buildingName);

    const request = await requestPromise;
    const response = await responsePromise;
    const body = request.postDataJSON() as {
      targetBuildingId?: string;
      racks: Array<{ rackId?: string }>;
    };

    test.expect(String(body.targetBuildingId)).toBe(buildingId.toString());
    test.expect(body.racks).toHaveLength(1);
    test.expect(String(body.racks[0]?.rackId)).toBe(rackId.toString());
    test.expect(response.status()).toBe(200);
  });
}

export async function validateBuildingPlacementAcrossTabs({
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
  scenario: BuildingsScenarioData;
  selectedMinerIps: string[];
}) {
  await test.step("Validate the sites tab shows the correct counts", async () => {
    await fleetLocationsPage.validateSiteRowCounts(scenario.siteName, {
      buildings: 1,
      racks: 1,
      miners: 2,
    });
  });

  await test.step("Validate the buildings tab shows the correct counts", async () => {
    await fleetLocationsPage.validateBuildingRowCounts(scenario.buildingName, {
      siteName: scenario.siteName,
      racks: 1,
      miners: 2,
    });
  });

  await test.step("Validate the racks tab shows the building placement", async () => {
    await racksPage.navigateToRacksPage();
    await racksPage.clickViewList();
    await racksPage.waitForRackListToLoad({ allowEmpty: false });
    await racksPage.validateRackRow(scenario.rackLabel, TEMP_ZONE, 2);
    await racksPage.validateRackPlacementRow(scenario.rackLabel, scenario.siteName, scenario.buildingName);
  });

  await test.step("Validate the miners tab shows the building placement for both miners", async () => {
    await page.goto("/fleet/miners");
    await minersPage.waitForMinersTitle();
    await minersPage.waitForMinersListToLoad();

    for (const ipAddress of selectedMinerIps) {
      test.expect(await minersPage.getMinerColumnText(ipAddress, "site")).toBe(scenario.siteName);
      test.expect(await minersPage.getMinerColumnText(ipAddress, "building")).toBe(scenario.buildingName);
      test.expect(await minersPage.getMinerColumnText(ipAddress, "rack")).toBe(scenario.rackLabel);
    }
  });
}
